package services

import (
	"context"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

func TestCalculatePoints_DeterministicForCorrectAnswers(t *testing.T) {
	tests := []struct {
		name        string
		basePoints  int
		difficulty  int
		timeTakenMs int
		comboStreak int
	}{
		{"easy quick", 10, 1, 3000, 1},
		{"medium medium speed", 15, 2, 9000, 3},
		{"hard slow high combo", 20, 3, 13000, 7},
		{"zero time", 10, 1, 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			points := CalculatePoints(tt.basePoints, tt.difficulty, tt.timeTakenMs, tt.comboStreak)
			if points != MCQCorrectAnswerPoints {
				t.Errorf("expected %d, got %d", MCQCorrectAnswerPoints, points)
			}
		})
	}
}

func TestCalculateMCQAnswerPoints(t *testing.T) {
	tests := []struct {
		name      string
		isCorrect bool
		expected  int
	}{
		{name: "correct", isCorrect: true, expected: 10},
		{name: "wrong", isCorrect: false, expected: 0},
		{name: "skipped", isCorrect: false, expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateMCQAnswerPoints(tt.isCorrect); got != tt.expected {
				t.Fatalf("expected %d points, got %d", tt.expected, got)
			}
		})
	}
}

func TestSuddenDeathScoringAndRewards(t *testing.T) {
	streaks := []struct {
		streak int
		want   int
	}{
		{streak: 1, want: 10},
		{streak: 2, want: 10},
		{streak: 3, want: 15},
		{streak: 6, want: 15},
	}
	for _, tc := range streaks {
		if got := CalculateSuddenDeathAnswerPoints(true, tc.streak); got != tc.want {
			t.Fatalf("streak %d: expected %d points, got %d", tc.streak, tc.want, got)
		}
	}
	if got := CalculateSuddenDeathAnswerPoints(false, 3); got != 0 {
		t.Fatalf("wrong answer must score 0, got %d", got)
	}
	if got := CalculateMaxScoreForMode(models.GameModeSuddenDeath, 10); got != 115 {
		t.Fatalf("expected Sudden Death max score 115, got %d", got)
	}
	if got := CalculateMaxScoreForMode(models.GameModeMCQ, 10); got != 100 {
		t.Fatalf("expected MCQ max score to remain 100, got %d", got)
	}

	reward := CalculateSuddenDeathRewards(10, 10, 3, true)
	if reward.XP != 90 || reward.Coins != 44 || reward.Gems != 3 {
		t.Fatalf("unexpected perfect Sudden Death rewards: %+v", reward)
	}
}

func TestCalculateCoins_NoPerQuestionCoins(t *testing.T) {
	tests := []int{0, 1, 10, 15, 100}
	for _, points := range tests {
		got := CalculateCoins(points)
		if got != 0 {
			t.Errorf("CalculateCoins(%d): expected 0, got %d", points, got)
		}
	}
}

