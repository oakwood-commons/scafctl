// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// tracingOperation is the operation name handed to otelhttp. otelhttp's
// default span-name formatter ignores it (spans are named per the HTTP
// semantic conventions), so it never appears in exported spans.
const tracingOperation = "http.server"

// Tracing returns middleware that instruments HTTP requests with OpenTelemetry
// traces.
//
// Spans follow the HTTP semantic conventions: a span starts named "{method}"
// and, once chi has matched a route, is renamed to "{method} {route}" with the
// http.route attribute set. Using the route pattern rather than the raw path
// keeps span-name cardinality bounded. Requests that never reach a route
// (404s, or rejections by middleware that runs before routing) keep the
// method-only name.
//
// opts are forwarded to otelhttp (for example otelhttp.WithTracerProvider);
// without options the global tracer provider and propagators are used.
func Tracing(opts ...otelhttp.Option) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(routeSpanName(next), tracingOperation, opts...)
	}
}

// routeSpanName renames the active span after next has run, using the chi
// route pattern. chi stores a *chi.Context in the request context before any
// middleware runs and fills in the matched patterns in place while routing,
// so the pattern is readable here even though routing happens further down
// the chain. (otelhttp's own r.Pattern check does not help: chi sets Pattern
// on the request copy it routes, which is not the one otelhttp holds once
// any intermediate middleware has called r.WithContext.) The rename is
// deferred so it also runs while a handler panic unwinds to an outer
// recoverer.
func routeSpanName(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rctx := chi.RouteContext(r.Context())
			if rctx == nil {
				return
			}
			pattern := rctx.RoutePattern()
			if pattern == "" {
				return
			}
			span := trace.SpanFromContext(r.Context())
			span.SetName(r.Method + " " + pattern)
			span.SetAttributes(semconv.HTTPRoute(pattern))
		}()

		next.ServeHTTP(w, r)
	})
}

// traceIDFromContext returns the hex trace ID of the span in ctx, or "" when
// ctx carries no trace (tracing disabled, or a path tracing does not cover).
func traceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
