// Package slack delivers messages to Slack users as bot direct messages. It
// deliberately mirrors the shape of email.Sender (Configured + Send) so the
// messaging dispatcher can treat Slack as just another channel.
//
// Delivery uses a workspace **bot token** (xoxb-…). That is the enterprise
// install model: an admin installs the app once to the workspace, and the bot
// can then DM any employee — recipients do NOT install or authorize anything
// themselves. Each recipient's company email is resolved to a Slack user ID with
// users.lookupByEmail, then a DM is posted with chat.postMessage. The bot needs
// the scopes `users:read.email` and `chat:write`.
//
// Like internal/email this is intentionally minimal: a single Notifier value
// carries the token and Send delivers one message to a set of recipients. There
// is no queueing; the one transient Slack reliably surfaces under a burst of
// sends — HTTP 429 — gets a single Retry-After-respecting retry.
package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// defaultAPIBase is the Slack Web API root. Overridable via Notifier.APIBase in
// tests.
const defaultAPIBase = "https://slack.com/api"

// Notifier holds Slack connection settings. The zero value is "not configured":
// Configured reports false until a bot token is set.
type Notifier struct {
	// Token is a Slack bot token (xoxb-…). Empty disables Slack delivery.
	Token string
	// APIBase overrides the Slack Web API root (tests only). Empty uses the real
	// api.slack.com endpoint.
	APIBase string
	// HTTPClient overrides the HTTP client (tests only). Empty uses a client with
	// a sane timeout.
	HTTPClient *http.Client
}

// Configured reports whether a bot token is present to attempt delivery.
func (n Notifier) Configured() bool {
	return strings.TrimSpace(n.Token) != ""
}

// Send delivers a single message to every address in to as a Slack DM. Slack
// messages have no subject, so a non-empty subject becomes a bold first line
// above the body. Each email is resolved to a Slack user ID and DM'd in turn;
// the first failure aborts and is returned — so the dispatcher's one-recipient
// call maps cleanly to one delivery outcome (claimed/unclaimed for retry).
func (n Notifier) Send(to []string, subject, body string) error {
	if !n.Configured() {
		return fmt.Errorf("slack: notifier not configured")
	}
	text := body
	if s := strings.TrimSpace(subject); s != "" {
		text = "*" + s + "*\n\n" + body
	}
	sent := false
	for _, addr := range to {
		if addr = strings.TrimSpace(addr); addr == "" {
			continue
		}
		userID, err := n.lookupUserID(addr)
		if err != nil {
			return err
		}
		if err := n.postMessage(userID, text); err != nil {
			return err
		}
		sent = true
	}
	if !sent {
		return fmt.Errorf("slack: no recipients")
	}
	return nil
}

// lookupUserID resolves a company email to a Slack user ID via
// users.lookupByEmail. A user not present in the workspace is an actionable
// error (surfaced in the admin delivery-failure list).
func (n Notifier) lookupUserID(email string) (string, error) {
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := n.call("users.lookupByEmail", url.Values{"email": {email}}, &out); err != nil {
		return "", err
	}
	if !out.OK {
		return "", fmt.Errorf("slack: lookup %s: %s", email, slackErr(out.Error))
	}
	if out.User.ID == "" {
		return "", fmt.Errorf("slack: lookup %s: empty user id", email)
	}
	return out.User.ID, nil
}

// postMessage posts text as a DM. Passing a user ID as `channel` makes Slack
// open (or reuse) the IM and deliver there — no separate conversations.open
// needed.
func (n Notifier) postMessage(userID, text string) error {
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := n.call("chat.postMessage", url.Values{"channel": {userID}, "text": {text}}, &out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("slack: post to %s: %s", userID, slackErr(out.Error))
	}
	return nil
}

// call POSTs a form-encoded request to a Slack Web API method and decodes the
// JSON response into out. Slack accepts form encoding for both methods used here
// and always replies 200 with {ok,error}; a non-200 means a transport/proxy
// problem. One retry on 429, respecting Retry-After.
func (n Notifier) call(method string, form url.Values, out any) error {
	endpoint := strings.TrimRight(n.apiBase(), "/") + "/" + method
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return fmt.Errorf("slack: new request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(n.Token))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := n.client().Do(req)
		if err != nil {
			return fmt.Errorf("slack: %s: %w", method, err)
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			wait := retryAfter(resp.Header.Get("Retry-After"))
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			time.Sleep(wait)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return fmt.Errorf("slack: %s: http %d: %s", method, resp.StatusCode, strings.TrimSpace(string(b)))
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("slack: %s: decode: %w", method, err)
		}
		return nil
	}
}

func (n Notifier) apiBase() string {
	if n.APIBase != "" {
		return n.APIBase
	}
	return defaultAPIBase
}

func (n Notifier) client() *http.Client {
	if n.HTTPClient != nil {
		return n.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// retryAfter parses a Retry-After header (seconds), clamped to [1s, 60s].
func retryAfter(h string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil || secs < 1 {
		secs = 1
	}
	if secs > 60 {
		secs = 60
	}
	return time.Duration(secs) * time.Second
}

// slackErr maps the common Slack API error codes to a friendlier message,
// falling back to the raw code so anything unexpected is still visible.
func slackErr(code string) string {
	switch code {
	case "":
		return "unknown error"
	case "users_not_found":
		return "no Slack user with that email"
	case "invalid_auth", "not_authed", "account_inactive", "token_revoked":
		return "Slack bot token is invalid or revoked"
	case "missing_scope":
		return "Slack bot token is missing a required scope (users:read.email, chat:write)"
	default:
		return code
	}
}
