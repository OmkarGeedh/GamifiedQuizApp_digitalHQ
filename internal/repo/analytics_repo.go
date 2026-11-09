package repo

import (
	"context"
	"strings"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

// InsertAnalyticsEvents stores the events in one statement. props is passed as
// JSON text, which PostgreSQL reads into its jsonb column.
func InsertAnalyticsEvents(ctx context.Context, events []models.AnalyticsEvent) error {
	db := GetDB()
	if db == nil || len(events) == 0 {
		return nil
	}
	placeholders := make([]string, len(events))
	args := make([]any, 0, len(events)*4)
	for i, e := range events {
		placeholders[i] = "(?, ?, ?, ?)"
		args = append(args, e.ClientID, e.Name, e.Props, e.OccurredAt)
	}
	return db.WithContext(ctx).Exec(
		`INSERT INTO analytics_events (client_id, name, props, occurred_at) VALUES `+strings.Join(placeholders, ", "),
		args...).Error
}
