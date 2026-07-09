package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"irlplanner/internal/metrics"
)

// reminderWindow is one due reminder occurrence (a kind + an idempotency key).
type reminderWindow struct {
	Kind      string // "weekly" | "deadline"
	PeriodKey string // e.g. "2026-W40" or "2026-10-12"
}

// StartReminders launches the reminder + digest scheduler. No-op when disabled.
// Mirrors the reference's StartSkillAudit goroutine lifecycle: bound to ctx,
// tracked by wg, driven by a ticker, with one immediate pass at startup.
func (a *App) StartReminders(ctx context.Context, wg *sync.WaitGroup) {
	if !a.Cfg.RemindersEnabled {
		log.Printf("reminders disabled (REMINDERS_ENABLED=false)")
		return
	}
	if !a.Email.Configured() && !a.Slack.Configured() {
		log.Printf("WARN: reminders enabled but neither SMTP nor Slack configured — reminders/digests will be skipped until SMTP_HOST or SLACK_BOT_TOKEN is set")
	}
	log.Printf("reminders enabled: tick=%s", a.Cfg.ReminderTickInterval)

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.runReminderTick(ctx, time.Now())
		ticker := time.NewTicker(a.Cfg.ReminderTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				a.runReminderTick(ctx, now)
			}
		}
	}()
}

// runReminderTick processes every open event once. Sends are skipped (nothing
// is claimed) when no delivery channel is configured, so they fire once one is.
func (a *App) runReminderTick(ctx context.Context, now time.Time) {
	if len(a.configuredChannels()) == 0 {
		return
	}
	ids, err := a.openEventIDs(ctx, now)
	if err != nil {
		log.Printf("WARN: reminder tick: list events: %v", err)
		return
	}
	for _, id := range ids {
		e, err := a.Store.loadEventByColumn(ctx, "id", id, now)
		if err != nil {
			log.Printf("WARN: reminder tick: load event %s: %v", id, err)
			continue
		}
		a.processEventReminders(ctx, e, now)
		a.processEventDigest(ctx, e, now)
	}
}