func TestCalculateGameRewards_MCQScenarios(t *testing.T) {
	tests := []struct {
		name           string
		correctCount   int
		totalQuestions int
		expectedScore  int
		expectedXP     int
		expectedCoins  int
		expectedGems   int
		perfect        bool
	}{
		{name: "0 of 10", correctCount: 0, totalQuestions: 10, expectedScore: 0, expectedXP: 10, expectedCoins: 5},
		{name: "1 of 10", correctCount: 1, totalQuestions: 10, expectedScore: 10, expectedXP: 15, expectedCoins: 7},
		{name: "5 of 10", correctCount: 5, totalQuestions: 10, expectedScore: 50, expectedXP: 35, expectedCoins: 15},
		{name: "9 of 10", correctCount: 9, totalQuestions: 10, expectedScore: 90, expectedXP: 55, expectedCoins: 23, expectedGems: 1},
		{name: "10 of 10", correctCount: 10, totalQuestions: 10, expectedScore: 100, expectedXP: 75, expectedCoins: 35, expectedGems: 3, perfect: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reward := CalculateGameRewards(tt.expectedScore, tt.correctCount, tt.totalQuestions)
			if reward.XP != tt.expectedXP {
				t.Errorf("expected XP %d, got %d", tt.expectedXP, reward.XP)
			}
			if reward.Coins != tt.expectedCoins {
				t.Errorf("expected coins %d, got %d", tt.expectedCoins, reward.Coins)
			}
			if reward.Gems != tt.expectedGems {
				t.Errorf("expected existing gem reward %d, got %d", tt.expectedGems, reward.Gems)
			}

			expectedPerfectXP := 0
			expectedPerfectCoins := 0
			if tt.perfect {
				expectedPerfectXP = MCQPerfectBonusXP
				expectedPerfectCoins = MCQPerfectBonusCoins
			}
			if got := reward.XPBreakdown["perfect_bonus_xp"]; got != expectedPerfectXP {
				t.Errorf("expected perfect XP bonus %d, got %d", expectedPerfectXP, got)
			}
			if got := reward.CoinBreakdown["perfect_bonus_coins"]; got != expectedPerfectCoins {
				t.Errorf("expected perfect coin bonus %d, got %d", expectedPerfectCoins, got)
			}

			xpSum := sumBreakdown(reward.XPBreakdown)
			if xpSum != reward.XP {
				t.Errorf("XP breakdown sum %d does not match awarded XP %d", xpSum, reward.XP)
			}
			coinSum := sumBreakdown(reward.CoinBreakdown)
			if coinSum != reward.Coins {
				t.Errorf("coin breakdown sum %d does not match awarded coins %d", coinSum, reward.Coins)
			}

			scoreBreakdown := map[string]int{
				"correct_answer_points": tt.correctCount * MCQCorrectAnswerPoints,
			}
			if scoreSum := sumBreakdown(scoreBreakdown); scoreSum != tt.expectedScore {
				t.Errorf("score breakdown sum %d does not match final score %d", scoreSum, tt.expectedScore)
			}
			if maxScore := CalculateMaxScore(tt.totalQuestions); maxScore != 100 {
				t.Errorf("expected max score 100, got %d", maxScore)
			}
		})
	}
}

func TestCalculateMaxScore(t *testing.T) {
	tests := []struct {
		totalQuestions int
		expected       int
	}{
		{totalQuestions: 0, expected: 0},
		{totalQuestions: 5, expected: 50},
		{totalQuestions: 10, expected: 100},
		{totalQuestions: 20, expected: 200},
	}

	for _, tt := range tests {
		if got := CalculateMaxScore(tt.totalQuestions); got != tt.expected {
			t.Errorf("CalculateMaxScore(%d): expected %d, got %d", tt.totalQuestions, tt.expected, got)
		}
	}
}

func sumBreakdown(breakdown map[string]int) int {
	total := 0
	for _, value := range breakdown {
		total += value
	}
	return total
}

func TestCalculateNewLevel(t *testing.T) {
	tests := []struct {
		totalXP  int
		expected int
	}{
		{0, 1},
		{50, 1},
		{100, 2}, // Level 1 requires 100 XP
		{250, 2}, // Level 2 requires 300 XP (100+300 = 400 needed for level 3)
		{400, 3},
		{1000, 4},
	}
	for _, tt := range tests {
		got := CalculateNewLevel(tt.totalXP)
		if got != tt.expected {
			t.Errorf("CalculateNewLevel(%d): expected %d, got %d", tt.totalXP, tt.expected, got)
		}
	}
}

