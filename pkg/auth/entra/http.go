// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
)

// HTTPClient interface for token endpoint requests.
// Abstracted for testing.
type HTTPClient interface {
	PostForm(ctx context.Context, endpoint string, data url.Values) (*http.Response, error)
}

// GraphClient handles authenticated GET requests to the Microsoft Graph API.
// It is separate from HTTPClient because Graph calls require a Bearer token
// and have different retry/caching semantics.
type GraphClient interface {
	Get(ctx context.Context, url, bearerToken string) (*http.Response, error)
}

// DefaultGraphClient implements GraphClient using httpc.
type DefaultGraphClient struct {
	client *httpc.Client
}

// NewDefaultGraphClient creates a new Graph API HTTP client.
// Caching is disabled because membership responses must always be fresh.
func NewDefaultGraphClient() *DefaultGraphClient {
	return &DefaultGraphClient{
		client: httpc.NewClient(&httpc.ClientConfig{
			Timeout:           30 * time.Second,
			RetryMax:          3,
			RetryWaitMin:      1 * time.Second,
			RetryWaitMax:      30 * time.Second,
			EnableCache:       false,
			EnableCompression: false,
		}),
	}
}

// Get performs an authenticated GET request against the Microsoft Graph API.
func (c *DefaultGraphClient) Get(ctx context.Context, url, bearerToken string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Accept", "application/json")
	return c.client.Do(req)
}

// DefaultHTTPClient implements HTTPClient using httpc.
type DefaultHTTPClient struct {
	client *httpc.Client
}

// NewDefaultHTTPClient creates a new default HTTP client backed by httpc.
// Caching is disabled because token-exchange responses must never be served from cache.
func NewDefaultHTTPClient() *DefaultHTTPClient {
	return &DefaultHTTPClient{
		client: httpc.NewClient(&httpc.ClientConfig{
			Timeout:           30 * time.Second,
			RetryMax:          3,
			RetryWaitMin:      1 * time.Second,
			RetryWaitMax:      30 * time.Second,
			EnableCache:       false,
			EnableCompression: false,
		}),
	}
}

// PostForm performs a POST request with form-encoded data.
func (c *DefaultHTTPClient) PostForm(ctx context.Context, endpoint string, data url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.client.Do(req)
}
