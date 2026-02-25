// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package telemetry owns all OpenTelemetry provider construction and is
// the single place in the binary that imports otel/sdk/* packages.
// Application packages import only the API packages (otel/trace, otel/metric,
// otel/log) or use the global accessors via otel.Tracer(), otel.Meter().
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	logGlobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// Options configures the OTel SDK setup.
type Options struct {
	// ServiceName is the OTel resource service.name attribute.
	// Defaults to settings.CliBinaryName when empty.
	ServiceName string
	// ServiceVersion is the OTel resource service.version attribute.
	// Defaults to settings.VersionInformation.BuildVersion when empty.
	ServiceVersion string
	// ExporterEndpoint is the OTLP gRPC endpoint (e.g. "localhost:4317").
	// Overrides the OTEL_EXPORTER_OTLP_ENDPOINT environment variable.
	// When empty and the env var is also unset, tracing is disabled (noop).
	ExporterEndpoint string
	// ExporterInsecure disables TLS for the OTLP gRPC connection.
	// Useful for local development against an OTel collector without TLS.
	ExporterInsecure bool
	// SamplerType controls the trace sampler. Supported values:
	//   always_on  — sample every span (default)
	//   always_off — drop all spans
	//   traceidratio — sample a fraction of spans (controlled by SamplerArg)
	SamplerType string
	// SamplerArg is the argument passed to the sampler.
	// For traceidratio this is the sampling ratio (0.0–1.0). Defaults to 1.0.
	SamplerArg float64
}

// Setup initializes the OTel TracerProvider and LoggerProvider and registers
// them globally. It must be called before logger.GetWithOptions so that the
// otellogr bridge picks up the real SDK provider rather than the noop default.
//
// The returned shutdown function flushes buffered telemetry and must be called
// before process exit. Typical usage:
//
//	shutdown, err := telemetry.Setup(ctx, opts)
//	if err != nil { /* handle */ }
//	defer func() { _ = shutdown(ctx) }()
func Setup(ctx context.Context, opts Options) (shutdown func(context.Context) error, err error) {
	if opts.ServiceName == "" {
		opts.ServiceName = settings.CliBinaryName
	}
	if opts.ServiceVersion == "" {
		opts.ServiceVersion = settings.VersionInformation.BuildVersion
	}

	// Resolve OTLP endpoint: option > env var.
	endpoint := opts.ExporterEndpoint
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}

	// ── Shared resource ───────────────────────────────────────────────────────
	res, resErr := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceName(opts.ServiceName),
			semconv.ServiceVersion(opts.ServiceVersion),
			attribute.String(logger.CommitKey, settings.VersionInformation.Commit),
			attribute.String(logger.BuildTimeKey, settings.VersionInformation.BuildTime),
		),
		sdkresource.WithOS(),
		sdkresource.WithHost(),
	)
	if resErr != nil {
		// Merge error is non-fatal; fall back to partial resource.
		res = sdkresource.Default()
	}

	// ── Trace provider ────────────────────────────────────────────────────────
	sampler, samplerErr := parseSampler(opts.SamplerType, opts.SamplerArg)
	if samplerErr != nil {
		return nil, samplerErr
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	}
	if endpoint != "" {
		dialOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
		if opts.ExporterInsecure {
			dialOpts = append(dialOpts, otlptracegrpc.WithInsecure())
		}
		traceExporter, traceErr := otlptracegrpc.New(ctx, dialOpts...)
		if traceErr != nil {
			return nil, traceErr
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(traceExporter))
	}
	// When no endpoint is configured, the TracerProvider is created without an
	// exporter. Spans are still recorded in-process (propagation works) but
	// nothing is exported — no stderr noise.

	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)

	// Register W3C TraceContext + Baggage propagators so otelhttp and other
	// instrumentation can inject/extract trace context headers automatically.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// ── Log provider ──────────────────────────────────────────────────────────
	// Only create OTel log processors when an OTLP endpoint is configured.
	// Without an endpoint, the slog/logr logger handles all log output; no
	// stderr fallback exporter is needed.
	var logProcessors []sdklog.Processor
	if endpoint != "" {
		logDialOpts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(endpoint)}
		if opts.ExporterInsecure {
			logDialOpts = append(logDialOpts, otlploggrpc.WithInsecure())
		}
		otlpLogExp, otlpLogErr := otlploggrpc.New(ctx, logDialOpts...)
		if otlpLogErr == nil {
			logProcessors = append(logProcessors, sdklog.NewBatchProcessor(otlpLogExp))
		}
	}

	lpOpts := []sdklog.LoggerProviderOption{sdklog.WithResource(res)}
	for _, p := range logProcessors {
		lpOpts = append(lpOpts, sdklog.WithProcessor(p))
	}
	lp := sdklog.NewLoggerProvider(lpOpts...)
	logGlobal.SetLoggerProvider(lp)

	// ── Metric provider ──────────────────────────────────────────────────────
	var metricReaders []sdkmetric.Reader

	// Always add a Prometheus bridge exporter so /metrics is scrape-able.
	promExp, promErr := prometheus.New()
	if promErr == nil {
		metricReaders = append(metricReaders, promExp)
	}

	if endpoint != "" {
		metricDialOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(endpoint)}
		if opts.ExporterInsecure {
			metricDialOpts = append(metricDialOpts, otlpmetricgrpc.WithInsecure())
		}
		otlpMetricExp, otlpMetricErr := otlpmetricgrpc.New(ctx, metricDialOpts...)
		if otlpMetricErr == nil {
			metricReaders = append(metricReaders, sdkmetric.NewPeriodicReader(otlpMetricExp))
		}
	}

	mpOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	for _, r := range metricReaders {
		mpOpts = append(mpOpts, sdkmetric.WithReader(r))
	}
	mp := sdkmetric.NewMeterProvider(mpOpts...)
	otel.SetMeterProvider(mp)

	// ── Shutdown ──────────────────────────────────────────────────────────────
	return func(ctx context.Context) error {
		var errs []error
		if e := mp.Shutdown(ctx); e != nil {
			errs = append(errs, e)
		}
		if e := lp.Shutdown(ctx); e != nil {
			errs = append(errs, e)
		}
		if e := tp.Shutdown(ctx); e != nil {
			errs = append(errs, e)
		}
		return errors.Join(errs...)
	}, nil
}

// parseSampler converts a sampler type string + argument into an sdktrace.Sampler.
// Recognised types: always_on (default), always_off, traceidratio.
func parseSampler(samplerType string, samplerArg float64) (sdktrace.Sampler, error) {
	switch strings.ToLower(strings.TrimSpace(samplerType)) {
	case "", "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "traceidratio":
		if samplerArg < 0 || samplerArg > 1 {
			return nil, fmt.Errorf("traceidratio sampler arg must be between 0.0 and 1.0, got %f", samplerArg)
		}
		return sdktrace.TraceIDRatioBased(samplerArg), nil
	default:
		return nil, fmt.Errorf("unknown sampler type %q (valid: always_on, always_off, traceidratio)", samplerType)
	}
}
