package server

import (
	"fmt"
	"time"
)

// dateLayout is the event-local calendar-date format used on the wire and for
// DATE columns ("2006-01-02").
const dateLayout = "2006-01-02"

// localDateTimeLayout is how the client sends a wall-clock date-time the backend
// must interpret in the event's timezone (no offset — the zone supplies it).
const localDateTimeLayout = "2006-01-02T15:04"

// loadLocation resolves an IANA timezone name to a *time.Location, returning a
// clear error for an unknown zone so handlers can map it to a 400.
func loadLocation(tz string) (*time.Location, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	return loc, nil
}

// parseLocalDateTimeInZone interprets a wall-clock "2006-01-02T15:04" in loc and
// returns the corresponding UTC instant for storage.
func parseLocalDateTimeInZone(s string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation(localDateTimeLayout, s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date-time %q (expected YYYY-MM-DDTHH:MM): %w", s, err)
	}
	return t.UTC(), nil
}

// formatLocalDateTime renders a UTC instant as a wall-clock string in loc, for
// prefilling the admin edit form.
func formatLocalDateTime(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(localDateTimeLayout)
}

// companyTimezone is the fixed reference zone for all human-facing deadline
// display (email + the SPA). The company is international, so deadlines are shown
// in one consistent zone — HQ time — rather than each viewer's local time.
const companyTimezone = "Europe/Paris"

// deadlineDisplayLayout renders an instant for human-facing copy: a long US date
// (spelled-out month) and 12-hour US time, e.g. "July 3, 2026 at 2:00 AM". The
// zone is appended separately as a fixed label so it matches the SPA exactly.
const deadlineDisplayLayout = "January 2, 2006 at 3:04 PM"

// formatDeadline renders a deadline instant in the company timezone with an
// explicit label, e.g. "July 3, 2026 at 2:00 AM (Europe/Paris)". Recipients in
// any zone read the same unambiguous, clearly-labelled time.
func formatDeadline(t time.Time) string {
	loc, err := loadLocation(companyTimezone)
	if err != nil {
		loc = time.UTC
	}
	return t.In(loc).Format(deadlineDisplayLayout) + " (" + companyTimezone + ")"
}

// parseDate parses an event-local calendar date ("2006-01-02").
func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (expected YYYY-MM-DD): %w", s, err)
	}
	return t, nil
}

// todayInZone returns today's calendar date (year, month, day) as seen in loc,
// normalised to midnight UTC so it compares cleanly against parsed DATE values.
func todayInZone(loc *time.Location, now time.Time) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// isEventPast reports whether the event's last day is before today in its zone.
func isEventPast(endDate time.Time, loc *time.Location, now time.Time) bool {
	end := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 0, 0, 0, 0, time.UTC)
	return end.Before(todayInZone(loc, now))
}

// eachDay calls fn for every calendar date in [start, end] inclusive.
func eachDay(start, end time.Time, fn func(d time.Time)) {
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		fn(d)
	}
}
