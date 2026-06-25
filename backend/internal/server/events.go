package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"irlplanner/internal/metrics"
)

// EventDay is one typed calendar day of an event.
type EventDay struct {
	Date string `json:"date"` // YYYY-MM-DD (event-local)
	Type string `json:"type"` // "travel" | "event"
}

// Event is the full event config returned to admins. Attendee views read the
// same shape (all fields here are non-sensitive event metadata).
type Event struct {
	ID                      string     `json:"id"`
	Slug                    string     `json:"slug"`
	Name                    string     `json:"name"`
	Country                 string     `json:"country"`
	City                    string     `json:"city"`
	HotelName               string     `json:"hotelName"`
	HotelAddress            string     `json:"hotelAddress"`
	Timezone                string     `json:"timezone"`
	StartDate               string     `json:"startDate"`               // YYYY-MM-DD
	EndDate                 string     `json:"endDate"`                 // YYYY-MM-DD
	SubmissionDeadline      time.Time  `json:"submissionDeadline"`      // UTC instant
	SubmissionDeadlineLocal string     `json:"submissionDeadlineLocal"` // wall-clock in event tz, for form prefill
	ReminderDaysBefore      int        `json:"reminderDaysBefore"`
	WeeklyReminders         bool       `json:"weeklyReminders"`
	ReminderHour            int        `json:"reminderHour"`
	DailyActivityEmail      bool       `json:"dailyActivityEmail"`
	IsPast                  bool       `json:"isPast"`
	Days                    []EventDay `json:"days"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
}

type eventReq struct {
	Slug                    string     `json:"slug"`
	Name                    string     `json:"name"`
	Country                 string     `json:"country"`
	City                    string     `json:"city"`
	HotelName               string     `json:"hotelName"`
	HotelAddress            string     `json:"hotelAddress"`
	Timezone                string     `json:"timezone"`
	StartDate               string     `json:"startDate"`
	EndDate                 string     `json:"endDate"`
	SubmissionDeadlineLocal string     `json:"submissionDeadlineLocal"`
	ReminderDaysBefore      int        `json:"reminderDaysBefore"`
	WeeklyReminders         bool       `json:"weeklyReminders"`
	ReminderHour            int        `json:"reminderHour"`
	DailyActivityEmail      bool       `json:"dailyActivityEmail"`
	Days                    []EventDay `json:"days"` // optional override of day types
}

// validateAndNormalize checks the request and returns the parsed start/end
// dates, the deadline as a UTC instant, and the resolved day list.
func (req *eventReq) validateAndNormalize() (start, end time.Time, deadlineUTC time.Time, days []EventDay, err error) {
	req.Slug = strings.TrimSpace(strings.ToLower(req.Slug))
	req.Name = strings.TrimSpace(req.Name)
	req.Timezone = strings.TrimSpace(req.Timezone)

	if !slugRe.MatchString(req.Slug) {
		return start, end, deadlineUTC, nil, errors.New("slug must be a lowercase slug, 3–64 chars, letters/digits/hyphens, starting and ending alphanumeric")
	}
	if req.Name == "" {
		return start, end, deadlineUTC, nil, errors.New("name is required")
	}
	loc, lerr := loadLocation(req.Timezone)
	if lerr != nil {
		return start, end, deadlineUTC, nil, lerr
	}
	start, err = parseDate(req.StartDate)
	if err != nil {
		return start, end, deadlineUTC, nil, err
	}
	end, err = parseDate(req.EndDate)
	if err != nil {
		return start, end, deadlineUTC, nil, err
	}
	if end.Before(start) {
		return start, end, deadlineUTC, nil, errors.New("endDate must be on or after startDate")
	}
	deadlineUTC, err = parseLocalDateTimeInZone(req.SubmissionDeadlineLocal, loc)
	if err != nil {
		return start, end, deadlineUTC, nil, err
	}
	if req.ReminderDaysBefore < 0 {
		return start, end, deadlineUTC, nil, errors.New("reminderDaysBefore must be >= 0")
	}
	if req.ReminderHour < 0 || req.ReminderHour > 23 {
		return start, end, deadlineUTC, nil, errors.New("reminderHour must be 0–23")
	}
	days = resolveDays(start, end, req.Days)
	return start, end, deadlineUTC, days, nil
}

// resolveDays builds the typed day list for [start, end]. Defaults: first and
// last day are "travel", the rest "event". Any override whose date falls in the
// range and names a valid type wins.
func resolveDays(start, end time.Time, overrides []EventDay) []EventDay {
	ov := make(map[string]string, len(overrides))
	for _, d := range overrides {
		if d.Type == "travel" || d.Type == "event" {
			ov[d.Date] = d.Type
		}
	}
	var days []EventDay
	dates := []time.Time{}
	eachDay(start, end, func(d time.Time) { dates = append(dates, d) })
	for i, d := range dates {
		key := d.Format(dateLayout)
		typ := "event"
		if i == 0 || i == len(dates)-1 {
			typ = "travel"
		}
		if o, ok := ov[key]; ok {
			typ = o
		}
		days = append(days, EventDay{Date: key, Type: typ})
	}
	return days
}

// loadEventByColumn loads an event (and its days) by a unique column (id or slug).
func (a *App) loadEventByColumn(ctx context.Context, column, value string, now time.Time) (*Event, error) {
	e := &Event{}
	var start, end, deadline time.Time
	err := a.DB.QueryRowContext(ctx,
		`SELECT id, slug, name, country, city, hotel_name, hotel_address, timezone,
		        start_date, end_date, submission_deadline, reminder_days_before,
		        weekly_reminders, reminder_hour, daily_activity_email, created_at, updated_at
		   FROM events WHERE `+column+` = $1`, value).
		Scan(&e.ID, &e.Slug, &e.Name, &e.Country, &e.City, &e.HotelName, &e.HotelAddress, &e.Timezone,
			&start, &end, &deadline, &e.ReminderDaysBefore,
			&e.WeeklyReminders, &e.ReminderHour, &e.DailyActivityEmail, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	e.StartDate = start.Format(dateLayout)
	e.EndDate = end.Format(dateLayout)
	e.SubmissionDeadline = deadline.UTC()
	loc, lerr := loadLocation(e.Timezone)
	if lerr != nil {
		loc = time.UTC
	}
	e.SubmissionDeadlineLocal = formatLocalDateTime(deadline, loc)
	e.IsPast = isEventPast(end, loc, now)

	days, err := a.loadEventDays(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	e.Days = days
	return e, nil
}

func (a *App) loadEventDays(ctx context.Context, eventID string) ([]EventDay, error) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT day_date, day_type FROM event_days WHERE event_id = $1 ORDER BY day_date`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	days := []EventDay{}
	for rows.Next() {
		var d time.Time
		var typ string
		if err := rows.Scan(&d, &typ); err != nil {
			return nil, err
		}
		days = append(days, EventDay{Date: d.Format(dateLayout), Type: typ})
	}
	return days, rows.Err()
}

