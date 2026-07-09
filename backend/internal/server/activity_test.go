package server

import (
	"context"
	"testing"
)

// actionCategory classifies *what was done*, not who did it: only the two
// participant submission verbs are "user"; everything else (config, roster,
// admin edits, reminders) is "admin". This mapping must stay in sync with the
// backfill in migration 0011.
func TestActionCategory(t *testing.T) {
	cases := map[string]string{
		actionSubmissionCreated:     categoryUser,
		actionSubmissionUpdated:     categoryUser,
		actionAdminEditedSubmission: categoryAdmin,
		actionEventCreated:          categoryAdmin,
		actionEventUpdated:          categoryAdmin,
		actionAttendeesImported:     categoryAdmin,
		actionAttendeeAdded:         categoryAdmin,
		actionAttendeeRemoved:       categoryAdmin,
		actionReminderSent:          categoryAdmin,
		"something.unknown":         categoryAdmin,
	}
	for action, want := range cases {
		if got := actionCategory(action); got != want {
			t.Errorf("actionCategory(%q) = %q, want %q", action, got, want)
		}
	}
}

// queryDetailedActivity merges message_send_log entries into the activity
// timeline so the admin view surfaces individual per-channel deliveries
// alongside the coarser campaign-summary entries.
func TestQueryDetailedActivityIncludesSends(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin := mkAdmin(t, a, ctx, "admin@oglimmer.com")
	eventID := mkEventForTest(t, a, ctx, admin, "detailed-activity", "2026-09-01", "2026-09-03")

	if err := a.logActivity(ctx, a.DB, eventID, &admin, "admin@oglimmer.com", "",
		actionInvitationSent, "Sent invitation to 1 attendee(s) via email", nil, false); err != nil {
		t.Fatalf("logActivity: %v", err)
	}
	a.logSend(ctx, eventID, "someone@oglimmer.com", "invitation", "email", "sent", "")

	entries, err := a.queryDetailedActivity(ctx, eventID)
	if err != nil {
		t.Fatalf("queryDetailedActivity: %v", err)
	}
	var foundSend bool
	for _, e := range entries {
		if e.Channel == "email" && e.Status == "sent" && e.SubjectEmail == "someone@oglimmer.com" {
			foundSend = true
		}
	}
	if !foundSend {
		t.Errorf("expected a delivery entry for someone@oglimmer.com, got %+v", entries)
	}
	if len(entries) < 2 {
		t.Errorf("expected at least 2 entries (campaign + delivery), got %d", len(entries))
	}
}
