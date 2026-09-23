// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net"
	"net/http"
	"slices"

	"github.com/go-logr/logr"
)

// AdminRole is the JWT role claim required to reach admin endpoints when
// authentication is enabled.
const AdminRole = "admin"

// adminForbiddenBody is the problem+json payload returned when an admin
// authorization check fails. The message is deliberately identical for every
// denial reason so a caller cannot probe the server's auth configuration by
// comparing responses.
const adminForbiddenBody = `{"title":"Forbidden","status":403,"detail":"admin access required"}`

// AdminAuthorization enforces the admin access-control policy documented in
// docs/design/api-surface.md:
//
//	| Auth State          | Admin Access                            |
//	| Entra OIDC enabled  | Requires "admin" role in JWT claims      |
//	| Auth disabled       | Localhost-only (non-loopback -> 403)     |
//
// Attach it to the admin path prefix only, and AFTER the authentication
// middleware so validated claims are present in the request context.
//
// When authEnabled is true the caller has already been authenticated upstream
// (a missing or invalid token yields 401 there), so this middleware only makes
// the authorization decision and returns 403.
func AdminAuthorization(authEnabled bool, lgr logr.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authEnabled {
				claims := ClaimsFromContext(r.Context())
				// A nil claims value means the authentication middleware did not
				// run before this one. Deny rather than fall through, so a
				// middleware-ordering mistake cannot silently open admin routes.
				if claims == nil {
					lgr.Info("admin access denied: no claims in context",
						"path", r.URL.Path, "reason", "auth_middleware_not_run")
					writeAdminForbidden(w)
					return
				}
				if !slices.Contains(claims.Roles, AdminRole) {
					lgr.Info("admin access denied: missing role",
						"path", r.URL.Path, "subject", claims.Subject, "requiredRole", AdminRole)
					writeAdminForbidden(w)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Auth disabled: restrict admin routes to loopback callers.
			if !isLoopbackRequest(r) {
				lgr.Info("admin access denied: non-loopback caller with auth disabled",
					"path", r.URL.Path, "remoteAddr", r.RemoteAddr)
				writeAdminForbidden(w)
				return
			}
			// A loopback peer address is only meaningful if this server is the
			// first hop. Behind a same-host reverse proxy -- the very deployment
			// the startup exposure warning recommends -- every forwarded request
			// arrives from 127.0.0.1, which would make this gate accept the whole
			// internet. Proxy headers prove a hop occurred, so their presence
			// DENIES. They are never allowed to grant access, so a spoofed header
			// can only lock an attacker out, never let them in.
			if hop := proxyHopHeader(r); hop != "" {
				lgr.Info("admin access denied: loopback caller carrying proxy headers with auth disabled",
					"path", r.URL.Path, "remoteAddr", r.RemoteAddr, "header", hop)
				writeAdminForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// proxyHeaders are set by reverse proxies to record the original client. Their
// presence means this server is not the first hop, so a loopback peer address
// no longer proves the caller is local.
var proxyHeaders = []string{ //nolint:gochecknoglobals // fixed lookup table
	"X-Forwarded-For",
	"Forwarded",
	"X-Real-IP",
}

// proxyHopHeader returns the name of the first proxy header present on r, or ""
// if the request carries none.
//
// Presence is tested with Header.Values, not Header.Get: Get returns "" both for
// an absent header and for one sent with an empty value, so it cannot express
// the presence question this policy asks. An empty-valued header is trivially
// sendable -- `curl -H "X-Forwarded-For;"`, or any client writing the bare
// `X-Forwarded-For:` line -- and reaches the handler as []string{""}. A Get-based
// check would read that as "no proxy hop" and fall through to the loopback
// grant, which is the whole hole this gate exists to close.
func proxyHopHeader(r *http.Request) string {
	for _, h := range proxyHeaders {
		if len(r.Header.Values(h)) > 0 {
			return h
		}
	}
	return ""
}

// writeAdminForbidden emits the standard 403 problem+json response.
func writeAdminForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(adminForbiddenBody))
}

// isLoopbackRequest reports whether the request originated from a loopback
// address.
//
// It reads only r.RemoteAddr, the peer address observed by the server. Proxy
// headers (X-Forwarded-For, X-Real-IP) are deliberately ignored: they are
// attacker-controlled, and honouring them here would let any remote caller
// claim to be localhost and bypass the admin gate entirely.
func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr without a port (some test servers and unix sockets).
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
