package server

import (
	"database/sql"
	"encoding/csv"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// handleExportCSV streams a CSV of roster members whose attending state matches
// the ?attending= filter (a comma-separated subset of yes,no,not_sure,
// no_response). An empty filter exports everyone. Rows for no-response members
// carry empty submission columns, so the export doubles as a non-responder list.
func (a *App) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	e, err := a.loadEventByColumn(r.Context(), "id", id, time.Now())
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	loc, lerr := loadLocation(e.Timezone)
	if lerr != nil {
		loc = time.UTC
	}

	filter := parseAttendingFilter(r.URL.Query().Get("attending"))

	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT er.full_name, er.email, s.attending, s.first_name, s.last_name,
		        s.arrival_day, s.arrival_time, s.arrival_mode, s.arrival_details,
		        s.departure_day, s.departure_time, s.departure_mode, s.departure_details,
		        s.long_haul, s.extra_stay_start, s.extra_stay_end, s.allergies, s.comments,
		        s.updated_at
		   FROM event_roster er
		   LEFT JOIN users u ON lower(u.email) = er.email
		   LEFT JOIN submissions s ON s.event_id = er.event_id AND s.user_id = u.id
		  WHERE er.event_id = $1
		  ORDER BY er.full_name`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+e.Slug+`-responses.csv"`)
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{
		"name", "email", "attending", "arrival_day", "arrival_time", "arrival_mode",
		"arrival_details", "departure_day", "departure_time", "departure_mode",
		"departure_details", "long_haul", "extra_night_before", "extra_night_after",
		"allergies", "comments", "last_updated",
	})

	for rows.Next() {
		var fullName, email string
		var attending, firstName, lastName, arrTime, arrMode, arrDetails sql.NullString
		var depTime, depMode, depDetails, allergies, comments sql.NullString
		var arrDay, depDay, extraStart, extraEnd, updatedAt sql.NullTime
		var longHaul sql.NullBool
		if err := rows.Scan(&fullName, &email, &attending, &firstName, &lastName,
			&arrDay, &arrTime, &arrMode, &arrDetails,
			&depDay, &depTime, &depMode, &depDetails,
			&longHaul, &extraStart, &extraEnd, &allergies, &comments, &updatedAt); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
		state := "no_response"
		if attending.Valid {
			state = attending.String
		}
		if len(filter) > 0 && !filter[state] {
			continue
		}
		name := fullName
		if firstName.Valid && (firstName.String != "" || lastName.String != "") {
			name = strings.TrimSpace(firstName.String + " " + lastName.String)
		}
		_ = cw.Write([]string{
			name, email, state,
			dateOrEmpty(arrDay), arrTime.String, arrMode.String, arrDetails.String,
			dateOrEmpty(depDay), depTime.String, depMode.String, depDetails.String,
			boolOrEmpty(longHaul), dateOrEmpty(extraStart), dateOrEmpty(extraEnd),
			allergies.String, comments.String, timeInZoneOrEmpty(updatedAt, loc),
		})
	}
	if err := rows.Err(); err != nil {
		serverErr(w, r, err, "db error")
	}
}

// handleListSubmissions returns every submission for an event (admin table view).
func (a *App) handleListSubmissions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT s.user_id FROM submissions s WHERE s.event_id = $1`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	var userIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			serverErr(w, r, err, "db error")
			return
		}
		userIDs = append(userIDs, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	out := []Submission{}
	for _, uid := range userIDs {
		s, err := a.loadSubmission(r.Context(), id, uid)
		if err != nil {
			serverErr(w, r, err, "db error")
			return
		}
		out = append(out, *s)
	}
	writeJSON(w, http.StatusOK, out)
}

// parseAttendingFilter parses the comma-separated ?attending= filter into a set.
// An empty/absent value yields an empty set, meaning "no filter" (export all).
func parseAttendingFilter(s string) map[string]bool {
	set := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		switch p {
		case "yes", "no", "not_sure", "no_response":
			set[p] = true
		}
	}
	return set
}

func dateOrEmpty(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(dateLayout)
}

func boolOrEmpty(b sql.NullBool) string {
	if !b.Valid {
		return ""
	}
	if b.Bool {
		return "yes"
	}
	return "no"
}

func timeInZoneOrEmpty(t sql.NullTime, loc *time.Location) string {
	if !t.Valid {
		return ""
	}
	return t.Time.In(loc).Format("2006-01-02 15:04 MST")
}
