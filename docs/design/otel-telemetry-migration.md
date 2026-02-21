# OpenTelemetry Telemetry Migration

**Status:** Proposed  
**Date:** 2026-02-20  
**Scope:** `pkg/logger`, `pkg/metrics`, `pkg/telemetry` (new), `pkg/cmd/scafctl/root.go`, trace instrumentation across all subsystems

---

## Overview

This document describes the migration of scafctl's telemetry stack from its current bespoke combination of zap/zapr/Prometheus to a unified OpenTelemetry foundation covering all three observability signals: **logs**, **traces**, and **metrics**.

### Goals

- Remove `go.uber.org/zap`, `go.uber.org/zap/zapcore`, and `github.com/go-logr/zapr`
- Keep `github.com/go-logr/logr` as the **sole logging interface** — zero changes in 80+ consumer files
- Route all three signals through the OTel SDK, exported via OTLP when configured and written locally by default
- Preserve the existing Prometheus `/metrics` scrape endpoint
- Accept the otel log SDK's Beta stability — spec is stable, breaking API changes are manageable given not being in production

### Non-Goals

- Changing any log call sites (`logger.FromContext`, `lgr.Info(...)`, `lgr.V(1).Info(...)`, etc.)
- Adding a slog API surface to the codebase
- Replacing the `pkg/terminal/writer` package — it is terminal UX, not structured logging

---

## Current State

### Logging

`pkg/logger/logger.go` initializes a `*zap.Logger` via `zapcore` and wraps it with `zapr.NewLogger()` to produce a `logr.Logger`. The `sync.Once`-guarded `GetWithOptions()` function sets a package-level `globalLogrLogger`. All 80+ packages retrieve the logger via `logger.FromContext(ctx)` or `logger.WithLogger(ctx, lgr)`.

`pkg/cmd/scafctl/root.go` calls `logger.ParseLogLevel()` (returns `zapcore.Level`) then `logger.GetWithOptions(logger.Options{Level: zapcore.Level(...)})` in `PersistentPreRun`.

`root.go` also directly imports `go.uber.org/zap` to use `zap.Field`-style structured fields on the logr instance.

### Metrics

`pkg/metrics/metrics.go` directly uses `github.com/prometheus/client_golang` types (`prometheus.CounterVec`, `prometheus.HistogramVec`, `prometheus.GaugeVec`). `RegisterMetrics()` calls `prometheus.MustRegister()` on all 15 metrics and wires `promhttp.Handler()` to `/metrics`. There is no OTel involvement.

### Traces

No trace instrumentation exists anywhere in the codebase.

### OpenTelemetry

There are no `go.opentelemetry.io/*` imports in the main module. OTel is only present as transitive indirect dependencies in the separate `scripts/go.mod` sub-module.

---

## Architecture After Migration

```
┌──────────────────────────────────────────────────────────────────────┐
│                         Application Code                             │
│           lgr.Info(...)   lgr.V(1).Info(...)   lgr.Error(...)        │
│           tracer.Start(ctx, "span")                                  │
│           meter.Float64Counter("metric")                             │
└────────────────────────┬─────────────────────────────────────────────┘
                         │ logr interface (unchanged)
              ┌──────────▼──────────┐
              │  pkg/logger         │  MultiSink
              │  ─────────────────  │  ├─ slog text/json handler → stderr / file
              │  logr.New(sink)     │  └─ otellogr.LogSink → OTel LoggerProvider
              └─────────────────────┘
                                           │
┌──────────────────────────────────────────▼──────────────────────────┐
│                        pkg/telemetry                                 │
│                                                                      │
│  LoggerProvider  ─────────────────────────────────────┐             │
│  TracerProvider  ──────────────────────────┐          │             │
│  MeterProvider   ─────────────┐            │          │             │
│                               │            │          │             │
│            ┌──────────────────▼──┐  ┌──────▼──┐  ┌───▼──────┐     │
│            │  Prometheus         │  │  OTLP   │  │  OTLP    │     │
│            │  exporter           │  │  trace  │  │  log     │     │
│            │  /metrics (HTTP)    │  │  grpc   │  │  grpc    │     │
│            └─────────────────────┘  └─────────┘  └──────────┘     │
│                 (always on)          (when OTEL_EXPORTER_OTLP_ENDPOINT set)
└──────────────────────────────────────────────────────────────────────┘
```

