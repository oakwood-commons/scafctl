// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// SecurityHeaders returns middleware that sets standard security headers.
func SecurityHeaders(tlsEnabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			// Huma serves its interactive docs UI (Swagger/Elements) at /{version}/docs.
			// That page loads scripts and styles from external CDNs, so we relax the CSP
			// only for that path to avoid breaking the docs while keeping API routes strict.
			if strings.HasSuffix(r.URL.Path, "/docs") || strings.HasSuffix(r.URL.Path, "/docs/") {
				w.Header().Set("Content-Security-Policy", "default-src 'self' https:; script-src 'self' 'unsafe-inline' https:; style-src 'self' 'unsafe-inline' https:; img-src 'self' data: https:; connect-src 'self' https:")
			} else {
				w.Header().Set("Content-Security-Policy", "default-src 'none'")
			}
			w.Header().Set("X-XSS-Protection", "0")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			w.Header().Set("Cache-Control", "no-store")

			if tlsEnabled {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// MaxBodySize returns middleware that rejects requests with bodies larger than maxSize.
func MaxBodySize(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxSize {
				writeJSONError(w, `{"title":"Request Entity Too Large","status":413,"detail":"request body too large"}`, http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			next.ServeHTTP(w, r)
		})
	}
}

// GzipResponseWriter wraps an http.ResponseWriter with a gzip writer.
type GzipResponseWriter struct {
	Writer         io.Writer
	ResponseWriter http.ResponseWriter
}

// Header returns the header map.
func (g *GzipResponseWriter) Header() http.Header {
	return g.ResponseWriter.Header()
}

// Write writes the compressed data.
func (g *GzipResponseWriter) Write(b []byte) (int, error) {
	return g.Writer.Write(b)
}

// WriteHeader sends an HTTP response header with the provided status code.
func (g *GzipResponseWriter) WriteHeader(code int) {
	g.ResponseWriter.WriteHeader(code)
}

// Flush flushes the underlying gzip writer and response writer.
func (g *GzipResponseWriter) Flush() {
	if f, ok := g.Writer.(interface{ Flush() error }); ok {
		_ = f.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Close closes the underlying gzip writer.
func (g *GzipResponseWriter) Close() error {
	if c, ok := g.Writer.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// GzipEncoderFunc is a chi-compatible EncoderFunc that creates a gzip writer.
// The compression level is validated by the chi Compressor before this function
// is called, so gzip.NewWriterLevel only returns an error for invalid levels.
func GzipEncoderFunc(w io.Writer, level int) io.Writer {
	gzw, _ := gzip.NewWriterLevel(w, level) //nolint:errcheck // level validated by caller
	return gzw
}
