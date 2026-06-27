# ID5 IRL Attendance App

A web app for collecting attendee information ahead of company offsites ("IRLs").
Admins (People team) configure an event once; employees sign in with Google SSO
(restricted to `@id5.io`) and submit attendance + travel details via a form with
conditional logic. The app tracks non-responders, sends invitations + tz-aware
reminders over **email or Slack**, logs all activity, and exports responses.
Events can carry a cover image, and the admin activity timeline is filterable by
participant vs. admin actions.

See **[DESIGN.md](./DESIGN.md)** for the full design & implementation plan.

## Stack

- **Backend** — Go 1.26, chi, PostgreSQL (pgx), OIDC + JWT, Prometheus.
- **Frontend** — Vue 3 + Vite + Pinia + vue-router (TypeScript).
- **Messaging** — SMTP email (`internal/email`) + Slack bot DMs (`internal/slack`).
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

> **`.env` is only read by Docker Compose, not by the backend.** `config.Load()`
> reads the real process environment (`os.Getenv`) — it does **not** parse a
> `.env` file. So `docker compose up` picks up `.env` automatically, but
> `go run ./cmd/server` ignores it. To run the backend directly, export the vars
> into your shell first, e.g. in fish:
>
> ```fish
> for line in (grep -vE '^\s*#|^\s*$' .env)
>     set -gx (string split -m1 '=' $line)
> end
> go run ./cmd/server
> ```
>
> Note `.env`'s `DATABASE_URL` uses host `db` (the Compose service name); when
> running on the host, point it at `localhost:5432` instead.

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
- [x] Phase 5 — Notifications, reminders & digest (tz-aware scheduler, idempotent, edit emails, Messaging tab w/ email + Slack)
- [~] Phase 6 — Hardening & deploy (Helm chart in `helm/` done)
- [x] Phase 7 — MCP server (OAuth 2.1 + PKCE, admin-scoped tools — off by default)

### Messaging (email + Slack)

The admin **Messaging tab** sends per-event invitations and manual follow-ups
over a selectable channel, using admin-editable templates (the same copy the
background reminder scheduler sends). Both channels are best-effort and idempotent
(a `reminder_log` claim makes every recipient exactly-once; a `message_send_log`
row records each outcome for the failure list):

- **Email** — stdlib SMTP (`internal/email`). Empty `SMTP_HOST` disables it.
- **Slack** — workspace **bot** DMs (`internal/slack`): the recipient's company
  email is resolved with `users.lookupByEmail`, then `chat.postMessage` sends the
  DM. Because it uses a bot token, the People team can message any employee
  **without that employee installing or authorizing the app** — the enterprise
  install model. Set `SLACK_BOT_TOKEN` (scopes `users:read.email` + `chat:write`);
  empty disables Slack. In Helm, supply `SLACK_BOT_TOKEN` in the sealed secret.

Scheduled reminders and admin notices remain email-only. See DESIGN.md §9.

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

#### Connecting Claude Code to `/mcp` in local dev

The OAuth 2.1 flow is built for **claude.ai** (the default redirect URI is
`https://claude.ai/api/mcp/auth_callback`, redirect URIs are exact-matched, and
there is no dynamic client registration), so it doesn't fit the **Claude Code
CLI** cleanly. For local dev, skip OAuth entirely: the `/mcp` gate also accepts
an ordinary session JWT (not just an `mcp_access` token), so you can pass a token
via a custom header. Every MCP tool requires **admin**, and the **first user to
sign in becomes admin**, so mint the token for that first user.

1. **Enable `/mcp` + dev auth** in `.env` (both client vars must be non-empty or
   `/mcp` isn't mounted; values are arbitrary for the header bypass):

   ```sh
   AUTH_MODE=password
   MCP_OAUTH_CLIENT_ID=dev
   MCP_OAUTH_CLIENT_SECRET=dev
   PUBLIC_BASE_URL=http://localhost:8080
   ```

2. **Boot the stack:** `docker compose up --build` (→ http://localhost:8080).

3. **Mint an admin session token** (first email becomes admin if `users` is empty):

   ```sh
   curl -s -X POST http://localhost:8080/api/auth/dev-login \
     -H 'Content-Type: application/json' \
     -d '{"email":"you@id5.io","name":"You Dev"}' | jq -r .token
   ```

4. **Register it with Claude Code** (HTTP transport + the token as a header):

   ```sh
   claude mcp add --transport http irl http://localhost:8080/mcp \
     --header "Authorization: Bearer <token-from-step-3>"
   ```

Then `/mcp` inside Claude Code lists `irl` as connected; `claude mcp get irl`
verifies it. Point Claude Code at the **backend** (`:8080`) — the Vite dev proxy
forwards `/api` and `/healthz` but **not** `/mcp`. The token is a 30-day session
JWT; re-mint via dev-login if `JWT_SECRET` or the user's `token_version` changes.
Use the default `--scope local`; don't commit a bearer token to a shared scope.
