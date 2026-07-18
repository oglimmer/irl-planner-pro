-- 0008_event_hotel_link: events already carry a hotel name and address; add an
-- optional hotel_link (a URL to the hotel's website or booking page) so the
-- attendee UI can turn the hotel name into a clickable link. Empty string means
-- "no link" — the same convention as the other optional text columns.
ALTER TABLE events ADD COLUMN IF NOT EXISTS hotel_link TEXT NOT NULL DEFAULT '';
