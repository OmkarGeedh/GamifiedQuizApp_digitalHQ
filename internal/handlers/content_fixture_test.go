package handlers_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/google/uuid"
)

// contentFixture owns a throwaway subject with one chapter. Rows are written to the
// base content tables and removed by cascade when the test ends.
type contentFixture struct {
	t         *testing.T
	Topic     string
	chapterID int
}

func newContentFixture(t *testing.T) *contentFixture {
	t.Helper()
	topic := "t" + strings.ReplaceAll(uuid.NewString(), "-", "")[:7]
	f := &contentFixture{t: t, Topic: topic}
	var subjectID int
	f.exec(&subjectID, "INSERT INTO subjects (code, short_code, grade, name) VALUES (?, ?, 11, 'Test') RETURNING id", topic, topic)
	f.exec(&f.chapterID, "INSERT INTO chapters (subject_id, chapter_no, title) VALUES (?, 1, 'Test') RETURNING id", subjectID)
	t.Cleanup(func() { config.DB.Exec("DELETE FROM subjects WHERE code = ?", topic) })
	return f
}

func (f *contentFixture) exec(dest *int, sql string, args ...any) {
	f.t.Helper()
	if err := config.DB.Raw(sql, args...).Scan(dest).Error; err != nil {
		f.t.Fatalf("fixture: %v", err)
	}
}

// Code returns a valid question code for this fixture, e.g. t1a2b3c4-01-mcq-0001.
func (f *contentFixture) Code(tag string, n int) string {
	return fmt.Sprintf("%s-01-%s-%04d", f.Topic, tag, n)
}

func (f *contentFixture) MCQ(n int, prompt, a, b, c, d, correct string) string {
	code := f.Code("mcq", n)
	var id int
	f.exec(&id, `INSERT INTO mcq_questions (question_code, chapter_id, prompt, option_a, option_b, option_c, option_d, correct_option, difficulty, points)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 10) RETURNING id`, code, f.chapterID, prompt, a, b, c, d, correct)
	return code
}

func (f *contentFixture) SuddenDeath(n int, prompt, a, b, correct string) string {
	code := f.Code("sd", n)
	var id int
	f.exec(&id, `INSERT INTO sudden_death_questions (question_code, chapter_id, prompt, option_a, option_b, correct_option, difficulty, points)
		VALUES (?, ?, ?, ?, ?, ?, 1, 10) RETURNING id`, code, f.chapterID, prompt, a, b, correct)
	return code
}

// SortSet creates a set with the given items (prompt -> correct bucket) and returns its set code.
func (f *contentFixture) SortSet(n int, bucketA, bucketB string, items map[string]string) string {
	setCode := fmt.Sprintf("%s-01-set-%03d", f.Topic, n)
	var setID int
	f.exec(&setID, `INSERT INTO sort_it_out_sets (set_code, chapter_id, item_type, bucket_a, bucket_b) VALUES (?, ?, 'Test', ?, ?) RETURNING id`,
		setCode, f.chapterID, bucketA, bucketB)
	i := n * 100
	for prompt, correct := range items {
		i++
		var id int
		f.exec(&id, `INSERT INTO sort_it_out_items (question_code, set_id, prompt, correct_bucket, difficulty, points) VALUES (?, ?, ?, ?, 1, 3) RETURNING id`,
			f.Code("so", i), setID, prompt, correct)
	}
	return setCode
}

// Board creates a match board with pairs L1..Ln / R1..Rn and returns its code.
func (f *contentFixture) Board(n int, prompt string, pairs int) string {
	code := f.Code("mf", n)
	var boardID int
	f.exec(&boardID, `INSERT INTO match_boards (question_code, chapter_id, prompt, difficulty, points) VALUES (?, ?, ?, 1, 40) RETURNING id`,
		code, f.chapterID, prompt)
	for i := 1; i <= pairs; i++ {
		if err := config.DB.Exec(`INSERT INTO match_pairs (board_id, pair_order, left_text, right_text) VALUES (?, ?, ?, ?)`,
			boardID, i, fmt.Sprintf("L%d", i), fmt.Sprintf("R%d", i)).Error; err != nil {
			f.t.Fatalf("fixture: %v", err)
		}
	}
	return code
}
