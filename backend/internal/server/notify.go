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
// never affects the request. The People team is NOT on this path — they receive
// only the daily summary (see reminders.go / processEventDigest).
func (a *App) notifySubmissionActivity(e *Event, ownerEmail string, actor *User, existed bool, summary string) {
	verb := "submitted"
	if existed {
		verb = "updated"
	}
	subject := fmt.Sprintf("[IRL %s] response %s", e.Name, verb)
	link := strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/admin/events/" + e.ID
	body := fmt.Sprintf("%s\n\nAttendee: %s\nChanged by: %s\nEvent: %s\n\nDashboard: %s\n",
		summary, ownerEmail, actor.Email, e.Name, link)

	// Detached context: the request may complete before the email/DM is sent.
	go func() {
		emailTo, slackTo := a.notifyTargets(context.Background(), e.ID, notifTypeActivity)
		a.dispatch(emailTo, slackTo, subject, body)
	}()
}
