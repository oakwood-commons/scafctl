// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func TestRequestLogging(t *testing.T) {
	handler := RequestLogging(logr.Discard())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequestLogging_CorrelationFields(t *testing.T) {
	tests := []struct {
		name          string
		traced        bool
		clientReqID   string
		wantRequestID string
	}{
		{name: "traced request logs traceID and generated requestID", traced: true},
		{name: "untraced request omits traceID", traced: false},
		{name: "client-supplied request ID is logged", traced: true, clientReqID: "abc-123", wantRequestID: "abc-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, sr := newRecordingTracerProvider(t)
			lgr, lines := newCaptureLogger(t)

			var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			h = RequestLogging(lgr)(h)
			if tt.traced {
				h = Tracing(otelhttp.WithTracerProvider(tp))(h)
			}
			h = chimiddleware.RequestID(h)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/test", nil)
			if tt.clientReqID != "" {
				req.Header.Set(chimiddleware.RequestIDHeader, tt.clientReqID)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			logged := lines()
			require.Len(t, logged, 1)
			entry := logged[0]

			assert.NotEmpty(t, entry["requestID"])
			if tt.wantRequestID != "" {
				assert.Equal(t, tt.wantRequestID, entry["requestID"])
			}

			if tt.traced {
				spans := sr.Ended()
				require.Len(t, spans, 1)
				assert.Equal(t, spans[0].SpanContext().TraceID().String(), entry["traceID"])
			} else {
				assert.NotContains(t, entry, "traceID")
			}
		})
	}
}

func TestStatusWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}
	sw.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, sw.status)
	assert.True(t, sw.wroteHeader)
	sw.WriteHeader(http.StatusInternalServerError)
	assert.Equal(t, http.StatusNotFound, sw.status)
}

func TestStatusWriter_Write(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}
	n, err := sw.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.True(t, sw.wroteHeader)
}

func TestStatusWriter_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}
	sw.Flush()
}

func BenchmarkRequestLogging(b *testing.B) {
	handler := RequestLogging(logr.Discard())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/bench", nil)
	for b.Loop() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}
