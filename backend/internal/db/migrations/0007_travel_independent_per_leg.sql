-- 0007_travel_independent_per_leg: travel to and travel from the offsite are
-- independent decisions — an attendee may arrange their own flight out but want
-- the People team to book the return (or vice versa). The single
-- travel_independent flag (migration 0005) could only say "self-arrange
-- everything", so split it into one flag per leg. Each, when set, blanks only
-- its own leg; the long-haul/accommodation block is dropped only when BOTH legs
-- are self-arranged (the old all-or-nothing case). Enforced server-side in
-- submissions.go.

-- Add the per-leg flags and carry the old combined value onto both. The whole
-- block is guarded on the new column being absent so it runs exactly once: a
-- bare backfill would re-run on every boot (Migrate runs each boot) and clobber
-- later per-leg edits.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_name = 'submissions' AND column_name = 'arrival_independent') THEN
    ALTER TABLE submissions ADD COLUMN arrival_independent   BOOLEAN NOT NULL DEFAULT FALSE;
    ALTER TABLE submissions ADD COLUMN departure_independent BOOLEAN NOT NULL DEFAULT FALSE;
    -- travel_independent always exists here on the first run (0005 runs earlier
    -- in Migrate and predates this migration), but guard anyway.
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'submissions' AND column_name = 'travel_independent') THEN
      UPDATE submissions
         SET arrival_independent   = travel_independent,
             departure_independent = travel_independent;
    END IF;
  END IF;
END $$;

-- travel_independent is superseded. Migration 0005 re-adds it (ADD COLUMN IF NOT
-- EXISTS) on every boot, so drop it on every boot too: the backfill above ran
-- once while the column still held real data, and any copy 0005 re-adds on a
-- later boot is empty (default FALSE) and safe to discard.
ALTER TABLE submissions DROP COLUMN IF EXISTS travel_independent;
