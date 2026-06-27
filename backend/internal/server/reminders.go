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
	if !a.Email.Configured() {
		log.Printf("WARN: reminders enabled but SMTP not configured — reminders/digests will be skipped until SMTP_HOST is set")
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

// runReminderTick processes every open event once. Email sends are skipped
// (nothing is claimed) when SMTP is unconfigured, so they fire once it is.
func (a *App) runReminderTick(ctx context.Context, now time.Time) {
	if !a.Email.Configured() {
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
	nonResponders, err := a.Store.nonResponders(ctx, e.ID)
	if err != nil {
		log.Printf("WARN: reminder: non-responders for %s: %v", e.ID, err)
		return
	}
	link := strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/events/" + e.Slug
	for _, win := range windows {
		for _, email := range nonResponders {
			claimed, err := a.claimReminder(ctx, e.ID, email, win.Kind, win.PeriodKey)
			if err != nil {
				log.Printf("WARN: reminder claim: %v", err)
				continue
			}
			if !claimed {
				continue // already sent for this window
			}
			subject := fmt.Sprintf("Reminder: please respond for %s", e.Name)
			body := fmt.Sprintf("Hi,\n\nWe haven't received your attendance details for %s yet.\n"+
				"Please respond here: %s\n\nThanks,\nThe People team\n", e.Name, link)
			if err := a.Email.Send([]string{email}, subject, body); err != nil {
				log.Printf("WARN: reminder send to %s: %v", email, err)
				continue
			}
			metrics.RemindersSentTotal.WithLabelValues(win.Kind).Inc()
		}
	}
}

func (a *App) processEventDigest(ctx context.Context, e *Event, now time.Time) {
	if !e.DailyActivityEmail || !dueAtReminderHour(e, now) {
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
	recipients := a.recipients(ctx)
	if len(recipients) == 0 {
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
	if err := a.Email.Send(recipients, subject, b.String()); err != nil {
		log.Printf("WARN: digest send for %s: %v", e.ID, err)
		return
	}
	metrics.RemindersSentTotal.WithLabelValues("daily_digest").Inc()
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
