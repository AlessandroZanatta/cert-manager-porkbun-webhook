{{/*
Chart name, truncated to fit Kubernetes name limits.
*/}}
{{- define "porkbun-webhook.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name. The release name is used alone when it already
contains the chart name.
*/}}
{{- define "porkbun-webhook.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "porkbun-webhook.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "porkbun-webhook.selectorLabels" -}}
app.kubernetes.io/name: {{ include "porkbun-webhook.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "porkbun-webhook.labels" -}}
helm.sh/chart: {{ include "porkbun-webhook.chart" . }}
{{ include "porkbun-webhook.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "porkbun-webhook.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "porkbun-webhook.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- required "serviceAccount.name is required when serviceAccount.create is false" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "porkbun-webhook.groupName" -}}
{{- required "groupName is required, e.g. --set groupName=acme.example.com" .Values.groupName -}}
{{- end -}}

{{- define "porkbun-webhook.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
{{- end -}}

{{- define "porkbun-webhook.selfSignedIssuer" -}}
{{ printf "%s-selfsign" (include "porkbun-webhook.fullname" .) }}
{{- end -}}

{{- define "porkbun-webhook.rootCAIssuer" -}}
{{ printf "%s-ca" (include "porkbun-webhook.fullname" .) }}
{{- end -}}

{{- define "porkbun-webhook.rootCACertificate" -}}
{{ printf "%s-ca" (include "porkbun-webhook.fullname" .) }}
{{- end -}}

{{- define "porkbun-webhook.servingCertificate" -}}
{{ printf "%s-webhook-tls" (include "porkbun-webhook.fullname" .) }}
{{- end -}}
