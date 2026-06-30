# ID5 IRL Attendance App — Architecture & Reference

Technical documentation for the IRL Attendance App: a web app for collecting
attendee information ahead of company offsites ("IRLs"). It describes the
architecture, data model, API surface, authentication model, conditional form
rules, and operational setup as they exist today.

---

## 1. Overview

An **admin** (the People team) configures an event once; **employees** sign in
with Google SSO and submit their attendance + travel details through a form with
conditional logic. The app tracks non-responders against each event's list of
expected attendees (drawn from the company-wide employee directory), sends
automated reminders, notifies admins on edits, and exports responses as CSV.

### What the app does

- **Admin setup** — event name, dates (per-day travel/event type), location
  (country + city), hotel (name + address), submission deadline, **timezone**
  (default `Europe/Paris`), and reminder lead days (default 3).
- **Attendee form** — attendance (Yes / No / Not sure, the last requiring a
  reason); conditional travel + long-haul (extra night before the offsite)
  on Yes; an instructions message on No. Name and dietary preferences live on the
  user's profile, not the form.
- **Edit, activity log & notify** — attendees may edit after submitting; every
  action is recorded in an activity log (employees see their own; admins see all,
  with after-deadline edits highlighted); admins are emailed on any change and can
  enable a per-event daily activity digest.
- **Access** — Google SSO restricted to `@id5.io`; each event has a shareable URL.
- **Attendees + dashboard** — each event has a list of expected attendees, who are
  company-directory users; admins import them via CSV (provisioning users) or add
  existing employees, and anyone who responds is added automatically. The
  dashboard buckets everyone by attending state (yes / no / not sure / no
  response), filterable, and auto-reloads (5s / 15s / 1m / 5m / off).
- **Lifecycle** — events are never deleted; past events move to a separate area
  and stay admin-editable.
- **Export** — CSV export of responses, following the active dashboard filter.
- **Reminders** — automated emails to non-responders: one per week plus
  configurable daily emails in the run-up to the deadline; timing is
  admin-configurable and evaluated in the event timezone.
- **MCP** — an optional, OAuth-gated MCP server lets an admin query and manage
  events from an MCP client (e.g. Claude). Disabled unless configured (§16).

---

## 2. Technology stack

| Layer | Choice | Notes |
|---|---|---|
| Backend language | **Go 1.26** | single module (`irlplanner`), `internal/` packages |
| HTTP router | **go-chi/chi v5** + middleware (`RequestID`, `RealIP`, `Recoverer`, `cors`, `httprate`) | |
| Database | **PostgreSQL 16** via **pgx/v5** through the `database/sql` adapter | `QueryExecModeExec`, no statement cache (PgBouncer-safe) |
| Migrations | Embedded `.sql` files run sequentially in `db.Migrate` | `//go:embed migrations/NNNN_*.sql`; no external migration tool |
| Auth | **OIDC** (`coreos/go-oidc/v3`) + **JWT** sessions (`golang-jwt/jwt/v5`) | Google Workspace `hd`-claim domain restriction → `id5.io` |
| Email | stdlib `net/smtp` wrapper (`internal/email`) | best-effort; empty `SMTP_HOST` disables it |
| Slack | stdlib HTTP client over the Slack Web API (`internal/slack`) | optional outreach channel; bot DMs via `users.lookupByEmail` + `chat.postMessage`; empty `SLACK_BOT_TOKEN` disables it |
| Metrics | **Prometheus** (`/metrics`), `/healthz`, `/readyz` | `internal/metrics` |
| Background jobs | goroutines bound to a root `context`, tracked by a `sync.WaitGroup`; `time.Ticker` schedulers | reminder + digest scheduler |
| MCP | **`modelcontextprotocol/go-sdk`** Streamable HTTP server at `/mcp` + **OAuth 2.1** (PKCE) | optional, admin-facing; off unless configured |
| Frontend | **Vue 3** (`<script setup>`) + **vue-router** + **Pinia** | lazy-loaded views, `beforeEach` auth guard |
| Build/tooling | **Vite**, **TypeScript**, **vue-tsc**, **ESLint** | `npm run check` = typecheck + lint + test |
| Frontend tests | **Vitest** + `@testing-library/vue` + **MSW** | |
| Frontend HTTP | thin `fetch` wrapper in `src/api.ts` with `ApiError` + client-side JWT-exp check | |
| Packaging | multi-stage Dockerfiles; **nginx** serves the SPA and proxies `/api` | |
| Orchestration | Docker **Compose** (db + backend + frontend) for dev; **Helm** + **ArgoCD** for prod | |

The backend is **stateless** apart from Postgres and outbound SMTP — there is no
on-disk state, so no backend PVC is required.

---

## 3. High-level architecture

```
                              ┌──────────────────────────────────────┐
   Browser (Vue SPA)          │            nginx (frontend pod)       │
   ───────────────────  HTTPS │  /            → SPA static files      │
   Google sign-in button ────►│  /api/*       → proxy → backend:8080  │
   Attendee form / Dashboard  │  /healthz     → proxy → backend       │
                              └──────────────────────────────────────┘
                                              │  (Helm: cluster Ingress
                                              │   does path routing instead)
                                              ▼
                    ┌───────────────────────────────────────────────┐
                    │              Go backend (chi)                   │
                    │  /api/auth/oidc/*   OIDC login/callback/logout  │
                    │  /api/me            session identity            │
                    │  /api/events*       admin CRUD + config         │
                    │  /api/events/:slug  attendee-facing event view  │
                    │  /api/events/:slug/submission  form CRUD        │
                    │  /api/events/:id/attendees (import/add/remove)  │
                    │  /api/events/:id/dashboard /export.csv  stats   │
                    │  /mcp           MCP Streamable HTTP (optional)  │
                    │  /oauth, /.well-known  OAuth 2.1 for /mcp        │
                    │                                                 │
                    │  background goroutines (root ctx + WaitGroup):  │
                    │   • reminder scheduler (time.Ticker)            │
                    │   • admin-notify email sends (on edit)          │
                    └───────────────────────────────────────────────┘
                                              │ pgx (QueryExecModeExec)
                                              ▼
                    ┌───────────────────────────────────────────────┐
                    │              PostgreSQL 16                      │
                    │  users, events, event_days, event_attendees,   │
                    │  submissions, submission_revisions,            │
                    │  reminder_log, activity_log,                   │
                    │  oauth_auth_codes, oauth_refresh_tokens,       │
                    │  oauth_pending                                 │
                    └───────────────────────────────────────────────┘
                                              │ SMTP
                                              ▼
                                    Outbound email (reminders, admin notify)
```

The MCP endpoint and its OAuth tables are present but inert unless MCP is
configured (§16); the core app runs fully without them.

---

## 4. Repository layout

```
irl-planner-pro/
├── compose.yml
├── .env.example
├── README.md
├── DESIGN.md                      ← this document
├── backend/
│   ├── go.mod                     (module: irlplanner)
│   ├── Dockerfile
│   ├── cmd/server/main.go         boot: config → db → migrate → schedulers → http
│   └── internal/
│       ├── config/                env loading + validation
│       ├── db/                    Open + Migrate + embedded migrations/
│       │   └── migrations/0001_init.sql … NNNN_*.sql (sequential)
│       ├── email/                 SMTP sender
│       ├── metrics/               Prometheus middleware
│       ├── workspaceauth/         Google hd-claim validation
│       └── server/
│           ├── app.go             App struct (Cfg, DB, OIDC, Email)
│           ├── router.go          chi route wiring
│           ├── auth.go            JWT mint/verify, authMiddleware, requireAdmin
│           ├── oidc.go            OIDC login/callback/logout
│           ├── users.go           /api/me, provisioning (first-user-admin), promote/demote
│           ├── events.go          event CRUD + per-day config
│           ├── attendees.go       attendee import/add/remove (provisions users)
│           ├── submissions.go     attendee form CRUD + conditional validation
│           ├── dashboard.go       counts by attending state, unified attendee list
│           ├── export.go          CSV export
│           ├── activity.go        activity_log writes + read endpoints
│           ├── reminders.go       scheduler + reminders + daily digest + reminder_log
│           ├── notify.go          admin "submission changed" emails
│           ├── timeutil.go        event-tz <-> UTC helpers, "is past" / "today"
│           ├── mcp.go             MCP server + tool registrations
│           ├── oauth.go           OAuth 2.1 authorize/token + discovery
│           └── errors.go          JSON/HTML error responses
├── frontend/
│   ├── Dockerfile, nginx.conf, nginx-security-headers.conf
│   ├── package.json, vite.config.ts, tsconfig*.json
│   └── src/
│       ├── main.ts, App.vue, router.ts, api.ts, types.ts, styles.css
│       ├── stores/   auth.ts, events.ts
│       ├── views/
│       │   ├── LoginView.vue          Google sign-in
│       │   ├── OIDCCallbackView.vue   token handoff
│       │   ├── EventListView.vue      admin: events index (current vs Past tabs)
│       │   ├── EventEditView.vue      admin: configure event + days + tz + reminders
│       │   ├── EventDashboardView.vue admin: Responses / Activity / Attendees tabs
│       │   ├── AttendeeFormView.vue   employee: conditional form + My-activity (/events/:slug)
│       │   ├── WelcomeView.vue         first-login profile confirm (/welcome)
│       │   ├── ProfileView.vue         edit own name + allergies (/profile)
│       │   ├── UsersView.vue          admin: list users, promote/demote
│       │   └── ErrorView.vue          403/404/500
│       ├── components/  ActivityLog.vue, AttendingFilter.vue
│       ├── lib/         datetime.ts (event-tz formatting via Intl)
│       └── composables/ useAutoReload.ts (dashboard polling), useConfirm.ts
└── helm/
    └── irl-planner-pro/   Chart.yaml, values.yaml, templates/
```

