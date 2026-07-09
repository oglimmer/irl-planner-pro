package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// mockSender is a fake notifier that records recipients and always succeeds.
type mockSender struct {
	mu       sync.Mutex
	emails   []string
	subjects []string
	bodies   []string
}

func (m *mockSender) Configured() bool { return true }
func (m *mockSender) Send(to []string, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range to {
		m.emails = append(m.emails, r)
		m.subjects = append(m.subjects, subject)
		m.bodies = append(m.bodies, body)
	}
	return nil
}

func TestFormatDeadline(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		// 00:00 UTC in summer is 02:00 in Paris (CEST, +2).
		{"summer", time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), "July 3, 2026 at 2:00 AM (Europe/Paris)"},
		// 17:30 UTC in winter is 18:30 in Paris (CET, +1).
		{"winter", time.Date(2026, 12, 25, 17, 30, 0, 0, time.UTC), "December 25, 2026 at 6:30 PM (Europe/Paris)"},
		// An already-Paris-zoned instant renders the same wall clock.
		{"non-utc input", time.Date(2026, 7, 3, 2, 0, 0, 0, time.FixedZone("CEST", 2*3600)), "July 3, 2026 at 2:00 AM (Europe/Paris)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatDeadline(tc.in); got != tc.want {
				t.Errorf("formatDeadline(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRenderTemplate(t *testing.T) {
	vars := map[string]string{
		"name":     "Ada",
		"event":    "Summer Offsite",
		"city":     "Lisbon",
		"link":     "https://x/events/summer",
		"deadline": "2026-07-01 17:00",
	}
	tests := []struct {
		name, in, want string
	}{
		{"all placeholders", "Hi {{name}}, join {{event}} in {{city}}: {{link}} by {{deadline}}",
			"Hi Ada, join Summer Offsite in Lisbon: https://x/events/summer by 2026-07-01 17:00"},
		{"repeated placeholder", "{{name}} {{name}}", "Ada Ada"},
		{"unknown left intact", "Hi {{name}} {{unknown}}", "Hi Ada {{unknown}}"},
		{"no placeholders", "plain text", "plain text"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderTemplate(tc.in, vars); got != tc.want {
				t.Errorf("renderTemplate(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("override", "fallback"); got != "override" {
		t.Errorf("non-empty override: got %q", got)
	}
	if got := firstNonEmpty("", "fallback"); got != "fallback" {
		t.Errorf("empty override: got %q", got)
	}
	if got := firstNonEmpty("   ", "fallback"); got != "fallback" {
		t.Errorf("whitespace override should fall back: got %q", got)
	}
}

// defaultTemplates must keep using the shared placeholder vocabulary so the
// renderer fills them; a default with a stray literal would ship raw to users.
func TestDefaultTemplatesRenderCleanly(t *testing.T) {
	vars := map[string]string{
		"name": "Ada", "event": "Offsite", "city": "Lisbon",
		"link": "https://x", "deadline": "soon",
	}
	for _, tmpl := range []string{
		defaultInviteSubject, defaultInviteBody, defaultReminderSubject, defaultReminderBody,
	} {
		if out := renderTemplate(tmpl, vars); contains(out, "{{") {
			t.Errorf("default template left an unrendered placeholder: %q", out)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestInvitationActivityDetail(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	adminID := mkAdmin(t, a, ctx, "admin@oglimmer.com")
	eventID := mkEventForTest(t, a, ctx, adminID, "detail-event", "2026-09-01", "2026-09-03")

	addAttendee(t, a, ctx, eventID, "alice@oglimmer.com", "Alice", "")
	addAttendee(t, a, ctx, eventID, "bob@oglimmer.com", "Bob", "")

	emailMock := &mockSender{}
	a.Email = emailMock
	a.Slack = noopSender{}

	r := httptest.NewRequest(http.MethodPost, "/api/admin/events/"+eventID+"/messaging/invite", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", eventID)
	r = r.WithContext(context.WithValue(withAdmin(ctx, adminID), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	a.handleSendInvitation(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d", w.Code)
	}

	// Poll until the activity entry appears (async write).
	var entries []ActivityEntry
	deadline := time.Now().Add(2 * time.Second)
	for {
		var err error
		entries, err = a.queryActivity(ctx, eventID, "", "")
		if err != nil {
			t.Fatalf("query activity: %v", err)
		}
		if len(entries) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Action != actionInvitationSent {
		t.Fatalf("action = %s", entry.Action)
	}

	type detailPayload struct {
		Recipients []messageRecipDetail `json:"recipients"`
	}
	var detail detailPayload
	if entry.Detail == nil {
		t.Fatal("detail is nil")
	}
	if err := json.Unmarshal(entry.Detail, &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(detail.Recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(detail.Recipients))
	}
	for _, rd := range detail.Recipients {
		if rd.Status != "sent" {
			t.Errorf("recipient %s status = %s", rd.Email, rd.Status)
		}
	}
}

func addAttendee(t *testing.T, a *App, ctx context.Context, eventID, email, firstName, _ string) {
	t.Helper()
	u, err := a.Store.findOrCreateUser(ctx, email, firstName, "", "")
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO event_attendees (event_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		eventID, u.ID); err != nil {
		t.Fatalf("add attendee %s: %v", email, err)
	}
}
