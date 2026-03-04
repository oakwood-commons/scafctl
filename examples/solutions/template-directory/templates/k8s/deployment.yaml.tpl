apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .appName }}
  namespace: {{ .namespace }}
  labels:
    app: {{ .appName }}
    version: {{ .appVersion }}
    environment: {{ .environment }}
spec:
  replicas: {{ .replicas }}
  selector:
    matchLabels:
      app: {{ .appName }}
  template:
    metadata:
      labels:
        app: {{ .appName }}
        version: {{ .appVersion }}
    spec:
      containers:
        - name: {{ .appName }}
          image: {{ .registry }}/{{ .appName }}:{{ .appVersion }}
          ports:
            - containerPort: {{ .containerPort }}
          resources:
            requests:
              cpu: {{ .cpuRequest }}
              memory: {{ .memoryRequest }}
            limits:
              cpu: {{ .cpuLimit }}
              memory: {{ .memoryLimit }}
          env:
            - name: APP_ENV
              value: {{ .environment }}
            - name: LOG_LEVEL
              value: {{ .logLevel }}
