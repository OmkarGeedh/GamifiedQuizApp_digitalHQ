package services

import (
	"fmt"
	"strings"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

func TestDealMatchBoards(t *testing.T) {
	var pairs []models.MatchPair
	add := func(ch int, left, right string) {
		pairs = append(pairs, models.MatchPair{ID: fmt.Sprintf("p%d", len(pairs)), TopicID: "accounts", ChapterNo: ch, LeftText: left, RightText: right})
	}
	// Chapter 1: 6 pairs, two of which share a right text. Chapter 2: 1 pair (too few for a board).
	for i := 1; i <= 4; i++ {
		add(1, fmt.Sprintf("L%d", i), fmt.Sprintf("R%d", i))
	}
	add(1, "A", "Same")
	add(1, "B", "same")
	add(2, "Lonely", "Pair")

	for run := 0; run < 50; run++ {
		boards := DealMatchBoards(pairs, true)
		total := 0
		for _, b := range boards {
			if len(b) < 2 || len(b) > MatchPairsPerBoard {
				t.Fatalf("board size %d outside 2..%d", len(b), MatchPairsPerBoard)
			}
			rights := map[string]bool{}
			for _, p := range b {
				if p.ChapterNo != 1 {
					t.Fatalf("board mixes chapters or kept a 1-pair chapter: %+v", b)
				}
				k := strings.ToLower(p.RightText)
				if rights[k] {
					t.Fatalf("board repeats right text %q: %+v", p.RightText, b)
				}
				rights[k] = true
			}
			total += len(b)
		}
		if len(boards) != 2 || total != 6 {
			t.Fatalf("expected chapter 1's 6 pairs dealt as 2 boards, got %d boards / %d pairs", len(boards), total)
		}
	}
}
