package server

import (
	"net/http"

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
	Total   int              `json:"total"`
	Counts  map[string]int   `json:"counts"`
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

	entries, counts, err := a.Store.dashboardEntries(r.Context(), id)
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
