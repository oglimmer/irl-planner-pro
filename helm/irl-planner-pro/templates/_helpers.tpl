{{/*
Expand the name of the chart.
*/}}
{{- define "irl.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "irl.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart label
*/}}
{{- define "irl.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "irl.labels" -}}
helm.sh/chart: {{ include "irl.chart" . }}
{{ include "irl.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "irl.selectorLabels" -}}
app.kubernetes.io/name: {{ include "irl.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Backend
*/}}
{{- define "irl.backend.fullname" -}}
{{- printf "%s-backend" (include "irl.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "irl.backend.labels" -}}
helm.sh/chart: {{ include "irl.chart" . }}
{{ include "irl.backend.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: backend
{{- end }}

{{- define "irl.backend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "irl.name" . }}-backend
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: backend
{{- end }}

{{/*
Frontend
*/}}
{{- define "irl.frontend.fullname" -}}
{{- printf "%s-frontend" (include "irl.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "irl.frontend.labels" -}}
helm.sh/chart: {{ include "irl.chart" . }}
{{ include "irl.frontend.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: frontend
{{- end }}

{{- define "irl.frontend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "irl.name" . }}-frontend
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Postgres
*/}}
{{- define "irl.postgres.fullname" -}}
{{- printf "%s-postgres" (include "irl.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "irl.postgres.labels" -}}
helm.sh/chart: {{ include "irl.chart" . }}
{{ include "irl.postgres.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: postgres
{{- end }}

{{- define "irl.postgres.selectorLabels" -}}
app.kubernetes.io/name: {{ include "irl.name" . }}-postgres
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: postgres
{{- end }}

{{/*
Service account name
*/}}
{{- define "irl.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "irl.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Secret name. Resolution order:
  1. .Values.existingSecret — explicit override pointing at a Secret/SealedSecret
     the user manages out-of-band (the chart does NOT create it).
  2. "<fullname>-secret" — derived from the release; the user is still expected
     to apply a matching Secret/SealedSecret separately, since the chart does
     not ship one (see helm/argocd/irl-planner-pro-sealed-secret.yaml for the
     production deployment's SealedSecret).

Required keys: JWT_SECRET, POSTGRES_PASSWORD (or DATABASE_URL when
postgres.enabled=false). Optional keys: OIDC_CLIENT_SECRET (auth.mode=oidc),
SMTP_PASSWORD, METRICS_TOKEN.
*/}}
{{- define "irl.secretName" -}}
{{- if .Values.existingSecret -}}
{{- .Values.existingSecret -}}
{{- else -}}
{{- printf "%s-secret" (include "irl.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}
