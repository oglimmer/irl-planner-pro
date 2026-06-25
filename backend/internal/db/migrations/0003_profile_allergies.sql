-- 0003_profile_allergies: allergies / dietary preferences describe the person,
-- not any one event, so they live on the user profile and are no longer captured
-- per submission. Mirrors 0002_profile_names (which moved the name). See DESIGN.md §8.

ALTER TABLE users ADD COLUMN IF NOT EXISTS allergies TEXT NOT NULL DEFAULT '';

-- Backfill from submissions before dropping the column, then drop it. Wrapped in
-- a guard so re-running this migration (Migrate runs on every boot) is a no-op
-- once the submissions.allergies column is already gone.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'submissions' AND column_name = 'allergies') THEN
    -- Take each user's most recently updated non-empty value. Only fill profiles
    -- that don't already have one so a re-run can't clobber a later profile edit.
    UPDATE users u
       SET allergies = sub.allergies
      FROM (
        SELECT DISTINCT ON (user_id) user_id, allergies
          FROM submissions
         WHERE allergies <> ''
         ORDER BY user_id, updated_at DESC
      ) sub
     WHERE sub.user_id = u.id AND u.allergies = '';
    ALTER TABLE submissions DROP COLUMN allergies;
  END IF;
END $$;
