// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// newRecordingTracerProvider returns a tracer provider whose ended spans are
// captured by the returned recorder.
func newRecordingTracerProvider(t testing.TB) (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, sr
}

// newCaptureLogger returns a JSON logger at verbosity 1 and a function that
// returns every log line written to it, decoded into a map.
func newCaptureLogger(t *testing.T) (logr.Logger, func() []map[string]any) {
	t.Helper()
	var (
		mu    sync.Mutex
		lines []string
	)
	lgr := funcr.NewJSON(func(obj string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, obj)
	}, funcr.Options{Verbosity: 1})

	return lgr, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		out := make([]map[string]any, 0, len(lines))
		for _, l := range lines {
			var m map[string]any
			require.NoError(t, json.Unmarshal([]byte(l), &m))
			out = append(out, m)
		}
		return out
	}
}

// cloneRequest mimics middleware such as chi's Timeout that hand a request
// copy (r.WithContext) to the next handler, which is what defeats otelhttp's
// own r.Pattern-based renaming.
func cloneRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(r.Context()))
	})
}

func rejectForbidden(_ http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
}

func TestTracing_ReturnsMiddleware(t *testing.T) {
	mw := Tracing()
	assert.NotNil(t, mw)
}

func TestTracing_PassesThrough(t *testing.T) {
	called := false
	handler := Tracing()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTracing_SpanNaming(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		reject     bool
		panics     bool
		wantName   string
		wantRoute  string
		wantStatus int
	}{
		{
			name:       "route with param uses pattern not raw path",
			method:     http.MethodGet,
			path:       "/v1/items/42",
			wantName:   "GET /v1/items/{id}",
			wantRoute:  "/v1/items/{id}",
			wantStatus: http.StatusOK,
		},
		{
			name:       "static route",
			method:     http.MethodPost,
			path:       "/v1/solutions/run",
			wantName:   "POST /v1/solutions/run",
			wantRoute:  "/v1/solutions/run",
			wantStatus: http.StatusOK,
		},
		{
			name:       "unmatched route keeps method-only name",
			method:     http.MethodGet,
			path:       "/v1/missing",
			wantName:   "GET",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "rejected before routing keeps method-only name",
			method:     http.MethodGet,
			path:       "/v1/items/42",
			reject:     true,
			wantName:   "GET",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "panicking handler still gets route name",
			method:     http.MethodGet,
			path:       "/v1/items/42",
			panics:     true,
			wantName:   "GET /v1/items/{id}",
			wantRoute:  "/v1/items/{id}",
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, sr := newRecordingTracerProvider(t)

			var handlerTraceID string
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerTraceID = traceIDFromContext(r.Context())
				if tt.panics {
					panic("boom")
				}
				w.WriteHeader(http.StatusOK)
			})

			router := chi.NewRouter()
			// Recoverer sits outside tracing, as in SetupMiddleware.
			router.Use(chimiddleware.Recoverer)
			router.Use(Tracing(otelhttp.WithTracerProvider(tp)))
			router.Use(cloneRequest)
			if tt.reject {
				router.Use(rejectForbidden)
			}
			router.Get("/v1/items/{id}", h)
			router.Post("/v1/solutions/run", h)

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)

			spans := sr.Ended()
			require.Len(t, spans, 1)
			span := spans[0]
			assert.Equal(t, tt.wantName, span.Name())

			var gotRoute string
			for _, kv := range span.Attributes() {
				if kv.Key == semconv.HTTPRouteKey {
					gotRoute = kv.Value.AsString()
				}
			}
			assert.Equal(t, tt.wantRoute, gotRoute)

			if tt.wantStatus == http.StatusOK {
				assert.Equal(t, span.SpanContext().TraceID().String(), handlerTraceID,
					"handler should see the request span in its context")
			}
		})
	}
}

func TestTraceIDFromContext(t *testing.T) {
	t.Run("no span", func(t *testing.T) {
		assert.Empty(t, traceIDFromContext(context.Background()))
	})

	t.Run("recording span", func(t *testing.T) {
		tp, _ := newRecordingTracerProvider(t)
		ctx, span := tp.Tracer("test").Start(context.Background(), "op")
		defer span.End()
		assert.Equal(t, span.SpanContext().TraceID().String(), traceIDFromContext(ctx))
	})
}

func BenchmarkTracing(b *testing.B) {
	handler := Tracing()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/bench", nil)
	for b.Loop() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkTracing_RoutedRecording(b *testing.B) {
	// No span processor: spans are recorded (so renaming runs) but not
	// retained, keeping memory flat across iterations.
	tp := sdktrace.NewTracerProvider()
	b.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	router := chi.NewRouter()
	router.Use(Tracing(otelhttp.WithTracerProvider(tp)))
	router.Get("/v1/items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/v1/items/42", nil)
	for b.Loop() {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}
