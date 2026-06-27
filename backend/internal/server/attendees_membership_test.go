package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mkEventForTest creates an event via the admin handler and returns its id.
func mkEventForTest(t *testing.T, a *App, ctx context.Context, adminID, slug, start, end string) string {
	t.Helper()
	body, _ := json.Marshal(eventReq{
		Slug: slug, Name: slug, Timezone: "Europe/Paris",
		StartDate: start, EndDate: end,
		SubmissionDeadlineLocal: start + "T17:00", ReminderHour: 9,
	})
	r := httptest.NewRequest(http.MethodPost, "/api/admin/events", bytes.NewReader(body))
	r = r.WithContext(withAdmin(ctx, adminID))
	w := httptest.NewRecorder()
	a.handleCreateEvent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create %s: status %d body %s", slug, w.Code, w.Body.String())
	}
	var e Event
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode %s: %v", slug, err)
	}
	return e.ID
}

func attendeeCount(t *testing.T, a *App, ctx context.Context, eventID string) int {
	t.Helper()
	var n int
	if err := a.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM event_attendees WHERE event_id = $1`, eventID).Scan(&n); err != nil {
		t.Fatalf("count attendees: %v", err)
	}
	return n
}

func isAttendee(t *testing.T, a *App, ctx context.Context, eventID, userID string) bool {
	t.Helper()
	var ok bool
	if err := a.DB.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM event_attendees WHERE event_id = $1 AND user_id = $2)`,
		eventID, userID).Scan(&ok); err != nil {
		t.Fatalf("is attendee: %v", err)
	}
	return ok
}

// Creating an event snapshots every existing user onto it (everyone is in by default).
func TestEventCreationSnapshotsAllUsers(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@id5.io", "Admin", "", "")
	a.Store.findOrCreateUser(ctx, "bob@id5.io", "Bob", "Jones", "")
	a.Store.findOrCreateUser(ctx, "carol@id5.io", "Carol", "Smith", "")

	id := mkEventForTest(t, a, ctx, admin.ID, "offsite", "2099-10-12", "2099-10-16")
	if got := attendeeCount(t, a, ctx, id); got != 3 {
		t.Fatalf("want all 3 users seeded as attendees, got %d", got)
	}
}

// A brand-new user (first login) is added to open events only, never to past ones.
func TestNewUserAddedToOpenEventsOnly(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@id5.io", "Admin", "", "")
	openID := mkEventForTest(t, a, ctx, admin.ID, "future-offsite", "2099-10-12", "2099-10-16")
	pastID := mkEventForTest(t, a, ctx, admin.ID, "past-offsite", "2000-10-12", "2000-10-16")

	// New employee logs in for the first time after both events exist.
	bob, err := a.Store.findOrCreateUser(ctx, "bob@id5.io", "Bob", "Jones", "")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if !isAttendee(t, a, ctx, openID, bob.ID) {
		t.Error("new user should be auto-added to the open event")
	}
	if isAttendee(t, a, ctx, pastID, bob.ID) {
		t.Error("new user should NOT be added to a past event")
	}
}

// An explicit removal sticks: creating later users never resurrects a removed one.
func TestRemovedAttendeeStaysRemoved(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@id5.io", "Admin", "", "")
	bob, _ := a.Store.findOrCreateUser(ctx, "bob@id5.io", "Bob", "Jones", "")
	id := mkEventForTest(t, a, ctx, admin.ID, "offsite", "2099-10-12", "2099-10-16")

	// Remove Bob from the event.
	rctx := withParams(withAdmin(ctx, admin.ID), "id", id, "userId", bob.ID)
	r := httptest.NewRequest(http.MethodDelete, "/api/admin/events/"+id+"/attendees/"+bob.ID, nil)
	r = r.WithContext(rctx)
	w := httptest.NewRecorder()
	a.handleRemoveAttendee(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("remove attendee: status %d body %s", w.Code, w.Body.String())
	}
	if isAttendee(t, a, ctx, id, bob.ID) {
		t.Fatal("bob should be removed")
	}

	// A new employee joining must not re-add Bob.
	a.Store.findOrCreateUser(ctx, "carol@id5.io", "Carol", "Smith", "")
	if isAttendee(t, a, ctx, id, bob.ID) {
		t.Error("removed attendee must not be resurrected by a later user creation")
	}
}

// upsertDirectoryUser provisions an unknown email and reports created; a second
// call for the same email reports it pre-existing.
func TestUpsertDirectoryUser(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	id1, created, err := upsertDirectoryUser(ctx, a.DB, "new@id5.io", "New", "Hire")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !created {
		t.Error("first upsert of an unknown email should report created")
	}
	id2, created2, err := upsertDirectoryUser(ctx, a.DB, "new@id5.io", "Ignored", "Name")
	if err != nil {
		t.Fatalf("upsert again: %v", err)
	}
	if created2 {
		t.Error("second upsert should report the existing user, not created")
	}
	if id1 != id2 {
		t.Errorf("same email should resolve to the same id: %s vs %s", id1, id2)
	}
}
