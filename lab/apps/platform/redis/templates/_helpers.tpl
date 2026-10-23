{{- define "kuchdesk.labels" -}}
app.kubernetes.io/name: {{ index . 1 }}
app.kubernetes.io/instance: {{ (index . 0).Release.Name }}
app.kubernetes.io/part-of: kuchdesk-shared-development
app.kubernetes.io/managed-by: {{ (index . 0).Release.Service }}
{{- end -}}
