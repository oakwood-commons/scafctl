apiVersion: v1
kind: Service
metadata:
  name: {{ .appName }}-svc
  namespace: {{ .namespace }}
spec:
  type: ClusterIP
  ports:
    - port: {{ .servicePort }}
