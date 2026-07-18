-- 0019_flightcost_reminder_kinds: admit the flight-cost reminder stream.
--
-- Attendees who responded "yes" but left their flight cost blank now get their
-- own reminder stream (scheduler + manual "Send flight-cost reminder now"),
-- disjoint from the non-responder reminders. Each of its sends claims a
-- reminder_log row under a new reminder_kind, so the CHECK must admit them:
--   flightcost_weekly / flightcost_deadline  (scheduled, mirror weekly/deadline)
--   manual_flightcost                          (admin-pressed, mirror 'manual')
--
-- reminder_log is the exactly-once claim ledger; without this the claim INSERT
-- fails the CHECK and no flight-cost reminder is ever sent. message_send_log.kind
-- has no CHECK, so it needs no change.
--
-- Re-runs on every boot: drop-then-add keeps it idempotent (matches 0012).
ALTER TABLE reminder_log DROP CONSTRAINT IF EXISTS reminder_log_reminder_kind_check;
ALTER TABLE reminder_log ADD CONSTRAINT reminder_log_reminder_kind_check
    CHECK (reminder_kind IN (
        'weekly', 'deadline', 'daily_digest', 'invitation', 'manual',
        'flightcost_weekly', 'flightcost_deadline', 'manual_flightcost'
    ));
