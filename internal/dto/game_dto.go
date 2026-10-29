package dto

import (
	"errors"
	"strings"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

// --- Request DTOs ---

// CreateSessionRequestDTO validates input for starting a new quiz session.
type CreateSessionRequestDTO struct {
	TopicID string `json:"topic" binding:"required"`
	// ChapterNo limits questions to one chapter of the subject; 0 means the whole subject.
	ChapterNo     int      `json:"chapter_no,omitempty"`
	QuestionCount int      `json:"question_count"`
	AbandonStale  bool     `json:"abandon_stale,omitempty"`
	QuestionCodes []string `json:"question_codes,omitempty"`
	// GameMode selects the rules the session runs under. Omitted means "mcq",
	// which preserves the original answer-every-question flow.
	GameMode                string `json:"game_mode,omitempty"`
	PairType                string `json:"pair_type,omitempty"`
	TimeLimitPerQuestionSec int    `json:"time_limit_per_question_sec,omitempty"`
}

// Validate ensures the request has sane defaults and limits.
func (r *CreateSessionRequestDTO) Validate() error {
	r.TopicID = strings.TrimSpace(r.TopicID)
	if r.TopicID == "" {
		return errors.New("topic is required")
	}
	if r.ChapterNo < 0 || r.ChapterNo > 99 {
		return errors.New("chapter_no must be between 1 and 99")
	}

	// Normalize and validate the mode. An empty value keeps the historical
	// default rather than erroring, so older clients that never send the field
	// keep working unchanged.
	r.GameMode = strings.ToLower(strings.TrimSpace(r.GameMode))
	switch r.GameMode {
	case "":
		r.GameMode = models.GameModeMCQ
	case models.GameModeMCQ, models.GameModeSuddenDeath, models.GameModeSortItOut, models.GameModeMatchTheFollowing:
	default:
		return errors.New("game_mode must be 'mcq', 'sudden_death', 'sort_it_out', or 'match_the_following'")
	}

	if len(r.QuestionCodes) > 0 {
		r.QuestionCount = len(r.QuestionCodes)
	} else if r.QuestionCount <= 0 {
		r.QuestionCount = 10
	}
	if r.QuestionCount > 25 {
		return errors.New("question_count must not exceed 25")
	}

	r.PairType = strings.TrimSpace(r.PairType)
	if r.GameMode == models.GameModeSortItOut {
		if r.TimeLimitPerQuestionSec == 0 {
			r.TimeLimitPerQuestionSec = 7
		} else if r.TimeLimitPerQuestionSec < 5 || r.TimeLimitPerQuestionSec > 10 {
			return errors.New("time_limit_per_question_sec for sort_it_out must be between 5 and 10 seconds")
		}
	} else if r.GameMode == models.GameModeMatchTheFollowing {
		if r.QuestionCount < 1 || r.QuestionCount > 25 {
			return errors.New("question_count for match_the_following must be between 1 and 25 boards")
		}
		if r.TimeLimitPerQuestionSec == 0 {
			r.TimeLimitPerQuestionSec = 25
		} else if r.TimeLimitPerQuestionSec < 15 || r.TimeLimitPerQuestionSec > 45 {
			return errors.New("time_limit_per_question_sec for match_the_following must be between 15 and 45 seconds")
		}
	}
	return nil
}

// SubmitAnswerRequestDTO validates a player's answer submission.
type SubmitAnswerRequestDTO struct {
	Session      string `json:"session" binding:"required"`
	Question     string `json:"question" binding:"required"`
	Option       string `json:"option" binding:"required"`
	SelectedText string `json:"selected_text,omitempty"`
	TimeTakenMs  int    `json:"time_taken_ms"`
}

// Validate ensures the answer fields are correctly formatted.
func (r *SubmitAnswerRequestDTO) Validate() error {
	r.Session = strings.TrimSpace(r.Session)
	if r.Session == "" {
		return errors.New("session is required")
	}
	r.Question = strings.TrimSpace(r.Question)
	if r.Question == "" {
		return errors.New("question is required")
	}
	r.Option = strings.TrimSpace(r.Option)
	if r.Option == "" {
		return errors.New("option is required")
	}
	r.SelectedText = strings.TrimSpace(r.SelectedText)

	lowerOption := strings.ToLower(r.Option)
	validOptions := map[string]bool{"a": true, "b": true, "c": true, "d": true, "skip": true}
	if validOptions[lowerOption] {
		r.Option = lowerOption
	} else if len(r.Option) > 1 {
		// Player or client sent the full answer text in the option field
		if r.SelectedText == "" {
			r.SelectedText = r.Option
		}
	} else {
		return errors.New("option must be 'a', 'b', 'c', 'd', 'skip', or the option text")
	}

	if r.TimeTakenMs < 0 {
		return errors.New("time_taken_ms cannot be negative")
	}
	// Server-enforced maximum: 15s + 2s grace
	if r.TimeTakenMs > 17000 {
		r.TimeTakenMs = 17000
	}
	return nil
}

// FiftyFiftyRequestDTO validates a 50:50 power-up request.
type FiftyFiftyRequestDTO struct {
	Session  string `json:"session" binding:"required"`
	Question string `json:"question" binding:"required"`
}

// Validate ensures the fields are non-empty.
func (r *FiftyFiftyRequestDTO) Validate() error {
	r.Session = strings.TrimSpace(r.Session)
	if r.Session == "" {
		return errors.New("session is required")
	}
	r.Question = strings.TrimSpace(r.Question)
	if r.Question == "" {
		return errors.New("question is required")
	}
	return nil
}

// CompleteSessionRequestDTO validates a session completion request.
type CompleteSessionRequestDTO struct {
	Session string `json:"session" binding:"required"`
}

// Validate ensures the session field is non-empty.
func (r *CompleteSessionRequestDTO) Validate() error {
	r.Session = strings.TrimSpace(r.Session)
	if r.Session == "" {
		return errors.New("session is required")
	}
	return nil
}

// --- Response DTOs ---

// OptionDTO is a single option within a question payload.
type OptionDTO struct {
	Option string `json:"option"`
	Text   string `json:"text"`
}

// QuestionDTO is a question payload safe for client delivery.
// CorrectOption is included only when the client needs it for power-ups or sort-it-out.
type QuestionDTO struct {
	Question      string      `json:"question"`
	Prompt        string      `json:"prompt"`
	Points        int         `json:"points"`
	Hint          *string     `json:"hint,omitempty"`
	TimeLimitSec  int         `json:"time_limit_sec,omitempty"`
	Options       []OptionDTO `json:"options"`
	CorrectOption string      `json:"correct_option,omitempty"`
	PairType      string      `json:"pair_type,omitempty"`
	Explanation   *string     `json:"explanation,omitempty"`
}

// TopicQuestionsResponseDTO wraps the list of questions for a topic.
type TopicQuestionsResponseDTO struct {
	Topic                   string        `json:"topic"`
	Total                   int           `json:"total"`
	TimeLimitSec            int           `json:"time_limit_sec"`
	TimeLimitPerQuestionSec int           `json:"time_limit_per_question_sec,omitempty"`
	Questions               []QuestionDTO `json:"questions"`
}

// SessionCreatedResponseDTO is returned when a new quiz session is created.
type SessionCreatedResponseDTO struct {
	Session                 string          `json:"session"`
	Topic                   string          `json:"topic"`
	GameMode                string          `json:"game_mode"`
	PairType                string          `json:"pair_type,omitempty"`
	TotalQuestions          int             `json:"total_questions"`
	TimeLimitSec            int             `json:"time_limit_sec"`
	TimeLimitPerQuestionSec int             `json:"time_limit_per_question_sec,omitempty"`
	Questions               []QuestionDTO   `json:"questions,omitempty"`
	Boards                  []MatchBoardDTO `json:"boards,omitempty"`
}

// AnswerResultDTO is the evaluation result for a submitted answer.
type AnswerResultDTO struct {
	Question      string  `json:"question"`
	Option        string  `json:"option"`
	CorrectOption string  `json:"correct_option"`
	IsCorrect     bool    `json:"is_correct"`
	IsSkipped     bool    `json:"is_skipped"`
	Explanation   *string `json:"explanation,omitempty"`
	PointsEarned  int     `json:"points_earned"`
	CoinsEarned   int     `json:"coins_earned"`
	ComboStreak   int     `json:"combo_streak"`
	TotalScore    int     `json:"total_score"`
}

// FiftyFiftyResponseDTO lists the two options to hide.
type FiftyFiftyResponseDTO struct {
	Question      string   `json:"question"`
	HiddenOptions []string `json:"hidden_options"`
}

// LevelInfoDTO contains level/XP progression data.
type LevelInfoDTO struct {
	Current      int  `json:"current"`
	DidLevelUp   bool `json:"did_level_up"`
	Experience   int  `json:"experience"`
	NextLevelExp int  `json:"next_level_exp"`
}

// StreakInfoDTO contains streak summary data.
type StreakInfoDTO struct {
	CurrentStreak  int  `json:"current_streak"`
	TodayCompleted bool `json:"today_completed"`
}

// LevelUpRewardDTO keeps the existing level-up economy event separate from
// the base MCQ rewards and their breakdown totals.
type LevelUpRewardDTO struct {
	Coins int `json:"coins"`
	XP    int `json:"xp"`
	Gems  int `json:"gems"`
}

// SessionCompleteResponseDTO is the final game summary after quiz completion.
type SessionCompleteResponseDTO struct {
	Session            string            `json:"session"`
	TotalQuestions     int               `json:"total_questions"`
	CorrectCount       int               `json:"correct_count"`
	AccuracyPercentage float64           `json:"accuracy_percentage"`
	FinalScore         int               `json:"final_score"`
	MaxScore           int               `json:"max_score,omitempty"`
	CoinsAwarded       int               `json:"coins_awarded"`
	XPAwarded          int               `json:"xp_awarded"`
	GemsAwarded        int               `json:"gems_awarded"`
	ScoreBreakdown     map[string]int    `json:"score_breakdown,omitempty"`
	CoinBreakdown      map[string]int    `json:"coin_breakdown,omitempty"`
	XPBreakdown        map[string]int    `json:"xp_breakdown,omitempty"`
	LevelUpReward      *LevelUpRewardDTO `json:"level_up_reward,omitempty"`
	Level              LevelInfoDTO      `json:"level"`
	Streak             StreakInfoDTO     `json:"streak"`
}

// --- Sort It Out Mini-Game DTOs ---

// SortItOutAnswerSubmissionDTO represents a single answered card in a batch submission.
type SortItOutAnswerSubmissionDTO struct {
	Question    string `json:"question" binding:"required"`
	Option      string `json:"option"` // "a", "b", or "skip"/"timeout"
	TimeTakenMs int    `json:"time_taken_ms"`
	IsTimeout   bool   `json:"is_timeout,omitempty"`
}

// CompleteSortItOutRequestDTO is the batch payload sent at the end of a Sort It Out run.
type CompleteSortItOutRequestDTO struct {
	Session string                         `json:"session" binding:"required"`
	Answers []SortItOutAnswerSubmissionDTO `json:"answers" binding:"required"`
}

// Validate validates the batch submission payload.
func (r *CompleteSortItOutRequestDTO) Validate() error {
	r.Session = strings.TrimSpace(r.Session)
	if r.Session == "" {
		return errors.New("session is required")
	}
	if len(r.Answers) == 0 {
		return errors.New("answers cannot be empty")
	}
	for i := range r.Answers {
		r.Answers[i].Question = strings.TrimSpace(r.Answers[i].Question)
		if r.Answers[i].Question == "" {
			return errors.New("question is required for all answers")
		}
		r.Answers[i].Option = strings.ToLower(strings.TrimSpace(r.Answers[i].Option))
		if r.Answers[i].Option == "" && !r.Answers[i].IsTimeout {
			return errors.New("option is required")
		}
		if r.Answers[i].TimeTakenMs < 0 {
			return errors.New("time_taken_ms cannot be negative")
		}
	}
	return nil
}

// SortItOutCardReviewDTO provides audit breakdown for a single card.
type SortItOutCardReviewDTO struct {
	Question       string  `json:"question"`
	Prompt         string  `json:"prompt"`
	SelectedOption string  `json:"selected_option"`
	CorrectOption  string  `json:"correct_option"`
	IsCorrect      bool    `json:"is_correct"`
	IsTimeout      bool    `json:"is_timeout"`
	TimeTakenMs    int     `json:"time_taken_ms"`
	BasePoints     int     `json:"base_points"`
	SpeedBonus     int     `json:"speed_bonus"`
	ComboBonus     int     `json:"combo_bonus"`
	PointsEarned   int     `json:"points_earned"`
	Explanation    *string `json:"explanation,omitempty"`
}

// CompleteSortItOutResponseDTO is the rich end-of-game summary for Sort It Out.
type CompleteSortItOutResponseDTO struct {
	Session            string                   `json:"session"`
	PairType           string                   `json:"pair_type"`
	TotalQuestions     int                      `json:"total_questions"`
	CorrectCount       int                      `json:"correct_count"`
	TimeoutCount       int                      `json:"timeout_count"`
	BestStreak         int                      `json:"best_streak"`
	AccuracyPercentage float64                  `json:"accuracy_percentage"`
	FinalScore         int                      `json:"final_score"`
	CoinsAwarded       int                      `json:"coins_awarded"`
	XPAwarded          int                      `json:"xp_awarded"`
	GemsAwarded        int                      `json:"gems_awarded"`
	ScoreBreakdown     map[string]int           `json:"score_breakdown"`
	CoinBreakdown      map[string]int           `json:"coin_breakdown"`
	XPBreakdown        map[string]int           `json:"xp_breakdown"`
	LevelUpReward      *LevelUpRewardDTO        `json:"level_up_reward,omitempty"`
	Level              LevelInfoDTO             `json:"level"`
	Streak             StreakInfoDTO            `json:"streak"`
	CardsReview        []SortItOutCardReviewDTO `json:"cards_review"`
}

// --- Match the Following Mini-Game DTOs ---

// MatchPairItemDTO represents a single left or right item on a match board.
type MatchPairItemDTO struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// MatchBoardDTO represents a single board in a Match the Following puzzle run.
type MatchBoardDTO struct {
	QuestionCode string             `json:"question_code"`
	Prompt       string             `json:"prompt"`
	TimeLimitSec int                `json:"time_limit_sec"`
	LeftItems    []MatchPairItemDTO `json:"left_items"`
	RightItems   []MatchPairItemDTO `json:"right_items"`            // Shuffled order
	SolutionMap  map[string]string  `json:"solution_map,omitempty"` // left_id -> right_id
	Hint         *string            `json:"hint,omitempty"`
	Explanation  *string            `json:"explanation,omitempty"`
}

// MatchBoardAnswerSubmissionDTO represents the player's answers for a single board.
type MatchBoardAnswerSubmissionDTO struct {
	QuestionCode string            `json:"question_code" binding:"required"`
	Matches      map[string]string `json:"matches"` // player's left_id -> right_id selections
	Mistakes     int               `json:"mistakes"`
	TimeTakenMs  int               `json:"time_taken_ms"`
	IsTimeout    bool              `json:"is_timeout,omitempty"`
}

// CompleteMatchTheFollowingRequestDTO is the batch payload sent at the end of a Match the Following run.
type CompleteMatchTheFollowingRequestDTO struct {
	Session string                          `json:"session" binding:"required"`
	Boards  []MatchBoardAnswerSubmissionDTO `json:"boards" binding:"required"`
}

// Validate validates the batch submission payload for Match the Following.
func (r *CompleteMatchTheFollowingRequestDTO) Validate() error {
	r.Session = strings.TrimSpace(r.Session)
	if r.Session == "" {
		return errors.New("session is required")
	}
	if len(r.Boards) == 0 {
		return errors.New("boards cannot be empty")
	}
	for i := range r.Boards {
		r.Boards[i].QuestionCode = strings.TrimSpace(r.Boards[i].QuestionCode)
		if r.Boards[i].QuestionCode == "" {
			return errors.New("question_code is required for all boards")
		}
		if r.Boards[i].Mistakes < 0 {
			return errors.New("mistakes cannot be negative")
		}
		if r.Boards[i].TimeTakenMs < 0 {
			return errors.New("time_taken_ms cannot be negative")
		}
	}
	return nil
}

// MatchBoardReviewDTO provides score and audit breakdown for a single board.
type MatchBoardReviewDTO struct {
	QuestionCode    string  `json:"question_code"`
	Prompt          string  `json:"prompt"`
	PairsMatched    int     `json:"pairs_matched"`
	TotalPairs      int     `json:"total_pairs"`
	Mistakes        int     `json:"mistakes"`
	TimeTakenMs     int     `json:"time_taken_ms"`
	IsFlawless      bool    `json:"is_flawless"`
	IsTimeout       bool    `json:"is_timeout"`
	BasePoints      int     `json:"base_points"`
	FlawlessBonus   int     `json:"flawless_bonus"`
	SpeedBonus      int     `json:"speed_bonus"`
	MistakePenalty  int     `json:"mistake_penalty"`
	TotalBoardScore int     `json:"total_board_score"`
	Explanation     *string `json:"explanation,omitempty"`
}

// CompleteMatchTheFollowingResponseDTO is the rich end-of-game summary for Match the Following.
type CompleteMatchTheFollowingResponseDTO struct {
	Session            string                `json:"session"`
	TotalBoards        int                   `json:"total_boards"`
	ClearedBoards      int                   `json:"cleared_boards"`
	TotalPairsMatched  int                   `json:"total_pairs_matched"`
	TotalPairsCount    int                   `json:"total_pairs_count"`
	TotalMistakes      int                   `json:"total_mistakes"`
	AccuracyPercentage float64               `json:"accuracy_percentage"`
	FinalScore         int                   `json:"final_score"`
	CoinsAwarded       int                   `json:"coins_awarded"`
	XPAwarded          int                   `json:"xp_awarded"`
	GemsAwarded        int                   `json:"gems_awarded"`
	ScoreBreakdown     map[string]int        `json:"score_breakdown"`
	CoinBreakdown      map[string]int        `json:"coin_breakdown"`
	XPBreakdown        map[string]int        `json:"xp_breakdown"`
	LevelUpReward      *LevelUpRewardDTO     `json:"level_up_reward,omitempty"`
	Level              LevelInfoDTO          `json:"level"`
	Streak             StreakInfoDTO         `json:"streak"`
	BoardsReview       []MatchBoardReviewDTO `json:"boards_review"`
}

// SubjectDTO lists a subject's chapters with per-game question counts (learning mode / topic picker).
type SubjectDTO struct {
	TopicID  string       `json:"topic_id"`
	Name     string       `json:"name"`
	Grade    int          `json:"grade"`
	Chapters []ChapterDTO `json:"chapters"`
}

type ChapterDTO struct {
	ChapterNo int            `json:"chapter_no"`
	Title     string         `json:"title"`
	Counts    map[string]int `json:"counts"`
}
