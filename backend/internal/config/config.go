// Package config loads and validates process configuration from environment
// variables. It is the only place getenv-style defaults live.
package config

import (
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultJWTSecret is the placeholder used when JWT_SECRET is unset. Load()
// rejects it (and any secret shorter than minJWTSecretLen) at startup unless
// ALLOW_INSECURE_JWT_SECRET=true, so a real deployment can never silently sign
// tokens with a value that is published in this source tree.
const defaultJWTSecret = "change-me-please-use-32-chars-minimum"

// minJWTSecretLen is the smallest accepted JWT_SECRET. `openssl rand -hex 32`
// yields 64 chars and is the documented way to generate one.
const minJWTSecretLen = 32

type Config struct {
	DatabaseURL   string
	JWTSecret     string
	ListenAddr    string
	PublicBaseURL string

	// AllowedOrigins is the CORS allowlist for browser cross-origin requests.
	// Derived from CORS_ALLOWED_ORIGINS when set; otherwise from PublicBaseURL —
	// permissive ("*") for localhost dev so the Vite dev server can reach the
	// API, locked to the app's own origin in production.
	AllowedOrigins []string

	AuthMode string // "oidc" (only supported in production); "password" is dev-stub only

	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string // defaults to PublicBaseURL + "/api/auth/oidc/callback"
	OIDCScopes       string // space-separated; defaults to "openid email profile"

	// AllowedGoogleWorkspaceDomains restricts Google sign-in to ID tokens whose
	// `hd` claim is in this list. Only applied when the issuer is Google.
	AllowedGoogleWorkspaceDomains []string

	// PeopleTeamEmail receives "submission changed" notifications and the daily
	// activity digest.
	PeopleTeamEmail string

	// DefaultEventTimezone is the IANA tz pre-filled when creating a new event.
	DefaultEventTimezone string

	// Reminders / digest scheduler.
	RemindersEnabled     bool
	ReminderTickInterval time.Duration

	// MetricsToken, when non-empty, gates /metrics with Bearer auth.
	MetricsToken string

	// SMTP settings for outbound email. Empty SMTPHost disables all email.
	SMTPHost        string
	SMTPPort        int
	SMTPUsername    string
	SMTPPassword    string
	SMTPFrom        string
	SMTPUseTLS      bool // STARTTLS upgrade (typically port 587)
	SMTPImplicitTLS bool // implicit TLS / SMTPS (typically port 465, e.g. Fastmail)

	// MCP OAuth 2.1 (Phase 7). Both set → /mcp + OAuth enabled; both empty → off.
	MCPOAuthClientID     string
	MCPOAuthClientSecret string
	MCPOAuthRedirectURIs []string
}

// MCPEnabled reports whether the Phase 7 MCP surface should be wired up.
func (c Config) MCPEnabled() bool {
	return c.MCPOAuthClientID != "" && c.MCPOAuthClientSecret != ""
}

func Load() Config {
	c := Config{
		DatabaseURL:   getenv("DATABASE_URL", "postgres://irl:irl@localhost:5432/irl?sslmode=disable"),
		JWTSecret:     getenv("JWT_SECRET", defaultJWTSecret),
		ListenAddr:    getenv("LISTEN_ADDR", ":8080"),
		PublicBaseURL: getenv("PUBLIC_BASE_URL", "http://localhost:8080"),

		AuthMode: strings.ToLower(getenv("AUTH_MODE", "oidc")),

		OIDCIssuerURL:    strings.TrimRight(getenv("OIDC_ISSUER_URL", ""), "/"),
		OIDCClientID:     getenv("OIDC_CLIENT_ID", ""),
		OIDCClientSecret: getenv("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  getenv("OIDC_REDIRECT_URL", ""),
		OIDCScopes:       getenv("OIDC_SCOPES", "openid email profile"),

		AllowedGoogleWorkspaceDomains: parseList(getenv("OIDC_GOOGLE_WORKSPACE_DOMAINS", ""), true),

		PeopleTeamEmail:      strings.TrimSpace(getenv("PEOPLE_TEAM_EMAIL", "")),
		DefaultEventTimezone: getenv("DEFAULT_EVENT_TIMEZONE", "Europe/Paris"),

		RemindersEnabled:     getenv("REMINDERS_ENABLED", "true") != "false",
		ReminderTickInterval: parseDuration(getenv("REMINDER_TICK_INTERVAL", "1h"), time.Hour),

		MetricsToken: getenv("METRICS_TOKEN", ""),

		SMTPHost:        strings.TrimSpace(getenv("SMTP_HOST", "")),
		SMTPPort:        parseInt(getenv("SMTP_PORT", "587"), 587),
		SMTPUsername:    getenv("SMTP_USERNAME", ""),
		SMTPPassword:    getenv("SMTP_PASSWORD", ""),
		SMTPFrom:        strings.TrimSpace(getenv("SMTP_FROM", "")),
		SMTPUseTLS:      getenv("SMTP_USE_TLS", "true") == "true",
		SMTPImplicitTLS: getenv("SMTP_IMPLICIT_TLS", "false") == "true",

		MCPOAuthClientID:     getenv("MCP_OAUTH_CLIENT_ID", ""),
		MCPOAuthClientSecret: getenv("MCP_OAUTH_CLIENT_SECRET", ""),
		MCPOAuthRedirectURIs: parseList(getenv("MCP_OAUTH_REDIRECT_URIS",
			"https://claude.ai/api/mcp/auth_callback"), false),
	}

	if c.AuthMode != "oidc" && c.AuthMode != "password" {
		log.Fatalf("AUTH_MODE must be 'oidc' or 'password', got %q", c.AuthMode)
	}
	if c.AuthMode == "password" {
		log.Printf("WARN: AUTH_MODE=password is a local-dev stub with no real auth — set AUTH_MODE=oidc for any shared deployment")
	}
	// Refuse to boot with a forgeable JWT signing key.
	if insecureJWTSecret(c.JWTSecret) {
		if getenv("ALLOW_INSECURE_JWT_SECRET", "") != "true" {
			log.Fatalf("JWT_SECRET is the in-repo default or shorter than %d characters — set a unique value generated with `openssl rand -hex 32`. For local development only, set ALLOW_INSECURE_JWT_SECRET=true.", minJWTSecretLen)
		}
		log.Printf("WARN: JWT_SECRET is insecure (default or under %d chars) — permitted only because ALLOW_INSECURE_JWT_SECRET=true; never do this in production", minJWTSecretLen)
	}
	if c.OIDCRedirectURL == "" {
		c.OIDCRedirectURL = strings.TrimRight(c.PublicBaseURL, "/") + "/api/auth/oidc/callback"
	}
	if c.AuthMode == "oidc" && len(c.AllowedGoogleWorkspaceDomains) == 0 {
		log.Printf("WARN: AUTH_MODE=oidc but OIDC_GOOGLE_WORKSPACE_DOMAINS is empty — Google Workspace domain restriction is disabled")
	}
	if (c.MCPOAuthClientID == "") != (c.MCPOAuthClientSecret == "") {
		log.Fatalf("MCP_OAUTH_CLIENT_ID and MCP_OAUTH_CLIENT_SECRET must both be set or both be empty")
	}
	c.AllowedOrigins = deriveAllowedOrigins(c.PublicBaseURL, getenv("CORS_ALLOWED_ORIGINS", ""))
	if len(c.AllowedOrigins) == 1 && c.AllowedOrigins[0] == "*" {
		log.Printf("WARN: CORS allows any origin (*) — set CORS_ALLOWED_ORIGINS or a non-localhost PUBLIC_BASE_URL to lock this down in production")
	}
	return c
}

// insecureJWTSecret reports whether a JWT signing secret is unsafe to sign real
// tokens with — i.e. it's the in-repo default or shorter than minJWTSecretLen.
func insecureJWTSecret(s string) bool {
	return s == defaultJWTSecret || len(s) < minJWTSecretLen
}

// deriveAllowedOrigins builds the CORS allowlist. An explicit
// CORS_ALLOWED_ORIGINS always wins. Otherwise localhost/loopback gets "*" so the
// Vite dev server can call the API, while a real host is locked to its origin.
func deriveAllowedOrigins(publicBaseURL, override string) []string {
	if o := parseList(override, false); len(o) > 0 {
		return o
	}
	u, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || u.Host == "" {
		return []string{"*"}
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return []string{"*"}
	}
	return []string{u.Scheme + "://" + u.Host}
}

func parseDuration(s string, def time.Duration) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		log.Printf("WARN: invalid duration %q, using %s", s, def)
		return def
	}
	return d
}

func parseInt(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Printf("WARN: invalid integer %q, using %d", s, def)
		return def
	}
	return n
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseList splits a comma-separated list, trimming whitespace and dropping
// empties. When lower is true each item is lower-cased (for domains); URIs keep
// their case.
func parseList(s string, lower bool) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if lower {
			p = strings.ToLower(p)
		}
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
