// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimit_AllowsWithinLimit(t *testing.T) {
	handler := RateLimit(t.Context(), 5, time.Minute, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "request %d should succeed", i)
	}
}

func TestRateLimit_RejectsOverLimit(t *testing.T) {
	handler := RateLimit(t.Context(), 2, time.Minute, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// Third request should be rejected
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestRateLimit_SetsHeaders(t *testing.T) {
	handler := RateLimit(t.Context(), 5, time.Minute, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "5", rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "4", rec.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Reset"))
}

func TestRateLimit_HeadersDecrementRemaining(t *testing.T) {
	handler := RateLimit(t.Context(), 3, time.Minute, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.200:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, "3", rec.Header().Get("X-RateLimit-Limit"))
		expectedRemaining := 3 - (i + 1)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, strconv.Itoa(expectedRemaining), rec.Header().Get("X-RateLimit-Remaining"))
	}

	// Fourth request - rejected, remaining should be 0
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.200:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "3", rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_DifferentIPs(t *testing.T) {
	handler := RateLimit(t.Context(), 1, time.Minute, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First IP
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Second IP should also succeed
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.2:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestExtractIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")

	ip := extractIP(req, true)
	assert.Equal(t, "10.0.0.1", ip)
}

// TestExtractIP_XForwardedFor_TrimsWhitespace verifies leading/trailing spaces
// are stripped from the first XFF entry (common with multi-proxy setups).
func TestExtractIP_XForwardedFor_TrimsWhitespace(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", " 10.0.0.1 , 10.0.0.2")

	ip := extractIP(req, true)
	assert.Equal(t, "10.0.0.1", ip)
}

func TestExtractIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "10.0.0.5")

	ip := extractIP(req, true)
	assert.Equal(t, "10.0.0.5", ip)
}

// TestExtractIP_XRealIP_TrimsWhitespace verifies X-Real-IP whitespace is stripped.
func TestExtractIP_XRealIP_TrimsWhitespace(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", " 10.0.0.5 ")

	ip := extractIP(req, true)
	assert.Equal(t, "10.0.0.5", ip)
}

func TestExtractIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:54321"

	ip := extractIP(req, false)
	assert.Equal(t, "192.168.1.1", ip)
}

// TestExtractIP_RemoteAddr_IPv6 verifies that IPv6 RemoteAddr (e.g. "[::1]:port")
// is parsed correctly using net.SplitHostPort so brackets are stripped.
func TestExtractIP_RemoteAddr_IPv6(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[::1]:54321"

	ip := extractIP(req, false)
	assert.Equal(t, "::1", ip)
}

// TestExtractIP_IgnoresXFFWhenTrustProxyFalse verifies that X-Forwarded-For is
// ignored when trustProxy is false, preventing clients from spoofing their IP.
func TestExtractIP_IgnoresXFFWhenTrustProxyFalse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:54321"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	ip := extractIP(req, false)
	assert.Equal(t, "192.168.1.1", ip, "should use RemoteAddr when trustProxy is false")
}

func BenchmarkRateLimit(b *testing.B) {
	handler := RateLimit(b.Context(), 100000, time.Minute, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/bench", nil)
	req.RemoteAddr = "192.168.1.1:1234"

	for b.Loop() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}
