package services

import (
	"context"
	"encoding/json"
	"os"
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

func TestSuddenDeathSeedQuestionsEnforceTwoOptionContract(t *testing.T) {
	raw, err := os.ReadFile("../data/seed_questions.json")
	if err != nil {
		t.Fatalf("read seed data: %v", err)
	}
	type seedQuestion struct {
		models.Question
		CorrectOption string `json:"correct_option"`
	}
	var all []seedQuestion
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatalf("decode seed data: %v", err)
	}
	suddenDeath := make([]models.Question, 0, 10)
	for _, q := range all {
		if q.QuestionType == models.GameModeSuddenDeath {
			q.Question.CorrectOption = q.CorrectOption
			suddenDeath = append(suddenDeath, q.Question)
		}
	}
	if len(suddenDeath) != 10 {
		t.Fatalf("expected 10 Sudden Death seed questions, got %d", len(suddenDeath))
	}
	if err := ValidateSuddenDeathQuestions(suddenDeath); err != nil {
		t.Fatalf("invalid Sudden Death seed contract: %v", err)
	}
	for _, q := range suddenDeath {
		sq := ShuffleQuestion(q)
		correct, _, _ := GradeAnswer(&sq, nil, sq.CorrectOption, "")
		if !correct {
			t.Fatalf("%s: shuffled correct slot %s was rejected", q.QuestionCode, sq.CorrectOption)
		}
		wrong := "a"
		if sq.CorrectOption == "a" {
			wrong = "b"
		}
		if accepted, _, _ := GradeAnswer(&sq, nil, wrong, sq.CorrectText); accepted {
			t.Fatalf("%s: wrong served slot %s bypassed grading", q.QuestionCode, wrong)
		}
	}
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
