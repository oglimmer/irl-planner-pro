package server

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// notifySubmissionChanged emails the People team / admins when an existing
// submission is edited (not the first create, per DESIGN.md §9.2). Best-effort
// and asynchronous: a send failure logs a WARN and never affects the request.
func (a *App) notifySubmissionChanged(e *Event, ownerEmail string, actor *User, existed bool, summary string) {
	if !existed || !a.Email.Configured() {
		return
	}
	subject := fmt.Sprintf("[IRL %s] response updated", e.Name)
	link := strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/admin/events/" + e.ID
	body := fmt.Sprintf("%s\n\nAttendee: %s\nChanged by: %s\nEvent: %s\n\nDashboard: %s\n",
		summary, ownerEmail, actor.Email, e.Name, link)

	// Detached context: the request may complete before the email is sent.
	go func() {
		recipients := a.recipients(context.Background())
		if len(recipients) == 0 {
			return
		}
		if err := a.Email.Send(recipients, subject, body); err != nil {
			log.Printf("WARN: submission-changed email failed: %v", err)
		}
	}()
}

// recipients returns the admin notification list: PEOPLE_TEAM_EMAIL plus every
// current admin's email, de-duplicated (lower-cased).
func (a *App) recipients(ctx context.Context) []string {
	seen := map[string]bool{}
	var out []string
	add := func(e string) {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	add(a.Cfg.PeopleTeamEmail)

	rows, err := a.DB.QueryContext(ctx, `SELECT email FROM users WHERE is_admin`)
	if err != nil {
		log.Printf("WARN: load admin recipients: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err == nil {
			add(e)
		}
	}
	return out
}
