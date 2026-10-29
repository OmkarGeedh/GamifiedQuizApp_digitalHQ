package handlers_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/google/uuid"
)

// contentFixture owns a throwaway subject with one chapter. Rows are written to the
// content tables and removed by cascade when the test ends.
type contentFixture struct {
	t     *testing.T
	Topic string
}

func newContentFixture(t *testing.T) *contentFixture {
	t.Helper()
	topic := "t" + strings.ReplaceAll(uuid.NewString(), "-", "")[:7]
	f := &contentFixture{t: t, Topic: topic}
	f.exec(`INSERT INTO subjects (code, short_code, grade, name, chapters) VALUES (?, ?, 11, 'Test', '{"1": "Test"}')`, topic, topic)
	t.Cleanup(func() { config.DB.Exec("DELETE FROM subjects WHERE code = ?", topic) })
	return f
}

func (f *contentFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if err := config.DB.Exec(sql, args...).Error; err != nil {
		f.t.Fatalf("fixture: %v", err)
	}
}

// Code returns a valid question code for this fixture, e.g. t1a2b3c4-01-mcq-0001.
func (f *contentFixture) Code(tag string, n int) string {
	return fmt.Sprintf("%s-01-%s-%04d", f.Topic, tag, n)
}

func (f *contentFixture) MCQ(n int, prompt, a, b, c, d, correct string) string {
	code := f.Code("mcq", n)
	f.exec(`INSERT INTO mcq_questions (question_code, subject, chapter_no, prompt, option_a, option_b, option_c, option_d, correct_option, difficulty)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, 1)`, code, f.Topic, prompt, a, b, c, d, correct)
	return code
}

func (f *contentFixture) SuddenDeath(n int, prompt, a, b, correct string) string {
	code := f.Code("sd", n)
	f.exec(`INSERT INTO sudden_death_questions (question_code, subject, chapter_no, prompt, option_a, option_b, correct_option, difficulty)
		VALUES (?, ?, 1, ?, ?, ?, ?, 1)`, code, f.Topic, prompt, a, b, correct)
	return code
}

// SortSet creates a set with the given items (prompt -> correct bucket) and returns its set code.
func (f *contentFixture) SortSet(n int, bucketA, bucketB string, items map[string]string) string {
	setCode := fmt.Sprintf("%s-01-set-%03d", f.Topic, n)
	i := n * 100
	for prompt, correct := range items {
		i++
		f.exec(`INSERT INTO sort_it_out_questions (question_code, subject, chapter_no, set_code, item_type, bucket_a, bucket_b, prompt, correct_bucket, difficulty)
			VALUES (?, ?, 1, ?, 'Test', ?, ?, ?, ?, 1)`, f.Code("so", i), f.Topic, setCode, bucketA, bucketB, prompt, correct)
	}
	return setCode
}

// MatchPairs creates n pairs L1..Ln / R1..Rn and returns their codes.
func (f *contentFixture) MatchPairs(n int) []string {
	codes := make([]string, n)
	for i := range codes {
		codes[i] = f.Code("mf", i+1)
		f.exec(`INSERT INTO match_pairs (question_code, subject, chapter_no, left_text, right_text, difficulty) VALUES (?, ?, 1, ?, ?, 1)`,
			codes[i], f.Topic, fmt.Sprintf("L%d", i+1), fmt.Sprintf("R%d", i+1))
	}
	return codes
}
