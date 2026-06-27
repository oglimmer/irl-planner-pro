package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"irlplanner/internal/db"
)

// Activity action vocabulary (see DESIGN.md §5.8).
const (
	actionSubmissionCreated     = "submission.created"
	actionSubmissionUpdated     = "submission.updated"
	actionAdminEditedSubmission = "admin.edited_submission"
	actionEventCreated          = "event.created"
	actionEventUpdated          = "event.updated"
	actionAttendeesImported     = "attendees.imported"
	actionAttendeeAdded         = "attendee.added"
	actionAttendeeRemoved       = "attendee.removed"
	actionReminderSent          = "reminder.sent"
)

// Activity categories classify *what was done*, not who did it. A participant
// action (submitting one's own attendance) is "user" even when performed by an
// admin account; configuration, roster management, admin edits, and reminders
// are "admin". The admin all-activity view defaults to the "user" stream (the
// common case) and filters to "admin" only on demand. See DESIGN.md §5.8.
const (
	categoryUser  = "user"
	categoryAdmin = "admin"
)

// actionCategory maps an action verb to its category. Category is a pure
// function of the action, so it is derived here at write time; migration 0011
// backfills historical rows with the same mapping — keep the two in sync.
func actionCategory(action string) string {
	switch action {
	case actionSubmissionCreated, actionSubmissionUpdated:
		return categoryUser
	default:
		return categoryAdmin
	}
}

// ActivityEntry is the API shape for one activity-log row.
type ActivityEntry struct {
	ID            string          `json:"id"`
	ActorEmail    string          `json:"actorEmail"`
	SubjectEmail  string          `json:"subjectEmail"`
	Action        string          `json:"action"`
	Category      string          `json:"category"`
	Summary       string          `json:"summary"`
	Detail        json.RawMessage `json:"detail,omitempty"`
	AfterDeadline bool            `json:"afterDeadline"`
	CreatedAt     time.Time       `json:"createdAt"`
}

// logActivity appends one entry to the timeline. actorID may be nil for system
// actions. detail is optional structured context (nil to omit). Runs against any
// db.Exec so it can participate in the caller's transaction.
func (a *App) logActivity(ctx context.Context, q db.Exec, eventID string, actorID *string,
	actorEmail, subjectEmail, action, summary string, detail any, afterDeadline bool) error {
	// Bind detail as a string with an explicit ::jsonb cast: pgx sends a Go
	// string as text, which Postgres won't implicitly coerce into jsonb.
	var detailParam interface{}
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		detailParam = string(b)
	}
	_, err := q.ExecContext(ctx,
		`INSERT INTO activity_log (event_id, actor_id, actor_email, subject_email, action, category, summary, detail, after_deadline)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)`,
		eventID, actorID, actorEmail, subjectEmail, action, actionCategory(action), summary, detailParam, afterDeadline)
	return err
}

// queryActivity returns timeline entries for an event, optionally filtered to a
// single subject email (the employee's own view) and/or a category ("user" /
// "admin"; empty means all), newest first.
func (a *App) queryActivity(ctx context.Context, eventID, subjectEmail, category string) ([]ActivityEntry, error) {
	q := `SELECT id, actor_email, subject_email, action, category, summary, detail, after_deadline, created_at
	        FROM activity_log WHERE event_id = $1`
	args := []interface{}{eventID}
	if subjectEmail != "" {
		args = append(args, subjectEmail)
		q += fmt.Sprintf(` AND subject_email = $%d`, len(args))
	}
	if category != "" {
		args = append(args, category)
		q += fmt.Sprintf(` AND category = $%d`, len(args))
	}
	q += ` ORDER BY created_at DESC`
	rows, err := a.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityEntry{}
	for rows.Next() {
		var e ActivityEntry
		var detail []byte
		if err := rows.Scan(&e.ID, &e.ActorEmail, &e.SubjectEmail, &e.Action, &e.Category, &e.Summary,
			&detail, &e.AfterDeadline, &e.CreatedAt); err != nil {
			return nil, err
		}
		if len(detail) > 0 {
			e.Detail = json.RawMessage(detail)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// handleMyActivity returns the caller's own activity entries for an event (by
// slug). Employees see only their own history.
func (a *App) handleMyActivity(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(chi.URLParam(r, "slug"))
	user := currentUser(r)
	var eventID string
	err := a.DB.QueryRowContext(r.Context(), `SELECT id FROM events WHERE slug = $1`, slug).Scan(&eventID)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	entries, err := a.queryActivity(r.Context(), eventID, strings.ToLower(user.Email), "")
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleEventActivity returns the full timeline for an event (admin, by id),
// optionally narrowed to one category via ?category=user|admin (default: all).
func (a *App) handleEventActivity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	category := r.URL.Query().Get("category")
	if category != "" && category != categoryUser && category != categoryAdmin {
		writeErr(w, http.StatusBadRequest, "category must be 'user' or 'admin'")
		return
	}
	entries, err := a.queryActivity(r.Context(), id, "", category)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}
