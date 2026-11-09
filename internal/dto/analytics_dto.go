package dto

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

// Limits on what the app may report. They keep the table small and make sure no
// personal text can slip in through a free-form field.
const (
	MaxAnalyticsEventsPerRequest = 25
	maxAnalyticsProps            = 8
	maxAnalyticsPropsBytes       = 512
	// An event may be reported late (the phone was offline) but not from long ago.
	maxAnalyticsEventAge = 24 * time.Hour
)

// AnalyticsEventNames are the only events the app may report. Add a name here (and
// to analytics/reports.sql in the migration repo) when the app starts reporting a
// new one; anything else is dropped.
var AnalyticsEventNames = map[string]bool{
	"app_open":              true,
	"reminder_prompt_shown": true,
	"reminder_enabled":      true,
	"reminder_declined":     true,
	"reminder_blocked":      true,
	"game_quit":             true,
}

var (
	analyticsKeyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)
	analyticsValuePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
)

// AnalyticsEventDTO is one reported event.
type AnalyticsEventDTO struct {
	Name  string         `json:"name"`
	Props map[string]any `json:"props"`
	// At is when it happened on the phone; omitted means "now".
	At *time.Time `json:"at"`
}

// AnalyticsBatchDTO is the body of POST /analytics/events.
type AnalyticsBatchDTO struct {
	Events []AnalyticsEventDTO `json:"events" binding:"required"`
}

// Validate checks the batch as a whole. Single bad events are dropped later, not
// rejected, so one odd event never loses the rest.
func (r *AnalyticsBatchDTO) Validate() error {
	if len(r.Events) == 0 {
		return errors.New("events must not be empty")
	}
	if len(r.Events) > MaxAnalyticsEventsPerRequest {
		return errors.New("too many events in one request")
	}
	return nil
}

// AnalyticsAcceptedDTO says how many events were stored and how many were dropped
// as unknown or malformed.
type AnalyticsAcceptedDTO struct {
	Accepted int `json:"accepted"`
	Dropped  int `json:"dropped"`
}

// CleanAnalyticsEvents turns the reported events into rows to store. Events with
// an unknown name or unusable details are dropped; the count is returned. now is
// the server's time, used to keep timestamps believable.
func CleanAnalyticsEvents(clientID int, events []AnalyticsEventDTO, now time.Time) (rows []models.AnalyticsEvent, dropped int) {
	for _, e := range events {
		if !AnalyticsEventNames[e.Name] {
			dropped++
			continue
		}
		props, ok := cleanAnalyticsProps(e.Props)
		if !ok {
			dropped++
			continue
		}
		at := now
		if e.At != nil {
			at = e.At.UTC()
			if at.After(now) {
				at = now // a phone clock running ahead
			}
			if at.Before(now.Add(-maxAnalyticsEventAge)) {
				at = now.Add(-maxAnalyticsEventAge)
			}
		}
		rows = append(rows, models.AnalyticsEvent{
			ClientID: clientID, Name: e.Name, Props: props, OccurredAt: at,
		})
	}
	return rows, dropped
}

// cleanAnalyticsProps accepts a flat object of short strings, numbers and booleans
// with simple lower_snake_case keys, and returns it as JSON text. Nested values,
// long strings, odd keys or an oversized object make the whole event unusable.
func cleanAnalyticsProps(props map[string]any) (string, bool) {
	if len(props) > maxAnalyticsProps {
		return "", false
	}
	clean := make(map[string]any, len(props))
	for k, v := range props {
		if !analyticsKeyPattern.MatchString(k) {
			return "", false
		}
		switch val := v.(type) {
		case string:
			// Only short identifiers such as "dashboard" or "mcq": no spaces, no
			// "@", no capitals, so an email address or a name cannot be sent.
			if !analyticsValuePattern.MatchString(val) {
				return "", false
			}
		case bool, float64:
		default:
			return "", false // nested objects, lists and null are not allowed
		}
		clean[k] = v
	}
	encoded, err := json.Marshal(clean)
	if err != nil || len(encoded) > maxAnalyticsPropsBytes {
		return "", false
	}
	return string(encoded), true
}
