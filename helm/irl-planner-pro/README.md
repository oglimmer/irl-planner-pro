# irl-planner-pro Helm chart

Deploys the ID5 IRL Attendance App: Go backend (API + tz-aware reminder
scheduler), Vue SPA served by nginx, and a bundled single-replica Postgres.
Mirrors the deployment pattern of `plugin-skill-hosting`, tailored to this app
(the backend is stateless — all state is in Postgres, so there is no backend PVC).

## What it deploys

| Component | Kind | Notes |
|-----------|------|-------|
| backend   | Deployment + Service | Go API on :8080, `/healthz` + `/readyz` probes, optional Prometheus `/metrics` |
| frontend  | Deployment + Service + ConfigMap | nginx serving the SPA (SPA-only config; `/api` is routed by the Ingress, not nginx) |
| postgres  | StatefulSet + Service + PVC | Bundled DB; set `postgres.enabled=false` to use an external one via the `DATABASE_URL` secret key |
| ingress   | Ingress | cert-manager TLS; backend paths (`/api`, `/healthz`, `/readyz`) before the SPA catch-all |

## Secrets

The chart does **not** create a Secret. Apply one named `<release>-irl-planner-pro-secret`
(or point `existingSecret` at your own). Keys:

- `JWT_SECRET` (required, ≥32 chars)
- `POSTGRES_PASSWORD` (when `postgres.enabled=true`) or `DATABASE_URL` (when `false`)
- `OIDC_CLIENT_SECRET` (when `auth.mode=oidc`)
- `SMTP_USERNAME` + `SMTP_PASSWORD` (optional; both needed for an authenticating
  relay like Fastmail — without them the backend skips SMTP AUTH and sends fail 530)
- `METRICS_TOKEN` (optional)

See `helm/argocd/irl-planner-pro-sealed-secret.yaml` for the SealedSecret template.

## Quick start

```sh
helm install irl helm/irl-planner-pro \
  --namespace irl-planner-pro --create-namespace \
  --set auth.oidc.clientID=<google-client-id>
```

GitOps: apply the two ArgoCD Applications in `helm/argocd/` (regenerate the
SealedSecret first).
