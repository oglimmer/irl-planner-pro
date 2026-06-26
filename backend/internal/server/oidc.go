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

// initiateOIDCFlow generates a nonce, sets the OIDC state/nonce cookies, and
// redirects the browser to the OIDC provider. The state value is caller-supplied
// so both the normal login path (plain hex) and the Phase 7 OAuth-initiated path
// ("oauth:<key>") share the same redirect machinery.
func (a *App) initiateOIDCFlow(w http.ResponseWriter, r *http.Request, state string) error {
	nonce, err := randHex(16)
	if err != nil {
		return err
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
	return nil
}

func (a *App) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randHex(16)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "state error")
		return
	}
	if err := a.initiateOIDCFlow(w, r, state); err != nil {
		writeErr(w, http.StatusInternalServerError, "oidc init error")
	}
}

type oidcClaims struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`  // from the `profile` scope
	FamilyName    string `json:"family_name"` // from the `profile` scope
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

	// Phase 7: detect an OAuth-initiated flow and atomically consume the pending
	// OAuth context early, so every downstream failure can redirect the OAuth
	// client correctly (rather than bouncing to the SPA).
	var pending *oauthPendingRow
	if strings.HasPrefix(stateCookie.Value, "oauth:") {
		stateKey := strings.TrimPrefix(stateCookie.Value, "oauth:")
		pending, err = a.loadAndDeleteOAuthPending(r.Context(), stateKey)
		if err != nil || pending == nil {
			a.oidcFail(w, r, oidcErrProvider)
			return
		}
	}

	clearOIDCCookie(w, oidcStateCookie)
	clearOIDCCookie(w, oidcNonceCookie)

	// fail routes a reason code to the OAuth client when in an OAuth flow,
	// otherwise to the SPA callback. Policy/identity reasons map to OAuth
	// access_denied; everything else is server_error.
	fail := func(reason string) {
		if pending != nil {
			metrics.LoginsTotal.WithLabelValues("oidc", "failure").Inc()
			oauthErr := "server_error"
			if reason == oidcErrDomain {
				oauthErr = "access_denied"
			}
			oauthRedirectErr(w, r, pending.RedirectURI, pending.OAuthState, oauthErr, reason)
			return
		}
		a.oidcFail(w, r, reason) // increments the oidc failure metric internally
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		log.Printf("INFO: oidc provider returned error=%q", errParam)
		fail(oidcErrProvider)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		fail(oidcErrProvider)
		return
	}

	tok, err := a.OIDC.oauth2.Exchange(r.Context(), code)
	if err != nil {
		fail(oidcErrProvider)
		return
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		fail(oidcErrProvider)
		return
	}
	idToken, err := a.OIDC.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		fail(oidcErrProvider)
		return
	}
	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		fail(oidcErrProvider)
		return
	}
	if claims.Nonce != nonceCookie.Value || claims.Sub == "" {
		fail(oidcErrProvider)
		return
	}

	// Enforce the Google Workspace domain allowlist. Generic message so the
	// configured domains aren't leaked.
	if err := workspaceauth.ValidateGoogleHD(idToken.Issuer, claims.HD, a.Cfg.AllowedGoogleWorkspaceDomains); err != nil {
		log.Printf("WARN: oidc workspace domain rejected: hd=%q email=%q sub=%q issuer=%q",
			claims.HD, claims.Email, claims.Sub, idToken.Issuer)
		fail(oidcErrDomain)
		return
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		fail(oidcErrProvider)
		return
	}
	// Require a verified email: we key accounts on email, so an unverified one
	// would let anyone who can set that address at the IdP take over. Google
	// always sets email_verified.
	if claims.EmailVerified != nil && !*claims.EmailVerified {
		log.Printf("WARN: oidc email not verified: email=%q", email)
		fail(oidcErrDomain)
		return
	}

	// Prefer the split given/family names from the `profile` scope; fall back to
	// splitting the combined `name` (some IdPs, e.g. Keycloak, may send only that).
	first, last := strings.TrimSpace(claims.GivenName), strings.TrimSpace(claims.FamilyName)
	if first == "" && last == "" {
		first, last = splitName(claims.Name)
	}
	user, err := a.findOrCreateUser(r.Context(), email, first, last, "")
	if err != nil {
		log.Printf("ERROR: oidc user provisioning: %v", err)
		fail(oidcErrAccount)
		return
	}

	metrics.LoginsTotal.WithLabelValues("oidc", "success").Inc()

	if pending != nil {
		// OAuth-initiated flow: issue an authorization code and redirect back to
		// the OAuth client's redirect_uri instead of forwarding to the SPA.
		authCode, err := a.issueAuthCode(r.Context(), user.ID, pending.RedirectURI, pending.CodeChallenge)
		if err != nil {
			log.Printf("ERROR: issue auth code (oauth/oidc): %v", err)
			oauthRedirectErr(w, r, pending.RedirectURI, pending.OAuthState, "server_error", "auth code issuance failed")
			return
		}
		q := url.Values{"code": {authCode}}
		if pending.OAuthState != "" {
			q.Set("state", pending.OAuthState)
		}
		http.Redirect(w, r, redirectWithParams(pending.RedirectURI, q), http.StatusFound)
		return
	}

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
