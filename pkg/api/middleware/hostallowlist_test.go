// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			// nginx wildcard server_name semantics. An operator serving the
			// apex lists it explicitly. Multi-label subdomains ARE covered,
			// unlike TLS certificate wildcards.
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

	t.Run("an allowlist with no usable entries fails closed", func(t *testing.T) {
		// Regression test for a fail-open security hole. An operator who
		// supplied entries asked for the check to be ON; if every entry
		// normalizes away, accepting all hosts would silently deliver the exact
		// opposite of the request. Config validation rejects this at startup,
		// so this path only runs for a direct caller -- which must still deny.
		unusable := [][]string{
			{"", "  "},
			{"*."},
			{"", "*.", "\t"},
		}

		for _, hosts := range unusable {
			t.Run(strings.Join(hosts, ","), func(t *testing.T) {
				var reached bool
				mw := middleware.HostAllowlist(hosts, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/providers", nil)
				req.Host = "anything.example.com"
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusForbidden, rec.Code)
				assert.False(t, reached, "an inert allowlist must not fail open")
				assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			})
		}
	})
}

// TestValidateAllowedHosts asserts configuration validation rejects an
// allowlist that would leave the control configured but inert, while leaving
// both documented opt-outs (unset, and a bare "*") valid.
func TestValidateAllowedHosts(t *testing.T) {
	tests := []struct {
		name    string
		hosts   []string
		wantErr bool
	}{
		{"unset is the documented default", nil, false},
		{"empty slice is the documented default", []string{}, false},
		{"bare wildcard is the explicit opt-out", []string{"*"}, false},
		{"usable entry", []string{"api.example.com"}, false},
		{"usable entry alongside a blank", []string{"", "api.example.com"}, false},
		{"wildcard subdomain entry", []string{"*.example.com"}, false},
		{"only blanks", []string{"", "   "}, true},
		{"only the malformed wildcard", []string{"*."}, true},
		{"blanks and malformed wildcard", []string{" ", "*.", ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := middleware.ValidateAllowedHosts(tt.hosts)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
