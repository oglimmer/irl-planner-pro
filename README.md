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
- [x] Phase 7 — MCP server (OAuth 2.1 + PKCE, admin-scoped tools — off by default)

### MCP server (Phase 7)

An additive, admin-scoped [MCP](https://modelcontextprotocol.io) surface lets a
client (e.g. Claude) query and manage events conversationally. It is **off by
default** — the backend wires up `/mcp`, `/oauth`, and `/.well-known/*` only when
both `MCP_OAUTH_CLIENT_ID` and `MCP_OAUTH_CLIENT_SECRET` are set, so it can't
weaken a deployment that doesn't opt in. Auth is OAuth 2.1 (Authorization Code +
PKCE); tools enforce the same admin authorization as the REST API and every
mutation lands in the activity log. Tools: `list_events`, `get_event`,
`get_dashboard`, `list_non_responders`, `list_submissions`, `get_activity`
(read) and `create_event`, `update_event`, `upload_roster`, `trigger_reminders`
(write). See DESIGN.md §18. In Helm, set `mcp.enabled=true` and supply
`MCP_OAUTH_CLIENT_SECRET` in the sealed secret.
