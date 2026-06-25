package server

import (
	"context"
	"testing"
	"time"
)

func TestClaimReminderIdempotent(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.findOrCreateUser(ctx, "admin@id5.io", "Admin")

	var eventID string
	if err := a.DB.QueryRowContext(ctx,
		`INSERT INTO events (slug,name,timezone,start_date,end_date,submission_deadline,created_by)
		 VALUES ('claim-ev','E','Europe/Paris','2026-10-12','2026-10-16','2026-10-01T15:00:00Z',$1)
		 RETURNING id`, admin.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	first, err := a.claimReminder(ctx, eventID, "x@id5.io", "weekly", "2026-W40")
	if err != nil {
		t.Fatal(err)
	}
	if !first {
		t.Fatal("first claim should succeed")
	}
	second, err := a.claimReminder(ctx, eventID, "x@id5.io", "weekly", "2026-W40")
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Fatal("second claim for the same window must be a no-op (idempotent)")
	}
	// A different period key is a fresh claim.
	third, _ := a.claimReminder(ctx, eventID, "x@id5.io", "weekly", "2026-W41")
	if !third {
		t.Fatal("a new period key should claim successfully")
	}
}

func TestNonResponders(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.findOrCreateUser(ctx, "admin@id5.io", "Admin")

	var eventID string
	if err := a.DB.QueryRowContext(ctx,
		`INSERT INTO events (slug,name,timezone,start_date,end_date,submission_deadline,created_by)
		 VALUES ('nr-ev','E','Europe/Paris','2026-10-12','2026-10-16','2026-10-01T15:00:00Z',$1)
		 RETURNING id`, admin.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	for _, em := range []string{"alice@id5.io", "bob@id5.io", "carol@id5.io"} {
		if _, err := a.DB.ExecContext(ctx,
			`INSERT INTO event_roster (event_id, full_name, email) VALUES ($1,$2,$3)`,
			eventID, em, em); err != nil {
			t.Fatal(err)
		}
	}
	// Bob responds.
	bob, _ := a.findOrCreateUser(ctx, "bob@id5.io", "Bob")
	e, _ := a.loadEventByColumn(ctx, "id", eventID, time.Now())
	req := &submissionReq{FirstName: "Bob", LastName: "B", Attending: "no"}
	if err := req.normalizeAndValidate(e, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO submissions (event_id, user_id, first_name, last_name, attending)
		 VALUES ($1,$2,'Bob','B','no')`, eventID, bob.ID); err != nil {
		t.Fatal(err)
	}

	nr, err := a.nonResponders(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nr) != 2 {
		t.Fatalf("want 2 non-responders (alice, carol), got %v", nr)
	}
}
