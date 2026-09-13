// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
)

// TestHostAllowlist covers the DNS-rebinding guard. The default (empty config)
// must stay permissive so enabling the feature cannot silently break an
// existing deployment; once configured it must reject anything unlisted.
func TestHostAllowlist(t *testing.T) {
	t.Run("empty allowlist accepts any host", func(t *testing.T) {
		hosts := []string{"evil.example.com", "127.0.0.1:8080", "anything"}

		for _, host := range hosts {
			t.Run(host, func(t *testing.T) {
				var reached bool
				mw := middleware.HostAllowlist(nil, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
				req.Host = host
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusOK, rec.Code)
				assert.True(t, reached, "empty allowlist must not block")
			})
		}
	})

	t.Run("wildcard entry disables the check", func(t *testing.T) {
		var reached bool
		mw := middleware.HostAllowlist([]string{"*"}, logr.Discard())

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
		req.Host = "evil.example.com"
		rec := httptest.NewRecorder()

		mw(okHandler(&reached)).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, reached)
	})

	t.Run("listed hosts are accepted", func(t *testing.T) {
		allowed := []string{"api.example.com", "localhost"}
		accepted := []string{
			"api.example.com",
			"api.example.com:8080",
			"API.EXAMPLE.COM",
			"localhost",
			"localhost:8080",
		}

		for _, host := range accepted {
			t.Run(host, func(t *testing.T) {
				var reached bool
				mw := middleware.HostAllowlist(allowed, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
				req.Host = host
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusOK, rec.Code, "host %q should be allowed", host)
				assert.True(t, reached)
			})
		}
	})

	t.Run("unlisted hosts are rejected", func(t *testing.T) {
		allowed := []string{"api.example.com"}
		rejected := []string{
			"evil.example.com",
			"api.example.com.evil.com",
			"notapi.example.com",
			"example.com",
			"",
		}

		for _, host := range rejected {
			t.Run("host="+host, func(t *testing.T) {
				var reached bool
				mw := middleware.HostAllowlist(allowed, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
				req.Host = host
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusForbidden, rec.Code, "host %q should be rejected", host)
				assert.False(t, reached)
				assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			})
		}
	})

	t.Run("suffix confusion is not allowed", func(t *testing.T) {
		// "api.example.com.evil.com" ends with the allowed name as a substring
		// but is a different domain. A naive HasSuffix check would accept it.
		var reached bool
		mw := middleware.HostAllowlist([]string{"example.com"}, logr.Discard())

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
		req.Host = "example.com.evil.com"
		rec := httptest.NewRecorder()

		mw(okHandler(&reached)).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.False(t, reached)
	})

	t.Run("wildcard subdomain matching", func(t *testing.T) {
		allowed := []string{"*.example.com"}
		cases := []struct {
			host    string
			allowed bool
		}{
			{"api.example.com", true},
			{"deep.nested.example.com", true},
			// The bare apex is NOT covered by "*.example.com" -- matching
			// nginx and x509 wildcard semantics. An operator serving the apex
			// lists it explicitly.
			{"example.com", false},
			{"example.com.evil.com", false},
			{"notexample.com", false},
			{"evil.com", false},
		}

		for _, tc := range cases {
			t.Run(tc.host, func(t *testing.T) {
				var reached bool
				mw := middleware.HostAllowlist(allowed, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
				req.Host = tc.host
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				if tc.allowed {
					assert.Equal(t, http.StatusOK, rec.Code, "%q should match *.example.com", tc.host)
				} else {
					assert.Equal(t, http.StatusForbidden, rec.Code, "%q should not match *.example.com", tc.host)
				}
			})
		}
	})

	t.Run("ipv6 literal hosts are handled", func(t *testing.T) {
		cases := []struct {
			name    string
			allowed []string
			host    string
			ok      bool
		}{
			{"bracketed with port matches bare entry", []string{"::1"}, "[::1]:8080", true},
			{"bracketed without port matches bare entry", []string{"::1"}, "[::1]", true},
			{"different address rejected", []string{"::1"}, "[2001:db8::1]:8080", false},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var reached bool
				mw := middleware.HostAllowlist(tc.allowed, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
				req.Host = tc.host
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				if tc.ok {
					assert.Equal(t, http.StatusOK, rec.Code)
				} else {
					assert.Equal(t, http.StatusForbidden, rec.Code)
				}
			})
		}
	})

	t.Run("blank and whitespace entries are ignored", func(t *testing.T) {
		var reached bool
		mw := middleware.HostAllowlist([]string{"", "   ", "api.example.com"}, logr.Discard())

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
		req.Host = "api.example.com"
		rec := httptest.NewRecorder()

		mw(okHandler(&reached)).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, reached, "blank entries must not corrupt the allowlist")
	})

	t.Run("an allowlist of only blanks does not lock everything out", func(t *testing.T) {
		// Normalizing away every entry leaves an empty list, which means
		// "disabled" rather than "deny all" -- matching the documented default.
		var reached bool
		mw := middleware.HostAllowlist([]string{"", "  "}, logr.Discard())

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
		req.Host = "anything.example.com"
		rec := httptest.NewRecorder()

		mw(okHandler(&reached)).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, reached)
	})
}
