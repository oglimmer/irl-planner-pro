package server

import (
	"context"
	"fmt"
	"strings"
)

// notifySubmissionActivity sends an immediate notification to admins who opted
// into the "any activity" stream for an event — on both first submission and
// later edits (DESIGN.md §9.2). Best-effort and asynchronous: it dispatches over
// each opted-in channel (email and/or Slack), a delivery failure logs a WARN and
// never affects the request. The IRL team is NOT on this path — they receive
// only the daily summary (see reminders.go / processEventDigest).
func (a *App) notifySubmissionActivity(e *Event, ownerEmail string, actor *User, existed bool, summary string, changes []fieldChange) {
	verb := "submitted"
	if existed {
		verb = "updated"
	}
	subject := fmt.Sprintf("[IRL %s] response %s", e.Name, verb)
	link := strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/admin/events/" + e.ID
	body := fmt.Sprintf("%s\n\nAttendee: %s\nChanged by: %s\nEvent: %s\n%s\nDashboard: %s\n",
		summary, ownerEmail, actor.Email, e.Name, formatChanges(changes), link)

	// Detached context: the request may complete before the email/DM is sent.
	go func() {
		emailTo, slackTo := a.notifyTargets(context.Background(), e.ID, notifTypeActivity)
		a.dispatch(emailTo, slackTo, subject, body)
	}()
}

// formatChanges renders the field-level diff as an indented "Details:" block for
// the email/Slack body — the same set/changed fields the Activity tab shows, kept
// terse so an admin scanning a DM sees just what moved. Empty when nothing
// changed (an admin re-save with no edits), collapsing the body to the headline.
// A set field (first response) reads "Field: value"; an edit reads
// "Field: old → new"; a cleared field reads "Field: old → (cleared)".
func formatChanges(changes []fieldChange) string {
	if len(changes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nDetails:\n")
	for _, c := range changes {
		switch {
		case c.From == "":
			fmt.Fprintf(&b, "  %s: %s\n", c.Field, c.To)
		case c.To == "":
			fmt.Fprintf(&b, "  %s: %s → (cleared)\n", c.Field, c.From)
		default:
			fmt.Fprintf(&b, "  %s: %s → %s\n", c.Field, c.From, c.To)
		}
	}
	return b.String()
}
