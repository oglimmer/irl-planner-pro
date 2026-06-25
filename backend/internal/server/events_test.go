package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolveDaysDefaults(t *testing.T) {
	start, _ := parseDate("2026-10-12")
	end, _ := parseDate("2026-10-16")
	days := resolveDays(start, end, nil)
	if len(days) != 5 {
		t.Fatalf("want 5 days, got %d", len(days))
	}
	if days[0].Type != "travel" || days[4].Type != "travel" {
		t.Errorf("first/last should be travel: %+v", days)
	}
	for _, d := range days[1:4] {
		if d.Type != "event" {
			t.Errorf("middle day should be event: %+v", d)
		}
	}
}

func TestResolveDaysOverride(t *testing.T) {
	start, _ := parseDate("2026-10-12")
	end, _ := parseDate("2026-10-14")
	days := resolveDays(start, end, []EventDay{{Date: "2026-10-12", Type: "event"}})
	if days[0].Type != "event" {
		t.Errorf("override should make first day an event day: %+v", days[0])
	}
}

func TestSingleDayEvent(t *testing.T) {
	start, _ := parseDate("2026-10-12")
	days := resolveDays(start, start, nil)
	if len(days) != 1 || days[0].Type != "travel" {
		t.Errorf("single-day event should be one travel day: %+v", days)
	}
}

func TestParseLocalDateTimeInZone(t *testing.T) {
	loc, err := loadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	// CEST is UTC+2 in October, so 17:00 Paris == 15:00 UTC.
	got, err := parseLocalDateTimeInZone("2026-10-16T17:00", loc)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
	// And it round-trips back to the local wall clock.
	if local := formatLocalDateTime(got, loc); local != "2026-10-16T17:00" {
		t.Errorf("round-trip got %q", local)
	}
}

func TestIsEventPast(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	past, _ := parseDate("2026-10-16")
	future, _ := parseDate("2026-10-25")
	if !isEventPast(past, loc, now) {
		t.Error("event ending 2026-10-16 should be past on 2026-10-20")
	}
	if isEventPast(future, loc, now) {
		t.Error("event ending 2026-10-25 should not be past on 2026-10-20")
	}
}

func TestInvalidTimezoneRejected(t *testing.T) {
	req := &eventReq{
		Slug: "dubrovnik-2026", Name: "IRL", Timezone: "Mars/Phobos",
		StartDate: "2026-10-12", EndDate: "2026-10-16", SubmissionDeadlineLocal: "2026-10-01T17:00",
	}
	if _, _, _, _, err := req.validateAndNormalize(); err == nil {
		t.Fatal("expected invalid timezone to be rejected")
	}
}

// withAdmin returns ctx carrying an admin *User, as authMiddleware would.
func withAdmin(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxUserKey, &User{ID: id, Email: "admin@id5.io", IsAdmin: true})
}

// withUser returns ctx carrying a regular (non-admin) *User.
func withUser(ctx context.Context, id, email string) context.Context {
	return context.WithValue(ctx, ctxUserKey, &User{ID: id, Email: email})
}

func TestCreateEventHandlerDBRoundtrip(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.findOrCreateUser(ctx, "admin@id5.io", "Admin", "")

	body, _ := json.Marshal(eventReq{
		Slug: "dubrovnik-oct-2026", Name: "IRL Dubrovnik October 2026",
		Country: "Croatia", City: "Dubrovnik", HotelName: "Hotel Excelsior",
		Timezone: "Europe/Paris", StartDate: "2026-10-12", EndDate: "2026-10-16",
		SubmissionDeadlineLocal: "2026-10-01T17:00", ReminderDaysBefore: 3,
		WeeklyReminders: true, ReminderHour: 9,
	})
	r := httptest.NewRequest(http.MethodPost, "/api/admin/events", bytes.NewReader(body))
	r = r.WithContext(withAdmin(ctx, admin.ID))
	w := httptest.NewRecorder()
	a.handleCreateEvent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create event: status %d body %s", w.Code, w.Body.String())
	}
	var e Event
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(e.Days) != 5 {
		t.Errorf("want 5 generated days, got %d", len(e.Days))
	}
	if e.SubmissionDeadline.UTC() != time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC) {
		t.Errorf("deadline not converted to UTC: %s", e.SubmissionDeadline)
	}
	if e.IsPast {
		t.Error("a 2026 event should not be past")
	}

	// Duplicate slug → 409.
	r2 := httptest.NewRequest(http.MethodPost, "/api/admin/events", bytes.NewReader(body))
	r2 = r2.WithContext(withAdmin(ctx, admin.ID))
	w2 := httptest.NewRecorder()
	a.handleCreateEvent(w2, r2)
	if w2.Code != http.StatusConflict {
		t.Errorf("duplicate slug: want 409, got %d", w2.Code)
	}
}

func TestListCurrentEventsAnnotatesRSVP(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.findOrCreateUser(ctx, "admin@id5.io", "Admin", "")
	user, _ := a.findOrCreateUser(ctx, "bob@id5.io", "Bob", "Jones")

	mkEvent := func(slug, start, end string) string {
		body, _ := json.Marshal(eventReq{
			Slug: slug, Name: slug, Timezone: "Europe/Paris",
			StartDate: start, EndDate: end,
			SubmissionDeadlineLocal: start + "T17:00", ReminderHour: 9,
		})
		r := httptest.NewRequest(http.MethodPost, "/api/admin/events", bytes.NewReader(body))
		r = r.WithContext(withAdmin(ctx, admin.ID))
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
	futureID := mkEvent("future-offsite", "2099-10-12", "2099-10-16")
	mkEvent("past-offsite", "2000-10-12", "2000-10-16")

	listAs := func(uid string) []ActiveEvent {
		r := httptest.NewRequest(http.MethodGet, "/api/active-events", nil)
		r = r.WithContext(withUser(ctx, uid, "bob@id5.io"))
		w := httptest.NewRecorder()
		a.handleListCurrentEvents(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("active-events: status %d body %s", w.Code, w.Body.String())
		}
		var got []ActiveEvent
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return got
	}

	// Only the upcoming event surfaces, and the user hasn't RSVP'd yet.
	got := listAs(user.ID)
	if len(got) != 1 || got[0].Slug != "future-offsite" {
		t.Fatalf("want only future-offsite, got %+v", got)
	}
	if got[0].HasSubmitted || got[0].MyAttending != "" {
		t.Errorf("expected no RSVP yet, got hasSubmitted=%v attending=%q", got[0].HasSubmitted, got[0].MyAttending)
	}

	// After the user RSVPs, the same card reflects their attending state.
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO submissions (event_id, user_id, attending)
		 VALUES ($1, $2, 'yes')`, futureID, user.ID); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
	got = listAs(user.ID)
	if len(got) != 1 || !got[0].HasSubmitted || got[0].MyAttending != "yes" {
		t.Fatalf("want RSVP=yes annotated, got %+v", got)
	}
}
