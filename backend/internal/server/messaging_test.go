package server

import "testing"

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
