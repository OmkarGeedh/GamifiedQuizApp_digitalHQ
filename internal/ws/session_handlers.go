package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/gorilla/websocket"
)

// onPlayerAttached handles player binding, unpausing the timer or serving initial state.
func (gs *GameSession) onPlayerAttached(ctx context.Context, newPlayer *PlayerConn, qTimer *time.Timer, tTimer *time.Timer, dTimer *time.Timer) {
	// Stop disconnect eviction timer
	stopTimer(dTimer)

	gs.mu.Lock()
	oldPlayer := gs.Player
	gs.Player = newPlayer
	gs.mu.Unlock()

	// Gracefully disconnect previous socket if different
	if oldPlayer != nil && oldPlayer.Conn != newPlayer.Conn {
		oldPlayer.CloseGracefully("superseded by new connection")
	}

	switch gs.state {
	case stateWaiting:
		// Initial attach or resumed from transition while disconnected
		gs.timeRemaining = questionTimeLimitSec * time.Second
		gs.broadcastQuestion(int(gs.timeRemaining.Milliseconds()))
		gs.questionDeadline = time.Now().Add(gs.timeRemaining)
		resetTimer(qTimer, gs.timeRemaining)
		gs.state = stateQuestionActive

	case statePaused:
		// Resume paused question with exact remaining time
		remainingMs := int(gs.timeRemaining.Milliseconds())
		if remainingMs <= 0 {
			// The clock ran out while disconnected. Grade the timeout now; tTimer
			// must be threaded through or the 800ms transition never re-arms and
			// the session wedges in stateTransitioning.
			gs.state = stateQuestionActive
			gs.handleTimeout(ctx, tTimer)
		} else {
			gs.questionDeadline = time.Now().Add(gs.timeRemaining)
			gs.broadcastQuestion(remainingMs)
			resetTimer(qTimer, gs.timeRemaining)
			gs.state = stateQuestionActive
		}

	case stateQuestionActive:
		// Reconnect while already running: resync remaining time
		remaining := gs.remainingAt(time.Now())
		gs.broadcastQuestion(int(remaining.Milliseconds()))

	case stateTransitioning:
		// Player attached during 800ms transition; wait for transition timer to complete

	case stateEnded:
		newPlayer.CloseGracefully("game already finished")
	}
}

// onPlayerDetached handles player disconnection, pausing the question timer.
//
// Only the connection that currently owns the session may mutate it. A superseded
// socket (reconnect, second tab) still fires its handler's deferred DetachPlayer
// after the replacement has attached; without the identity guard that stale event
// pauses the live player's question timer and arms the eviction timer, leaving a
// connected player unable to answer and the session doomed to be abandoned.
func (gs *GameSession) onPlayerDetached(conn *websocket.Conn, qTimer *time.Timer, dTimer *time.Timer) {
	gs.mu.Lock()
	isActivePlayer := gs.Player != nil && conn != nil && gs.Player.Conn == conn
	var oldPlayer *PlayerConn
	if isActivePlayer {
		oldPlayer = gs.Player
		gs.Player = nil
	}
	gs.mu.Unlock()

	if !isActivePlayer {
		return
	}

	if oldPlayer != nil {
		oldPlayer.Close()
	}

	if gs.state == stateQuestionActive {
		gs.timeRemaining = gs.remainingAt(time.Now())
		gs.questionDeadline = time.Time{}
		stopTimer(qTimer)
		gs.state = statePaused
	}

	// Start 5-minute disconnect eviction timer
	dTimer.Reset(disconnectTTL)
}