func TestShuffleQuestion_PreservesOptionsAndMapsCorrectAnswer(t *testing.T) {
	orig := models.Question{
		QuestionCode:  "42",
		Prompt:        "What is the capital of France?",
		OptionA:       "Paris",
		OptionB:       "Berlin",
		OptionC:       "Madrid",
		OptionD:       "Rome",
		CorrectOption: "a",
		Difficulty:    1,
	}

	for i := 0; i < 50; i++ {
		shuffled := ShuffleQuestion(orig)
		if shuffled.QuestionCode != "42" {
			t.Fatalf("QuestionCode changed: %s", shuffled.QuestionCode)
		}

		seen := map[string]bool{}
		for _, opt := range []string{shuffled.OptionA, shuffled.OptionB, shuffled.OptionC, shuffled.OptionD} {
			seen[opt] = true
		}
		for _, exp := range []string{"Paris", "Berlin", "Madrid", "Rome"} {
			if !seen[exp] {
				t.Fatalf("Missing option text: %s in %+v", exp, shuffled)
			}
		}

		var correctText string
		switch shuffled.CorrectOption {
		case "a":
			correctText = shuffled.OptionA
		case "b":
			correctText = shuffled.OptionB
		case "c":
			correctText = shuffled.OptionC
		case "d":
			correctText = shuffled.OptionD
		default:
			t.Fatalf("Invalid correct option letter: %s", shuffled.CorrectOption)
		}

		if correctText != "Paris" {
			t.Fatalf("CorrectOption '%s' points to '%s', expected 'Paris'", shuffled.CorrectOption, correctText)
		}
	}
}

func TestShuffleQuestion_UniformDistribution(t *testing.T) {
	orig := models.Question{
		QuestionCode:  "1",
		Prompt:        "Test prompt",
		OptionA:       "Correct Answer",
		OptionB:       "Wrong 1",
		OptionC:       "Wrong 2",
		OptionD:       "Wrong 3",
		CorrectOption: "a",
	}

	counts := map[string]int{"a": 0, "b": 0, "c": 0, "d": 0}
	trials := 400
	for i := 0; i < trials; i++ {
		sq := ShuffleQuestion(orig)
		counts[sq.CorrectOption]++
	}

	for letter, count := range counts {
		if count < 40 || count > 180 {
			t.Errorf("Option %s frequency %d out of %d is outside expected distribution", letter, count, trials)
		}
	}
}

func TestGradeAnswer_MultiLayeredEvaluation(t *testing.T) {
	orig := models.Question{
		QuestionCode:  "q101",
		Prompt:        "What is 2 + 2?",
		OptionA:       "4",
		OptionB:       "3",
		OptionC:       "2",
		OptionD:       "5",
		CorrectOption: "a", // "4"
		Difficulty:    1,
	}

	servedA := models.SessionQuestion{
		QuestionCode:    orig.QuestionCode,
		OptionA:         "4",
		OptionB:         "3",
		CorrectOption:   "a",
		OriginalCorrect: "a",
		CorrectText:     "4",
	}
	servedB := models.SessionQuestion{
		QuestionCode:    orig.QuestionCode,
		OptionA:         "3",
		OptionB:         "4",
		CorrectOption:   "b",
		OriginalCorrect: "a",
		CorrectText:     "4",
	}

	t.Run("unshuffled served A", func(t *testing.T) {
		correct, skipped, res := GradeAnswer(&servedA, nil, "a", "")
		if !correct || skipped || res != "a" {
			t.Fatalf("expected served A to be correct, got correct=%v skipped=%v res=%s", correct, skipped, res)
		}
		wrong, _, _ := GradeAnswer(&servedA, nil, "b", "")
		if wrong {
			t.Fatal("served B must be wrong when served A is authoritative")
		}
	})

	t.Run("shuffled served B rejects original A", func(t *testing.T) {
		correct, skipped, res := GradeAnswer(&servedB, nil, "b", "")
		if !correct || skipped || res != "b" {
			t.Fatalf("expected shuffled B to be correct, got correct=%v skipped=%v res=%s", correct, skipped, res)
		}
		wrong, _, _ := GradeAnswer(&servedB, nil, "a", "")
		if wrong {
			t.Fatal("original pre-shuffle A must not bypass served mapping")
		}
	})

	t.Run("contradictory selected text cannot bypass option", func(t *testing.T) {
		correct, skipped, _ := GradeAnswer(&servedB, nil, "a", "4")
		if correct || skipped {
			t.Fatalf("wrong served slot plus correct selected_text must be wrong, got correct=%v skipped=%v", correct, skipped)
		}
	})

	t.Run("full option text resolves through served mapping", func(t *testing.T) {
		correct, skipped, _ := GradeAnswer(&servedB, nil, "4", "")
		if !correct || skipped {
			t.Fatal("correct full option text should resolve to served slot B")
		}
	})

	t.Run("rejects incorrect text", func(t *testing.T) {
		correct, skipped, _ := GradeAnswer(&servedB, nil, "999", "4")
		if correct || skipped {
			t.Fatalf("expected incorrect answer to be false, got correct=%v", correct)
		}
	})

	t.Run("handles skip", func(t *testing.T) {
		correct, skipped, res := GradeAnswer(&servedB, nil, "skip", "")
		if correct || !skipped || res != servedB.CorrectOption {
			t.Fatalf("Expected correct=false, skipped=true, got correct=%v, skipped=%v", correct, skipped)
		}
	})

	t.Run("database fallback keeps option authoritative", func(t *testing.T) {
		// Evaluating directly against models.Question
		correctLetter, _, _ := GradeAnswer(nil, &orig, "a", "")
		if !correctLetter {
			t.Fatal("Expected DB question letter 'a' to evaluate as correct")
		}
		correctText, _, _ := GradeAnswer(nil, &orig, "4", "")
		if !correctText {
			t.Fatal("Expected DB question option text '4' to evaluate as correct")
		}
		bypassed, _, _ := GradeAnswer(nil, &orig, "b", "4")
		if bypassed {
			t.Fatal("selected_text must not override a wrong database option letter")
		}
	})
}

