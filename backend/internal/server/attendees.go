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
	"time"

	"github.com/go-chi/chi/v5"

	"irlplanner/internal/db"
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
	detail := map[string]any{
		"added":   added,
		"skipped": result.Skipped,
	}
	const maxErrors = 10
	if len(result.Errors) > 0 {
		if len(result.Errors) > maxErrors {
			detail["errors"] = result.Errors[:maxErrors]
			detail["errorCount"] = len(result.Errors)
		} else {
			detail["errors"] = result.Errors
		}
	}
	if err := a.logActivity(r.Context(), tx, eventID, &actorID, actor.Email, "", actionAttendeesImported, summary, detail, false); err != nil {
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
// to the event. Returns the number of attendees newly added to THIS event (i.e.
// not already on its list). Provisioned users get a NULL last_login_at until they
// sign in. A genuinely new directory user is also linked to every other open
// event, so a freshly imported employee lands on all current events by default.
func provisionAttendees(ctx context.Context, tx *sql.Tx, eventID string, entries []RosterEntry) (int, error) {
	now := time.Now()
	added := 0
	for _, e := range entries {
		first, last := splitName(e.FullName)
		userID, created, err := upsertDirectoryUser(ctx, tx, e.Email, first, last)
		if err != nil {
			return added, err
		}
		if created {
			if err := addUserToOpenEvents(ctx, tx, userID, now); err != nil {
				return added, err
			}
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

// upsertDirectoryUser returns the id of the directory user with this (already
// lower-cased) email, creating the row from first/last when absent. created
// reports whether a new user was inserted (vs. an existing one looked up) — the
// caller uses that to decide whether to seed default event memberships. The
// INSERT … ON CONFLICT … RETURNING yields no row on conflict, so we fall back to
// a SELECT for the existing user.
func upsertDirectoryUser(ctx context.Context, q db.Exec, email, first, last string) (userID string, created bool, err error) {
	err = q.QueryRowContext(ctx,
		`INSERT INTO users (email, first_name, last_name) VALUES ($1,$2,$3)
		 ON CONFLICT (email) DO NOTHING RETURNING id`, email, first, last).Scan(&userID)
	if err == sql.ErrNoRows {
		if err = q.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
			return "", false, err
		}
		return userID, false, nil
	}
	if err != nil {
		return "", false, err
	}
	return userID, true, nil
}

// addUserToOpenEvents links a (typically newly created) user to every event that
// is not yet past, so the default-everyone membership model holds: a new employee
// automatically appears on all open events' attendee lists. "Past" is evaluated
// in each event's own timezone, so this can't be a single SQL predicate. It is
// idempotent (ON CONFLICT DO NOTHING); because it only ever runs when the user is
// first created, it never resurrects someone an admin later removed.
func addUserToOpenEvents(ctx context.Context, q db.Exec, userID string, now time.Time) error {
	rows, err := q.QueryContext(ctx, `SELECT id, end_date, timezone FROM events`)
	if err != nil {
		return err
	}
	var openIDs []string
	for rows.Next() {
		var id, tz string
		var end time.Time
		if err := rows.Scan(&id, &end, &tz); err != nil {
			rows.Close()
			return err
		}
		loc, lerr := loadLocation(tz)
		if lerr != nil {
			loc = time.UTC
		}
		if !isEventPast(end, loc, now) {
			openIDs = append(openIDs, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, eid := range openIDs {
		if _, err := q.ExecContext(ctx,
			`INSERT INTO event_attendees (event_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			eid, userID); err != nil {
			return err
		}
	}
	return nil
}

// seedAllUsersAsAttendees links every existing directory user to the event. Used
// at event creation so the whole company is an attendee by default. Idempotent.
func seedAllUsersAsAttendees(ctx context.Context, q db.Exec, eventID string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO event_attendees (event_id, user_id)
		 SELECT $1, id FROM users WHERE NOT archived ON CONFLICT DO NOTHING`, eventID)
	return err
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
		detail := map[string]any{"email": email}
		if err := a.logActivity(r.Context(), tx, eventID, &actorID, actor.Email, email, actionAttendeeAdded, summary, detail, false); err != nil {
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
		detail := map[string]any{"email": email}
		if err := a.logActivity(r.Context(), tx, eventID, &actorID, actor.Email, email, actionAttendeeRemoved, summary, detail, false); err != nil {
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
