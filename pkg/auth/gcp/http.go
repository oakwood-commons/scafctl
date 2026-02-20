// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
)

// HTTPClient abstracts HTTP calls for testability.
type HTTPClient interface {
	// PostForm sends a POST request with form-encoded body and returns the response.
	PostForm(ctx context.Context, url string, data url.Values) (*http.Response, error)

	// Get sends a GET request with the given headers and returns the response.
	Get(ctx context.Context, url string, headers map[string]string) (*http.Response, error)

	// Do sends an arbitrary HTTP request.
	Do(ctx context.Context, req *http.Request) (*http.Response, error)
}

// DefaultHTTPClient is the standard HTTP client implementation.
type DefaultHTTPClient struct {
	client *httpc.Client
}

// NewDefaultHTTPClient creates a new DefaultHTTPClient backed by httpc.
// Caching is disabled because metadata-server responses must never be served from cache.
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

// PostForm sends a POST request with form-encoded body.
func (c *DefaultHTTPClient) PostForm(ctx context.Context, endpoint string, data url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating POST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.client.Do(req)
}

// Get sends a GET request with custom headers.
func (c *DefaultHTTPClient) Get(ctx context.Context, reqURL string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating GET request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.client.Do(req)
}

// Do sends an arbitrary HTTP request.
func (c *DefaultHTTPClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.WithContext(ctx)
	return c.client.Do(req)
}

// PostJSON sends a POST request with JSON body.
func (c *DefaultHTTPClient) PostJSON(ctx context.Context, endpoint string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("creating POST JSON request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.client.Do(req)
}
