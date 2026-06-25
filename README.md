# ID5 IRL Attendance App

A web app for collecting attendee information ahead of company offsites ("IRLs").
Admins (People team) configure an event once; employees sign in with Google SSO
(restricted to `@id5.io`) and submit attendance + travel details via a form with
conditional logic. The app tracks non-responders, sends reminders, logs all
activity, and exports responses.

See **[DESIGN.md](./DESIGN.md)** for the full design & implementation plan.

## Stack

- **Backend** — Go 1.26, chi, PostgreSQL (pgx), OIDC + JWT, Prometheus.
- **Frontend** — Vue 3 + Vite + Pinia + vue-router (TypeScript).
- **Deploy** — Docker Compose (dev); Helm + ArgoCD (prod).

## Layout

```
backend/    Go API server (internal/ packages, embedded SQL migrations)
frontend/   Vue 3 SPA (served by nginx, which proxies /api in prod)
compose.yml Docker Compose for local dev (db + backend + frontend)
```

## Local development

### Everything in Docker

```sh
cp .env.example .env          # adjust as needed
docker compose up --build
# app on http://localhost:8080
```

For a zero-config boot without a Google OAuth client, set `AUTH_MODE=password`
(a dev-only stub) in `.env`.

### Backend directly

```sh
cd backend
go build ./...
go test ./...
go run ./cmd/server     # needs DATABASE_URL to a running Postgres
```

### Frontend directly

```sh
cd frontend
npm install
npm run dev             # http://localhost:5173, proxies /api → :8080
npm run check           # typecheck + lint + test
```

## Implementation status

Built phase by phase (see DESIGN.md §16):

- [x] Phase 0 — Scaffolding (backend skeleton, frontend skeleton, schema, compose)
- [x] Phase 1 — Auth + user roles (OIDC + JWT, first-user-admin, promote/demote)
- [x] Phase 2 — Event config + timezone (CRUD, typed days, tz-aware deadlines)
- [x] Phase 3 — Attendee form + activity log (conditional form, past-event lock, timeline)
- [x] Phase 4 — Roster + dashboard + export (CSV upload, attending filter, auto-reload)
- [x] Phase 5 — Notifications, reminders & digest (tz-aware scheduler, idempotent, edit emails)
- [~] Phase 6 — Hardening & deploy (Helm chart in `helm/` done)
- [ ] Phase 7 — MCP server
