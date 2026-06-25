# ID5 IRL Attendance App — Design & Implementation Document

Status: Draft for review
Author: generated from `plan.md`
Reference architecture: `/Users/ozimpasser/dev/plugin-skill-hosting`

---

## 1. Purpose & scope

A web app for collecting attendee information ahead of company offsites ("IRLs").
An **admin** (People team) configures an event once; **employees** sign in with
Google SSO and submit their attendance + travel details via a form with
conditional logic. The app tracks non-responders against an uploaded employee
roster, sends automated reminders, notifies admins on edits, and exports
responses.

This document fixes the **tech stack, architecture, data model, API surface, and
a phased implementation plan**, deliberately mirroring the conventions of the
`plugin-skill-hosting` project so the two codebases feel like siblings.

### Functional summary (from `plan.md`)

- **Admin setup** — event name, dates (per-day travel/event type), location
  (country + city), hotel (name + address), submission deadline, **timezone**
  (default `Europe/Paris`), reminder lead days (default 3).
- **Attendee form** — basic details + attending (Yes / No / Not sure, the last
  requiring a reason); conditional travel + long-haul (extra night before/after
  the offsite) + dietary sections on Yes; an instructions message on No.
- **Edit, activity log & notify** — attendees may edit after submitting; every
  action is recorded in an activity log (employees see their own; admins see all,
  with after-deadline edits highlighted); admins are emailed on any change and can
  enable a per-event daily activity digest.
- **Access** — Google SSO restricted to `@id5.io`; each event has a shareable URL.
- **Roster + dashboard** — admin uploads a CSV (name + work email) per event; the
  dashboard buckets everyone by attending state (yes / no / not sure / no
  response), filterable, and auto-reloads (5s / 15s / 1m / 5m / off).
- **Lifecycle** — events are never deleted; past events move to a separate area
  and stay admin-editable.
- **Export** — CSV export of responses.
- **Reminders** — automated emails to non-responders: 1/week plus configurable
  daily emails in the run-up to the deadline; admin-configurable timing, evaluated
  in the event timezone.

---

## 2. Tech stack (inherited from the reference)

| Layer | Choice | Notes |
|---|---|---|
| Backend language | **Go 1.26** | single module, `internal/` packages |
| HTTP router | **go-chi/chi v5** + middleware (`RequestID`, `RealIP`, `Recoverer`, `cors`, `httprate`) | same router composition as reference |
| Database | **PostgreSQL 16** via **pgx/v5** through the `database/sql` adapter | `QueryExecModeExec`, no statement cache (PgBouncer-safe) — copy `db.Open` verbatim |
| Migrations | Embedded `.sql` files run sequentially in `db.Migrate` | `//go:embed migrations/NNNN_*.sql`; no external migration tool |
| Auth | **OIDC** (`coreos/go-oidc/v3`) + **JWT** sessions (`golang-jwt/jwt/v5`) | Google Workspace `hd`-claim domain restriction → `id5.io` |
| Email | stdlib `net/smtp` wrapper (`internal/email`) | copy the reference `email.Sender` as-is |
| Metrics | **Prometheus** (`/metrics`), `/healthz`, `/readyz` | reuse `internal/metrics` |
| Background jobs | goroutines bound to a root `context`, tracked by a `sync.WaitGroup`; `time.Ticker` schedulers | reminder scheduler mirrors `StartSkillAudit` |
| MCP (Phase 7) | **`modelcontextprotocol/go-sdk`** Streamable HTTP server at `/mcp` + **OAuth 2.1** (PKCE) | admin-facing tools; copy `mcp.go` + `oauth.go` patterns |
| Frontend | **Vue 3** (`<script setup>`) + **vue-router** + **Pinia** | lazy-loaded views, `beforeEach` auth guard |
| Build/tooling | **Vite**, **TypeScript**, **vue-tsc**, **ESLint** | `npm run check` = typecheck + lint + test |
| Frontend tests | **Vitest** + `@testing-library/vue` + **MSW** | mirror reference test layout |
| Frontend HTTP | thin `fetch` wrapper in `src/api.ts` with `ApiError` + client-side JWT-exp check | copy the pattern |
| Packaging | multi-stage Dockerfiles; **nginx** serves the SPA and proxies `/api` | identical nginx routing approach |
| Orchestration | Docker **Compose** (db + backend + frontend) for dev; **Helm** + **ArgoCD** for prod | reuse chart structure |

**Deliberately dropped** from the reference (not needed here): git smart-HTTP
hosting, the Anthropic skill validator/audit, external-git mirroring, the
plugin/skill domain. We keep the *patterns* those used (ticker-driven schedulers,
append-only audit/versioning, OIDC domain gating) where they apply.

**Kept and adapted:** the **MCP server + OAuth 2.1** surface — built in **Phase 7**
(§18) as an additive, admin-facing way to query and manage events from an MCP
client (e.g. Claude). The core app (Phases 0–6) ships fully without it.

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
                    │  /api/events/:id/roster (CSV up) /export (CSV)  │
                    │  /api/events/:id/dashboard      stats           │
                    │  /mcp  (Phase 7) MCP Streamable HTTP, admin     │
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
                    │  users, events, event_days, event_roster,      │
                    │  submissions, submission_revisions,            │
                    │  reminder_log, activity_log                    │
                    └───────────────────────────────────────────────┘
                                              │ SMTP
                                              ▼
                                    Outbound email (reminders, admin notify)
