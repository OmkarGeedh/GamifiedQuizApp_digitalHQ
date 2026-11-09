package models

import "time"

// AnalyticsEvent is one thing the app reported (a screen opened, a reminder
// choice, a game quit half way). Append-only; table owned by the migration repo
// (000008_analytics_events). Props holds a small JSON object, stored as text here
// and as jsonb in PostgreSQL.
type AnalyticsEvent struct {
	ClientID   int
	Name       string
	Props      string
	OccurredAt time.Time
}
