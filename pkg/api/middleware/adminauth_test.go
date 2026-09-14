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
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
)

// okHandler records whether the protected handler was reached.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

// TestAdminAuthorization covers the access-control policy documented in
// docs/design/api-surface.md. Before this middleware existed the matrix in that
// document described a control that was not implemented, so these cases pin the
// documented behaviour to the code.
func TestAdminAuthorization(t *testing.T) {
	t.Run("auth enabled: admin role is allowed", func(t *testing.T) {
		var reached bool
		mw := middleware.AdminAuthorization(true, logr.Discard())

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
		req = req.WithContext(middleware.WithAuthClaims(req.Context(), &middleware.AuthClaims{
			Subject: "user-1",
			Roles:   []string{"reader", middleware.AdminRole},
		}))
		rec := httptest.NewRecorder()

		mw(okHandler(&reached)).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, reached, "handler should be reached for an admin caller")
	})

	t.Run("auth enabled: missing admin role is forbidden", func(t *testing.T) {
		var reached bool
		mw := middleware.AdminAuthorization(true, logr.Discard())

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
		req = req.WithContext(middleware.WithAuthClaims(req.Context(), &middleware.AuthClaims{
			Subject: "user-2",
			Roles:   []string{"reader"},
		}))
		rec := httptest.NewRecorder()

		mw(okHandler(&reached)).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.False(t, reached, "handler must not be reached without the admin role")
		assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	})

	t.Run("auth enabled: empty roles is forbidden", func(t *testing.T) {
		var reached bool
		mw := middleware.AdminAuthorization(true, logr.Discard())

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
		req = req.WithContext(middleware.WithAuthClaims(req.Context(), &middleware.AuthClaims{
			Subject: "user-3",
		}))
		rec := httptest.NewRecorder()

		mw(okHandler(&reached)).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.False(t, reached)
	})

	t.Run("auth enabled: nil claims is forbidden, not fail-open", func(t *testing.T) {
		// Guards against a middleware-ordering regression: if the auth
		// middleware stops running before this one, admin routes must close
		// rather than silently open.
		var reached bool
		mw := middleware.AdminAuthorization(true, logr.Discard())

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
		rec := httptest.NewRecorder()

		mw(okHandler(&reached)).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.False(t, reached, "missing claims must deny, never fall through")
	})

	t.Run("auth disabled: loopback callers are allowed", func(t *testing.T) {
		loopback := []string{"127.0.0.1:54321", "[::1]:54321", "127.0.0.53:9999"}

		for _, addr := range loopback {
			t.Run(addr, func(t *testing.T) {
				var reached bool
				mw := middleware.AdminAuthorization(false, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
				req.RemoteAddr = addr
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusOK, rec.Code)
				assert.True(t, reached)
			})
		}
	})

	t.Run("auth disabled: non-loopback callers are forbidden", func(t *testing.T) {
		remote := []string{"192.168.1.10:1234", "10.0.0.5:1234", "203.0.113.7:1234", "[2001:db8::1]:1234"}

		for _, addr := range remote {
			t.Run(addr, func(t *testing.T) {
				var reached bool
				mw := middleware.AdminAuthorization(false, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
				req.RemoteAddr = addr
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusForbidden, rec.Code)
				assert.False(t, reached)
			})
		}
	})

	t.Run("auth disabled: proxy headers cannot spoof loopback", func(t *testing.T) {
		// X-Forwarded-For and X-Real-IP are attacker-controlled. Honouring them
		// here would let any remote caller claim to be localhost and bypass the
		// admin gate, so the middleware must ignore them entirely.
		spoofHeaders := []struct{ key, value string }{
			{"X-Forwarded-For", "127.0.0.1"},
			{"X-Real-IP", "127.0.0.1"},
			{"X-Forwarded-For", "::1"},
			{"Forwarded", "for=127.0.0.1"},
		}

		for _, h := range spoofHeaders {
			t.Run(h.key+"="+h.value, func(t *testing.T) {
				var reached bool
				mw := middleware.AdminAuthorization(false, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
				req.RemoteAddr = "203.0.113.7:1234"
				req.Header.Set(h.key, h.value)
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusForbidden, rec.Code,
					"proxy header %s must not grant loopback access", h.key)
				assert.False(t, reached)
			})
		}
	})

	t.Run("auth disabled: unparseable remote address is forbidden", func(t *testing.T) {
		for _, addr := range []string{"", "not-an-ip", "garbage:port"} {
			t.Run("addr="+addr, func(t *testing.T) {
				var reached bool
				mw := middleware.AdminAuthorization(false, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
				req.RemoteAddr = addr
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusForbidden, rec.Code,
					"an unparseable address must deny, not default to allow")
				assert.False(t, reached)
			})
		}
	})

	t.Run("auth disabled: loopback caller behind a proxy is forbidden", func(t *testing.T) {
		// Regression test for the same-host reverse-proxy hole. When the server
		// binds loopback behind a proxy on the same machine -- the deployment the
		// startup exposure warning recommends -- every forwarded request arrives
		// from 127.0.0.1. Without this check the loopback gate would accept the
		// entire internet.
		proxied := []struct{ key, value string }{
			{"X-Forwarded-For", "203.0.113.7"},
			{"X-Forwarded-For", "127.0.0.1"},
			{"Forwarded", "for=203.0.113.7"},
			{"X-Real-IP", "203.0.113.7"},
		}

		for _, h := range proxied {
			t.Run(h.key+"="+h.value, func(t *testing.T) {
				var reached bool
				mw := middleware.AdminAuthorization(false, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
				req.RemoteAddr = "127.0.0.1:54321"
				req.Header.Set(h.key, h.value)
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusForbidden, rec.Code,
					"a loopback request carrying %s proves a proxy hop and must be denied", h.key)
				assert.False(t, reached)
			})
		}
	})

	t.Run("auth disabled: an empty-valued proxy header still proves a hop", func(t *testing.T) {
		// The policy is presence-based, but http.Header.Get returns "" for both
		// an absent header and one sent with an empty value, so it cannot
		// express presence. An empty-valued header is trivially sendable
		// (`curl -H "X-Forwarded-For;"`, or any client writing the bare header
		// line) and arrives as []string{""}, so a Get-based check would hand
		// such a caller the loopback fallback.
		empty := []string{"X-Forwarded-For", "Forwarded", "X-Real-IP"}

		for _, key := range empty {
			t.Run(key, func(t *testing.T) {
				var reached bool
				mw := middleware.AdminAuthorization(false, logr.Discard())

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
				req.RemoteAddr = "127.0.0.1:54321"
				req.Header.Set(key, "")
				rec := httptest.NewRecorder()

				mw(okHandler(&reached)).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusForbidden, rec.Code,
					"%s present with an empty value must count as a proxy hop", key)
				assert.False(t, reached)
			})
		}
	})

	t.Run("auth enabled: proxy headers do not affect the role check", func(t *testing.T) {
		// The proxy-hop denial applies only to the loopback fallback. With auth
		// enabled, identity comes from the validated token, so a legitimate
		// proxied admin request must still succeed.
		var reached bool
		mw := middleware.AdminAuthorization(true, logr.Discard())

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Header.Set("X-Forwarded-For", "203.0.113.7")
		req = req.WithContext(middleware.WithAuthClaims(req.Context(), &middleware.AuthClaims{
			Subject: "user-admin",
			Roles:   []string{middleware.AdminRole},
		}))
		rec := httptest.NewRecorder()

		mw(okHandler(&reached)).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code,
			"an authenticated admin behind a proxy must not be blocked")
		assert.True(t, reached)
	})

	t.Run("denial response does not disclose the auth configuration", func(t *testing.T) {
		// The body must be identical whether the caller failed the role check or
		// the loopback check, so it cannot be used to probe server config.
		authEnabledReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
		authEnabledReq = authEnabledReq.WithContext(
			middleware.WithAuthClaims(authEnabledReq.Context(), &middleware.AuthClaims{Roles: []string{"reader"}}),
		)
		authEnabledRec := httptest.NewRecorder()
		var r1 bool
		middleware.AdminAuthorization(true, logr.Discard())(okHandler(&r1)).ServeHTTP(authEnabledRec, authEnabledReq)

		authDisabledReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/admin/info", nil)
		authDisabledReq.RemoteAddr = "203.0.113.7:1234"
		authDisabledRec := httptest.NewRecorder()
		var r2 bool
		middleware.AdminAuthorization(false, logr.Discard())(okHandler(&r2)).ServeHTTP(authDisabledRec, authDisabledReq)

		require.Equal(t, http.StatusForbidden, authEnabledRec.Code)
		require.Equal(t, http.StatusForbidden, authDisabledRec.Code)
		assert.Equal(t, authEnabledRec.Body.String(), authDisabledRec.Body.String(),
			"denial bodies must be indistinguishable across auth configurations")
	})
}