```

The backend is **stateless** apart from Postgres and outbound SMTP — no local
data directory is required (unlike the reference, which kept git repos on disk),
which simplifies deployment (no PVC needed for the backend).

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
│       ├── config/                env loading + validation (copy & trim reference)
│       ├── db/                    Open + Migrate + embedded migrations/
│       │   └── migrations/0001_init.sql …
│       ├── email/                 SMTP sender (copy from reference)
│       ├── metrics/               Prometheus middleware (copy from reference)
│       ├── workspaceauth/         Google hd-claim validation (copy from reference)
│       └── server/
│           ├── app.go             App struct (Cfg, DB, OIDC, Email)
│           ├── router.go          chi route wiring
│           ├── auth.go            JWT mint/verify, authMiddleware, requireAdmin
│           ├── oidc.go            OIDC login/callback/logout (copy & trim)
│           ├── users.go           /api/me, provisioning (first-user-admin), promote/demote
│           ├── events.go          event CRUD + per-day config
│           ├── roster.go          CSV upload + parse
│           ├── submissions.go     attendee form CRUD + conditional validation
│           ├── dashboard.go       counts by attending state + roster join
│           ├── export.go          CSV export
│           ├── activity.go        activity_log writes + read endpoints
│           ├── reminders.go       scheduler + reminders + daily digest + reminder_log
│           ├── notify.go          admin "submission changed" emails
│           ├── timeutil.go        event-tz <-> UTC helpers, "is past" / "today"
│           ├── mcp.go             (Phase 7) MCP server + tool registrations
│           ├── oauth.go           (Phase 7) OAuth 2.1 authorize/token + discovery
│           └── errors.go          JSON/HTML error responses (copy pattern)
├── frontend/
│   ├── Dockerfile, nginx.conf, nginx-security-headers.conf
│   ├── package.json, vite.config.ts, tsconfig*.json
│   └── src/
│       ├── main.ts, App.vue, router.ts, api.ts, types.ts, styles.css
│       ├── stores/   auth.ts, events.ts
│       ├── views/
│       │   ├── LoginView.vue          Google sign-in
│       │   ├── OIDCCallbackView.vue   token handoff (copy pattern)
│       │   ├── EventListView.vue      admin: events index (current vs Past tabs)
│       │   ├── EventEditView.vue      admin: configure event + days + tz + reminders
│       │   ├── EventDashboardView.vue admin: Responses / Activity / Roster tabs
│       │   ├── AttendeeFormView.vue   employee: conditional form + My-activity (/events/:slug)
│       │   ├── UsersView.vue          admin: list users, promote/demote
│       │   └── ErrorView.vue          403/404/500
│       ├── components/  ActivityLog.vue, AttendingFilter.vue
│       ├── lib/         datetime.ts (event-tz formatting via Intl)
│       └── composables/ useAutoReload.ts (dashboard polling), useConfirm.ts
└── helm/
    └── irl-planner-pro/   Chart.yaml, values.yaml, templates/
```

Module name proposed: **`irlplanner`** (the reference used `marketplace`).

---

## 5. Data model

Postgres, UUID PKs (`gen_random_uuid()` via `pgcrypto`), `TIMESTAMPTZ` everywhere,
sequential embedded migrations. Schema below is `0001_init.sql` conceptually.

### 5.1 `users`
Provisioned on first OIDC login. Mirrors the reference user model minus
password auth.

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT UNIQUE NOT NULL,         -- @id5.io, lower-cased
    name        TEXT NOT NULL DEFAULT '',     -- from OIDC profile claim
    is_admin    BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    token_version INTEGER NOT NULL DEFAULT 0  -- bump to revoke all sessions
);
```

**Admin bootstrap:** the **first user to sign in** is made admin automatically
(`is_admin = true` when the `users` table is otherwise empty — done atomically in
the provisioning transaction). From then on, admins **promote/demote** other
users in-app (ported from the reference's promote/demote pattern). No
self-service registration; the `@id5.io` domain restriction is the gate. A
guard prevents the last remaining admin from demoting themselves, so an event is
never left without an admin.

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
    created_by         UUID NOT NULL REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

The `slug` is the shareable URL component (e.g. `dubrovnik-oct-2026`), validated
against the same slug regex the reference uses (`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`).

**No deletion.** Events are never deleted (they hold historical attendance data).
Instead an event is **past** when `end_date < today` (computed in the event's
timezone). The UI surfaces *current/upcoming* events prominently and tucks past
events into a separate "Past events" area. Past events are **read-only for
employees** (the form locks) but **fully editable by admins** — every such admin
edit is captured in the activity log (§5.8, §11). `timezone` is an IANA name
(default `Europe/Paris`); all timestamps are stored UTC and rendered in this zone
(§6.3).

### 5.3 `event_days`
One row per calendar day in `[start_date, end_date]`, each typed. Generated on
event create with **first and last day = `travel`, the rest = `event`**; admin
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

### 5.4 `event_roster`
The uploaded CSV (name + work email), per event. **Used for non-responder
tracking only** — never for sending individual invites.

```sql
CREATE TABLE event_roster (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id  UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    full_name TEXT NOT NULL,
    email     TEXT NOT NULL,                 -- lower-cased
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, email)
);
```

Re-uploading a CSV replaces the roster for that event (delete + insert in one
transaction) so corrections are easy.

### 5.5 `submissions`
One submission per (event, user). Holds all form fields including the
conditional travel block.

```sql
CREATE TABLE submissions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id           UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id            UUID NOT NULL REFERENCES users(id),
    first_name         TEXT NOT NULL,
    last_name          TEXT NOT NULL,
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
    long_haul          BOOLEAN NOT NULL DEFAULT false,  -- intl flight 7h+
    -- Extra hotel nights modelled as an extended stay window (event-local dates).
    -- extra_stay_start: first night needing accommodation when EARLIER than the
    --   event's first travel day (start_date). NULL = no extra night before.
    -- extra_stay_end:   last night needing accommodation when LATER than the
    --   event's last travel day (end_date). NULL = no extra night after.
    -- Self-service cap: a non-admin may shift each bound by at most ONE day
    -- (extra_stay_start >= start_date - 1, extra_stay_end <= end_date + 1).
    -- Admins may set any earlier start / later end (2+ extra nights) for special
    -- cases. Mainly surfaced for long-haul travellers, but an admin may set these
    -- on any submission.
    extra_stay_start   DATE,
    extra_stay_end     DATE,

    allergies          TEXT NOT NULL DEFAULT '',
    comments           TEXT NOT NULL DEFAULT '',

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, user_id)
);
```

### 5.6 `submission_revisions`
Append-only history so admins can see what changed, and the source of the
"submission changed" admin notification. Cheap, and matches the reference's
versioning instinct.

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
    recipient     TEXT NOT NULL,                       -- roster email
    reminder_kind TEXT NOT NULL CHECK (reminder_kind IN ('weekly','deadline','daily_digest')),
    period_key    TEXT NOT NULL,                       -- e.g. '2026-W40' or '2026-10-12'
    sent_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, recipient, reminder_kind, period_key)
);
```

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
    summary     TEXT NOT NULL DEFAULT '',     -- pre-rendered, human-readable line
    detail      JSONB,                        -- optional structured diff / context
    after_deadline BOOLEAN NOT NULL DEFAULT false, -- true if created past the event deadline
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX activity_log_event_idx   ON activity_log(event_id, created_at DESC);
CREATE INDEX activity_log_subject_idx ON activity_log(event_id, subject_email);
```

**Action vocabulary** (extensible): `submission.created`, `submission.updated`,
`submission.attending_changed`, `event.created`, `event.updated`,
`event.config_changed`, `roster.uploaded`, `admin.edited_submission`,
`reminder.sent`. The `summary` is computed at write time so both the UI and the
digest email render without re-deriving anything. `after_deadline` is stamped by
comparing `now()` to the event's `submission_deadline` — it's the single flag the
admin UI uses to highlight late changes.

The earlier `submission_revisions` table (§5.6) remains the *full-snapshot* store
(for precise field-level diffs); `activity_log` is the *timeline* layered on top.

---

## 6. Authentication & access control

### 6.1 Sign-in
- **`AUTH_MODE=oidc` only** (the reference's dev-only password mode is dropped).
- Google as the OIDC provider; `OIDC_GOOGLE_WORKSPACE_DOMAINS=id5.io` enforces the
  `hd` claim via the copied `workspaceauth.ValidateGoogleHD`. Anyone outside
  `@id5.io` is rejected at callback with a generic "domain not allowed" page.
- On successful callback the user is **upserted** into `users` (no approval queue
  — domain restriction is sufficient). The **first** user ever provisioned is made
  admin (§5.1); everyone after is a regular employee until an admin promotes them.
- A signed **JWT** (30-day expiry, `token_version` embedded) is handed to the SPA
  via the same `/auth/callback#token=…` fragment flow the reference uses, stored
  in `localStorage`, and sent as `Authorization: Bearer`.

