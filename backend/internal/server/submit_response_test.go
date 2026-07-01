package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNilIfEmpty(t *testing.T) {
	if got := nilIfEmpty(""); got != nil {
		t.Errorf(`nilIfEmpty("") = %v, want nil`, got)
	}
	if got := nilIfEmpty("   "); got != nil {
		t.Errorf(`nilIfEmpty("   ") = %v, want nil`, got)
	}
	if got := nilIfEmpty("  flight "); got == nil || *got != "flight" {
		t.Errorf(`nilIfEmpty("  flight ") = %v, want "flight" (trimmed)`, got)
	}
}

// seedEventForRSVP creates an admin, a future event, and returns the event.
func seedEventForRSVP(t *testing.T, a *App, ctx context.Context, adminID string) *Event {
	t.Helper()
	body, _ := json.Marshal(eventReq{
		Slug: "dubrovnik-oct-2026", Name: "IRL Dubrovnik October 2026",
		Country: "Croatia", City: "Dubrovnik", HotelName: "Hotel Excelsior",
		Timezone: "Europe/Paris", StartDate: "2026-10-14", EndDate: "2026-10-16",
		SubmissionDeadlineLocal: "2026-09-30T17:00", ReminderDaysBefore: 3,
		WeeklyReminders: true, ReminderHour: 9,
	})
	r := httptest.NewRequest(http.MethodPost, "/api/admin/events", bytes.NewReader(body)).
		WithContext(withAdmin(ctx, adminID))
	w := httptest.NewRecorder()
	a.handleCreateEvent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create event: status %d body %s", w.Code, w.Body.String())
	}
	var e Event
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return &e
}

// TestApplySubmissionAsAttendeeRSVP exercises the shared core that the MCP
// submit_response tool calls: an attendee's own "yes" RSVP with two booked legs.
// It must persist the submission, link the attendee, and append an activity-log
// entry attributed to the attendee (not an admin edit).
func TestApplySubmissionAsAttendeeRSVP(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@oglimmer.com", "Admin", "", "")
	e := seedEventForRSVP(t, a, ctx, admin.ID)

	owner, err := a.Store.findOrCreateUser(ctx, "elena.rossi@oglimmer.com", "Elena", "Rossi", "")
	if err != nil {
		t.Fatalf("create attendee: %v", err)
	}

	arrDay, depDay := "2026-10-14", "2026-10-16"
	arrMode, depMode := "flight", "flight"
	req := submissionReq{
		Attending:        "yes",
		ArrivalDay:       &arrDay,
		ArrivalMode:      &arrMode,
		ArrivalTime:      "12:30",
		ArrivalDetails:   "AF1234",
		DepartureDay:     &depDay,
		DepartureMode:    &depMode,
		DepartureTime:    "18:00",
		DepartureDetails: "AF5678",
		Comments:         "Looking forward to it",
	}

	// Recorded as the attendee themselves (isAdmin=false, actor=owner).
	sub, err := a.applySubmission(ctx, e, &req, owner.ID, owner, false, false)
	if err != nil {
		t.Fatalf("applySubmission: %v", err)
	}
	if sub.Attending != "yes" || sub.Email != "elena.rossi@oglimmer.com" {
		t.Fatalf("unexpected submission: %+v", sub)
	}
	if sub.ArrivalDay == nil || *sub.ArrivalDay != "2026-10-14" {
		t.Errorf("arrival day not persisted: %+v", sub.ArrivalDay)
	}

	// The RSVP makes them an attendee of the event.
	var attendeeCount int
	if err := a.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM event_attendees WHERE event_id=$1 AND user_id=$2`, e.ID, owner.ID).
		Scan(&attendeeCount); err != nil {
		t.Fatalf("count attendees: %v", err)
	}
	if attendeeCount != 1 {
		t.Errorf("attendee link not created, count=%d", attendeeCount)
	}

	// An activity entry is appended, attributed to the attendee (not the admin).
	var actor string
	if err := a.DB.QueryRowContext(ctx,
		`SELECT actor_email FROM activity_log WHERE event_id=$1 ORDER BY created_at DESC LIMIT 1`, e.ID).
		Scan(&actor); err != nil {
		t.Fatalf("read activity: %v", err)
	}
	if actor != "elena.rossi@oglimmer.com" {
		t.Errorf("activity actor = %q, want the attendee", actor)
	}
}

// TestAdminEditLocksResponse confirms an admin edit (lock=true) sets the sticky
// locked flag, that a later non-locking write keeps it locked, and that the
// employee HTTP path then refuses to edit a locked response with a 403.
func TestAdminEditLocksResponse(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@oglimmer.com", "Admin", "", "")
	e := seedEventForRSVP(t, a, ctx, admin.ID)
	owner, _ := a.Store.findOrCreateUser(ctx, "carla@oglimmer.com", "Carla", "", "")

	// Admin edits on the attendee's behalf with no validation (any option) and
	// locks the response.
	adminReq := submissionReq{Attending: "no"}
	sub, err := a.applySubmission(ctx, e, &adminReq, owner.ID, admin, true, true)
	if err != nil {
		t.Fatalf("admin applySubmission: %v", err)
	}
	if !sub.Locked {
		t.Fatal("admin edit should lock the response")
	}

	// A subsequent non-locking write (lock=false) must NOT clear the lock.
	again := submissionReq{Attending: "not_sure", NotSureReason: "tbd"}
	sub, err = a.applySubmission(ctx, e, &again, owner.ID, admin, true, false)
	if err != nil {
		t.Fatalf("second applySubmission: %v", err)
	}
	if !sub.Locked {
		t.Fatal("lock should be sticky across a later non-locking write")
	}

	// The employee HTTP path refuses to edit a locked response.
	body, _ := json.Marshal(submissionReq{
		Attending: "yes", ArrivalDay: strp("2026-10-14"), ArrivalMode: strp("car"),
		DepartureIndependent: true,
	})
	r := httptest.NewRequest(http.MethodPut, "/api/events/"+e.Slug+"/submission", bytes.NewReader(body)).
		WithContext(withParams(withUser(ctx, owner.ID, owner.Email), "slug", e.Slug))
	w := httptest.NewRecorder()
	a.handlePutMySubmission(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("locked employee edit: status %d body %s, want 403", w.Code, w.Body.String())
	}
}

// TestApplySubmissionValidationError confirms a conditional-form violation comes
// back as errSubmissionInvalid (the sentinel callers map to a 400 / bad input),
// not a generic db error.
func TestApplySubmissionValidationError(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@oglimmer.com", "Admin", "", "")
	e := seedEventForRSVP(t, a, ctx, admin.ID)
	owner, _ := a.Store.findOrCreateUser(ctx, "bob@oglimmer.com", "Bob", "", "")

	// attending=yes but no travel legs and not independent → invalid.
	req := submissionReq{Attending: "yes"}
	_, err := a.applySubmission(ctx, e, &req, owner.ID, owner, false, false)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var inv errSubmissionInvalid
	if !errors.As(err, &inv) {
		t.Fatalf("error %v is not errSubmissionInvalid", err)
	}
}
