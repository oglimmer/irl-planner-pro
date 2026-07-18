package server

import (
	"context"
	"testing"
	"time"
)

func TestClaimReminderIdempotent(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@oglimmer.com", "Admin", "", "")

	var eventID string
	if err := a.DB.QueryRowContext(ctx,
		`INSERT INTO events (slug,name,timezone,start_date,end_date,submission_deadline,created_by)
		 VALUES ('claim-ev','E','Europe/Paris','2026-10-12','2026-10-16','2026-10-01T15:00:00Z',$1)
		 RETURNING id`, admin.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	first, err := a.claimReminder(ctx, eventID, "x@oglimmer.com", "weekly", "2026-W40")
	if err != nil {
		t.Fatal(err)
	}
	if !first {
		t.Fatal("first claim should succeed")
	}
	second, err := a.claimReminder(ctx, eventID, "x@oglimmer.com", "weekly", "2026-W40")
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Fatal("second claim for the same window must be a no-op (idempotent)")
	}
	// A different period key is a fresh claim.
	third, _ := a.claimReminder(ctx, eventID, "x@oglimmer.com", "weekly", "2026-W41")
	if !third {
		t.Fatal("a new period key should claim successfully")
	}
}

func TestNonResponders(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.Store.findOrCreateUser(ctx, "admin@oglimmer.com", "Admin", "", "")

	var eventID string
	if err := a.DB.QueryRowContext(ctx,
		`INSERT INTO events (slug,name,timezone,start_date,end_date,submission_deadline,created_by)
		 VALUES ('nr-ev','E','Europe/Paris','2026-10-12','2026-10-16','2026-10-01T15:00:00Z',$1)
		 RETURNING id`, admin.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	// alice, bob, carol are all attendees (provisioning creates their directory
	// users and links them to the event).
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provisionAttendees(ctx, tx, eventID, []RosterEntry{
		{FullName: "Alice", Email: "alice@oglimmer.com"},
		{FullName: "Bob", Email: "bob@oglimmer.com"},
		{FullName: "Carol", Email: "carol@oglimmer.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Bob responds.
	bob, _ := a.Store.findOrCreateUser(ctx, "bob@oglimmer.com", "Bob", "B", "")
	e, _ := a.Store.loadEventByColumn(ctx, "id", eventID, time.Now())
	req := &submissionReq{Attending: "no"}
	if err := req.normalizeAndValidate(e, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO submissions (event_id, user_id, attending)
		 VALUES ($1,$2,'no')`, eventID, bob.ID); err != nil {
		t.Fatal(err)
	}

	nr, err := a.Store.nonResponders(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nr) != 2 {
		t.Fatalf("want 2 non-responders (alice, carol), got %v", nr)
	}
}
