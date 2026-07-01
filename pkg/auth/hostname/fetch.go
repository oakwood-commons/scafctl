// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// maxInventoryBytes caps the inventory response body read to guard against
// oversized or malicious responses. Real inventories are tens of KB.
const maxInventoryBytes = 8 << 20 // 8 MiB

// defaultFetch retrieves the inventory body over HTTP(S) using the scafctl
// HTTP client, injecting static headers and an optional bearer token.
func defaultFetch(ctx context.Context, src config.HostnameResolverSource, bearer string) ([]byte, error) {
	u, err := url.Parse(src.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("invalid inventory source URL %q: must be an absolute http(s) URL", src.URL)
	}

	// Refuse to leak a bearer token over plaintext HTTP. Loopback hosts are
	// exempt since the token never leaves the machine.
	if bearer != "" && u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("refusing to send bearer token to non-HTTPS inventory URL %q: use https", src.URL)
	}

	var httpCfg *config.HTTPClientConfig
	if c := config.FromContext(ctx); c != nil {
		httpCfg = &c.HTTPClient
	}

	lg := logr.Discard()
	if l := logger.FromContext(ctx); l != nil {
		lg = *l
	}
	client := httpc.NewClientFromAppConfig(httpCfg, lg)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating inventory request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range src.Headers {
		req.Header.Set(k, v)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := client.Do(req) //nolint:gosec // URL from trusted admin config (auth.handlers.<name>.hostname.resolver.source.url)
	if err != nil {
		return nil, fmt.Errorf("inventory request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxInventoryBytes))
	if err != nil {
		return nil, fmt.Errorf("reading inventory response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inventory endpoint returned status %d", resp.StatusCode)
	}
	return body, nil
}

// isLoopbackHost reports whether host is localhost or a loopback IP literal.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
