package services

import (
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/game/models"
)

func TestCalculatePoints_EasyQuick(t *testing.T) {
	// Easy question, answered very fast (under 50% time), no combo
	points := CalculatePoints(10, 1, 3000, 1)
	// Base 10 * diff 1.0 * speed 1.5 * combo 1.0 = 15
	if points != 15 {
		t.Errorf("expected 15, got %d", points)
	}
}

func TestCalculatePoints_MediumMediumSpeed(t *testing.T) {
	// Medium question, 60% time used, combo streak 3
	points := CalculatePoints(15, 2, 9000, 3)
	// Base 15 * diff 1.5 * speed 1.25 * combo 1.2 = 33.75 → 34
	if points != 34 {
		t.Errorf("expected 34, got %d", points)
	}
}

func TestCalculatePoints_HardSlowHighCombo(t *testing.T) {
	// Hard question, slow answer (>75%), combo streak 7
	points := CalculatePoints(20, 3, 13000, 7)
	// Base 20 * diff 2.0 * speed 1.0 * combo 2.0 = 80
	if points != 80 {
		t.Errorf("expected 80, got %d", points)
	}
}

func TestCalculatePoints_ZeroTime(t *testing.T) {
	// Zero time (should not crash, no speed bonus)
	points := CalculatePoints(10, 1, 0, 1)
	if points != 10 {
		t.Errorf("expected 10, got %d", points)
	}
}

func TestCalculateCoins(t *testing.T) {
	tests := []struct {
		points   int
		expected int
	}{
		{10, 2},
		{15, 3},
		{0, 0},
		{1, 1},
		{100, 20},
	}
	for _, tt := range tests {
		got := CalculateCoins(tt.points)
		if got != tt.expected {
			t.Errorf("CalculateCoins(%d): expected %d, got %d", tt.points, tt.expected, got)
		}
	}
}

func TestCalculateGameRewards_PerfectGame(t *testing.T) {
	coins, xp, gems := CalculateGameRewards(100, 10, 10)
	// coins = ceil(100/5) + 5 = 25
	// xp = ceil(100/2) + 10 = 60
	// gems = 3 (perfect game)
	if coins != 25 {
		t.Errorf("expected coins 25, got %d", coins)
	}
	if xp != 60 {
		t.Errorf("expected xp 60, got %d", xp)
	}
	if gems != 3 {
		t.Errorf("expected gems 3, got %d", gems)
	}
}

func TestCalculateGameRewards_GoodAccuracy(t *testing.T) {
	coins, xp, gems := CalculateGameRewards(80, 8, 10)
	// gems = 1 (80% accuracy)
	if gems != 1 {
		t.Errorf("expected gems 1, got %d", gems)
	}
	if coins <= 0 {
		t.Errorf("expected positive coins, got %d", coins)
	}
	if xp <= 0 {
		t.Errorf("expected positive xp, got %d", xp)
	}
}

func TestCalculateGameRewards_PoorAccuracy(t *testing.T) {
	_, _, gems := CalculateGameRewards(30, 3, 10)
	// gems = 0 (30% accuracy)
	if gems != 0 {
		t.Errorf("expected gems 0, got %d", gems)
	}
}

func TestCalculateNewLevel(t *testing.T) {
	tests := []struct {
		totalXP  int
		expected int
	}{
		{0, 1},
		{50, 1},
		{100, 2},  // Level 1 requires 100 XP
		{250, 2},  // Level 2 requires 300 XP (100+300 = 400 needed for level 3)
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

func TestCalculatePoints_ComboTiers(t *testing.T) {
	base := 10
	difficulty := 1
	timeMs := 14000 // Slow (>75%), no speed bonus

	// No combo (streak 0)
	p0 := CalculatePoints(base, difficulty, timeMs, 0)
	// Combo 3 (1.2x)
	p3 := CalculatePoints(base, difficulty, timeMs, 3)
	// Combo 5 (1.5x)
	p5 := CalculatePoints(base, difficulty, timeMs, 5)
	// Combo 7 (2.0x)
	p7 := CalculatePoints(base, difficulty, timeMs, 7)

	if p3 <= p0 {
		t.Errorf("combo 3 (%d) should be greater than no combo (%d)", p3, p0)
	}
	if p5 <= p3 {
		t.Errorf("combo 5 (%d) should be greater than combo 3 (%d)", p5, p3)
	}
	if p7 <= p5 {
		t.Errorf("combo 7 (%d) should be greater than combo 5 (%d)", p7, p5)
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
		CorrectOption: "a", // Paris
		Points:        10,
		Difficulty:    1,
	}

	for i := 0; i < 50; i++ {
		shuffled := ShuffleQuestion(orig)
		if shuffled.QuestionCode != "42" {
			t.Fatalf("QuestionCode changed: %s", shuffled.QuestionCode)
		}

		// Ensure all original texts are present in shuffled options
		seen := map[string]bool{}
		for _, opt := range []string{shuffled.OptionA, shuffled.OptionB, shuffled.OptionC, shuffled.OptionD} {
			seen[opt] = true
		}
		for _, exp := range []string{"Paris", "Berlin", "Madrid", "Rome"} {
			if !seen[exp] {
				t.Fatalf("Missing option text: %s in %+v", exp, shuffled)
			}
		}

		// Ensure CorrectOption accurately points to "Paris"
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

	// Each option should be selected roughly 25% of the time (100 times out of 400)
	// Allow wide tolerance [40, 180] to avoid flaky tests
	for letter, count := range counts {
		if count < 40 || count > 180 {
			t.Errorf("Option %s frequency %d out of %d is outside expected distribution", letter, count, trials)
		}
	}
}

