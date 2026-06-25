package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// tokenTypeMCPAccess is the "typ" claim stamped on OAuth access tokens issued to
// MCP clients (Phase 7). Session JWTs carry no "typ" and remain full-access; an
// mcp_access token is accepted only at the /mcp gate so a Claude-held OAuth
// token can't be replayed against regular /api routes.
const tokenTypeMCPAccess = "mcp_access"

const sessionTTL = 30 * 24 * time.Hour

// issueToken mints a 30-day browser session JWT. tokenVersion is the user's
// current revocation counter, stamped as "ver" so the session is rejected the
// moment the counter is bumped.
func (a *App) issueToken(userID string, tokenVersion int) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": userID,
		"ver": tokenVersion,
		"iat": now.Unix(),
		"exp": now.Add(sessionTTL).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(a.Cfg.JWTSecret))
}

// parseToken validates a JWT and returns its subject, "typ", and "ver" claim.
// typ is empty for ordinary session tokens and "mcp_access" for OAuth tokens.
func (a *App) parseToken(tok string) (sub, typ string, ver int, err error) {
	parsed, err := jwt.Parse(tok, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(a.Cfg.JWTSecret), nil
	})
	if err != nil {
		return "", "", 0, err
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return "", "", 0, errors.New("invalid token")
	}
	sub, _ = claims["sub"].(string)
	if sub == "" {
		return "", "", 0, errors.New("missing sub")
	}
	typ, _ = claims["typ"].(string)
	if v, ok := claims["ver"].(float64); ok {
		ver = int(v)
	}
	return sub, typ, ver, nil
}

// resolveToken resolves a session/MCP JWT to a user. It distinguishes "bad
// credential" (msg set, err nil) from "DB lookup failed" (msg empty, err set)
// so an intermittent backend hiccup isn't reported as a 401. An mcp_access JWT
// is rejected unless allowMCPScope is set.
func (a *App) resolveToken(ctx context.Context, tok string, allowMCPScope bool) (*User, string, error) {
	userID, typ, ver, err := a.parseToken(tok)
	if err != nil {
		return nil, "invalid token", nil
	}
	if typ == tokenTypeMCPAccess && !allowMCPScope {
		return nil, "token not valid for this endpoint", nil
	}
	u, err := a.userByID(ctx, userID)
	if err == sql.ErrNoRows {
		return nil, "unknown user", nil
	}
	if err != nil {
		return nil, "", err
	}
	// Session revocation: a token signed with a stale version is no longer valid.
	if ver != u.TokenVersion {
		return nil, "token revoked", nil
	}
	return u, "", nil
}

// authenticateRequest reads a Bearer JWT from the Authorization header.
// Returns (user, "", nil) on success; (nil, msg, nil) on credential failure
// (msg empty when no credential was presented); (nil, "", err) on backend error.
func (a *App) authenticateRequest(r *http.Request, allowMCPScope bool) (*User, string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return nil, "", nil
	}
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, "invalid authorization header", nil
	}
	tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	if tok == "" {
		return nil, "empty bearer token", nil
	}
	return a.resolveToken(r.Context(), tok, allowMCPScope)
}

// authMiddleware authenticates a request and stashes the *User in the context.
func (a *App) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, msg, err := a.authenticateRequest(r, false)
		if err != nil {
			serverErr(w, r, err, "auth lookup error")
			return
		}
		if u == nil {
			if msg == "" {
				msg = "missing bearer token"
			}
			writeErr(w, http.StatusUnauthorized, msg)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdminMiddleware refuses non-admin users. Runs after authMiddleware.
func (a *App) requireAdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		if !u.IsAdmin {
			writeErr(w, http.StatusForbidden, "admin only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) userByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	err := a.DB.QueryRowContext(ctx,
		`SELECT id, email, first_name, last_name, is_admin, created_at, token_version FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.IsAdmin, &u.CreatedAt, &u.TokenVersion)
	if err != nil {
		return nil, err
	}
	u.setDisplayName()
	return u, nil
}

// randHex returns n random bytes hex-encoded (2n chars).
func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