// handleJoinGame handles client resynchronization requests.
func (gs *GameSession) handleJoinGame(ctx context.Context, rawData json.RawMessage, qTimer *time.Timer, tTimer *time.Timer) {
	var data JoinGameData
	if len(rawData) > 0 {
		_ = json.Unmarshal(rawData, &data)
	}

	if data.Session != "" && data.Session != gs.ID {
		gs.sendError(ErrCodeSessionMismatch, fmt.Sprintf("session mismatch: expected %s", gs.ID))
		return
	}

	if gs.state == stateEnded || gs.Session.Status != models.SessionStatusInProgress {
		gs.sendError(ErrCodeSessionNotActive, "session has finished or been abandoned")
		return
	}

	switch gs.state {
	case stateQuestionActive:
		if gs.expireIfDeadlineReached(ctx, time.Now(), qTimer, tTimer) {
			return
		}
		remaining := gs.remainingAt(time.Now())
		gs.broadcastQuestion(int(remaining.Milliseconds()))

	case statePaused:
		gs.broadcastQuestion(int(gs.timeRemaining.Milliseconds()))

	case stateWaiting:
		gs.broadcastQuestion(questionTimeLimitSec * 1000)

	case stateTransitioning:
		// The next question is mid-transition and has no timer yet. Answer with an
		// explicit code instead of silence so the client can retry.
		gs.sendError(ErrCodeTransitionInProgress, "question is transitioning, retry shortly")
	}
}

// handleUsePowerUp processes mode-specific power-ups against the authoritative
// question deadline.
func (gs *GameSession) handleUsePowerUp(ctx context.Context, rawData json.RawMessage, qTimer *time.Timer, tTimer *time.Timer) {
	var data UsePowerUpData
	if err := json.Unmarshal(rawData, &data); err != nil {
		gs.sendError(ErrCodeInvalidPayload, "frame could not be unmarshalled")
		return
	}

	if data.Session != "" && data.Session != gs.ID {
		gs.sendError(ErrCodeSessionMismatch, fmt.Sprintf("session mismatch: expected %s", gs.ID))
		return
	}

	if gs.Session.Status != models.SessionStatusInProgress || gs.state == stateEnded {
		gs.sendError(ErrCodeSessionNotActive, "session has finished or been abandoned")
		return
	}

	if gs.state != stateQuestionActive {
		gs.sendError(ErrCodeStaleAnswer, "question is not currently active")
		return
	}
	if gs.expireIfDeadlineReached(ctx, time.Now(), qTimer, tTimer) {
		return
	}

	if gs.Session.CurrentIdx >= len(gs.Questions) {
		return
	}
	q := gs.Questions[gs.Session.CurrentIdx]

	cleanQuestion := strings.TrimSpace(data.Question)
	if cleanQuestion == "" {
		gs.sendError(ErrCodeMissingQuestion, "use_power_up sent without a question")
		return
	}

	if cleanQuestion != q.QuestionCode {
		gs.sendError(ErrCodeStaleAnswer, fmt.Sprintf("stale answer: current question is %s", q.QuestionCode))
		return
	}

	powerUp := strings.ToLower(strings.TrimSpace(data.PowerUp))
	switch powerUp {
	case "add_time":
		if !gs.Session.IsSuddenDeath() {
			gs.sendError(ErrCodeUnsupportedPowerUp, "add_time is only supported for sudden_death")
			return
		}
		if gs.addTimeUsed {
			gs.sendError(ErrCodePowerUpAlreadyUsed, "add_time power-up has already been used in this session")
			return
		}

		now := time.Now()
		gs.questionDeadline = gs.questionDeadline.Add(addTimeDuration)
		remaining := gs.remainingAt(now)
		gs.timeRemaining = remaining
		gs.addTimeUsed = true
		resetTimer(qTimer, remaining)

		gs.sendToPlayer(OutboundMessage{
			Type: MsgTypePowerUpResult,
			Data: PowerUpResultPayload{
				Question:          q.QuestionCode,
				PowerUp:           powerUp,
				AddedTimeMs:       int(addTimeDuration.Milliseconds()),
				RemainingTimeMs:   int(remaining.Milliseconds()),
				ServerTimestampMs: time.Now().UnixMilli(),
			},
		})

	case "fifty_fifty":
		if gs.Session.IsSuddenDeath() {
			gs.sendError(ErrCodeUnsupportedPowerUp, "fifty_fifty is not supported for sudden_death")
			return
		}
		if gs.fiftyFiftyUsed {
			gs.sendError(ErrCodePowerUpAlreadyUsed, "fifty_fifty power-up has already been used in this session")
			return
		}
		gs.fiftyFiftyUsed = true

		correct := strings.ToLower(strings.TrimSpace(q.CorrectOption))
		wrongOptions := make([]string, 0, 3)
		options := map[string]string{"a": q.OptionA, "b": q.OptionB, "c": q.OptionC, "d": q.OptionD}
		for _, opt := range []string{"a", "b", "c", "d"} {
			if strings.TrimSpace(options[opt]) != "" && !strings.EqualFold(opt, correct) {
				wrongOptions = append(wrongOptions, opt)
			}
		}
		rand.Shuffle(len(wrongOptions), func(i, j int) {
			wrongOptions[i], wrongOptions[j] = wrongOptions[j], wrongOptions[i]
		})
		hidden := wrongOptions
		if len(wrongOptions) >= 2 {
			hidden = wrongOptions[:2]
		}

		gs.sendToPlayer(OutboundMessage{
			Type: MsgTypePowerUpResult,
			Data: PowerUpResultPayload{
				Question:          q.QuestionCode,
				PowerUp:           powerUp,
				HiddenOptions:     hidden,
				ServerTimestampMs: time.Now().UnixMilli(),
			},
		})

	default:
		gs.sendError(ErrCodeUnsupportedPowerUp, "unsupported power_up")
	}
}

