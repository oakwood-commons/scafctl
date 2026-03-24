---
title: "Telemetry (Traces, Metrics, Logs)"
weight: 96
---

# Telemetry Tutorial

scafctl emits **all three OpenTelemetry signals** — logs, traces, and metrics.
By default, telemetry is silent (noop providers). When you point scafctl at an
OTLP endpoint every signal is exported for backend analysis.

---

## Overview

| Signal  | Default output | With `--otel-endpoint` |
|---------|---------------|------------------------|
| Logs    | slog text/JSON → stderr (via `--log-level`) | Also batched via OTLP gRPC |
| Traces  | Disabled (noop) | Exported via OTLP gRPC |
| Metrics | Prometheus `/metrics` (MCP server) | Also pushed via OTLP gRPC |

---

## Configuration

### Flags (per-invocation)

{{< tabs "telemetry-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
# Point at a local OTel Collector
scafctl run solution -f solution.yaml \
  --otel-endpoint localhost:4317 \
  --otel-insecure          # disable TLS (development only)

# Override with environment variable instead
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
  scafctl run solution -f solution.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Point at a local OTel Collector
scafctl run solution -f solution.yaml `
  --otel-endpoint localhost:4317 `
  --otel-insecure          # disable TLS (development only)

# Override with environment variable instead
$env:OTEL_EXPORTER_OTLP_ENDPOINT = 'localhost:4317'
scafctl run solution -f solution.yaml
```
{{% /tab %}}
{{< /tabs >}}

| Flag | Env override | Default | Description |
|------|-------------|---------|-------------|
| `--otel-endpoint` | `OTEL_EXPORTER_OTLP_ENDPOINT` | _(none)_ | OTLP gRPC endpoint. When unset, tracing is disabled (noop). |
| `--otel-insecure` | _(none)_ | `false` | Skip TLS verification. Use in local dev only. |

---

## Signals in Detail

### Logs

Logs use the `go-logr/logr` interface throughout the codebase. The underlying
sink is a `multiSink` that fans out to:

1. **slog handler** → stderr (text or JSON via `--log-format`)
2. **otellogr bridge** → OTel `LoggerProvider` → OTLP when `--otel-endpoint` is set

When no OTLP endpoint is configured, only the slog handler is active — no OTel
log records are emitted to stderr. When an active span is in scope, the OTLP log
record carries the `trace_id` and `span_id` automatically (correlation without
any extra code).

Control verbosity:

{{< tabs "telemetry-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl run solution -f solution.yaml \
  --log-level debug \
  --log-format json \
  --otel-endpoint localhost:4317 \
  --otel-insecure
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run solution -f solution.yaml `
  --log-level debug `
  --log-format json `
  --otel-endpoint localhost:4317 `
  --otel-insecure
```
{{% /tab %}}
{{< /tabs >}}

See the [Logging Tutorial](logging-tutorial.md) for full flag reference.

### Traces

Spans are created at every major execution boundary:

| Subsystem | Span name | Key attributes |
|-----------|-----------|----------------|
| HTTP client | `http.client.request` (otelhttp) | `http.method`, `http.url`, `http.status_code` |
| Provider executor | `provider.Execute` | `provider.name` |
| Resolver executor | `resolver.Execute` | `resolver.count` |
| Resolver (single) | `resolver.executeResolver` | `resolver.name`, `resolver.phase`, `resolver.sensitive` |
| Solution loader | `solution.Get` | `solution.path` |
| Solution loader (bundle) | `solution.GetWithBundle` | `solution.path` |
| Solution (local FS) | `solution.FromLocalFileSystem` | `solution.path` |
| Solution (URL) | `solution.FromURL` | `solution.url` |
| Action workflow | `action.Execute` | `action.count` |
| Action (single) | `action.executeAction` | `action.name` |
| MCP tool call | `mcp.tool` | `mcp.tool.name` |

All spans propagate W3C `traceparent` / `tracestate` headers on outbound HTTP
requests via `otelhttp.NewTransport`, enabling distributed tracing when calling
instrumented backends.

#### Local trace debugging

Without `--otel-endpoint`, tracing is disabled (noop). To inspect traces locally,
run a local collector such as [otel-desktop-viewer](https://github.com/CtrlSpice/otel-desktop-viewer)
or Jaeger and point scafctl at it:

{{< tabs "telemetry-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
# Start local Jaeger (see examples/telemetry/ for Docker Compose)
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
  scafctl run solution -f solution.yaml --otel-insecure
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Start local Jaeger (see examples/telemetry/ for Docker Compose)
$env:OTEL_EXPORTER_OTLP_ENDPOINT = 'localhost:4317'
scafctl run solution -f solution.yaml --otel-insecure
```
{{% /tab %}}
{{< /tabs >}}

### Metrics

Metrics are exported via the Prometheus bridge exporter. The MCP server exposes
a `/metrics` endpoint (Prometheus scrape format). When `--otel-endpoint` is set,
the same metrics are also pushed via OTLP gRPC at the default push interval.

Key metrics:

| Metric | Type | Labels |
|--------|------|--------|
| `scafctl_provider_execution_duration_seconds` | Histogram | `provider_name`, `status` |
| `scafctl_provider_execution_total` | Counter | `provider_name`, `status` |
| `scafctl_http_client_duration_seconds` | Histogram | `status_code`, `url`, `method` |
| `scafctl_http_client_requests_total` | Counter | `status_code`, `url`, `method` |
| `scafctl_resolver_execution_duration_seconds` | Histogram | `resolver_name`, `status` |
| `scafctl_resolver_executions_total` | Counter | `resolver_name`, `status` |
| `scafctl_get_solution_time_histogram` | Histogram | `path` |

---

## Running Locally with Jaeger

The `examples/telemetry/` directory contains a ready-to-use Docker Compose stack
with Jaeger (all-in-one) and an OTel Collector.

### Prerequisites

- Docker and Docker Compose

### Start the stack

```bash
cd examples/telemetry
docker compose up -d
```

Services started:

| Service | URL |
|---------|-----|
| Jaeger UI | http://localhost:16686 |
| OTel Collector (OTLP gRPC) | localhost:4317 |
| OTel Collector (OTLP HTTP) | localhost:4318 |
| Prometheus metrics (collector) | http://localhost:8888/metrics |

### Run a traced command

{{< tabs "telemetry-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
  scafctl run solution -f examples/actions/hello-world.yaml \
  --otel-insecure \
  --log-level debug
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
$env:OTEL_EXPORTER_OTLP_ENDPOINT = 'localhost:4317'
scafctl run solution -f examples/actions/hello-world.yaml `
  --otel-insecure `
  --log-level debug
```
{{% /tab %}}
{{< /tabs >}}

### View traces in Jaeger

1. Open http://localhost:16686
2. Select service **scafctl** from the dropdown
3. Click **Find Traces**
4. Click any trace to see the waterfall of spans

### Tear down

```bash
cd examples/telemetry
docker compose down
```

---

## Running with a Production Collector

Point scafctl at your organisation's OTLP endpoint (no `--otel-insecure` flag
for TLS-enabled collectors):

{{< tabs "telemetry-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
scafctl run solution -f solution.yaml \
  --otel-endpoint otel-collector.example.com:4317 \
  --log-level info
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run solution -f solution.yaml `
  --otel-endpoint otel-collector.example.com:4317 `
  --log-level info
```
{{% /tab %}}
{{< /tabs >}}

Use the environment variable in CI/CD pipelines instead of flags:

```yaml
# GitHub Actions
env:
  OTEL_EXPORTER_OTLP_ENDPOINT: otel-collector.example.com:4317
steps:
  - run: scafctl run solution -f solution.yaml --log-level info
```

---

## Resource Attributes

Every span, metric, and log record produced by scafctl includes:

| Attribute | Value |
|-----------|-------|
| `service.name` | `scafctl` |
| `service.version` | Build version (e.g. `1.2.3`) |
| `commit` | Git commit SHA |
| `build_time` | Binary build timestamp |
| `os.type` | Host OS (e.g. `darwin`, `linux`) |
| `host.name` | Hostname |

---

## Next Steps

- [Logging Tutorial](logging-tutorial.md) — Log levels, formats, and file output
- [MCP Server Tutorial](mcp-server-tutorial.md) — Host the MCP server and scrape `/metrics`
- [examples/telemetry/README.md](../../examples/telemetry/README.md) — Local Jaeger + Collector setup