func TestCheckAndAbandonIfExpired(t *testing.T) {
	ctx := context.Background()

	t.Run("Returns false for nil session", func(t *testing.T) {
		if CheckAndAbandonIfExpired(ctx, nil) {
			t.Fatal("expected false for nil session")
		}
	})

	t.Run("Returns false for already finished session", func(t *testing.T) {
		session := &models.GameSession{
			Status:    models.SessionStatusFinished,
			UpdatedAt: time.Now().Add(-10 * time.Minute),
		}
		if CheckAndAbandonIfExpired(ctx, session) {
			t.Fatal("expected false for finished session")
		}
	})

	t.Run("Returns false for already abandoned session", func(t *testing.T) {
		session := &models.GameSession{
			Status:    models.SessionStatusAbandoned,
			UpdatedAt: time.Now().Add(-10 * time.Minute),
		}
		if CheckAndAbandonIfExpired(ctx, session) {
			t.Fatal("expected false for already abandoned session")
		}
	})

	t.Run("Returns false when session active within 5 minutes", func(t *testing.T) {
		session := &models.GameSession{
			Status:    models.SessionStatusInProgress,
			UpdatedAt: time.Now().Add(-2 * time.Minute),
		}
		if CheckAndAbandonIfExpired(ctx, session) {
			t.Fatal("expected false for recently active session (2 min ago)")
		}
		if session.Status != models.SessionStatusInProgress {
			t.Fatalf("expected status to remain in_progress, got %s", session.Status)
		}
	})

	t.Run("Returns true and abandons session when inactive > 5 minutes", func(t *testing.T) {
		session := &models.GameSession{
			Status:    models.SessionStatusInProgress,
			UpdatedAt: time.Now().Add(-5*time.Minute - 10*time.Second),
		}
		if !CheckAndAbandonIfExpired(ctx, session) {
			t.Fatal("expected true for session inactive > 5 minutes")
		}
		if session.Status != models.SessionStatusAbandoned {
			t.Fatalf("expected status to change to abandoned, got %s", session.Status)
		}
		if session.EndedAt == nil {
			t.Fatal("expected EndedAt to be populated")
		}
	})

	t.Run("Falls back to StartedAt when UpdatedAt is zero", func(t *testing.T) {
		session := &models.GameSession{
			Status:    models.SessionStatusInProgress,
			StartedAt: time.Now().Add(-6 * time.Minute),
		}
		if !CheckAndAbandonIfExpired(ctx, session) {
			t.Fatal("expected true when StartedAt > 5 min ago")
		}
		if session.Status != models.SessionStatusAbandoned {
			t.Fatalf("expected status to change to abandoned, got %s", session.Status)
		}
	})
}

