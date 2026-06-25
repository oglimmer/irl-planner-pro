package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// findOrCreateUser upserts a user by email. The first user ever provisioned is
// made admin (decided in SQL via NOT EXISTS so concurrent first logins can't
// both win). The name is seeded from the IdP only when the account is first
// created; on subsequent logins it is left untouched so a user's own profile
// edit is never clobbered.
func (a *App) findOrCreateUser(ctx context.Context, email, firstName, lastName string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)

	u := &User{}
	err := a.DB.QueryRowContext(ctx,
		`SELECT id, email, first_name, last_name, is_admin, created_at, token_version FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.IsAdmin, &u.CreatedAt, &u.TokenVersion)
	if err == nil {
		// Existing user: keep whatever name they (or a prior login) already have.
		u.setDisplayName()
		return u, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// ON CONFLICT leaves an existing row's names untouched (a concurrent first
	// login that lost the race must not overwrite the winner's seeded name).
	err = a.DB.QueryRowContext(ctx,
		`INSERT INTO users (email, first_name, last_name, is_admin)
		 VALUES ($1, $2, $3, NOT EXISTS (SELECT 1 FROM users))
		 ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		 RETURNING id, email, first_name, last_name, is_admin, created_at, token_version`,
		email, firstName, lastName).
		Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.IsAdmin, &u.CreatedAt, &u.TokenVersion)
	if err != nil {
		return nil, err
	}
	u.setDisplayName()
	return u, nil
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

// updateMeReq is the self-service profile edit payload.
type updateMeReq struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// handleUpdateMe lets a signed-in user edit their own display name (first/last).
// Both parts are required; the name is otherwise free-form.
func (a *App) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req updateMeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	if req.FirstName == "" || req.LastName == "" {
		writeErr(w, http.StatusBadRequest, "first name and last name are required")
		return
	}
	if _, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET first_name = $1, last_name = $2 WHERE id = $3`,
		req.FirstName, req.LastName, user.ID); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	user.FirstName = req.FirstName
	user.LastName = req.LastName
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
	CreatedAt string `json:"createdAt"`
}

func (a *App) handleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT id, email, first_name, last_name, is_admin, created_at FROM users ORDER BY created_at`)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer rows.Close()
	out := []UserSummary{}
	for rows.Next() {
		var u UserSummary
		var created sql.NullTime
		if err := rows.Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.IsAdmin, &created); err != nil {
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
	user, err := a.findOrCreateUser(r.Context(), req.Email, first, last)
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
