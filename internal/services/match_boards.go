package services

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

// MatchPairsPerBoard is how many pairs a Match the Following board holds.
const MatchPairsPerBoard = 4

// DealMatchBoards groups pairs by subject/chapter and deals each chapter's pairs evenly
// into boards of up to MatchPairsPerBoard. A board never repeats a left or right text,
// so every board has exactly one solution. Boards with fewer than 2 pairs are dropped.
// With shuffle, pairs within a chapter and the final board order are randomized.
func DealMatchBoards(pairs []models.MatchPair, shuffle bool) [][]models.MatchPair {
	type chapterKey struct {
		topic string
		no    int
	}
	var order []chapterKey
	byChapter := map[chapterKey][]models.MatchPair{}
	for _, p := range pairs {
		k := chapterKey{p.TopicID, p.ChapterNo}
		if _, ok := byChapter[k]; !ok {
			order = append(order, k)
		}
		byChapter[k] = append(byChapter[k], p)
	}

	var boards [][]models.MatchPair
	for _, k := range order {
		group := byChapter[k]
		if shuffle {
			rand.Shuffle(len(group), func(i, j int) { group[i], group[j] = group[j], group[i] })
		}
		// Spread the chapter evenly over ceil(n/size) boards: each pair goes to the
		// emptiest board that has room and no clashing text.
		open := make([][]models.MatchPair, (len(group)+MatchPairsPerBoard-1)/MatchPairsPerBoard)
		for _, p := range group {
			best := -1
			for i, b := range open {
				if len(b) < MatchPairsPerBoard && !boardHasText(b, p) && (best < 0 || len(b) < len(open[best])) {
					best = i
				}
			}
			if best < 0 {
				open = append(open, nil)
				best = len(open) - 1
			}
			open[best] = append(open[best], p)
		}
		for _, b := range open {
			if len(b) >= 2 {
				boards = append(boards, b)
			}
		}
	}
	if shuffle {
		rand.Shuffle(len(boards), func(i, j int) { boards[i], boards[j] = boards[j], boards[i] })
	}
	return boards
}

func boardHasText(board []models.MatchPair, p models.MatchPair) bool {
	for _, q := range board {
		if strings.EqualFold(q.LeftText, p.LeftText) || strings.EqualFold(q.RightText, p.RightText) {
			return true
		}
	}
	return false
}

// matchBoardTitles returns a function naming a board after its subject and chapter,
// e.g. "Accounts · Ledger".
func matchBoardTitles(ctx context.Context) func(topic string, chapterNo int) string {
	names := map[string]string{}
	if rows, err := repo.GetContentCounts(ctx); err == nil {
		for _, r := range rows {
			names[fmt.Sprintf("%s/%d", r.TopicID, r.ChapterNo)] = r.SubjectName + " · " + r.ChapterTitle
		}
	}
	return func(topic string, chapterNo int) string {
		if name, ok := names[fmt.Sprintf("%s/%d", topic, chapterNo)]; ok {
			return name
		}
		return "Match the pairs"
	}
}
