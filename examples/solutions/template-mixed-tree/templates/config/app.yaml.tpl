apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .appName }}-config
  namespace: {{ .namespace }}
data:
  replicas: "{{ .replicas }}"
