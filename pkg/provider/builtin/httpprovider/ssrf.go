// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// maxResponseBodySize returns the configured maximum response body size from
// the application config, falling back to settings.DefaultMaxResponseBodySize.
func maxResponseBodySize(ctx context.Context) int64 {
	cfg := config.FromContext(ctx)
	if cfg != nil && cfg.HTTPClient.MaxResponseBodySize > 0 {
		return cfg.HTTPClient.MaxResponseBodySize
	}
	return settings.DefaultMaxResponseBodySize
}

// effectiveOrigin normalizes a URL into (scheme, hostname, port) per RFC 6454
// Section 4, treating implicit and explicit default ports (http:80, https:443)
// as identical while rejecting empty or out-of-range port numbers.
func effectiveOrigin(u *url.URL) (scheme, host, port string, ok bool) {
	if u == nil || u.Scheme == "" || u.Host == "" {
		return "", "", "", false
	}
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return "", "", "", false
	}
	scheme = strings.ToLower(u.Scheme)
	rawPort := u.Port()
	if rawPort != "" {
		n, err := strconv.Atoi(rawPort)
		if err != nil || n < 1 || n > 65535 {
			return "", "", "", false
		}
		return scheme, hostname, strconv.Itoa(n), true
	}
	switch scheme {
	case "https":
		return scheme, hostname, "443", true
	case "http":
		return scheme, hostname, "80", true
	default:
		return scheme, hostname, "", true
	}
}

// sameOriginURL reports whether two URLs share the exact same origin (scheme,
// hostname, and effective port), treating default ports (https:443, http:80) as
// equivalent to omitted ports.
func sameOriginURL(a, b *url.URL) bool {
	aScheme, aHost, aPort, aOK := effectiveOrigin(a)
	bScheme, bHost, bPort, bOK := effectiveOrigin(b)
	if !aOK || !bOK {
		return false
	}
	return aScheme == bScheme && aHost == bHost && aPort == bPort
}

// sameOriginRedirectChain reports whether target and every intermediate hop in
// via stayed on via[0]'s exact origin (scheme, hostname, and effective port).
func sameOriginRedirectChain(target *url.URL, via []*http.Request) bool {
	if len(via) == 0 || via[0] == nil {
		return false
	}
	origin := via[0].URL
	if !sameOriginURL(target, origin) {
		return false
	}
	for _, hop := range via[1:] {
		if hop == nil || !sameOriginURL(hop.URL, origin) {
			return false
		}
	}
	return true
}

// stripCrossOriginAuthOnRedirect wraps an HTTP client's CheckRedirect hook so
// Authorization headers are removed whenever a redirect leaves the initial
// request's exact origin (scheme, hostname, and effective port). Go's standard
// library redirect handling retains Authorization across subdomains, port
// changes, and https-to-http scheme downgrades.
func stripCrossOriginAuthOnRedirect(prev func(req *http.Request, via []*http.Request) error) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if req != nil && len(via) > 0 && !sameOriginRedirectChain(req.URL, via) {
			req.Header.Del("Authorization")
		}
		if prev != nil {
			return prev(req, via)
		}
		if len(via) >= httpc.DefaultMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", len(via))
		}
		return nil
	}
}

// validateNextURLHost ensures that a pagination next URL belongs to the same
// origin (scheme, hostname, and effective port) as the original request URL and
// carries no embedded userinfo. This prevents open-redirect/SSRF and credential
// leakage or injection through pagination responses.
func validateNextURLHost(originalURL, nextURL string) error {
	orig, err := url.Parse(originalURL)
	if err != nil {
		return fmt.Errorf("invalid original URL: %w", err)
	}
	origScheme, origHost, origPort, origOK := effectiveOrigin(orig)
	if !origOK {
		return fmt.Errorf("invalid original URL: missing scheme or host")
	}
	next, err := url.Parse(nextURL)
	if err != nil {
		return fmt.Errorf("invalid next URL: %w", err)
	}
	if next.User != nil {
		return fmt.Errorf("pagination next URL must not contain userinfo")
	}

	// Relative URLs (no scheme and no host, excluding network-path references
	// like //other.example.com) resolve onto the original origin.
	if next.Scheme == "" && next.Host == "" && !strings.HasPrefix(nextURL, "//") {
		return nil
	}

	if !strings.EqualFold(origScheme, next.Scheme) {
		return fmt.Errorf(
			"pagination next URL scheme %q does not match original scheme %q; scheme changes during pagination are not permitted",
			next.Scheme, orig.Scheme,
		)
	}

	_, nextHost, nextPort, nextOK := effectiveOrigin(next)
	if !nextOK || origHost != nextHost || origPort != nextPort {
		return fmt.Errorf(
			"pagination next URL host %q does not match original host %q; cross-origin pagination is not permitted",
			next.Host, orig.Host,
		)
	}
	return nil
}

// resolveNextURL validates nextURL against currentURL's origin and resolves
// relative references against currentURL while leaving absolute URLs intact.
func resolveNextURL(currentURL, nextURL string) (string, error) {
	if err := validateNextURLHost(currentURL, nextURL); err != nil {
		return "", err
	}
	next, err := url.Parse(nextURL)
	if err != nil {
		return "", fmt.Errorf("invalid next URL: %w", err)
	}
	if next.Scheme == "" && next.Host == "" {
		orig, origErr := url.Parse(currentURL)
		if origErr != nil {
			return "", fmt.Errorf("invalid original URL: %w", origErr)
		}
		return orig.ResolveReference(next).String(), nil
	}
	return nextURL, nil
}