---

## 5. Data model

Postgres, UUID PKs (`gen_random_uuid()` via `pgcrypto`), `TIMESTAMPTZ` for all
instants. The schema is built by sequential embedded migrations, each run in
order on every boot and written to be idempotent. The definitions below show the
**current** shape of each table.

### 5.1 `users`
Provisioned on first OIDC login.

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT UNIQUE NOT NULL,         -- @id5.io, lower-cased
    first_name  TEXT NOT NULL DEFAULT '',     -- seeded from OIDC given_name on first login
    last_name   TEXT NOT NULL DEFAULT '',     -- seeded from OIDC family_name on first login
    allergies   TEXT NOT NULL DEFAULT '',     -- dietary preferences; a profile property, not per-event
    profile_confirmed BOOLEAN NOT NULL DEFAULT false, -- true once the user reviews the IdP-seeded profile
    is_admin    BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,                  -- NULL = provisioned by an admin import, never signed in
    token_version INTEGER NOT NULL DEFAULT 0  -- bump to revoke all sessions
);
```

**`users` is the company directory.** It is the single canonical record for every
employee, populated two ways: on **first login** (OIDC/dev), or by an **admin
import** that provisions a row from a name + email before the person has ever
signed in. `last_login_at` distinguishes the two — it is `NULL`
for a provisioned-but-never-logged-in user and stamped by `findOrCreateUser` on
every login. Because an import matches on email, a provisioned user's first real
login reuses the same row (their admin-entered name is preserved). Per-event
membership lives in `event_attendees` (§5.4), not here.

**Name = a profile property.** The user's name lives on `users` as `first_name` /
`last_name`, seeded from the OIDC `profile` scope (`given_name` / `family_name`, or
the split `name` claim) **only when the account is created**. It is **never refreshed
from the IdP on later logins**, so a user's own edit always wins. Users edit their
name at `/profile` (`PUT /api/me`); the API also returns a derived read-only `name`
(first + last) for display. Submissions carry **no** name — every name shown on a
dashboard, export, or activity line is read from the submitter's profile.

**Allergies = a profile property too.** Allergies / dietary preferences describe
the person rather than any one event, so they live on `users.allergies` and are
edited at `/profile` alongside the name (the same `PUT /api/me` payload). They are
entered once and reused for every event. The submission read shape still exposes an
`allergies` field for dashboards/exports, but it is joined in from the submitter's
profile, not stored per submission.

**First-login profile confirmation.** Because the IdP's given/family split is often
wrong and it never carries dietary needs, a newly provisioned user is asked to
confirm or correct their name and allergies before anything else. `profile_confirmed`
is `false` for new accounts and flips `true` on the first
`PUT /api/me` — saving the profile *is* the confirmation.
The SPA router guard sends any authenticated, unconfirmed user to `/welcome` (a
focused, chrome-less confirm step pre-filled from the seeded values) ahead of their
intended destination, then forwards them on once they save.

**Admin bootstrap.** The **first user to sign in** is made admin automatically
(`is_admin = true` when the `users` table is otherwise empty — decided atomically
in SQL so concurrent first logins can't both win). From then on, admins
**promote/demote** other users in-app. There is no self-service registration; the
`@id5.io` domain restriction is the gate. A guard prevents the last remaining
admin from being demoted, so a deployment is never left without an admin.

### 5.2 `events`

```sql
CREATE TABLE events (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug               TEXT UNIQUE NOT NULL,      -- shareable URL: /events/<slug>
    name               TEXT NOT NULL,             -- "IRL Dubrovnik October 2026"
    country            TEXT NOT NULL DEFAULT '',
    city               TEXT NOT NULL DEFAULT '',
    hotel_name         TEXT NOT NULL DEFAULT '',
    hotel_address      TEXT NOT NULL DEFAULT '',
    timezone           TEXT NOT NULL DEFAULT 'Europe/Paris',  -- IANA tz; UI renders all dates/times in this
    start_date         DATE NOT NULL,             -- first travel day (event-local)
    end_date           DATE NOT NULL,             -- last travel day (event-local)
    submission_deadline TIMESTAMPTZ NOT NULL,     -- stored UTC; entered/shown in event tz
    -- reminders
    reminder_days_before INTEGER NOT NULL DEFAULT 3,  -- daily emails this many days pre-deadline
    weekly_reminders     BOOLEAN NOT NULL DEFAULT true,
    reminder_hour        INTEGER NOT NULL DEFAULT 9,  -- hour-of-day (0-23) in the EVENT timezone
    daily_activity_email BOOLEAN NOT NULL DEFAULT false, -- admin digest; sent only when ≥1 activity that day
    -- messaging templates — admin-editable invite/reminder copy for the
    -- Messaging tab. '' means "no override" → generated default (§9).
    invite_subject     TEXT NOT NULL DEFAULT '',
    invite_body        TEXT NOT NULL DEFAULT '',
    reminder_subject   TEXT NOT NULL DEFAULT '',
    reminder_body      TEXT NOT NULL DEFAULT '',
    created_by         UUID NOT NULL REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

The `slug` is the shareable URL component (e.g. `dubrovnik-oct-2026`), validated
against the slug regex `^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`.

**No deletion.** Events are never deleted (they hold historical attendance data).
An event is **past** when `end_date < today`, computed in the event's timezone. The
UI surfaces *current/upcoming* events prominently and tucks past events into a
separate "Past events" area. Past events are **read-only for employees** (the form
locks) but **fully editable by admins** — every such admin edit is captured in the
activity log (§5.8, §11). All timestamps are stored UTC and rendered in the event
zone (§6.3).

### 5.3 `event_days`
One row per calendar day in `[start_date, end_date]`, each typed. Generated on
event create with **first and last day = `travel`, the rest = `event`**; admins
can override per day.

```sql
CREATE TABLE event_days (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id  UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    day_date  DATE NOT NULL,
    day_type  TEXT NOT NULL CHECK (day_type IN ('travel','event')),
    UNIQUE (event_id, day_date)
);
```

### 5.4 `event_attendees`
The per-event membership: which **employees (users)** are expected at an event.
There is a single canonical person record — the company-wide `users` directory
(§5.1) — and **everyone is an attendee by default**: creating an event snapshots
every existing user onto it, and provisioning a brand-new user (first login or
import) links them onto every event that is not yet past. Admins remove anyone
who isn't expected; that removal is an explicit per-event unlink and is never
re-created. This table simply links users to an event.

```sql
CREATE TABLE event_attendees (
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, user_id)
);
```

Membership is **default-everyone**, then maintained additively. Links are
created by: event creation (snapshot every current user); user creation, which
links the new user onto all non-past events (`addUserToOpenEvents`, covering both
first login and CSV/MCP provisioning); a CSV import (name + work email) that
provisions a directory user for each new email and links them; an admin adding an
existing employee from the directory; and **submitting an RSVP auto-adds its
author** (server/submissions.go). Because everyone starts in and responding keeps
you in, the overview is exactly this set — there is no separate "off-roster"
category. Removing an attendee unlinks them only (their directory record and any
submission are kept) and sticks, since the only writers are these create-time
seeds.

### 5.5 `submissions`
One submission per (event, user). Holds all form fields including the conditional
travel block.

```sql
CREATE TABLE submissions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id           UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id            UUID NOT NULL REFERENCES users(id),
    -- No name or allergies columns: both are read from the attendee's users
    -- profile (§5.1).
    attending          TEXT NOT NULL CHECK (attending IN ('yes','no','not_sure')),
    not_sure_reason    TEXT NOT NULL DEFAULT '',  -- required when attending='not_sure'

    -- travel (only meaningful when attending='yes')
    arrival_day        DATE,
    arrival_time       TEXT,                       -- free text "14:30"
    arrival_mode       TEXT CHECK (arrival_mode IN ('flight','car','train','other')),
    arrival_details    TEXT NOT NULL DEFAULT '',   -- flight no. / other info
    departure_day      DATE,
    departure_time     TEXT,
    departure_mode     TEXT CHECK (departure_mode IN ('flight','car','train','other')),
    departure_details  TEXT NOT NULL DEFAULT '',
    arrival_independent   BOOLEAN NOT NULL DEFAULT false,  -- arrival self-arranged, no support
    departure_independent BOOLEAN NOT NULL DEFAULT false,  -- departure self-arranged, no support
    long_haul          BOOLEAN NOT NULL DEFAULT false,  -- intl flight 7h+
    -- Extra hotel nights modelled as an extended stay window (event-local dates).
    -- extra_stay_start: first night needing accommodation when EARLIER than the
    --   event's first travel day (start_date). NULL = no extra night before.
    -- extra_stay_end:   RETIRED. Late return is no longer offered, so the server
    --   always blanks this column. Kept (not dropped) so historical rows survive.
    -- Self-service cap: a non-admin may shift extra_stay_start by at most ONE day
    -- (extra_stay_start >= start_date - 1). Admins may set any earlier start (2+
    -- extra nights) for special cases. Mainly surfaced for long-haul travellers,
    -- but an admin may set it on any submission.
    extra_stay_start   DATE,
    extra_stay_end     DATE,
    -- extra_stay_self_funded: attendee arrives the day before and arranges their
    --   own accommodation (no company hotel) but still wants company transport.
    --   An alternative to extra_stay_start that legitimises an early arrival;
    --   mutually exclusive with it. See migration 0017.
    extra_stay_self_funded BOOLEAN NOT NULL DEFAULT false,

    -- allergies live on users (the profile), not here — see §5.1.
    comments           TEXT NOT NULL DEFAULT '',

    -- Set when an admin edits this response on the attendee's behalf (§8). Once
    -- locked the employee form is read-only; only admins can change it. The lock
    -- is permanent (no in-app unlock) and sticky (a later non-locking write — e.g.
    -- an MCP RSVP sync — never clears it). Migration 0015.
    locked             BOOLEAN NOT NULL DEFAULT false,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, user_id)
);
```

### 5.6 `submission_revisions`
Append-only history so admins can see what changed, and the source of the
"submission changed" admin notification.

```sql
CREATE TABLE submission_revisions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    submission_id UUID NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id),  -- who made the change
    snapshot      JSONB NOT NULL,                       -- full submission at this point
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 5.7 `reminder_log`
Idempotency ledger so a reminder is never sent twice for the same window even if
the scheduler ticks multiple times or the process restarts.

```sql
CREATE TABLE reminder_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id      UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    recipient     TEXT NOT NULL,                       -- attendee email
    reminder_kind TEXT NOT NULL CHECK (reminder_kind IN ('weekly','deadline','daily_digest','invitation','manual')),
    period_key    TEXT NOT NULL,                       -- e.g. '2026-W40' or '2026-10-12'
    sent_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, recipient, reminder_kind, period_key)
);
```

The Messaging tab (§9) reuses this same claim ledger for exactly-once admin
sends: the `invitation` kind (fixed period key `invitation`) emails each
attendee at most once, and the `manual` kind (event-local date period key)
stops a repeated same-day "send follow-up now" from double-sending. A failed
send releases its claim so the next attempt retries.

### 5.8 `activity_log`
The human-readable audit trail of everything that happens on an event. Drives the
employee's "my activity" view, the admin all-activity view (where post-deadline
edits stand out), and the daily activity digest email. Append-only.

```sql
CREATE TABLE activity_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id    UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    actor_id    UUID REFERENCES users(id),    -- who acted (null for system actions)
    actor_email TEXT NOT NULL DEFAULT '',     -- denormalised for stable display
    subject_email TEXT NOT NULL DEFAULT '',   -- whose data was affected (the attendee)
    action      TEXT NOT NULL,                -- see action vocabulary below
    category    TEXT NOT NULL DEFAULT 'admin', -- 'user' | 'admin' — what was done, not who did it
    summary     TEXT NOT NULL DEFAULT '',     -- pre-rendered, human-readable line
    detail      JSONB,                        -- optional structured diff / context
    after_deadline BOOLEAN NOT NULL DEFAULT false, -- true if created past the event deadline
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX activity_log_event_idx    ON activity_log(event_id, created_at DESC);
CREATE INDEX activity_log_subject_idx  ON activity_log(event_id, subject_email);
CREATE INDEX activity_log_category_idx ON activity_log(event_id, category, created_at DESC);
```

**Action vocabulary** (extensible): `submission.created`, `submission.updated`,
`submission.attending_changed`, `event.created`, `event.updated`,
`event.config_changed`, `attendees.imported`, `attendee.added`,
`attendee.removed`, `admin.edited_submission`,
`reminder.sent`. The `summary` is computed at write time so both the UI and the
digest email render without re-deriving anything. `after_deadline` is stamped by
comparing `now()` to the event's `submission_deadline` — it's the single flag the
admin UI uses to highlight late changes.

**`category` classifies *what was done*, not who did it.** It is a pure function
of `action`, computed at write time (`actionCategory` in `activity.go`).
Only the two participant submission verbs (`submission.created`,
`submission.updated`) are `user`; everything else — event config, roster
management, `admin.edited_submission`, `reminder.sent` — is `admin`. The key
consequence: an admin account is also an employee, so when an admin submits
**their own** attendance the entry is `user`, not `admin`. This lets the admin
all-activity view default to the participant stream (the common review case) and
narrow to administrative actions on demand. The admin endpoint accepts
`?category=user|admin` (empty = all); the MCP `get_activity` tool takes the same
optional `category`. The employee "my activity" view is unaffected (it is already
scoped to the caller's own subject).

`submission_revisions` (§5.6) is the *full-snapshot* store (for precise
field-level diffs); `activity_log` is the *timeline* layered on top.

### 5.9 OAuth tables (MCP)
Backing store for the OAuth 2.1 Authorization Code + PKCE flow that guards the MCP
endpoint (§16). These tables exist on every deployment but are only written to
when MCP is enabled. Auth codes and refresh tokens are stored **hashed**; the
plaintext is only ever held by the client.

```sql
CREATE TABLE oauth_auth_codes (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash      TEXT NOT NULL UNIQUE,
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    redirect_uri   TEXT NOT NULL,
    code_challenge TEXT NOT NULL,                  -- PKCE S256 challenge
    expires_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE oauth_refresh_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT NOT NULL UNIQUE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Transient store for OAuth params while the user authenticates via OIDC.
-- Keyed by the nonce embedded in the OIDC state parameter ("oauth:<state_key>").
CREATE TABLE oauth_pending (
    state_key      TEXT PRIMARY KEY,
    redirect_uri   TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    oauth_state    TEXT NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 5.10 `event_images`
Optional cover image per event, shown on the home page feature card and the
attendee RSVP page. Stored **out-of-line** from the `events` row (1:1, PK =
`event_id`) so the hot event reads never pull the binary — the image is fetched
only by its own endpoint. `etag` is the SHA-256 content hash; it both drives HTTP
caching (`ETag` / `304`) and is appended as `?v=<etag>` to the image URL so a
replaced image is picked up immediately despite long browser caching.

```sql
CREATE TABLE event_images (
    event_id     UUID PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    content_type TEXT NOT NULL,    -- server-sniffed: image/{jpeg,png,gif,webp}
    data         BYTEA NOT NULL,   -- raw bytes, ≤ 4 MiB (matches the nginx body limit)
    etag         TEXT NOT NULL,    -- sha256 hex of data
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Upload is a single upsert (`ON CONFLICT (event_id) DO UPDATE`); the content type
is sniffed server-side via `http.DetectContentType`, never trusted from the
client. `loadEventByColumn` and the active-events list `LEFT JOIN` this table for
the `etag` only (never the blob) and expose `imageUrl` on the event JSON (`""`
when there is no image).

### 5.11 `message_send_log`
Append-only **result** ledger: one row per outbound message attempt (invitation,
manual follow-up, or scheduled reminder), over either channel. Distinct from
`reminder_log` — that is the exactly-once *claim* ledger ("did we attempt this
recipient for this window?"); this records the *outcome* ("did it succeed, and if
not why"). The Messaging tab reads it to show recent failures so an admin can fix
a bad address or an unmapped Slack user and resend.

```sql
CREATE TABLE message_send_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    recipient  TEXT NOT NULL,
    kind       TEXT NOT NULL,   -- invitation | manual | weekly | deadline
    channel    TEXT NOT NULL,   -- email | slack
    status     TEXT NOT NULL CHECK (status IN ('sent','failed')),
    error      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`status='sent'` means the channel *accepted* the message (SMTP relay / Slack API
`ok`), not that it was delivered — asynchronous email bounces and Slack delivery
are not tracked.

---

## 6. Authentication & access control

### 6.1 Sign-in
- **Three deployment profiles** (see `.env.example`):
  - **prod** — `AUTH_MODE=oidc`, Google as the OIDC provider, restricted to id5.io.
  - **stage** — `AUTH_MODE=oidc` against Keycloak with `OIDC_GOOGLE_WORKSPACE_DOMAINS`
    empty, so any Keycloak user is allowed (it's stage).
  - **local** — `AUTH_MODE=password`, a dev stub that mints a session for any email
    with no real credential check (`handleDevLogin`); never enabled on a shared
    deployment.
- In prod, `OIDC_GOOGLE_WORKSPACE_DOMAINS=id5.io` enforces the `hd` claim via
  `workspaceauth.ValidateGoogleHD` (and is sent as the `hd` auth hint). Anyone
  outside `@id5.io` is rejected at callback with a generic "domain not allowed"
  page. The check only applies to the Google issuer, so stage/Keycloak is
  unaffected.
- On successful callback the user is **upserted** into `users` (no approval queue —
  the domain restriction is sufficient). The **first** user ever provisioned is
  made admin (§5.1); everyone after is a regular employee until an admin promotes
  them.
- A signed **JWT** (30-day expiry, `token_version` embedded) is handed to the SPA
  via the `/auth/callback#token=…` URL-fragment flow, stored in `localStorage`,
  and sent as `Authorization: Bearer`.

### 6.2 Authorization
Two roles:

| Capability | Employee | Admin (People team) |
|---|---|---|
| Sign in (`@id5.io`) | ✓ | ✓ |
| View an event page by URL, submit/edit own response (current events) | ✓ | ✓ |
| View own activity log | ✓ | ✓ |
| Create/configure events (incl. past events) | — | ✓ |
| Edit any attendee's submission (logged) | — | ✓ |
| Import / add / remove attendees | — | ✓ |
| View dashboard + full activity log | — | ✓ |
| Export CSV | — | ✓ |
| Configure reminders / daily digest | — | ✓ |
| Promote/demote admins | — | ✓ |

Enforced by chi middleware: `authMiddleware` (verifies JWT, loads user, checks
`token_version`) then `requireAdminMiddleware` on the admin route group. The
frontend router `beforeEach` guard mirrors this for UX (redirect to `/login`, 403
page for non-admins) but the **backend is the source of truth**.

**Past-event edits.** Employees may create/edit their own submission only while
the event is current/upcoming (`end_date >= today`). Once an event is past, the
employee form is read-only — but **admins can still edit any submission and any
event config**, at any time. Each admin edit (and any edit landing after the
submission deadline) is recorded in the activity log with `after_deadline=true` so
it is conspicuous in the admin timeline.

### 6.3 Time zones & date handling

Each event carries an IANA **`timezone`** (default `Europe/Paris`). The rules:

- **Storage is always UTC.** `TIMESTAMPTZ` columns hold UTC; `DATE` columns
  (`start_date`, `end_date`, `event_days.day_date`, arrival/departure days) are
  event-local calendar dates with no zone — they mean "that day at the offsite".
- **Display and input are in the event timezone.** The SPA renders every
  timestamp (deadline, activity-log entries, reminder times) converted into the
  event's zone, and labels it (e.g. "12 Oct 2026, 17:00 CEST"). When an admin
  enters the submission deadline as a local date-time, the backend interprets it
  in the event's `timezone` and converts to UTC for storage.
- **Backend** uses Go's `time.LoadLocation(event.Timezone)` to resolve the zone
  for: interpreting the entered deadline, computing the reminder send window
  (`reminder_hour` is event-local), and deciding "today" / "is the event past".
- **`timezone` is validated** at write time against the tz database
  (`time.LoadLocation` must succeed) — an invalid zone is a 400.

---

## 7. Backend API surface

All under `/api`, JSON, with `httprate` per-IP throttles on auth and
mutation-heavy endpoints.

### Public / auth
```
GET  /api/version                      build info
GET  /api/auth/config                  { mode, ... } for the SPA
GET  /api/auth/oidc/login              → redirect to Google
GET  /api/auth/oidc/callback           ← Google, mints JWT, redirects to SPA
GET  /api/auth/oidc/logout             RP-initiated logout
GET  /api/me                           current user { id, email, firstName, lastName, name, allergies, profileConfirmed, isAdmin, createdAt }
PUT  /api/me                           edit own profile { firstName, lastName, allergies } (names required; allergies optional) — also flips profile_confirmed=true (first-login confirm, §5.1)
```

### Attendee-facing (any signed-in @id5.io user)
```
GET  /api/active-events                current/upcoming events the caller can RSVP to
GET  /api/events/:slug                 event details + typed days + timezone + imageUrl (form header)
GET  /api/events/:slug/image           cover image bytes — PUBLIC (no auth, so a plain <img src> loads it); ETag-cached, 404 if none
GET  /api/events/:slug/submission      caller's own submission (404 if none)
PUT  /api/events/:slug/submission      create/update own submission (upsert; 403 if event is past)
GET  /api/events/:slug/activity        caller's OWN activity entries for this event
```

### Admin (requireAdmin)

Event-management routes are namespaced under **`/api/admin/events/{id}`** (id-keyed)
so they never collide with the slug-keyed attendee read `/api/events/{slug}`.
```
GET    /api/users                      list users (email, firstName, lastName, name, isAdmin)
POST   /api/users/:id/promote          grant admin
POST   /api/users/:id/demote           revoke admin (blocked for the last admin)

GET    /api/admin/events?scope=current|past  list events, split current vs past (default current)
POST   /api/admin/events               create event (+ generate event_days)
GET    /api/admin/events/:id           full event config
PUT    /api/admin/events/:id           update event + day types + reminder config + message templates (admins: even when past)
POST   /api/admin/events/:id/image     upload cover image (multipart "image", ≤4 MiB), upsert; returns { imageUrl }
DELETE /api/admin/events/:id/image     remove cover image (idempotent, 204)

POST   /api/admin/events/:id/attendees           import CSV (multipart), additive — provisions users + links them
POST   /api/admin/events/:id/attendees/:userId   add an existing directory user as an attendee (idempotent, 204)
DELETE /api/admin/events/:id/attendees/:userId   remove an attendee (unlink only, idempotent, 204)

GET    /api/admin/events/:id/dashboard       counts keyed by attending state + per-attendee rows (see §10)
GET    /api/admin/events/:id/submissions     all submissions (table view; admins may edit any)
PUT    /api/admin/events/:id/submissions/:userId  admin edit of an attendee's submission
GET    /api/admin/events/:id/activity?category=user|admin  ALL activity for the event (timeline; flags after_deadline; empty category = all)
GET    /api/admin/events/:id/export.csv      CSV download of all submissions

GET    /api/admin/events/:id/messaging           templates + defaults + audience stats + channel availability + recent failures
PUT    /api/admin/events/:id/messaging           save the editable invite/reminder templates
POST   /api/admin/events/:id/messaging/invite    send invitation to not-yet-invited attendees ({ channel }: email|slack)
POST   /api/admin/events/:id/messaging/followup  send follow-up to current non-responders now ({ channel }: email|slack)
```

There is no delete endpoint — events persist and become read-only-to-employees
once past (§5.2, §6).

`PUT /submission` runs the **conditional validation** (§8), writes the
`submissions` row, appends a `submission_revisions` snapshot, writes an
`activity_log` entry (stamping `after_deadline`), and — if this is an edit of an
existing submission — enqueues an **admin notification** email (§9.2). Admin edits
via `PUT …/submissions/:userId` follow the same path but log the
`admin.edited_submission` action with the admin as `actor` and the attendee as
`subject`. They additionally **bypass all field validation** (any day, any option,
no required fields — §8) and **lock** the response: `locked` is set so the
attendee can no longer edit it from the form. The lock is permanent and sticky
(§5.5).

### MCP & OAuth
```
/mcp                                    MCP Streamable HTTP (admin tools; see §16)
/oauth/authorize  /oauth/token          OAuth 2.1 Authorization Code + PKCE
/.well-known/oauth-authorization-server RFC 8414 discovery
/.well-known/oauth-protected-resource   RFC 9728 protected-resource metadata
```
These are mounted only when MCP is configured and are gated by
`mcpTokenGateMiddleware` (OAuth bearer) rather than the SPA's JWT. Detailed in §16.

---

## 8. Attendee form & conditional logic

The form is a single Vue view (`AttendeeFormView.vue`) driven by reactive state;
the **same rules are enforced server-side** in `submissions.go` (the client is
never trusted).

### Step 1 — Basic details (always)
- The attendee's **name and allergies are not collected here** — they come from
  their profile (`first_name` / `last_name` / `allergies`, editable at `/profile`).
  The form shows a read-only "Submitting as …" block (name + dietary preferences)
  with a single link to edit the profile.
- **Attending?** `yes` / `no` / `not_sure`.
  - `not_sure` → `not_sure_reason` **required** (the server rejects empty): an
    employee who can't commit to yes/no before the deadline must say why.

### Branch: `attending = no`
- No further fields. The UI shows the fixed instructions message:
  > If for any reason you cannot attend this offsite, please follow the steps below:
  > 1. Let your manager know
  > 2. Inform the People team by emailing irl@id5.io

  (Stored as a constant; no DB field needed.)

### Branch: `attending = yes` → travel + other
- **Independent traveller (per leg).** Travel *to* and *from* the offsite are
  separate decisions — an attendee may self-arrange one leg and have the People
  team book the other. The top item of **each** leg's **Day** dropdown is "I
  arrange my own travel here, no support needed" (`arrival_independent` /
  `departure_independent`). Selecting it hides and blanks only that leg's
  time/mode/details and skips its validation. The long-haul/accommodation section
  is hidden and blanked only when **both** legs are independent.
- **Arrival** — day (constrained to the day before the event through its last day —
  there is no day-after option), time, mode
  (`flight`/`car`/`train`/`other`), details (flight number / free text). When the
  mode is `flight` the **Time** field is labelled "Flight arrival time" ("Flight
  departure time" on the departure leg) and the details field "Flight number", and
  **both time and flight number are required**; for every other mode time and
  details stay **optional** (the details label reads "Travel details (optional)").
- **Departure** — same shape.
- **The night before** (employee form). The accommodation question appears **only
  when the attendee arrives the day before the event** (`arrival_day = start_date −
  1` on a non-independent arrival leg) — that's the only night needing cover.
  Arriving on the event day shows nothing. It is a **single mutually-exclusive
  choice** of how that night is handled:
  - **"The company books and pays for it"** — for long-haul travellers (intl flight
    7h+). Sets `long_haul = true` **and** `extra_stay_start = start_date − 1`
    together.
  - **"I'll book and pay for it myself"** — self-funded; the attendee still wants
    company transport / shared-transfer consideration. Sets `extra_stay_self_funded
    = true`. See [Self-funded early arrival](#self-funded) below.
  The two are mutually exclusive (`long_haul`/`extra_stay_start` is one outcome,
  `extra_stay_self_funded` the other), and the three flags are written atomically so
  they never disagree. **Late return is not offered** — there is no "night after"
  option and `extra_stay_end` is never written (column retained only for historical
  data).
  - **Admin override** — in the admin submission editor `long_haul`,
    `extra_stay_start` (a *date picker* with no one-day cap, so the People team can
    grant 2+ extra nights for visa stopovers etc.) and `extra_stay_self_funded` are
    each independent controls with no validation. The after-night is blanked for
    every writer, admin included.
- <a name="self-funded"></a>**Self-funded early arrival** (`extra_stay_self_funded`).
  The self-pay branch of the choice above: the attendee arrives the day before and
  arranges their own accommodation, but still wants the People team's transport and
  to be considered for any shared transfer that day. Mutually exclusive with
  `extra_stay_start` (the company-paid night wins). Only meaningful for a day-before
  arrival on a non-independent arrival leg; blanked otherwise.
- **Comments** (free text). (Allergies / dietary preferences are **not** asked here
  — they live on the profile; see Step 1.)

### Validation matrix (server-enforced)

| Field | Required when |
|---|---|
| name | not on the submission — set on the user profile (`first_name` / `last_name` required there) |
| allergies / dietary | not on the submission — set on the user profile (`/profile`, optional) |
| `not_sure_reason` | `attending = 'not_sure'` |
| `arrival_independent` / `departure_independent` | optional; only when `attending = 'yes'`. When a leg's flag is true that leg is blanked and not validated. When **both** are true, `long_haul` + extra-night dates are blanked too |
| `arrival_*` | `attending = 'yes'` **and** `arrival_independent = false` (day + mode required; when mode = `flight`, time + flight-number details also required, else optional) |
| `departure_*` | `attending = 'yes'` **and** `departure_independent = false` (day + mode required; when mode = `flight`, time + flight-number details also required, else optional) |
| `extra_stay_start` | optional; the extra night before, one-day cap from the event start for employees, unrestricted for admins. `extra_stay_end` (late return) is always blanked — no longer offered |
| `extra_stay_self_funded` | optional; a day-before arrival must be covered by **either** `extra_stay_start` (company night) **or** this flag, else rejected. Mutually exclusive with `extra_stay_start`; blanked unless arriving the day before on a non-independent arrival leg |
| comments | optional |

Fields outside the chosen branch are blanked on write so a user toggling Yes→No
doesn't leave stale travel data.

**Admin edits bypass this entire matrix.** When an admin edits a response on an
attendee's behalf (`PUT …/submissions/:userId`, the admin editor in the Responses
tab), every field-level rule above is dropped: any arrival/departure day (a free
date picker, not a constrained dropdown), any/empty mode, no required fields, no
date windows, no extra-night caps or consistency checks. Only the branch-blanking
and the parse/enum normalization the DB columns demand still run (a day must be a
real date or NULL; a mode must be a valid enum value or NULL; `attending` must be
one of the three). Saving an admin edit **locks** the response (§5.5): the
attendee form goes read-only and only admins can change it thereafter.

### Editing
The same `PUT` endpoint handles create and edit (upsert on `(event_id,user_id)`).
The form pre-loads the existing submission via `GET …/submission`. For employees,
editing is allowed **before and after the deadline** (the deadline gates
*reminders* and the meaning of *"not sure"*, not the ability to edit) **as long as
the event is not yet past and the response has not been admin-locked** — once
`end_date < today` *or* an admin has edited the response (§5.5), the employee form
goes read-only (the server returns 403 on an employee write to a locked or past
submission). Admins can always edit, including past and locked events. Every save appends a
`submission_revisions` snapshot **and** an `activity_log` entry; edits that land
after the submission deadline are stamped `after_deadline=true` so they stand out
in the admin timeline (§11). The admin notification email fires on edits (§9.2).

---

## 9. Notifications & reminders

Outbound email uses `internal/email.Sender` (stdlib SMTP). All email is
**best-effort**: a send failure logs a WARN and never fails the user's request.
An empty `SMTP_HOST` disables email entirely.

**Channels.** The admin Messaging tab (invitations + manual follow-ups) can
dispatch over **email** or **Slack**. Slack delivery (`internal/slack.Notifier`)
posts a bot **direct message** to each recipient: the company email is resolved
to a Slack user ID with `users.lookupByEmail`, then `chat.postMessage` sends the
DM. It uses a workspace **bot token** (`SLACK_BOT_TOKEN`, scopes
`users:read.email` + `chat:write`), so the People team can message any employee
**without that employee installing or authorizing the app** — the enterprise
install model. An empty `SLACK_BOT_TOKEN` disables Slack (the tab shows it as
selectable but "not configured"). Each per-recipient send is recorded in
`message_send_log` with its `channel`, and the same `reminder_log` idempotency
claim makes Slack sends exactly-once and retry-safe, identical to email.
The scheduled reminders (§9.1) and admin notices (§9.2–9.3) remain email-only.

### 9.1 Reminder scheduler
A single background goroutine started in `main.go`, bound to the root context,
tracked by the `WaitGroup`, and driven by a `time.Ticker` (hourly). On each tick,
for every event that is still open (deadline not yet passed, event not past):

1. Compute the **non-responders**: attendee emails (from `event_attendees` joined
   to `users`) with **no `submissions` row** for
   the event. (Any submission — including `not_sure` — counts as a response; only
   true silence is chased. Reminders are about *getting a response*, while the
   dashboard then filters responses by `attending` state, §10.)
2. Decide which reminder windows are open *now*, evaluated in the **event
   timezone** at the event-local `reminder_hour`:
   - **Weekly** (`weekly_reminders = true`): one per ISO week → `period_key` =
     `2026-W40`.
   - **Deadline run-up** (`reminder_days_before`): one per day for the N days
     immediately before `submission_deadline` → `period_key` = the date.
3. For each non-responder × open window, attempt an insert into `reminder_log`
   (`ON CONFLICT DO NOTHING`). **Only if the insert created a row** is the email
   sent — this makes sends idempotent and restart-safe.

The email links the recipient to the event URL (`PUBLIC_BASE_URL/events/:slug`).

Because the recipient pool is the event's **attendees**, reminders may reach
people an admin imported who have never signed in. As a company-internal tool,
sending to any `@id5.io` address is acceptable without separate consent — there
is no opt-out flow.

Reminder timing is configured per event (`reminder_days_before`,
`weekly_reminders`, `reminder_hour`, `daily_activity_email`) via the event edit
form.

### 9.2 Admin "submission changed" notification
When `PUT …/submission` updates an **existing** submission (not the first create),
`notify.go` sends a summary email to `PEOPLE_TEAM_EMAIL` (and/or the current set of
admin users). The email names the employee, the event, and what changed (a diff
derived from the latest two `submission_revisions` snapshots). Sent asynchronously
so it never blocks the response.

### 9.3 Daily activity digest (admin, per event)
When an event has `daily_activity_email = true`, the reminder scheduler also
assembles a once-per-day digest of that event's `activity_log` entries from the
last 24h, in the event timezone at `reminder_hour`. **The email is sent only if
there is at least one activity in the window** — a quiet day produces no mail. The
digest groups entries (new submissions, edits, attending changes, roster uploads)
and visibly flags any `after_deadline` edits at the top, giving admins a low-effort
way to notice late changes without watching the dashboard. Idempotency reuses
`reminder_log` with `reminder_kind='daily_digest'` keyed by event + date so a
restart never double-sends.

---

## 10. Dashboard, non-responder tracking & export

### Dashboard (`EventDashboardView.vue`, admin)
The dashboard is organised around the **`attending` state**, not a binary
"submitted / not". Every **attendee** (§5.4) falls into one of four mutually
exclusive buckets — `yes`, `no`, `not_sure`, and `no_response` (no submission
row) — and the UI lets the admin **filter the table by any combination** of these
four states. Because every attendee is a directory user and everyone is an
attendee by default (§5.4), the overview is one unified list — there is no
separate "off-roster" section.

`GET /api/events/:id/dashboard` returns:
```json
{
  "total": 48,
  "counts": { "yes": 33, "no": 5, "notSure": 3, "noResponse": 7 },
  "entries": [
    { "userId": "…", "name": "…", "email": "…", "attending": "yes", "afterDeadlineEdit": false, "hasLoggedIn": true },
    { "userId": "…", "name": "…", "email": "…", "attending": "no_response", "afterDeadlineEdit": false, "hasLoggedIn": false }
  ]
}
```
- Each attendee is joined to their submission and assigned one of the four states;
  `no_response` is itself just one filterable state — the "who hasn't responded,
  by name" view is the `no_response` filter.
- `name` is read from the user's profile; `hasLoggedIn` is `false` for someone an
  admin imported who hasn't signed in yet (surfaced as a "not signed in" badge).
- Filtering is client-side over `entries` (small lists; the whole set is
  fetched each poll), with quick chips for each state and their counts.

**Auto-reload.** A `useAutoReload(intervalRef, fetchFn)` composable polls the
dashboard endpoint. A dropdown offers **5s / 15s / 1m / 5m / off**, default **1m**.
The composable cleans up its timer on unmount and pauses when the tab is hidden
(`visibilitychange`) to avoid wasted polls.

### Export (`GET /api/events/:id/export.csv`)
**One export button that follows the filter.** The single Export button downloads
*exactly what the dashboard filter currently shows*. The endpoint takes the same
filter the table uses:

```
GET /api/events/:id/export.csv?attending=yes,not_sure       # only those states
GET /api/events/:id/export.csv?attending=no_response        # the non-responders
GET /api/events/:id/export.csv                              # no filter → everyone
```

`attending` is a comma-separated subset of `{yes,no,not_sure,no_response}`
(mirroring the dashboard chips). Rows for `no_response` attendees are emitted
with empty submission columns, so the CSV doubles as a non-responder list when that
filter is active. One row per person, all form fields + email + timestamps
(rendered in the event timezone), streamed with the stdlib `encoding/csv` and
`Content-Disposition: attachment`. Because the filter is the single source of truth
for "which people", any future filter dimension (e.g. long-haul only, by arrival
day) extends both the table and the export for free.

### Attendee CSV import
`POST /api/events/:id/attendees` accepts `multipart/form-data`. Parsed with
`encoding/csv`; expected headers `name,email` (case-insensitive, tolerant of extra
columns). Validated: non-empty name, well-formed email; rows are lower-cased and
de-duplicated. Each row **provisions a directory user** if its email is new
(splitting the name into first/last) and links them to the event. The import is
**additive** — `ON CONFLICT DO NOTHING` on both `users` and `event_attendees`, so
re-running only ever adds people; removal is a separate per-person action
(`DELETE /api/events/:id/attendees/{userId}`). A report
(`{ added, skipped, errors[] }`) is returned for admin feedback. Admins can also
add an existing employee directly (`POST /api/events/:id/attendees/{userId}`).

---

## 11. Frontend

- **Vue 3 `<script setup>` + Pinia + vue-router**, lazy-loaded views.
- **`stores/auth.ts`** — token + user in `localStorage`, `ensureFreshUser()`
  re-validates `/api/me` once per load, `loginViaOIDC()` redirects to
  `/api/auth/oidc/login`, client-side JWT-exp short-circuit.
- **`stores/events.ts`** — admin event list/detail caching + mutations.
- **`api.ts`** — the thin `fetch` wrapper with `ApiError`, `isJwtExpired`, and a
  multipart helper for roster upload.
- **Router guard** — `requiresAuth` + admin-only meta on event-management routes;
  redirects to `/login` (with a `redirect` query) or `/error?code=403`. It also
  sends any authenticated, unconfirmed user to `/welcome` until they confirm their
  profile (§5.1).

### Routes
```
/login                         Google sign-in
/auth/callback                 OIDC token handoff
/welcome                       WelcomeView        (first-login profile confirm; the auth guard redirects here until profile_confirmed, §5.1)
/profile                       ProfileView        (any signed-in user; edit name + allergies)
/events/:slug                  AttendeeFormView   (any signed-in @id5.io user)   ← the shareable URL
                                 — includes a "My activity" panel (own log only)
/admin/events                  EventListView      (admin; current vs Past tabs)
/admin/events/new              EventEditView      (admin)
/admin/events/:id/edit         EventEditView      (admin)
/admin/events/:id              EventDashboardView (admin; Responses / Activity / Roster tabs)
/admin/users                   UsersView          (admin; promote/demote)
/error                         ErrorView (403/404/500)
```

`EventListView` separates **current/upcoming** from **Past** events, keeping the
past ones out of the way but reachable; admins can open a past event and still edit
it. A shared **`ActivityLog.vue`** component renders the timeline in both the
employee panel (scoped to their own entries) and the admin Activity tab (all
entries).

The **shareable event URL** given to employees is `/<base>/events/:slug`. An
unauthenticated visitor hits the auth guard, signs in with Google, and lands back
on the form.

### 11.1 Activity log & audit trail

The `activity_log` (§5.8) is surfaced two ways, both rendered by the shared
`ActivityLog.vue` (a reverse-chronological, plain-language timeline):

- **Employee — "My activity"** (`GET /api/events/:slug/activity`): scoped to the
  caller's `subject_email`. They see only their own history — "You set attending
  to *Yes* on 2 Oct", "You updated your travel details on 5 Oct". Read-only.
- **Admin — "Activity" tab** (`GET /api/events/:id/activity`): the entire event
  timeline across all attendees and admins. Entries are easy to scan (actor,
  subject, action, time in event tz). Any entry with `after_deadline = true` — and
  any `admin.edited_submission` — is visually highlighted so a change made after the
  deadline, or an admin editing on someone's behalf, is immediately obvious.
  A **Participant / Admin / All** segmented filter on `category` (§5.8) sits at the
  front of the toolbar and **defaults to Participant** — the People team almost
  always wants to review what attendees did, and only occasionally audits admin
  configuration. The tab also filters by free-text search and toggles sort order.

This is the mechanism that makes post-deadline editing *allowed but conspicuous*
(§6): nothing is blocked, but every late or admin-made change is on the record and
easy to find. The daily activity digest (§9.3) is the push companion to this pull
view.

---

## 12. Configuration (env vars)

Loaded by `config.Load()` (getenv with defaults, fail-fast validation). A trimmed
`.env.example`:

```dotenv
# Core
PUBLIC_BASE_URL=http://localhost:8080
LISTEN_ADDR=:8080
DATABASE_URL=postgres://irl:irl@db:5432/irl?sslmode=disable

# Session signing (>=32 chars; openssl rand -hex 32). Insecure default refused
# at boot unless ALLOW_INSECURE_JWT_SECRET=true (local dev only).
JWT_SECRET=change-me-please-use-32-chars-minimum
# ALLOW_INSECURE_JWT_SECRET=true

# Auth — OIDC. Google Workspace, restricted to id5.io.
AUTH_MODE=oidc
OIDC_ISSUER_URL=https://accounts.google.com
OIDC_CLIENT_ID=...
OIDC_CLIENT_SECRET=...
OIDC_REDIRECT_URL=                       # defaults to PUBLIC_BASE_URL + /api/auth/oidc/callback
OIDC_GOOGLE_WORKSPACE_DOMAINS=id5.io

# People team. The FIRST user to sign in becomes admin automatically; admins
# then promote/demote others in-app — no admin allowlist env needed.
PEOPLE_TEAM_EMAIL=irl@id5.io          # recipient for "submission changed" + digest notices

# SMTP (reminders + admin notifications). Empty SMTP_HOST disables email.
# The sender (internal/email) is a provider-agnostic stdlib net/smtp client
# (STARTTLS or implicit TLS + AUTH PLAIN), so any relay works with config alone.
# Production target is AWS SES via its SMTP interface — no code change, just point
# at the regional SES SMTP endpoint with SES-generated SMTP credentials:
#   SMTP_HOST=email-smtp.<region>.amazonaws.com   SMTP_PORT=587   SMTP_USE_TLS=true
# (SMTP_FROM must be a verified SES identity; the account must be out of the SES
#  sandbox to reach arbitrary recipients.) Port 465 instead → SMTP_IMPLICIT_TLS=true.
SMTP_HOST=
SMTP_PORT=587
SMTP_USERNAME=
SMTP_PASSWORD=
SMTP_FROM=irl-noreply@id5.io
SMTP_USE_TLS=true

# Slack messaging channel (optional). Workspace bot token (xoxb-…) with scopes
# users:read.email + chat:write. Empty disables Slack.
SLACK_BOT_TOKEN=

# Reminder + digest scheduler
REMINDERS_ENABLED=true
REMINDER_TICK_INTERVAL=1h
# Default IANA timezone pre-filled when an admin creates a new event.
DEFAULT_EVENT_TIMEZONE=Europe/Paris

# CORS (defaults derived from PUBLIC_BASE_URL)
# CORS_ALLOWED_ORIGINS=
# METRICS_TOKEN=

# MCP server. Both set → /mcp + OAuth 2.1 enabled; both empty → /mcp off.
# MCP_OAUTH_CLIENT_ID=
# MCP_OAUTH_CLIENT_SECRET=
# Allowlisted OAuth callback URIs (comma-separated). Defaults to Claude's connector.
# MCP_OAUTH_REDIRECT_URIS=https://claude.ai/api/mcp/auth_callback
```

Boot-time validation: the insecure/short `JWT_SECRET` is refused, OIDC vars are
required when `AUTH_MODE=oidc`, and an empty Workspace-domain allowlist logs a
warning.

---

## 13. Deployment

### Dev — Docker Compose
`compose.yml` runs three services:
- `db` — `postgres:16-alpine`, with a healthcheck and a volume.
- `backend` — built from `./backend`, env from `.env`, depends on a healthy db.
- `frontend` — built from `./frontend`, nginx on `:8080`, proxies `/api` and
  `/healthz` (and, when MCP is enabled, `/mcp`, `/oauth`, `/.well-known`) to the
  backend. The `/mcp` block needs `proxy_buffering off` + long read/send timeouts
  for the SSE stream.

### Prod — Helm + ArgoCD
`helm/irl-planner-pro` is a self-contained chart: a backend Deployment (no PVC), a
frontend Deployment + Service, a frontend-nginx ConfigMap, bundled Postgres, and an
Ingress that path-routes `/api`, `/healthz` (and, when MCP is enabled, `/mcp`,
`/oauth`, `/.well-known`) to the backend and everything else to the frontend.
Secrets (JWT, OIDC client secret, SMTP, DB password, and the MCP OAuth client
secret) are supplied via a Secret / sealed-secret; `helm/argocd/` holds the ArgoCD
Applications and a SealedSecret template. `DATABASE_URL` points at Postgres (with
PgBouncer in front in managed setups — hence the `QueryExecModeExec` pool config).

The frontend ConfigMap ships an SPA-only nginx config (it drops the Compose
`proxy_pass http://backend` blocks, which would crash nginx in-cluster). When
adding a backend path, update **both** the ingress `paths` in `values.yaml` **and**
`frontend/nginx.conf`.

### Observability
- `/metrics` (Prometheus, optionally `METRICS_TOKEN`-gated), `/healthz`, `/readyz`
  (the backend is ready as soon as the DB is migrated).
- Structured request logging via chi middleware; health probes are skipped from
  logs.

---

## 14. Security

- **Domain-restricted SSO** is the primary gate; no password auth, no open
  registration.
- **JWT** signed with a validated secret; `token_version` enables "sign out
  everywhere"; the client-side exp check avoids doomed requests.
- **Per-IP rate limits** (`httprate`) on auth and write endpoints.
- **Security headers** + strict CSP on both the backend (`securityHeaders`) and the
  nginx SPA layer (`nginx-security-headers.conf`); HSTS at the TLS edge.
- **Input validation**: slug regex, CSV size cap (`client_max_body_size 4m`),
  email/format checks, and server-side enforcement of all conditional form rules.
- **Least authority**: employees can only read/write *their own* submission (and
  not at all once the event is past); every admin route is behind
  `requireAdminMiddleware`. Admin edits of others' data and past events are
  permitted but always recorded in the activity log.
- **PII handling**: profiles carry dietary/health info (allergies) and submissions
  carry travel details — both are treated as sensitive; export is admin-only, no
  PII appears in URLs or logs, and OIDC error pages are generic.
- **MCP surface**: `/mcp` is gated by OAuth 2.1 (Authorization Code + PKCE),
  disabled unless both `MCP_OAUTH_CLIENT_*` are set, with allowlisted callback URIs
  and per-IP-rate-limited `/oauth/*`. MCP tools resolve the caller to a user and
  enforce the **same admin authorization** as the REST API — no tool exposes data a
  non-admin couldn't already see, and write tools require admin. Mutations made via
  MCP are written to the activity log like any other.

---

## 15. Testing

- **Backend** — table-driven Go tests per package, `*_test.go` beside sources, plus
  DB-backed integration tests against a real Postgres (skipped unless
  `IRL_TEST_DATABASE_URL` is set). Coverage includes: conditional submission
  validation, roster CSV parsing edge cases, attending-state bucketing +
  `no_response` computation, reminder idempotency (`reminder_log` conflict path) and
  **event-tz** window evaluation, daily-digest "send only when ≥1 activity",
  `after_deadline` stamping, past-event employee lock vs admin edit, timezone ↔ UTC
  round-trips (`timeutil`), OIDC domain rejection, and admin authorization.
- **Frontend** — Vitest + `@testing-library/vue` + **MSW** mocking `/api`. Coverage
  includes: form conditional rendering (Yes/No/Not-sure branches, per-leg
  independent travel, long-haul → extra night before), client validation
  messages, dashboard attending-state filter + auto-reload composable, activity-log
  rendering (own vs all, after-deadline badge), event-tz date formatting,
  current/Past split, and auth-guard redirects. `npm run check` (typecheck + lint +
  test) gates CI.
- **MCP** — integration tests for the OAuth PKCE happy path + rejection of bad
  redirect URIs, `/mcp` rejecting unauthenticated and non-admin callers, each read
  tool returning the same data as its REST sibling, and write tools writing an
  activity-log entry.
- **CI** — GitHub Actions runs the backend and frontend gates on every change.

---

## 16. MCP server

An **optional, additive** surface that lets an MCP client (e.g. Claude) query and
manage events conversationally — "who hasn't responded for Dubrovnik?", "create the
Lisbon March 2027 offsite". It is **off by default**: enabled only when
`MCP_OAUTH_CLIENT_ID` + `MCP_OAUTH_CLIENT_SECRET` are set, so it can never weaken a
deployment that doesn't opt in. The core app is fully functional without it.

### 16.1 Transport & auth
- **Server**: `modelcontextprotocol/go-sdk` `NewStreamableHTTPHandler`,
  **stateless** (no per-session map — every tool is a stateless DB read/write, which
  avoids "session not found" 404s after a redeploy).
- **Auth**: **OAuth 2.1 Authorization Code + PKCE** (`oauth.go`), with RFC 8414 /
  RFC 9728 discovery at `/.well-known/*`. The MCP client runs the OAuth dance,
  obtains a bearer token, and presents it on `/mcp`; `mcpTokenGateMiddleware`
  resolves it to a `*User` stashed in the request context. The access token carries
  a `typ=mcp_access` claim so it is accepted only at the `/mcp` gate and can't be
  replayed against the regular `/api` routes.
- **Authorization**: tool handlers enforce the **same role rules as the REST API**.
  Read tools require an authenticated admin; write tools require admin. Nothing is
  exposed via MCP that the same user couldn't reach through the SPA.
- **Rate limiting**: `/oauth/authorize` and `/oauth/token` are per-IP throttled;
  discovery is left open.

### 16.2 Tools
Each tool has typed in/out structs with `jsonschema` tags and is wrapped by an
`instrumentMCP` helper for Prometheus metrics. Mutating tools write to the
**activity log** exactly like the REST handlers (actor = the MCP user), so MCP
changes are as visible as any other.

**Read (admin):**
- `list_events` — current + past events with response counts.
- `get_event` — full config (dates, typed days, hotel, timezone, reminders).
- `get_dashboard` — attending-state counts + `no_response` list for an event.
- `list_non_responders` — roster members with no submission, by name (a focused
  shortcut over `get_dashboard`).
- `list_submissions` — submissions for an event (optionally filtered by attending
  state), mirroring the export filter.
- `list_attendees` — the full attendee roster for an event with each person's
  response state and whether they've ever signed in.
- `get_activity` — recent activity-log entries for an event (after-deadline
  flagged), with an optional `category` filter (`user` = participant actions,
  `admin` = administrative ones; empty = all).

**Write (admin):**
- `create_event` — create an event (+ generate typed days); validates slug + tz.
  Snapshots every existing user as a default attendee (§5.4).
- `update_event` — change config / reminder settings / day types.
- `upload_roster` — add attendees from inline `name,email` rows (additive,
  provisions new directory users), for onboarding new employees in bulk.
- `add_attendee` — add one person by email; provisions a directory user if the
  email is new (and seeds their default open-event memberships).
- `remove_attendee` — unlink one person from an event by email (record kept).
- `trigger_reminders` — force the reminder/digest evaluation for an event now
  (idempotent via `reminder_log`), for ad-hoc nudges.

Write tools deliberately stop short of editing individual attendees' personal
travel/dietary data over MCP (that stays in the admin UI), keeping the MCP write
surface to event administration rather than PII mutation.

### 16.3 Scope boundary
The MCP server reuses the existing query/command functions in `events.go`,
`dashboard.go`, `roster.go`, `activity.go`, and `reminders.go` — it is a thin
protocol adapter, not a second copy of the business logic.

---

## 17. Design decisions & rationale

The reasoning behind the choices baked into the sections above.

1. **No "Submitted" concept.** The dashboard is keyed by the `attending` state with
   all four buckets (`yes` / `no` / `not_sure` / `no_response`), filterable by any
   combination — not a binary submitted/not. Reminders still chase only
   `no_response` (true silence). → §10, §9.1.

2. **Edit after deadline is allowed but conspicuous, via an activity log.** Editing
   is never blocked for admins (and is allowed for employees until the event is
   past). Every action is recorded in `activity_log`; late changes are stamped
   `after_deadline`. Employees see only their own log; admins see the whole event
   timeline with after-deadline edits highlighted. Admins can enable a per-event
   **daily activity email**, sent only on days with ≥1 activity. → §5.8, §6, §9.3,
   §11.1.

3. **Extra nights are a stay date, not "Sunday".** Stored as `extra_stay_start`
   (event-local date). Employees can add at most one extra night before the first
   travel day (single-night toggle); admins can set wider ranges (2+ nights) for
   special cases. Late return is no longer offered, so `extra_stay_end` is always
   blanked (the column is retained only for historical rows). → §5.5, §8.

4. **Travel independence is per leg.** Travel to and from the offsite are separate
   decisions, so `arrival_independent` and `departure_independent` are tracked
   independently; the long-haul/accommodation block is dropped only when both legs
   are self-arranged. → §5.5, §8.

5. **Reminders may go to anyone on the roster.** This is a company-internal tool;
   there is no consent/opt-out flow. → §9.1.

6. **Events are never deleted; past events are tucked away.** "Past" is derived from
   `end_date` in the event timezone. The UI separates current/upcoming from Past;
   past events are read-only for employees and fully editable by admins (logged).
   Volume is small (~3/year now, up to 10–20/year if department offsites adopt it),
   so no pagination is needed — the current/Past split keeps the list tidy. → §5.2,
   §6, §11.

7. **Per-event timezone (default `Europe/Paris`).** All timestamps are stored UTC
   and rendered/entered in the event's IANA timezone; reminder windows and the "is
   past" / deadline logic are evaluated in that zone. → §5.2, §6.3.

8. **Admin membership is bootstrapped + in-app.** The first user to sign in becomes
   admin; admins then promote/demote others via `/admin/users` (last-admin demotion
   is blocked). There is no `ADMIN_EMAILS` env. → §5.1, §6.1, §7.

9. **One filter-driven export, not two buttons.** The single Export button downloads
   exactly what the dashboard filter currently shows (`export.csv?attending=…`); any
   future filter dimension extends the export for free. `no_response` rows carry
   empty submission columns, so the export doubles as a non-responder list when that
   filter is active. → §10.

10. **Name and allergies are profile properties.** They live on `users`, are seeded
    from the IdP only on first login, and are edited at `/profile`; submissions join
    them in rather than storing copies. A first-login confirm step lets the user fix
    the IdP-seeded values. → §5.1, §8.

11. **MCP is optional and admin-scoped.** An OAuth-gated `/mcp` server reuses the
    REST business logic and enforces the same authorization; it is off unless
    configured, so it can't weaken a deployment that doesn't use it. → §14, §16.
