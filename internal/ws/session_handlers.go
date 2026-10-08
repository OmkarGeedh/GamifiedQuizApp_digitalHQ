package ws

import (
	"context"
	"encoding/json"
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
	if isActivePlayer {
		gs.Player = nil
	}
	gs.mu.Unlock()

	if !isActivePlayer {
		return
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
				Question:        q.QuestionCode,
				PowerUp:         powerUp,
				AddedTimeMs:     int(addTimeDuration.Milliseconds()),
				RemainingTimeMs: int(remaining.Milliseconds()),
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
				Question:      q.QuestionCode,
				PowerUp:       powerUp,
				HiddenOptions: hidden,
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

	var isCorrect, isSkipped bool
	var pointsEarned int
	var resolvedCorrect string

	if strings.EqualFold(cleanOption, "skip") {
		isCorrect = false
		isSkipped = true
		pointsEarned = 0
		resolvedCorrect = q.CorrectOption
		gs.Session.ComboStreak = 0
		if gs.Session.IsSuddenDeath() {
			gs.Session.SkippedCount++
		}
	} else {
		isCorrect, _, resolvedCorrect = services.GradeAnswer(&q, nil, cleanOption, data.SelectedText)
		isSkipped = false
		if isCorrect {
			gs.Session.ComboStreak++
			if gs.Session.ComboStreak > gs.Session.BestStreak {
				gs.Session.BestStreak = gs.Session.ComboStreak
			}
			gs.Session.CorrectCount++
			if gs.Session.IsSuddenDeath() {
				pointsEarned = services.CalculateSuddenDeathAnswerPoints(true, gs.Session.ComboStreak)
				if gs.Session.ComboStreak%services.SuddenDeathStreakSize == 0 {
					gs.Session.StreakMilestones++
				}
			} else {
				pointsEarned = services.CalculateMCQAnswerPoints(true)
			}
		} else {
			gs.Session.ComboStreak = 0
		}
	}
	gs.Session.Score += pointsEarned
	currentScore := gs.Session.Score

	// Clamp time taken for audit recording
	timeTaken := data.TimeTakenMs
	if timeTaken < 0 {
		timeTaken = 0
	} else if timeTaken > 17000 {
		timeTaken = 17000
	}

	// Persist history before acknowledging the authoritative answer. A failed
	// write stops this in-memory run so rewards and progression cannot diverge
	// from the durable audit trail.
	historyRecord := &models.UserQuestionHistory{
		ClientID:       gs.ClientID,
		SessionID:      gs.Session.ID,
		QuestionCode:   q.QuestionCode,
		SelectedOption: cleanOption,
		IsCorrect:      isCorrect,
		TimeTakenMs:    timeTaken,
		PointsAwarded:  pointsEarned,
		CoinsAwarded:   0,
	}
	if err := repo.RecordAnswer(ctx, historyRecord); err != nil {
		log.Printf("[ws] session %s: failed to persist answer for %s: %v", gs.ID, historyRecord.QuestionCode, err)
		gs.stopAfterPersistenceFailure("failed to persist answer")
		return
	}

	// Send answer result frame
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

	// Persist timeout before reporting/finalizing it for the same consistency
	// guarantee as a submitted answer.
	historyRecord := &models.UserQuestionHistory{
		ClientID:       gs.ClientID,
		SessionID:      gs.Session.ID,
		QuestionCode:   q.QuestionCode,
		SelectedOption: "timeout",
		IsCorrect:      false,
		TimeTakenMs:    questionTimeLimitSec * 1000,
		PointsAwarded:  0,
		CoinsAwarded:   0,
	}
	if err := repo.RecordAnswer(ctx, historyRecord); err != nil {
		log.Printf("[ws] session %s: failed to persist timeout for %s: %v", gs.ID, historyRecord.QuestionCode, err)
		gs.stopAfterPersistenceFailure("failed to persist timeout")
		return
	}

	// Send timeout result frame
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

	completedSuccessfully := endReason == EndReasonCompleted || endReason == EndReasonCleared
	gs.Session.CompletedSuccessfully = completedSuccessfully
	gs.Session.EndReason = endReason
	if gs.Session.IsSuddenDeath() {
		gs.Session.Score = gs.Session.CorrectCount*services.MCQCorrectAnswerPoints + gs.Session.StreakMilestones*services.SuddenDeathStreakPoints
	} else {
		gs.Session.Score = gs.Session.CorrectCount * services.MCQCorrectAnswerPoints
	}
	reward := services.CalculateGameRewards(gs.Session.Score, gs.Session.CorrectCount, gs.Session.TotalQuestions)
	if gs.Session.IsSuddenDeath() {
		reward = services.CalculateSuddenDeathRewards(gs.Session.CorrectCount, gs.Session.TotalQuestions, gs.Session.StreakMilestones, completedSuccessfully)
	}

	// Fetch profile for level info
	profile, err := services.GetOrCreateProfile(ctx, gs.ClientID)
	newLevel := 1
	didLevelUp := false
	if err != nil {
		log.Printf("[ws] session %s: failed to load profile for reward calculation: %v", gs.ID, err)
		gs.stopAfterPersistenceFailure("failed to prepare game rewards")
		return
	}
	oldLevel := profile.Level
	newLevel = services.CalculateNewLevel(profile.Experience + reward.XP)
	didLevelUp = newLevel > oldLevel

	var levelUpReward *LevelUpRewardPayload
	levelBonusCoins := 0
	levelBonusGems := 0
	if didLevelUp && profile != nil {
		levelsGained := newLevel - profile.Level
		levelBonusCoins = levelsGained * 50
		levelBonusGems = levelsGained
		levelUpReward = &LevelUpRewardPayload{
			Coins: levelBonusCoins,
			// XP is deliberately left at 0. docs/points_calculation.md keeps the
			// level-up event out of xp_awarded, and the REST completion endpoint
			// (services.CompleteSession) also leaves it unset. Assigning reward.XP
			// here would double-count it against XPEarned in this same frame.
			Gems: levelBonusGems,
		}
	}

	// Atomic finalization in PostgreSQL
	if err := repo.FinalizeSession(
		ctx,
		gs.Session,
		reward.Coins+levelBonusCoins,
		reward.XP,
		reward.Gems+levelBonusGems,
	); err != nil {
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

	if didLevelUp && profile != nil {
		if err := repo.UpdateProfileLevel(ctx, gs.ClientID, newLevel); err != nil {
			log.Printf("[ws] session %s: failed to persist level %d: %v", gs.ID, newLevel, err)
		}
	}

	// Send final game_over summary frame
	gs.sendToPlayer(OutboundMessage{
		Type: MsgTypeGameOver,
		Data: GameOverPayload{
			FinalScore:            gs.Session.Score,
			TotalQuestions:        gs.Session.TotalQuestions,
			CorrectCount:          gs.Session.CorrectCount,
			SkippedCount:          gs.Session.SkippedCount,
			BestStreak:            gs.Session.BestStreak,
			CoinsEarned:           reward.Coins,
			XPEarned:              reward.XP,
			GemsEarned:            reward.Gems,
			NewLevel:              newLevel,
			DidLevelUp:            didLevelUp,
			LevelUpReward:         levelUpReward,
			GameMode:              gs.Session.GameMode,
			EndReason:             endReason,
			EndedEarly:            !completedSuccessfully,
			CompletedSuccessfully: completedSuccessfully,
		},
	})

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

	payload := OutboundMessage{
		Type: MsgTypeQuestion,
		Data: QuestionPayload{
			Question:        q.QuestionCode,
			Prompt:          q.Prompt,
			Points:          q.Points,
			Hint:            q.Hint,
			Options:         opts,
			TimeLimitMs:     questionTimeLimitSec * 1000,
			RemainingTimeMs: remainingMs,
			QuestionNumber:  idx + 1,
			TotalQuestions:  total,
		},
	}
	gs.sendToPlayer(payload)
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
