-- Attendees who arrange their own travel and need no support from the People
-- team can flag that instead of filling in arrival/departure legs. When set,
-- the travel legs, long-haul flag, and extra-night dates are all blanked on
-- write (enforced server-side in submissions.go).
ALTER TABLE submissions
    ADD COLUMN IF NOT EXISTS travel_independent BOOLEAN NOT NULL DEFAULT FALSE;
