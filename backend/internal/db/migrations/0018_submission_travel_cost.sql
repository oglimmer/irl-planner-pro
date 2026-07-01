-- 0018_submission_travel_cost: capture the attendee's total personal travel cost
-- (ticket fare, ticket price and any other personal travel spend rolled into one
-- figure) as a value plus its currency. travel_cost is the amount and
-- travel_cost_currency an ISO-4217 code (e.g. USD, EUR, GBP). Only meaningful for
-- an attending=yes response; blanked otherwise server-side (see submissions.go).
-- The admin Financial tab converts these to USD/GBP/EUR via live FX rates.
ALTER TABLE submissions ADD COLUMN IF NOT EXISTS travel_cost NUMERIC(14,2);
ALTER TABLE submissions ADD COLUMN IF NOT EXISTS travel_cost_currency TEXT;
