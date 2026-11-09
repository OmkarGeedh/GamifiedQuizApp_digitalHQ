package services

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

// analyticsNow is replaced in tests.
var analyticsNow = time.Now

var (
	analyticsLogMu   sync.Mutex
	analyticsLastLog time.Time
)

// logAnalyticsFailure logs a storage failure at most once a minute, so a database
// that is missing the table (migration 000008 not applied yet) does not flood the
// log with one line per request.
func logAnalyticsFailure(err error) {
	analyticsLogMu.Lock()
	defer analyticsLogMu.Unlock()
	if now := analyticsNow(); now.Sub(analyticsLastLog) >= time.Minute {
		analyticsLastLog = now
		log.Printf("analytics events not stored: %v", err)
	}
}

// RecordAnalyticsEvents stores the events the app reports. Analytics must never
// get in the way of the app, so the player always gets a success answer: events
// that are unknown or malformed are dropped one by one, and a storage failure is
// logged, not returned.
func RecordAnalyticsEvents(ctx context.Context, clientID int, req *dto.AnalyticsBatchDTO) (*dto.AnalyticsAcceptedDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}
	rows, dropped := dto.CleanAnalyticsEvents(clientID, req.Events, analyticsNow().UTC())

	if err := repo.InsertAnalyticsEvents(ctx, rows); err != nil {
		logAnalyticsFailure(err)
		return &dto.AnalyticsAcceptedDTO{Accepted: 0, Dropped: len(req.Events)}, http.StatusAccepted, nil
	}
	return &dto.AnalyticsAcceptedDTO{Accepted: len(rows), Dropped: dropped}, http.StatusAccepted, nil
}