### Signal Summary

| Signal  | Local output                     | OTLP output (when endpoint set)          | SDK stability |
|---------|----------------------------------|------------------------------------------|---------------|
| Logs    | slog text/json handler → stderr  | `otlploggrpc` BatchProcessor             | Beta (v0.x)   |
| Traces  | `stdouttrace` → stderr           | `otlptracegrpc`                          | Stable        |
| Metrics | Prometheus `/metrics` (always)   | `otlpmetricgrpc` (optional addition)     | Stable        |

---

## Implementation Phases

### Phase 1 — Replace zap/zapr with otellogr + slog MultiSink

**Affected files:**
- `pkg/logger/logger.go` — full rewrite
- `pkg/logger/multisink.go` — new file
- `pkg/cmd/scafctl/root.go` — remove `go.uber.org/zap` import, update `ParseLogLevel` call
- `go.mod` / `go.sum` — remove three deps, add one

#### 1.1 `pkg/logger/multisink.go` (new)

A `logr.LogSink` that fans out to multiple sinks simultaneously. `Enabled` returns true if any child sink reports enabled (so the most verbose sink wins). All other methods delegate to every sink in order.

```go
// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package logger

import (
    "github.com/go-logr/logr"
)

// multiSink fans structured log records out to multiple logr.LogSink
// implementations simultaneously. Enabled returns true when any child
// sink is enabled so the most-verbose configured sink wins.
type multiSink struct {
    sinks []logr.LogSink
}

func newMultiSink(sinks ...logr.LogSink) *multiSink {
    return &multiSink{sinks: sinks}
}

func (m *multiSink) Init(info logr.RuntimeInfo) {
    for _, s := range m.sinks {
        s.Init(info)
    }
}

func (m *multiSink) Enabled(level int) bool {
    for _, s := range m.sinks {
        if s.Enabled(level) {
            return true
        }
    }
    return false
}

func (m *multiSink) Info(level int, msg string, keysAndValues ...any) {
    for _, s := range m.sinks {
        if s.Enabled(level) {
            s.Info(level, msg, keysAndValues...)
        }
    }
}

func (m *multiSink) Error(err error, msg string, keysAndValues ...any) {
    for _, s := range m.sinks {
        s.Error(err, msg, keysAndValues...)
    }
}

func (m *multiSink) WithValues(keysAndValues ...any) logr.LogSink {
    next := make([]logr.LogSink, len(m.sinks))
    for i, s := range m.sinks {
        next[i] = s.WithValues(keysAndValues...)
    }
    return &multiSink{sinks: next}
}

func (m *multiSink) WithName(name string) logr.LogSink {
    next := make([]logr.LogSink, len(m.sinks))
    for i, s := range m.sinks {
        next[i] = s.WithName(name)
    }
    return &multiSink{sinks: next}
}
```

#### 1.2 `pkg/logger/logger.go` — key changes

**Type changes (breaking — acceptable):**

| Before | After |
|--------|-------|
| `Options.Level zapcore.Level` | `Options.Level slog.Level` |
| `LogLevelNone = zapcore.FatalLevel + 1` | `LogLevelNone = slog.Level(math.MaxInt32)` |
| `ParseLogLevel() (zapcore.Level, error)` | `ParseLogLevel() (slog.Level, error)` |
| `IsDebugLevel()` checks `<= zapcore.Level(-1)` | checks `<= slog.Level(-1)` |
| `globalZapLogger *zap.Logger` | removed |
| `Sync()` flushes zap buffer | removed (no-op; telemetry.Shutdown handles OTel flush) |

**Level mapping in `ParseLogLevel`:**

The logr/slog bridge (`logr.FromSlogHandler`) maps logr V-levels to slog levels as `slog.Level(-v)`. We set the handler's minimum level to match:

| Named level | `slog.Level` returned | logr V-levels enabled |
|-------------|----------------------|-----------------------|
| `none` / `""` | `math.MaxInt32` | none |
| `error` | `slog.LevelError` (8) | errors only |
| `warn` | `slog.LevelWarn` (4) | warn + error |
| `info` | `slog.LevelInfo` (0) | V(0) info and above |
| `debug` | `slog.Level(-1)` | V(1) and above |
| `trace` | `slog.Level(-2)` | V(2) and above |
| `"n"` (numeric) | `slog.Level(-n)` | V(n) and above |

