package services

import (
	"context"
	"testing"
	"time"

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

func TestCalculateCoins_NoPerQuestionCoins(t *testing.T) {
	tests := []int{0, 1, 10, 15, 100}
	for _, points := range tests {
		got := CalculateCoins(points)
		if got != 0 {
			t.Errorf("CalculateCoins(%d): expected 0, got %d", points, got)
		}
	}
}

func TestCalculateGameRewards_PerfectGame(t *testing.T) {
	reward := CalculateGameRewards(50, 5, 5)
	if reward.Coins != 15 {
		t.Errorf("expected coins 15, got %d", reward.Coins)
	}
	if reward.XP != 55 {
		t.Errorf("expected xp 55, got %d", reward.XP)
	}
	if reward.Gems != 3 {
		t.Errorf("expected gems 3, got %d", reward.Gems)
	}
	if reward.XPBreakdown["completion_xp"] != 20 ||
		reward.XPBreakdown["correct_answer_xp"] != 25 ||
		reward.XPBreakdown["perfect_bonus_xp"] != 10 {
		t.Errorf("unexpected XP breakdown: %+v", reward.XPBreakdown)
	}
	if reward.CoinBreakdown["completion_coins"] != 10 ||
		reward.CoinBreakdown["accuracy_bonus_coins"] != 5 {
		t.Errorf("unexpected coin breakdown: %+v", reward.CoinBreakdown)
	}
}

func TestCalculateGameRewards_GoodAccuracy(t *testing.T) {
	reward := CalculateGameRewards(40, 4, 5)
	if reward.Gems != 1 {
		t.Errorf("expected gems 1, got %d", reward.Gems)
	}
	if reward.Coins != 15 {
		t.Errorf("expected coins 15, got %d", reward.Coins)
	}
	if reward.XP != 40 {
		t.Errorf("expected xp 40, got %d", reward.XP)
	}
}

func TestCalculateGameRewards_PoorAccuracy(t *testing.T) {
	reward := CalculateGameRewards(10, 1, 5)
	if reward.Gems != 0 {
		t.Errorf("expected gems 0, got %d", reward.Gems)
	}
	if reward.Coins != 10 {
		t.Errorf("expected completion-only coins 10, got %d", reward.Coins)
	}
	if reward.XP != 25 {
		t.Errorf("expected xp 25, got %d", reward.XP)
	}
}

func TestRewardBreakdownSumsMatchAwardedTotals(t *testing.T) {
	reward := CalculateGameRewards(40, 4, 5)

	xpSum := 0
	for _, value := range reward.XPBreakdown {
		xpSum += value
	}
	if xpSum != reward.XP {
		t.Errorf("expected XP breakdown sum %d to match awarded XP %d", xpSum, reward.XP)
	}

	coinSum := 0
	for _, value := range reward.CoinBreakdown {
		coinSum += value
	}
	if coinSum != reward.Coins {
		t.Errorf("expected coin breakdown sum %d to match awarded coins %d", coinSum, reward.Coins)
	}
}

func TestCalculateMaxScore(t *testing.T) {
	if got := CalculateMaxScore(5); got != 50 {
		t.Errorf("expected max score 50, got %d", got)
	}
	if got := CalculateMaxScore(0); got != 0 {
		t.Errorf("expected max score 0, got %d", got)
	}
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
		Points:        10,
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
		Points:        10,
		Difficulty:    1,
	}

	sq := ShuffleQuestion(orig)
	// sq.CorrectText is "4"
	// sq.OriginalCorrect is "a"
	// sq.CorrectOption is whichever slot "4" was shuffled into

	t.Run("Matches Shuffled Letter", func(t *testing.T) {
		correct, skipped, res := GradeAnswer(&sq, nil, sq.CorrectOption, "")
		if !correct || skipped || res != sq.CorrectOption {
			t.Fatalf("Expected correct=true, skipped=false, got correct=%v, skipped=%v, res=%s", correct, skipped, res)
		}
	})

	t.Run("Matches Original DB Letter", func(t *testing.T) {
		correct, skipped, _ := GradeAnswer(&sq, nil, "a", "")
		if !correct || skipped {
			t.Fatalf("Expected original letter 'a' to evaluate as correct, got %v", correct)
		}
	})

	t.Run("Matches Option Text in Option Field", func(t *testing.T) {
		correct, skipped, _ := GradeAnswer(&sq, nil, "4", "")
		if !correct || skipped {
			t.Fatalf("Expected option text '4' to evaluate as correct, got %v", correct)
		}
	})

	t.Run("Matches SelectedText when Option is Desynchronized", func(t *testing.T) {
		// Suppose client showed option 'x' as "4", but session shuffled "4" elsewhere
		wrongLetter := "b"
		if sq.CorrectOption == "b" {
			wrongLetter = "c"
		}
		correct, skipped, _ := GradeAnswer(&sq, nil, wrongLetter, "4")
		if !correct || skipped {
			t.Fatalf("Expected selected_text '4' to override mismatched option letter, got %v", correct)
		}
	})

	t.Run("Matches Slot Text in Session Question", func(t *testing.T) {
		// Selecting the letter that has "4"
		correct, _, _ := GradeAnswer(&sq, nil, sq.CorrectOption, "")
		if !correct {
			t.Fatal("Expected slot text match to evaluate as correct")
		}
	})

	t.Run("Rejects Incorrect Letter and Incorrect Text", func(t *testing.T) {
		wrongLetter := "b"
		if sq.CorrectOption == "b" {
			wrongLetter = "c"
		}
		correct, skipped, _ := GradeAnswer(&sq, nil, wrongLetter, "999")
		if correct || skipped {
			t.Fatalf("Expected incorrect answer to be false, got correct=%v", correct)
		}
	})

	t.Run("Handles Skip", func(t *testing.T) {
		correct, skipped, res := GradeAnswer(&sq, nil, "skip", "")
		if correct || !skipped || res != sq.CorrectOption {
			t.Fatalf("Expected correct=false, skipped=true, got correct=%v, skipped=%v", correct, skipped)
		}
	})

	t.Run("Fallback DB Question when Session State is Missing", func(t *testing.T) {
		// Evaluating directly against models.Question
		correctLetter, _, _ := GradeAnswer(nil, &orig, "a", "")
		if !correctLetter {
			t.Fatal("Expected DB question letter 'a' to evaluate as correct")
		}
		correctText, _, _ := GradeAnswer(nil, &orig, "b", "4")
		if !correctText {
			t.Fatal("Expected DB question with selected_text '4' to evaluate as correct")
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

