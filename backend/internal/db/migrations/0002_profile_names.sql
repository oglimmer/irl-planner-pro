-- 0002_profile_names: a user's name lives on their profile (first/last), seeded
-- from the OIDC `profile` scope on first login and editable thereafter. The name
-- is no longer captured per submission. See DESIGN.md §8.

-- Split the single users.name into first/last. The backfill + drop is wrapped in
-- a guard so re-running this migration (Migrate runs on every boot) is a no-op
-- once the old `name` column is already gone.
ALTER TABLE users ADD COLUMN IF NOT EXISTS first_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_name  TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'users' AND column_name = 'name') THEN
    -- Backfill once: everything before the first space is the first name, the
    -- remainder the last.
    UPDATE users
       SET first_name = split_part(name, ' ', 1),
           last_name  = CASE
                          WHEN position(' ' in name) > 0
                            THEN substring(name from position(' ' in name) + 1)
                          ELSE ''
                        END
     WHERE first_name = '' AND last_name = '' AND name <> '';
    ALTER TABLE users DROP COLUMN name;
  END IF;
END $$;

-- Names are now read from the submitter's profile, not stored on the submission.
ALTER TABLE submissions DROP COLUMN IF EXISTS first_name;
ALTER TABLE submissions DROP COLUMN IF EXISTS last_name;