Note: the `-4*n` spacing used by slog's own named levels (Debug=-4, Info=0, Warn=4, Error=8) is **not** used here. The bridge maps V-level `n` → `slog.Level(-n)`, so the handler threshold must also be `-n`.

**`GetWithOptions` rewrite — replacing zap with slog + otellogr:**

```go
func GetWithOptions(opts Options) *logr.Logger {
    once.Do(func() {
        if opts.Level >= LogLevelNone {
            gl := logr.Discard()
            globalLogrLogger = &gl
            return
        }

        buildInfo, _ := debug.ReadBuildInfo()

        // ── Sink 1: slog handler for local console/file output ──────────────
        slogLevel := &slog.LevelVar{}
        slogLevel.Set(opts.Level)

        var slogWriter io.Writer = os.Stderr
        // (handle opts.FilePath / opts.AlsoStderr — same logic as current zap code
        // but writing to an io.Writer instead of zapcore.WriteSyncer)

        var slogHandler slog.Handler
        handlerOpts := &slog.HandlerOptions{
            Level:     slogLevel,
            AddSource: true,
            ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
                if a.Key == slog.TimeKey && !opts.Timestamps {
                    return slog.Attr{} // drop timestamp
                }
                if a.Key == slog.TimeKey {
                    a.Key = TimeStampKey
                }
                if a.Key == slog.MessageKey {
                    a.Key = MessageKey
                }
                return a
            },
        }
        if opts.Format == FormatJSON {
            slogHandler = slog.NewJSONHandler(slogWriter, handlerOpts)
        } else {
            slogHandler = slog.NewTextHandler(slogWriter, handlerOpts)
        }

        // Add static fields (commit, version, build time, go version)
        slogHandler = slogHandler.WithAttrs([]slog.Attr{
            slog.String(CommitKey, settings.VersionInformation.Commit),
            slog.String(VersionKey, settings.VersionInformation.BuildVersion),
            slog.String(BuildTimeKey, settings.VersionInformation.BuildTime),
            slog.String(GoVersionKey, buildInfo.GoVersion),
        })

        consoleSink := logr.FromSlogHandler(slogHandler).GetSink()

        // ── Sink 2: otellogr forwarding to global OTel LoggerProvider ────────
        // Uses log/global.GetLoggerProvider() which returns the provider set by
        // telemetry.Setup(). If Setup has not been called, this is the noop provider
        // and nothing is exported — safe for unit tests.
        otelSink := otellogr.NewLogSink(settings.CliBinaryName,
            otellogr.WithLoggerProvider(logGlobal.GetLoggerProvider()),
        )

        gl := logr.New(newMultiSink(consoleSink, otelSink))
        globalLogrLogger = &gl
    })
    if globalLogrLogger == nil {
        return &defaultNoopLogger
    }
    return globalLogrLogger
}
```

**Imports after rewrite:**

```go
import (
    "context"
    "fmt"
    "io"
    "log/slog"
    "math"
    "os"
    "runtime/debug"
    "strconv"
    "strings"
    "sync"

    "github.com/go-logr/logr"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "go.opentelemetry.io/contrib/bridges/otellogr"
    logGlobal "go.opentelemetry.io/otel/log/global"
)
```

#### 1.3 `pkg/cmd/scafctl/root.go` — changes

- Remove `"go.uber.org/zap"` import
- Update variable name: `zapLevel` → `logLevel` (type `slog.Level`)
- Remove `//nolint:gosec` comment that was needed for the `int8` zap cast
- The `logger.Options{Level: logLevel, ...}` call is otherwise identical

Before:
```go
zapLevel, parseErr := logger.ParseLogLevel(resolvedLogLevel)
// ...
logOpts := logger.Options{
    Level: zapLevel,
    // ...
}
```

After:
```go
logLevel, parseErr := logger.ParseLogLevel(resolvedLogLevel)
// ...
logOpts := logger.Options{
    Level: logLevel,
    // ...
}
```

Any `zap.Field`-style calls (e.g., `zap.String("key", value)`) become standard logr key-value pairs:
```go
// Before
lgr.Error(err, "failed", zap.String("path", p))
// After
lgr.Error(err, "failed", "path", p)
```