// --- handlers --------------------------------------------------------------

func (a *App) handleListEvents(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "current"
	}
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT id, slug, name, country, city, timezone, start_date, end_date,
		        submission_deadline, created_at
		   FROM events ORDER BY start_date DESC`)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer rows.Close()
	now := time.Now()
	current := []Event{}
	past := []Event{}
	for rows.Next() {
		var e Event
		var start, end, deadline time.Time
		if err := rows.Scan(&e.ID, &e.Slug, &e.Name, &e.Country, &e.City, &e.Timezone,
			&start, &end, &deadline, &e.CreatedAt); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
		e.StartDate = start.Format(dateLayout)
		e.EndDate = end.Format(dateLayout)
		e.SubmissionDeadline = deadline.UTC()
		loc, lerr := loadLocation(e.Timezone)
		if lerr != nil {
			loc = time.UTC
		}
		e.IsPast = isEventPast(end, loc, now)
		if e.IsPast {
			past = append(past, e)
		} else {
			current = append(current, e)
		}
	}
	if err := rows.Err(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	switch scope {
	case "past":
		writeJSON(w, http.StatusOK, past)
	case "all":
		writeJSON(w, http.StatusOK, append(current, past...))
	default:
		writeJSON(w, http.StatusOK, current)
	}
}

func (a *App) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	var req eventReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	start, end, deadlineUTC, days, verr := req.validateAndNormalize()
	if verr != nil {
		writeErr(w, http.StatusBadRequest, verr.Error())
		return
	}
	user := currentUser(r)

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer tx.Rollback()

	var id string
	err = tx.QueryRowContext(r.Context(),
		`INSERT INTO events (slug, name, country, city, hotel_name, hotel_address, timezone,
		        start_date, end_date, submission_deadline, reminder_days_before,
		        weekly_reminders, reminder_hour, daily_activity_email, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`,
		req.Slug, req.Name, req.Country, req.City, req.HotelName, req.HotelAddress, req.Timezone,
		start, end, deadlineUTC, req.ReminderDaysBefore, req.WeeklyReminders, req.ReminderHour,
		req.DailyActivityEmail, user.ID).Scan(&id)
	if err != nil {
		metrics.EventMutationsTotal.WithLabelValues("create", "error").Inc()
		if isUniqueViolation(err) {
			writeErr(w, http.StatusConflict, "an event with that slug already exists")
			return
		}
		serverErr(w, r, err, "db error")
		return
	}
	if err := insertDays(r.Context(), tx, id, days); err != nil {
		metrics.EventMutationsTotal.WithLabelValues("create", "error").Inc()
		serverErr(w, r, err, "db error")
		return
	}
	if err := tx.Commit(); err != nil {
		metrics.EventMutationsTotal.WithLabelValues("create", "error").Inc()
		serverErr(w, r, err, "db error")
		return
	}
	metrics.EventMutationsTotal.WithLabelValues("create", "success").Inc()

	e, err := a.loadEventByColumn(r.Context(), "id", id, time.Now())
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (a *App) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	e, err := a.loadEventByColumn(r.Context(), "id", id, time.Now())
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (a *App) handleUpdateEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req eventReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	start, end, deadlineUTC, days, verr := req.validateAndNormalize()
	if verr != nil {
		writeErr(w, http.StatusBadRequest, verr.Error())
		return
	}

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(r.Context(),
		`UPDATE events SET slug=$1, name=$2, country=$3, city=$4, hotel_name=$5, hotel_address=$6,
		        timezone=$7, start_date=$8, end_date=$9, submission_deadline=$10,
		        reminder_days_before=$11, weekly_reminders=$12, reminder_hour=$13,
		        daily_activity_email=$14, updated_at=now()
		   WHERE id=$15`,
		req.Slug, req.Name, req.Country, req.City, req.HotelName, req.HotelAddress, req.Timezone,
		start, end, deadlineUTC, req.ReminderDaysBefore, req.WeeklyReminders, req.ReminderHour,
		req.DailyActivityEmail, id)
	if err != nil {
		metrics.EventMutationsTotal.WithLabelValues("update", "error").Inc()
		if isUniqueViolation(err) {
			writeErr(w, http.StatusConflict, "an event with that slug already exists")
			return
		}
		serverErr(w, r, err, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	// Replace the day list (range or types may have changed).
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM event_days WHERE event_id = $1`, id); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if err := insertDays(r.Context(), tx, id, days); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if err := tx.Commit(); err != nil {
		metrics.EventMutationsTotal.WithLabelValues("update", "error").Inc()
		serverErr(w, r, err, "db error")
		return
	}
	metrics.EventMutationsTotal.WithLabelValues("update", "success").Inc()

	e, err := a.loadEventByColumn(r.Context(), "id", id, time.Now())
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleGetEventBySlug is the attendee-facing event read (the shareable URL).
func (a *App) handleGetEventBySlug(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(chi.URLParam(r, "slug"))
	e, err := a.loadEventByColumn(r.Context(), "slug", slug, time.Now())
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func insertDays(ctx context.Context, tx *sql.Tx, eventID string, days []EventDay) error {
	for _, d := range days {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO event_days (event_id, day_date, day_type) VALUES ($1, $2, $3)`,
			eventID, d.Date, d.Type); err != nil {
			return err
		}
	}
	return nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505), surfaced through pgx's error string.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "23505") || strings.Contains(s, "duplicate key")
}