func TestCalculateSortItOutPoints(t *testing.T) {
	tests := []struct {
		name        string
		isCorrect   bool
		timeTakenMs int
		comboStreak int
		wantBase    int
		wantSpeed   int
		wantCombo   int
		wantTotal   int
	}{
		{
			name:        "Incorrect answer yields 0 points across all components",
			isCorrect:   false,
			timeTakenMs: 1200,
			comboStreak: 3,
			wantBase:    0,
			wantSpeed:   0,
			wantCombo:   0,
			wantTotal:   0,
		},
		{
			name:        "Fast swipe (<2s) with 1x streak",
			isCorrect:   true,
			timeTakenMs: 1500,
			comboStreak: 1,
			wantBase:    10,
			wantSpeed:   5,
			wantCombo:   2,
			wantTotal:   17,
		},
		{
			name:        "Medium swipe (<4s) with 2x streak",
			isCorrect:   true,
			timeTakenMs: 2500,
			comboStreak: 2,
			wantBase:    10,
			wantSpeed:   2,
			wantCombo:   4,
			wantTotal:   16,
		},
		{
			name:        "Slow swipe (>=4s) with 3x streak",
			isCorrect:   true,
			timeTakenMs: 4200,
			comboStreak: 3,
			wantBase:    10,
			wantSpeed:   0,
			wantCombo:   6,
			wantTotal:   16,
		},
		{
			name:        "Fast swipe with max combo streak cap (+10 pts)",
			isCorrect:   true,
			timeTakenMs: 800,
			comboStreak: 7, // 7 * 2 = 14, capped at 10
			wantBase:    10,
			wantSpeed:   5,
			wantCombo:   10,
			wantTotal:   25,
		},
		{
			name:        "Boundary speed exactly 2000ms (medium speed +2)",
			isCorrect:   true,
			timeTakenMs: 2000,
			comboStreak: 1,
			wantBase:    10,
			wantSpeed:   2,
			wantCombo:   2,
			wantTotal:   14,
		},
		{
			name:        "Boundary speed exactly 4000ms (slow +0)",
			isCorrect:   true,
			timeTakenMs: 4000,
			comboStreak: 1,
			wantBase:    10,
			wantSpeed:   0,
			wantCombo:   2,
			wantTotal:   12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, speed, combo, total := CalculateSortItOutPoints(tt.isCorrect, tt.timeTakenMs, tt.comboStreak)
			if base != tt.wantBase {
				t.Errorf("base points: got %d, want %d", base, tt.wantBase)
			}
			if speed != tt.wantSpeed {
				t.Errorf("speed bonus: got %d, want %d", speed, tt.wantSpeed)
			}
			if combo != tt.wantCombo {
				t.Errorf("combo bonus: got %d, want %d", combo, tt.wantCombo)
			}
			if total != tt.wantTotal {
				t.Errorf("total points: got %d, want %d", total, tt.wantTotal)
			}
		})
	}
}