// handleAnswer evaluates client answer submissions and initiates question progression.
func (gs *GameSession) handleAnswer(ctx context.Context, rawData json.RawMessage, qTimer *time.Timer, tTimer *time.Timer) {
	var data SubmitAnswerData
	if err := json.Unmarshal(rawData, &data); err != nil {
		gs.sendError(ErrCodeInvalidPayload, "frame could not be unmarshalled")
		return
	}

	if data.Session != "" && data.Session != gs.ID {
		gs.sendError(ErrCodeSessionMismatch, fmt.Sprintf("session mismatch: expected %s", gs.ID))
		return
	}

	if gs.Session.Status != models.SessionStatusInProgress || gs.state == stateEnded {
		gs.sendError(ErrCodeSessionNotActive, "session has finished or been abandoned")
		return
	}

	if gs.state != stateQuestionActive {
		gs.sendError(ErrCodeStaleAnswer, "question is not currently active")
		return
	}
	if gs.expireIfDeadlineReached(ctx, time.Now(), qTimer, tTimer) {
		return
	}

	if gs.Session.CurrentIdx >= len(gs.Questions) {
		return
	}
	q := gs.Questions[gs.Session.CurrentIdx]

	cleanQuestion := strings.TrimSpace(data.Question)
	if cleanQuestion == "" {
		gs.sendError(ErrCodeMissingQuestion, "submit_answer sent without a question")
		return
	}

	if cleanQuestion != q.QuestionCode {
		gs.sendError(ErrCodeStaleAnswer, fmt.Sprintf("stale answer: current question is %s", q.QuestionCode))
		return
	}

	cleanOption := strings.TrimSpace(data.Option)
	if cleanOption == "" {
		gs.sendError(ErrCodeMissingOption, "submit_answer sent without an option")
		return
	}

	// Stop question timer immediately upon answer receipt
	stopTimer(qTimer)

	isCorrect, isSkipped, pointsEarned, resolvedCorrect := scoreAnswer(gs.Session, &q, cleanOption, data.SelectedText)
	currentScore := gs.Session.Score

	// Clamp time taken for audit recording
	timeTaken := data.TimeTakenMs
	if timeTaken < 0 {
		timeTaken = 0
	} else if timeTaken > 17000 {
		timeTaken = 17000
	}

	historyRecord := &models.UserQuestionHistory{
		ClientID:       gs.ClientID,
		SessionID:      gs.Session.ID,
		GameMode:       gs.Session.GameMode,
		QuestionCode:   q.QuestionCode,
		SelectedOption: cleanOption,
		IsCorrect:      isCorrect,
		TimeTakenMs:    timeTaken,
		PointsAwarded:  pointsEarned,
		CoinsAwarded:   0,
	}

	// Send answer result frame immediately to the player for zero perceived latency
	gs.sendToPlayer(OutboundMessage{
		Type: MsgTypeAnswerResult,
		Data: AnswerResultPayload{
			Question:      q.QuestionCode,
			Option:        cleanOption,
			CorrectOption: strings.ToLower(resolvedCorrect),
			IsCorrect:     isCorrect,
			IsSkipped:     isSkipped,
			Explanation:   q.Explanation,
			PointsEarned:  pointsEarned,
			CoinsEarned:   0,
			YourScore:     currentScore,
			IsTimeout:     false,
		},
	})

	// Asynchronously persist answer history so database disk I/O does not delay client feedback
	go func(rec *models.UserQuestionHistory) {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := repo.RecordAnswer(bgCtx, rec); err != nil {
			log.Printf("[ws] session %s: async persist answer failed for %s: %v", gs.ID, rec.QuestionCode, err)
		}
	}(historyRecord)

	// Sudden Death: the first genuinely wrong answer ends the run. Skips are
	// excluded on purpose — a skip is a purchased power-up that bypasses a
	// question safely, so it must not eliminate the player.
	if gs.Session.IsSuddenDeath() && !isCorrect && !isSkipped {
		gs.endReason = EndReasonWrongAnswer
		gs.handleGameEnd(ctx)
		return
	}

	gs.advanceQuestion(ctx, tTimer)
}

