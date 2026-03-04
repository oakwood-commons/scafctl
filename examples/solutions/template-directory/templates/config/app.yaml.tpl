server:
  host: 0.0.0.0
  port: {{ .containerPort }}

logging:
  level: {{ .logLevel }}
  format: json

database:
  host: {{ .dbHost }}
  port: {{ .dbPort }}
  name: {{ .dbName }}
  pool:
    maxOpen: {{ .dbPoolSize }}
    maxIdle: 5

cache:
  enabled: {{ .cacheEnabled }}
  ttl: {{ .cacheTTL }}
