apiVersion: v1
kind: Service
metadata:
  name: {{ .appName }}
  namespace: {{ .namespace }}
  labels:
    app: {{ .appName }}
spec:
  type: {{ .serviceType }}
  ports:
    - port: {{ .servicePort }}
      targetPort: {{ .containerPort }}
      protocol: TCP
  selector:
    app: {{ .appName }}