// handleTimeout marks the active question timed out and advances.
func (gs *GameSession) handleTimeout(ctx context.Context, tTimer *time.Timer) {
	if gs.Session.CurrentIdx >= len(gs.Questions) {
		return
	}
	q := gs.Questions[gs.Session.CurrentIdx]
	gs.Session.ComboStreak = 0
	score := gs.Session.Score

	historyRecord := &models.UserQuestionHistory{
		ClientID:       gs.ClientID,
		SessionID:      gs.Session.ID,
		GameMode:       gs.Session.GameMode,
		QuestionCode:   q.QuestionCode,
		SelectedOption: "timeout",
		IsCorrect:      false,
		TimeTakenMs:    questionTimeLimitSec * 1000,
		PointsAwarded:  0,
		CoinsAwarded:   0,
	}

	// Send timeout result frame immediately to the player
	gs.sendToPlayer(OutboundMessage{
		Type: MsgTypeAnswerResult,
		Data: AnswerResultPayload{
			Question:      q.QuestionCode,
			Option:        "timeout",
			CorrectOption: strings.ToLower(q.CorrectOption),
			IsCorrect:     false,
			IsSkipped:     false,
			Explanation:   q.Explanation,
			PointsEarned:  0,
			CoinsEarned:   0,
			YourScore:     score,
			IsTimeout:     true,
		},
	})

	// Asynchronously persist timeout
	go func(rec *models.UserQuestionHistory) {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := repo.RecordAnswer(bgCtx, rec); err != nil {
			log.Printf("[ws] session %s: async persist timeout failed for %s: %v", gs.ID, rec.QuestionCode, err)
		}
	}(historyRecord)

	// Sudden Death: letting the clock run out scores exactly like a wrong
	// answer (zero points, streak reset), so it eliminates too. The player does
	// get the answer_result frame first and can see what they missed.
	if gs.Session.IsSuddenDeath() {
		gs.endReason = EndReasonTimeout
		gs.handleGameEnd(ctx)
		return
	}

	gs.advanceQuestion(ctx, tTimer)
}

