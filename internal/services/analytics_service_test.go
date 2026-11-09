package services

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/testutil"
	"gorm.io/gorm"
)

func analyticsDB(t *testing.T, withTable bool) *gorm.DB {
	t.Helper()
	db := testutil.NewDB(t)
	if withTable {
		err := db.Exec(`CREATE TABLE analytics_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT, client_id INTEGER NOT NULL, name TEXT NOT NULL,
			props TEXT NOT NULL DEFAULT '{}', occurred_at DATETIME NOT NULL,
			received_at DATETIME DEFAULT CURRENT_TIMESTAMP)`).Error
		if err != nil {
			t.Fatal(err)
		}
	}
	prev := analyticsNow
	analyticsNow = func() time.Time { return time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { analyticsNow = prev })
	return db
}

type storedEvent struct {
	ClientID int
	Name     string
	Props    string
}

func storedEvents(t *testing.T, db *gorm.DB) []storedEvent {
	t.Helper()
	var rows []storedEvent
	if err := db.Raw(`SELECT client_id, name, props FROM analytics_events ORDER BY id`).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestRecordAnalyticsEvents_StoresGoodEventsAndCountsTheRest(t *testing.T) {
	db := analyticsDB(t, true)
	id := testutil.SeedClient(t, db, "a")

	res, status, err := RecordAnalyticsEvents(context.Background(), id, &dto.AnalyticsBatchDTO{
		Events: []dto.AnalyticsEventDTO{
			{Name: "app_open", Props: map[string]any{"platform": "android"}},
			{Name: "not_an_event"},
			{Name: "game_quit", Props: map[string]any{"mode": "mcq", "question_index": float64(3)}},
		},
	})
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("status %d err %v", status, err)
	}
	if res.Accepted != 2 || res.Dropped != 1 {
		t.Errorf("result = %+v, want 2 accepted 1 dropped", res)
	}
	rows := storedEvents(t, db)
	if len(rows) != 2 || rows[0].Name != "app_open" || rows[1].Name != "game_quit" {
		t.Fatalf("stored = %+v", rows)
	}
	if rows[0].ClientID != id || rows[0].Props != `{"platform":"android"}` {
		t.Errorf("first row = %+v", rows[0])
	}
}

func TestRecordAnalyticsEvents_EmptyBatchIsABadRequest(t *testing.T) {
	analyticsDB(t, true)
	_, status, err := RecordAnalyticsEvents(context.Background(), 1, &dto.AnalyticsBatchDTO{})
	if err == nil || status != http.StatusBadRequest {
		t.Errorf("status %d err %v, want 400", status, err)
	}
}

func TestRecordAnalyticsEvents_AllBadStoresNothingButIsNotAnError(t *testing.T) {
	db := analyticsDB(t, true)
	id := testutil.SeedClient(t, db, "a")
	res, status, err := RecordAnalyticsEvents(context.Background(), id, &dto.AnalyticsBatchDTO{
		Events: []dto.AnalyticsEventDTO{{Name: "nope"}, {Name: "also_nope"}},
	})
	if err != nil || status != http.StatusAccepted || res.Accepted != 0 || res.Dropped != 2 {
		t.Errorf("status %d err %v res %+v", status, err, res)
	}
	if n := len(storedEvents(t, db)); n != 0 {
		t.Errorf("%d rows stored, want none", n)
	}
}

// The migration may not have been applied yet. The player must never see a
// failure for it.
func TestRecordAnalyticsEvents_MissingTableNeverFailsThePlayer(t *testing.T) {
	db := analyticsDB(t, false)
	id := testutil.SeedClient(t, db, "a")

	res, status, err := RecordAnalyticsEvents(context.Background(), id, &dto.AnalyticsBatchDTO{
		Events: []dto.AnalyticsEventDTO{{Name: "app_open"}},
	})
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("status %d err %v, want a quiet 202", status, err)
	}
	if res.Accepted != 0 || res.Dropped != 1 {
		t.Errorf("result = %+v", res)
	}
}

func TestRecordAnalyticsEvents_StoresTheCallersIdNotAnythingFromTheBody(t *testing.T) {
	db := analyticsDB(t, true)
	a := testutil.SeedClient(t, db, "a")
	b := testutil.SeedClient(t, db, "b")
	// A client_id smuggled into props is just a rejected number-keyed field.
	_, _, _ = RecordAnalyticsEvents(context.Background(), a, &dto.AnalyticsBatchDTO{
		Events: []dto.AnalyticsEventDTO{{Name: "app_open", Props: map[string]any{"client_id": float64(b)}}},
	})
	for _, e := range storedEvents(t, db) {
		if e.ClientID != a {
			t.Errorf("event stored for player %d, want the caller %d", e.ClientID, a)
		}
	}
}
