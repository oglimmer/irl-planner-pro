package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"

	"irlplanner/internal/metrics"
)

// NewRouter wires every route the backend exposes onto a chi router.
func NewRouter(app *App) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(skipLogger("/healthz", "/readyz"))
	r.Use(middleware.Recoverer)
	r.Use(metrics.HTTPMiddleware)
	r.Use(securityHeaders)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: app.Cfg.AllowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		// Authorization + Content-Type cover the REST API. The Mcp-* and
		// Last-Event-ID request headers are sent by browser-based MCP Streamable
		// HTTP clients (e.g. the MCP Inspector); without them the CORS preflight
		// for /mcp fails even when the origin is allowed.
		AllowedHeaders: []string{
			"Authorization", "Content-Type", "Accept",
			"Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID",
		},
		// Mcp-Session-Id must be readable by MCP clients so they can carry a
		// session across requests.
		ExposedHeaders:   []string{"Link", "Mcp-Session-Id"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Consistent fallbacks: JSON for API clients, friendly HTML for browsers.
	r.NotFound(app.handleNotFound)
	r.MethodNotAllowed(app.handleMethodNotAllowed)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !app.IsReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("starting"))
			return
		}
		w.Write([]byte("ok"))
	})
	r.Method("GET", "/metrics", metrics.Handler(app.Cfg.MetricsToken))

	// Phase 7 — MCP server + OAuth 2.1. Mounted only when MCP_OAUTH_CLIENT_* are
	// configured, so an opt-out deployment exposes none of this surface. The /mcp
	// gate accepts the OAuth mcp_access bearer token (not the SPA's JWT); tool
	// handlers enforce admin authorization themselves.
	if app.Cfg.MCPEnabled() {
		r.Group(func(r chi.Router) {
			r.Use(app.mcpTokenGateMiddleware)
			r.Mount("/mcp", app.mcpHandler())
		})

		// Discovery is cheap JSON read by clients during setup — left unthrottled.
		r.Get("/.well-known/oauth-authorization-server", app.handleOAuthMeta)
		r.Get("/.well-known/oauth-protected-resource", app.handleOAuthProtectedResource)
		r.Get("/.well-known/oauth-protected-resource/mcp", app.handleOAuthProtectedResource)

		// authorize/token handle auth codes, refresh tokens, and the client
		// secret, so throttle them per client IP (RealIP-keyed). 60/min is far
		// above any real MCP client but caps abusive loops; volumetric DoS stays
		// the edge's job.
		r.Group(func(r chi.Router) {
			r.Use(httprate.LimitByIP(60, time.Minute))
			r.Get("/oauth/authorize", app.handleOAuthAuthorize)
			r.Post("/oauth/authorize", app.handleOAuthAuthorizeSubmit)
			r.Post("/oauth/token", app.handleOAuthToken)
		})
	}

	r.Route("/api", func(r chi.Router) {
		r.Get("/version", app.handleVersion)
		r.Get("/auth/config", app.handleAuthConfig)

		// Event cover image. Public (no auth) so a plain <img src> can load it,
		// and ETag-cached. Slugs are already shareable, non-secret links, and the
		// image is non-sensitive event metadata.
		r.Get("/events/{slug}/image", app.handleGetEventImage)

		// Sign-in endpoints. Per-IP throttle blunts credential-stuffing/abuse;
		// 60/min stays clear of an org's shared-egress-IP login surge.
		r.Group(func(r chi.Router) {
			r.Use(httprate.LimitByIP(60, time.Minute))
			switch app.Cfg.AuthMode {
			case "oidc":
				r.Get("/auth/oidc/login", app.handleOIDCLogin)
				r.Get("/auth/oidc/callback", app.handleOIDCCallback)
				r.Get("/auth/oidc/logout", app.handleOIDCLogout)
			case "password":
				r.Post("/auth/dev-login", app.handleDevLogin)
			}
		})

		// Authenticated API.
		r.Group(func(r chi.Router) {
			r.Use(app.authMiddleware)
			r.Get("/me", app.handleMe)
			r.Put("/me", app.handleUpdateMe)

			// Attendee-facing. Any signed-in user (everyone is invited).
			r.Get("/active-events", app.handleListCurrentEvents)
			r.Get("/events/{slug}", app.handleGetEventBySlug)
			r.Get("/events/{slug}/submission", app.handleGetMySubmission)
			r.Put("/events/{slug}/submission", app.handlePutMySubmission)
			r.Get("/events/{slug}/activity", app.handleMyActivity)

			// Admin-only.
			r.Group(func(r chi.Router) {
				r.Use(app.requireAdminMiddleware)
				r.Get("/users", app.handleListUsers)
				r.Post("/users/{id}/promote", app.handlePromoteUser)
				r.Post("/users/{id}/demote", app.handleDemoteUser)
				r.Post("/users/{id}/archive", app.handleArchiveUser)
				r.Post("/users/{id}/unarchive", app.handleUnarchiveUser)

				// Send a one-off test notification over a single channel
				// ("email"/"slack") to the calling admin — a config smoke test.
				r.Post("/notifications/test/{channel}", app.handleSendTestNotification)

				// Event management is namespaced under /admin so the id-keyed
				// admin routes don't collide with the slug-keyed attendee read.
				r.Get("/admin/events", app.handleListEvents)
				r.Post("/admin/events", app.handleCreateEvent)
				r.Get("/admin/events/{id}", app.handleGetEvent)
				r.Put("/admin/events/{id}", app.handleUpdateEvent)
				r.Post("/admin/events/{id}/image", app.handleUploadEventImage)
				r.Delete("/admin/events/{id}/image", app.handleDeleteEventImage)
				r.Get("/admin/events/{id}/activity", app.handleEventActivity)
				r.Put("/admin/events/{id}/submissions/{userId}", app.handleAdminUpdateSubmission)

				// Attendees (event membership), dashboard, export. Attendees are
				// company-directory users; importing provisions them, and an RSVP
				// auto-adds its author — see attendees.go / submissions.go.
				r.Post("/admin/events/{id}/attendees", app.handleImportAttendees)
				r.Post("/admin/events/{id}/attendees/{userId}", app.handleAddAttendee)
				r.Delete("/admin/events/{id}/attendees/{userId}", app.handleRemoveAttendee)
				r.Get("/admin/events/{id}/dashboard", app.handleDashboard)
				r.Get("/admin/events/{id}/submissions", app.handleListSubmissions)
				r.Get("/admin/events/{id}/export.csv", app.handleExportCSV)

				// Financial tab: every attendee's declared travel cost converted
				// to USD/GBP/EUR via live Frankfurter FX rates (see financial.go).
				r.Get("/admin/events/{id}/financial", app.handleFinancial)

				// Messaging tab: editable invite/reminder templates, an
				// admin-pressed invitation to all attendees, and a manual
				// follow-up to current non-responders. Dispatches over email
				// (SMTP) or Slack bot DMs (see messaging.go / internal/slack).
				r.Get("/admin/events/{id}/messaging", app.handleGetMessaging)
				r.Put("/admin/events/{id}/messaging", app.handleSaveMessaging)
				r.Post("/admin/events/{id}/messaging/invite", app.handleSendInvitation)
				r.Post("/admin/events/{id}/messaging/followup", app.handleSendFollowup)

				// Per-event notification matrix: the People-team daily-summary
				// toggle plus each admin's stream + channel preferences
				// (see notifications.go).
				r.Get("/admin/events/{id}/notifications", app.handleGetNotifications)
				r.Put("/admin/events/{id}/notifications", app.handleSaveNotifications)
			})
		})
	})

	return r
}

// securityHeaders adds defense-in-depth response headers to every backend
// response. The backend only ever emits JSON or self-contained HTML (error
// pages, and the Phase 7 OAuth login form), which load no scripts; inline
// styles are allowed for those pages. HSTS is left to the TLS edge.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; img-src 'self' data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// skipLogger wraps chi's request logger so health/readiness probes don't spam logs.
func skipLogger(skipPaths ...string) func(http.Handler) http.Handler {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = struct{}{}
	}
	logger := middleware.Logger
	return func(next http.Handler) http.Handler {
		logged := logger(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := skip[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}
			logged.ServeHTTP(w, r)
		})
	}
}
