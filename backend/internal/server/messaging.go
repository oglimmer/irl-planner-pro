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
// template (falling back to a generated default) and dispatch over every
// configured channel at once — email over SMTP (internal/email) and Slack bot
// DMs (internal/slack) — with no per-send channel choice. A recipient is claimed
// once (a single channel-agnostic idempotency flag) and counts as reached if any
// channel accepts; the claim is only released when every channel fails.
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
		  WHERE ea.event_id = $1 AND NOT u.archived
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
		  WHERE ea.event_id = $1 AND s.id IS NULL AND NOT u.archived
		  ORDER BY u.email`, eventID)
}

// flightCostMissingContacts returns attendees who HAVE responded "yes", are
// flying on at least one leg, but left their flight cost blank (the flight-cost
// nudge audience). Flight cost is only meaningful for attending = 'yes', so
// "no"/"not_sure" responders are excluded; and it is a *flight* fare, so
// train/car/other-only travellers are excluded too — otherwise we would chase
// people for a number they can never have. An independent leg has its mode
// blanked on write, so the mode check alone covers that case.
// This set is disjoint from nonResponderContacts (which requires no submission),
// so nobody receives both the "please respond" and the "add your flight cost"
// reminder.
func (s *Store) flightCostMissingContacts(ctx context.Context, eventID string) ([]contact, error) {
	return s.scanContacts(ctx,
		`SELECT u.email, u.first_name
		   FROM event_attendees ea
		   JOIN users u ON u.id = ea.user_id
		   JOIN submissions s ON s.event_id = ea.event_id AND s.user_id = ea.user_id
		  WHERE ea.event_id = $1 AND NOT u.archived
		    AND s.attending = 'yes' AND s.travel_cost IS NULL
		    AND (s.arrival_mode = 'flight' OR s.departure_mode = 'flight')
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
	defaultInviteBody      = "Hi {{name}},\n\nYou're invited to {{event}} in {{city}}.\n\nPlease share your attendance and travel details here:\n{{link}}\n\nKindly respond by {{deadline}}.\n\nThanks,\nThe IRL team\n"
	defaultReminderSubject = "Reminder: please respond for {{event}}"
	defaultReminderBody    = "Hi {{name}},\n\nWe haven't received your attendance details for {{event}} yet.\n\nPlease respond here:\n{{link}}\n\nThe deadline is {{deadline}}.\n\nThanks,\nThe IRL team\n"
	// Flight-cost nudge, sent to attendees who responded "yes" but left the
	// (optional) flight cost blank. Uses the same {{placeholder}} vocabulary and
	// the same schedule/channels as the non-responder reminder. Admin-editable per
	// event (flight_reminder_subject/body, migration 0020); this is the fallback.
	defaultFlightReminderSubject = "Reminder: add your flight cost for {{event}}"
	defaultFlightReminderBody    = "Hi {{name}},\n\nThanks for confirming you're attending {{event}}. We still need your flight cost to estimate the overall offsite budget.\n\nPlease add it here — flight only, no need for train, taxi, or other travel:\n{{link}}\n\nThe deadline is {{deadline}}.\n\nThanks,\nThe IRL team\n"
)

// messageTemplates is the editable per-event copy, on the wire and in storage.
type messageTemplates struct {
	InviteSubject         string `json:"inviteSubject"`
	InviteBody            string `json:"inviteBody"`
	ReminderSubject       string `json:"reminderSubject"`
	ReminderBody          string `json:"reminderBody"`
	FlightReminderSubject string `json:"flightReminderSubject"`
	FlightReminderBody    string `json:"flightReminderBody"`
}

