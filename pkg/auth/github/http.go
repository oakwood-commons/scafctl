// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
)

// HTTPClient abstracts HTTP calls for testability.
type HTTPClient interface {
	// PostForm sends a POST request with form-encoded body and returns the response.
	PostForm(ctx context.Context, url string, data url.Values) (*http.Response, error)

	// PostJSON sends a POST request with JSON body and custom headers.
	PostJSON(ctx context.Context, url string, body any, headers map[string]string) (*http.Response, error)

	// Get sends a GET request with the given headers and returns the response.
	Get(ctx context.Context, url string, headers map[string]string) (*http.Response, error)
}

// DefaultHTTPClient is the standard HTTP client implementation.
type DefaultHTTPClient struct {
	client *httpc.Client
}

// NewDefaultHTTPClient creates a new DefaultHTTPClient backed by httpc.
// Caching is disabled because token-exchange responses must never be served from cache.
// The logger parameter controls HTTP-level logging; pass logr.Discard() to suppress.
func NewDefaultHTTPClient(logger logr.Logger) *DefaultHTTPClient {
	return &DefaultHTTPClient{
		client: httpc.NewClient(&httpc.ClientConfig{
			Timeout:           30 * time.Second,
			RetryMax:          3,
			RetryWaitMin:      1 * time.Second,
			RetryWaitMax:      30 * time.Second,
			EnableCache:       false,
			EnableCompression: false,
			Logger:            logger,
		}),
	}
}

// PostForm sends a POST request with form-encoded body.
func (c *DefaultHTTPClient) PostForm(ctx context.Context, reqURL string, data url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating POST request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.URL.RawQuery = data.Encode()
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

// PostJSON sends a POST request with a JSON body and custom headers.
func (c *DefaultHTTPClient) PostJSON(ctx context.Context, reqURL string, body any, headers map[string]string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling JSON body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating POST request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.client.Do(req)
}
