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

	// ArrivalIndependent / DepartureIndependent: the attendee self-arranges that
	// leg and wants no support, so its fields are blank. The two legs are
	// independent; when both are set the long-haul/accommodation block is blank too.
	ArrivalIndependent   bool `json:"arrivalIndependent"`
	DepartureIndependent bool `json:"departureIndependent"`

	LongHaul       bool    `json:"longHaul"`
	ExtraStayStart *string `json:"extraStayStart"`
	ExtraStayEnd   *string `json:"extraStayEnd"`

	// Allergies is read-only here: it lives on the submitter's profile (see
	// migration 0003_profile_allergies) and is joined in for display.
	Allergies string `json:"allergies"`
	Comments  string `json:"comments"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// submissionReq is the create/update payload (the writable subset of Submission).
// The attendee's name and allergies are not part of it — they live on the user
// profile.
type submissionReq struct {
	Attending            string  `json:"attending"`
	NotSureReason        string  `json:"notSureReason"`
	ArrivalDay           *string `json:"arrivalDay"`
	ArrivalTime          string  `json:"arrivalTime"`
	ArrivalMode          *string `json:"arrivalMode"`
	ArrivalDetails       string  `json:"arrivalDetails"`
	DepartureDay         *string `json:"departureDay"`
	DepartureTime        string  `json:"departureTime"`
	DepartureMode        *string `json:"departureMode"`
	DepartureDetails     string  `json:"departureDetails"`
	ArrivalIndependent   bool    `json:"arrivalIndependent"`
	DepartureIndependent bool    `json:"departureIndependent"`
	LongHaul             bool    `json:"longHaul"`
	ExtraStayStart       *string `json:"extraStayStart"`
	ExtraStayEnd         *string `json:"extraStayEnd"`
	Comments             string  `json:"comments"`
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
		req.ArrivalIndependent, req.DepartureIndependent = false, false
		req.LongHaul = false
		req.ExtraStayStart, req.ExtraStayEnd = nil, nil
		req.Comments = ""
		return nil
	}

	start, _ := parseDate(e.StartDate)
	end, _ := parseDate(e.EndDate)

	// Each leg is independent: a self-arranged leg is blanked and not validated;
	// otherwise the leg's day/mode/details are required.
	if req.ArrivalIndependent {
		req.ArrivalDay, req.ArrivalMode, req.ArrivalTime, req.ArrivalDetails = nil, nil, "", ""
	} else if err := validateTravelLeg("arrival", &req.ArrivalDay, &req.ArrivalMode, &req.ArrivalTime, &req.ArrivalDetails, start, end, isAdmin); err != nil {
		return err
	}
	if req.DepartureIndependent {
		req.DepartureDay, req.DepartureMode, req.DepartureTime, req.DepartureDetails = nil, nil, "", ""
	} else if err := validateTravelLeg("departure", &req.DepartureDay, &req.DepartureMode, &req.DepartureTime, &req.DepartureDetails, start, end, isAdmin); err != nil {
		return err
	}

	// Long-haul accommodation only applies when the People team handles at least
	// one leg; a fully self-arranging attendee gets no accommodation block (this
	// is the old single-flag behavior, now keyed on both legs being independent).
	if req.ArrivalIndependent && req.DepartureIndependent {
		req.LongHaul = false
		req.ExtraStayStart, req.ExtraStayEnd = nil, nil
	} else if !req.LongHaul {
		req.ExtraStayStart, req.ExtraStayEnd = nil, nil
	} else if err := validateExtraStay(&req.ExtraStayStart, &req.ExtraStayEnd, start, end, isAdmin); err != nil {
		return err
	}
	return nil
}

// validateTravelLeg checks one arrival/departure leg: a day and mode are
// required. The day must fall in the allowed window (event range ±1 day for
// employees, unrestricted for admins). For a flight, the time and details
// (flight number) are also required so the People team can track the flight; for
// every other mode both stay optional.
func validateTravelLeg(label string, day **string, mode **string, travelTime *string, details *string, start, end time.Time, isAdmin bool) error {
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

	if m == "flight" {
		if strings.TrimSpace(*travelTime) == "" {
			return errors.New(label + " flight time is required")
		}
		if strings.TrimSpace(*details) == "" {
			return errors.New(label + " flight number is required")
		}
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
	eventID, err := a.Store.eventIDBySlug(r.Context(), slug)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	sub, err := a.Store.loadSubmission(r.Context(), eventID, user.ID)
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

	e, err := a.Store.loadEventByColumn(r.Context(), "slug", slug, time.Now())
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

	e, err := a.Store.loadEventByColumn(r.Context(), "id", eventID, time.Now())
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

// errSubmissionInvalid wraps a conditional-form validation failure so callers
// can map it to a 400 / bad-input response. Everything else applySubmission
// returns is a server/db error.
type errSubmissionInvalid struct{ err error }

func (e errSubmissionInvalid) Error() string { return e.err.Error() }
func (e errSubmissionInvalid) Unwrap() error { return e.err }

// errSubmissionOwnerNotFound means the target user id has no users row.
var errSubmissionOwnerNotFound = errors.New("user not found")

// writeSubmission is the shared HTTP upsert path for both the employee and admin
// writes. actor is whoever is making the change; isAdmin relaxes validation and
// selects the admin-edit activity action. It decodes the request, delegates the
// transactional work to applySubmission, and maps its error sentinels to status
// codes.
func (a *App) writeSubmission(w http.ResponseWriter, r *http.Request, e *Event, ownerID string, actor *User, isAdmin bool) {
	var req submissionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	sub, err := a.applySubmission(r.Context(), e, &req, ownerID, actor, isAdmin)
	if err != nil {
		var inv errSubmissionInvalid
		switch {
		case errors.As(err, &inv):
			writeErr(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, errSubmissionOwnerNotFound):
			writeErr(w, http.StatusNotFound, "user not found")
		default:
			serverErr(w, r, err, "db error")
		}
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// applySubmission is the transport-agnostic core of a submission upsert, shared
// by the HTTP handlers (writeSubmission) and the MCP submit_response tool. It
// validates req in place, performs the upsert + revision snapshot + attendee
// link + activity log in one transaction, fires the best-effort admin
// notification, and returns the persisted submission. actor is whoever is making
// the change; isAdmin relaxes validation and selects the admin-edit activity
// action. Validation failures come back as errSubmissionInvalid and a missing
// owner as errSubmissionOwnerNotFound; any other error is a db/server error.
func (a *App) applySubmission(ctx context.Context, e *Event, req *submissionReq, ownerID string, actor *User, isAdmin bool) (*Submission, error) {
	if err := req.normalizeAndValidate(e, isAdmin); err != nil {
		return nil, errSubmissionInvalid{err}
	}

	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Owner must exist (admin edit targets an arbitrary user id). The name is read
	// from the profile here — it drives the activity summary below.
	var ownerEmail, ownerFirst, ownerLast string
	if err := tx.QueryRowContext(ctx,
		`SELECT email, first_name, last_name FROM users WHERE id = $1`, ownerID).
		Scan(&ownerEmail, &ownerFirst, &ownerLast); err != nil {
		if err == sql.ErrNoRows {
			return nil, errSubmissionOwnerNotFound
		}
		return nil, err
	}
	ownerName := strings.TrimSpace(ownerFirst + " " + ownerLast)
	if ownerName == "" {
		ownerName = ownerEmail
	}

	// Prior submission state, for create-vs-update detection and the field-level
	// diff recorded in the activity detail.
	var prev submissionReq
	existed := true
	var pArrDay, pDepDay, pExtraStart, pExtraEnd sql.NullTime
	var pArrMode, pDepMode sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT attending, not_sure_reason, arrival_day, arrival_time, arrival_mode, arrival_details,
		        departure_day, departure_time, departure_mode, departure_details,
		        arrival_independent, departure_independent, long_haul, extra_stay_start, extra_stay_end, comments
		   FROM submissions WHERE event_id = $1 AND user_id = $2`, e.ID, ownerID).
		Scan(&prev.Attending, &prev.NotSureReason, &pArrDay, &prev.ArrivalTime, &pArrMode, &prev.ArrivalDetails,
			&pDepDay, &prev.DepartureTime, &pDepMode, &prev.DepartureDetails,
			&prev.ArrivalIndependent, &prev.DepartureIndependent, &prev.LongHaul, &pExtraStart, &pExtraEnd, &prev.Comments)
	if err == sql.ErrNoRows {
		existed = false
	} else if err != nil {
		return nil, err
	}
	prev.ArrivalDay = nullDateStr(pArrDay)
	prev.DepartureDay = nullDateStr(pDepDay)
	prev.ExtraStayStart = nullDateStr(pExtraStart)
	prev.ExtraStayEnd = nullDateStr(pExtraEnd)
	prev.ArrivalMode = nullStr(pArrMode)
	prev.DepartureMode = nullStr(pDepMode)

	var subID string
	err = tx.QueryRowContext(ctx,
		`INSERT INTO submissions (event_id, user_id, attending, not_sure_reason,
		   arrival_day, arrival_time, arrival_mode, arrival_details,
		   departure_day, departure_time, departure_mode, departure_details,
		   arrival_independent, departure_independent, long_haul, extra_stay_start, extra_stay_end, comments)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		 ON CONFLICT (event_id, user_id) DO UPDATE SET
		   attending=EXCLUDED.attending,
		   not_sure_reason=EXCLUDED.not_sure_reason, arrival_day=EXCLUDED.arrival_day,
		   arrival_time=EXCLUDED.arrival_time, arrival_mode=EXCLUDED.arrival_mode,
		   arrival_details=EXCLUDED.arrival_details, departure_day=EXCLUDED.departure_day,
		   departure_time=EXCLUDED.departure_time, departure_mode=EXCLUDED.departure_mode,
		   departure_details=EXCLUDED.departure_details,
		   arrival_independent=EXCLUDED.arrival_independent,
		   departure_independent=EXCLUDED.departure_independent, long_haul=EXCLUDED.long_haul,
		   extra_stay_start=EXCLUDED.extra_stay_start, extra_stay_end=EXCLUDED.extra_stay_end,
		   comments=EXCLUDED.comments, updated_at=now()
		 RETURNING id`,
		e.ID, ownerID, req.Attending, req.NotSureReason,
		datePtr(req.ArrivalDay), req.ArrivalTime, strPtr(req.ArrivalMode), req.ArrivalDetails,
		datePtr(req.DepartureDay), req.DepartureTime, strPtr(req.DepartureMode), req.DepartureDetails,
		req.ArrivalIndependent, req.DepartureIndependent, req.LongHaul, datePtr(req.ExtraStayStart), datePtr(req.ExtraStayEnd), req.Comments).
		Scan(&subID)
	if err != nil {
		metrics.SubmissionMutationsTotal.WithLabelValues("write", "error").Inc()
		return nil, err
	}

	// Responding makes you an attendee of the event: keep the unified overview's
	// membership in lock-step with submissions so there is no "off-roster" gap.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO event_attendees (event_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		e.ID, ownerID); err != nil {
		return nil, err
	}

	// Snapshot the new state for the revision history. The snapshot is cast to
	// jsonb explicitly because pgx sends a Go string as text, which Postgres
	// won't implicitly coerce into a jsonb column.
	snapshot, _ := json.Marshal(req)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO submission_revisions (submission_id, user_id, snapshot) VALUES ($1,$2,$3::jsonb)`,
		subID, actor.ID, string(snapshot)); err != nil {
		return nil, err
	}

	// Activity log. after_deadline is stamped when the change lands past the
	// event's submission deadline — the flag the admin timeline highlights.
	afterDeadline := time.Now().After(e.SubmissionDeadline)
	action, summary := submissionActivity(existed, isAdmin, ownerName, actor, *req, prev.Attending)
	// On an update, record exactly which fields changed so the timeline shows
	// what was edited, not just that something was. Omitted on first response.
	var detail any
	if existed {
		if changes := diffSubmissionReq(prev, *req); len(changes) > 0 {
			detail = map[string]any{"changes": changes}
		}
	}
	actorID := actor.ID
	if err := a.logActivity(ctx, tx, e.ID, &actorID, actor.Email, ownerEmail, action, summary, detail, afterDeadline); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		metrics.SubmissionMutationsTotal.WithLabelValues("write", "error").Inc()
		return nil, err
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

	return a.Store.loadSubmission(ctx, e.ID, ownerID)
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

// fieldChange is one before/after pair recorded in an update's activity detail.
type fieldChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// diffSubmissionReq lists the fields that differ between the prior persisted
// state and the new (already normalized) request, in form order. Values are
// rendered for display; empty means "not set". Both sides are post-normalization,
// so out-of-branch fields blanked by normalizeAndValidate show up as cleared.
func diffSubmissionReq(prev, next submissionReq) []fieldChange {
	var changes []fieldChange
	add := func(field, from, to string) {
		if from != to {
			changes = append(changes, fieldChange{Field: field, From: from, To: to})
		}
	}
	add("Attending", attendingLabel(prev.Attending), attendingLabel(next.Attending))
	add("Not-sure reason", prev.NotSureReason, next.NotSureReason)
	add("Arrival day", optStr(prev.ArrivalDay), optStr(next.ArrivalDay))
	add("Arrival time", prev.ArrivalTime, next.ArrivalTime)
	add("Arrival mode", optStr(prev.ArrivalMode), optStr(next.ArrivalMode))
	add("Arrival details", prev.ArrivalDetails, next.ArrivalDetails)
	add("Departure day", optStr(prev.DepartureDay), optStr(next.DepartureDay))
	add("Departure time", prev.DepartureTime, next.DepartureTime)
	add("Departure mode", optStr(prev.DepartureMode), optStr(next.DepartureMode))
	add("Departure details", prev.DepartureDetails, next.DepartureDetails)
	add("Arrival self-arranged", boolStr(prev.ArrivalIndependent), boolStr(next.ArrivalIndependent))
	add("Departure self-arranged", boolStr(prev.DepartureIndependent), boolStr(next.DepartureIndependent))
	add("Long-haul travel", boolStr(prev.LongHaul), boolStr(next.LongHaul))
	add("Extra stay start", optStr(prev.ExtraStayStart), optStr(next.ExtraStayStart))
	add("Extra stay end", optStr(prev.ExtraStayEnd), optStr(next.ExtraStayEnd))
	add("Comments", prev.Comments, next.Comments)
	return changes
}

func optStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
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
