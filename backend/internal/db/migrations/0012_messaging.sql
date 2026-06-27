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
-- event-local date) so a repeated same-day click doesn't double-send. Widen the
-- kind CHECK to admit those two new kinds. Drop-then-add stays idempotent across
-- the every-boot re-run because the DROP always precedes the ADD.
ALTER TABLE reminder_log DROP CONSTRAINT IF EXISTS reminder_log_reminder_kind_check;
ALTER TABLE reminder_log ADD CONSTRAINT reminder_log_reminder_kind_check
    CHECK (reminder_kind IN ('weekly','deadline','daily_digest','invitation','manual'));