#### 1.4 `go.mod` changes

Remove:
```
github.com/go-logr/zapr v1.3.0
go.uber.org/zap v1.27.1
go.uber.org/multierr v1.11.0  // will be pruned by go mod tidy
```

Add:
```
go.opentelemetry.io/contrib/bridges/otellogr v0.15.0
```

The `otel/log` and `otel/log/global` APIs are pulled in transitively via the bridge. They are also added explicitly in Phase 2.

**Verification:**
```bash
go build ./...
grep -r "go.uber.org/zap" .    # must be empty
go test ./pkg/logger/...
```

---

### Phase 2 — OTel SDK initialization (`pkg/telemetry`)

**New package:** `pkg/telemetry/`  
**Affected files:** `pkg/cmd/scafctl/root.go`

This package owns all OTel provider construction and is the single place that imports `otel/sdk/*`. Application packages import only the API packages (`otel/trace`, `otel/metric`, `otel/log`) or use the global accessors via `otel.Tracer()`, `otel.Meter()`.

#### 2.1 `pkg/telemetry/telemetry.go`

```go
package telemetry

import (
    "context"
    "os"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
    "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
    logGlobal "go.opentelemetry.io/otel/log/global"
    sdklog "go.opentelemetry.io/otel/sdk/log"
    sdkresource "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
    // metrics provider added in Phase 3
)

// Options configures the OTel SDK setup.
type Options struct {
    // ServiceName is the OTel resource service.name attribute.
    // Defaults to settings.CliBinaryName.
    ServiceName string
    // ServiceVersion is the OTel resource service.version attribute.
    // Defaults to settings.VersionInformation.BuildVersion.
    ServiceVersion string
    // ExporterEndpoint overrides the OTEL_EXPORTER_OTLP_ENDPOINT environment variable.
    // When empty, the env var is used. When both are empty no OTLP export occurs.
    ExporterEndpoint string
    // ExporterInsecure disables TLS for the OTLP gRPC connection. Useful for local
    // development against an OTel collector without TLS configured.
    ExporterInsecure bool
}

// Setup initializes the OTel TracerProvider, MeterProvider (Phase 3), and
// LoggerProvider and registers them globally. It must be called before
// logger.GetWithOptions so the otellogr bridge picks up the real provider.
//
// The returned shutdown function must be called before process exit to flush
// buffered telemetry. Typical usage:
//
//   shutdown, err := telemetry.Setup(ctx, opts)
//   if err != nil { /* handle */ }
//   defer func() { _ = shutdown(ctx) }()
func Setup(ctx context.Context, opts Options) (shutdown func(context.Context) error, err error) {
    // ...
}
```

**Resource construction** — shared across all providers:

```go
res, err := sdkresource.New(ctx,
    sdkresource.WithAttributes(
        semconv.ServiceName(opts.ServiceName),
        semconv.ServiceVersion(opts.ServiceVersion),
        attribute.String(logger.CommitKey, settings.VersionInformation.Commit),
        attribute.String(logger.BuildTimeKey, settings.VersionInformation.BuildTime),
    ),
    sdkresource.WithOS(),
    sdkresource.WithHost(),
)
```

**Trace provider:**

When `OTEL_EXPORTER_OTLP_ENDPOINT` or `opts.ExporterEndpoint` is set:
```go
traceExporter, err := otlptracegrpc.New(ctx,
    otlptracegrpc.WithEndpoint(endpoint),
    // otlptracegrpc.WithInsecure() when opts.ExporterInsecure
)
```

Otherwise, write JSON to stderr for local visibility:
```go
traceExporter, err := stdouttrace.New(
    stdouttrace.WithWriter(os.Stderr),
    stdouttrace.WithPrettyPrint(),
)
```

```go
tp := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(traceExporter),
    sdktrace.WithResource(res),
    sdktrace.WithSampler(sdktrace.AlwaysSample()), // configurable later
)
otel.SetTracerProvider(tp)
```

**Log provider:**