// advanceQuestion increments question index and starts the 800ms transition delay.
func (gs *GameSession) advanceQuestion(ctx context.Context, tTimer *time.Timer) {
	gs.Session.CurrentIdx++
	idx := gs.Session.CurrentIdx
	total := len(gs.Questions)

	// Persist progress synchronously. This write is a full-row Save that still
	// carries status=in_progress, so it must commit before handleGameEnd runs
	// FinalizeSession; a deferred write would land after the rewards transaction
	// and revert the finished session back to in_progress.
	if err := repo.UpdateSession(ctx, gs.Session); err != nil {
		log.Printf("[ws] session %s: failed to persist question progression: %v", gs.ID, err)
		gs.stopAfterPersistenceFailure("failed to persist question progression")
		return
	}

	if idx >= total {
		gs.handleGameEnd(ctx)
		return
	}

	gs.state = stateTransitioning
	if tTimer != nil {
		tTimer.Reset(transitionDelayMs * time.Millisecond)
	}
}

// handleTransitionDone is called when the 800ms transition delay expires.
func (gs *GameSession) handleTransitionDone(ctx context.Context, qTimer *time.Timer) {
	if gs.Session.CurrentIdx >= len(gs.Questions) {
		gs.handleGameEnd(ctx)
		return
	}

	gs.mu.Lock()
	player := gs.Player
	gs.mu.Unlock()

	if player == nil {
		// Player disconnected during transition; hold until reconnection (Guarantee 4)
		gs.state = stateWaiting
		return
	}

	// Serve next question with full 15s timer
	gs.broadcastQuestion(questionTimeLimitSec * 1000)
	gs.timeRemaining = questionTimeLimitSec * time.Second
	gs.questionDeadline = time.Now().Add(gs.timeRemaining)
	resetTimer(qTimer, gs.timeRemaining)
	gs.state = stateQuestionActive
}

// handleGameEnd finalizes scoring and economy atomically and terminates the socket.
func (gs *GameSession) handleGameEnd(ctx context.Context) {
	gs.mu.Lock()
	if gs.state == stateEnded || gs.Session.Status != models.SessionStatusInProgress {
		gs.mu.Unlock()
		return
	}
	gs.state = stateEnded
	// Default to a clean completion; terminal failure paths set this first.
	if gs.endReason == "" {
		if gs.Session.IsSuddenDeath() {
			gs.endReason = EndReasonCompleted
		} else {
			gs.endReason = EndReasonCleared
		}
	}
	endReason := gs.endReason
	gs.mu.Unlock()

	gameOver, err := finalizeRun(ctx, gs.ClientID, gs.Session, endReason)
	if errors.Is(err, errPrepareRewards) {
		log.Printf("[ws] session %s: %v", gs.ID, err)
		gs.stopAfterPersistenceFailure("failed to prepare game rewards")
		return
	}
	if err != nil {
		log.Printf("[ws] session %s: finalization failed: %v", gs.ID, err)
		gs.sendError(ErrCodeInternalError, "failed to finalize game session")
		gs.mu.Lock()
		player := gs.Player
		gs.mu.Unlock()
		if player != nil {
			player.CloseGracefully("game finalization failed")
		}
		return
	}
	gs.sendToPlayer(OutboundMessage{Type: MsgTypeGameOver, Data: *gameOver})

	// Graceful close: wait 1s grace period, then close connection (Guarantee 1)
	gs.mu.Lock()
	player := gs.Player
	gs.mu.Unlock()

	if player != nil {
		go func(p *PlayerConn) {
			time.Sleep(1 * time.Second)
			p.CloseGracefully("game finished")
		}(player)
	}
}

// handleDisconnectTimeout cleans up sessions abandoned by prolonged client disconnect.
// It reports whether the session was terminated. A false return means the player
// reattached before the eviction timer fired, so the caller must keep serving.
func (gs *GameSession) handleDisconnectTimeout(ctx context.Context) bool {
	gs.mu.Lock()
	if gs.state == stateEnded || gs.Player != nil {
		gs.mu.Unlock()
		return false
	}
	gs.state = stateEnded
	gs.mu.Unlock()

	now := time.Now().UTC()
	gs.Session.Status = models.SessionStatusAbandoned
	gs.Session.EndReason = EndReasonAbandoned
	gs.Session.CompletedSuccessfully = false
	gs.Session.EndedAt = &now
	if err := repo.UpdateSession(ctx, gs.Session); err != nil {
		log.Printf("[ws] session %s: failed to persist disconnect abandonment: %v", gs.ID, err)
	}
	services.ClearSessionTTL(ctx, gs.ID)
	return true
}

