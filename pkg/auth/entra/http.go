package entra

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPClient interface for token endpoint requests.
// Abstracted for testing.
type HTTPClient interface {
	PostForm(ctx context.Context, endpoint string, data url.Values) (*http.Response, error)
}

// DefaultHTTPClient implements HTTPClient using standard library.
type DefaultHTTPClient struct {
	client *http.Client
}

// NewDefaultHTTPClient creates a new default HTTP client.
func NewDefaultHTTPClient() *DefaultHTTPClient {
	return &DefaultHTTPClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
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
