-- 0014_event_admin_notifications: per-event, per-admin notification preferences.
-- Replaces the old "IRL team + every admin" blanket notification model. The
-- event-level `daily_activity_email` flag (0001) is repurposed to mean *IRL
-- team only* (a daily summary email to IRL_TEAM_EMAIL); admins now opt in
-- individually here.
--
-- A row exists ONLY for an admin who opted in — absence of a row means "off".
-- notif_type is 'daily' (the daily activity digest) or 'activity' (an immediate
-- notification on every submission create/edit). channel_email / channel_slack
-- select the delivery transport(s); at least one is set for a live row.
--
-- Idempotent (re-runs every boot). Intentionally NOT backfilled: like
-- event_attendees, a re-running backfill would resurrect a preference an admin
-- cleared. New events start with no admin opted in.
CREATE TABLE IF NOT EXISTS event_admin_notifications (
    event_id      UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    notif_type    TEXT NOT NULL,                 -- 'daily' | 'activity'
    channel_email BOOLEAN NOT NULL DEFAULT false,
    channel_slack BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (event_id, user_id)
);
