package server

import (
	"testing"
	"time"
)

func reminderEvent() (*Event, *time.Location) {
	loc, _ := loadLocation("Europe/Paris")
	return &Event{
		Timezone:           "Europe/Paris",
		ReminderHour:       9,
		WeeklyReminders:    true,
		ReminderDaysBefore: 3,
		SubmissionDeadline: time.Date(2026, 10, 16, 17, 0, 0, 0, loc).UTC(),
	}, loc
}

func kinds(ws []reminderWindow) map[string]string {
	m := map[string]string{}
	for _, w := range ws {
		m[w.Kind] = w.PeriodKey
	}
	return m
}

func TestWeeklyFiresMondayAtReminderHour(t *testing.T) {
	e, loc := reminderEvent()
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, loc) // Monday 09:00 Paris
	got := kinds(computeDueReminders(e, now))
	if _, ok := got["weekly"]; !ok {
		t.Fatalf("expected a weekly window on Monday 9am, got %+v", got)
	}
	if _, ok := got["deadline"]; ok {
		t.Errorf("deadline window should not fire 11 days out: %+v", got)
	}
}

func TestNothingOutsideReminderHour(t *testing.T) {
	e, loc := reminderEvent()
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, loc) // 10:00, not the reminder hour
	if ws := computeDueReminders(e, now); len(ws) != 0 {
		t.Fatalf("expected nothing at 10:00, got %+v", ws)
	}
}

func TestDeadlineRunUpFiresWithinWindow(t *testing.T) {
	e, loc := reminderEvent()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, loc) // Wed, 2 days before deadline
	got := kinds(computeDueReminders(e, now))
	if key, ok := got["deadline"]; !ok || key != "2026-10-14" {
		t.Fatalf("expected deadline window keyed 2026-10-14, got %+v", got)
	}
	if _, ok := got["weekly"]; ok {
		t.Errorf("weekly should not fire on a Wednesday: %+v", got)
	}
}

func TestNothingAfterDeadline(t *testing.T) {
	e, loc := reminderEvent()
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, loc) // day after deadline
	if ws := computeDueReminders(e, now); len(ws) != 0 {
		t.Fatalf("expected no reminders after the deadline, got %+v", ws)
	}
}

func TestWeeklyDisabled(t *testing.T) {
	e, loc := reminderEvent()
	e.WeeklyReminders = false
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, loc) // Monday
	if _, ok := kinds(computeDueReminders(e, now))["weekly"]; ok {
		t.Fatal("weekly disabled but a weekly window fired")
	}
}

func TestDueAtReminderHourTimezone(t *testing.T) {
	e, _ := reminderEvent()
	// 07:00 UTC == 09:00 Paris (CEST) in October.
	now := time.Date(2026, 10, 5, 7, 0, 0, 0, time.UTC)
	if !dueAtReminderHour(e, now) {
		t.Error("07:00 UTC should be 09:00 Paris and match the reminder hour")
	}
}