```go
var processors []sdklog.Processor

// Always: stdout/stderr for local visibility
stdoutLogExp, _ := stdoutlog.New(stdoutlog.WithWriter(os.Stderr))
processors = append(processors, sdklog.NewSimpleProcessor(stdoutLogExp))

// When OTLP endpoint is configured:
if endpoint != "" {
    otlpLogExp, err := otlploggrpc.New(ctx,
        otlploggrpc.WithEndpoint(endpoint),
        // otlploggrpc.WithInsecure() when opts.ExporterInsecure
    )
    processors = append(processors, sdklog.NewBatchProcessor(otlpLogExp))
}

lp := sdklog.NewLoggerProvider(
    sdklog.WithResource(res),
    // add all processors
)
for _, p := range processors { /* lp constructed with each */ }
logGlobal.SetLoggerProvider(lp)
```

**Shutdown function:**

```go
return func(ctx context.Context) error {
    var errs []error
    if err := lp.Shutdown(ctx); err != nil { errs = append(errs, err) }
    if err := tp.Shutdown(ctx); err != nil { errs = append(errs, err) }
    // mp.Shutdown added in Phase 3
    return errors.Join(errs...)
}, nil
```

#### 2.2 `pkg/telemetry/tracer.go`

```go
package telemetry

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
)

const (
    TracerProvider    = "github.com/oakwood-commons/scafctl"
    TracerHTTPClient  = TracerProvider + "/httpc"
    TracerProvider_   = TracerProvider + "/provider"
    TracerResolver    = TracerProvider + "/resolver"
    TracerSolution    = TracerProvider + "/solution"
    TracerAction      = TracerProvider + "/action"
    TracerMCP         = TracerProvider + "/mcp"
)

// Tracer returns a named tracer from the global TracerProvider.
func Tracer(name string) trace.Tracer {
    return otel.Tracer(name)
}
```

#### 2.3 `pkg/cmd/scafctl/root.go` — `PersistentPreRun` update

**Order of operations is critical:** `telemetry.Setup` must run before `logger.GetWithOptions` so that when the `otellogr` sink calls `logGlobal.GetLoggerProvider()` it gets the real SDK provider, not the noop default.

```go
PersistentPreRun: func(cCmd *cobra.Command, args []string) {
    // 1. Load config
    mgr := config.NewManager(configPath)
    cfg, _ := mgr.Load()

    // 2. Resolve OTLP endpoint (flag > env)
    otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    if cCmd.Flags().Changed("otel-endpoint") {
        otelEndpoint, _ = cCmd.Flags().GetString("otel-endpoint")
    }

    // 3. Setup OTel providers (MUST be before logger init)
    telShutdown, err := telemetry.Setup(context.Background(), telemetry.Options{
        ServiceName:      settings.CliBinaryName,
        ServiceVersion:   settings.VersionInformation.BuildVersion,
        ExporterEndpoint: otelEndpoint,
        ExporterInsecure: otelInsecureFlag,
    })
    if err != nil {
        _, _ = ioStreams.ErrOut.Write([]byte("Warning: failed to initialize telemetry: " + err.Error() + "\n"))
    }

    // 4. Initialize logger (otellogr sink now picks up real LoggerProvider)
    logLevel, parseErr := logger.ParseLogLevel(resolvedLogLevel)
    lgr := logger.GetWithOptions(logger.Options{Level: logLevel, ...})

    // 5. Defer telemetry shutdown (after cobra command tree completes)
    cCmd.PostRun = func(_ *cobra.Command, _ []string) {
        if telShutdown != nil {
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            _ = telShutdown(ctx)
        }
    }

    // ... rest unchanged
},
```

New flags added to the root command:
```go
cCmd.PersistentFlags().String("otel-endpoint", "", "OpenTelemetry OTLP exporter endpoint (e.g. localhost:4317). Overrides OTEL_EXPORTER_OTLP_ENDPOINT")
cCmd.PersistentFlags().Bool("otel-insecure", false, "Disable TLS for OTLP gRPC connection (development only)")
```

#### 2.4 `go.mod` additions (Phase 2)

```
go.opentelemetry.io/otel v1.40.0
go.opentelemetry.io/otel/sdk v1.40.0
go.opentelemetry.io/otel/trace v1.40.0
go.opentelemetry.io/otel/log v0.16.0
go.opentelemetry.io/otel/log/global v0.16.0
go.opentelemetry.io/otel/sdk/log v0.16.0
go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.40.0
go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.16.0
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.40.0
go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.16.0
go.opentelemetry.io/otel/semconv/v1.27.0 v1.40.0
```

