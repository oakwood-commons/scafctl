// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package metrics provides OpenTelemetry metric instruments for the scafctl application.
// All instruments are initialized by calling InitMetrics after telemetry.Setup so that
// the real SDK MeterProvider is in place.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// Attribute key constants used as OTel metric labels.
const (
	AttrStatusCode    = "status_code"
	AttrURL           = "url"
	AttrPath          = "path"
	AttrMethod        = "method"
	AttrErrorType     = "error_type"
	LabelHost         = "host"
	LabelPathTemplate = "path_template"
	AttrProviderName  = "provider_name"
	AttrStatus        = "status"
	AttrPluginName    = "plugin_name"
	AttrPluginSource  = "source"
)

// requestTimesBuckets are the histogram bucket boundaries used for duration metrics.
var requestTimesBuckets = []float64{.1, .2, .4, .6, .8, 1, 2.5, 5, 10, 20}

// backing stores for observable gauge instruments
var (
	cacheSizeBytesVal    atomic.Int64
	circuitBreakerStates sync.Map // map[string]float64
)

// OTel metric instruments (initialised by InitMetrics).
var (
	HTTPClientDuration       metric.Float64Histogram
	HTTPClientRequestsTotal  metric.Int64Counter
	HTTPClientErrorsTotal    metric.Int64Counter
	HTTPClientRetriesTotal   metric.Int64Counter
	HTTPClientCacheHits      metric.Int64Counter
	HTTPClientCacheMisses    metric.Int64Counter
	HTTPClientRequestSize    metric.Float64Histogram
	HTTPClientResponseSize   metric.Float64Histogram
	GetSolutionTimeHistogram metric.Float64Histogram

	ProviderExecutionDuration metric.Float64Histogram
	ProviderExecutionTotal    metric.Int64Counter

	PluginResolutionDuration metric.Float64Histogram
	PluginResolutionTotal    metric.Int64Counter
)

// private instruments
var (
	httpRequestsTotal             metric.Int64Counter
	httpClientConcurrentRequests  metric.Int64UpDownCounter
	httpClientCacheSizeBytes      metric.Float64ObservableGauge
	httpClientCircuitBreakerState metric.Float64ObservableGauge
)

var initOnce sync.Once

