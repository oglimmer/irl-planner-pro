package server

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// DashboardEntry is one event attendee (a company user expected at the event)
// joined to their submission state. Every person in the overview is a real user;
// there is no separate "off-roster" category — a submission auto-adds its author
// as an attendee, so this list is exactly the event's membership.
type DashboardEntry struct {
	UserID            string `json:"userId"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	Attending         string `json:"attending"` // yes | no | not_sure | no_response
	AfterDeadlineEdit bool   `json:"afterDeadlineEdit"`
	HasLoggedIn       bool   `json:"hasLoggedIn"` // false = provisioned by import, never signed in
}

// Dashboard is the admin response overview, organised by attending state.
type Dashboard struct {
	Total   int             `json:"total"`
	Counts  map[string]int  `json:"counts"`
	Entries []DashboardEntry `json:"entries"`
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Confirm the event exists so an unknown id 404s rather than returning an
	// empty-but-200 dashboard.
	var exists bool
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM events WHERE id = $1)`, id).Scan(&exists); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}

	entries, counts, err := a.dashboardEntries(r.Context(), id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	writeJSON(w, http.StatusOK, Dashboard{
		Total:   len(entries),
		Counts:  counts,
		Entries: entries,
	})
}

// dashboardEntries returns every attendee of the event joined to their
// submission, bucketed by attending state.
func (a *App) dashboardEntries(ctx context.Context, eventID string) ([]DashboardEntry, map[string]int, error) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT u.id, u.first_name, u.last_name, u.email,
		        (u.last_login_at IS NOT NULL) AS has_logged_in,
		        s.attending,
		        (s.id IS NOT NULL AND s.updated_at > e.submission_deadline) AS after_deadline_edit
		   FROM event_attendees ea
		   JOIN events e ON e.id = ea.event_id
		   JOIN users u ON u.id = ea.user_id
		   LEFT JOIN submissions s ON s.event_id = ea.event_id AND s.user_id = ea.user_id
		  WHERE ea.event_id = $1
		  ORDER BY u.first_name, u.last_name, u.email`, eventID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	counts := map[string]int{"yes": 0, "no": 0, "notSure": 0, "noResponse": 0}
	entries := []DashboardEntry{}
	for rows.Next() {
		var e DashboardEntry
		var first, last string
		var attending sql.NullString
		if err := rows.Scan(&e.UserID, &first, &last, &e.Email, &e.HasLoggedIn, &attending, &e.AfterDeadlineEdit); err != nil {
			return nil, nil, err
		}
		e.Name = strings.TrimSpace(first + " " + last)
		if e.Name == "" {
			e.Name = e.Email
		}
		if attending.Valid {
			e.Attending = attending.String
		} else {
			e.Attending = "no_response"
		}
		counts[countKey(e.Attending)]++
		entries = append(entries, e)
	}
	return entries, counts, rows.Err()
}

// countKey maps an attending value to its dashboard counts key.
func countKey(attending string) string {
	switch attending {
	case "yes":
		return "yes"
	case "no":
		return "no"
	case "not_sure":
		return "notSure"
	default:
		return "noResponse"
	}
}
