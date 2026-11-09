package dto_test

import (
	"strings"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
)

var analyticsNow = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

func TestAnalyticsBatch_Validate(t *testing.T) {
	if err := (&dto.AnalyticsBatchDTO{}).Validate(); err == nil {
		t.Error("an empty batch must be refused")
	}
	ok := &dto.AnalyticsBatchDTO{Events: make([]dto.AnalyticsEventDTO, dto.MaxAnalyticsEventsPerRequest)}
	if err := ok.Validate(); err != nil {
		t.Errorf("a full batch should be fine: %v", err)
	}
	tooMany := &dto.AnalyticsBatchDTO{Events: make([]dto.AnalyticsEventDTO, dto.MaxAnalyticsEventsPerRequest+1)}
	if err := tooMany.Validate(); err == nil {
		t.Error("one over the limit must be refused")
	}
}

func TestCleanAnalyticsEvents_KnownEventIsKept(t *testing.T) {
	rows, dropped := dto.CleanAnalyticsEvents(7, []dto.AnalyticsEventDTO{
		{Name: "game_quit", Props: map[string]any{"screen": "dashboard", "scrolled": true, "n": float64(3)}},
	}, analyticsNow)
	if dropped != 0 || len(rows) != 1 {
		t.Fatalf("rows=%d dropped=%d", len(rows), dropped)
	}
	r := rows[0]
	if r.ClientID != 7 || r.Name != "game_quit" || !r.OccurredAt.Equal(analyticsNow) {
		t.Errorf("row = %+v", r)
	}
	for _, want := range []string{`"screen":"dashboard"`, `"scrolled":true`, `"n":3`} {
		if !strings.Contains(r.Props, want) {
			t.Errorf("props %s should contain %s", r.Props, want)
		}
	}
}

func TestCleanAnalyticsEvents_NoPropsBecomesEmptyObject(t *testing.T) {
	rows, _ := dto.CleanAnalyticsEvents(1, []dto.AnalyticsEventDTO{{Name: "app_open"}}, analyticsNow)
	if len(rows) != 1 || rows[0].Props != "{}" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestCleanAnalyticsEvents_DropsOnlyTheBadOnes(t *testing.T) {
	long := strings.Repeat("x", 65)
	manyKeys := map[string]any{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} {
		manyKeys[k] = true
	}
	events := []dto.AnalyticsEventDTO{
		{Name: "app_open"}, // good
		{Name: "buy_everything"},                                 // unknown name
		{Name: "game_quit", Props: map[string]any{"s": long}},  // string too long
		{Name: "game_quit", Props: map[string]any{"s": map[string]any{"a": 1}}}, // nested
		{Name: "game_quit", Props: map[string]any{"s": []any{1}}},               // list
		{Name: "game_quit", Props: map[string]any{"s": nil}},                    // null
		{Name: "game_quit", Props: map[string]any{"Bad Key": "x"}},              // key not snake_case
		{Name: "game_quit", Props: map[string]any{"s": "a@b.c"}},               // an email address
		{Name: "game_quit", Props: map[string]any{"s": "Omkar Geedh"}},          // a name
		{Name: "game_quit", Props: manyKeys},                                    // too many keys
		{Name: "game_quit", Props: map[string]any{"mode": "mcq", "question_index": float64(4)}}, // good
	}
	rows, dropped := dto.CleanAnalyticsEvents(1, events, analyticsNow)
	if len(rows) != 2 || dropped != 9 {
		t.Errorf("kept %d dropped %d, want 2 and 9", len(rows), dropped)
	}
}

func TestCleanAnalyticsEvents_OversizedPropsDropped(t *testing.T) {
	// 8 keys of 64 characters is well over the 512-byte cap once encoded.
	props := map[string]any{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		props[k] = strings.Repeat("y", 64)
	}
	rows, dropped := dto.CleanAnalyticsEvents(1, []dto.AnalyticsEventDTO{{Name: "game_quit", Props: props}}, analyticsNow)
	if len(rows) != 0 || dropped != 1 {
		t.Errorf("kept %d dropped %d, want the oversized event dropped", len(rows), dropped)
	}
}

func TestCleanAnalyticsEvents_TimestampsAreKeptBelievable(t *testing.T) {
	at := func(d time.Duration) *time.Time { v := analyticsNow.Add(d); return &v }
	rows, _ := dto.CleanAnalyticsEvents(1, []dto.AnalyticsEventDTO{
		{Name: "app_open", At: at(-time.Hour)},          // reported late: kept as is
		{Name: "app_open", At: at(+6 * time.Hour)},      // phone clock ahead: pulled back to now
		{Name: "app_open", At: at(-72 * time.Hour)},     // ancient: pulled up to 24h ago
	}, analyticsNow)
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	if want := analyticsNow.Add(-time.Hour); !rows[0].OccurredAt.Equal(want) {
		t.Errorf("late event at %v, want %v", rows[0].OccurredAt, want)
	}
	if !rows[1].OccurredAt.Equal(analyticsNow) {
		t.Errorf("future event at %v, want now", rows[1].OccurredAt)
	}
	if want := analyticsNow.Add(-24 * time.Hour); !rows[2].OccurredAt.Equal(want) {
		t.Errorf("ancient event at %v, want %v", rows[2].OccurredAt, want)
	}
}

func TestAnalyticsEventNames_AreTheDocumentedOnes(t *testing.T) {
	for _, name := range []string{"app_open", "reminder_prompt_shown",
		"reminder_enabled", "reminder_declined", "reminder_blocked", "game_quit"} {
		if !dto.AnalyticsEventNames[name] {
			t.Errorf("%s should be allowed", name)
		}
	}
	if len(dto.AnalyticsEventNames) != 6 {
		t.Errorf("%d names allowed; update the migration repo's reports and this test together", len(dto.AnalyticsEventNames))
	}
}
