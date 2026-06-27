-- 0011_activity_category: tag every activity-log entry as a participant ("user")
-- or administrative ("admin") action. This classifies *what was done*, not who
-- did it — an admin who is also an employee submitting their own attendance
-- produces a "user" entry. It lets the admin all-activity view default to
-- reviewing participant activity (the common case) and filter to admin actions
-- only when needed. See DESIGN.md §5.8.
--
-- Category is a deterministic function of the action verb, so it is computed at
-- write time (server/activity.go actionCategory) and backfilled here. Both this
-- migration and the writer must agree on the mapping.

ALTER TABLE activity_log ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'admin';

-- Participant-facing actions are the only "user" entries; everything else
-- (event config, roster management, admin edits, reminders) is "admin". The
-- guard keeps re-runs (every migration re-runs on every boot) from rewriting
-- already-classified rows.
UPDATE activity_log SET category = 'user'
 WHERE action IN ('submission.created', 'submission.updated')
   AND category <> 'user';

-- Index the admin view's default filter (one event, one category, newest first).
CREATE INDEX IF NOT EXISTS activity_log_category_idx
    ON activity_log(event_id, category, created_at DESC);
