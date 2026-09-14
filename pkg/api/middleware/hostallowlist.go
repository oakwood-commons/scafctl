// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
)

// hostForbiddenBody is the problem+json payload returned when a request's Host
// header is not in the configured allowlist.
const hostForbiddenBody = `{"title":"Forbidden","status":403,"detail":"host not allowed"}`

// reasonWildcardAll is the disabledReason reported when the allowlist contains
// a bare "*", the documented explicit opt-out. It is kept distinct from every
// other reason because it is an intentional choice rather than a
// misconfiguration, and so must not trigger the fail-closed path.
const reasonWildcardAll = `wildcard entry "*" accepts all hosts`

// HostAllowlist rejects requests whose Host header is not in allowedHosts.
//
// This defends against DNS rebinding: an attacker who controls a domain can
// point it at the server's address, causing a victim's browser to send requests
// to this server while the browser still treats the attacker's origin as the
// page origin. Checking the Host header ensures the server only answers to the
// names it was configured to serve.
//
// An EMPTY allowedHosts disables the check entirely and every Host is accepted.
// That is the default, chosen so enabling this feature cannot break an existing
// deployment that sits behind a proxy or load balancer using an unexpected name.
// Operators exposing the server publicly should set it.
//
// Matching rules:
//   - Comparison is case-insensitive and ignores the port, since the port is not
//     part of the server's identity for this purpose.
//   - A leading "*." entry matches any single-or-multi label subdomain at any
//     depth, e.g. "*.example.com" matches "api.example.com" and
//     "a.b.example.com". It does NOT match the bare apex "example.com" -- the
//     same semantics nginx's wildcard server_name uses. (Note this is
//     deliberately broader than TLS certificate wildcards, where "*.example.com"
//     covers exactly one label and would reject "a.b.example.com".) List the
//     apex explicitly if the server answers to it too.
//   - An entry of "*" accepts any host, equivalent to disabling the check.
func HostAllowlist(allowedHosts []string, lgr logr.Logger) func(http.Handler) http.Handler {
	normalized, disabledReason := normalizeAllowedHosts(allowedHosts)

	// A non-empty config that normalizes to nothing means the operator asked for
	// the check to be ON but supplied only unusable entries. config
	// .APIServerConfig.Validate rejects that at startup, so reaching here means
	// a caller built the middleware directly and bypassed config validation.
	// Fail CLOSED: an explicitly configured security control must never
	// silently degrade into no control at all.
	//
	// A bare "*" is different -- it is the documented explicit opt-out -- so it
	// still disables the check, with a warning so the choice stays visible.
	failClosed := false
	switch {
	case len(allowedHosts) == 0 || disabledReason == "":
		// Unset (check disabled by default) or fully usable: nothing to report.
	case disabledReason == reasonWildcardAll:
		lgr.Info("WARNING: host allowlist contains \"*\"; every Host header will be accepted",
			"reason", disabledReason, "configured", allowedHosts)
	default:
		failClosed = true
		lgr.Info("ERROR: host allowlist is configured but has no usable entries; rejecting ALL requests",
			"reason", disabledReason, "configured", allowedHosts)
	}

	return func(next http.Handler) http.Handler {
		if failClosed {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				lgr.Info("request rejected: host allowlist has no usable entries",
					"host", r.Host, "path", r.URL.Path, "remoteAddr", r.RemoteAddr)
				writeHostForbidden(w)
			})
		}

		// No allowlist configured: pass everything through without per-request
		// work, preserving the pre-existing behaviour.
		if len(normalized) == 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hostAllowed(r.Host, normalized) {
				lgr.Info("request rejected: host not in allowlist",
					"host", r.Host, "path", r.URL.Path, "remoteAddr", r.RemoteAddr)
				writeHostForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeHostForbidden emits the problem+json 403 used for every Host rejection.
func writeHostForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(hostForbiddenBody))
}

// ValidateAllowedHosts reports whether a configured allowlist is usable.
//
// It returns an error when the operator supplied entries but every one of them
// is blank or malformed, because that combination would otherwise leave an
// explicitly requested security control doing nothing. Callers wire this into
// configuration validation so the deployment fails at startup rather than
// running unprotected. An empty allowlist is valid -- it is the documented
// default that disables the check -- as is a bare "*", the explicit opt-out.
func ValidateAllowedHosts(allowedHosts []string) error {
	if len(allowedHosts) == 0 {
		return nil
	}
	if _, reason := normalizeAllowedHosts(allowedHosts); reason != "" && reason != reasonWildcardAll {
		return fmt.Errorf("%s; use an empty list to disable the check or \"*\" to accept every Host", reason)
	}
	return nil
}

// normalizeAllowedHosts lowercases entries, strips any port and trailing dot,
// and drops blanks. It returns the normalized list plus a non-empty reason when
// the result disables the check despite entries having been supplied.
//
// A bare "*" disables the check outright. The malformed entry "*." is rejected
// rather than normalized, because it would otherwise reduce to a suffix match
// on "." and accept every fully-qualified host.
func normalizeAllowedHosts(allowedHosts []string) (normalized []string, disabledReason string) {
	normalized = make([]string, 0, len(allowedHosts))
	for _, h := range allowedHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if h == "*" {
			return nil, reasonWildcardAll
		}
		if h == "*." {
			continue
		}
		normalized = append(normalized, stripPort(h))
	}
	if len(normalized) == 0 {
		return nil, "no usable entries after normalization"
	}
	return normalized, ""
}

// hostAllowed reports whether host matches any entry in the normalized allowlist.
func hostAllowed(host string, allowed []string) bool {
	host = stripPort(strings.ToLower(strings.TrimSpace(host)))
	if host == "" {
		return false
	}

	for _, entry := range allowed {
		if suffix, ok := strings.CutPrefix(entry, "*."); ok {
			// "*.example.com" matches subdomains at any depth
			// ("api.example.com", "a.b.example.com") but NOT the bare apex
			// "example.com" -- the same semantics nginx's wildcard server_name
			// uses, and deliberately broader than a TLS certificate wildcard,
			// which covers exactly one label. An operator who also serves
			// the apex lists it explicitly, so the allowlist never grants a
			// host the operator did not name.
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
			continue
		}
		if host == entry {
			return true
		}
	}
	return false
}

// stripPort removes a trailing ":port" from a host value, tolerating bracketed
// IPv6 literals and values that carry no port at all. A single trailing dot is
// also removed so an absolute FQDN ("api.example.com.") compares equal to the
// configured relative form.
func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else {
		// No port present; drop IPv6 brackets so "[::1]" compares equal to "::1".
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if len(host) > 1 {
		host = strings.TrimSuffix(host, ".")
	}
	return host
}
