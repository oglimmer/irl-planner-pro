-- 0006_profile_confirmed: an OIDC user's name and allergies are seeded from the
-- IdP on first login, but the IdP's split of given/family name is often wrong and
-- it never carries dietary needs. So on first sign-in we ask the user to confirm
-- or correct their profile. profile_confirmed tracks whether they've done so;
-- it flips true on the first PUT /api/me (see handleUpdateMe).

-- Add the flag (new accounts default to false → they see the confirm step) and,
-- exactly once, mark every account that already existed as confirmed so users
-- onboarded before this change aren't forced back through it. The backfill is
-- guarded on the column being absent so re-running (Migrate runs on every boot)
-- is a no-op — a plain UPDATE would re-confirm everyone on every restart.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_name = 'users' AND column_name = 'profile_confirmed') THEN
    ALTER TABLE users ADD COLUMN profile_confirmed BOOLEAN NOT NULL DEFAULT false;
    UPDATE users SET profile_confirmed = true;
  END IF;
END $$;
