-- 0001_init: full schema for the IRL attendance app.
-- See DESIGN.md §5. All timestamps stored UTC; DATE columns are event-local.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Users are provisioned on first OIDC login. The first user ever provisioned is
-- made admin (see server/users.go). token_version powers "sign out everywhere".
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT UNIQUE NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    is_admin      BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    token_version INTEGER NOT NULL DEFAULT 0
);

-- One row per offsite. Never deleted; an event is "past" when end_date < today
-- (computed in the event timezone).
CREATE TABLE IF NOT EXISTS events (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                 TEXT UNIQUE NOT NULL,
    name                 TEXT NOT NULL,
    country              TEXT NOT NULL DEFAULT '',
    city                 TEXT NOT NULL DEFAULT '',
    hotel_name           TEXT NOT NULL DEFAULT '',
    hotel_address        TEXT NOT NULL DEFAULT '',
    timezone             TEXT NOT NULL DEFAULT 'Europe/Paris',
    start_date           DATE NOT NULL,
    end_date             DATE NOT NULL,
    submission_deadline  TIMESTAMPTZ NOT NULL,
    reminder_days_before INTEGER NOT NULL DEFAULT 3,
    weekly_reminders     BOOLEAN NOT NULL DEFAULT true,
    reminder_hour        INTEGER NOT NULL DEFAULT 9,
    daily_activity_email BOOLEAN NOT NULL DEFAULT false,
    created_by           UUID NOT NULL REFERENCES users(id),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per calendar day in [start_date, end_date], each typed.
CREATE TABLE IF NOT EXISTS event_days (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id  UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    day_date  DATE NOT NULL,
    day_type  TEXT NOT NULL CHECK (day_type IN ('travel','event')),
    UNIQUE (event_id, day_date)
);

-- Uploaded CSV (name + work email) per event. Non-responder tracking only.
CREATE TABLE IF NOT EXISTS event_roster (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    full_name  TEXT NOT NULL,
    email      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, email)
);

-- One submission per (event, user).
CREATE TABLE IF NOT EXISTS submissions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id           UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id            UUID NOT NULL REFERENCES users(id),
    first_name         TEXT NOT NULL,
    last_name          TEXT NOT NULL,
    attending          TEXT NOT NULL CHECK (attending IN ('yes','no','not_sure')),
    not_sure_reason    TEXT NOT NULL DEFAULT '',
    arrival_day        DATE,
    arrival_time       TEXT NOT NULL DEFAULT '',
    arrival_mode       TEXT CHECK (arrival_mode IN ('flight','car','train','other')),
    arrival_details    TEXT NOT NULL DEFAULT '',
    departure_day      DATE,
    departure_time     TEXT NOT NULL DEFAULT '',
    departure_mode     TEXT CHECK (departure_mode IN ('flight','car','train','other')),
    departure_details  TEXT NOT NULL DEFAULT '',
    long_haul          BOOLEAN NOT NULL DEFAULT false,
    extra_stay_start   DATE,
    extra_stay_end     DATE,
    allergies          TEXT NOT NULL DEFAULT '',
    comments           TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, user_id)
);

CREATE INDEX IF NOT EXISTS submissions_event_idx ON submissions(event_id);

-- Append-only full snapshots, source of the "submission changed" diff.
CREATE TABLE IF NOT EXISTS submission_revisions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    submission_id UUID NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id),
    snapshot      JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS submission_revisions_sub_idx
    ON submission_revisions(submission_id, created_at DESC);

-- Idempotency ledger for reminders + the daily digest.
CREATE TABLE IF NOT EXISTS reminder_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id      UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    recipient     TEXT NOT NULL,
    reminder_kind TEXT NOT NULL CHECK (reminder_kind IN ('weekly','deadline','daily_digest')),
    period_key    TEXT NOT NULL,
    sent_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, recipient, reminder_kind, period_key)
);

-- Human-readable timeline. Drives the employee "my activity" view, the admin
-- all-activity view, and the daily digest email.
CREATE TABLE IF NOT EXISTS activity_log (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id       UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    actor_id       UUID REFERENCES users(id),
    actor_email    TEXT NOT NULL DEFAULT '',
    subject_email  TEXT NOT NULL DEFAULT '',
    action         TEXT NOT NULL,
    summary        TEXT NOT NULL DEFAULT '',
    detail         JSONB,
    after_deadline BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS activity_log_event_idx   ON activity_log(event_id, created_at DESC);
CREATE INDEX IF NOT EXISTS activity_log_subject_idx ON activity_log(event_id, subject_email);
