package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"irlplanner/internal/metrics"
)

// Messaging is the admin outreach surface (the event "Messaging" tab): an
// admin-pressed invitation to every attendee, and a manual follow-up to the
// people who still haven't responded. Both render an admin-editable, per-event
// template (falling back to a generated default) and dispatch over a channel.
// Today only email delivers; Slack is a logged-no-op stub (see internal/slack)
// so the channel plumbing is in place for later.
//
// The scheduled non-responder reminders (reminders.go) share the same template
// and rendering helpers here, so editing the reminder copy in the tab also
// changes what the background scheduler sends.

// Message channels. The wire value of the `channel` request field.
const (
	channelEmail = "email"
	channelSlack = "slack"
)

// contact is a recipient with just enough identity to personalize a message.
type contact struct {
	Email     string
	FirstName string
}

// allAttendeeContacts returns every attendee of an event (the invitation
// audience), ordered for stable output.
func (s *Store) allAttendeeContacts(ctx context.Context, eventID string) ([]contact, error) {
	return s.scanContacts(ctx,
		`SELECT u.email, u.first_name
		   FROM event_attendees ea
		   JOIN users u ON u.id = ea.user_id
		  WHERE ea.event_id = $1
		  ORDER BY u.email`, eventID)
}

// nonResponderContacts returns attendees with no submission (the follow-up
// audience). Mirrors Store.nonResponders but carries the first name too.
func (s *Store) nonResponderContacts(ctx context.Context, eventID string) ([]contact, error) {
	return s.scanContacts(ctx,
		`SELECT u.email, u.first_name
		   FROM event_attendees ea
		   JOIN users u ON u.id = ea.user_id
		   LEFT JOIN submissions s ON s.event_id = ea.event_id AND s.user_id = ea.user_id
		  WHERE ea.event_id = $1 AND s.id IS NULL
		  ORDER BY u.email`, eventID)
}