### 6.2 Authorization
Two roles:

| Capability | Employee | Admin (People team) |
|---|---|---|
| Sign in (`@id5.io`) | ✓ | ✓ |
| View an event page by URL, submit/edit own response (current events) | ✓ | ✓ |
| View own activity log | ✓ | ✓ |
| Create/configure events (incl. past events) | — | ✓ |
| Edit any attendee's submission (logged) | — | ✓ |
| Upload roster CSV | — | ✓ |
| View dashboard + full activity log | — | ✓ |
| Export CSV | — | ✓ |
| Configure reminders / daily digest | — | ✓ |
| Promote/demote admins | — | ✓ |

Enforced by chi middleware mirroring the reference: `authMiddleware`
(verifies JWT, loads user, checks `token_version`) then `requireAdminMiddleware`
on the admin route group. The frontend router `beforeEach` guard mirrors this for
UX (redirect to `/login`, 403 page for non-admins) but the **backend is the
source of truth**.

**Past-event edits.** Employees may create/edit their own submission only while
the event is current/upcoming (`end_date >= today`). Once an event is past, the
employee form is read-only — but **admins can still edit any submission and any
event config**, at any time. Each admin edit (and any edit landing after the
submission deadline) is recorded in the activity log with `after_deadline=true`
so it is conspicuous in the admin timeline.

### 6.3 Time zones & date handling

Each event carries an IANA **`timezone`** (default `Europe/Paris`). The rule:

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
  (`reminder_hour` is event-local), and deciding "today"/"is the event past".
- **`timezone` is validated** at write time against the tz database
  (`time.LoadLocation` must succeed) — an invalid zone is a 400.

---

## 7. Backend API surface

