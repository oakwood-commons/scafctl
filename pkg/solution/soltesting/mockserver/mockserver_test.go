// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mockserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// httpGet is a test helper that performs a GET request with a background context.
func httpGet(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// httpPost is a test helper that performs a POST request with a background context.
func httpPost(t *testing.T, url, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
	require.NoError(t, err)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestNew(t *testing.T) {
	routes := []Route{
		{Path: "/test", Status: 200, Body: "ok"},
	}
	s := New(routes)
	assert.NotNil(t, s)
	assert.Len(t, s.routes, 1)
}

func TestStartStop(t *testing.T) {
	s := New(nil)
	require.NoError(t, s.Start())
	assert.Greater(t, s.Port(), 0)
	assert.Contains(t, s.BaseURL(), fmt.Sprintf(":%d", s.Port()))
	require.NoError(t, s.Stop())
}

func TestStopIdempotent(t *testing.T) {
	s := New(nil)
	assert.NoError(t, s.Stop())
}

func TestStaticRoute(t *testing.T) {
	s := New([]Route{
		{
			Path:   "/api/data",
			Method: "GET",
			Status: 200,
			Body:   "{\"key\":\"value\"}",
			Headers: map[string]string{
				"X-Custom": "test-header",
			},
		},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp := httpGet(t, fmt.Sprintf("%s/api/data", s.BaseURL()))
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "test-header", resp.Header.Get("X-Custom"))
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "{\"key\":\"value\"}", string(body))
}

func TestStaticRoutePlainText(t *testing.T) {
	s := New([]Route{
		{Path: "/hello", Body: "hello world"},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp := httpGet(t, fmt.Sprintf("%s/hello", s.BaseURL()))
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(body))
}

func TestErrorStatus(t *testing.T) {
	s := New([]Route{
		{Path: "/error", Status: 500, Body: "{\"error\":\"internal\"}"},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp := httpGet(t, fmt.Sprintf("%s/error", s.BaseURL()))
	defer resp.Body.Close()

	assert.Equal(t, 500, resp.StatusCode)
}

func TestMethodMatching(t *testing.T) {
	s := New([]Route{
		{Path: "/api", Method: "POST", Status: 201, Body: "{\"created\":true}"},
		{Path: "/api", Method: "GET", Status: 200, Body: "{\"list\":true}"},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp := httpGet(t, fmt.Sprintf("%s/api", s.BaseURL()))
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	resp2 := httpPost(t, fmt.Sprintf("%s/api", s.BaseURL()), "application/json")
	defer resp2.Body.Close()
	assert.Equal(t, 201, resp2.StatusCode)
}

func TestNoRouteMatch(t *testing.T) {
	s := New([]Route{
		{Path: "/exists", Status: 200},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp := httpGet(t, fmt.Sprintf("%s/not-found", s.BaseURL()))
	defer resp.Body.Close()

	assert.Equal(t, 404, resp.StatusCode)
}

func TestEchoRoute(t *testing.T) {
	s := New([]Route{
		{Path: "/echo", Echo: true},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp := httpPost(t, fmt.Sprintf("%s/echo?key=val", s.BaseURL()), "text/plain")
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var echo EchoResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&echo))
	assert.Equal(t, "POST", echo.Method)
	assert.Equal(t, "/echo", echo.Path)
	assert.Equal(t, "val", echo.Query["key"])
}

func TestDelay(t *testing.T) {
	s := New([]Route{
		{Path: "/slow", Delay: "100ms", Body: "delayed"},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	start := time.Now()
	resp := httpGet(t, fmt.Sprintf("%s/slow", s.BaseURL()))
	defer resp.Body.Close()
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 90*time.Millisecond)
}

func TestRequestRecording(t *testing.T) {
	s := New([]Route{
		{Path: "/record", Echo: true},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp1 := httpGet(t, fmt.Sprintf("%s/record", s.BaseURL()))
	resp1.Body.Close()

	resp2 := httpPost(t, fmt.Sprintf("%s/record", s.BaseURL()), "")
	resp2.Body.Close()

	reqs := s.Requests()
	assert.Len(t, reqs, 2)
	assert.Equal(t, "GET", reqs[0].Method)
	assert.Equal(t, "POST", reqs[1].Method)
}

func TestHealthEndpoint(t *testing.T) {
	s := New(nil)
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp := httpGet(t, fmt.Sprintf("%s/__health", s.BaseURL()))
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
}

func TestWildcardMethod(t *testing.T) {
	s := New([]Route{
		{Path: "/any", Body: "any-method"},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp := httpGet(t, fmt.Sprintf("%s/any", s.BaseURL()))
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	resp2 := httpPost(t, fmt.Sprintf("%s/any", s.BaseURL()), "")
	resp2.Body.Close()
	assert.Equal(t, 200, resp2.StatusCode)
}

func TestDefaultStatusIs200(t *testing.T) {
	s := New([]Route{
		{Path: "/default", Body: "ok"},
	})
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop() })

	resp := httpGet(t, fmt.Sprintf("%s/default", s.BaseURL()))
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
}
