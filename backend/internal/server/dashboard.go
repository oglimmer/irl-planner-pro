package server

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// DashboardRosterEntry is one roster member joined to their submission state.
type DashboardRosterEntry struct {
	FullName          string `json:"fullName"`
	Email             string `json:"email"`
	Attending         string `json:"attending"` // yes | no | not_sure | no_response
	AfterDeadlineEdit bool   `json:"afterDeadlineEdit"`
}

// DashboardOffRoster is someone who submitted but isn't on the roster.
type DashboardOffRoster struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Attending string `json:"attending"`
}

// Dashboard is the admin response overview, organised by attending state.
type Dashboard struct {
	RosterTotal   int                    `json:"rosterTotal"`
	Counts        map[string]int         `json:"counts"`
	RosterEntries []DashboardRosterEntry `json:"rosterEntries"`
	OffRoster     []DashboardOffRoster   `json:"offRoster"`
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

	rosterEntries, counts, err := a.dashboardRoster(r, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	offRoster, err := a.dashboardOffRoster(r, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	writeJSON(w, http.StatusOK, Dashboard{
		RosterTotal:   len(rosterEntries),
		Counts:        counts,
		RosterEntries: rosterEntries,
		OffRoster:     offRoster,
	})
}

func (a *App) dashboardRoster(r *http.Request, eventID string) ([]DashboardRosterEntry, map[string]int, error) {
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT er.full_name, er.email, s.attending,
		        (s.id IS NOT NULL AND s.updated_at > e.submission_deadline) AS after_deadline_edit
		   FROM event_roster er
		   JOIN events e ON e.id = er.event_id
		   LEFT JOIN users u ON lower(u.email) = er.email
		   LEFT JOIN submissions s ON s.event_id = er.event_id AND s.user_id = u.id
		  WHERE er.event_id = $1
		  ORDER BY er.full_name`, eventID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	counts := map[string]int{"yes": 0, "no": 0, "notSure": 0, "noResponse": 0}
	entries := []DashboardRosterEntry{}
	for rows.Next() {
		var e DashboardRosterEntry
		var attending sql.NullString
		if err := rows.Scan(&e.FullName, &e.Email, &attending, &e.AfterDeadlineEdit); err != nil {
			return nil, nil, err
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

func (a *App) dashboardOffRoster(r *http.Request, eventID string) ([]DashboardOffRoster, error) {
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT u.first_name, u.last_name, u.email, s.attending
		   FROM submissions s
		   JOIN users u ON u.id = s.user_id
		  WHERE s.event_id = $1
		    AND lower(u.email) NOT IN (SELECT email FROM event_roster WHERE event_id = $1)
		  ORDER BY u.first_name`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DashboardOffRoster{}
	for rows.Next() {
		var first, last, email, attending string
		if err := rows.Scan(&first, &last, &email, &attending); err != nil {
			return nil, err
		}
		out = append(out, DashboardOffRoster{Name: first + " " + last, Email: email, Attending: attending})
	}
	return out, rows.Err()
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
