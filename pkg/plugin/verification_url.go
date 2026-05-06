// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateVerificationURI validates a device code verification URI against
// security requirements and an optional trusted domain list. This is the
// host-side defense against malicious plugins sending phishing URLs in
// device code prompts.
//
// Rules:
//   - Must be a valid URL with HTTPS scheme
//   - Must not target private/loopback addresses
//   - Must not use non-standard ports (only 443 allowed)
//   - If trusted is non-empty, host must match or be a subdomain of an entry
//   - If trusted is empty, any HTTPS URL passing the above checks is allowed
func ValidateVerificationURI(uri string, trusted []string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid verification URI: %w", err)
	}

	if parsed.Scheme != "https" {
		return fmt.Errorf("verification URI must use HTTPS: %q", uri)
	}

	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "" {
		return fmt.Errorf("verification URI has empty hostname: %q", uri)
	}

	if isPrivateOrLoopback(hostname) {
		return fmt.Errorf("verification URI must not target private network: %q", uri)
	}

	if port := parsed.Port(); port != "" && port != "443" {
		return fmt.Errorf("verification URI uses non-standard port: %q", uri)
	}

	// If trusted list is empty, allow any HTTPS URL (backward compat).
	if len(trusted) == 0 {
		return nil
	}

	for _, domain := range trusted {
		normDomain := strings.ToLower(strings.TrimSuffix(domain, "."))
		if hostname == normDomain || strings.HasSuffix(hostname, "."+normDomain) {
			return nil
		}
	}

	return fmt.Errorf("verification URI %q not in trusted domains %v", uri, trusted)
}

// isPrivateOrLoopback returns true if the host is "localhost" or is a literal
// IP address in private, loopback, or link-local ranges. It does NOT perform
// DNS resolution, so hostnames (other than "localhost") that resolve to
// private/loopback addresses will not be caught. This is a defense-in-depth
// check for IP-literal URIs; DNS-rebinding attacks are mitigated at the
// transport layer via SSRF-safe HTTP clients.
func isPrivateOrLoopback(host string) bool {
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}
