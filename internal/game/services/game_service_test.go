package services

import (
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/game/models"
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
		{100, 2},
		{250, 2},
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
