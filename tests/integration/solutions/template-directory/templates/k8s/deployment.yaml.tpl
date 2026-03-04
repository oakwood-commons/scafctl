apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .appName }}
  namespace: {{ .namespace }}
spec:
  replicas: {{ .replicas }}
