-- 0010_event_attendees: unify the three ways an employee enters the system
-- (logged in before the event, logged in after, or uploaded by an admin) onto a
-- single canonical record — the company-wide `users` directory — and model an
-- event's expected population as a membership relation referencing real users.
-- This replaces the standalone `event_roster` email list. See DESIGN.md §5.4.

-- last_login_at distinguishes a user who has actually signed in from one merely
-- provisioned by an admin import (NULL = provisioned, never logged in). Stamped
-- by findOrCreateUser on every login (server/users.go).
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;

-- Per-event membership: which employees (users) are expected at an event. Drives
-- the admin overview, non-responder tracking, and reminders. A submission also
-- auto-adds its author here (server/submissions.go), so the overview is exactly
-- this set — there is no longer an "off-roster" category.
CREATE TABLE IF NOT EXISTS event_attendees (
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, user_id)
);

-- One-time backfill of the legacy event_roster + existing submissions into the
-- unified model. Guarded so it runs only while event_attendees is still empty:
-- Migrate() re-runs every migration on every boot, and once admins start
-- managing membership we must never re-add a removed attendee. Reaching this
-- guard with provisioned (NULL last_login_at) users present is unreachable via
-- the app, because every provisioning path also inserts into event_attendees.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM event_attendees) THEN
    -- Every pre-existing user got into `users` by logging in, so mark them as
    -- having signed in. Roster-provisioned users are inserted *after* this and
    -- keep their NULL last_login_at.
    UPDATE users SET last_login_at = created_at WHERE last_login_at IS NULL;

    -- Provision a user row for each roster email that has none yet, splitting the
    -- CSV full_name into first/last on the first space (mirrors splitName).
    INSERT INTO users (email, first_name, last_name)
    SELECT DISTINCT ON (lower(er.email))
           lower(er.email),
           split_part(er.full_name, ' ', 1),
           ltrim(substr(er.full_name, length(split_part(er.full_name, ' ', 1)) + 1))
      FROM event_roster er
     WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.email = lower(er.email))
     ORDER BY lower(er.email)
    ON CONFLICT (email) DO NOTHING;

    -- Link every roster row to its user as an event attendee.
    INSERT INTO event_attendees (event_id, user_id)
    SELECT er.event_id, u.id
      FROM event_roster er
      JOIN users u ON u.email = lower(er.email)
    ON CONFLICT DO NOTHING;

    -- Anyone who already submitted is an attendee too (folds in the old
    -- "responded but off-roster" population).
    INSERT INTO event_attendees (event_id, user_id)
    SELECT s.event_id, s.user_id
      FROM submissions s
    ON CONFLICT DO NOTHING;
  END IF;
END $$;
