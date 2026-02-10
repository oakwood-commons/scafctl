// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsTransport_RoundTrip(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		expectedStatus int
	}{
		{
			name: "successful request",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("success"))
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "server error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("error"))
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "client error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("bad request"))
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			// Create metrics transport wrapping default transport
			transport := newMetricsTransport(http.DefaultTransport)

			// Create a request
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
			require.NoError(t, err)

			// Execute request
			resp, err := transport.RoundTrip(req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer resp.Body.Close()

			// Verify response
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
		})
	}
}

func TestCategorizeError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		resp         *http.Response
		expectedType string
	}{
		{
			name:         "no error",
			err:          nil,
			resp:         nil,
			expectedType: "none",
		},
		{
			name:         "client error 4xx",
			err:          errors.New("bad request"),
			resp:         &http.Response{StatusCode: 400},
			expectedType: "client_error",
		},
		{
			name:         "server error 5xx",
			err:          errors.New("internal server error"),
			resp:         &http.Response{StatusCode: 500},
			expectedType: "server_error",
		},
		{
			name:         "context canceled",
			err:          context.Canceled,
			resp:         nil,
			expectedType: "context_canceled",
		},
		{
			name:         "context deadline exceeded",
			err:          context.DeadlineExceeded,
			resp:         nil,
			expectedType: "context_timeout",
		},
		{
			name:         "network timeout",
			err:          &testTimeoutError{},
			resp:         nil,
			expectedType: "network_timeout",
		},
		{
			name:         "connection refused",
			err:          &net.OpError{Err: syscall.ECONNREFUSED},
			resp:         nil,
			expectedType: "connection_refused",
		},
		{
			name:         "dns error",
			err:          &net.DNSError{Err: "no such host"},
			resp:         nil,
			expectedType: "dns_error",
		},
		{
			name:         "unknown error",
			err:          errors.New("some unknown error"),
			resp:         nil,
			expectedType: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorType := categorizeError(tt.err, tt.resp)
			assert.Equal(t, tt.expectedType, errorType)
		})
	}
}

func TestCategorizeError_Priority(t *testing.T) {
	// Test that HTTP errors take priority over other error types
	t.Run("HTTP error takes priority over context error", func(t *testing.T) {
		resp := &http.Response{StatusCode: 500}
		err := context.Canceled
		errorType := categorizeError(err, resp)
		assert.Equal(t, "server_error", errorType)
	})

	t.Run("HTTP 404 categorized as client error", func(t *testing.T) {
		resp := &http.Response{StatusCode: 404}
		err := errors.New("not found")
		errorType := categorizeError(err, resp)
		assert.Equal(t, "client_error", errorType)
	})

	t.Run("HTTP 503 categorized as server error", func(t *testing.T) {
		resp := &http.Response{StatusCode: 503}
		err := errors.New("service unavailable")
		errorType := categorizeError(err, resp)
		assert.Equal(t, "server_error", errorType)
	})
}

func TestMetricsTransport_WithContextTimeout(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Create metrics transport
	transport := newMetricsTransport(http.DefaultTransport)

	// Create request with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	// Execute request (should timeout)
	resp, err := transport.RoundTrip(req)

	// Should have an error
	assert.Error(t, err)

	// Response may be nil due to timeout
	if resp != nil {
		resp.Body.Close()
	}
}

func TestMetricsTransport_WithDifferentMethods(t *testing.T) {
	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, method, r.Method)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			transport := newMetricsTransport(http.DefaultTransport)
			req, err := http.NewRequestWithContext(context.Background(), method, server.URL, nil)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

// testTimeoutError is a mock net.Error that represents a timeout
type testTimeoutError struct{}

func (e *testTimeoutError) Error() string   { return "timeout" }
func (e *testTimeoutError) Timeout() bool   { return true }
func (e *testTimeoutError) Temporary() bool { return true }

func TestParameterizePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "root path",
			path:     "/",
			expected: "/",
		},
		{
			name:     "static path",
			path:     "/api/users",
			expected: "/api/users",
		},
		{
			name:     "integer ID",
			path:     "/api/users/123",
			expected: "/api/users/{id}",
		},
		{
			name:     "multiple integer IDs",
			path:     "/api/users/123/posts/456",
			expected: "/api/users/{id}/posts/{id}",
		},
		{
			name:     "UUID v4",
			path:     "/api/users/550e8400-e29b-41d4-a716-446655440000",
			expected: "/api/users/{id}",
		},
		{
			name:     "UUID in middle of path",
			path:     "/api/users/550e8400-e29b-41d4-a716-446655440000/profile",
			expected: "/api/users/{id}/profile",
		},
		{
			name:     "SHA-1 hash (40 chars)",
			path:     "/repos/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b/commits",
			expected: "/repos/{hash}/commits",
		},
		{
			name:     "SHA-256 hash (64 chars)",
			path:     "/blobs/e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			expected: "/blobs/{hash}",
		},
		{
			name:     "mixed static and dynamic",
			path:     "/api/v1/users/123/posts/456",
			expected: "/api/v1/users/{id}/posts/{id}",
		},
		{
			name:     "no false positive on version",
			path:     "/api/v1/users",
			expected: "/api/v1/users",
		},
		{
			name:     "no false positive on oauth2",
			path:     "/api/oauth2/callback",
			expected: "/api/oauth2/callback",
		},
		{
			name:     "integer at start",
			path:     "/123/resource",
			expected: "/{id}/resource",
		},
		{
			name:     "complex real-world path",
			path:     "/repos/owner/repo/pulls/42/files",
			expected: "/repos/owner/repo/pulls/{id}/files",
		},
		{
			name:     "GitHub release by ID",
			path:     "/repos/owner/repo/releases/87654321",
			expected: "/repos/owner/repo/releases/{id}",
		},
		{
			name:     "Git commit SHA",
			path:     "/repos/owner/repo/commits/abc123def456789012345678901234567890abcd",
			expected: "/repos/owner/repo/commits/{hash}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parameterizePath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractMetricLabels(t *testing.T) {
	tests := []struct {
		name         string
		urlStr       string
		expectedHost string
		expectedPath string
	}{
		{
			name:         "simple http URL",
			urlStr:       "http://example.com/api/users",
			expectedHost: "example.com",
			expectedPath: "/api/users",
		},
		{
			name:         "https with default port",
			urlStr:       "https://example.com:443/api/users",
			expectedHost: "example.com",
			expectedPath: "/api/users",
		},
		{
			name:         "http with default port",
			urlStr:       "http://example.com:80/api/users",
			expectedHost: "example.com",
			expectedPath: "/api/users",
		},
		{
			name:         "non-standard port",
			urlStr:       "http://example.com:8080/api/users",
			expectedHost: "example.com:8080",
			expectedPath: "/api/users",
		},
		{
			name:         "https with non-standard port",
			urlStr:       "https://example.com:8443/api/users",
			expectedHost: "example.com:8443",
			expectedPath: "/api/users",
		},
		{
			name:         "with query parameters",
			urlStr:       "https://example.com/api/users?page=1&limit=10",
			expectedHost: "example.com",
			expectedPath: "/api/users",
		},
		{
			name:         "with integer ID in path",
			urlStr:       "https://example.com/api/users/123",
			expectedHost: "example.com",
			expectedPath: "/api/users/{id}",
		},
		{
			name:         "with UUID in path",
			urlStr:       "https://api.github.com/repos/550e8400-e29b-41d4-a716-446655440000/releases",
			expectedHost: "api.github.com",
			expectedPath: "/repos/{id}/releases",
		},
		{
			name:         "with SHA hash",
			urlStr:       "https://api.github.com/repos/owner/repo/commits/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b",
			expectedHost: "api.github.com",
			expectedPath: "/repos/owner/repo/commits/{hash}",
		},
		{
			name:         "complex with port, ID, and query",
			urlStr:       "http://localhost:3000/api/users/456/posts?sort=date",
			expectedHost: "localhost:3000",
			expectedPath: "/api/users/{id}/posts",
		},
		{
			name:         "root path",
			urlStr:       "https://example.com/",
			expectedHost: "example.com",
			expectedPath: "/",
		},
		{
			name:         "no path",
			urlStr:       "https://example.com",
			expectedHost: "example.com",
			expectedPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.urlStr)
			require.NoError(t, err)

			host, path := extractMetricLabels(u)
			assert.Equal(t, tt.expectedHost, host, "host mismatch")
			assert.Equal(t, tt.expectedPath, path, "path mismatch")
		})
	}
}
