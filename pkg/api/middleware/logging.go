// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"time"

	"github.com/go-logr/logr"
)

// RequestLogging returns middleware that logs every request.
func RequestLogging(lgr logr.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			duration := time.Since(start)
			lgr.V(1).Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration", duration.String(),
				"remote", r.RemoteAddr,
				"requestID", r.Header.Get("X-Request-ID"),
			)
		})
	}
}

// statusWriter wraps ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// WriteHeader captures the status code.
func (sw *statusWriter) WriteHeader(code int) {
	if !sw.wroteHeader {
		sw.status = code
		sw.wroteHeader = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

// Write captures that the header has been written.
func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.wroteHeader = true
	}
	return sw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher if the underlying writer supports it.
func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// writeJSONError writes a JSON problem+json error response with the correct
// Content-Type header. Use this instead of http.Error for JSON-formatted
// error bodies so clients can rely on the Content-Type being accurate.
func writeJSONError(w http.ResponseWriter, body string, code int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}
