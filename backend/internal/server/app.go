// Package server hosts the App-coupled HTTP layer: handlers, middleware, and
// the data structures that flow between them. Everything in this package shares
// the *App receiver (cfg + db + optional oidc runtime).
package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"irlplanner/internal/config"
	"irlplanner/internal/email"
	"irlplanner/internal/slack"
)

type App struct {
	Cfg  config.Config
	DB   *sql.DB      // raw pool; used directly for some queries and all tx work
	OIDC *oidcRuntime // populated only when Cfg.AuthMode == "oidc"

	// Store owns data-access helpers (see store.go for scope and rationale).
	// Callers use a.Store.* directly.
	Store *Store

	// Email sends outbound notifications (reminders, digests, admin alerts).
	// Its zero value is "not configured" — Send is a no-op guarded by callers.
	Email email.Sender

	// Slack is the Slack channel for the Messaging tab: bot DMs via the Slack
	// Web API. Configured() reports whether a bot token is set; when unset, Send
	// errors and the channel surfaces as "not configured" (see internal/slack).
	Slack slack.Notifier

	// ready gates the readiness probe. The backend is ready as soon as the DB
	// is migrated, so this flips true at startup.
	ready atomic.Bool
}

func (a *App) MarkReady()    { a.ready.Store(true) }
func (a *App) IsReady() bool { return a.ready.Load() }

// User is the authenticated principal. Provisioned on first OIDC login.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	// Name is the derived display name (first + last). Read-only: set from
	// FirstName/LastName when a row is scanned (see setDisplayName); clients edit
	// the two parts, never this field.
	Name string `json:"name"`
	// Allergies / dietary preferences are a property of the person, not any one
	// event, so they live on the profile (moved off submissions in migration
	// 0003_profile_allergies). Free-form and optional.
	Allergies string `json:"allergies"`
	// ProfileConfirmed is false until the user has reviewed the IdP-seeded name
	// and allergies once (via PUT /api/me). The SPA routes an unconfirmed user
	// through a one-time welcome/confirm step before anything else.
	ProfileConfirmed bool      `json:"profileConfirmed"`
	IsAdmin          bool      `json:"isAdmin"`
	CreatedAt        time.Time `json:"createdAt"`
	// TokenVersion is the session-revocation counter, stamped into JWTs as the
	// "ver" claim and compared on each request. Never serialised to clients.
	TokenVersion int `json:"-"`
}

// setDisplayName recomputes the derived Name from FirstName/LastName. Call after
// scanning a user row so the SPA header and the OIDC redirect payload have a
// ready display name without every caller re-joining the two parts.
func (u *User) setDisplayName() {
	u.Name = strings.TrimSpace(u.FirstName + " " + u.LastName)
}

type ctxKey string

const ctxUserKey ctxKey = "user"

// slugRe validates an event slug: a lowercase slug of 3–64 chars that starts
// and ends alphanumeric.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

// currentUser pulls the *User stashed by authMiddleware, or nil.
func currentUser(r *http.Request) *User {
	v, _ := r.Context().Value(ctxUserKey).(*User)
	return v
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// serverErr logs the underlying error tagged with the chi request ID, method,
// and route, then responds with a generic 500 so internal detail doesn't leak.
func serverErr(w http.ResponseWriter, r *http.Request, err error, publicMsg string) {
	log.Printf("ERROR reqID=%s %s %s: %s: %v",
		middleware.GetReqID(r.Context()), r.Method, r.URL.Path, publicMsg, err)
	writeErr(w, http.StatusInternalServerError, publicMsg)
}

// AuthConfig is the unauthenticated bootstrap payload the SPA reads to render
// the right sign-in UI and seed app-wide defaults.
type AuthConfig struct {
	Mode                 string `json:"mode"`
	DefaultEventTimezone string `json:"defaultEventTimezone"`
}

func (a *App) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, AuthConfig{
		Mode:                 a.Cfg.AuthMode,
		DefaultEventTimezone: a.Cfg.DefaultEventTimezone,
	})
}
