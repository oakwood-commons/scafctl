// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	apiRequestsTotal   metric.Int64Counter
	apiRequestDuration metric.Float64Histogram
	apiActiveRequests  metric.Int64UpDownCounter
	// metricsOnce ensures thread-safe one-time initialization of OTel instruments.
	metricsOnce sync.Once
)

func initAPIMetrics() {
	meter := otel.Meter("scafctl.api")

	var err error
	apiRequestsTotal, err = meter.Int64Counter(
		"scafctl.api.requests.total",
		metric.WithDescription("Total number of API requests"),
	)
	if err != nil {
		return
	}

	apiRequestDuration, err = meter.Float64Histogram(
		"scafctl.api.request.duration",
		metric.WithDescription("API request duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return
	}

	apiActiveRequests, err = meter.Int64UpDownCounter(
		"scafctl.api.active_requests",
		metric.WithDescription("Number of currently active API requests"),
	)
	if err != nil {
		return
	}
}

// Metrics returns middleware that records per-request metrics.
func Metrics() func(http.Handler) http.Handler {
	// sync.Once provides the memory barrier required for safe concurrent use
	// of apiRequestsTotal, apiRequestDuration, and apiActiveRequests.
	metricsOnce.Do(initAPIMetrics)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiRequestsTotal == nil || apiRequestDuration == nil || apiActiveRequests == nil {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			apiActiveRequests.Add(r.Context(), 1)
			defer apiActiveRequests.Add(r.Context(), -1)

			next.ServeHTTP(sw, r)

			duration := time.Since(start).Seconds()
			attrs := []attribute.KeyValue{
				attribute.String("method", r.Method),
				attribute.String("path", r.URL.Path),
				attribute.String("status_code", strconv.Itoa(sw.status)),
			}

			apiRequestsTotal.Add(r.Context(), 1, metric.WithAttributes(attrs...))
			apiRequestDuration.Record(r.Context(), duration, metric.WithAttributes(attrs...))
		})
	}
}
