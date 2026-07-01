package server

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"
)

// Store owns the data access helpers extracted from App.
//
// Callers (handlers, reminders, MCP tools, tests) use a.Store.* directly for
// these operations. This keeps App focused on composition, HTTP concerns,
// background jobs, and collaborators (Cfg, Email, OIDC).
//
// Scope today:
//   - Read helpers and simple queries (user, event, submission, dashboard, non-responders...)
//   - Methods are unexported because everything is in the same package.
//
// Not everything is here yet:
//   - Transactional writes, attendee imports, activity logging, and several
//     ad-hoc queries still go through a.DB or the free helper functions that
//     accept db.Exec (see attendees.go, activity.go, etc.).
//   - This is a deliberate partial split; making Store a full repository layer
//     can be done later without changing the public shape much.
//
// When adding a new query helper, prefer putting it on *Store.
type Store struct {
	db *sql.DB
}

// NewStore returns a Store backed by the given pool.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// userByID is the core lookup used by auth.
func (s *Store) userByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, first_name, last_name, allergies, profile_confirmed, is_admin, created_at, token_version FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Allergies, &u.ProfileConfirmed, &u.IsAdmin, &u.CreatedAt, &u.TokenVersion)
	if err != nil {
		return nil, err
	}
	u.setDisplayName()
	return u, nil
}

// findOrCreateUser upserts a user by email (first user becomes admin, etc).
// Delegates the membership seeding to the existing package-level helper.
func (s *Store) findOrCreateUser(ctx context.Context, email, firstName, lastName, allergies string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)
	allergies = strings.TrimSpace(allergies)

	u := &User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, first_name, last_name, allergies, profile_confirmed, is_admin, created_at, token_version FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Allergies, &u.ProfileConfirmed, &u.IsAdmin, &u.CreatedAt, &u.TokenVersion)
	if err == nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, u.ID); err != nil {
			return nil, err
		}
		u.setDisplayName()
		return u, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	var inserted bool
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO users (email, first_name, last_name, allergies, is_admin, last_login_at)
		 VALUES ($1, $2, $3, $4, NOT EXISTS (SELECT 1 FROM users), now())
		 ON CONFLICT (email) DO UPDATE SET last_login_at = now()
		 RETURNING id, email, first_name, last_name, allergies, profile_confirmed, is_admin, created_at, token_version, (xmax = 0)`,
		email, firstName, lastName, allergies).
		Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Allergies, &u.ProfileConfirmed, &u.IsAdmin, &u.CreatedAt, &u.TokenVersion, &inserted)
	if err != nil {
		return nil, err
	}
	if inserted {
		if err := addUserToOpenEvents(ctx, s.db, u.ID, time.Now()); err != nil {
			log.Printf("WARN: seed default event memberships for %s: %v", u.Email, err)
		}
	}
	u.setDisplayName()
	return u, nil
}

// loadEventByColumn loads an event (and its days) by id or slug.
func (s *Store) loadEventByColumn(ctx context.Context, column, value string, now time.Time) (*Event, error) {
	e := &Event{}
	var start, end, deadline time.Time
	var imageEtag sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT e.id, e.slug, e.name, e.country, e.city, e.hotel_name, e.hotel_address, e.hotel_link, e.timezone,
		        e.start_date, e.end_date, e.submission_deadline, e.reminder_days_before,
		        e.weekly_reminders, e.reminder_hour, e.daily_activity_email,
		        e.invite_subject, e.invite_body, e.reminder_subject, e.reminder_body,
		        e.created_at, e.updated_at,
		        i.etag
		   FROM events e LEFT JOIN event_images i ON i.event_id = e.id
		  WHERE e.`+column+` = $1`, value).
		Scan(&e.ID, &e.Slug, &e.Name, &e.Country, &e.City, &e.HotelName, &e.HotelAddress, &e.HotelLink, &e.Timezone,
			&start, &end, &deadline, &e.ReminderDaysBefore,
			&e.WeeklyReminders, &e.ReminderHour, &e.DailyActivityEmail,
			&e.InviteSubject, &e.InviteBody, &e.ReminderSubject, &e.ReminderBody,
			&e.CreatedAt, &e.UpdatedAt,
			&imageEtag)
	if err != nil {
		return nil, err
	}
	e.ImageURL = eventImageURL(e.Slug, imageEtag)
	e.StartDate = start.Format(dateLayout)
	e.EndDate = end.Format(dateLayout)
	e.SubmissionDeadline = deadline.UTC()
	loc, lerr := loadLocation(e.Timezone)
	if lerr != nil {
		loc = time.UTC
	}
	e.SubmissionDeadlineLocal = formatLocalDateTime(deadline, loc)
	e.IsPast = isEventPast(end, loc, now)

	days, err := s.loadEventDays(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	e.Days = days
	return e, nil
}

