{{- define "mdai.natsUrl" -}}
{{- if .Values.natsUrl }}
{{- .Values.natsUrl }}
{{- else -}}
nats://{{ .Release.Name }}-nats.{{ .Release.Namespace }}.svc.cluster.local:4222
{{- end }}
{{- end }}
