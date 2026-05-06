// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	upstream "github.com/oakwood-commons/httpc"

	"github.com/oakwood-commons/scafctl/pkg/metrics"
)

// OTelMetrics implements the upstream httpc.Metrics interface by delegating to
// the scafctl pkg/metrics OTel instruments. This bridges the external library's
// generic metrics interface to scafctl's application-level OTel setup.
type OTelMetrics struct{}

var _ upstream.Metrics = OTelMetrics{}

func (OTelMetrics) RecordRequestDuration(ctx context.Context, method, host, pathTemplate string, statusCode int, duration time.Duration) {
	if metrics.HTTPClientDuration == nil {
		return
	}
	metrics.HTTPClientDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			attribute.String(metrics.AttrMethod, method),
			attribute.String(metrics.LabelHost, host),
			attribute.String(metrics.LabelPathTemplate, pathTemplate),
			attribute.String(metrics.AttrStatusCode, strconv.Itoa(statusCode)),
		))
}

func (OTelMetrics) IncrementRequestsTotal(ctx context.Context, method, host, pathTemplate string, statusCode int) {
	if metrics.HTTPClientRequestsTotal == nil {
		return
	}
	metrics.HTTPClientRequestsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String(metrics.AttrMethod, method),
			attribute.String(metrics.LabelHost, host),
			attribute.String(metrics.LabelPathTemplate, pathTemplate),
			attribute.String(metrics.AttrStatusCode, strconv.Itoa(statusCode)),
		))
}

func (OTelMetrics) IncrementErrorsTotal(ctx context.Context, method, host, pathTemplate, errorType string) {
	if metrics.HTTPClientErrorsTotal == nil {
		return
	}
	metrics.HTTPClientErrorsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String(metrics.AttrMethod, method),
			attribute.String(metrics.LabelHost, host),
			attribute.String(metrics.LabelPathTemplate, pathTemplate),
			attribute.String(metrics.AttrErrorType, errorType),
		))
}

func (OTelMetrics) IncrementRetries(ctx context.Context, method, host, pathTemplate string) {
	if metrics.HTTPClientRetriesTotal == nil {
		return
	}
	metrics.HTTPClientRetriesTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String(metrics.AttrMethod, method),
			attribute.String(metrics.LabelHost, host),
			attribute.String(metrics.LabelPathTemplate, pathTemplate),
		))
}

func (OTelMetrics) IncrementCacheHits(ctx context.Context) {
	if metrics.HTTPClientCacheHits == nil {
		return
	}
	metrics.HTTPClientCacheHits.Add(ctx, 1)
}

func (OTelMetrics) IncrementCacheMisses(ctx context.Context) {
	if metrics.HTTPClientCacheMisses == nil {
		return
	}
	metrics.HTTPClientCacheMisses.Add(ctx, 1)
}

func (OTelMetrics) SetCacheSizeBytes(bytes int64) {
	metrics.SetCacheSizeBytes(bytes)
}

func (OTelMetrics) SetCircuitBreakerState(host string, state float64) {
	metrics.SetCircuitBreakerState(host, state)
}

func (OTelMetrics) IncrementConcurrentRequests(ctx context.Context) {
	metrics.IncConcurrentRequests(ctx)
}

func (OTelMetrics) DecrementConcurrentRequests(ctx context.Context) {
	metrics.DecConcurrentRequests(ctx)
}

func (OTelMetrics) RecordRequestSize(ctx context.Context, method, host, pathTemplate string, bytes float64) {
	if metrics.HTTPClientRequestSize == nil {
		return
	}
	metrics.HTTPClientRequestSize.Record(ctx, bytes,
		metric.WithAttributes(
			attribute.String(metrics.AttrMethod, method),
			attribute.String(metrics.LabelHost, host),
			attribute.String(metrics.LabelPathTemplate, pathTemplate),
		))
}

func (OTelMetrics) RecordResponseSize(ctx context.Context, method, host, pathTemplate string, bytes float64) {
	if metrics.HTTPClientResponseSize == nil {
		return
	}
	metrics.HTTPClientResponseSize.Record(ctx, bytes,
		metric.WithAttributes(
			attribute.String(metrics.AttrMethod, method),
			attribute.String(metrics.LabelHost, host),
			attribute.String(metrics.LabelPathTemplate, pathTemplate),
		))
}
