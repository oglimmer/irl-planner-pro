package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"irlplanner/internal/metrics"
	"irlplanner/internal/workspaceauth"
)

// oidcRuntime holds the OIDC provider/verifier/oauth2 config resolved at
// startup. Populated by InitOIDC; nil in password/dev mode.
type oidcRuntime struct {
	provider           *oidc.Provider
	verifier           *oidc.IDTokenVerifier
	oauth2             *oauth2.Config
	endSessionEndpoint string // empty when the IdP's discovery doc advertises none
}

const (
	oidcStateCookie   = "oidc_state"
	oidcNonceCookie   = "oidc_nonce"
	oidcIDTokenCookie = "oidc_id_token"
	oidcIDTokenMaxAge = 30 * 24 * 3600
)

// OIDC sign-in failure reason codes. They travel to the SPA as
// /auth/callback#error=<code>; OIDCCallbackView maps each to user-facing copy.
const (
	oidcErrProvider = "provider_error"     // protocol / IdP / transient — retryable
	oidcErrDomain   = "domain_not_allowed" // Workspace-domain allowlist rejection
	oidcErrAccount  = "account_error"      // unexpected provisioning failure
)

func (a *App) InitOIDC(ctx context.Context) error {
	if a.Cfg.OIDCIssuerURL == "" || a.Cfg.OIDCClientID == "" || a.Cfg.OIDCClientSecret == "" {
		return errors.New("OIDC_ISSUER_URL, OIDC_CLIENT_ID and OIDC_CLIENT_SECRET are required when AUTH_MODE=oidc")
	}
	provider, err := oidc.NewProvider(ctx, a.Cfg.OIDCIssuerURL)
	if err != nil {
		return fmt.Errorf("discover provider: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: a.Cfg.OIDCClientID})
	scopes := strings.Fields(a.Cfg.OIDCScopes)
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}
	var extra struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&extra)

	a.OIDC = &oidcRuntime{
		provider: provider,
		verifier: verifier,
		oauth2: &oauth2.Config{
			ClientID:     a.Cfg.OIDCClientID,
			ClientSecret: a.Cfg.OIDCClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  a.Cfg.OIDCRedirectURL,
			Scopes:       scopes,
		},
		endSessionEndpoint: extra.EndSessionEndpoint,
	}
	return nil
}

func (a *App) setShortLivedCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.Cfg.PublicBaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
}

func (a *App) setOIDCSessionCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.Cfg.PublicBaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   oidcIDTokenMaxAge,
	})
}

func clearOIDCCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func (a *App) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randHex(16)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "state error")
		return
	}
	nonce, err := randHex(16)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "nonce error")
		return
	}
	a.setShortLivedCookie(w, oidcStateCookie, state)
	a.setShortLivedCookie(w, oidcNonceCookie, nonce)
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce)}
	// UI hint only: when exactly one Workspace domain is configured, ask Google
	// to pre-filter the account chooser. Backend validation is authoritative.
	if len(a.Cfg.AllowedGoogleWorkspaceDomains) == 1 {
		opts = append(opts, oauth2.SetAuthURLParam("hd", a.Cfg.AllowedGoogleWorkspaceDomains[0]))
	}
	http.Redirect(w, r, a.OIDC.oauth2.AuthCodeURL(state, opts...), http.StatusFound)
}

type oidcClaims struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
	Nonce         string `json:"nonce"`
	HD            string `json:"hd"` // Google Workspace hosted-domain claim
}

func (a *App) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(oidcStateCookie)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}
	nonceCookie, err := r.Cookie(oidcNonceCookie)
	if err != nil || nonceCookie.Value == "" {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}
	clearOIDCCookie(w, oidcStateCookie)
	clearOIDCCookie(w, oidcNonceCookie)

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		log.Printf("INFO: oidc provider returned error=%q", errParam)
		a.oidcFail(w, r, oidcErrProvider)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}

	tok, err := a.OIDC.oauth2.Exchange(r.Context(), code)
	if err != nil {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}
	idToken, err := a.OIDC.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}
	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}
	if claims.Nonce != nonceCookie.Value || claims.Sub == "" {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}

	// Enforce the Google Workspace domain allowlist. Generic message so the
	// configured domains aren't leaked.
	if err := workspaceauth.ValidateGoogleHD(idToken.Issuer, claims.HD, a.Cfg.AllowedGoogleWorkspaceDomains); err != nil {
		log.Printf("WARN: oidc workspace domain rejected: hd=%q email=%q sub=%q issuer=%q",
			claims.HD, claims.Email, claims.Sub, idToken.Issuer)
		a.oidcFail(w, r, oidcErrDomain)
		return
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}
	// Require a verified email: we key accounts on email, so an unverified one
	// would let anyone who can set that address at the IdP take over. Google
	// always sets email_verified.
	if claims.EmailVerified != nil && !*claims.EmailVerified {
		log.Printf("WARN: oidc email not verified: email=%q", email)
		a.oidcFail(w, r, oidcErrDomain)
		return
	}

	user, err := a.findOrCreateUser(r.Context(), email, claims.Name)
	if err != nil {
		log.Printf("ERROR: oidc user provisioning: %v", err)
		a.oidcFail(w, r, oidcErrAccount)
		return
	}

	metrics.LoginsTotal.WithLabelValues("oidc", "success").Inc()

	jwtStr, err := a.issueToken(user.ID, user.TokenVersion)
	if err != nil {
		a.oidcFail(w, r, oidcErrProvider)
		return
	}
	a.setOIDCSessionCookie(w, oidcIDTokenCookie, rawIDToken)
	a.redirectToSPAWithSession(w, r, jwtStr, user)
}

// redirectToSPAWithSession forwards the browser to the SPA callback with the
// session token + user payload in the URL fragment (never sent to the server).
func (a *App) redirectToSPAWithSession(w http.ResponseWriter, r *http.Request, jwtStr string, user *User) {
	userJSON, _ := json.Marshal(user)
	frag := url.Values{}
	frag.Set("token", jwtStr)
	frag.Set("user", base64.RawURLEncoding.EncodeToString(userJSON))
	dest := strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/auth/callback#" + frag.Encode()
	http.Redirect(w, r, dest, http.StatusFound)
}

func (a *App) oidcFail(w http.ResponseWriter, r *http.Request, msg string) {
	metrics.LoginsTotal.WithLabelValues("oidc", "failure").Inc()
	dest := strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/auth/callback#error=" + url.QueryEscape(msg)
	http.Redirect(w, r, dest, http.StatusFound)
}

// handleOIDCLogout drives RP-initiated logout: clear the cached id_token, and
// if the IdP advertised an end_session_endpoint, bounce through it; otherwise
// fall back to /login.
func (a *App) handleOIDCLogout(w http.ResponseWriter, r *http.Request) {
	loginURL := strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/login"

	var idTokenHint string
	if c, err := r.Cookie(oidcIDTokenCookie); err == nil {
		idTokenHint = c.Value
	}
	clearOIDCCookie(w, oidcIDTokenCookie)

	if a.OIDC == nil || a.OIDC.endSessionEndpoint == "" {
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}
	u, err := url.Parse(a.OIDC.endSessionEndpoint)
	if err != nil {
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}
	q := u.Query()
	q.Set("post_logout_redirect_uri", loginURL)
	q.Set("client_id", a.Cfg.OIDCClientID)
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
