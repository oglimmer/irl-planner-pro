-- 0015_submission_locked: an admin may edit an attendee's response on their
-- behalf (People team correcting travel, recording a special case, etc.). When
-- they do, the response is *locked*: the employee can no longer edit it from the
-- attendee form — only admins can change it from then on. The lock is permanent
-- (there is no in-app unlock), so a deliberate admin correction can't be silently
-- overwritten by the attendee. Enforced server-side in submissions.go: the admin
-- write path sets locked=true and the employee write path rejects a locked row.
ALTER TABLE submissions ADD COLUMN IF NOT EXISTS locked BOOLEAN NOT NULL DEFAULT FALSE;
