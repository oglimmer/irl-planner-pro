-- 0017_submission_self_funded_early: an attendee may arrive the day before the
-- event and arrange (pay for) their own accommodation, while still wanting the
-- IRL team to handle their transport and to be considered for any shared
-- transfer on that early-arrival day. This is distinct from the company-paid
-- extra night before (extra_stay_start), which is reserved for long-haul
-- travellers. When set, the early arrival is legitimised without booking a
-- company hotel night. Enforced server-side in submissions.go: an early arrival
-- requires either the company extra night or this self-funded flag.
ALTER TABLE submissions ADD COLUMN IF NOT EXISTS extra_stay_self_funded BOOLEAN NOT NULL DEFAULT FALSE;
