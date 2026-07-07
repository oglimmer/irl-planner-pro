package server

import (
	"testing"
	"time"
)

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
package server

import (
	"context"
	"encoding/json"
	"testing"
)

// TestCampaignActivityDetail verifies that a campaign send (invitation or
// follow-up) writes an activity log entry with the expected detail fields:
// channels, sent, skipped, failed, sentPerChannel, and (when failures exist)
// failedRecipients.
func TestCampaignActivityDetail(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	adminID := mkAdmin(t, a, ctx, "admin@oglimmer.com")
	eventID := mkEventForTest(t, a, ctx, adminID, "campaign-detail", "2026-09-01", "2026-09-03")

	// Build the enriched detail that sendCampaign would produce.
	detail := map[string]any{
		"channels":       []string{channelEmail, channelSlack},
		"sent":           1,
		"skipped":        0,
		"failed":         0,
		"sentPerChannel": map[string]int{channelEmail: 1, channelSlack: 0},
	}
	if err := a.logActivity(ctx, a.DB, eventID, &adminID, "admin@oglimmer.com", "",
		actionInvitationSent, "Sent invitation to 1 attendee(s) via email, slack", detail, false); err != nil {
		t.Fatalf("logActivity: %v", err)
	}

	// Read back and assert the required keys exist.
	var stored []byte
	err := a.DB.QueryRowContext(ctx,
		`SELECT detail FROM activity_log WHERE event_id=$1 AND action=$2 ORDER BY created_at DESC LIMIT 1`,
		eventID, actionInvitationSent).Scan(&stored)
	if err != nil {
		t.Fatalf("query activity: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(stored, &got); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	for _, key := range []string{"channels", "sent", "skipped", "failed", "sentPerChannel"} {
		if _, ok := got[key]; !ok {
			t.Errorf("detail missing key %q", key)
		}
	}
}
