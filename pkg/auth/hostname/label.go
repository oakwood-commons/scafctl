// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"net/url"
	"sort"
	"strings"
)

// AliasForURL reverse-maps a concrete endpoint URL back to its configured alias
// selector. Aliases is the static selector->URL map from
// auth.handlers.<name>.hostname.aliases. Matching is by normalized host (scheme,
// port, and path are ignored). When several selectors map to the same host it
// returns the lexicographically-first one for deterministic output. It returns
// ("", false) when no alias matches.
func AliasForURL(aliases map[string]string, rawURL string) (string, bool) {
	if len(aliases) == 0 || strings.TrimSpace(rawURL) == "" {
		return "", false
	}
	target := normalizeHost(rawURL)
	if target == "" {
		return "", false
	}
	var matches []string
	for selector, u := range aliases {
		if normalizeHost(u) == target {
			matches = append(matches, selector)
		}
	}
	if len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)
	return matches[0], true
}

// DisplayHostFromURL trims a URL down to a compact host label for display,
// stripping the scheme, port, and any path. A bare hostname is returned
// lower-cased; an unparseable value is returned trimmed but otherwise unchanged.
func DisplayHostFromURL(rawURL string) string {
	if h := normalizeHost(rawURL); h != "" {
		return h
	}
	return strings.TrimSpace(rawURL)
}

// normalizeHost extracts the lower-cased host (without port) from a URL or bare
// hostname. It returns "" when no host can be determined.
func normalizeHost(rawURL string) string {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
		return strings.ToLower(u.Hostname())
	}
	// Fallback: strip scheme prefix, then trailing path/port manually.
	s = strings.TrimSpace(rawURL)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexAny(s, "/:"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}