**Verification:**
```bash
go build ./...
go test ./pkg/telemetry/...
# Start a local OTel collector or use the stdout exporter:
go run ./cmd/scafctl/... version  # should emit a trace span to stderr
```

---

### Phase 3 — Migrate metrics to otel SDK + Prometheus exporter

**Affected files:** `pkg/metrics/metrics.go`, `pkg/telemetry/telemetry.go`

#### 3.1 `pkg/metrics/metrics.go` — full rewrite

Replace `prometheus.CounterVec` etc. with `metric.Float64Counter` etc. The `RegisterMetrics()` function becomes `InitMetrics()` and uses `otel.GetMeterProvider().Meter(settings.CliBinaryName)`.

**Instrument mapping:**

| Current Prometheus instrument | OTel instrument | OTel method |
|-------------------------------|-----------------|-------------|
| `prometheus.CounterVec` | `metric.Float64Counter` | `.Add(ctx, 1, metric.WithAttributes(...))` |
| `prometheus.HistogramVec` | `metric.Float64Histogram` | `.Record(ctx, val, metric.WithAttributes(...))` |
| `prometheus.GaugeVec` | `metric.Float64UpDownCounter` | `.Add(ctx, delta, ...)` |
| `prometheus.Gauge` | `metric.Float64ObservableGauge` (or `Float64UpDownCounter`) | `.Record(ctx, val)` |

Example — `ProviderExecutionTotal` before:
```go
ProviderExecutionTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: fmt.Sprintf("%s_provider_execution_total", settings.CliBinaryName),
    Help: "Total number of provider executions",
}, []string{providerNameLabel, statusLabel})
```

After:
```go
var ProviderExecutionTotal metric.Float64Counter

func InitMetrics() {
    m := otel.GetMeterProvider().Meter(settings.CliBinaryName)
    var err error
    ProviderExecutionTotal, err = m.Float64Counter(
        fmt.Sprintf("%s_provider_execution_total", settings.CliBinaryName),
        metric.WithDescription("Total number of provider executions"),
    )
    if err != nil { /* log warning */ }
    // ... all other instruments
}
```

`RecordProviderExecution` after:
```go
func RecordProviderExecution(providerName string, duration float64, success bool) {
    status := "success"
    if !success {
        status = "failure"
    }
    attrs := metric.WithAttributes(
        attribute.String(providerNameLabel, providerName),
        attribute.String(statusLabel, status),
    )
    ProviderExecutionDuration.Record(context.Background(), duration, attrs)
    ProviderExecutionTotal.Add(context.Background(), 1, attrs)
}
```

`Handler()` returns the Prometheus registry HTTP handler provided by the OTel Prometheus exporter (the exporter is created in `telemetry.Setup` and the registry is stored for this use).

`PrometheusMiddleware` continues to record HTTP metrics using the new OTel counter.

#### 3.2 `pkg/telemetry/telemetry.go` — add MeterProvider

```go
import (
    prometheusexporter "go.opentelemetry.io/otel/exporters/prometheus"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// In Setup():
promExporter, err := prometheusexporter.New()
mp := sdkmetric.NewMeterProvider(
    sdkmetric.WithReader(promExporter),
    sdkmetric.WithResource(res),
)
otel.SetMeterProvider(mp)
```

The `prometheusexporter` uses the default Prometheus registry by default, so `promhttp.Handler()` in `metrics.Handler()` continues to serve all OTel metrics at `/metrics` with no additional configuration.

#### 3.3 `go.mod` additions (Phase 3)

```
go.opentelemetry.io/otel/metric v1.40.0
go.opentelemetry.io/otel/sdk/metric v1.40.0
go.opentelemetry.io/otel/exporters/prometheus v0.62.0
```

`github.com/prometheus/client_golang` remains in `go.mod` as an indirect dependency of the OTel Prometheus exporter. It is no longer imported directly by `pkg/metrics`.

**Verification:**
```bash
go build ./...
curl http://localhost:<mcp-port>/metrics   # or start the MCP server and scrape
grep "scafctl_provider_execution_total" <output>
```

---

### Phase 4 — Trace instrumentation

Add spans at key execution boundaries. All instrumentation follows the same pattern:

```go
ctx, span := telemetry.Tracer(telemetry.TracerProvider_).Start(ctx, "provider.Execute",
    trace.WithAttributes(
        attribute.String("provider.name", name),
        attribute.String("provider.type", providerType),
    ),
)
defer span.End()

// On error:
span.RecordError(err)
span.SetStatus(codes.Error, err.Error())
```

#### Instrumentation points

| Package | Tracer constant | Span name | Key attributes |
|---------|-----------------|-----------|----------------|
| `pkg/httpc` | `TracerHTTPClient` | auto via `otelhttp.NewTransport` | `http.method`, `http.url`, `http.status_code` |
| `pkg/provider` | `TracerProvider_` | `provider.Execute` | `provider.name`, `provider.type` |
| `pkg/resolver` | `TracerResolver` | `resolver.Evaluate` | `resolver.name`, `resolver.type` |
| `pkg/solution` | `TracerSolution` | `solution.Run` | `solution.path` |
| `pkg/action` | `TracerAction` | `action.Execute` | `action.name`, `action.step` |
| `pkg/mcp` | `TracerMCP` | `mcp.Handle` | `mcp.tool`, `mcp.method` |

#### HTTP client auto-instrumentation

In `pkg/httpc`, wrap the `http.Transport` or `http.Client`:

```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

transport := otelhttp.NewTransport(http.DefaultTransport)
client := &http.Client{Transport: transport}
```

This adds W3C Trace Context propagation headers to outgoing requests and creates a span per request automatically.

#### `go.mod` additions (Phase 4)

```
go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.15.0
go.opentelemetry.io/otel/codes v1.40.0
go.opentelemetry.io/otel/propagation v1.40.0
```

---

### Phase 5 — Documentation, examples, and integration tests

#### Documentation

- Update [docs/tutorials/logging-tutorial.md](../tutorials/logging-tutorial.md): document the new log level flags, the slog text format, and how trace/span IDs appear in log records when a trace is active
- Create `docs/tutorials/telemetry-tutorial.md`: covers all three signals, configuring `--otel-endpoint`, running a local OTel collector with Jaeger, viewing traces, interpreting stdout trace output
- Update [docs/design/providers.md](providers.md) with trace span naming conventions
- Update [docs/design/resolvers.md](resolvers.md) with resolver span attributes

#### Examples

Create `examples/telemetry/`:
- `collector-config.yaml` — minimal OTel Collector config (receivers: OTLP; exporters: Jaeger, Prometheus; pipelines for all three signals)
- `docker-compose.yaml` — Jaeger all-in-one + OTel Collector for local development
- `README.md` — how to run locally and issue a traced scafctl command

#### Integration tests

In [tests/integration/cli_test.go](../../tests/integration/cli_test.go):
- Verify `--log-level debug` flag still produces debug output after the slog migration
- Verify `--log-level 3` numeric V-level still works
- Verify `--otel-endpoint` flag is registered on the root command
- Verify `--otel-insecure` flag is registered on the root command

In `tests/integration/solutions/telemetry/`:
- Solution that triggers provider execution and verifies `scafctl_provider_execution_total` increments

---

## Dependency Changes Summary

### Removed

| Module | Reason |
|--------|--------|
| `github.com/go-logr/zapr v1.3.0` | Replaced by otellogr bridge |
| `go.uber.org/zap v1.27.1` | Replaced by slog + OTel |
| `go.uber.org/multierr v1.11.0` | Transitive dep of zap — removed with it |

### Added

| Module | Version | Phase | Use |
|--------|---------|-------|-----|
| `go.opentelemetry.io/contrib/bridges/otellogr` | v0.15.0 | 1 | logr→OTel log bridge |
| `go.opentelemetry.io/otel` | v1.40.0 | 2 | Root API, global setters |
| `go.opentelemetry.io/otel/sdk` | v1.40.0 | 2 | SDK base |
| `go.opentelemetry.io/otel/trace` | v1.40.0 | 2 | Trace API |
| `go.opentelemetry.io/otel/log` | v0.16.0 | 2 | Log API (Beta) |
| `go.opentelemetry.io/otel/log/global` | v0.16.0 | 2 | Global log provider |
| `go.opentelemetry.io/otel/sdk/log` | v0.16.0 | 2 | Log SDK (Beta) |
| `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` | v1.40.0 | 2 | Local trace output |
| `go.opentelemetry.io/otel/exporters/stdout/stdoutlog` | v0.16.0 | 2 | Local log output |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.40.0 | 2 | OTLP trace export |
| `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc` | v0.16.0 | 2 | OTLP log export |
| `go.opentelemetry.io/otel/semconv/v1.27.0` | v1.40.0 | 2 | Resource semantic conventions |
| `go.opentelemetry.io/otel/metric` | v1.40.0 | 3 | Metric API |
| `go.opentelemetry.io/otel/sdk/metric` | v1.40.0 | 3 | Metric SDK |
| `go.opentelemetry.io/otel/exporters/prometheus` | v0.62.0 | 3 | Prometheus metric export |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v0.15.0 | 4 | HTTP auto-instrumentation |
| `go.opentelemetry.io/otel/codes` | v1.40.0 | 4 | Span status codes |
| `go.opentelemetry.io/otel/propagation` | v1.40.0 | 4 | W3C trace context propagation |

