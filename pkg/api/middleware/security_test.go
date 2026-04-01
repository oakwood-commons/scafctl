// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flusherRecorder wraps httptest.ResponseRecorder and implements http.Flusher.
type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flusherRecorder) Flush() { f.flushed = true }

func TestSecurityHeaders(t *testing.T) {
	tests := []struct {
		name     string
		tls      bool
		wantHSTS bool
	}{
		{"without TLS", false, false},
		{"with TLS", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := SecurityHeaders(tt.tls)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
			assert.NotEmpty(t, rec.Header().Get("Content-Security-Policy"))

			if tt.wantHSTS {
				assert.NotEmpty(t, rec.Header().Get("Strict-Transport-Security"))
			} else {
				assert.Empty(t, rec.Header().Get("Strict-Transport-Security"))
			}
		})
	}
}

func TestMaxBodySize_WithinLimit(t *testing.T) {
	handler := MaxBodySize(1024)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("small body")
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.ContentLength = int64(body.Len())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMaxBodySize_ExceedsLimit(t *testing.T) {
	handler := MaxBodySize(10)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("this body is way too large for the limit")
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.ContentLength = int64(body.Len())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestGzipResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	gzw := &GzipResponseWriter{
		Writer:         io.Discard,
		ResponseWriter: rec,
	}

	gzw.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, rec.Code)

	n, err := gzw.Write([]byte("test"))
	assert.NoError(t, err)
	assert.Equal(t, 4, n)

	assert.NotNil(t, gzw.Header())
}

func TestGzipResponseWriter_Flush_WithGzipWriter(t *testing.T) {
	var buf bytes.Buffer
	gzw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	w := &GzipResponseWriter{Writer: gzw, ResponseWriter: rec}
	w.Flush() // should not panic; gzip.Writer implements Flush() error
}

func TestGzipResponseWriter_Flush_PropagatesResponseFlusher(t *testing.T) {
	var buf bytes.Buffer
	gzw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	require.NoError(t, err)

	fr := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	w := &GzipResponseWriter{Writer: gzw, ResponseWriter: fr}
	w.Flush()
	assert.True(t, fr.flushed)
}

func TestGzipResponseWriter_Flush_NoFlusher(t *testing.T) {
	// io.Discard does not implement Flush() and ResponseRecorder is not an http.Flusher.
	w := &GzipResponseWriter{Writer: io.Discard, ResponseWriter: httptest.NewRecorder()}
	w.Flush() // should not panic
}

func TestGzipResponseWriter_Close_WithCloser(t *testing.T) {
	var buf bytes.Buffer
	gzw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	require.NoError(t, err)

	w := &GzipResponseWriter{Writer: gzw, ResponseWriter: httptest.NewRecorder()}
	err = w.Close()
	assert.NoError(t, err)
}

func TestGzipResponseWriter_Close_WithoutCloser(t *testing.T) {
	// io.Discard does not implement io.Closer.
	w := &GzipResponseWriter{Writer: io.Discard, ResponseWriter: httptest.NewRecorder()}
	err := w.Close()
	assert.NoError(t, err)
}

func TestGzipEncoderFunc(t *testing.T) {
	var buf bytes.Buffer
	w := GzipEncoderFunc(&buf, gzip.DefaultCompression)
	require.NotNil(t, w)
	n, err := w.Write([]byte("hello gzip"))
	assert.NoError(t, err)
	assert.Equal(t, 10, n)
}

func BenchmarkSecurityHeaders(b *testing.B) {
	handler := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/bench", nil)

	for b.Loop() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}
