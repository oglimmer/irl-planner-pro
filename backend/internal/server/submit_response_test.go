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
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@id5.io", "Admin", "", "")
	e := seedEventForRSVP(t, a, ctx, admin.ID)

	owner, err := a.Store.findOrCreateUser(ctx, "elena.rossi@id5.io", "Elena", "Rossi", "")
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
	sub, err := a.applySubmission(ctx, e, &req, owner.ID, owner, false)
	if err != nil {
		t.Fatalf("applySubmission: %v", err)
	}
	if sub.Attending != "yes" || sub.Email != "elena.rossi@id5.io" {
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
	if actor != "elena.rossi@id5.io" {
		t.Errorf("activity actor = %q, want the attendee", actor)
	}
}

// TestApplySubmissionValidationError confirms a conditional-form violation comes
// back as errSubmissionInvalid (the sentinel callers map to a 400 / bad input),
// not a generic db error.
func TestApplySubmissionValidationError(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@id5.io", "Admin", "", "")
	e := seedEventForRSVP(t, a, ctx, admin.ID)
	owner, _ := a.Store.findOrCreateUser(ctx, "bob@id5.io", "Bob", "", "")

	// attending=yes but no travel legs and not independent → invalid.
	req := submissionReq{Attending: "yes"}
	_, err := a.applySubmission(ctx, e, &req, owner.ID, owner, false)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var inv errSubmissionInvalid
	if !errors.As(err, &inv) {
		t.Fatalf("error %v is not errSubmissionInvalid", err)
	}
}
