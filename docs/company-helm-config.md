# Company identity / email configuration

The application ships with placeholder company values (team email addresses, the
mail `From:` header, and — for a Google IdP — the sign-in domain allowlist).
Nothing about these is hard-coded: every one is read from an **environment
variable** at boot (`backend/internal/config/config.go`), so when you deploy from
a **different Helm chart in another repo** (the company chart), set them there.

This repo's in-tree chart (`helm/irl-planner-pro`) already sets these via its
`values.yaml`; the table below is the canonical list to replicate in the company
chart's own values + deployment template.

## What to set

| Env var | Backend config field | In-tree Helm value (`values.yaml`) | Default if unset | Purpose |
|---|---|---|---|---|
| `PEOPLE_TEAM_EMAIL` | `PeopleTeamEmail` | `peopleTeamEmail` | `people@oglimmer.com` | Address employees are told to contact in the "can't attend" form instructions. Also served to the SPA via `GET /api/auth/config` and rendered on the form. |
| `IRL_TEAM_EMAIL` | `IRLTeamEmail` | `irlTeamEmail` | *(empty → no digest recipient)* | Recipient of the daily activity digest and "submission changed" notifications. |
| `SMTP_FROM` | `SMTPFrom` | `smtp.from` | *(empty)* | `From:` header on all outbound mail (invitations, reminders, notifications). Must be a sender the relay/SES identity is authorized to use. |
| `OIDC_GOOGLE_WORKSPACE_DOMAINS` | `AllowedGoogleWorkspaceDomains` | `auth.oidc.googleWorkspaceDomains` (YAML list) | *(empty → no restriction)* | Google Workspace sign-in allowlist (the `hd`-claim gate). **Only enforced when the OIDC issuer is Google** (`accounts.google.com`); a no-op for any other IdP (e.g. Keycloak). Set to `oglimmer.com` when signing in with Google. |
| `SIGN_IN_DOMAIN` | `SignInDomain` | `signInDomain` | *(empty → first `OIDC_GOOGLE_WORKSPACE_DOMAINS` entry, else generic copy)* | The email domain shown in the **login-page copy** ("Restricted to verified @&lt;domain&gt; accounts", the sign-in placeholder, and the domain-not-allowed error). Served via `GET /api/auth/config` and rendered by the SPA — this is what makes the login text prod-configurable instead of hard-coded. Set to `id5.io` to keep the current wording. |

Notes:
- `PEOPLE_TEAM_EMAIL` is the only email value with a non-empty code default
  (`people@oglimmer.com`), so it renders even if the chart forgets to set it —
  set it explicitly anyway so the displayed address is correct.
- `IRL_TEAM_EMAIL` and `SMTP_FROM` default to empty. Empty `IRL_TEAM_EMAIL` means
  the daily digest has no recipient; empty `SMTP_FROM` leaves the `From:` header
  to the relay's default. Set both for a production deployment.
- `OIDC_GOOGLE_WORKSPACE_DOMAINS` is a comma/space-separated list in the env var,
  and a YAML list in Helm values. When `AUTH_MODE=oidc` and this is empty, the
  backend logs a WARN that domain restriction is disabled.

## How the in-tree chart wires them (mirror this)

`helm/irl-planner-pro/templates/deployment-backend.yaml`:

```yaml
- name: IRL_TEAM_EMAIL
  value: {{ .Values.irlTeamEmail | quote }}
- name: PEOPLE_TEAM_EMAIL
  value: {{ .Values.peopleTeamEmail | quote }}
{{- with .Values.auth.oidc.googleWorkspaceDomains }}
- name: OIDC_GOOGLE_WORKSPACE_DOMAINS
  value: {{ join "," . | quote }}
{{- end }}
{{- with .Values.smtp.from }}
- name: SMTP_FROM
  value: {{ . | quote }}
{{- end }}
```

## Company `values.yaml` snippet

```yaml
irlTeamEmail: "irl@id5.io"
peopleTeamEmail: "people@id5.io"
signInDomain: "id5.io"            # login-page copy: "Restricted to verified @id5.io accounts"
smtp:
  from: '"ID5 IRL" <noreply@id5.io>'
auth:
  oidc:
    # Only meaningful with a Google issuer; drop/leave empty for Keycloak et al.
    googleWorkspaceDomains:
      - id5.io
```

> The values above are the prod company values. The GitHub repo itself no longer
> contains any `@id5.io` email address — the prod `@id5.io` identity lives only in
> the company chart's values, injected via these env vars at deploy time.

## Related non-email placeholders (out of scope of the email cleanup)

These still reference `id5.io` as an example/default and are the auth-domain, not
an email — review separately if the company IdP differs:

- `OIDC_GOOGLE_WORKSPACE_DOMAINS` default example in `.env.example` and `DESIGN.md`.
- `helm/irl-planner-pro/values.yaml` commented example `# - id5.io` under
  `googleWorkspaceDomains`.
- `frontend/src/components/Id5Logo.vue` — the ID5 wordmark artwork (branding, not
  config).