func TestCalculateSortItOutRewards(t *testing.T) {
	t.Run("Perfect 10 of 10 session", func(t *testing.T) {
		reward := CalculateSortItOutRewards(10, 10, 200)
		if reward.XP != 75 { // 10 completion + 50 correct + 15 perfect bonus
			t.Errorf("expected XP 75, got %d", reward.XP)
		}
		if reward.Coins != 35 { // 5 completion + 20 correct + 10 perfect bonus
			t.Errorf("expected coins 35, got %d", reward.Coins)
		}
		if reward.Gems != 3 {
			t.Errorf("expected gems 3, got %d", reward.Gems)
		}
		if reward.XPBreakdown["perfect_bonus_xp"] != 15 {
			t.Errorf("expected perfect XP bonus 15, got %d", reward.XPBreakdown["perfect_bonus_xp"])
		}
		if reward.CoinBreakdown["perfect_bonus_coins"] != 10 {
			t.Errorf("expected perfect coin bonus 10, got %d", reward.CoinBreakdown["perfect_bonus_coins"])
		}
	})

	t.Run("8 of 10 session (80% accuracy)", func(t *testing.T) {
		reward := CalculateSortItOutRewards(8, 10, 140)
		if reward.XP != 50 { // 10 completion + 40 correct
			t.Errorf("expected XP 50, got %d", reward.XP)
		}
		if reward.Coins != 21 { // 5 completion + 16 correct
			t.Errorf("expected coins 21, got %d", reward.Coins)
		}
		if reward.Gems != 1 {
			t.Errorf("expected gems 1, got %d", reward.Gems)
		}
	})

	t.Run("0 of 10 session", func(t *testing.T) {
		reward := CalculateSortItOutRewards(0, 10, 0)
		if reward.XP != 10 {
			t.Errorf("expected XP 10, got %d", reward.XP)
		}
		if reward.Coins != 5 {
			t.Errorf("expected coins 5, got %d", reward.Coins)
		}
		if reward.Gems != 0 {
			t.Errorf("expected gems 0, got %d", reward.Gems)
		}
	})
}

func TestSortItOutMaxScoreForMode(t *testing.T) {
	// 10 questions:
	// Base = 10 * 10 = 100
	// Speed = 10 * 5 = 50
	// Combos = 2 + 4 + 6 + 8 + 10*6 = 20 + 60 = 80
	// Total max score = 230
	maxScore := CalculateMaxScoreForMode(models.GameModeSortItOut, 10)
	if maxScore != 230 {
		t.Fatalf("expected max score for sort_it_out with 10 questions to be 230, got %d", maxScore)
	}
}

func TestSortItOutDTOValidations(t *testing.T) {
	t.Run("CreateSessionRequestDTO validation for sort_it_out", func(t *testing.T) {
		req := dto.CreateSessionRequestDTO{
			TopicID:  "accounting",
			GameMode: "sort_it_out",
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("unexpected error for valid sort_it_out create: %v", err)
		}
		if req.TimeLimitPerQuestionSec != 7 {
			t.Fatalf("expected default time limit of 7 seconds, got %d", req.TimeLimitPerQuestionSec)
		}

		reqInvalidTime := dto.CreateSessionRequestDTO{
			TopicID:                 "accounting",
			GameMode:                "sort_it_out",
			TimeLimitPerQuestionSec: 4,
		}
		if err := reqInvalidTime.Validate(); err == nil {
			t.Fatal("expected error for time_limit_per_question_sec < 5")
		}

		reqInvalidTimeHigh := dto.CreateSessionRequestDTO{
			TopicID:                 "accounting",
			GameMode:                "sort_it_out",
			TimeLimitPerQuestionSec: 11,
		}
		if err := reqInvalidTimeHigh.Validate(); err == nil {
			t.Fatal("expected error for time_limit_per_question_sec > 10")
		}

		reqValidCustomTime := dto.CreateSessionRequestDTO{
			TopicID:                 "accounting",
			GameMode:                "sort_it_out",
			TimeLimitPerQuestionSec: 9,
		}
		if err := reqValidCustomTime.Validate(); err != nil {
			t.Fatalf("unexpected error for time limit 9: %v", err)
		}
		if reqValidCustomTime.TimeLimitPerQuestionSec != 9 {
			t.Fatalf("expected time limit 9, got %d", reqValidCustomTime.TimeLimitPerQuestionSec)
		}
	})

	t.Run("CompleteSortItOutRequestDTO validation", func(t *testing.T) {
		reqEmptySession := dto.CompleteSortItOutRequestDTO{
			Session: "",
			Answers: []dto.SortItOutAnswerSubmissionDTO{
				{Question: "q1", Option: "a"},
			},
		}
		if err := reqEmptySession.Validate(); err == nil {
			t.Fatal("expected error for empty session")
		}

		reqEmptyAnswers := dto.CompleteSortItOutRequestDTO{
			Session: "sess-123",
			Answers: []dto.SortItOutAnswerSubmissionDTO{},
		}
		if err := reqEmptyAnswers.Validate(); err == nil {
			t.Fatal("expected error for empty answers")
		}

		reqMissingQuestion := dto.CompleteSortItOutRequestDTO{
			Session: "sess-123",
			Answers: []dto.SortItOutAnswerSubmissionDTO{
				{Question: "", Option: "a"},
			},
		}
		if err := reqMissingQuestion.Validate(); err == nil {
			t.Fatal("expected error for missing question")
		}

		reqTimeoutWithoutOption := dto.CompleteSortItOutRequestDTO{
			Session: "sess-123",
			Answers: []dto.SortItOutAnswerSubmissionDTO{
				{Question: "q1", Option: "", IsTimeout: true},
			},
		}
		if err := reqTimeoutWithoutOption.Validate(); err != nil {
			t.Fatalf("expected timeout without option to be valid, got: %v", err)
		}

		reqNegativeTime := dto.CompleteSortItOutRequestDTO{
			Session: "sess-123",
			Answers: []dto.SortItOutAnswerSubmissionDTO{
				{Question: "q1", Option: "a", TimeTakenMs: -100},
			},
		}
		if err := reqNegativeTime.Validate(); err == nil {
			t.Fatal("expected error for negative time taken")
		}
	})
}

