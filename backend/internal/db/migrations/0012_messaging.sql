-- 0012_messaging: per-event message templates for the Messaging tab.
--
-- Invitation and reminder copy is admin-editable and stored on the event. An
-- empty string means "no override" — the backend falls back to a generated
-- default template (see messaging.go), so existing events and the scheduled
-- reminder goroutine keep working unchanged.
--
-- Re-runs on every boot, so every statement is idempotent.
ALTER TABLE events ADD COLUMN IF NOT EXISTS invite_subject   TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN IF NOT EXISTS invite_body      TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN IF NOT EXISTS reminder_subject TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN IF NOT EXISTS reminder_body    TEXT NOT NULL DEFAULT '';

-- The Messaging tab reuses reminder_log as its idempotency ledger: admin-pressed
-- invitations claim ('invitation', fixed period 'invitation') so each attendee is
-- emailed at most once, and a manual "send follow-up now" claims ('manual', the
-- event-local date) so a repeated same-day click doesn't double-send. Both kinds
-- need the reminder_kind CHECK widened.
--
-- That widening deliberately does NOT live here. Because every migration re-runs
-- on every boot in order, a drop-then-add here would re-narrow the constraint on
-- each boot *before* the later migrations that widen it further get to run — and
-- ADD CONSTRAINT validates existing rows, so the first reminder_log row written
-- under any newer kind bricks every subsequent boot (this happened: the backend
-- crash-looped on "constraint ... is violated by some row"). Exactly one
-- migration may own this constraint, and it must be the newest one that touches
-- it — currently 0019_flightcost_reminder_kinds.sql, which lists 'invitation'
-- and 'manual' alongside the flight-cost kinds. When adding another kind, widen
-- the list in that latest migration or add a new one; never re-add the
-- constraint from an earlier file.
