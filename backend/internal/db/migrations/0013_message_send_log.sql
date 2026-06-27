-- 0013_message_send_log: per-recipient delivery outcome for outbound messages
-- (invitations, manual follow-ups, scheduled reminders).
--
-- reminder_log is the exactly-once *claim* ledger (did we already attempt this
-- recipient for this window?). This is the *result* ledger the admin UI reads to
-- show "sent N, failed M" and list who failed. Append-only; one row per attempt.
--
-- "sent" means the SMTP relay accepted the message at submission — not that it
-- was delivered. Asynchronous bounces are not tracked here.
--
-- Re-runs on every boot, so every statement is idempotent.
CREATE TABLE IF NOT EXISTS message_send_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    recipient  TEXT NOT NULL,
    kind       TEXT NOT NULL,   -- invitation | manual | weekly | deadline
    channel    TEXT NOT NULL,   -- email | slack
    status     TEXT NOT NULL CHECK (status IN ('sent','failed')),
    error      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS message_send_log_event_idx  ON message_send_log(event_id, created_at DESC);
CREATE INDEX IF NOT EXISTS message_send_log_failed_idx ON message_send_log(event_id, status, created_at DESC);
