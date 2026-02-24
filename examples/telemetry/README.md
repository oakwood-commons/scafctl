# Telemetry Example — Jaeger + OTel Collector

This directory contains a minimal Docker Compose stack for exploring
scafctl's OpenTelemetry integration locally. It starts:

- **Jaeger all-in-one** — trace storage and UI
- **OTel Collector** — receives OTLP from scafctl, forwards to Jaeger

## Prerequisites

- Docker and Docker Compose (v2)

## Quick Start

```bash
# Start the stack
docker compose up -d

# Run a traced scafctl command
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
  scafctl run solution -f ../../examples/actions/hello-world.yaml \
  --otel-insecure \
  --log-level debug

# Open Jaeger UI — select service "scafctl" and click Find Traces
open http://localhost:16686
```

## Services

| Service | Port | URL |
|---------|------|-----|
| Jaeger UI | 16686 | http://localhost:16686 |
| Jaeger OTLP gRPC (internal) | 4317 (collector-side) | — |
| OTel Collector OTLP gRPC | 4317 | grpc://localhost:4317 |
| OTel Collector OTLP HTTP | 4318 | http://localhost:4318 |
| OTel Collector metrics | 8888 | http://localhost:8888/metrics |
| Jaeger Zipkin (optional) | 9411 | — |

## All Three Signals

### Traces

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
  scafctl run solution -f ../../examples/actions/hello-world.yaml --otel-insecure
```

Open http://localhost:16686, select service **scafctl**, click **Find Traces**.

### Logs (via OTLP)

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
  scafctl run solution -f ../../examples/actions/hello-world.yaml \
  --otel-insecure \
  --log-level debug \
  --log-format json
```

Logs are batched and forwarded to the collector. They will appear correlated
with their parent trace span in any OTLP-log-aware backend.

### Metrics (Prometheus scrape)

The scafctl MCP server exposes `/metrics` (Prometheus format). While running:

```bash
scafctl mcp start &
curl http://localhost:8080/metrics | grep scafctl_provider
```

The OTel Collector in this stack is also configured to expose its own internal
metrics at http://localhost:8888/metrics — useful for verifying data flow.

## Tear Down

```bash
docker compose down
```

## Customising the Collector

Edit `collector-config.yaml` to:
- Add exporters (e.g. Grafana Tempo, Prometheus remote write)
- Adjust batch sizes and retry settings
- Add processors (e.g. resource detection, attribute transformation)

Then restart: `docker compose restart otel-collector`