func defaultTemplates() messageTemplates {
	return messageTemplates{
		InviteSubject:         defaultInviteSubject,
		InviteBody:            defaultInviteBody,
		ReminderSubject:       defaultReminderSubject,
		ReminderBody:          defaultReminderBody,
		FlightReminderSubject: defaultFlightReminderSubject,
		FlightReminderBody:    defaultFlightReminderBody,
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
// back to email (the default channel).
func (a *App) sendVia(channel string, to []string, subject, body string) error {
	if channel == channelSlack {
		return a.Slack.Send(to, subject, body)
	}
	return a.Email.Send(to, subject, body)
}

// channelStatus is the per-channel availability the notification/messaging
// surfaces read. Available means the channel is implemented (both email and
// Slack are); Configured means its transport is actually wired up (SMTP for
// email, a bot token for Slack) so a send can succeed.
type channelStatus struct {
	Name       string `json:"name"`
	Available  bool   `json:"available"`
	Configured bool   `json:"configured"`
}

func (a *App) channelStatuses() []channelStatus {
	return []channelStatus{
		{Name: channelEmail, Available: true, Configured: a.Email.Configured()},
		{Name: channelSlack, Available: true, Configured: a.Slack.Configured()},
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
	Attendees         int `json:"attendees"`
	Invited           int `json:"invited"`
	NonResponders     int `json:"nonResponders"`
	FlightCostMissing int `json:"flightCostMissing"` // responded "yes" but no flight cost
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
	flightMissing, err := a.Store.flightCostMissingContacts(r.Context(), id)
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
			InviteSubject:         e.InviteSubject,
			InviteBody:            e.InviteBody,
			ReminderSubject:       e.ReminderSubject,
			ReminderBody:          e.ReminderBody,
			FlightReminderSubject: e.FlightReminderSubject,
			FlightReminderBody:    e.FlightReminderBody,
		},
		Defaults: defaultTemplates(),
		Stats: messagingStats{
			Attendees:         len(attendees),
			Invited:           invited,
			NonResponders:     len(nonResponders),
			FlightCostMissing: len(flightMissing),
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
	req.FlightReminderSubject = strings.TrimSpace(req.FlightReminderSubject)
	if len(req.InviteSubject) > maxSubjectLen || len(req.ReminderSubject) > maxSubjectLen || len(req.FlightReminderSubject) > maxSubjectLen {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("subject must be at most %d characters", maxSubjectLen))
		return
	}
	if len(req.InviteBody) > maxBodyLen || len(req.ReminderBody) > maxBodyLen || len(req.FlightReminderBody) > maxBodyLen {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("body must be at most %d characters", maxBodyLen))
		return
	}

	res, err := a.DB.ExecContext(r.Context(),
		`UPDATE events SET invite_subject=$1, invite_body=$2, reminder_subject=$3, reminder_body=$4,
		        flight_reminder_subject=$5, flight_reminder_body=$6, updated_at=now()
		   WHERE id=$7`,
		req.InviteSubject, req.InviteBody, req.ReminderSubject, req.ReminderBody,
		req.FlightReminderSubject, req.FlightReminderBody, id)
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

type sendMessageResp struct {
	Channels []string `json:"channels"` // channels the campaign is delivering over
	Queued   int      `json:"queued"`   // recipients handed to the background sender
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
		summary: func(sent, failed int, chs []string) string {
			return sendSummary("Sent invitation to", sent, failed, "attendee(s)", chs)
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
		summary: func(sent, failed int, chs []string) string {
			return sendSummary("Sent follow-up to", sent, failed, "non-responder(s)", chs)
		},
		metricKind:     "manual",
		emptyAudienceM: "everyone has responded — no follow-up needed",
	})
}

// handleSendFlightFollowup sends the flight-cost nudge to attendees who responded
// "yes" but left their flight cost blank, now. Idempotent per event-local day.
// Uses the editable flight-reminder copy (falling back to the default) — the same
// message the scheduler sends. Its own reminder_kind keeps it disjoint from the
// non-responder follow-up so a person is never claimed by both.
func (a *App) handleSendFlightFollowup(w http.ResponseWriter, r *http.Request) {
	a.sendCampaign(w, r, campaign{
		kind:        "manual_flightcost",
		periodKey:   "", // resolved per-event to the event-local date below
		action:      actionFlightFollowupSent,
		subjectTmpl: func(e *Event) string { return firstNonEmpty(e.FlightReminderSubject, defaultFlightReminderSubject) },
		bodyTmpl:    func(e *Event) string { return firstNonEmpty(e.FlightReminderBody, defaultFlightReminderBody) },
		audience: func(ctx context.Context, id string) ([]contact, error) {
			return a.Store.flightCostMissingContacts(ctx, id)
		},
		summary: func(sent, failed int, chs []string) string {
			return sendSummary("Sent flight-cost reminder to", sent, failed, "attendee(s)", chs)
		},
		metricKind:     "manual_flightcost",
		emptyAudienceM: "no attendees are missing a flight cost — nothing to send",
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
	summary        func(sent, failed int, channels []string) string
	metricKind     string
	emptyAudienceM string
}

// sendSummary formats the activity summary for a completed campaign, appending
// the failure count only when there were failures.
func sendSummary(verb string, sent, failed int, noun string, channels []string) string {
	s := fmt.Sprintf("%s %d %s via %s", verb, sent, noun, strings.Join(channels, ", "))
	if failed > 0 {
		s += fmt.Sprintf(" (%d failed)", failed)
	}
	return s
}

// sendCampaign runs one outreach pass: validate channel, claim each recipient
// for exactly-once delivery, render + dispatch, then log one activity entry.
func (a *App) sendCampaign(w http.ResponseWriter, r *http.Request, c campaign) {
	id := chi.URLParam(r, "id")
	// Deliver over every configured channel (email + Slack DM) — there is no
	// per-send channel choice. Block only when nothing is wired up, otherwise the
	// campaign would claim recipients and silently deliver nothing.
	channels := a.configuredChannels()
	if len(channels) == 0 {
		writeErr(w, http.StatusConflict, "no delivery channel configured (set SMTP_HOST or SLACK_BOT_TOKEN)")
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
			// Fan out to every configured channel. The recipient is claimed once (a
			// single channel-agnostic idempotency flag), so we count them reached if
			// at least one channel accepts. Only when every channel fails do we
			// release the claim so a retry re-sends. Per-channel failures still land
			// in message_send_log so a partial failure surfaces to the admin.
			anySent := false
			for _, ch := range channels {
				if err := a.sendVia(ch, []string{rc.Email}, subject, body); err != nil {
					log.Printf("WARN: %s send to %s via %s: %v", c.kind, rc.Email, ch, err)
					a.logSend(ctx, e.ID, rc.Email, c.kind, ch, "failed", err.Error())
					metrics.MessageSendsTotal.WithLabelValues(c.metricKind, ch, "failed").Inc()
					continue
				}
				anySent = true
				a.logSend(ctx, e.ID, rc.Email, c.kind, ch, "sent", "")
				metrics.MessageSendsTotal.WithLabelValues(c.metricKind, ch, "sent").Inc()
			}
			if !anySent {
				// Nothing delivered on any channel — release so a retry re-sends.
				a.unclaimReminder(ctx, e.ID, rc.Email, c.kind, periodKey)
				failed++
				continue
			}
			sent++
			metrics.RemindersSentTotal.WithLabelValues(c.metricKind).Inc()
		}
		if err := a.logActivity(ctx, a.DB, e.ID, &actorID, actorEmail, "",
			c.action, c.summary(sent, failed, channels),
			map[string]any{"channels": channels, "sent": sent, "skipped": skipped, "failed": failed}, false); err != nil {
			log.Printf("WARN: log %s for %s: %v", c.kind, e.ID, err)
		}
		log.Printf("messaging: %s for event %s via %s — sent %d, skipped %d, failed %d", c.kind, e.ID, strings.Join(channels, ","), sent, skipped, failed)
	}()

	writeJSON(w, http.StatusAccepted, sendMessageResp{Channels: channels, Queued: len(recipients)})
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