// InitMetrics initialises all OTel metric instruments from the global MeterProvider.
// Must be called after telemetry.Setup. Safe to call multiple times.
func InitMetrics(_ context.Context) error {
	var initErr error
	initOnce.Do(func() {
		m := otel.GetMeterProvider().Meter(settings.CliBinaryName)
		n := settings.CliBinaryName
		var err error

		httpRequestsTotal, err = m.Int64Counter(fmt.Sprintf("%s_http_requests_total", n),
			metric.WithDescription("Number of HTTP requests"))
		if err != nil {
			initErr = fmt.Errorf("httpRequestsTotal: %w", err)
			return
		}

		httpClientConcurrentRequests, err = m.Int64UpDownCounter(fmt.Sprintf("%s_http_client_concurrent_requests", n),
			metric.WithDescription("Current number of concurrent HTTP client requests"))
		if err != nil {
			initErr = fmt.Errorf("httpClientConcurrentRequests: %w", err)
			return
		}

		httpClientCacheSizeBytes, err = m.Float64ObservableGauge(fmt.Sprintf("%s_http_client_cache_size_bytes", n),
			metric.WithDescription("Total size of HTTP client cache in bytes"),
			metric.WithUnit("By"))
		if err != nil {
			initErr = fmt.Errorf("httpClientCacheSizeBytes: %w", err)
			return
		}
		if _, regErr := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
			o.ObserveFloat64(httpClientCacheSizeBytes, float64(cacheSizeBytesVal.Load()))
			return nil
		}, httpClientCacheSizeBytes); regErr != nil {
			initErr = fmt.Errorf("register httpClientCacheSizeBytes callback: %w", regErr)
			return
		}

		httpClientCircuitBreakerState, err = m.Float64ObservableGauge(fmt.Sprintf("%s_http_client_circuit_breaker_state", n),
			metric.WithDescription("Circuit breaker state: 0=closed, 1=open, 2=half-open"))
		if err != nil {
			initErr = fmt.Errorf("httpClientCircuitBreakerState: %w", err)
			return
		}
		if _, regErr := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
			circuitBreakerStates.Range(func(key, val any) bool {
				host, _ := key.(string)
				state, _ := val.(float64)
				o.ObserveFloat64(httpClientCircuitBreakerState, state,
					metric.WithAttributes(attribute.String(LabelHost, host)))
				return true
			})
			return nil
		}, httpClientCircuitBreakerState); regErr != nil {
			initErr = fmt.Errorf("register httpClientCircuitBreakerState callback: %w", regErr)
			return
		}

		HTTPClientDuration, err = m.Float64Histogram(fmt.Sprintf("%s_http_client_duration_seconds", n),
			metric.WithDescription("HTTP client request duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(requestTimesBuckets...))
		if err != nil {
			initErr = fmt.Errorf("HTTPClientDuration: %w", err)
			return
		}

		HTTPClientRequestsTotal, err = m.Int64Counter(fmt.Sprintf("%s_http_client_requests_total", n),
			metric.WithDescription("Total number of HTTP client requests"))
		if err != nil {
			initErr = fmt.Errorf("HTTPClientRequestsTotal: %w", err)
			return
		}

		HTTPClientErrorsTotal, err = m.Int64Counter(fmt.Sprintf("%s_http_client_errors_total", n),
			metric.WithDescription("Total number of HTTP client errors by type"))
		if err != nil {
			initErr = fmt.Errorf("HTTPClientErrorsTotal: %w", err)
			return
		}

		HTTPClientRetriesTotal, err = m.Int64Counter(fmt.Sprintf("%s_http_client_retries_total", n),
			metric.WithDescription("Total number of HTTP client retry attempts"))
		if err != nil {
			initErr = fmt.Errorf("HTTPClientRetriesTotal: %w", err)
			return
		}

		HTTPClientCacheHits, err = m.Int64Counter(fmt.Sprintf("%s_http_client_cache_hits_total", n),
			metric.WithDescription("Total number of HTTP cache hits"))
		if err != nil {
			initErr = fmt.Errorf("HTTPClientCacheHits: %w", err)
			return
		}

		HTTPClientCacheMisses, err = m.Int64Counter(fmt.Sprintf("%s_http_client_cache_misses_total", n),
			metric.WithDescription("Total number of HTTP cache misses"))
		if err != nil {
			initErr = fmt.Errorf("HTTPClientCacheMisses: %w", err)
			return
		}

		HTTPClientRequestSize, err = m.Float64Histogram(fmt.Sprintf("%s_http_client_request_size_bytes", n),
			metric.WithDescription("HTTP client request body size in bytes"),
			metric.WithUnit("By"),
			metric.WithExplicitBucketBoundaries(100, 1000, 10000, 100000, 1000000, 10000000))
		if err != nil {
			initErr = fmt.Errorf("HTTPClientRequestSize: %w", err)
			return
		}

		HTTPClientResponseSize, err = m.Float64Histogram(fmt.Sprintf("%s_http_client_response_size_bytes", n),
			metric.WithDescription("HTTP client response body size in bytes"),
			metric.WithUnit("By"),
			metric.WithExplicitBucketBoundaries(100, 1000, 10000, 100000, 1000000, 10000000))
		if err != nil {
			initErr = fmt.Errorf("HTTPClientResponseSize: %w", err)
			return
		}

		GetSolutionTimeHistogram, err = m.Float64Histogram(fmt.Sprintf("%s_get_solution_duration_seconds", n),
			metric.WithDescription("Histogram of the time it takes to get a solution in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(requestTimesBuckets...))
		if err != nil {
			initErr = fmt.Errorf("GetSolutionTimeHistogram: %w", err)
			return
		}

		ProviderExecutionDuration, err = m.Float64Histogram(fmt.Sprintf("%s_provider_execution_duration_seconds", n),
			metric.WithDescription("Provider execution duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(requestTimesBuckets...))
		if err != nil {
			initErr = fmt.Errorf("ProviderExecutionDuration: %w", err)
			return
		}

		ProviderExecutionTotal, err = m.Int64Counter(fmt.Sprintf("%s_provider_execution_total", n),
			metric.WithDescription("Total number of provider executions"))
		if err != nil {
			initErr = fmt.Errorf("ProviderExecutionTotal: %w", err)
			return
		}

		PluginResolutionDuration, err = m.Float64Histogram(fmt.Sprintf("%s_plugin_resolution_duration_seconds", n),
			metric.WithDescription("Plugin resolution duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(requestTimesBuckets...))
		if err != nil {
			initErr = fmt.Errorf("PluginResolutionDuration: %w", err)
			return
		}

		PluginResolutionTotal, err = m.Int64Counter(fmt.Sprintf("%s_plugin_resolution_total", n),
			metric.WithDescription("Total number of plugin resolutions"))
		if err != nil {
			initErr = fmt.Errorf("PluginResolutionTotal: %w", err)
			return
		}
	})
	return initErr
}

// SetCacheSizeBytes updates the observable gauge backing value for HTTP client cache size.
func SetCacheSizeBytes(bytes int64) { cacheSizeBytesVal.Store(bytes) }

// SetCircuitBreakerState stores the circuit breaker state for a host.
// state: 0=closed, 1=open, 2=half-open.
func SetCircuitBreakerState(host string, state float64) { circuitBreakerStates.Store(host, state) }

// IncConcurrentRequests increments the in-flight HTTP request counter.
func IncConcurrentRequests(ctx context.Context) {
	if httpClientConcurrentRequests != nil {
		httpClientConcurrentRequests.Add(ctx, 1)
	}
}

// DecConcurrentRequests decrements the in-flight HTTP request counter.
func DecConcurrentRequests(ctx context.Context) {
	if httpClientConcurrentRequests != nil {
		httpClientConcurrentRequests.Add(ctx, -1)
	}
}

// Handler returns an HTTP handler that exposes Prometheus metrics via the OTel
// Prometheus bridge exporter.
func Handler() http.Handler { return promhttp.Handler() }

// RegisterMetrics is a compatibility shim that calls InitMetrics.
func RegisterMetrics() {
	_ = InitMetrics(context.Background())
	http.Handle("/metrics", Handler())
}

// PrometheusMiddleware returns a Gin middleware handler that records HTTP request totals.
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if httpRequestsTotal == nil {
			return
		}
		statusCode := strconv.Itoa(c.Writer.Status())
		httpRequestsTotal.Add(c.Request.Context(), 1,
			metric.WithAttributes(
				attribute.String(AttrPath, c.Request.URL.Path),
				attribute.String(AttrStatusCode, statusCode),
			))
	}
}

// RecordPluginResolution records plugin resolution metrics to OTel.
// source should be "cache" or "registry".
func RecordPluginResolution(ctx context.Context, pluginName, source string, duration float64, success bool) {
	if PluginResolutionDuration == nil || PluginResolutionTotal == nil {
		return
	}
	status := "success"
	if !success {
		status = "failure"
	}
	attrs := metric.WithAttributes(
		attribute.String(AttrPluginName, pluginName),
		attribute.String(AttrPluginSource, source),
		attribute.String(AttrStatus, status),
	)
	PluginResolutionDuration.Record(ctx, duration, attrs)
	PluginResolutionTotal.Add(ctx, 1, attrs)
}

// RecordProviderExecution records provider execution metrics to OTel.
func RecordProviderExecution(ctx context.Context, providerName string, duration float64, success bool) {
	if ProviderExecutionDuration == nil || ProviderExecutionTotal == nil {
		return
	}
	status := "success"
	if !success {
		status = "failure"
	}
	attrs := metric.WithAttributes(
		attribute.String(AttrProviderName, providerName),
		attribute.String(AttrStatus, status),
	)
	ProviderExecutionDuration.Record(ctx, duration, attrs)
	ProviderExecutionTotal.Add(ctx, 1, attrs)
}