All under `/api`, JSON, `httprate` per-IP throttles on auth + mutation-heavy
endpoints (copy the reference's limits).

### Public / auth
```
GET  /api/version                      build info
GET  /api/auth/config                  { mode, ... } for the SPA
GET  /api/auth/oidc/login              → redirect to Google
GET  /api/auth/oidc/callback           ← Google, mints JWT, redirects to SPA
GET  /api/auth/oidc/logout             RP-initiated logout
GET  /api/me                           current user { id, email, name, isAdmin }
```

### Attendee-facing (any signed-in @id5.io user)
```
GET  /api/events/:slug                 event details + typed days + timezone (form header)
GET  /api/events/:slug/submission      caller's own submission (404 if none)
PUT  /api/events/:slug/submission      create/update own submission (upsert; 403 if event is past)
GET  /api/events/:slug/activity        caller's OWN activity entries for this event
```

### Admin (requireAdmin)
```
GET    /api/users                      list users (email, name, isAdmin)
POST   /api/users/:id/promote          grant admin
POST   /api/users/:id/demote           revoke admin (blocked for the last admin)

GET    /api/events?scope=current|past  list events, split current vs past (default current)
POST   /api/events                     create event (+ generate event_days)
GET    /api/events/:id                 full event config
PUT    /api/events/:id                 update event + day types + reminder config (admins: even when past)

POST   /api/events/:id/roster          upload CSV (multipart), replaces roster
GET    /api/events/:id/roster          list roster

GET    /api/events/:id/dashboard       counts keyed by attending state + non-responders (see §10)
GET    /api/events/:id/submissions     all submissions (table view; admins may edit any)
PUT    /api/events/:id/submissions/:userId  admin edit of an attendee's submission
GET    /api/events/:id/activity        ALL activity for the event (timeline; flags after_deadline)
GET    /api/events/:id/export.csv      CSV download of all submissions
```

There is no delete endpoint — events persist and become read-only-to-employees
once past (§5.2, §6).

`PUT /submission` runs the **conditional validation** (section 8), writes the
`submissions` row, appends a `submission_revisions` snapshot, writes an
`activity_log` entry (stamping `after_deadline`), and — if this is an edit of an
existing submission — enqueues an **admin notification** email (section 9.2).
Admin edits via `PUT …/submissions/:userId` follow the same path but log the
`admin.edited_submission` action with the admin as `actor` and the attendee as
`subject`.

### MCP & OAuth (Phase 7)
```
/mcp                                    MCP Streamable HTTP (admin tools; see §18)
/oauth/authorize  /oauth/token          OAuth 2.1 Authorization Code + PKCE
/.well-known/oauth-authorization-server RFC 8414 discovery
/.well-known/oauth-protected-resource   RFC 9728 protected-resource metadata
```
These exist only once Phase 7 lands and are gated by `mcpTokenGateMiddleware`
(OAuth bearer) rather than the SPA's JWT. Detailed in §18.

---

## 8. Attendee form & conditional logic

The form is a single Vue view (`AttendeeFormView.vue`) driven by reactive state;
the **same rules are enforced server-side** in `submissions.go` (never trust the
client).

### Step 1 — Basic details (always)
- First name, last name (required).
- **Attending?** `yes` / `no` / `not_sure`.
  - `not_sure` → `not_sure_reason` **required** (server rejects empty). Rationale
    per `plan.md`: an employee who can't commit to yes/no before the deadline must
    say why.

### Branch: `attending = no`
- No further fields. The UI shows the fixed instructions message:
  > If for any reason you cannot attend this offsite, please follow the steps below:
  > 1. Let your manager know
  > 2. Inform the People team by emailing people@id5.io

  (Stored as a constant; no DB field needed.)

### Branch: `attending = yes` → travel + other
- **Arrival**: day (constrained to the event date range), time, mode
  (`flight`/`car`/`train`/`other`), details (flight number, or free text for
  other modes — required when mode is set).
- **Departure**: same shape.
- **Long-haul traveller?** (international flight 7h+) → `long_haul` yes/no.
  - If `long_haul = yes` → **"Would you require an extra night?"** with two
    independent toggles: **an extra night before the offsite** and/or **an extra
    night after the offsite**, each labelled with the concrete date (e.g. "Sun 11
    Oct — night before" / "Sat 17 Oct — night after"). For an employee these are
    single-night toggles that set `extra_stay_start = start_date − 1` and/or
    `extra_stay_end = end_date + 1`. Only shown when `long_haul = true`.
  - **Admin override:** in the admin submission editor the same field is a *date
    picker* with no one-day cap, so the People team can grant 2+ extra nights at
    either end for special cases (visa stopovers, connecting flights, etc.). The
    server enforces the one-day cap only for non-admin writers.
- **Allergies / dietary preferences** (free text).
- **Comments** (free text).

### Validation matrix (server-enforced)

| Field | Required when |
|---|---|
| `first_name`, `last_name` | always |
| `not_sure_reason` | `attending = 'not_sure'` |
| `arrival_*`, `departure_*` | `attending = 'yes'` (day + mode required; details required if mode set) |
| `extra_stay_start` / `extra_stay_end` | optional; one-day cap from event bounds for employees, unrestricted for admins |
| dietary / comments | optional |

Fields outside the chosen branch are blanked on write so a user toggling Yes→No
doesn't leave stale travel data.

### Editing
The same `PUT` endpoint handles create and edit (upsert on `(event_id,user_id)`).
The form pre-loads the existing submission via `GET …/submission`. For employees,
editing is allowed **before and after the deadline** (the deadline gates
*reminders* and the meaning of *"not sure"*, not the ability to edit) **as long as
the event is not yet past** — once `end_date < today` the employee form locks.
Admins can always edit, including past events. Every save appends a
`submission_revisions` snapshot **and** an `activity_log` entry; edits that land
after the submission deadline are stamped `after_deadline=true` so they stand out
in the admin timeline (§11). Admin notification email fires on edits (§9.2).

---

## 9. Notifications & reminders

Outbound email uses the copied `internal/email.Sender` (stdlib SMTP). All email
is **best-effort**: a send failure logs a WARN and never fails the user's request
(same posture as the reference's audit alerts).

### 9.1 Reminder scheduler
A single background goroutine started in `main.go` (pattern copied from
`StartSkillAudit`): bound to the root context, tracked by the `WaitGroup`, driven
by a `time.Ticker` (hourly). On each tick, for every event that is still open
(deadline not yet passed, event not past):

1. Compute the **non-responders**: roster emails with **no `submissions` row**
   for the event. (Any submission — including `not_sure` — counts as a response;
   only true silence is chased. Reminders are about *getting a response*, while
   the dashboard then filters responses by `attending` state, §10.)
2. Decide which reminder windows are open *now*, evaluated in the **event
   timezone** at the event-local `reminder_hour`:
   - **Weekly** (`weekly_reminders = true`): one per ISO week → `period_key` =
     `2026-W40`.
   - **Deadline run-up** (`reminder_days_before`): one per day for the N days
     immediately before `submission_deadline` → `period_key` = the date.

3. For each non-responder × open window, attempt an insert into `reminder_log`
   (`ON CONFLICT DO NOTHING`). **Only if the insert created a row** do we send the
   email — this makes sends idempotent and restart-safe.

The email links the recipient to the event URL (`PUBLIC_BASE_URL/events/:slug`).

Because the recipient pool is the **roster**, reminders may reach people who have
never signed in. This is a **company-internal tool**, so sending to any `@id5.io`
roster address is acceptable without separate consent — no opt-out flow is needed
in v1.

Admin can configure timing per event (`reminder_days_before`, `weekly_reminders`,
`reminder_hour`, `daily_activity_email`) via the event edit form, satisfying
"Admin should be able to configure the timing."

### 9.2 Admin "submission changed" notification
When `PUT …/submission` updates an **existing** submission (not the first create),
`notify.go` sends a summary email to `PEOPLE_TEAM_EMAIL` (and/or the current set
of admin users). The
email names the employee, the event, and what changed (diff derived from the
latest two `submission_revisions` snapshots). Sent asynchronously so it never
blocks the response.

### 9.3 Daily activity digest (admin, per event)
When an event has `daily_activity_email = true`, the same scheduler that drives
reminders also assembles a once-per-day digest of that event's `activity_log`
entries from the last 24h, in the event timezone at `reminder_hour`. **The email
is sent only if there is at least one activity in the window** — a quiet day
produces no mail. The digest groups entries (new submissions, edits, attending
changes, roster uploads) and visibly flags any `after_deadline` edits at the top,
giving admins a low-effort way to notice late changes without watching the
dashboard. Idempotency reuses `reminder_log` with `reminder_kind='daily_digest'`
(or a sibling table) keyed by event + date so a restart never double-sends.

---

## 10. Dashboard, non-responder tracking & export

### Dashboard (`EventDashboardView.vue`, admin)
The dashboard is organised around the **`attending` state**, not a binary
"submitted / not". There are four mutually exclusive buckets every roster member
falls into — `yes`, `no`, `not_sure`, and `no_response` (no submission row) — and
the UI lets the admin **filter the attendee table by any combination** of these
four states.

`GET /api/events/:id/dashboard` returns:
```json
{
  "rosterTotal": 48,
  "counts": { "yes": 33, "no": 5, "notSure": 3, "noResponse": 7 },
  "rosterEntries": [
    { "fullName": "…", "email": "…", "attending": "yes", "afterDeadlineEdit": false },
    { "fullName": "…", "email": "…", "attending": "no_response" }
  ],
  "offRoster": [
    { "name": "…", "email": "…", "attending": "yes" }
  ]
}
```
- Each roster member is joined to their submission and assigned one of the four
  states; `no_response` replaces the old "non-responder" label and is itself just
  one filterable state. The "who hasn't responded, by name" requirement is the
  `no_response` filter.
- `offRoster` lists people who submitted but aren't on the roster (e.g. a late
  add to the company), surfaced separately so the roster stays the source of
  truth for tracking.
- Filtering is client-side over `rosterEntries` (small lists; whole set fetched
  each poll), with quick chips for each state and their counts.

**Auto-reload**: a `useAutoReload(intervalRef, fetchFn)` composable polls the
dashboard endpoint. A dropdown offers **5s / 15s / 1m / 5m / off**, default
**1m**. The composable cleans up its timer on unmount and pauses when the tab is
hidden (`visibilitychange`) to avoid wasted polls.

### Export (`GET /api/events/:id/export.csv`)
**One export button that follows the filter.** Rather than a fixed "all" export
plus a separate "non-responders" export, the single Export button downloads
*exactly what the dashboard filter currently shows*. The endpoint takes the same
filter the table uses:

```
GET /api/events/:id/export.csv?attending=yes,not_sure       # only those states
GET /api/events/:id/export.csv?attending=no_response        # the non-responders
GET /api/events/:id/export.csv                              # no filter → everyone
```

`attending` is a comma-separated subset of `{yes,no,not_sure,no_response}`
(mirrors the dashboard chips). Rows for `no_response` roster members are emitted
with empty submission columns so the CSV doubles as a non-responder list when
that's the active filter. One row per person, all form fields + email +
timestamps (rendered in the event timezone), streamed with the stdlib
`encoding/csv` and `Content-Disposition: attachment`. Because the filter is the
single source of truth for "which people", **any future filter dimension** (e.g.
long-haul only, by arrival day) extends both the table and the export for free —
the export just passes the active filter through.

### Roster CSV upload
`POST /api/events/:id/roster` accepts `multipart/form-data` (same upload shape as
the reference's skill-zip import). Parsed with `encoding/csv`; expected headers
`name,email` (case-insensitive, tolerant of extra columns). Validated: non-empty
name, well-formed email; rows are lower-cased and de-duplicated; the whole roster
for the event is replaced transactionally. A parse report (`{ inserted, skipped,
errors[] }`) is returned for admin feedback.

---

## 11. Frontend design

- **Vue 3 `<script setup>` + Pinia + vue-router**, lazy-loaded views, exactly the
  reference's `main.ts` / `router.ts` shape.
- **`stores/auth.ts`** — copied/trimmed from the reference: token + user in
  `localStorage`, `ensureFreshUser()` re-validates `/api/me` once per load,
  `loginViaOIDC()` redirects to `/api/auth/oidc/login`, JWT-exp short-circuit.
- **`stores/events.ts`** — admin event list/detail caching + mutations.
- **`api.ts`** — the thin `fetch` wrapper with `ApiError`, `isJwtExpired`,
  multipart helper for roster upload (copy the reference's `importSkill` pattern).
- **Router guard** — `requiresAuth` + admin-only meta on event-management routes,
  redirect to `/login` (with `redirect` query) or `/error?code=403`.

### Routes
```
/login                         Google sign-in
/auth/callback                 OIDC token handoff
/events/:slug                  AttendeeFormView   (any signed-in @id5.io user)   ← the shareable URL
                                 — includes a "My activity" panel (own log only)
/admin/events                  EventListView      (admin; current vs Past tabs)
/admin/events/new              EventEditView      (admin)
/admin/events/:id/edit         EventEditView      (admin)
/admin/events/:id              EventDashboardView (admin; Responses / Activity / Roster tabs)
/admin/users                   UsersView          (admin; promote/demote)
/error                         ErrorView (403/404/500)
```

The `EventListView` separates **current/upcoming** from **Past** events (a tab or
collapsed section), keeping the past ones out of the way but reachable; admins can
open a past event and still edit it. A shared **`ActivityLog.vue`** component
renders the timeline in both the employee panel (scoped to their own entries) and
the admin Activity tab (all entries), described next.

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
  subject, action, time in event tz). Any entry with `after_deadline = true` —
  and any `admin.edited_submission` — is visually highlighted (badge/colour) so a
  change made after the deadline, or an admin editing on someone's behalf, is
  immediately obvious. Filterable by attendee and by "after-deadline only".

This is the mechanism that makes post-deadline editing *allowed but conspicuous*
(§6): nothing is blocked, but every late or admin-made change is on the record and
easy to find. The daily activity digest (§9.3) is the push-notification companion
to this pull view.

---

## 12. Configuration (env vars)

Mirrors `config.Load()` conventions (getenv with defaults, fail-fast validation).
A trimmed `.env.example`:

```dotenv
# Core
PUBLIC_BASE_URL=http://localhost:8080
LISTEN_ADDR=:8080
DATABASE_URL=postgres://irl:irl@db:5432/irl?sslmode=disable

# Session signing (>=32 chars; openssl rand -hex 32). Insecure default refused
# at boot unless ALLOW_INSECURE_JWT_SECRET=true (local dev only).
JWT_SECRET=change-me-please-use-32-chars-minimum
# ALLOW_INSECURE_JWT_SECRET=true

# Auth — OIDC only. Google Workspace, restricted to id5.io.
AUTH_MODE=oidc
OIDC_ISSUER_URL=https://accounts.google.com
OIDC_CLIENT_ID=...
OIDC_CLIENT_SECRET=...
OIDC_REDIRECT_URL=                       # defaults to PUBLIC_BASE_URL + /api/auth/oidc/callback
OIDC_GOOGLE_WORKSPACE_DOMAINS=id5.io

# People team. The FIRST user to sign in becomes admin automatically; admins
# then promote/demote others in-app — no admin allowlist env needed.
PEOPLE_TEAM_EMAIL=people@id5.io          # recipient for "submission changed" + digest notices

# SMTP (reminders + admin notifications). Empty SMTP_HOST disables email.
SMTP_HOST=
SMTP_PORT=587
SMTP_USERNAME=
SMTP_PASSWORD=
SMTP_FROM=irl-noreply@id5.io
SMTP_USE_TLS=true

# Reminder + digest scheduler
REMINDERS_ENABLED=true
REMINDER_TICK_INTERVAL=1h
# Default IANA timezone pre-filled when an admin creates a new event.
DEFAULT_EVENT_TIMEZONE=Europe/Paris

# CORS (defaults derived from PUBLIC_BASE_URL)
# CORS_ALLOWED_ORIGINS=
# METRICS_TOKEN=

# MCP server (Phase 7). Both set → /mcp + OAuth 2.1 enabled; both empty → /mcp off.
# MCP_OAUTH_CLIENT_ID=
# MCP_OAUTH_CLIENT_SECRET=
# Allowlisted OAuth callback URIs (comma-separated). Defaults to Claude's connector.
# MCP_OAUTH_REDIRECT_URIS=https://claude.ai/api/mcp/auth_callback
```

Boot-time validation copied from the reference: refuse the insecure/short
`JWT_SECRET`, require OIDC vars when `AUTH_MODE=oidc`, warn when the Workspace
domain allowlist is empty.

---

## 13. Deployment

### Dev — Docker Compose
`compose.yml` with three services (copy the reference shape, drop the data
volume on the backend since there's no on-disk state):
- `db` — `postgres:16-alpine`, healthcheck, volume.
- `backend` — built from `./backend`, env from `.env`, depends on healthy db.
- `frontend` — built from `./frontend`, nginx on `:8080`, proxies `/api`,
  `/healthz` to backend (Phase 7 adds `/mcp`, `/oauth`, `/.well-known` proxy
  blocks — `/mcp` needs `proxy_buffering off` + long read/send timeouts for the
  SSE stream, copied from the reference nginx).

### Prod — Helm + ArgoCD
Reuse the chart structure: a backend Deployment (no PVC needed), a frontend
Deployment + Service, a frontend-nginx ConfigMap, and an Ingress that path-routes
`/api`, `/healthz` (and, in Phase 7, `/mcp`, `/oauth`, `/.well-known`) to the
backend and everything else to the frontend. Secrets (JWT, OIDC client secret,
SMTP, DB URL, and the Phase 7 MCP OAuth client secret) via sealed-secrets as in
the reference.
`DATABASE_URL` points at managed Postgres (PgBouncer in front — hence the
`QueryExecModeExec` pool config carried over verbatim).

### Observability
- `/metrics` (Prometheus, optionally `METRICS_TOKEN`-gated), `/healthz`,
  `/readyz` (the backend is ready immediately — no rematerialization step).
- Structured request logging via chi middleware, health probes skipped from logs.

---

## 14. Security

- **Domain-restricted SSO** is the primary gate; no password auth, no open
  registration.
- **JWT** signed with a validated secret; `token_version` enables "sign out
  everywhere"; client-side exp check avoids doomed requests.
- **Per-IP rate limits** (`httprate`) on auth and write endpoints.
- **Security headers** + strict CSP on both the backend (copy `securityHeaders`)
  and the nginx SPA layer (copy `nginx-security-headers.conf`); HSTS at the TLS
  edge.
- **Input validation**: slug regex, CSV size cap (`client_max_body_size 4m`),
  email/format checks, server-side enforcement of all conditional form rules.
- **Least authority**: employees can only read/write *their own* submission (and
  not at all once the event is past); every admin route behind
  `requireAdminMiddleware`. Admin edits of others' data and past events are
  permitted but always recorded in the activity log.
- **PII handling**: submissions contain dietary/health info (allergies) and
  travel details — treat as sensitive; restrict export to admins, no PII in URLs
  or logs, generic OIDC error pages.
- **MCP surface (Phase 7)**: `/mcp` is gated by OAuth 2.1 (Authorization Code +
  PKCE), disabled unless both `MCP_OAUTH_CLIENT_*` are set, callback URIs
  allowlisted, and `/oauth/*` per-IP rate-limited (copy reference limits). MCP
  tools resolve the caller to a user and enforce the **same admin authorization**
  as the REST API — no tool exposes data a non-admin couldn't already see, and
  write tools require admin. Mutations made via MCP are written to the activity
  log like any other.

---

## 15. Testing strategy (mirrors the reference)

- **Backend** — table-driven Go tests per package; `*_test.go` beside sources;
  integration tests against a real Postgres (the reference runs an
  `integration_test.go`). Cover: conditional submission validation, roster CSV
  parsing edge cases, attending-state bucketing + `no_response` computation,
  reminder idempotency (`reminder_log` conflict path) and **event-tz** window
  evaluation, daily-digest "send only when ≥1 activity", `after_deadline`
  stamping, past-event employee lock vs admin edit, timezone ↔ UTC round-trips
  (`timeutil`), OIDC domain rejection, admin authorization.
- **Frontend** — Vitest + `@testing-library/vue` + **MSW** mocking `/api`. Cover:
  form conditional rendering (Yes/No/Not-sure branches, long-haul → extra night
  before/after), client validation messages, dashboard attending-state filter +
  auto-reload composable, activity-log rendering (own vs all, after-deadline
  badge), event-tz date formatting, current/Past split, auth guard redirects.
  `npm run check` (typecheck + lint + test) gates CI.
- **MCP (Phase 7)** — integration tests mirroring the reference's `mcp_test.go` /
  `oauth_test.go`: OAuth PKCE happy path + rejection of bad redirect URIs,
  `/mcp` rejecting unauthenticated and non-admin callers, each read tool returning
  the same data as its REST sibling, and write tools writing an activity-log entry.
- **CI** — GitHub Actions + `pre-commit` config as in the reference.

---

## 16. Implementation plan (phased)

Each phase is independently shippable and ends green (`go test ./...` +
`npm run check`).

**Phase 0 — Scaffolding (skeleton boots).**
Backend module + `cmd/server/main.go` (config → db → migrate → http), copy
`config`, `db`, `email`, `metrics`, `workspaceauth`, `errors` packages from the
reference and trim. Frontend Vite/Vue/Pinia/router skeleton, `api.ts`, `App.vue`,
`auth` store stubs. Compose file. `0001_init.sql` with `users` + `events` +
`event_days`.

**Phase 1 — Auth + user roles.**
OIDC login/callback/logout (copy & trim `oidc.go` + `workspaceauth`), JWT
mint/verify, `authMiddleware` / `requireAdminMiddleware`, `/api/me`, user upsert
with **first-user-admin** bootstrap, and `/api/users` + promote/demote (last-admin
guard). Frontend `LoginView`, `OIDCCallbackView`, `UsersView`, router guard.
*Done when:* the first `@id5.io` user signs in as admin and can promote/demote a
second user; non-`id5.io` is rejected.

**Phase 2 — Event configuration (admin) + timezone.**
`events` + `event_days` CRUD, day-type generation (first/last = travel), slug
validation, per-event `timezone` (validated via `time.LoadLocation`), reminder
config fields, and the `timeutil.go` event-tz ↔ UTC helpers. `EventListView`
(current vs **Past** tabs), `EventEditView` with a timezone picker and all
dates shown/entered in the event zone.
*Done when:* an admin creates and edits an event (typed days, deadline, timezone);
past events appear under their own tab and stay admin-editable.

**Phase 3 — Attendee form + activity log.**
`submissions` + `submission_revisions` + `activity_log` tables; `GET`/`PUT`
submission with full server-side conditional validation (incl. extra-night
before/after); `after_deadline` stamping; employee form **locks on past events**;
`AttendeeFormView` with all branches + "My activity" panel; `ActivityLog.vue`.
*Done when:* an employee opens `/events/:slug`, fills the conditional form, saves,
re-opens and edits, and sees their own activity timeline.

**Phase 4 — Roster + dashboard + export.**
`event_roster` table, CSV upload/parse, dashboard keyed by **attending state**
(yes/no/not_sure/no_response) with filter chips, admin **Activity** tab (all
entries, after-deadline highlight), admin submission edit, `useAutoReload` with
the interval dropdown, CSV export.
*Done when:* admin uploads a roster, filters attendees by attending state with
counts updating on the chosen interval, reviews the activity timeline, edits a
submission, and exports a CSV.

**Phase 5 — Notifications, reminders & digest.**
Admin "submission changed" email on edit; `reminder_log` table + scheduler
goroutine (weekly + deadline run-up, **event-tz-aware**, idempotent); per-event
reminder config; the **daily activity digest** (sent only when ≥1 activity).
*Done when:* editing a submission emails admins, non-responders receive weekly +
pre-deadline reminders exactly once per window in the event's local time, and an
active-digest event emails admins a once-daily summary only on days with activity.

**Phase 6 — Hardening & deploy.**
Security headers/CSP, rate limits, metrics, Helm chart + ArgoCD manifests,
README, CI. Production OIDC credentials + SMTP wired.

**Phase 7 — MCP server (additive).**
`mcp.go` (Streamable HTTP, stateless) + `oauth.go` (OAuth 2.1 Authorization Code +
PKCE, discovery) copied/adapted from the reference; `mcpTokenGateMiddleware`;
admin-scoped tools (§18); nginx/Ingress `/mcp` + `/oauth` + `/.well-known` blocks;
`MCP_OAUTH_*` config. Off by default — enabled only when the OAuth client vars are
set, so it cannot weaken a deployment that doesn't use it.
*Done when:* an admin connects an MCP client via OAuth and can list events, read a
dashboard/non-responders, and (write tools) create/configure an event — every
mutation showing up in the activity log.

---

## 17. Resolved decisions

These were open questions in the first draft; the answers are now baked into the
sections above and recorded here for traceability.

1. **No "Submitted" concept.** The dashboard is keyed by the `attending` state
   with all four buckets (`yes` / `no` / `not_sure` / `no_response`), filterable
   by any combination — not a binary submitted/not. Reminders still chase only
   `no_response` (true silence). → §10, §9.1.

2. **Edit after deadline is allowed but conspicuous, via an activity log.**
   Editing is never blocked for admins (and is allowed for employees until the
   event is past). Every action is recorded in `activity_log`; late changes are
   stamped `after_deadline`. Employees see only their own log; admins see the
   whole event timeline with after-deadline edits highlighted. Admins can enable a
   per-event **daily activity email**, sent only on days with ≥1 activity.
   → §5.8, §6, §9.3, §11.1.

3. **Extra nights are a stay date range, not "Sunday".** Stored as
   `extra_stay_start` / `extra_stay_end` dates. Employees can add at most one
   extra night before the first travel day and/or after the last (single-night
   toggles); admins can set wider ranges (2+ nights) for special cases. →
   §5.5, §8.

4. **Reminders may go to anyone on the roster.** This is a company-internal tool;
   no consent/opt-out flow is required in v1. → §9.1.

5. **Events are never deleted; past events are tucked away.** "Past" is derived
   from `end_date` in the event timezone. The UI separates current/upcoming from
   Past; past events are read-only for employees and fully editable by admins
   (logged). Volume is small (~3/year now, up to 10–20/year if department offsites
   adopt it) — no pagination needed, but the current/Past split keeps the list
   tidy. → §5.2, §6, §11.

6. **Per-event timezone (default `Europe/Paris`).** All timestamps are stored UTC
   and rendered/entered in the event's IANA timezone; reminder windows and the
   "is past" / deadline logic are evaluated in that zone. → §5.2, §6.3.

7. **Admin membership is bootstrapped + in-app.** The first user to sign in
   becomes admin; admins then promote/demote others via `/admin/users` (a
   last-admin demotion is blocked). No `ADMIN_EMAILS` env. → §5.1, §6.1, §7.

8. **One filter-driven export, not two buttons.** The single Export button
   downloads exactly what the dashboard filter currently shows
   (`export.csv?attending=…`); any future filter dimension extends the export for
   free. `no_response` rows carry empty submission columns, so the export doubles
   as a non-responder list when that filter is active. → §10.

9. **MCP surface included as Phase 7.** An additive, OAuth-gated, admin-scoped
   `/mcp` server (§18), off unless configured. The core app ships without it. →
   §2, §18.

---

## 18. MCP server (Phase 7)

An **optional, additive** surface that lets an MCP client (e.g. Claude) query and
manage events conversationally — "who hasn't responded for Dubrovnik?", "create
the Lisbon March 2027 offsite". It is **off by default**: enabled only when
`MCP_OAUTH_CLIENT_ID` + `MCP_OAUTH_CLIENT_SECRET` are set, so it can never weaken a
deployment that doesn't opt in. Phases 0–6 are fully functional without it.

### 18.1 Transport & auth
- **Server**: `modelcontextprotocol/go-sdk` `NewStreamableHTTPHandler`, **stateless**
  (no per-session map — every tool is a stateless DB read/write, and stateless mode
  avoids "session not found" 404s after a redeploy). Copied from the reference
  `mcpHandler()`.
- **Auth**: **OAuth 2.1 Authorization Code + PKCE** (`oauth.go`), with RFC 8414 /
  RFC 9728 discovery at `/.well-known/*`. The MCP client runs the OAuth dance,
  obtains a bearer token, and presents it on `/mcp`; `mcpTokenGateMiddleware`
  resolves it to a `*User` stashed in the request context (`userFromCtx`).
- **Authorization**: tool handlers enforce the **same role rules as the REST API**.
  Read tools require an authenticated admin; write tools require admin. Nothing is
  exposed via MCP that the same user couldn't reach through the SPA.
- **Rate limiting**: `/oauth/authorize`, `/oauth/token` per-IP throttled (copy the
  reference's 60/min); discovery left open.

### 18.2 Tools
Each tool has typed in/out structs with `jsonschema` tags (reference pattern) and
is wrapped by an `instrumentMCP` helper for Prometheus metrics. Mutating tools
write to the **activity log** exactly like the REST handlers (actor = the MCP
user), so MCP changes are as visible as any other.

**Read (admin):**
- `list_events` — current + past events with response counts.
- `get_event` — full config (dates, typed days, hotel, timezone, reminders).
- `get_dashboard` — attending-state counts + `no_response` list for an event.
- `list_non_responders` — roster members with no submission, by name (a focused
  shortcut over `get_dashboard`).
- `list_submissions` — submissions for an event (optionally filtered by attending
  state), mirroring the export filter.
- `get_activity` — recent activity-log entries for an event (after-deadline flagged).

**Write (admin):**
- `create_event` — create an event (+ generate typed days); validates slug + tz.
- `update_event` — change config / reminder settings / day types.
- `upload_roster` — replace the roster from inline `name,email` rows.
- `trigger_reminders` — force the reminder/digest evaluation for an event now
  (idempotent via `reminder_log`), for ad-hoc nudges.

Write tools deliberately stop short of editing individual attendees' personal
travel/dietary data over MCP (that stays in the admin UI), keeping the MCP write
surface to event administration rather than PII mutation.

### 18.3 Scope boundary
The MCP server reuses the existing query/command functions in `events.go`,
`dashboard.go`, `roster.go`, `activity.go`, and `reminders.go` — it is a thin
protocol adapter, not a second copy of the business logic. If Phase 7 is never
built, none of those packages change.
