// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// privateIPsAllowed delegates to httpc.PrivateIPsAllowed.
// Defaults to false (deny) when no config is present — secure by default.
func privateIPsAllowed(ctx context.Context) bool {
	return httpc.PrivateIPsAllowed(ctx)
}

// validateURLNotPrivate delegates to httpc.ValidateURLNotPrivate.
func validateURLNotPrivate(rawURL string) error {
	return httpc.ValidateURLNotPrivate(rawURL)
}

// maxResponseBodySize returns the configured maximum response body size from
// the application config, falling back to settings.DefaultMaxResponseBodySize.
func maxResponseBodySize(ctx context.Context) int64 {
	cfg := config.FromContext(ctx)
	if cfg != nil && cfg.HTTPClient.MaxResponseBodySize > 0 {
		return cfg.HTTPClient.MaxResponseBodySize
	}
	return settings.DefaultMaxResponseBodySize
}

// validateNextURLHost ensures that a pagination next URL belongs to the same host as the
// original request URL. This prevents open-redirect/SSRF through pagination responses
// (e.g., a malicious Link header pointing to an internal endpoint).
func validateNextURLHost(originalURL, nextURL string) error {
	orig, err := url.Parse(originalURL)
	if err != nil {
		return fmt.Errorf("invalid original URL: %w", err)
	}
	next, err := url.Parse(nextURL)
	if err != nil {
		return fmt.Errorf("invalid next URL: %w", err)
	}

	// Relative URLs (no host component) are always safe.
	if next.Host == "" {
		return nil
	}

	if !strings.EqualFold(orig.Hostname(), next.Hostname()) {
		return fmt.Errorf(
			"pagination next URL host %q does not match original host %q; cross-domain pagination is not permitted",
			next.Host, orig.Host,
		)
	}
	return nil
}