func TestGameSessionIsSortItOut(t *testing.T) {
	s := &models.GameSession{GameMode: models.GameModeSortItOut}
	if !s.IsSortItOut() {
		t.Fatal("expected IsSortItOut() to return true")
	}
	s2 := &models.GameSession{GameMode: models.GameModeMCQ}
	if s2.IsSortItOut() {
		t.Fatal("expected IsSortItOut() to return false for MCQ")
	}
}

func TestCalculateMatchBoardPoints(t *testing.T) {
	tests := []struct {
		name                   string
		matchedCount           int
		totalPairs             int
		mistakes               int
		timeLimitSec           int
		timeTakenMs            int
		expectedBase           int
		expectedFlawlessBonus  int
		expectedSpeedBonus     int
		expectedMistakePenalty int
		expectedTotal          int
	}{
		{
			name:                   "Flawless board clear with 10s remaining",
			matchedCount:           4,
			totalPairs:             4,
			mistakes:               0,
			timeLimitSec:           25,
			timeTakenMs:            15000,
			expectedBase:           40,
			expectedFlawlessBonus:  10,
			expectedSpeedBonus:     10,
			expectedMistakePenalty: 0,
			expectedTotal:          60,
		},
		{
			name:                   "Board clear with 2 mistakes and 5s remaining",
			matchedCount:           4,
			totalPairs:             4,
			mistakes:               2,
			timeLimitSec:           25,
			timeTakenMs:            20000,
			expectedBase:           40,
			expectedFlawlessBonus:  0,
			expectedSpeedBonus:     5,
			expectedMistakePenalty: 4,
			expectedTotal:          41,
		},
		{
			name:                   "Partial timeout with 2 pairs matched and 1 mistake",
			matchedCount:           2,
			totalPairs:             4,
			mistakes:               1,
			timeLimitSec:           25,
			timeTakenMs:            25000,
			expectedBase:           20,
			expectedFlawlessBonus:  0,
			expectedSpeedBonus:     0,
			expectedMistakePenalty: 2,
			expectedTotal:          18,
		},
		{
			name:                   "Zero matched and multiple mistakes clamped to zero",
			matchedCount:           0,
			totalPairs:             4,
			mistakes:               5,
			timeLimitSec:           25,
			timeTakenMs:            25000,
			expectedBase:           0,
			expectedFlawlessBonus:  0,
			expectedSpeedBonus:     0,
			expectedMistakePenalty: 10,
			expectedTotal:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, flawless, speed, mistake, total := CalculateMatchBoardPoints(
				tt.matchedCount,
				tt.totalPairs,
				tt.mistakes,
				tt.timeLimitSec,
				tt.timeTakenMs,
			)
			if base != tt.expectedBase {
				t.Errorf("base points: expected %d, got %d", tt.expectedBase, base)
			}
			if flawless != tt.expectedFlawlessBonus {
				t.Errorf("flawless bonus: expected %d, got %d", tt.expectedFlawlessBonus, flawless)
			}
			if speed != tt.expectedSpeedBonus {
				t.Errorf("speed bonus: expected %d, got %d", tt.expectedSpeedBonus, speed)
			}
			if mistake != tt.expectedMistakePenalty {
				t.Errorf("mistake penalty: expected %d, got %d", tt.expectedMistakePenalty, mistake)
			}
			if total != tt.expectedTotal {
				t.Errorf("total board score: expected %d, got %d", tt.expectedTotal, total)
			}
		})
	}
}

