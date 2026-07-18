package server

import "testing"

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
