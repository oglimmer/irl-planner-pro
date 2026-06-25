package server

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"

	"github.com/go-chi/chi/v5"
)

// rosterUploadCap bounds the uploaded CSV size (matches the nginx 4m limit).
const rosterUploadCap = 4 << 20

// RosterEntry is one roster member (name + work email).
type RosterEntry struct {
	FullName string `json:"fullName"`
	Email    string `json:"email"`
}

// rosterUploadResult is the parse report returned to the admin.
type rosterUploadResult struct {
	Inserted int      `json:"inserted"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}

func (a *App) handleListRoster(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT full_name, email FROM event_roster WHERE event_id = $1 ORDER BY full_name`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer rows.Close()
	out := []RosterEntry{}
	for rows.Next() {
		var e RosterEntry
		if err := rows.Scan(&e.FullName, &e.Email); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUploadRoster accepts a multipart CSV (field "file") with name,email
// columns and replaces the event's roster transactionally.
func (a *App) handleUploadRoster(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Confirm the event exists (and get its id back) before parsing.
	var eventID string
	err := a.DB.QueryRowContext(r.Context(), `SELECT id FROM events WHERE id = $1`, id).Scan(&eventID)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, rosterUploadCap)
	if err := r.ParseMultipartForm(rosterUploadCap); err != nil {
		writeErr(w, http.StatusBadRequest, "file too large or invalid upload")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	entries, result := parseRosterCSV(file)
	if len(entries) == 0 {
		result.Errors = append(result.Errors, "no valid rows found")
		writeJSON(w, http.StatusBadRequest, result)
		return
	}

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(), `DELETE FROM event_roster WHERE event_id = $1`, eventID); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO event_roster (event_id, full_name, email) VALUES ($1,$2,$3)`,
			eventID, e.FullName, e.Email); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
	}
	actor := currentUser(r)
	actorID := actor.ID
	summary := fmt.Sprintf("%s uploaded a roster of %d people", actor.Email, len(entries))
	if err := a.logActivity(r.Context(), tx, eventID, &actorID, actor.Email, "", actionRosterUploaded, summary, nil, false); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	result.Inserted = len(entries)
	writeJSON(w, http.StatusOK, result)
}

// parseRosterCSV reads name,email rows, de-duplicating by lower-cased email and
// validating each. It tolerates an optional header row and extra columns, and
// accepts the two columns in either order when a header names them.
func parseRosterCSV(rd io.Reader) ([]RosterEntry, rosterUploadResult) {
	var result rosterUploadResult
	cr := csv.NewReader(rd)
	cr.FieldsPerRecord = -1 // allow ragged rows
	cr.TrimLeadingSpace = true

	nameIdx, emailIdx := 0, 1
	seen := map[string]bool{}
	var out []RosterEntry

	rowNum := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("row %d: %v", rowNum+1, err))
			result.Skipped++
			continue
		}
		rowNum++
		if len(rec) < 2 {
			result.Skipped++
			result.Errors = append(result.Errors, fmt.Sprintf("row %d: expected name,email", rowNum))
			continue
		}
		// Header detection on the first row: if it names the columns, map indices.
		if rowNum == 1 && looksLikeHeader(rec) {
			nameIdx, emailIdx = headerIndices(rec)
			continue
		}
		name := strings.TrimSpace(rec[nameIdx])
		email := strings.ToLower(strings.TrimSpace(rec[emailIdx]))
		if name == "" || email == "" {
			result.Skipped++
			result.Errors = append(result.Errors, fmt.Sprintf("row %d: missing name or email", rowNum))
			continue
		}
		if _, err := mail.ParseAddress(email); err != nil {
			result.Skipped++
			result.Errors = append(result.Errors, fmt.Sprintf("row %d: invalid email %q", rowNum, email))
			continue
		}
		if seen[email] {
			result.Skipped++
			continue
		}
		seen[email] = true
		out = append(out, RosterEntry{FullName: name, Email: email})
	}
	return out, result
}

func looksLikeHeader(rec []string) bool {
	for _, c := range rec {
		lc := strings.ToLower(strings.TrimSpace(c))
		if lc == "email" || lc == "name" || lc == "full name" || lc == "full_name" {
			return true
		}
	}
	return false
}

func headerIndices(rec []string) (nameIdx, emailIdx int) {
	nameIdx, emailIdx = 0, 1
	for i, c := range rec {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "name", "full name", "full_name":
			nameIdx = i
		case "email", "work email", "e-mail":
			emailIdx = i
		}
	}
	return nameIdx, emailIdx
}