func (s *Store) loadEventDays(ctx context.Context, eventID string) ([]EventDay, error) {
	rows, err := s.db.QueryContext(ctx,
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

// eventIDBySlug is a small helper for attendee routes.
func (s *Store) eventIDBySlug(ctx context.Context, slug string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM events WHERE slug = $1`, slug).Scan(&id)
	return id, err
}

// loadSubmission loads a user's submission for an event with proper null handling.
func (s *Store) loadSubmission(ctx context.Context, eventID, userID string) (*Submission, error) {
	sub := &Submission{}
	var arrivalDay, departureDay, extraStart, extraEnd sql.NullTime
	var arrivalMode, departureMode sql.NullString
	var travelCost sql.NullFloat64
	var travelCurrency sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT s.id, s.event_id, s.user_id, u.email, u.first_name, u.last_name, s.attending, s.not_sure_reason,
		        s.arrival_day, s.arrival_time, s.arrival_mode, s.arrival_details,
		        s.departure_day, s.departure_time, s.departure_mode, s.departure_details,
		        s.arrival_independent, s.departure_independent, s.long_haul, s.extra_stay_start, s.extra_stay_end,
		        s.extra_stay_self_funded, u.allergies, s.comments,
		        s.locked, s.created_at, s.updated_at, s.travel_cost, s.travel_cost_currency
		   FROM submissions s JOIN users u ON u.id = s.user_id
		  WHERE s.event_id = $1 AND s.user_id = $2`, eventID, userID).
		Scan(&sub.ID, &sub.EventID, &sub.UserID, &sub.Email, &sub.FirstName, &sub.LastName, &sub.Attending, &sub.NotSureReason,
			&arrivalDay, &sub.ArrivalTime, &arrivalMode, &sub.ArrivalDetails,
			&departureDay, &sub.DepartureTime, &departureMode, &sub.DepartureDetails,
			&sub.ArrivalIndependent, &sub.DepartureIndependent, &sub.LongHaul, &extraStart, &extraEnd,
			&sub.ExtraStaySelfFunded, &sub.Allergies, &sub.Comments,
			&sub.Locked, &sub.CreatedAt, &sub.UpdatedAt, &travelCost, &travelCurrency)
	if err != nil {
		return nil, err
	}
	sub.ArrivalDay = nullDateStr(arrivalDay)
	sub.DepartureDay = nullDateStr(departureDay)
	sub.ExtraStayStart = nullDateStr(extraStart)
	sub.ExtraStayEnd = nullDateStr(extraEnd)
	sub.ArrivalMode = nullStr(arrivalMode)
	sub.DepartureMode = nullStr(departureMode)
	if travelCost.Valid {
		sub.TravelCost = &travelCost.Float64
	}
	sub.TravelCostCurrency = travelCurrency.String
	return sub, nil
}

// nonResponders returns emails of attendees who have not submitted.
func (s *Store) nonResponders(ctx context.Context, eventID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.email FROM event_attendees ea
		   JOIN users u ON u.id = ea.user_id
		   LEFT JOIN submissions s ON s.event_id = ea.event_id AND s.user_id = ea.user_id
		  WHERE ea.event_id = $1 AND s.id IS NULL AND NOT u.archived`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// dashboardEntries returns the full attendee + submission status for a dashboard.
func (s *Store) dashboardEntries(ctx context.Context, eventID string) ([]DashboardEntry, map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.first_name, u.last_name, u.email,
		        (u.last_login_at IS NOT NULL) AS has_logged_in,
		        s.attending,
		        (s.id IS NOT NULL AND s.updated_at > e.submission_deadline) AS after_deadline_edit
		   FROM event_attendees ea
		   JOIN events e ON e.id = ea.event_id
		   JOIN users u ON u.id = ea.user_id
		   LEFT JOIN submissions s ON s.event_id = ea.event_id AND s.user_id = ea.user_id
		  WHERE ea.event_id = $1 AND NOT u.archived
		  ORDER BY u.first_name, u.last_name, u.email`, eventID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	counts := map[string]int{"yes": 0, "no": 0, "notSure": 0, "noResponse": 0}
	entries := []DashboardEntry{}
	for rows.Next() {
		var e DashboardEntry
		var first, last string
		var attending sql.NullString
		if err := rows.Scan(&e.UserID, &first, &last, &e.Email, &e.HasLoggedIn, &attending, &e.AfterDeadlineEdit); err != nil {
			return nil, nil, err
		}
		e.Name = strings.TrimSpace(first + " " + last)
		if e.Name == "" {
			e.Name = e.Email
		}
		if attending.Valid {
			e.Attending = attending.String
		} else {
			e.Attending = "no_response"
		}
		counts[countKey(e.Attending)]++
		entries = append(entries, e)
	}
	return entries, counts, rows.Err()
}

// countKey maps an attending value to its dashboard counts key.
func countKey(attending string) string {
	switch attending {
	case "yes":
		return "yes"
	case "no":
		return "no"
	case "not_sure":
		return "notSure"
	default:
		return "noResponse"
	}
}
