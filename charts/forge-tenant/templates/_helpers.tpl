{{- define "forge-tenant.fullname" -}}
{{- if .Values.fullnameOverride -}}{{ .Values.fullnameOverride | trunc 50 | trimSuffix "-" }}
{{- else if .Values.global.forge.enabled -}}{{ printf "%s-tenant" .Release.Name | trunc 50 | trimSuffix "-" }}
{{- else -}}{{ .Release.Name }}{{- end -}}
{{- end -}}
{{- define "forge-tenant.envSecret" -}}
{{- if .Values.global.forge.enabled -}}{{ .Release.Name }}-tenant-env{{- else -}}{{ .Values.existingSecret }}{{- end -}}
{{- end -}}
{{- define "forge-tenant.externalSecret" -}}{{ .Release.Name }}-external-credentials{{- end -}}
{{- define "forge-tenant.caMount" -}}{{ if .Values.global.forge.enabled }}/tls{{ else }}/ca{{ end }}{{- end -}}
{{- define "forge-tenant.tlsSecret" -}}{{ if .Values.global.forge.enabled }}{{ .Release.Name }}-tls{{ else }}{{ required "internalTLS.existingSecret is required" .Values.internalTLS.existingSecret }}{{ end }}{{- end -}}