func TestCalculateMatchTheFollowingRewards(t *testing.T) {
	t.Run("100% accuracy awards perfect bonuses and 3 gems", func(t *testing.T) {
		reward := CalculateMatchTheFollowingRewards(12, 12, 3, 3, 150)
		if reward.Coins != 39 {
			t.Errorf("expected 39 coins, got %d", reward.Coins)
		}
		if reward.XP != 85 {
			t.Errorf("expected 85 XP, got %d", reward.XP)
		}
		if reward.Gems != 3 {
			t.Errorf("expected 3 gems, got %d", reward.Gems)
		}
		if reward.CoinBreakdown["perfect_bonus_coins"] != 10 {
			t.Errorf("expected 10 perfect bonus coins, got %d", reward.CoinBreakdown["perfect_bonus_coins"])
		}
		if reward.XPBreakdown["perfect_bonus_xp"] != 15 {
			t.Errorf("expected 15 perfect bonus XP, got %d", reward.XPBreakdown["perfect_bonus_xp"])
		}
	})

	t.Run("83.3% accuracy awards 1 gem and no perfect bonus", func(t *testing.T) {
		reward := CalculateMatchTheFollowingRewards(10, 12, 2, 3, 100)
		if reward.Coins != 25 {
			t.Errorf("expected 25 coins, got %d", reward.Coins)
		}
		if reward.XP != 60 {
			t.Errorf("expected 60 XP, got %d", reward.XP)
		}
		if reward.Gems != 1 {
			t.Errorf("expected 1 gem, got %d", reward.Gems)
		}
		if reward.CoinBreakdown["perfect_bonus_coins"] != 0 {
			t.Errorf("expected 0 perfect bonus coins, got %d", reward.CoinBreakdown["perfect_bonus_coins"])
		}
	})

	t.Run("50% accuracy awards 0 gems", func(t *testing.T) {
		reward := CalculateMatchTheFollowingRewards(6, 12, 1, 3, 50)
		if reward.Gems != 0 {
			t.Errorf("expected 0 gems, got %d", reward.Gems)
		}
	})
}

func TestGameSessionIsMatchTheFollowing(t *testing.T) {
	s := &models.GameSession{GameMode: models.GameModeMatchTheFollowing}
	if !s.IsMatchTheFollowing() {
		t.Fatal("expected IsMatchTheFollowing() to return true")
	}
	s2 := &models.GameSession{GameMode: models.GameModeMCQ}
	if s2.IsMatchTheFollowing() {
		t.Fatal("expected IsMatchTheFollowing() to return false for MCQ")
	}
	var nilSession *models.GameSession
	if nilSession.IsMatchTheFollowing() {
		t.Fatal("expected nil session IsMatchTheFollowing() to return false")
	}
}
