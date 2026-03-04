# {{ .appName }}

> Auto-generated documentation for **{{ .appName }}** ({{ .environment }})

## Overview

| Property     | Value                          |
|-------------|--------------------------------|
| Version     | {{ .appVersion }}              |
| Namespace   | {{ .namespace }}               |
| Environment | {{ .environment }}             |
| Replicas    | {{ .replicas }}                |
| Registry    | {{ .registry }}                |

## Endpoints

- **Service port**: {{ .servicePort }}
- **Container port**: {{ .containerPort }}
- **Service type**: {{ .serviceType }}

## Database

- **Host**: {{ .dbHost }}:{{ .dbPort }}
- **Database**: {{ .dbName }}
- **Pool size**: {{ .dbPoolSize }}
