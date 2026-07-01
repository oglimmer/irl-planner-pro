package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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

	// ExtraStaySelfFunded: the attendee arrives the day before and arranges their
	// own accommodation (no company hotel), but still wants company transport and
	// to be considered for any shared transfer on that day. Mutually exclusive with
	// ExtraStayStart (the company-paid night). See migration 0017.
	ExtraStaySelfFunded bool `json:"extraStaySelfFunded"`

	// Allergies is read-only here: it lives on the submitter's profile (see
	// migration 0003_profile_allergies) and is joined in for display.
	Allergies string `json:"allergies"`
	Comments  string `json:"comments"`

	// TravelCost is the attendee's total personal travel spend (ticket fare /
	// price and any other personal travel cost, as one figure) and
	// TravelCostCurrency its ISO-4217 code (e.g. "USD"). Only meaningful for an
	// attending=yes response; nil / "" when not provided or blanked (migration
	// 0018). The admin Financial tab converts these to USD/GBP/EUR.
	TravelCost         *float64 `json:"travelCost"`
	TravelCostCurrency string   `json:"travelCostCurrency"`

	// Locked is set when an admin edits this response on the attendee's behalf
	// (migration 0015). Once locked the employee form is read-only and only an
	// admin can change it; the lock is permanent (no in-app unlock).
	Locked bool `json:"locked"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// submissionReq is the create/update payload (the writable subset of Submission).
// The attendee's name and allergies are not part of it — they live on the user
// profile.
type submissionReq struct {
	Attending            string   `json:"attending"`
	NotSureReason        string   `json:"notSureReason"`
	ArrivalDay           *string  `json:"arrivalDay"`
	ArrivalTime          string   `json:"arrivalTime"`
	ArrivalMode          *string  `json:"arrivalMode"`
	ArrivalDetails       string   `json:"arrivalDetails"`
	DepartureDay         *string  `json:"departureDay"`
	DepartureTime        string   `json:"departureTime"`
	DepartureMode        *string  `json:"departureMode"`
	DepartureDetails     string   `json:"departureDetails"`
	ArrivalIndependent   bool     `json:"arrivalIndependent"`
	DepartureIndependent bool     `json:"departureIndependent"`
	LongHaul             bool     `json:"longHaul"`
	ExtraStayStart       *string  `json:"extraStayStart"`
	ExtraStayEnd         *string  `json:"extraStayEnd"`
	ExtraStaySelfFunded  bool     `json:"extraStaySelfFunded"`
	Comments             string   `json:"comments"`
	TravelCost           *float64 `json:"travelCost"`
	TravelCostCurrency   string   `json:"travelCostCurrency"`
}

var validTravelModes = map[string]bool{"flight": true, "car": true, "train": true, "other": true}

// supportedCurrencies is the ISO-4217 set the app accepts for a travel cost —
// exactly the currencies the Frankfurter FX API can convert, so every stored
// amount is convertible in the Financial tab. Keep in sync with
// frontend/src/lib/currencies.ts.
var supportedCurrencies = map[string]bool{
	"AUD": true, "BGN": true, "BRL": true, "CAD": true, "CHF": true, "CNY": true,
	"CZK": true, "DKK": true, "EUR": true, "GBP": true, "HKD": true, "HUF": true,
	"IDR": true, "ILS": true, "INR": true, "ISK": true, "JPY": true, "KRW": true,
	"MXN": true, "MYR": true, "NOK": true, "NZD": true, "PHP": true, "PLN": true,
	"RON": true, "SEK": true, "SGD": true, "THB": true, "TRY": true, "USD": true,
	"ZAR": true,
}

// normalizeAndValidate enforces the conditional form rules (DESIGN.md §8) and
// blanks fields outside the chosen branch. For an admin editing on an attendee's
// behalf (isAdmin) every field-level rule is dropped — any day, any option, no
// required fields, no date windows or extra-night caps — leaving only the
// branch-blanking and the parse/enum normalization the DB columns demand. It
// mutates req in place.
func (req *submissionReq) normalizeAndValidate(e *Event, isAdmin bool) error {
	switch req.Attending {
	case "yes", "no", "not_sure":
	default:
		return errors.New("attending must be yes, no, or not_sure")
	}

	if req.Attending != "not_sure" {
		req.NotSureReason = ""
	} else if !isAdmin && strings.TrimSpace(req.NotSureReason) == "" {
		// Admins edit on the attendee's behalf with no validation (any option),
		// so the reason is only mandatory for the employee form.
		return errors.New("a reason is required when you're not sure")
	}

	if req.Attending != "yes" {
		// Clear the whole travel/other block on No / Not sure.
		req.ArrivalDay, req.ArrivalMode, req.DepartureDay, req.DepartureMode = nil, nil, nil, nil
		req.ArrivalTime, req.ArrivalDetails, req.DepartureTime, req.DepartureDetails = "", "", "", ""
		req.ArrivalIndependent, req.DepartureIndependent = false, false
		req.LongHaul = false
		req.ExtraStayStart, req.ExtraStayEnd = nil, nil
		req.ExtraStaySelfFunded = false
		req.Comments = ""
		req.TravelCost, req.TravelCostCurrency = nil, ""
		return nil
	}

	start, _ := parseDate(e.StartDate)
	end, _ := parseDate(e.EndDate)

	// Each leg is independent: a self-arranged leg is blanked and not validated;
	// otherwise the leg's day/mode/details are required.
	if req.ArrivalIndependent {
		// A self-arranged arrival has no company-handled early arrival, so the
		// self-funded-early flag (which asks for company transport) doesn't apply.
		req.ArrivalDay, req.ArrivalMode, req.ArrivalTime, req.ArrivalDetails = nil, nil, "", ""
		req.ExtraStaySelfFunded = false
	} else if err := validateTravelLeg("arrival", &req.ArrivalDay, &req.ArrivalMode, &req.ArrivalTime, &req.ArrivalDetails, start, end, isAdmin); err != nil {
		return err
	}
	if req.DepartureIndependent {
		req.DepartureDay, req.DepartureMode, req.DepartureTime, req.DepartureDetails = nil, nil, "", ""
	} else if err := validateTravelLeg("departure", &req.DepartureDay, &req.DepartureMode, &req.DepartureTime, &req.DepartureDetails, start, end, isAdmin); err != nil {
		return err
	}

	// Late return is no longer offered: the company doesn't provide an extra night
	// after the offsite, so never persist one — for any writer, admin included.
	req.ExtraStayEnd = nil

	// Long-haul accommodation only applies when the People team handles at least
	// one leg; a fully self-arranging attendee gets no accommodation block (this
	// is the old single-flag behavior, now keyed on both legs being independent).
	if req.ArrivalIndependent && req.DepartureIndependent {
		req.LongHaul = false
		req.ExtraStayStart = nil
		req.ExtraStaySelfFunded = false
	} else if !req.LongHaul {
		req.ExtraStayStart = nil
	} else if err := validateExtraStay(&req.ExtraStayStart, start, isAdmin); err != nil {
		return err
	}

	// The company-paid night and the self-funded early arrival are mutually
	// exclusive ways to cover the night before; the company booking wins.
	if req.ExtraStayStart != nil {
		req.ExtraStaySelfFunded = false
	}

	// The arrival leg's travel day must agree with how the night before is covered.
	// Arriving the day before the event needs that night covered — either the
	// long-haul company extra night (ExtraStayStart) OR the attendee arranging their
	// own accommodation (ExtraStaySelfFunded) — otherwise it's rejected. Conversely a
	// company night booked with an in-window arrival is an orphan to remove, and a
	// stray self-funded flag with an in-window arrival is just cleared (it's only an
	// attendee note, not a People-team booking). Runs after the long-haul block has
	// settled ExtraStayStart. A self-arranged leg is skipped (handled above). Mirrors
	// submissionRules.ts / extraNightErrors; like the date-window check it relaxes
	// for admins, who record out-of-window days for special cases. (The departure
	// side has no mirror: with late return removed, the departure day can never fall
	// after the event.)
	if !isAdmin && !req.ArrivalIndependent {
		arrivesEarly := false
		if req.ArrivalDay != nil {
			if d, err := parseDate(*req.ArrivalDay); err == nil && d.Before(start) {
				arrivesEarly = true
			}
		}
		if !arrivesEarly {
			req.ExtraStaySelfFunded = false
		}
		covered := req.ExtraStayStart != nil || req.ExtraStaySelfFunded
		switch {
		case arrivesEarly && !covered:
			return errors.New("to arrive the day before the event, either book the company extra night before (long-haul travellers) or choose to arrange your own accommodation for that night")
		case !arrivesEarly && req.ExtraStayStart != nil:
			return errors.New("the extra night before isn't needed unless you arrive the day before — remove it or change your arrival day")
		}
	}

	// Travel cost (optional): a value + its currency. An amount of nil or ≤0 means
	// "not provided" and blanks both. When an amount is given the currency must be
	// a supported ISO-4217 code so the Financial tab can convert it — enforced for
	// every writer (an unconvertible currency is meaningless in the report).
	if err := normalizeTravelCost(&req.TravelCost, &req.TravelCostCurrency); err != nil {
		return err
	}
	return nil
}

// normalizeTravelCost canonicalizes the optional travel-cost pair in place. A
// missing or non-positive amount clears both fields; otherwise the currency is
// upper-cased and checked against supportedCurrencies. Applies to admins too:
// unlike the date/leg rules, a stored amount with an unconvertible currency would
// silently break the Financial report, so the currency is always validated.
func normalizeTravelCost(amount **float64, currency *string) error {
	cur := strings.ToUpper(strings.TrimSpace(*currency))
	if *amount == nil || **amount <= 0 {
		*amount, *currency = nil, ""
		return nil
	}
	if cur == "" {
		return errors.New("a currency is required for the travel cost")
	}
	if !supportedCurrencies[cur] {
		return errors.New("travel cost currency is not a supported code")
	}
	*currency = cur
	return nil
}

// validateTravelLeg checks one arrival/departure leg. For the employee form a
// day and mode are required, the day must fall in the allowed window (the day
// before the event through its last day — there is no day-after option), and a
// flight also requires its time + flight number. For an
// admin editing on the attendee's behalf, every one of those requirements is
// dropped (any day, any option, no validation): a blank day/mode is normalized
// to NULL and any present value is only canonicalized / range-free. The day and
// mode are still parsed/enum-checked even for admins so the DATE column and the
// mode CHECK constraint can't reject the write.
func validateTravelLeg(label string, day **string, mode **string, travelTime *string, details *string, start, end time.Time, isAdmin bool) error {
	if *day == nil || strings.TrimSpace(**day) == "" {
		if !isAdmin {
			return errors.New(label + " day is required")
		}
		*day = nil
	} else {
		d, err := parseDate(strings.TrimSpace(**day))
		if err != nil {
			return errors.New(label + " day is invalid")
		}
		if !isAdmin {
			// The window runs from the day before the event (for the extra night
			// before) through its last day. There is no day-after option — the
			// company no longer provides a late return.
			lo := start.AddDate(0, 0, -1)
			if d.Before(lo) || d.After(end) {
				return errors.New(label + " day must be within the event dates")
			}
		}
		canon := d.Format(dateLayout)
		*day = &canon
	}

	if *mode == nil || strings.TrimSpace(**mode) == "" {
		if !isAdmin {
			return errors.New(label + " travel mode is required")
		}
		*mode = nil
	} else {
		m := strings.TrimSpace(**mode)
		if !validTravelModes[m] {
			return errors.New(label + " travel mode is invalid")
		}
		*mode = &m
	}

	if !isAdmin && *mode != nil && **mode == "flight" {
		if strings.TrimSpace(*travelTime) == "" {
			return errors.New(label + " flight time is required")
		}
		if strings.TrimSpace(*details) == "" {
			return errors.New(label + " flight number is required")
		}
	}
	return nil
}

// validateExtraStay enforces the before-night bound for the employee form: it must
// sit exactly one day before the first day. (Late return is no longer offered, so
// there is no after-night to validate — the caller blanks extra_stay_end.) For an
// admin editing on the attendee's behalf the bound is dropped (any day, no
// validation) — any present date is only parsed/canonicalized so the DATE column
// accepts it.
func validateExtraStay(startPtr **string, start time.Time, isAdmin bool) error {
	if *startPtr != nil && strings.TrimSpace(**startPtr) != "" {
		d, err := parseDate(strings.TrimSpace(**startPtr))
		if err != nil {
			return errors.New("extra-night (before) date is invalid")
		}
		if !isAdmin {
			if !d.Before(start) {
				return errors.New("the extra night before must be earlier than the first day")
			}
			if !d.Equal(start.AddDate(0, 0, -1)) {
				return errors.New("you can add at most one extra night before the event")
			}
		}
		canon := d.Format(dateLayout)
		*startPtr = &canon
	} else {
		*startPtr = nil
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
	// Once an admin has edited this response it is locked: the attendee can no
	// longer change it from the form (admins keep editing via the admin path).
	sub, err := a.Store.loadSubmission(r.Context(), e.ID, user.ID)
	if err != nil && err != sql.ErrNoRows {
		serverErr(w, r, err, "db error")
		return
	}
	if err == nil && sub.Locked {
		writeErr(w, http.StatusForbidden, "your response has been finalized by an organizer and can no longer be edited — contact the People team if something needs changing")
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
	// An admin edit through the HTTP path locks the response (isAdmin is true
	// only on the admin-edit route); the employee path never locks.
	sub, err := a.applySubmission(r.Context(), e, &req, ownerID, actor, isAdmin, isAdmin)
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
// action. lock, when true, marks the response locked so the attendee can no
// longer edit it from the form; the column is sticky (once locked stays locked),
// so passing false never unlocks an already-locked row. Validation failures come
// back as errSubmissionInvalid and a missing owner as errSubmissionOwnerNotFound;
// any other error is a db/server error.
func (a *App) applySubmission(ctx context.Context, e *Event, req *submissionReq, ownerID string, actor *User, isAdmin, lock bool) (*Submission, error) {
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
	var pTravelCost sql.NullFloat64
	var pTravelCurrency sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT attending, not_sure_reason, arrival_day, arrival_time, arrival_mode, arrival_details,
		        departure_day, departure_time, departure_mode, departure_details,
		        arrival_independent, departure_independent, long_haul, extra_stay_start, extra_stay_end,
		        extra_stay_self_funded, comments, travel_cost, travel_cost_currency
		   FROM submissions WHERE event_id = $1 AND user_id = $2`, e.ID, ownerID).
		Scan(&prev.Attending, &prev.NotSureReason, &pArrDay, &prev.ArrivalTime, &pArrMode, &prev.ArrivalDetails,
			&pDepDay, &prev.DepartureTime, &pDepMode, &prev.DepartureDetails,
			&prev.ArrivalIndependent, &prev.DepartureIndependent, &prev.LongHaul, &pExtraStart, &pExtraEnd,
			&prev.ExtraStaySelfFunded, &prev.Comments, &pTravelCost, &pTravelCurrency)
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
	if pTravelCost.Valid {
		prev.TravelCost = &pTravelCost.Float64
	}
	prev.TravelCostCurrency = pTravelCurrency.String

	var subID string
	err = tx.QueryRowContext(ctx,
		`INSERT INTO submissions (event_id, user_id, attending, not_sure_reason,
		   arrival_day, arrival_time, arrival_mode, arrival_details,
		   departure_day, departure_time, departure_mode, departure_details,
		   arrival_independent, departure_independent, long_haul, extra_stay_start, extra_stay_end,
		   extra_stay_self_funded, comments, locked, travel_cost, travel_cost_currency)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
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
		   extra_stay_self_funded=EXCLUDED.extra_stay_self_funded,
		   comments=EXCLUDED.comments,
		   travel_cost=EXCLUDED.travel_cost, travel_cost_currency=EXCLUDED.travel_cost_currency,
		   -- locked is sticky: an admin edit sets it, and a later write (false)
		   -- never clears it.
		   locked=submissions.locked OR EXCLUDED.locked, updated_at=now()
		 RETURNING id`,
		e.ID, ownerID, req.Attending, req.NotSureReason,
		datePtr(req.ArrivalDay), req.ArrivalTime, strPtr(req.ArrivalMode), req.ArrivalDetails,
		datePtr(req.DepartureDay), req.DepartureTime, strPtr(req.DepartureMode), req.DepartureDetails,
		req.ArrivalIndependent, req.DepartureIndependent, req.LongHaul, datePtr(req.ExtraStayStart), datePtr(req.ExtraStayEnd),
		req.ExtraStaySelfFunded, req.Comments, lock, costPtr(req.TravelCost), strEmptyToNil(req.TravelCostCurrency)).
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

	// Notify "any activity" admins on every submission write — create or edit
	// (best-effort, async; channels per their per-event preference).
	a.notifySubmissionActivity(e, ownerEmail, actor, existed, summary)

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
	add("Self-funded early arrival", boolStr(prev.ExtraStaySelfFunded), boolStr(next.ExtraStaySelfFunded))
	add("Comments", prev.Comments, next.Comments)
	add("Travel cost", costStr(prev.TravelCost, prev.TravelCostCurrency), costStr(next.TravelCost, next.TravelCostCurrency))
	return changes
}

// costStr renders a travel-cost pair for the activity diff, e.g. "123.45 USD"
// (empty when no amount is set).
func costStr(amount *float64, currency string) string {
	if amount == nil {
		return ""
	}
	return strconv.FormatFloat(*amount, 'f', 2, 64) + " " + currency
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

// costPtr binds an optional travel-cost amount to a NUMERIC column (nil → SQL
// NULL). A nil or non-positive amount is normalized away upstream, but guard here
// too so a stray zero never lands as a row value.
func costPtr(f *float64) interface{} {
	if f == nil || *f <= 0 {
		return nil
	}
	return *f
}

// strEmptyToNil maps "" to a SQL NULL for a nullable TEXT column, keeping an
// unset travel-cost currency NULL rather than an empty string.
func strEmptyToNil(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
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
