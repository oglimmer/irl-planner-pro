package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// findOrCreateUser upserts a user by email. The first user ever provisioned is
// made admin (decided in SQL via NOT EXISTS so concurrent first logins can't
// both win). The name and allergies are seeded only when the account is first
// created (from the IdP / first-login form); on subsequent logins they are left
// untouched so a user's own profile edit is never clobbered.
func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

// updateMeReq is the self-service profile edit payload.
type updateMeReq struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	// Allergies / dietary preferences — free-form and optional (may be cleared).
	Allergies string `json:"allergies"`
}

// handleUpdateMe lets a signed-in user edit their own profile: display name
// (first/last, both required) and allergies/dietary preferences (free-form).
func (a *App) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req updateMeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Allergies = strings.TrimSpace(req.Allergies)
	if req.FirstName == "" || req.LastName == "" {
		writeErr(w, http.StatusBadRequest, "first name and last name are required")
		return
	}
	// Saving the profile also marks it confirmed: this is the action behind the
	// first-login confirm step, and is harmless (idempotent) on later edits.
	if _, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET first_name = $1, last_name = $2, allergies = $3, profile_confirmed = true WHERE id = $4`,
		req.FirstName, req.LastName, req.Allergies, user.ID); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	user.FirstName = req.FirstName
	user.LastName = req.LastName
	user.Allergies = req.Allergies
	user.ProfileConfirmed = true
	user.setDisplayName()
	writeJSON(w, http.StatusOK, user)
}

// UserSummary is the admin user-list row. Name is the derived display name.
type UserSummary struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Name      string `json:"name"`
	IsAdmin   bool   `json:"isAdmin"`
	Archived  bool   `json:"archived"`
	CreatedAt string `json:"createdAt"`
}

func (a *App) handleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT id, email, first_name, last_name, is_admin, archived, created_at FROM users ORDER BY created_at`)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer rows.Close()
	out := []UserSummary{}
	for rows.Next() {
		var u UserSummary
		var created sql.NullTime
		if err := rows.Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.IsAdmin, &u.Archived, &created); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
		u.Name = strings.TrimSpace(u.FirstName + " " + u.LastName)
		if created.Valid {
			u.CreatedAt = created.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handlePromoteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET is_admin = true WHERE id = $1`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDemoteUser revokes admin, refusing to demote the last remaining admin so
// the deployment is never left without one. The guard runs in a single UPDATE
// whose WHERE clause requires another admin to exist, making it race-safe.
func (a *App) handleDemoteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Distinguish "no such admin" from "would remove the last admin": check the
	// target is currently an admin first.
	var isAdmin bool
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT is_admin FROM users WHERE id = $1`, id).Scan(&isAdmin)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if !isAdmin {
		w.WriteHeader(http.StatusNoContent) // already not an admin — idempotent
		return
	}

	res, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET is_admin = false
		 WHERE id = $1 AND EXISTS (SELECT 1 FROM users WHERE is_admin AND id <> $1)`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusConflict, "cannot demote the last admin")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleArchiveUser archives a user: they keep their account and event
// memberships but are excluded from every event activity (attendee seeding,
// reminders, dashboards, exports, admin notifications) until reactivated. An
// admin cannot archive themselves, which would silently strip their own access.
func (a *App) handleArchiveUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == currentUser(r).ID {
		writeErr(w, http.StatusConflict, "you cannot archive yourself")
		return
	}
	res, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET archived = true WHERE id = $1`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnarchiveUser reactivates an archived user. Because archiving never
// unlinks membership rows, reactivating restores the user across every event at
// once.
func (a *App) handleUnarchiveUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET archived = false WHERE id = $1`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// splitName best-effort splits a single display name into first/last: everything
// before the first space is the first name, the remainder the last. Used when an
// IdP (or the dev login) supplies only a combined `name`.
func splitName(name string) (first, last string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	if i := strings.IndexByte(name, ' '); i >= 0 {
		return name[:i], strings.TrimSpace(name[i+1:])
	}
	return name, ""
}

// --- dev-only password-mode login ------------------------------------------

type devLoginReq struct {
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	// Allergies / dietary preferences, seeded onto the profile on account creation.
	Allergies string `json:"allergies"`
	// Name is a convenience for callers that only have a single name string; it
	// is split into first/last when firstName/lastName aren't supplied.
	Name string `json:"name"`
}

// handleDevLogin is a local-development stub (AUTH_MODE=password) that mints a
// session for any email without a real credential check. It is never wired up
// in oidc mode. Lets `docker compose up` work without a Google client.
func (a *App) handleDevLogin(w http.ResponseWriter, r *http.Request) {
	var req devLoginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(req.Email, "@") {
		writeErr(w, http.StatusBadRequest, "invalid email")
		return
	}
	first, last := req.FirstName, req.LastName
	if first == "" && last == "" {
		first, last = splitName(req.Name)
	}
	user, err := a.Store.findOrCreateUser(r.Context(), req.Email, first, last, req.Allergies)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	tok, err := a.issueToken(user.ID, user.TokenVersion)
	if err != nil {
		serverErr(w, r, err, "token error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"token": tok, "user": user})
}