func (gs *GameSession) remainingAt(now time.Time) time.Duration {
	if gs.questionDeadline.IsZero() {
		if gs.timeRemaining < 0 {
			return 0
		}
		return gs.timeRemaining
	}
	remaining := gs.questionDeadline.Sub(now)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (gs *GameSession) expireIfDeadlineReached(ctx context.Context, now time.Time, qTimer *time.Timer, tTimer *time.Timer) bool {
	if gs.questionDeadline.IsZero() || now.Before(gs.questionDeadline) {
		return false
	}
	stopTimer(qTimer)
	gs.handleTimeout(ctx, tTimer)
	return true
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	stopTimer(timer)
	if duration < 0 {
		duration = 0
	}
	timer.Reset(duration)
}

func (gs *GameSession) stopAfterPersistenceFailure(message string) {
	gs.sendError(ErrCodeInternalError, message)
	gs.mu.Lock()
	gs.state = stateEnded
	player := gs.Player
	gs.mu.Unlock()
	if player != nil {
		player.CloseGracefully("game state persistence failed")
	}
	gs.Stop()
}

// broadcastQuestion delivers the active question payload to the connected player.
func (gs *GameSession) broadcastQuestion(remainingMs int) {
	if gs.Session.CurrentIdx >= len(gs.Questions) {
		return
	}
	idx := gs.Session.CurrentIdx
	var payload QuestionPayload
	if idx < len(gs.cachedQuestions) {
		payload = gs.cachedQuestions[idx]
	} else {
		q := gs.Questions[idx]
		total := gs.Session.TotalQuestions
		opts := make([]OptionPayload, 0, 4)
		if strings.TrimSpace(q.OptionA) != "" {
			opts = append(opts, OptionPayload{Option: "a", Text: q.OptionA})
		}
		if strings.TrimSpace(q.OptionB) != "" {
			opts = append(opts, OptionPayload{Option: "b", Text: q.OptionB})
		}
		if strings.TrimSpace(q.OptionC) != "" {
			opts = append(opts, OptionPayload{Option: "c", Text: q.OptionC})
		}
		if strings.TrimSpace(q.OptionD) != "" {
			opts = append(opts, OptionPayload{Option: "d", Text: q.OptionD})
		}

		payload = QuestionPayload{
			Question:       q.QuestionCode,
			Prompt:         q.Prompt,
			Points:         q.Points,
			Hint:           q.Hint,
			Options:        opts,
			TimeLimitMs:    questionTimeLimitSec * 1000,
			QuestionNumber: idx + 1,
			TotalQuestions: total,
		}
	}
	payload.RemainingTimeMs = remainingMs
	payload.ServerTimestampMs = time.Now().UnixMilli()

	gs.sendToPlayer(OutboundMessage{
		Type: MsgTypeQuestion,
		Data: payload,
	})
}

// sendToPlayer safely writes an outbound frame to the attached player.
func (gs *GameSession) sendToPlayer(msg OutboundMessage) {
	gs.mu.Lock()
	player := gs.Player
	gs.mu.Unlock()

	if player == nil {
		return
	}
	_ = player.WriteJSON(msg)
}

// sendError sends a machine-readable error frame to the attached player.
func (gs *GameSession) sendError(code, message string) {
	gs.sendToPlayer(OutboundMessage{
		Type: MsgTypeError,
		Data: ErrorPayload{
			Code:    code,
			Message: message,
		},
	})
}

// scoreAnswer grades option for q and applies streak/score/skip bookkeeping to s.
func scoreAnswer(s *models.GameSession, q *models.SessionQuestion, option, selectedText string) (isCorrect, isSkipped bool, pointsEarned int, resolvedCorrect string) {
	if strings.EqualFold(option, "skip") {
		s.ComboStreak = 0
		if s.IsSuddenDeath() {
			s.SkippedCount++
		}
		return false, true, 0, q.CorrectOption
	}
	isCorrect, _, resolvedCorrect = services.GradeAnswer(q, nil, option, selectedText)
	if !isCorrect {
		s.ComboStreak = 0
		return false, false, 0, resolvedCorrect
	}
	s.ComboStreak++
	if s.ComboStreak > s.BestStreak {
		s.BestStreak = s.ComboStreak
	}
	s.CorrectCount++
	if s.IsSuddenDeath() {
		pointsEarned = services.CalculateSuddenDeathAnswerPoints(true, s.ComboStreak)
		if s.ComboStreak%services.SuddenDeathStreakSize == 0 {
			s.StreakMilestones++
		}
	} else {
		pointsEarned = services.CalculateMCQAnswerPoints(true)
	}
	s.Score += pointsEarned
	return true, false, pointsEarned, resolvedCorrect
}

var errPrepareRewards = errors.New("failed to load profile for reward calculation")

// finalizeRun computes the final score and rewards for a finished run, commits them
// atomically and returns the game_over payload. s.Status becomes finished.
func finalizeRun(ctx context.Context, clientID int, s *models.GameSession, endReason string) (*GameOverPayload, error) {
	completedSuccessfully := endReason == EndReasonCompleted || endReason == EndReasonCleared
	s.CompletedSuccessfully = completedSuccessfully
	s.EndReason = endReason
	var reward services.GameRewardSummary
	if s.IsSuddenDeath() {
		s.Score = s.CorrectCount*services.MCQCorrectAnswerPoints + s.StreakMilestones*services.SuddenDeathStreakPoints
		reward = services.CalculateSuddenDeathRewards(s.CorrectCount, s.TotalQuestions, s.StreakMilestones, completedSuccessfully)
	} else {
		s.Score = s.CorrectCount * services.MCQCorrectAnswerPoints
		reward = services.CalculateGameRewards(s.Score, s.CorrectCount, s.TotalQuestions)
	}

	profile, err := services.GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errPrepareRewards, err)
	}
	newLevel := services.CalculateNewLevel(profile.Experience + reward.XP)
	didLevelUp := newLevel > profile.Level

	var levelUpReward *LevelUpRewardPayload
	levelBonusCoins, levelBonusGems := 0, 0
	if didLevelUp {
		levelsGained := newLevel - profile.Level
		levelBonusCoins = levelsGained * 50
		levelBonusGems = levelsGained
		// XP stays 0: docs/points_calculation.md keeps the level-up event out of xp_awarded.
		levelUpReward = &LevelUpRewardPayload{Coins: levelBonusCoins, Gems: levelBonusGems}
	}

	if err := repo.FinalizeSession(ctx, s, reward.Coins+levelBonusCoins, reward.XP, reward.Gems+levelBonusGems); err != nil {
		return nil, err
	}
	if didLevelUp {
		if err := repo.UpdateProfileLevel(ctx, clientID, newLevel); err != nil {
			log.Printf("[ws] session %s: failed to persist level %d: %v", s.ID, newLevel, err)
		}
	}

	return &GameOverPayload{
		FinalScore:            s.Score,
		TotalQuestions:        s.TotalQuestions,
		CorrectCount:          s.CorrectCount,
		SkippedCount:          s.SkippedCount,
		BestStreak:            s.BestStreak,
		CoinsEarned:           reward.Coins,
		XPEarned:              reward.XP,
		GemsEarned:            reward.Gems,
		NewLevel:              newLevel,
		DidLevelUp:            didLevelUp,
		LevelUpReward:         levelUpReward,
		GameMode:              s.GameMode,
		EndReason:             endReason,
		EndedEarly:            !completedSuccessfully,
		CompletedSuccessfully: completedSuccessfully,
	}, nil
}
