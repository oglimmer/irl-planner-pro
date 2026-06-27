package server

import (
	"context"
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

// RosterEntry is one parsed CSV row (name + work email) for an attendee import.
type RosterEntry struct {
	FullName string `json:"fullName"`
	Email    string `json:"email"`
}

// attendeeImportResult is the parse/import report returned to the admin. Added is
// the number of users newly linked to the event; Skipped covers invalid rows and
// people already on the attendee list.
type attendeeImportResult struct {
	Added   int      `json:"added"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors"`
}

// handleImportAttendees accepts a multipart CSV (field "file") with name,email
// columns and adds each person to the event's attendee list, provisioning a
// company-directory user for any email not seen before. The import is additive
// (ON CONFLICT DO NOTHING) — it never removes existing attendees, so re-running
// it can only grow the list. Removal is an explicit per-person action.
func (a *App) handleImportAttendees(w http.ResponseWriter, r *http.Request) {
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

	added, err := provisionAttendees(r.Context(), tx, eventID, entries)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	result.Added = added
	result.Skipped += len(entries) - added // valid rows that were already attendees

	actor := currentUser(r)
	actorID := actor.ID
	summary := fmt.Sprintf("%s imported %d attendee(s)", actor.Email, added)
	if err := a.logActivity(r.Context(), tx, eventID, &actorID, actor.Email, "", actionAttendeesImported, summary, nil, false); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// provisionAttendees upserts a directory user for each entry (by lower-cased
// email, splitting full_name into first/last only when creating) and links them
// to the event. Returns the number of attendees newly added (i.e. not already on
// the list). Provisioned users get a NULL last_login_at until they sign in.
func provisionAttendees(ctx context.Context, tx *sql.Tx, eventID string, entries []RosterEntry) (int, error) {
	added := 0
	for _, e := range entries {
		first, last := splitName(e.FullName)
		var userID string
		// Create the directory user if absent; either way fetch their id. The
		// INSERT … ON CONFLICT … RETURNING yields no row on conflict, so fall back
		// to a SELECT for the existing user.
		err := tx.QueryRowContext(ctx,
			`INSERT INTO users (email, first_name, last_name) VALUES ($1,$2,$3)
			 ON CONFLICT (email) DO NOTHING RETURNING id`, e.Email, first, last).Scan(&userID)
		if err == sql.ErrNoRows {
			if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, e.Email).Scan(&userID); err != nil {
				return added, err
			}
		} else if err != nil {
			return added, err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO event_attendees (event_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			eventID, userID)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	return added, nil
}

// handleAddAttendee links an existing directory user to the event by user id.
// Used by the "add an employee" picker. Idempotent.
func (a *App) handleAddAttendee(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "userId")

	var email string
	err := a.DB.QueryRowContext(r.Context(), `SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(r.Context(),
		`INSERT INTO event_attendees (event_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		eventID, userID)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	// Only log when this call actually added them, so repeat clicks stay quiet.
	if n, _ := res.RowsAffected(); n > 0 {
		actor := currentUser(r)
		actorID := actor.ID
		summary := fmt.Sprintf("%s added %s as an attendee", actor.Email, email)
		if err := a.logActivity(r.Context(), tx, eventID, &actorID, actor.Email, email, actionAttendeeAdded, summary, nil, false); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveAttendee unlinks a user from the event. Their directory record and
// any submission they made are left intact — only the event membership is
// removed. Idempotent.
func (a *App) handleRemoveAttendee(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "userId")

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(r.Context(),
		`DELETE FROM event_attendees WHERE event_id = $1 AND user_id = $2`, eventID, userID)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		var email string
		_ = tx.QueryRowContext(r.Context(), `SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
		actor := currentUser(r)
		actorID := actor.ID
		summary := fmt.Sprintf("%s removed %s from the attendee list", actor.Email, email)
		if err := a.logActivity(r.Context(), tx, eventID, &actorID, actor.Email, email, actionAttendeeRemoved, summary, nil, false); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseRosterCSV reads name,email rows, de-duplicating by lower-cased email and
// validating each. It tolerates an optional header row and extra columns, and
// accepts the two columns in either order when a header names them.
func parseRosterCSV(rd io.Reader) ([]RosterEntry, attendeeImportResult) {
	var result attendeeImportResult
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
