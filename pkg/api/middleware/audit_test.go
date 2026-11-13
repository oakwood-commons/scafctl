// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func TestAuditLog(t *testing.T) {
	handler := AuditLog(logr.Discard(), false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuditLog_CorrelationFields(t *testing.T) {
	tests := []struct {
		name   string
		traced bool
	}{
		{name: "traced request logs traceID", traced: true},
		{name: "untraced request omits traceID", traced: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, sr := newRecordingTracerProvider(t)
			lgr, lines := newCaptureLogger(t)

			var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			h = AuditLog(lgr, false)(h)
			if tt.traced {
				h = Tracing(otelhttp.WithTracerProvider(tp))(h)
			}
			h = chimiddleware.RequestID(h)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/test", nil)
			h.ServeHTTP(httptest.NewRecorder(), req)

			logged := lines()
			require.Len(t, logged, 1)
			entry := logged[0]
			assert.Equal(t, "audit", entry["msg"])
			assert.NotEmpty(t, entry["requestID"])

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

func TestRedactBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		contains []string
		absent   []string
	}{
		{
			name:     "redacts password field",
			body:     `{"username":"alice","password":"s3cret"}`,
			contains: []string{`[REDACTED]`, `"alice"`},
			absent:   []string{"s3cret"},
		},
		{
			name:     "redacts token field",
			body:     `{"accessToken":"eyJ...","name":"test"}`,
			contains: []string{`[REDACTED]`, `"test"`},
			absent:   []string{"eyJ..."},
		},
		{
			name:     "redacts nested secrets",
			body:     `{"auth":{"clientSecret":"abc123"},"name":"test"}`,
			contains: []string{`[REDACTED]`, `"test"`},
			absent:   []string{"abc123"},
		},
		{
			name:     "case insensitive key matching",
			body:     `{"API_KEY":"my-key","value":"ok"}`,
			contains: []string{`[REDACTED]`, `"ok"`},
			absent:   []string{"my-key"},
		},
		{
			name:     "non-JSON body redacted",
			body:     `not json at all`,
			contains: []string{"<non-JSON body redacted>"},
			absent:   []string{"not json at all"},
		},
		{
			name:     "no sensitive fields unchanged",
			body:     `{"name":"alice","age":30}`,
			contains: []string{`"alice"`, `30`},
		},
		{
			name:     "redacts credential field",
			body:     `{"credential":"cred-val","ok":true}`,
			contains: []string{`[REDACTED]`},
			absent:   []string{"cred-val"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := redactBody([]byte(tt.body))
			for _, s := range tt.contains {
				assert.Contains(t, result, s)
			}
			for _, s := range tt.absent {
				assert.NotContains(t, result, s)
			}
		})
	}
}

func TestIsSensitiveKey(t *testing.T) {
	assert.True(t, isSensitiveKey("password"))
	assert.True(t, isSensitiveKey("Password"))
	assert.True(t, isSensitiveKey("clientSecret"))
	assert.True(t, isSensitiveKey("API_KEY"))
	assert.True(t, isSensitiveKey("accessToken"))
	assert.True(t, isSensitiveKey("authorization"))
	assert.True(t, isSensitiveKey("credential"))
	assert.False(t, isSensitiveKey("name"))
	assert.False(t, isSensitiveKey("email"))
	assert.False(t, isSensitiveKey("path"))
}

func TestAuditLog_RedactsBody(t *testing.T) {
	handler := AuditLog(logr.Discard(), false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the original body is still readable by downstream handlers
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		body := string(buf[:n])
		assert.Contains(t, body, "s3cret", "original body should be preserved for downstream handlers")
		w.WriteHeader(http.StatusOK)
	}))

	body := `{"username":"alice","password":"s3cret"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func BenchmarkAuditLog(b *testing.B) {
	handler := AuditLog(logr.Discard(), false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/bench", nil)

	for b.Loop() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkRedactBody_JSON(b *testing.B) {
	body := []byte(`{"username":"alice","password":"s3cret","token":"abc","name":"test"}`)
	for b.Loop() {
		redactBody(body)
	}
}

func BenchmarkRedactBody_NonJSON(b *testing.B) {
	body := []byte(`this is not json content`)
	for b.Loop() {
		redactBody(body)
	}
}

func TestRedactJSON_Object(t *testing.T) {
	v := map[string]any{"username": "alice", "password": "s3cret", "name": "test"}
	result := RedactJSON(v)

	m, ok := result.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "[REDACTED]", m["password"])
	assert.Equal(t, "alice", m["username"])
	assert.Equal(t, "test", m["name"])
}

func TestRedactJSON_Array(t *testing.T) {
	v := []any{
		map[string]any{"name": "a", "token": "tok1"},
		map[string]any{"name": "b", "token": "tok2"},
	}
	result := RedactJSON(v)

	arr, ok := result.([]any)
	assert.True(t, ok)
	for _, item := range arr {
		m, ok := item.(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "[REDACTED]", m["token"])
	}
}

func TestRedactJSON_NonSerializable(t *testing.T) {
	// channels cannot be JSON-marshaled; the original value should be returned unchanged
	ch := make(chan int)
	result := RedactJSON(ch)
	assert.Equal(t, ch, result)
}
