-- 0020_event_flight_reminder_templates: make the flight-cost reminder copy
-- admin-editable, like the invite/reminder templates (migration 0012).
--
-- Empty string means "fall back to the generated default"
-- (defaultFlightReminder* in messaging.go), matching the other template columns.
--
-- Re-runs on every boot: ADD COLUMN IF NOT EXISTS keeps it idempotent.
ALTER TABLE events ADD COLUMN IF NOT EXISTS flight_reminder_subject TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN IF NOT EXISTS flight_reminder_body    TEXT NOT NULL DEFAULT '';
