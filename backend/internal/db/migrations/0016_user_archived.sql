-- Archived users are excluded from every event activity (attendee seeding,
-- reminders, dashboards, exports, admin notifications) and listed in a separate
-- section of the admin /users page. Archiving is reversible: membership rows are
-- left intact, so reactivating a user restores them everywhere at once.
ALTER TABLE users ADD COLUMN IF NOT EXISTS archived BOOLEAN NOT NULL DEFAULT false;
