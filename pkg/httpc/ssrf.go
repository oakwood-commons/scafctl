// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// privateIPNets holds the CIDR blocks considered private/reserved.
// Requests to IP literals within these ranges are blocked when AllowPrivateIPs is false.
var privateIPNets = buildPrivateIPNets() //nolint:gochecknoglobals

func buildPrivateIPNets() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"127.0.0.0/8",    // IPv4 loopback
		"169.254.0.0/16", // IPv4 link-local / cloud metadata (AWS, GCP, Azure IMDS)
		"100.64.0.0/10",  // RFC 6598 shared address space (CGNAT)
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			nets = append(nets, network)
		}
	}
	return nets
}

// PrivateIPsAllowed returns true if the config permits requests to private IP
// addresses. Defaults to false (deny) when no config is present in the context,
// enforcing secure-by-default behaviour.
func PrivateIPsAllowed(ctx context.Context) bool {
	cfg := config.FromContext(ctx)
	if cfg == nil || cfg.HTTPClient.AllowPrivateIPs == nil {
		return false
	}
	return *cfg.HTTPClient.AllowPrivateIPs
}

// nonCanonicalIPPattern matches numeric-only, hex-prefixed, or octal-style
// host strings that some HTTP stacks interpret as IP addresses but that Go's
// net.ParseIP does not recognise. Blocking these prevents SSRF bypass via
// non-canonical IP representations (e.g. 2130706433 → 127.0.0.1).
var nonCanonicalIPPattern = regexp.MustCompile( //nolint:gochecknoglobals
	`^(?:0[xX][0-9a-fA-F]+|` + // hex: 0x7f000001
		`[0-9]+|` + // decimal: 2130706433
		`0[0-7]+(?:\.[0-7]+){0,3})$`, // octal: 0177.0.0.1
)

// blockedHostnames contains well-known hostnames that resolve to private or
// metadata IP addresses. Unlike arbitrary hostnames we cannot (and should not)
// DNS-resolve, these are static aliases that always point to loopback or cloud
// metadata endpoints. Blocking them prevents trivial SSRF bypasses such as
// http://localhost/... or http://metadata.google.internal/...
var blockedHostnames = map[string]struct{}{ //nolint:gochecknoglobals
	"localhost":                {},
	"localhost.localdomain":    {},
	"metadata.google.internal": {}, // GCP instance metadata
}

// ValidateURLNotPrivate returns an error if rawURL's host is an IP literal
// that falls within a private, loopback, link-local, or CGNAT range, or if
// the hostname is a well-known alias for a private/metadata address.
//
// Non-canonical IP forms (decimal, hex, octal) that net.ParseIP does not
// recognise are also rejected, because some HTTP stacks silently convert
// them to standard IPs.
//
// Arbitrary hostnames are not pre-resolved because DNS lookups introduce
// TOCTOU races and are not appropriate for a CLI tool. However, well-known
// hostnames (localhost, metadata.google.internal) are blocked because they
// have static, predictable resolutions.
func ValidateURLNotPrivate(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		return nil
	}

	// Block well-known hostnames that always resolve to private/metadata IPs.
	if _, blocked := blockedHostnames[strings.ToLower(host)]; blocked {
		return fmt.Errorf(
			"request to blocked hostname %q is denied (resolves to a private/metadata address); "+
				"to allow private network access set httpClient.allowPrivateIPs: true in your scafctl config",
			host,
		)
	}

	// Reject non-canonical IP representations that net.ParseIP won't catch.
	if nonCanonicalIPPattern.MatchString(host) {
		return fmt.Errorf(
			"request to non-canonical IP literal %q is blocked (potential SSRF bypass); "+
				"use a standard dotted-decimal or bracketed IPv6 address instead",
			host,
		)
	}

	// Only check IP literals; plain hostnames pass through.
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}

	for _, network := range privateIPNets {
		if network.Contains(ip) {
			return fmt.Errorf(
				"request to private/reserved IP address %s is blocked (httpClient.allowPrivateIPs is false); "+
					"to allow private network access set httpClient.allowPrivateIPs: true in your scafctl config",
				ip,
			)
		}
	}

	return nil
}
