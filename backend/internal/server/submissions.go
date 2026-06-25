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

// Submission is the API shape of an attendee's response. Nullable columns are
// pointers so "not provided" round-trips as JSON null.
type Submission struct {
	ID            string `json:"id"`
	EventID       string `json:"eventId"`
	UserID        string `json:"userId"`
	Email         string `json:"email"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	Attending     string `json:"attending"`
	NotSureReason string `json:"notSureReason"`

	ArrivalDay       *string `json:"arrivalDay"`
	ArrivalTime      string  `json:"arrivalTime"`
	ArrivalMode      *string `json:"arrivalMode"`
	ArrivalDetails   string  `json:"arrivalDetails"`
	DepartureDay     *string `json:"departureDay"`
	DepartureTime    string  `json:"departureTime"`
	DepartureMode    *string `json:"departureMode"`
	DepartureDetails string  `json:"departureDetails"`

	LongHaul       bool    `json:"longHaul"`
	ExtraStayStart *string `json:"extraStayStart"`
	ExtraStayEnd   *string `json:"extraStayEnd"`

	Allergies string `json:"allergies"`
	Comments  string `json:"comments"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// submissionReq is the create/update payload (the writable subset of Submission).
// The attendee's name is not part of it — it lives on the user profile.
type submissionReq struct {
	Attending        string  `json:"attending"`
	NotSureReason    string  `json:"notSureReason"`
	ArrivalDay       *string `json:"arrivalDay"`
	ArrivalTime      string  `json:"arrivalTime"`
	ArrivalMode      *string `json:"arrivalMode"`
	ArrivalDetails   string  `json:"arrivalDetails"`
	DepartureDay     *string `json:"departureDay"`
	DepartureTime    string  `json:"departureTime"`
	DepartureMode    *string `json:"departureMode"`
	DepartureDetails string  `json:"departureDetails"`
	LongHaul         bool    `json:"longHaul"`
	ExtraStayStart   *string `json:"extraStayStart"`
	ExtraStayEnd     *string `json:"extraStayEnd"`
	Allergies        string  `json:"allergies"`
	Comments         string  `json:"comments"`
}

var validTravelModes = map[string]bool{"flight": true, "car": true, "train": true, "other": true}

// normalizeAndValidate enforces the conditional form rules (DESIGN.md §8) and
// blanks fields outside the chosen branch. isAdmin relaxes the one-day extra-
// night cap and the arrival/departure date window. It mutates req in place.
func (req *submissionReq) normalizeAndValidate(e *Event, isAdmin bool) error {
	switch req.Attending {
	case "yes", "no", "not_sure":
	default:
		return errors.New("attending must be yes, no, or not_sure")
	}

	if req.Attending != "not_sure" {
		req.NotSureReason = ""
	} else if strings.TrimSpace(req.NotSureReason) == "" {
		return errors.New("a reason is required when you're not sure")
	}

	if req.Attending != "yes" {
		// Clear the whole travel/other block on No / Not sure.
		req.ArrivalDay, req.ArrivalMode, req.DepartureDay, req.DepartureMode = nil, nil, nil, nil
		req.ArrivalTime, req.ArrivalDetails, req.DepartureTime, req.DepartureDetails = "", "", "", ""
		req.LongHaul = false
		req.ExtraStayStart, req.ExtraStayEnd = nil, nil
		req.Allergies, req.Comments = "", ""
		return nil
	}

	// attending == yes
	start, _ := parseDate(e.StartDate)
	end, _ := parseDate(e.EndDate)

	if err := validateTravelLeg("arrival", &req.ArrivalDay, &req.ArrivalMode, &req.ArrivalDetails, start, end, isAdmin); err != nil {
		return err
	}
	if err := validateTravelLeg("departure", &req.DepartureDay, &req.DepartureMode, &req.DepartureDetails, start, end, isAdmin); err != nil {
		return err
	}

	if !req.LongHaul {
		req.ExtraStayStart, req.ExtraStayEnd = nil, nil
	} else {
		if err := validateExtraStay(&req.ExtraStayStart, &req.ExtraStayEnd, start, end, isAdmin); err != nil {
			return err
		}
	}
	return nil
}

// validateTravelLeg checks one arrival/departure leg: a day and mode are
// required; details are required once a mode is set. The day must fall in the
// allowed window (event range ±1 day for employees, unrestricted for admins).
func validateTravelLeg(label string, day **string, mode **string, details *string, start, end time.Time, isAdmin bool) error {
	if *day == nil || strings.TrimSpace(**day) == "" {
		return errors.New(label + " day is required")
	}
	d, err := parseDate(strings.TrimSpace(**day))
	if err != nil {
		return errors.New(label + " day is invalid")
	}
	if !isAdmin {
		lo := start.AddDate(0, 0, -1)
		hi := end.AddDate(0, 0, 1)
		if d.Before(lo) || d.After(hi) {
			return errors.New(label + " day must be within the event dates")
		}
	}
	canon := d.Format(dateLayout)
	*day = &canon

	if *mode == nil || strings.TrimSpace(**mode) == "" {
		return errors.New(label + " travel mode is required")
	}
	m := strings.TrimSpace(**mode)
	if !validTravelModes[m] {
		return errors.New(label + " travel mode is invalid")
	}
	*mode = &m
	if strings.TrimSpace(*details) == "" {
		return errors.New(label + " details are required (flight number or other travel info)")
	}
	return nil
}

// validateExtraStay enforces the stay-window bounds. Employees may add at most
// one night before the first day and/or one after the last; admins may set any
// earlier start / later end for special cases.
func validateExtraStay(startPtr, endPtr **string, start, end time.Time, isAdmin bool) error {
	if *startPtr != nil && strings.TrimSpace(**startPtr) != "" {
		d, err := parseDate(strings.TrimSpace(**startPtr))
		if err != nil {
			return errors.New("extra-night (before) date is invalid")
		}
		if !d.Before(start) {
			return errors.New("the extra night before must be earlier than the first day")
		}
		if !isAdmin && !d.Equal(start.AddDate(0, 0, -1)) {
			return errors.New("you can add at most one extra night before the event")
		}
		canon := d.Format(dateLayout)
		*startPtr = &canon
	} else {
		*startPtr = nil
	}
	if *endPtr != nil && strings.TrimSpace(**endPtr) != "" {
		d, err := parseDate(strings.TrimSpace(**endPtr))
		if err != nil {
			return errors.New("extra-night (after) date is invalid")
		}
		if !d.After(end) {
			return errors.New("the extra night after must be later than the last day")
		}
		if !isAdmin && !d.Equal(end.AddDate(0, 0, 1)) {
			return errors.New("you can add at most one extra night after the event")
		}
		canon := d.Format(dateLayout)
		*endPtr = &canon
	} else {
		*endPtr = nil
	}
	return nil
}

// --- read ------------------------------------------------------------------

func (a *App) handleGetMySubmission(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(chi.URLParam(r, "slug"))
	user := currentUser(r)
	eventID, err := a.eventIDBySlug(r.Context(), slug)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	sub, err := a.loadSubmission(r.Context(), eventID, user.ID)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "no submission yet")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// --- write (employee, own submission) --------------------------------------

func (a *App) handlePutMySubmission(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(chi.URLParam(r, "slug"))
	user := currentUser(r)

	e, err := a.loadEventByColumn(r.Context(), "slug", slug, time.Now())
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	// Employees can't edit a past event (admins use the admin edit path).
	if e.IsPast {
		writeErr(w, http.StatusForbidden, "this event has ended and can no longer be edited")
		return
	}
	a.writeSubmission(w, r, e, user.ID, user, false)
}

// --- write (admin, any attendee's submission) ------------------------------

func (a *App) handleAdminUpdateSubmission(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	targetUserID := chi.URLParam(r, "userId")

	e, err := a.loadEventByColumn(r.Context(), "id", eventID, time.Now())
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	a.writeSubmission(w, r, e, targetUserID, currentUser(r), true)
}

// writeSubmission is the shared upsert path for both the employee and admin
// writes. actor is whoever is making the change; isAdmin relaxes validation and
// selects the admin-edit activity action.
func (a *App) writeSubmission(w http.ResponseWriter, r *http.Request, e *Event, ownerID string, actor *User, isAdmin bool) {
	var req submissionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := req.normalizeAndValidate(e, isAdmin); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer tx.Rollback()

	// Owner must exist (admin edit targets an arbitrary user id). The name is read
	// from the profile here — it drives the activity summary below.
	var ownerEmail, ownerFirst, ownerLast string
	if err := tx.QueryRowContext(ctx,
		`SELECT email, first_name, last_name FROM users WHERE id = $1`, ownerID).
		Scan(&ownerEmail, &ownerFirst, &ownerLast); err != nil {
		if err == sql.ErrNoRows {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
		serverErr(w, r, err, "db error")
		return
	}
	ownerName := strings.TrimSpace(ownerFirst + " " + ownerLast)
	if ownerName == "" {
		ownerName = ownerEmail
	}

	// Was there a prior submission? (create vs update + attending-change detect)
	var prevAttending string
	existed := true
	err = tx.QueryRowContext(ctx,
		`SELECT attending FROM submissions WHERE event_id = $1 AND user_id = $2`, e.ID, ownerID).
		Scan(&prevAttending)
	if err == sql.ErrNoRows {
		existed = false
	} else if err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	var subID string
	err = tx.QueryRowContext(ctx,
		`INSERT INTO submissions (event_id, user_id, attending, not_sure_reason,
		   arrival_day, arrival_time, arrival_mode, arrival_details,
		   departure_day, departure_time, departure_mode, departure_details,
		   long_haul, extra_stay_start, extra_stay_end, allergies, comments)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		 ON CONFLICT (event_id, user_id) DO UPDATE SET
		   attending=EXCLUDED.attending,
		   not_sure_reason=EXCLUDED.not_sure_reason, arrival_day=EXCLUDED.arrival_day,
		   arrival_time=EXCLUDED.arrival_time, arrival_mode=EXCLUDED.arrival_mode,
		   arrival_details=EXCLUDED.arrival_details, departure_day=EXCLUDED.departure_day,
		   departure_time=EXCLUDED.departure_time, departure_mode=EXCLUDED.departure_mode,
		   departure_details=EXCLUDED.departure_details, long_haul=EXCLUDED.long_haul,
		   extra_stay_start=EXCLUDED.extra_stay_start, extra_stay_end=EXCLUDED.extra_stay_end,
		   allergies=EXCLUDED.allergies, comments=EXCLUDED.comments, updated_at=now()
		 RETURNING id`,
		e.ID, ownerID, req.Attending, req.NotSureReason,
		datePtr(req.ArrivalDay), req.ArrivalTime, strPtr(req.ArrivalMode), req.ArrivalDetails,
		datePtr(req.DepartureDay), req.DepartureTime, strPtr(req.DepartureMode), req.DepartureDetails,
		req.LongHaul, datePtr(req.ExtraStayStart), datePtr(req.ExtraStayEnd), req.Allergies, req.Comments).
		Scan(&subID)
	if err != nil {
		metrics.SubmissionMutationsTotal.WithLabelValues("write", "error").Inc()
		serverErr(w, r, err, "db error")
		return
	}

	// Snapshot the new state for the revision history. The snapshot is cast to
	// jsonb explicitly because pgx sends a Go string as text, which Postgres
	// won't implicitly coerce into a jsonb column.
	snapshot, _ := json.Marshal(req)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO submission_revisions (submission_id, user_id, snapshot) VALUES ($1,$2,$3::jsonb)`,
		subID, actor.ID, string(snapshot)); err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	// Activity log. after_deadline is stamped when the change lands past the
	// event's submission deadline — the flag the admin timeline highlights.
	afterDeadline := time.Now().After(e.SubmissionDeadline)
	action, summary := submissionActivity(existed, isAdmin, ownerName, actor, req, prevAttending)
	actorID := actor.ID
	if err := a.logActivity(ctx, tx, e.ID, &actorID, actor.Email, ownerEmail, action, summary, nil, afterDeadline); err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	if err := tx.Commit(); err != nil {
		metrics.SubmissionMutationsTotal.WithLabelValues("write", "error").Inc()
		serverErr(w, r, err, "db error")
		return
	}
	label := "create"
	if existed {
		label = "update"
	}
	if isAdmin {
		label = "admin_edit"
	}
	metrics.SubmissionMutationsTotal.WithLabelValues(label, "success").Inc()

	// Notify admins on an edit of an existing submission (best-effort, async).
	a.notifySubmissionChanged(e, ownerEmail, actor, existed, summary)

	sub, err := a.loadSubmission(ctx, e.ID, ownerID)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// submissionActivity builds the action code and human-readable summary line.
// who is the submission owner's profile display name (falls back to their email
// when the profile name is blank).
func submissionActivity(existed, isAdmin bool, who string, actor *User, req submissionReq, prevAttending string) (string, string) {
	switch {
	case isAdmin:
		return actionAdminEditedSubmission,
			actor.Email + " edited " + who + "'s response (attending: " + attendingLabel(req.Attending) + ")"
	case !existed:
		return actionSubmissionCreated,
			who + " responded: " + attendingLabel(req.Attending)
	case prevAttending != req.Attending:
		return actionSubmissionUpdated,
			who + " changed attending from " + attendingLabel(prevAttending) + " to " + attendingLabel(req.Attending)
	default:
		return actionSubmissionUpdated, who + " updated their response"
	}
}

func attendingLabel(a string) string {
	switch a {
	case "yes":
		return "Yes"
	case "no":
		return "No"
	case "not_sure":
		return "Not sure"
	default:
		return a
	}
}

// --- helpers ---------------------------------------------------------------

func (a *App) eventIDBySlug(ctx context.Context, slug string) (string, error) {
	var id string
	err := a.DB.QueryRowContext(ctx, `SELECT id FROM events WHERE slug = $1`, slug).Scan(&id)
	return id, err
}

func (a *App) loadSubmission(ctx context.Context, eventID, userID string) (*Submission, error) {
	s := &Submission{}
	var arrivalDay, departureDay, extraStart, extraEnd sql.NullTime
	var arrivalMode, departureMode sql.NullString
	err := a.DB.QueryRowContext(ctx,
		`SELECT s.id, s.event_id, s.user_id, u.email, u.first_name, u.last_name, s.attending, s.not_sure_reason,
		        s.arrival_day, s.arrival_time, s.arrival_mode, s.arrival_details,
		        s.departure_day, s.departure_time, s.departure_mode, s.departure_details,
		        s.long_haul, s.extra_stay_start, s.extra_stay_end, s.allergies, s.comments,
		        s.created_at, s.updated_at
		   FROM submissions s JOIN users u ON u.id = s.user_id
		  WHERE s.event_id = $1 AND s.user_id = $2`, eventID, userID).
		Scan(&s.ID, &s.EventID, &s.UserID, &s.Email, &s.FirstName, &s.LastName, &s.Attending, &s.NotSureReason,
			&arrivalDay, &s.ArrivalTime, &arrivalMode, &s.ArrivalDetails,
			&departureDay, &s.DepartureTime, &departureMode, &s.DepartureDetails,
			&s.LongHaul, &extraStart, &extraEnd, &s.Allergies, &s.Comments,
			&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.ArrivalDay = nullDateStr(arrivalDay)
	s.DepartureDay = nullDateStr(departureDay)
	s.ExtraStayStart = nullDateStr(extraStart)
	s.ExtraStayEnd = nullDateStr(extraEnd)
	s.ArrivalMode = nullStr(arrivalMode)
	s.DepartureMode = nullStr(departureMode)
	return s, nil
}

// datePtr converts an optional canonical date string to a time.Time for a DATE
// column (nil → SQL NULL). Passing a time.Time (not a string) lets pgx bind the
// correct DATE type; a bare string would be sent as text and rejected by the
// column without an explicit cast.
func datePtr(s *string) interface{} {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t, err := parseDate(strings.TrimSpace(*s))
	if err != nil {
		return nil
	}
	return t
}

func strPtr(s *string) interface{} {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

func nullDateStr(t sql.NullTime) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.Format(dateLayout)
	return &s
}

func nullStr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}