// openEventIDs lists events whose deadline hasn't passed yet (reminders stop at
// the deadline) — the candidate set for a tick.
func (a *App) openEventIDs(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT id FROM events WHERE submission_deadline > $1`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (a *App) processEventReminders(ctx context.Context, e *Event, now time.Time) {
	windows := computeDueReminders(e, now)
	if len(windows) == 0 {
		return
	}
	channels := a.configuredChannels()
	if len(channels) == 0 {
		return
	}
	nonResponders, err := a.Store.nonResponderContacts(ctx, e.ID)
	if err != nil {
		log.Printf("WARN: reminder: non-responders for %s: %v", e.ID, err)
		return
	}
	// Same editable template the Messaging tab saves; falls back to the default.
	subjectTmpl := firstNonEmpty(e.ReminderSubject, defaultReminderSubject)
	bodyTmpl := firstNonEmpty(e.ReminderBody, defaultReminderBody)
	var totalSent int
	var recipDetails []messageRecipDetail
	for _, win := range windows {
		for _, rc := range nonResponders {
			vars := a.messageVars(e, rc)
			subject := renderTemplate(subjectTmpl, vars)
			body := renderTemplate(bodyTmpl, vars)
			for _, ch := range channels {
				success, status, errStr := a.sendReminder(ctx, e, rc, win, ch, subject, body)
				if success {
					totalSent++
					recipDetails = append(recipDetails, messageRecipDetail{Email: rc.Email, Channel: ch, Status: status, Error: errStr})
				}
			}
		}
	}
	if totalSent > 0 {
		summary := fmt.Sprintf("Sent %d scheduled reminder(s) to non‑responders via %s", totalSent, strings.Join(channels, ", "))
		if err := a.logActivity(ctx, a.DB, e.ID, nil, "", "", actionScheduledRemindersSent, summary,
			map[string]any{"recipients": recipDetails}, false); err != nil {
			log.Printf("WARN: log scheduled reminders for %s: %v", e.ID, err)
		}
	}
}

// sendReminder delivers one scheduled reminder to one non-responder over one
// channel, exactly-once via a per-channel claim. A send failure releases the
// claim so the next due tick retries (matching the manual follow-up path).
// Returns whether the message was actually sent (success), the status string
// ("sent"/"failed"), and any error message.
func (a *App) sendReminder(ctx context.Context, e *Event, rc contact, win reminderWindow, channel, subject, body string) (bool, string, string) {
	key := reminderClaimKey(win.PeriodKey, channel)
	claimed, err := a.claimReminder(ctx, e.ID, rc.Email, win.Kind, key)
	if err != nil {
		log.Printf("WARN: reminder claim: %v", err)
		return false, "", ""
	}
	if !claimed {
		return false, "", "" // already sent for this window+channel
	}
	if err := a.sendVia(channel, []string{rc.Email}, subject, body); err != nil {
		log.Printf("WARN: reminder %s to %s: %v", channel, rc.Email, err)
		a.unclaimReminder(ctx, e.ID, rc.Email, win.Kind, key)
		a.logSend(ctx, e.ID, rc.Email, win.Kind, channel, "failed", err.Error())
		metrics.MessageSendsTotal.WithLabelValues(win.Kind, channel, "failed").Inc()
		return false, "failed", err.Error()
	}
	a.logSend(ctx, e.ID, rc.Email, win.Kind, channel, "sent", "")
	metrics.RemindersSentTotal.WithLabelValues(win.Kind).Inc()
	metrics.MessageSendsTotal.WithLabelValues(win.Kind, channel, "sent").Inc()
	return true, "sent", ""
}

// configuredChannels lists the delivery channels currently wired up, in send
// order (email first). Scheduled reminders and admin-pressed campaigns both go
// out on each.
func (a *App) configuredChannels() []string {
	var chs []string
	if a.Email.Configured() {
		chs = append(chs, channelEmail)
	}
	if a.Slack.Configured() {
		chs = append(chs, channelSlack)
	}
	return chs
}

// reminderClaimKey qualifies a window's idempotency key by channel so each
// channel is claimed and retried independently. Email keeps the bare key for
// backward compatibility with reminders already sent before multi-channel
// delivery (so a deploy doesn't re-send them); other channels get a suffix.
func reminderClaimKey(periodKey, channel string) string {
	if channel == channelEmail {
		return periodKey
	}
	return periodKey + "|" + channel
}

func (a *App) processEventDigest(ctx context.Context, e *Event, now time.Time) {
	if !dueAtReminderHour(e, now) {
		return
	}
	// Recipients: admins who opted into the daily stream (split by channel),
	// plus the IRL team (email only) when the event's daily-summary flag is
	// on. No opted-in recipient ⇒ nothing to do.
	emailTo, slackTo := a.notifyTargets(ctx, e.ID, notifTypeDaily)
	if e.DailyActivityEmail && strings.TrimSpace(a.Cfg.IRLTeamEmail) != "" {
		emailTo = append([]string{a.Cfg.IRLTeamEmail}, emailTo...)
	}
	if len(emailTo) == 0 && len(slackTo) == 0 {
		return
	}

	loc, err := loadLocation(e.Timezone)
	if err != nil {
		loc = time.UTC
	}
	periodKey := todayInZone(loc, now).Format(dateLayout)

	entries, err := a.activitySince(ctx, e.ID, now.Add(-24*time.Hour))
	if err != nil {
		log.Printf("WARN: digest activity for %s: %v", e.ID, err)
		return
	}
	if len(entries) == 0 {
		return // sent only on days with activity
	}
	// Claim once per event/day with a fixed sentinel recipient.
	claimed, err := a.claimReminder(ctx, e.ID, "__digest__", "daily_digest", periodKey)
	if err != nil || !claimed {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Daily activity for %s:\n\n", e.Name)
	late := 0
	for _, en := range entries {
		if en.AfterDeadline {
			late++
		}
	}
	if late > 0 {
		fmt.Fprintf(&b, "⚠ %d change(s) after the deadline.\n\n", late)
	}
	for _, en := range entries {
		flag := ""
		if en.AfterDeadline {
			flag = " [after deadline]"
		}
		fmt.Fprintf(&b, "- %s%s\n", en.Summary, flag)
	}
	subject := fmt.Sprintf("[IRL %s] daily activity digest", e.Name)
	if a.dispatch(emailTo, slackTo, subject, b.String()) > 0 {
		metrics.RemindersSentTotal.WithLabelValues("daily_digest").Inc()
	}
}

// computeDueReminders returns the reminder windows open at `now` for an event,
// gated to the event-local reminder hour. Weekly fires Mondays; the deadline
// run-up fires daily within reminderDaysBefore days of the deadline. Returns nil
// once the deadline has passed.
func computeDueReminders(e *Event, now time.Time) []reminderWindow {
	if !dueAtReminderHour(e, now) || now.After(e.SubmissionDeadline) {
		return nil
	}
	loc, err := loadLocation(e.Timezone)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	var out []reminderWindow

	if e.WeeklyReminders && local.Weekday() == time.Monday {
		y, wk := local.ISOWeek()
		out = append(out, reminderWindow{Kind: "weekly", PeriodKey: fmt.Sprintf("%d-W%02d", y, wk)})
	}

	today := todayInZone(loc, now)
	dl := e.SubmissionDeadline.In(loc)
	deadlineDate := time.Date(dl.Year(), dl.Month(), dl.Day(), 0, 0, 0, 0, time.UTC)
	daysUntil := int(deadlineDate.Sub(today).Hours() / 24)
	if e.ReminderDaysBefore > 0 && daysUntil >= 1 && daysUntil <= e.ReminderDaysBefore {
		out = append(out, reminderWindow{Kind: "deadline", PeriodKey: today.Format(dateLayout)})
	}
	return out
}

// dueAtReminderHour reports whether `now`, in the event's timezone, falls in the
// configured reminder hour.
func dueAtReminderHour(e *Event, now time.Time) bool {
	loc, err := loadLocation(e.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return now.In(loc).Hour() == e.ReminderHour
}

// claimReminder inserts an idempotency row, returning true only if THIS call
// created it (so the caller sends exactly once per window).
func (a *App) claimReminder(ctx context.Context, eventID, recipient, kind, periodKey string) (bool, error) {
	res, err := a.DB.ExecContext(ctx,
		`INSERT INTO reminder_log (event_id, recipient, reminder_kind, period_key)
		 VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		eventID, recipient, kind, periodKey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// activitySince returns activity entries newer than `since` for the digest.
func (a *App) activitySince(ctx context.Context, eventID string, since time.Time) ([]ActivityEntry, error) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT id, actor_email, subject_email, action, summary, after_deadline, created_at
		   FROM activity_log WHERE event_id = $1 AND created_at > $2 ORDER BY created_at`,
		eventID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityEntry{}
	for rows.Next() {
		var e ActivityEntry
		if err := rows.Scan(&e.ID, &e.ActorEmail, &e.SubjectEmail, &e.Action, &e.Summary,
			&e.AfterDeadline, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