func (s *Store) scanContacts(ctx context.Context, query, eventID string) ([]contact, error) {
	rows, err := s.db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contact{}
	for rows.Next() {
		var c contact
		if err := rows.Scan(&c.Email, &c.FirstName); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// invitedCount reports how many recipients have already been claimed for an
// invitation on this event (used to show "N already invited" in the tab).
func (s *Store) invitedCount(ctx context.Context, eventID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM reminder_log WHERE event_id = $1 AND reminder_kind = 'invitation'`,
		eventID).Scan(&n)
	return n, err
}

// --- templates -------------------------------------------------------------

// Default message templates. They use the same {{placeholder}} vocabulary as
// the stored overrides, so rendering is uniform whichever is in effect.
const (
	defaultInviteSubject   = "You're invited to {{event}}"
	defaultInviteBody      = "Hi {{name}},\n\nYou're invited to {{event}} in {{city}}.\n\nPlease share your attendance and travel details here:\n{{link}}\n\nKindly respond by {{deadline}}.\n\nThanks,\nThe People team\n"
	defaultReminderSubject = "Reminder: please respond for {{event}}"
	defaultReminderBody    = "Hi {{name}},\n\nWe haven't received your attendance details for {{event}} yet.\n\nPlease respond here:\n{{link}}\n\nThe deadline is {{deadline}}.\n\nThanks,\nThe People team\n"
)

// messageTemplates is the editable per-event copy, on the wire and in storage.
type messageTemplates struct {
	InviteSubject   string `json:"inviteSubject"`
	InviteBody      string `json:"inviteBody"`
	ReminderSubject string `json:"reminderSubject"`
	ReminderBody    string `json:"reminderBody"`
}

func defaultTemplates() messageTemplates {
	return messageTemplates{
		InviteSubject:   defaultInviteSubject,
		InviteBody:      defaultInviteBody,
		ReminderSubject: defaultReminderSubject,
		ReminderBody:    defaultReminderBody,
	}
}

// renderTemplate substitutes {{name}} {{event}} {{city}} {{link}} {{deadline}}
// (and any other key in vars) into a template. Unknown placeholders are left
// untouched so a typo is visible rather than silently dropped.
func renderTemplate(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

// firstNonEmpty returns the stored override when set, else the default.
func firstNonEmpty(override, fallback string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return fallback
}

// eventLink is the public attendee URL for an event (shared by invites,
// follow-ups, and the scheduled reminders).
func (a *App) eventLink(e *Event) string {
	return strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/events/" + e.Slug
}

// messageVars builds the placeholder values for one recipient.
func (a *App) messageVars(e *Event, c contact) map[string]string {
	name := strings.TrimSpace(c.FirstName)
	if name == "" {
		name = "there"
	}
	return map[string]string{
		"name":     name,
		"event":    e.Name,
		"city":     e.City,
		"link":     a.eventLink(e),
		"deadline": formatDeadline(e.SubmissionDeadline),
	}
}

// --- dispatch --------------------------------------------------------------

// sendVia delivers one message over the named channel. Unknown channels fall
// back to email (the only real channel today).
func (a *App) sendVia(channel string, to []string, subject, body string) error {
	if channel == channelSlack {
		return a.Slack.Send(to, subject, body)
	}
	return a.Email.Send(to, subject, body)
}

// channelStatus is the per-channel availability the tab uses to enable/disable
// its selector. Available means the channel is implemented and selectable (email
// is, Slack isn't yet — it's the "coming soon" stub); Configured means its
// transport is actually wired up (SMTP for email) so a send can succeed.
type channelStatus struct {
	Name       string `json:"name"`
	Available  bool   `json:"available"`
	Configured bool   `json:"configured"`
}

func (a *App) channelStatuses() []channelStatus {
	return []channelStatus{
		{Name: channelEmail, Available: true, Configured: a.Email.Configured()},
		{Name: channelSlack, Available: a.Slack.Configured(), Configured: a.Slack.Configured()},
	}
}

// --- handlers --------------------------------------------------------------

type messagingStatus struct {
	Templates messageTemplates `json:"templates"` // stored overrides ("" = use default)
	Defaults  messageTemplates `json:"defaults"`  // generated defaults, for the editor placeholder
	Stats     messagingStats   `json:"stats"`
	Channels  []channelStatus  `json:"channels"`
	Failures  []sendLogEntry   `json:"failures"` // recent failed sends (newest first)
}

type messagingStats struct {
	Attendees     int `json:"attendees"`
	Invited       int `json:"invited"`
	NonResponders int `json:"nonResponders"`
}

// handleGetMessaging returns the templates, defaults, audience stats, and
// channel availability for the Messaging tab.
func (a *App) handleGetMessaging(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	e, err := a.Store.loadEventByColumn(r.Context(), "id", id, time.Now())
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	attendees, err := a.Store.allAttendeeContacts(r.Context(), id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	nonResponders, err := a.Store.nonResponderContacts(r.Context(), id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	invited, err := a.Store.invitedCount(r.Context(), id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	failures, err := a.Store.recentFailures(r.Context(), id, 50)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, messagingStatus{
		Templates: messageTemplates{
			InviteSubject:   e.InviteSubject,
			InviteBody:      e.InviteBody,
			ReminderSubject: e.ReminderSubject,
			ReminderBody:    e.ReminderBody,
		},
		Defaults: defaultTemplates(),
		Stats: messagingStats{
			Attendees:     len(attendees),
			Invited:       invited,
			NonResponders: len(nonResponders),
		},
		Channels: a.channelStatuses(),
		Failures: failures,
	})
}

// template field length caps. Generous, but bound abuse and keep emails sane.
const (
	maxSubjectLen = 200
	maxBodyLen    = 8000
)

// handleSaveMessaging persists the editable templates. Empty fields are allowed
// and mean "fall back to the default".
func (a *App) handleSaveMessaging(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req messageTemplates
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.InviteSubject = strings.TrimSpace(req.InviteSubject)
	req.ReminderSubject = strings.TrimSpace(req.ReminderSubject)
	if len(req.InviteSubject) > maxSubjectLen || len(req.ReminderSubject) > maxSubjectLen {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("subject must be at most %d characters", maxSubjectLen))
		return
	}
	if len(req.InviteBody) > maxBodyLen || len(req.ReminderBody) > maxBodyLen {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("body must be at most %d characters", maxBodyLen))
		return
	}

	res, err := a.DB.ExecContext(r.Context(),
		`UPDATE events SET invite_subject=$1, invite_body=$2, reminder_subject=$3, reminder_body=$4, updated_at=now()
		   WHERE id=$5`,
		req.InviteSubject, req.InviteBody, req.ReminderSubject, req.ReminderBody, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	user := currentUser(r)
	if err := a.logActivity(r.Context(), a.DB, id, &user.ID, user.Email, "",
		actionMessageTemplateSaved, "Updated message templates", nil, false); err != nil {
		log.Printf("WARN: log template save for %s: %v", id, err)
	}
	writeJSON(w, http.StatusOK, req)
}

type sendMessageReq struct {
	Channel string `json:"channel"`
}

type sendMessageResp struct {
	Channel string `json:"channel"`
	Queued  int    `json:"queued"` // recipients handed to the background sender
}

// decodeChannel parses and validates the channel from a send request body,
// defaulting to email. An empty body is allowed.
func decodeChannel(r *http.Request) (string, error) {
	var req sendMessageReq
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			return "", fmt.Errorf("invalid json")
		}
	}
	ch := strings.TrimSpace(req.Channel)
	if ch == "" {
		ch = channelEmail
	}
	if ch != channelEmail && ch != channelSlack {
		return "", fmt.Errorf("channel must be 'email' or 'slack'")
	}
	return ch, nil
}

// handleSendInvitation emails the invitation to every attendee not yet invited
// (idempotent via reminder_log: re-pressing only catches newly-added people).
func (a *App) handleSendInvitation(w http.ResponseWriter, r *http.Request) {
	a.sendCampaign(w, r, campaign{
		kind:        "invitation",
		periodKey:   "invitation",
		action:      actionInvitationSent,
		subjectTmpl: func(e *Event) string { return firstNonEmpty(e.InviteSubject, defaultInviteSubject) },
		bodyTmpl:    func(e *Event) string { return firstNonEmpty(e.InviteBody, defaultInviteBody) },
		audience:    func(ctx context.Context, id string) ([]contact, error) { return a.Store.allAttendeeContacts(ctx, id) },
		summary: func(sent, failed int, ch string) string {
			return sendSummary("Sent invitation to", sent, failed, "attendee(s)", ch)
		},
		metricKind:     "invitation",
		emptyAudienceM: "no attendees to invite",
	})
}

// handleSendFollowup emails the reminder copy to current non-responders now.
// Idempotent per event-local day so a double-click doesn't double-send.
func (a *App) handleSendFollowup(w http.ResponseWriter, r *http.Request) {
	a.sendCampaign(w, r, campaign{
		kind:        "manual",
		periodKey:   "", // resolved per-event to the event-local date below
		action:      actionFollowupSent,
		subjectTmpl: func(e *Event) string { return firstNonEmpty(e.ReminderSubject, defaultReminderSubject) },
		bodyTmpl:    func(e *Event) string { return firstNonEmpty(e.ReminderBody, defaultReminderBody) },
		audience:    func(ctx context.Context, id string) ([]contact, error) { return a.Store.nonResponderContacts(ctx, id) },
		summary: func(sent, failed int, ch string) string {
			return sendSummary("Sent follow-up to", sent, failed, "non-responder(s)", ch)
		},
		metricKind:     "manual",
		emptyAudienceM: "everyone has responded — no follow-up needed",
	})
}

// campaign is the shared shape of an admin-pressed send (invitation/follow-up).
type campaign struct {
	kind           string // reminder_log.reminder_kind
	periodKey      string // fixed key, or "" to use the event-local date
	action         string // activity action verb
	subjectTmpl    func(e *Event) string
	bodyTmpl       func(e *Event) string
	audience       func(ctx context.Context, id string) ([]contact, error)
	summary        func(sent, failed int, channel string) string
	metricKind     string
	emptyAudienceM string
}

// sendSummary formats the activity summary for a completed campaign, appending
// the failure count only when there were failures.
func sendSummary(verb string, sent, failed int, noun, channel string) string {
	s := fmt.Sprintf("%s %d %s via %s", verb, sent, noun, channel)
	if failed > 0 {
		s += fmt.Sprintf(" (%d failed)", failed)
	}
	return s
}

// sendCampaign runs one outreach pass: validate channel, claim each recipient
// for exactly-once delivery, render + dispatch, then log one activity entry.
func (a *App) sendCampaign(w http.ResponseWriter, r *http.Request, c campaign) {
	id := chi.URLParam(r, "id")
	channel, err := decodeChannel(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Only block on email being unconfigured; the Slack stub is intentionally a
	// no-op that still "succeeds" so the channel can be exercised end to end.
	if channel == channelEmail && !a.Email.Configured() {
		writeErr(w, http.StatusConflict, "email is not configured (set SMTP_HOST)")
		return
	}

	e, err := a.Store.loadEventByColumn(r.Context(), "id", id, time.Now())
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	recipients, err := c.audience(r.Context(), id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if len(recipients) == 0 {
		writeErr(w, http.StatusBadRequest, c.emptyAudienceM)
		return
	}

	periodKey := c.periodKey
	if periodKey == "" {
		loc, lerr := loadLocation(e.Timezone)
		if lerr != nil {
			loc = time.UTC
		}
		periodKey = todayInZone(loc, time.Now()).Format(dateLayout)
	}

	subjectTmpl := c.subjectTmpl(e)
	bodyTmpl := c.bodyTmpl(e)
	user := currentUser(r)
	actorID, actorEmail := user.ID, user.Email

	// Send in the background and return immediately. Each recipient opens its own
	// SMTP connection, so a full batch (dozens of people) can take minutes — far
	// longer than an HTTP request should block, and longer than upstream proxy
	// timeouts. The reminder_log claim makes every recipient exactly-once, so a
	// detached send is safe (and safe to re-trigger). Detached context: the
	// request returns before the loop finishes.
	go func() {
		ctx := context.Background()
		sent, skipped, failed := 0, 0, 0
		for _, rc := range recipients {
			claimed, err := a.claimReminder(ctx, e.ID, rc.Email, c.kind, periodKey)
			if err != nil {
				log.Printf("WARN: %s claim for %s: %v", c.kind, rc.Email, err)
				continue
			}
			if !claimed {
				skipped++
				continue
			}
			vars := a.messageVars(e, rc)
			subject := renderTemplate(subjectTmpl, vars)
			body := renderTemplate(bodyTmpl, vars)
			if err := a.sendVia(channel, []string{rc.Email}, subject, body); err != nil {
				log.Printf("WARN: %s send to %s: %v", c.kind, rc.Email, err)
				// Release the claim so a retry re-sends rather than silently dropping.
				a.unclaimReminder(ctx, e.ID, rc.Email, c.kind, periodKey)
				a.logSend(ctx, e.ID, rc.Email, c.kind, channel, "failed", err.Error())
				metrics.MessageSendsTotal.WithLabelValues(c.metricKind, channel, "failed").Inc()
				failed++
				continue
			}
			sent++
			a.logSend(ctx, e.ID, rc.Email, c.kind, channel, "sent", "")
			metrics.RemindersSentTotal.WithLabelValues(c.metricKind).Inc()
			metrics.MessageSendsTotal.WithLabelValues(c.metricKind, channel, "sent").Inc()
		}
		if err := a.logActivity(ctx, a.DB, e.ID, &actorID, actorEmail, "",
			c.action, c.summary(sent, failed, channel),
			map[string]any{"channel": channel, "sent": sent, "skipped": skipped, "failed": failed}, false); err != nil {
			log.Printf("WARN: log %s for %s: %v", c.kind, e.ID, err)
		}
		log.Printf("messaging: %s for event %s via %s — sent %d, skipped %d, failed %d", c.kind, e.ID, channel, sent, skipped, failed)
	}()

	writeJSON(w, http.StatusAccepted, sendMessageResp{Channel: channel, Queued: len(recipients)})
}

// unclaimReminder removes an idempotency claim so a failed send can be retried.
func (a *App) unclaimReminder(ctx context.Context, eventID, recipient, kind, periodKey string) {
	if _, err := a.DB.ExecContext(ctx,
		`DELETE FROM reminder_log WHERE event_id=$1 AND recipient=$2 AND reminder_kind=$3 AND period_key=$4`,
		eventID, recipient, kind, periodKey); err != nil {
		log.Printf("WARN: unclaim %s for %s: %v", kind, recipient, err)
	}
}

// logSend records one per-recipient send outcome (message_send_log). Best-effort:
// a logging failure must never derail delivery, so it only WARNs. "sent" means
// the relay accepted the message — not that it was delivered (bounces aren't
// tracked). Shared by the campaign sender and the reminder scheduler.
func (a *App) logSend(ctx context.Context, eventID, recipient, kind, channel, status, sendErr string) {
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO message_send_log (event_id, recipient, kind, channel, status, error)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		eventID, recipient, kind, channel, status, sendErr); err != nil {
		log.Printf("WARN: message_send_log insert (%s/%s): %v", kind, recipient, err)
	}
}

// sendLogEntry is one row of the per-recipient delivery ledger, surfaced to the
// admin as a delivery-failure list.
type sendLogEntry struct {
	Recipient string    `json:"recipient"`
	Kind      string    `json:"kind"`
	Channel   string    `json:"channel"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"createdAt"`
}

// recentFailures returns the most recent failed sends for an event, newest
// first, capped at limit.
func (s *Store) recentFailures(ctx context.Context, eventID string, limit int) ([]sendLogEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT recipient, kind, channel, error, created_at
		   FROM message_send_log
		  WHERE event_id = $1 AND status = 'failed'
		  ORDER BY created_at DESC
		  LIMIT $2`, eventID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sendLogEntry{}
	for rows.Next() {
		var e sendLogEntry
		if err := rows.Scan(&e.Recipient, &e.Kind, &e.Channel, &e.Error, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
