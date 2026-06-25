# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

ID5 IRL Attendance App — a web app for collecting attendee info ahead of company
offsites. Admins (People team) configure an event; employees sign in with Google
SSO (restricted to `@id5.io`) and submit attendance + travel via a conditional
form. The app tracks non-responders, sends tz-aware reminders, logs all activity,
and exports CSV.

**`DESIGN.md` is the authoritative spec** — data model, API surface, conditional
form rules, auth model, and the phased plan all live there. Read the relevant
section before changing behavior; it explains *why*, which the code doesn't. The
README tracks phase status (Phases 0–5 done; 6 hardening/deploy partly done — the
`helm/` chart now exists — and 7 MCP not yet built, so `mcp.go`/`oauth.go`
referenced in DESIGN.md do not exist yet).

## Deploy (`helm/`)

`helm/irl-planner-pro` is a self-contained chart (backend + frontend + bundled
Postgres + ingress) deploying to `irl-planner.oglimmer.com`. The backend is
stateless — all state lives in Postgres, so there is no backend PVC. The chart
does not create the app Secret; supply `<release>-irl-planner-pro-secret`
(keys: `JWT_SECRET`, `POSTGRES_PASSWORD`, `OIDC_CLIENT_SECRET`, optional
`SMTP_PASSWORD`/`METRICS_TOKEN`). `helm/argocd/` holds the ArgoCD Applications
and a SealedSecret template. The frontend ConfigMap ships an SPA-only nginx
config (drops the compose `proxy_pass http://backend` blocks, which would crash
nginx in-cluster); when adding a backend path, update BOTH the ingress `paths`
in `values.yaml` AND `frontend/nginx.conf`.

## Commands

Backend (`cd backend`, Go 1.26):
```sh
go build ./...
go test ./...                       # DB-backed tests SKIP unless IRL_TEST_DATABASE_URL is set
go test ./internal/server -run TestFirstUserBecomesAdmin   # single test
go run ./cmd/server                 # needs DATABASE_URL to a running Postgres
```

Frontend (`cd frontend`):
```sh
npm install
npm run dev          # :5173, proxies /api and /healthz → :8080 (see vite.config.ts)
npm run check        # typecheck (vue-tsc) + lint (eslint) + test (vitest) — the CI gate
npm run test         # vitest run --passWithNoTests
```

Full stack: `cp .env.example .env && docker compose up --build` → http://localhost:8080.
For a zero-config boot without a Google OAuth client, set `AUTH_MODE=password` in
`.env` (dev stub: `POST /api/auth/dev-login` mints a session for any email — never
enable on a shared deployment).

### DB-backed tests

Tests under `internal/server` that touch Postgres call `testDB(t)`
(`users_test.go`), which **skips** the whole test when `IRL_TEST_DATABASE_URL` is
unset — so plain `go test ./...` stays green with no database. To run them, point
that var at a throwaway Postgres; `testDB` runs migrations then `TRUNCATE`s every
table so each test starts clean. Pure-logic tests (validation, timeutil) don't
need a DB and always run.

## Architecture

**Backend** (`backend/`, module `irlplanner`): single Go module, chi router,
Postgres via pgx/v5 through the `database/sql` adapter. Boot order in
`cmd/server/main.go`: `config.Load()` → `db.Open` → `db.Migrate` → `InitOIDC`
(oidc mode only) → `StartReminders` → HTTP serve. Graceful shutdown cancels a
root context that every background goroutine derives from, tracked by a
`sync.WaitGroup`.

- **`internal/server`** is the whole HTTP layer. Every handler hangs off the
  `*App` receiver (`app.go`: `Cfg`, `DB`, `OIDC`, `Email`, readiness flag). One
  file per domain: `users.go`, `events.go`, `submissions.go`, `roster.go`,
  `dashboard.go`, `export.go`, `activity.go`, `reminders.go`, `notify.go`,
  `oidc.go`, `auth.go`, `timeutil.go`, `errors.go`. `router.go` wires everything.
- **`internal/{config,db,email,metrics,workspaceauth,buildinfo}`** are leaf
  packages with no server deps.
- **Migrations** are embedded `.sql` files (`internal/db/migrations/NNNN_*.sql`)
  run sequentially by `db.Migrate` — no external migration tool. Add a new
  numbered file; never edit an applied one. `db.Open` uses `QueryExecModeExec`
  with no statement cache (PgBouncer-safe) — keep that pool config verbatim.

**Routing & auth** (`router.go`): three nested groups under `/api` —
(1) public (`/version`, `/auth/config`, the rate-limited login endpoints),
(2) `authMiddleware` (verifies JWT, loads user, checks `token_version`) for
attendee-facing reads/writes, (3) `requireAdminMiddleware` for the admin group.
**Admin event routes are namespaced under `/api/admin/events/{id}`** (id-keyed) so
they don't collide with the **slug-keyed** attendee route `/api/events/{slug}`.
The backend is the source of truth for authz; the frontend router guard mirrors it
only for UX.

**Frontend** (`frontend/src/`, Vue 3 `<script setup>` + Pinia + vue-router): lazy
views in `views/`, a `beforeEach` auth guard in `router.ts`, `stores/auth.ts`
(token + user in localStorage, JWT-exp short-circuit), and `api.ts` (thin `fetch`
wrapper with `ApiError`). nginx serves the SPA and proxies `/api` in prod.

## Conventions that bite if missed

- **Name and allergies are profile properties, not submission fields.** They live
  on `users` (`first_name`/`last_name` added in migration 0002, `allergies` in
  0003) and are edited at `/profile` via `PUT /api/me`. The IdP seeds the name
  **only on first login** — a later login never overwrites it, so a user's own
  edit always wins. Dashboards/exports join these in from the submitter's profile;
  do not add name/allergies columns back onto `submissions`.
- **Conditional form rules are enforced server-side** in `submissions.go`, not
  just in the Vue form — never trust the client. Fields outside the chosen
  `attending` branch are blanked on write. See DESIGN.md §8 for the full matrix.
- **Time zones**: storage is always UTC (`TIMESTAMPTZ`); `DATE` columns are
  event-local calendar dates with no zone. All display/input is in the event's
  IANA `timezone`. "Is past", deadline interpretation, and reminder windows are
  all computed in the event zone via `timeutil.go` — never with the server's local
  time.
- **Append-only audit**: every submission write appends a `submission_revisions`
  snapshot **and** an `activity_log` entry, stamping `after_deadline`. Reminders
  are made idempotent by inserting into `reminder_log` with `ON CONFLICT DO
  NOTHING` and sending **only if the insert created a row** (restart-safe).
- **No delete**: events are never deleted; "past" is derived from `end_date` in
  the event tz. There is no delete endpoint anywhere.
- **First user to sign in becomes admin** automatically (when `users` is empty);
  thereafter admins promote/demote in-app. The last admin can't be demoted.
- Email sends are best-effort: a failure logs a WARN and never fails the request.
  Empty `SMTP_HOST` disables email entirely.

## Local CLI tooling note

The user's global `~/.claude/CLAUDE.md` mandates structural tools over regex:
prefer `ast-grep` for code-structure search/rewrite, `yq` for YAML edits, `difft`
for reviewing diffs, and run `shellcheck`/`yamllint` on generated shell/YAML.