---

## Breaking Changes

| Change | Impact | Migration |
|--------|--------|-----------|
| `logger.Options.Level` type: `zapcore.Level` → `slog.Level` | Any code constructing `logger.Options` directly | Change type; numeric values shift but `ParseLogLevel()` handles it |
| `logger.ParseLogLevel()` return type: `zapcore.Level` → `slog.Level` | `root.go` and any direct callers | Update variable type; remove `zap` import |
| `logger.LogLevelNone` type: `zapcore.Level` → `slog.Level` | Direct comparisons against `LogLevelNone` | Update comparisons |
| `logger.Sync()` removed | `defer logger.Sync()` calls | Remove; use `defer telemetry.Shutdown()` instead |
| `logger.Get(logLevel int8)` parameter semantics | Any caller using raw `int8` values | Use named levels via `ParseLogLevel()` or `slog.Level` constants |
| `pkg/metrics` exported vars change type | Any code calling `.With(prometheus.Labels{...})` | Update to `metric.WithAttributes(attribute.String(...))` |

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| otel/log SDK API breaking change (Beta) | Medium | Low | Isolated to `pkg/logger` and `pkg/telemetry`; fix is contained |
| otellogr bridge behavior change (v0.x) | Low | Low | Unit tests on `pkg/logger` catch regressions |
| Performance regression (OTel overhead vs zap) | Low | Low | zap was unnecessary for a CLI; slog is fast enough; BatchProcessor is async |
| Prometheus metric names change | None | None | OTel Prometheus exporter preserves metric names using the instrument name directly |
| Init ordering bug (logger before telemetry) | Medium | High | Enforced by calling `telemetry.Setup` before `logger.GetWithOptions` in `PersistentPreRun`; unit tests for noop fallback |

---

## Testing Strategy

### Unit tests

- `pkg/logger/logger_test.go`: test `ParseLogLevel` with all named and numeric levels; test `GetWithOptions` with noop OTel provider (default when `telemetry.Setup` not called)
- `pkg/logger/multisink_test.go`: test fan-out, Enabled logic, WithValues/WithName propagation
- `pkg/telemetry/telemetry_test.go`: test `Setup` returns without error; test `shutdown` flushes cleanly; test with and without OTLP endpoint

### Integration tests

- `tests/integration/cli_test.go`: all existing log level tests must pass unchanged
- New: `--otel-endpoint` and `--otel-insecure` flags are registered

### Manual verification

```bash
# Phase 1 — confirm zap is removed
go build ./...
grep -r "go.uber.org/zap" $(go list -f '{{.Dir}}' ./...) # must find nothing

# Phase 2 — stdout trace output (no collector required)
unset OTEL_EXPORTER_OTLP_ENDPOINT
go run ./cmd/scafctl/... --log-level debug version 2>&1 | grep -A5 '"Name":"scafctl'

# Phase 2 — OTLP export
docker run -p 4317:4317 -p 16686:16686 jaegertracing/all-in-one:latest
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 go run ./cmd/scafctl/... version
# Open http://localhost:16686 — service "scafctl" should appear

# Phase 3 — Prometheus metrics
# Run a command that triggers provider execution, then:
curl http://localhost:<port>/metrics | grep scafctl_provider_execution_total

# Phase 4 — trace spans with HTTP calls
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 go run ./cmd/scafctl/... run -f examples/...
# Verify nested spans in Jaeger UI
```
