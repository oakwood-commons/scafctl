// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testContext returns a context with allowPrivateIPs=true so that tests using
// httptest.NewServer (which binds to 127.0.0.1) are not blocked by the SSRF guard.
func testContext(_ testing.TB) context.Context {
	cfg := &config.Config{}
	cfg.HTTPClient.AllowPrivateIPs = ptrs.BoolPtr(true)
	return config.WithConfig(context.Background(), cfg)
}

// testContextWithCLIAuth returns a test context with an auth registry containing the given handlers.
func testContextWithCLIAuth(t testing.TB, authReg *auth.Registry) context.Context {
	t.Helper()
	return auth.WithRegistry(testContext(t), authReg)
}

// testContextWithAPIIdentity returns a test context in API mode with mock auth handlers.
func testContextWithAPIIdentity(t testing.TB, handlers ...*auth.MockHandler) context.Context {
	t.Helper()
	reg := auth.NewRegistry()
	for _, h := range handlers {
		require.NoError(t, reg.Register(h))
	}
	ctx := auth.WithRegistry(testContext(t), reg)
	return ctx
}

func TestNewHTTPProvider(t *testing.T) {
	t.Parallel()
	p := NewHTTPProvider()

	require.NotNil(t, p)
	require.NotNil(t, p.Descriptor())
	assert.Equal(t, ProviderName, p.Descriptor().Name)
	assert.NotNil(t, p.Descriptor().Version)
	assert.Contains(t, p.Descriptor().Capabilities, provider.CapabilityFrom)
	assert.Contains(t, p.Descriptor().Capabilities, provider.CapabilityAction)
}

func TestHTTPProvider_Execute_GET(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)
	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)
	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
	assert.Equal(t, `{"message":"success"}`, data["body"])
	headers := data["headers"].(map[string]any)
	assert.Equal(t, "application/json", headers["Content-Type"])
}

func TestHTTPProvider_Execute_DryRun(t *testing.T) {
	t.Parallel()
	p := NewHTTPProvider()
	ctx := provider.WithDryRun(testContext(t), true)
	inputs := map[string]any{
		"url":    "https://api.example.com/test",
		"method": "GET",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)
	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
	assert.Contains(t, data["body"], "DRY-RUN")
}

func TestHTTPProvider_Execute_POST(t *testing.T) {
	t.Parallel()
	expectedBody := `{"key":"value"}`
	receivedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method
		assert.Equal(t, http.MethodPost, r.Method)

		// Read and store body
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(body)

		// Set headers before writing status code
		w.Header().Set("Content-Type", "application/json")
		// Respond with 201 Created
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"123","status":"created"}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "POST",
		"body":   expectedBody,
		"headers": map[string]any{
			"Content-Type": "application/json",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify request body was sent correctly
	assert.Equal(t, expectedBody, receivedBody)

	data := output.Data.(map[string]any)
	assert.Equal(t, 201, data["statusCode"])
	assert.Contains(t, data["body"], `"id":"123"`)
	assert.Contains(t, data["body"], `"status":"created"`)

	headers := data["headers"].(map[string]any)
	assert.Equal(t, "application/json", headers["Content-Type"])
}

func TestHTTPProvider_Execute_POST_StructuredBody(t *testing.T) {
	t.Parallel()
	receivedBody := ""
	receivedContentType := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(body)
		receivedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "POST",
		"body": map[string]any{
			"query": "graphQuery",
			"count": 3,
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// The map body is serialized to compact JSON.
	assert.JSONEq(t, `{"query":"graphQuery","count":3}`, receivedBody)
	// Content-Type is defaulted to application/json when not supplied.
	assert.Equal(t, "application/json", receivedContentType)
}

func TestHTTPProvider_Execute_POST_ArrayBody(t *testing.T) {
	t.Parallel()
	receivedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "POST",
		"body":   []any{"a", "b", "c"},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.JSONEq(t, `["a","b","c"]`, receivedBody)
}

func TestHTTPProvider_Execute_StructuredBody_RespectsExplicitContentType(t *testing.T) {
	t.Parallel()
	receivedContentType := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "POST",
		"body":   map[string]any{"query": "x"},
		// Explicit Content-Type must not be overridden (case-insensitive match).
		"headers": map[string]any{"content-type": "application/vnd.api+json"},
	}

	_, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, "application/vnd.api+json", receivedContentType)
}

func TestHTTPProvider_Execute_StringBody_NoContentTypeDefault(t *testing.T) {
	t.Parallel()
	receivedBody := ""
	receivedContentType := "sentinel"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(body)
		receivedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "POST",
		"body":   `{"raw":"string"}`,
	}

	_, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	// String bodies are sent verbatim and do not get an implicit Content-Type.
	assert.Equal(t, `{"raw":"string"}`, receivedBody)
	assert.Empty(t, receivedContentType)
}

func TestHTTPProvider_Execute_BodyExceedsMaxSize(t *testing.T) {
	t.Parallel()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    "https://api.example.com",
		"method": "POST",
		"body":   strings.Repeat("a", maxBodyBytes+1),
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request body exceeds maximum size")
}

func TestSerializeBody(t *testing.T) {
	t.Parallel()

	t.Run("nil yields empty and unstructured", func(t *testing.T) {
		t.Parallel()
		content, structured, err := serializeBody(nil)
		require.NoError(t, err)
		assert.Empty(t, content)
		assert.False(t, structured)
	})

	t.Run("string is verbatim and unstructured", func(t *testing.T) {
		t.Parallel()
		content, structured, err := serializeBody("hello")
		require.NoError(t, err)
		assert.Equal(t, "hello", content)
		assert.False(t, structured)
	})

	t.Run("map is JSON and structured", func(t *testing.T) {
		t.Parallel()
		content, structured, err := serializeBody(map[string]any{"a": 1})
		require.NoError(t, err)
		assert.JSONEq(t, `{"a":1}`, content)
		assert.True(t, structured)
	})

	t.Run("json.RawMessage is verbatim and structured", func(t *testing.T) {
		t.Parallel()
		content, structured, err := serializeBody(json.RawMessage(`{"a":1}`))
		require.NoError(t, err)
		assert.Equal(t, `{"a":1}`, content)
		assert.True(t, structured)
	})

	t.Run("byte slice is verbatim and unstructured", func(t *testing.T) {
		t.Parallel()
		content, structured, err := serializeBody([]byte("raw-bytes"))
		require.NoError(t, err)
		assert.Equal(t, "raw-bytes", content)
		assert.False(t, structured)
	})

	t.Run("unmarshalable value returns error", func(t *testing.T) {
		t.Parallel()
		content, structured, err := serializeBody(make(chan int))
		require.Error(t, err)
		assert.Empty(t, content)
		assert.False(t, structured)
	})
}

func TestHasContentType(t *testing.T) {
	t.Parallel()

	assert.True(t, hasContentType(map[string]any{"Content-Type": "application/json"}))
	assert.True(t, hasContentType(map[string]any{"content-type": "text/plain"}))
	assert.True(t, hasContentType(map[string]any{"CONTENT-TYPE": "x"}))
	assert.False(t, hasContentType(map[string]any{"Authorization": "Bearer x"}))
	assert.False(t, hasContentType(map[string]any{}))
}

func TestHTTPProvider_InputResolver_DoesNotMangleStructuredBody(t *testing.T) {
	t.Parallel()

	desc := NewHTTPProvider().Descriptor()
	r := provider.NewInputResolver(context.Background(), desc.Schema, nil)

	resolved, err := r.ResolveInputs(map[string]any{
		"url":  "https://api.example.com",
		"body": map[string]any{"query": "x"},
	})
	require.NoError(t, err)

	// The union body schema must leave a structured body as a map (not coerce it
	// into a Go "map[...]" string via fmt.Sprintf).
	body, ok := resolved["body"].(map[string]any)
	require.True(t, ok, "structured body should remain a map, got %T", resolved["body"])
	assert.Equal(t, "x", body["query"])
}

func TestHTTPProvider_Execute_CustomHeaders(t *testing.T) {
	t.Parallel()
	receivedHeaders := http.Header{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Store received headers
		receivedHeaders = r.Header.Clone()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url": server.URL,
		"headers": map[string]any{
			"Authorization": "Bearer token123",
			"X-Custom":      "custom-value",
			"X-Api-Key":     "secret-key",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify custom headers were sent
	assert.Equal(t, "Bearer token123", receivedHeaders.Get("Authorization"))
	assert.Equal(t, "custom-value", receivedHeaders.Get("X-Custom"))
	assert.Equal(t, "secret-key", receivedHeaders.Get("X-Api-Key"))

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
}

func TestHTTPProvider_Execute_PUT(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, `{"update":"data"}`, string(body))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"updated":true}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "PUT",
		"body":   `{"update":"data"}`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
	assert.Contains(t, data["body"], `"updated":true`)
}

func TestHTTPProvider_Execute_DELETE(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "DELETE",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 204, data["statusCode"])
}

func TestHTTPProvider_Execute_404(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url": server.URL,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 404, data["statusCode"])
	assert.Contains(t, data["body"], "not found")
}

func TestHTTPProvider_Execute_500(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url": server.URL,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 500, data["statusCode"])
	assert.Contains(t, data["body"], "Internal Server Error")
}

func TestHTTPProvider_Execute_Success_Default2xx(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	output, err := p.Execute(ctx, map[string]any{"url": server.URL})
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.Equal(t, true, data[fieldSuccess])
	assert.Equal(t, 200, data["statusCode"])
}

func TestHTTPProvider_Execute_Default2xx_NonSuccess(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	// Without acceptableStatusCodes a 404 must not error but success is false.
	output, err := p.Execute(ctx, map[string]any{"url": server.URL})
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.Equal(t, false, data[fieldSuccess])
	assert.Equal(t, 404, data["statusCode"])
}

func TestHTTPProvider_Execute_AcceptableStatusCodes_Allows404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	cases := []struct {
		name    string
		entries []any
	}{
		{name: "exact int", entries: []any{200, 404}},
		{name: "class shorthand", entries: []any{"2xx", "4xx"}},
		{name: "range", entries: []any{"400-404"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := p.Execute(ctx, map[string]any{
				"url":                      server.URL,
				fieldAcceptableStatusCodes: tc.entries,
			})
			require.NoError(t, err)
			data := output.Data.(map[string]any)
			assert.Equal(t, true, data[fieldSuccess])
			assert.Equal(t, 404, data["statusCode"])
		})
	}
}

func TestHTTPProvider_Execute_AcceptableStatusCodes_ErrorsOnUnacceptable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.Execute(ctx, map[string]any{
		"url":                      server.URL,
		fieldAcceptableStatusCodes: []any{200, "2xx"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not in acceptableStatusCodes")
}

func TestHTTPProvider_Execute_AcceptableStatusCodes_InvalidConfig(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.Execute(ctx, map[string]any{
		"url":                      server.URL,
		fieldAcceptableStatusCodes: []any{"not-a-code"},
	})
	require.Error(t, err)
}

func TestHTTPProvider_Execute_MultipleHeaderValues(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send multiple Set-Cookie headers
		w.Header().Add("Set-Cookie", "session=abc123")
		w.Header().Add("Set-Cookie", "token=xyz789")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url": server.URL,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])

	headers := data["headers"].(map[string]any)

	// Set-Cookie should be an array since there are multiple values
	setCookie := headers["Set-Cookie"]
	assert.NotNil(t, setCookie)

	// Should be a slice
	cookieSlice, ok := setCookie.([]string)
	require.True(t, ok, "Set-Cookie should be a []string")
	assert.Len(t, cookieSlice, 2)
	assert.Contains(t, cookieSlice, "session=abc123")
	assert.Contains(t, cookieSlice, "token=xyz789")

	// Content-Type should be a string since it's a single value
	contentType := headers["Content-Type"]
	assert.Equal(t, "text/plain", contentType)
}

func TestHTTPProvider_Execute_Timeout(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the timeout
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":     server.URL,
		"timeout": 1, // 1 second timeout - should succeed since sleep is 200ms
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
}

func TestHTTPProvider_Execute_TimeoutExceeded(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the timeout
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":     server.URL,
		"timeout": 1, // 1 second timeout - should fail since sleep is 2s
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	// Different Go versions / OS schedulers surface the timeout differently:
	// "Client.Timeout exceeded while awaiting headers" (older/macOS) or
	// "context deadline exceeded" (newer/Linux).
	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "Client.Timeout exceeded") || strings.Contains(errMsg, "context deadline exceeded"),
		"expected timeout error, got: %s", errMsg,
	)
}

func TestHTTPProvider_Execute_InvalidURL(t *testing.T) {
	t.Parallel()
	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url": "://invalid-url",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
}

func TestHTTPProvider_Execute_DefaultMethod(t *testing.T) {
	t.Parallel()
	receivedMethod := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	// Don't specify method - should default to GET
	inputs := map[string]any{
		"url": server.URL,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify GET was used
	assert.Equal(t, http.MethodGet, receivedMethod)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
}

func TestHTTPProvider_Execute_EmptyBody(t *testing.T) {
	t.Parallel()
	receivedBody := "not-empty"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(body)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "POST",
		// No body specified
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify empty body was sent
	assert.Empty(t, receivedBody)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
}

func TestHTTPProvider_Execute_RetryOnServerError(t *testing.T) {
	t.Parallel()
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			// Return 500 for first 2 attempts
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Server Error"))
			return
		}
		// Return 200 on 3rd attempt
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Success"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"retry": map[string]any{
			"maxAttempts": 3,
			"backoff":     "none",
			"retryOn":     []any{500},
			"initialWait": "10ms",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify 3 attempts were made
	assert.Equal(t, 3, attemptCount)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
	assert.Equal(t, "Success", data["body"])
}

func TestHTTPProvider_Execute_RetryExhausted(t *testing.T) {
	t.Parallel()
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		// Always return 503
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Service Unavailable"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"retry": map[string]any{
			"maxAttempts": 3,
			"backoff":     "none",
			"retryOn":     []any{503},
			"initialWait": "10ms",
		},
	}

	output, err := p.Execute(ctx, inputs)

	// After all retries exhausted, the provider returns the last response
	// (not an error) because the HTTP request itself succeeded
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify all 3 attempts were made
	assert.Equal(t, 3, attemptCount)

	// The final response should be 503
	data := output.Data.(map[string]any)
	assert.Equal(t, 503, data["statusCode"])
	assert.Equal(t, "Service Unavailable", data["body"])
}

func TestHTTPProvider_Execute_RetryLinearBackoff(t *testing.T) {
	t.Parallel()
	attemptTimes := []time.Time{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptTimes = append(attemptTimes, time.Now())
		if len(attemptTimes) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"retry": map[string]any{
			"maxAttempts": 3,
			"backoff":     "linear",
			"retryOn":     []any{500},
			"initialWait": "50ms",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify 3 attempts
	assert.Len(t, attemptTimes, 3)

	// Linear backoff: first wait = 50ms, second wait = 100ms
	// Be lenient with timing checks due to test flakiness
	if len(attemptTimes) >= 2 {
		firstGap := attemptTimes[1].Sub(attemptTimes[0])
		assert.GreaterOrEqual(t, firstGap.Milliseconds(), int64(40), "First gap should be ~50ms")
	}
	if len(attemptTimes) >= 3 {
		secondGap := attemptTimes[2].Sub(attemptTimes[1])
		assert.GreaterOrEqual(t, secondGap.Milliseconds(), int64(80), "Second gap should be ~100ms")
	}
}

func TestHTTPProvider_Execute_RetryExponentialBackoff(t *testing.T) {
	t.Parallel()
	attemptTimes := []time.Time{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptTimes = append(attemptTimes, time.Now())
		if len(attemptTimes) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"retry": map[string]any{
			"maxAttempts": 3,
			"backoff":     "exponential",
			"retryOn":     []any{500},
			"initialWait": "25ms",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify 3 attempts
	assert.Len(t, attemptTimes, 3)

	// Exponential backoff: first wait = 25ms (2^0), second wait = 50ms (2^1)
	if len(attemptTimes) >= 2 {
		firstGap := attemptTimes[1].Sub(attemptTimes[0])
		assert.GreaterOrEqual(t, firstGap.Milliseconds(), int64(15), "First gap should be ~25ms")
	}
	if len(attemptTimes) >= 3 {
		secondGap := attemptTimes[2].Sub(attemptTimes[1])
		assert.GreaterOrEqual(t, secondGap.Milliseconds(), int64(35), "Second gap should be ~50ms")
	}
}

func TestHTTPProvider_Execute_RetryContextCancellation(t *testing.T) {
	t.Parallel()
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		// Always return 500 to trigger retry
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx, cancel := context.WithCancel(testContext(t))

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"retry": map[string]any{
			"maxAttempts": 5,
			"backoff":     "none",
			"retryOn":     []any{500},
			"initialWait": "100ms",
		},
	}

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	output, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "cancel") // matches both "cancelled" and "context canceled"

	// Should have made at least 1 attempt but not all 5
	assert.GreaterOrEqual(t, attemptCount, 1)
	assert.Less(t, attemptCount, 5)
}

func TestHTTPProvider_Execute_NoRetryOnNonRetryableStatus(t *testing.T) {
	t.Parallel()
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		// Return 400 which is not in the default retryOn list
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad Request"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"retry": map[string]any{
			"maxAttempts": 3,
			"backoff":     "none",
			"retryOn":     []any{500, 502, 503}, // 400 not in list
			"initialWait": "10ms",
		},
	}

	output, err := p.Execute(ctx, inputs)

	// Should succeed (no error) but with 400 status
	require.NoError(t, err)
	require.NotNil(t, output)

	// Only 1 attempt because 400 is not retryable
	assert.Equal(t, 1, attemptCount)

	data := output.Data.(map[string]any)
	assert.Equal(t, 400, data["statusCode"])
}

func TestHTTPProvider_Execute_RetryOnRateLimited(t *testing.T) {
	t.Parallel()
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		if attemptCount < 2 {
			// Return 429 Too Many Requests
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"retry": map[string]any{
			"maxAttempts": 3,
			"backoff":     "none",
			"retryOn":     []any{429}, // Rate limit status code
			"initialWait": "10ms",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, 2, attemptCount)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
}

// ============================================================================
// Auth Integration Tests
// ============================================================================

func TestHTTPProvider_Execute_AuthProvider_Success(t *testing.T) {
	t.Parallel()
	var receivedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"authenticated": true}`))
	}))
	defer server.Close()

	// Set up mock auth handler
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}
	mockHandler.SetToken(&auth.Token{
		AccessToken: "test-access-token-12345",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})

	// Create registry and register mock handler
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))

	// Create context with auth registry
	ctx := testContextWithCLIAuth(t, registry)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          server.URL,
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Bearer test-access-token-12345", receivedAuthHeader)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])

	// Verify GetToken was called with correct options
	require.Len(t, mockHandler.GetTokenCalls, 1)
	assert.Equal(t, "https://graph.microsoft.com/.default", mockHandler.GetTokenCalls[0].Scope)
	// MinValidFor should be timeout (30s) + 60s buffer = 90s
	assert.True(t, mockHandler.GetTokenCalls[0].MinValidFor >= 90*time.Second)
}

func TestHTTPProvider_Execute_AuthProvider_MissingScope(t *testing.T) {
	t.Parallel()
	// Scope is required for handlers with CapScopesOnTokenRequest (e.g., entra)
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := testContextWithCLIAuth(t, registry)

	p := NewHTTPProvider()

	inputs := map[string]any{
		"url":          "https://example.com/api",
		"method":       "GET",
		"authProvider": "entra",
		// scope is missing
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "scope is required")
}

func TestHTTPProvider_Execute_AuthProvider_GitHubNoScope(t *testing.T) {
	t.Parallel()
	var receivedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	// GitHub handler does NOT have CapScopesOnTokenRequest
	mockHandler := auth.NewMockHandler("github")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapHostname}
	mockHandler.SetToken(&auth.Token{
		AccessToken: "gho_github_token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := testContextWithCLIAuth(t, registry)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          server.URL,
		"method":       "GET",
		"authProvider": "github",
		// No scope — GitHub scopes are fixed at login time
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Bearer gho_github_token", receivedAuthHeader)

	// Verify GetToken was called with empty scope
	require.Len(t, mockHandler.GetTokenCalls, 1)
	assert.Empty(t, mockHandler.GetTokenCalls[0].Scope)
}

func TestHTTPProvider_Execute_AuthProvider_MissingRegistry(t *testing.T) {
	t.Parallel()
	p := NewHTTPProvider()
	// Context without auth registry
	ctx := testContext(t)

	inputs := map[string]any{
		"url":          "https://example.com/api",
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "no auth registry in context")
}

func TestHTTPProvider_Execute_AuthProvider_UnknownHandler(t *testing.T) {
	t.Parallel()
	// Create empty registry
	registry := auth.NewRegistry()
	ctx := testContextWithCLIAuth(t, registry)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          "https://example.com/api",
		"method":       "GET",
		"authProvider": "unknown-handler",
		"scope":        "https://graph.microsoft.com/.default",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "not found")
}

func TestHTTPProvider_Execute_AuthProvider_TokenError(t *testing.T) {
	t.Parallel()
	// Set up mock auth handler that returns error
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}
	mockHandler.SetTokenError(auth.ErrNotAuthenticated)

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := testContextWithCLIAuth(t, registry)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          "https://example.com/api",
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to get auth token")
}

func TestHTTPProvider_Execute_AuthProvider_401Retry(t *testing.T) {
	t.Parallel()
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++

		if attemptCount == 1 {
			// First request: return 401
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
			return
		}

		// Second request: success
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	// Set up mock handler
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}
	mockHandler.SetToken(&auth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := testContextWithCLIAuth(t, registry)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          server.URL,
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	// Should have made 2 requests (initial + retry after 401)
	assert.Equal(t, 2, attemptCount)

	// Should have called GetToken twice (once for initial, once with ForceRefresh)
	require.Len(t, mockHandler.GetTokenCalls, 2)
	assert.False(t, mockHandler.GetTokenCalls[0].ForceRefresh)
	assert.True(t, mockHandler.GetTokenCalls[1].ForceRefresh)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
}

func TestHTTPProvider_Execute_AuthProvider_401RetryOnlyOnce(t *testing.T) {
	t.Parallel()
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		// Always return 401
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}
	mockHandler.SetToken(&auth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := testContextWithCLIAuth(t, registry)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          server.URL,
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err) // Returns 401 as result, not error
	require.NotNil(t, output)

	// Should have made exactly 2 requests (initial + 1 retry)
	assert.Equal(t, 2, attemptCount)

	data := output.Data.(map[string]any)
	assert.Equal(t, 401, data["statusCode"])
}

func TestHTTPProvider_Execute_FloatTimeout(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	// Timeout as float64 (from JSON/YAML unmarshaling)
	inputs := map[string]any{
		"url":     server.URL,
		"method":  "GET",
		"timeout": float64(10),
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
}

func TestHTTPProvider_Execute_HeadersCopy(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.SetToken(&auth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := testContextWithCLIAuth(t, registry)

	// Original headers
	originalHeaders := map[string]any{
		"X-Custom-Header": "custom-value",
	}

	inputs := map[string]any{
		"url":          server.URL,
		"method":       "GET",
		"headers":      originalHeaders,
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	_, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	// Original headers should NOT have Authorization (we made a copy)
	_, hasAuth := originalHeaders["Authorization"]
	assert.False(t, hasAuth, "Original headers should not be modified")
}

func TestHTTPProvider_Execute_AutoParseJson(t *testing.T) {
	t.Parallel()

	jsonHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"users":[{"name":"alice"},{"name":"bob"}],"count":2}`))
	})

	t.Run("enabled", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(jsonHandler)
		defer server.Close()

		p := NewHTTPProvider()
		ctx := testContext(t)
		inputs := map[string]any{
			"url":           server.URL,
			"method":        "GET",
			"autoParseJson": true,
		}

		output, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := output.Data.(map[string]any)

		// Body should be parsed into structured data
		body, ok := data["body"].(map[string]any)
		require.True(t, ok, "body should be a map when autoParseJson is true")
		assert.Equal(t, float64(2), body["count"])

		users, ok := body["users"].([]any)
		require.True(t, ok)
		assert.Len(t, users, 2)
	})

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(jsonHandler)
		defer server.Close()

		p := NewHTTPProvider()
		ctx := testContext(t)
		inputs := map[string]any{
			"url":    server.URL,
			"method": "GET",
		}

		output, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := output.Data.(map[string]any)

		// Body should be a raw string when autoParseJson is not set
		body, ok := data["body"].(string)
		require.True(t, ok, "body should be a string when autoParseJson is false")
		assert.Contains(t, body, `"users"`)
	})

	t.Run("non-json-content-type", func(t *testing.T) {
		t.Parallel()
		textServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
		}))
		defer textServer.Close()

		p := NewHTTPProvider()
		ctx := testContext(t)
		inputs := map[string]any{
			"url":           textServer.URL,
			"method":        "GET",
			"autoParseJson": true,
		}

		output, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := output.Data.(map[string]any)

		// Body should remain a string for non-JSON content type
		_, ok := data["body"].(string)
		assert.True(t, ok, "body should remain string for non-JSON content type")
	})
}

func TestIsJSONContentType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		contentType string
		expected    bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"APPLICATION/JSON", true},
		{"application/vnd.api+json", true},
		{"text/plain", false},
		{"text/html", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isJSONContentType(tt.contentType))
		})
	}
}

// ── Delegation registry tests ──

func TestHTTPProvider_Execute_Success(t *testing.T) {
	t.Parallel()
	var receivedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"delegated": true}`))
	}))
	defer server.Close()

	mock := auth.NewMockHandler("entra")
	mock.GetTokenResult = &auth.Token{AccessToken: "delegated-token-abc", TokenType: "Bearer", ExpiresAt: time.Now().Add(3600 * time.Second)}

	ctx := testContextWithAPIIdentity(t, mock)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          server.URL,
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Bearer delegated-token-abc", receivedAuthHeader)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
}

func TestHTTPProvider_Execute_ProviderNotRegistered(t *testing.T) {
	t.Parallel()
	// Empty auth registry — no handlers registered.
	reg := auth.NewRegistry()
	ctx := auth.WithRegistry(testContext(t), reg)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          "https://example.com/api",
		"method":       "GET",
		"authProvider": "gcp",
		"scope":        "https://www.googleapis.com/auth/cloud-platform",
	}

	_, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gcp")
}

func TestHTTPProvider_Execute_Provider_Error(t *testing.T) {
	t.Parallel()
	mock := auth.NewMockHandler("entra")
	mock.GetTokenErr = fmt.Errorf("token endpoint returned 400")

	ctx := testContextWithAPIIdentity(t, mock)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          "https://example.com/api",
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
}

func TestHTTPProvider_Execute_Provider_401Retry(t *testing.T) {
	t.Parallel()
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	mock := auth.NewMockHandler("entra")
	mock.GetTokenResult = &auth.Token{AccessToken: "delegated-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(3600 * time.Second)}

	ctx := testContextWithAPIIdentity(t, mock)

	p := NewHTTPProvider()
	inputs := map[string]any{
		"url":          server.URL,
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)
	// Should retry on 401 via OnUnauthorized hook — 2 requests made.
	assert.Equal(t, 2, attemptCount)

	data := output.Data.(map[string]any)
	assert.Equal(t, 401, data["statusCode"])
}

// clusterAwareAuthHandler is a test auth.Handler that routes GetToken responses
// by TokenOptions.Hostname, mirroring multi-cluster handlers advertising
// CapTokenHostname.
type clusterAwareAuthHandler struct {
	*auth.MockHandler
	mu       sync.Mutex
	tokens   map[string]*auth.Token
	calls    []auth.TokenOptions
	profiles []string
}

func newClusterAwareAuthHandler(name string, tokens map[string]*auth.Token) *clusterAwareAuthHandler {
	base := auth.NewMockHandler(name)
	base.CapabilitiesValue = []auth.Capability{auth.CapHostname, auth.CapTokenHostname}
	return &clusterAwareAuthHandler{
		MockHandler: base,
		tokens:      tokens,
	}
}

func (h *clusterAwareAuthHandler) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, opts)
	h.profiles = append(h.profiles, auth.ProfileFromContext(ctx))
	tok, ok := h.tokens[opts.Hostname]
	if !ok || tok == nil {
		return nil, fmt.Errorf("%w: no session for %q", auth.ErrNotAuthenticated, opts.Hostname)
	}
	return tok, nil
}

func (h *clusterAwareAuthHandler) recordedCalls() []auth.TokenOptions {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]auth.TokenOptions, len(h.calls))
	copy(out, h.calls)
	return out
}

func (h *clusterAwareAuthHandler) recordedProfiles() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.profiles))
	copy(out, h.profiles)
	return out
}

func TestNewHTTPProvider_HostnameSchemaProperty(t *testing.T) {
	t.Parallel()
	p := NewHTTPProvider()
	schema := p.Descriptor().Schema

	prop, ok := schema.Properties["hostname"]
	require.True(t, ok, "http provider schema should expose hostname input")
	assert.Contains(t, prop.Description, "token_hostname")
	require.NotNil(t, prop.MaxLength)
	assert.Equal(t, maxHostnameLength, *prop.MaxLength)

	authProp, ok := schema.Properties["authProvider"]
	require.True(t, ok, "http provider schema should expose authProvider input")
	require.NotNil(t, authProp.MaxLength)
	assert.Equal(t, 129, maxAuthProviderLength)
	assert.Equal(t, maxAuthProviderLength, *authProp.MaxLength)

	validator := provider.NewSchemaValidator()
	maxHandler := strings.Repeat("h", 64)
	maxProfile := strings.Repeat("p", auth.MaxProfileNameLength)
	validMaxSelector := maxHandler + "@" + maxProfile
	require.Len(t, validMaxSelector, 129)

	// 129-char selector (64 handler + '@' + 64 profile) passes for regular and state inputs.
	require.NoError(t, validator.ValidateInputs(map[string]any{
		"url":          "https://api.example.com/v1",
		"authProvider": validMaxSelector,
	}, schema))
	require.NoError(t, validator.ValidateInputs(map[string]any{
		"operation":    "state_load",
		"url":          "https://api.example.com/state",
		"authProvider": validMaxSelector,
	}, schema))

	// 130-char selector exceeds maxAuthProviderLength and fails schema validation.
	tooLongSelector := validMaxSelector + "x"
	require.Len(t, tooLongSelector, 130)
	require.Error(t, validator.ValidateInputs(map[string]any{
		"url":          "https://api.example.com/v1",
		"authProvider": tooLongSelector,
	}, schema))
	require.Error(t, validator.ValidateInputs(map[string]any{
		"operation":    "state_load",
		"url":          "https://api.example.com/state",
		"authProvider": tooLongSelector,
	}, schema))
}

func TestHTTPProvider_Execute_QualifiedAuthProviderWithPassThroughToken(t *testing.T) {
	t.Parallel()
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Context carries a pass-through header token for "Github" and NO auth registry.
	ctx := middleware.WithContextTokens(testContext(t), map[string]string{
		"Github": "caller-github-token",
	})
	p := NewHTTPProvider()

	// (a) "github@work" and "github@built-in" match the bare "Github" caller token without a registry.
	for _, selector := range []string{"github@work", "github@built-in"} {
		receivedAuth = ""
		out, err := p.Execute(ctx, map[string]any{
			"url":          srv.URL + "/repos",
			"method":       "GET",
			"authProvider": selector,
		})
		require.NoError(t, err, "selector %s should use caller pass-through token", selector)
		assert.Equal(t, 200, out.Data.(map[string]any)["statusCode"])
		assert.Equal(t, "Bearer caller-github-token", receivedAuth)
	}

	// (b) "github@work" + caller pass-through token + explicit hostname fails closed.
	_, err := p.Execute(ctx, map[string]any{
		"url":          srv.URL + "/repos",
		"method":       "GET",
		"authProvider": "github@work",
		"hostname":     "cluster-a",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request-header token pass-through")

	// (c) Invalid profile selector fails closed even when caller token is present.
	_, err = p.Execute(ctx, map[string]any{
		"url":          srv.URL + "/repos",
		"method":       "GET",
		"authProvider": "github@bad/profile",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid profile")
}

func TestHTTPProvider_Execute_AuthProvider_CapTokenHostname_MultiCluster(t *testing.T) {
	t.Parallel()

	var clusterAAuth, clusterBAuth string
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clusterAAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cluster":"a"}`))
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clusterBAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cluster":"b"}`))
	}))
	defer srvB.Close()

	srvUnauthenticated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srvUnauthenticated.Close()

	handler := newClusterAwareAuthHandler("openshift", map[string]*auth.Token{
		srvA.URL: {AccessToken: "token-for-cluster-a", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)},
		srvB.URL: {AccessToken: "token-for-cluster-b", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)},
	})

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(handler))
	ctx := testContextWithCLIAuth(t, reg)

	p := NewHTTPProvider()

	outA, err := p.Execute(ctx, map[string]any{
		"url":          srvA.URL + "/apis/apps/v1/deployments?limit=5",
		"method":       "GET",
		"authProvider": "openshift",
	})
	require.NoError(t, err)
	assert.Equal(t, 200, outA.Data.(map[string]any)["statusCode"])
	assert.Equal(t, "Bearer token-for-cluster-a", clusterAAuth)

	outB, err := p.Execute(ctx, map[string]any{
		"url":          srvB.URL + "/apis/apps/v1/deployments",
		"method":       "GET",
		"authProvider": "openshift",
	})
	require.NoError(t, err)
	assert.Equal(t, 200, outB.Data.(map[string]any)["statusCode"])
	assert.Equal(t, "Bearer token-for-cluster-b", clusterBAuth)

	// Target cluster with no session must fail closed rather than returning another cluster's token.
	_, err = p.Execute(ctx, map[string]any{
		"url":          srvUnauthenticated.URL + "/api/v1/namespaces",
		"method":       "GET",
		"authProvider": "openshift",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrNotAuthenticated)

	calls := handler.recordedCalls()
	require.Len(t, calls, 3)
	assert.Equal(t, srvA.URL, calls[0].Hostname)
	assert.Equal(t, srvB.URL, calls[1].Hostname)
	assert.Equal(t, srvUnauthenticated.URL, calls[2].Hostname)
}

func TestHTTPProvider_Execute_AuthProvider_ExplicitHostnameTunnelAndCapabilityGate(t *testing.T) {
	t.Parallel()

	var receivedAuth string
	tunnelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer tunnelSrv.Close()

	clusterHandler := newClusterAwareAuthHandler("openshift", map[string]*auth.Token{
		"https://api.cluster-a.example.com:6443": {
			AccessToken: "tunnel-cluster-a-token",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	})

	nonHostnameHandler := auth.NewMockHandler("github")
	nonHostnameHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapHostname}
	nonHostnameHandler.SetToken(&auth.Token{
		AccessToken: "github-default-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(clusterHandler))
	require.NoError(t, reg.Register(nonHostnameHandler))

	cfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: ptrs.BoolPtr(true)},
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {
					Hostname: &config.HostnameConfig{
						Aliases: map[string]string{
							"cluster-a": "https://api.cluster-a.example.com:6443",
						},
					},
				},
			},
		},
	}
	ctx := auth.WithRegistry(config.WithConfig(context.Background(), cfg), reg)
	p := NewHTTPProvider()

	// 1. Explicit hostname alias resolves to cluster URL while dialing local tunnel.
	out, err := p.Execute(ctx, map[string]any{
		"url":          tunnelSrv.URL + "/api/v1/namespaces",
		"method":       "GET",
		"authProvider": "openshift",
		"hostname":     "cluster-a",
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.Data.(map[string]any)["statusCode"])
	assert.Equal(t, "Bearer tunnel-cluster-a-token", receivedAuth)

	// 2. Non-CapTokenHostname handler without hostname leaves Hostname empty.
	_, err = p.Execute(ctx, map[string]any{
		"url":          tunnelSrv.URL + "/user",
		"method":       "GET",
		"authProvider": "github",
	})
	require.NoError(t, err)
	require.Len(t, nonHostnameHandler.GetTokenCalls, 1)
	assert.Empty(t, nonHostnameHandler.GetTokenCalls[0].Hostname)

	// 3. Non-CapTokenHostname handler with explicit hostname fails closed.
	_, err = p.Execute(ctx, map[string]any{
		"url":          tunnelSrv.URL + "/user",
		"method":       "GET",
		"authProvider": "github",
		"hostname":     "cluster-a",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `hostname input is not supported by authProvider "github"`)

	// 4. Unknown alias selector propagates ErrSelectorNotFound through Execute.
	_, err = p.Execute(ctx, map[string]any{
		"url":          tunnelSrv.URL + "/api/v1/namespaces",
		"method":       "GET",
		"authProvider": "openshift",
		"hostname":     "nonexistent-alias",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to resolve hostname "nonexistent-alias"`)

	// 5. Setting hostname without authProvider fails closed.
	_, err = p.Execute(ctx, map[string]any{
		"url":      tunnelSrv.URL + "/api/v1/namespaces",
		"method":   "GET",
		"hostname": "cluster-a",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname requires authProvider to be set")

	// 6. Profile-qualified authProvider ("openshift@work") resolves the bare
	// handler, resolves aliases under "openshift", and propagates profile="work"
	// across initial GetToken and 401 refresh.
	attempts := 0
	profileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer profileSrv.Close()

	_, err = p.Execute(ctx, map[string]any{
		"url":          profileSrv.URL + "/api/v1/namespaces",
		"method":       "GET",
		"authProvider": "openshift@work",
		"hostname":     "cluster-a",
	})
	require.NoError(t, err)
	profiles := clusterHandler.recordedProfiles()
	require.GreaterOrEqual(t, len(profiles), 3)
	assert.Equal(t, []string{"work", "work"}, profiles[len(profiles)-2:])

	// 7. Built-in profile selector ("github@built-in") overrides ambient profile.
	ambientCtx := auth.WithProfile(ctx, "other-profile")
	_, err = p.Execute(ambientCtx, map[string]any{
		"url":          tunnelSrv.URL + "/user",
		"method":       "GET",
		"authProvider": "github@built-in",
	})
	require.NoError(t, err)
	assert.Equal(t, "", nonHostnameHandler.LastContextProfile)
}

func TestHTTPProvider_Execute_AuthProvider_DefaultsEmptyTokenTypeAndOverridesLowercaseAuthHeader(t *testing.T) {
	t.Parallel()
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mockHandler := auth.NewMockHandler("custom")
	// TokenType intentionally omitted ("") to verify Bearer default.
	mockHandler.SetToken(&auth.Token{
		AccessToken: "raw-access-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))
	ctx := testContextWithCLIAuth(t, reg)

	p := NewHTTPProvider()
	for range 10 {
		_, err := p.Execute(ctx, map[string]any{
			"url":          srv.URL,
			"method":       "GET",
			"authProvider": "custom",
			"headers": map[string]any{
				"authorization": "Bearer stale-caller-header",
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "Bearer raw-access-token", receivedAuth)
	}
}

func TestHTTPProvider_Execute_RedirectStripsCrossOriginAuthAndPreservesOriginalHostnameOn401(t *testing.T) {
	t.Parallel()

	t.Run("same-origin redirect preserves Authorization", func(t *testing.T) {
		t.Parallel()
		var redirectedAuth string
		var srvURL string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/start" {
				http.Redirect(w, r, srvURL+"/final", http.StatusFound)
				return
			}
			redirectedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		srvURL = srv.URL

		p := NewHTTPProvider()
		_, err := p.Execute(testContext(t), map[string]any{
			"url":    srv.URL + "/start",
			"method": "GET",
			"headers": map[string]any{
				"Authorization": "Bearer secret-token",
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "Bearer secret-token", redirectedAuth)
	})

	t.Run("cross-port redirect strips Authorization and 401 refresh keeps original Hostname", func(t *testing.T) {
		t.Parallel()
		var targetReceivedAuth []string
		var mu sync.Mutex

		targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			targetReceivedAuth = append(targetReceivedAuth, r.Header.Get("Authorization"))
			callNum := len(targetReceivedAuth)
			mu.Unlock()
			if callNum == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer targetSrv.Close()

		originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, targetSrv.URL+"/landed", http.StatusFound)
		}))
		defer originSrv.Close()

		handler := newClusterAwareAuthHandler("openshift", map[string]*auth.Token{
			originSrv.URL: {
				AccessToken: "origin-cluster-secret",
				TokenType:   "Bearer",
				ExpiresAt:   time.Now().Add(time.Hour),
			},
			targetSrv.URL: {
				AccessToken: "wrong-target-token",
				TokenType:   "Bearer",
				ExpiresAt:   time.Now().Add(time.Hour),
			},
		})
		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(handler))
		ctx := testContextWithCLIAuth(t, reg)

		p := NewHTTPProvider()
		out, err := p.Execute(ctx, map[string]any{
			"url":          originSrv.URL + "/start",
			"method":       "GET",
			"authProvider": "openshift",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, out.Data.(map[string]any)["statusCode"])

		// Neither initial redirect nor 401-retried redirect may leak Authorization to targetSrv.
		mu.Lock()
		gotAuths := append([]string(nil), targetReceivedAuth...)
		mu.Unlock()
		assert.Equal(t, []string{"", ""}, gotAuths)

		// Both initial GetToken and ForceRefresh GetToken must keep originSrv.URL as Hostname.
		calls := handler.recordedCalls()
		require.Len(t, calls, 2)
		assert.Equal(t, originSrv.URL, calls[0].Hostname)
		assert.False(t, calls[0].ForceRefresh)
		assert.Equal(t, originSrv.URL, calls[1].Hostname)
		assert.True(t, calls[1].ForceRefresh)
	})

	t.Run("multi-hop bounce through another origin keeps Authorization stripped", func(t *testing.T) {
		t.Parallel()
		var finalAuth string
		var originURL string
		bounceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, originURL+"/final", http.StatusFound)
		}))
		defer bounceSrv.Close()

		originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/start" {
				http.Redirect(w, r, bounceSrv.URL+"/hop", http.StatusFound)
				return
			}
			finalAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer originSrv.Close()
		originURL = originSrv.URL

		p := NewHTTPProvider()
		_, err := p.Execute(testContext(t), map[string]any{
			"url":    originSrv.URL + "/start",
			"method": "GET",
			"headers": map[string]any{
				"Authorization": "Bearer bounce-secret",
			},
		})
		require.NoError(t, err)
		assert.Empty(t, finalAuth)
	})

	t.Run("subdomain and https-to-http downgrade redirects strip Authorization", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name     string
			fromURL  string
			location string
		}{
			{
				name:     "subdomain redirect",
				fromURL:  "https://api.example.com/v1/resource",
				location: "https://untrusted.api.example.com/v1/resource",
			},
			{
				name:     "https to http scheme downgrade",
				fromURL:  "https://api.example.com/v1/resource",
				location: "http://api.example.com/v1/resource",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				origReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, tc.fromURL, nil)
				require.NoError(t, err)
				origReq.Header.Set("Authorization", "Bearer top-secret")

				nextURL, err := url.Parse(tc.location)
				require.NoError(t, err)
				redirectReq := &http.Request{
					Method: http.MethodGet,
					URL:    nextURL,
					Header: http.Header{"Authorization": []string{"Bearer top-secret"}},
				}

				hook := stripCrossOriginAuthOnRedirect(nil)
				require.NoError(t, hook(redirectReq, []*http.Request{origReq}))
				assert.Empty(t, redirectReq.Header.Get("Authorization"))
			})
		}
	})

	t.Run("explicit default port 443 redirect preserves Authorization", func(t *testing.T) {
		t.Parallel()
		origReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.com/v1/resource", nil)
		require.NoError(t, err)
		origReq.Header.Set("Authorization", "Bearer same-origin-secret")

		nextURL, err := url.Parse("https://api.example.com:443/v1/canonical")
		require.NoError(t, err)
		redirectReq := &http.Request{
			Method: http.MethodGet,
			URL:    nextURL,
			Header: http.Header{"Authorization": []string{"Bearer same-origin-secret"}},
		}

		hook := stripCrossOriginAuthOnRedirect(nil)
		require.NoError(t, hook(redirectReq, []*http.Request{origReq}))
		assert.Equal(t, "Bearer same-origin-secret", redirectReq.Header.Get("Authorization"))
	})
}

func TestHTTPProvider_Execute_ReusesConnectionAndClosesPool(t *testing.T) {
	t.Parallel()

	var newConns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	for range 5 {
		out, err := p.Execute(ctx, map[string]any{
			"url":    srv.URL + "/items",
			"method": "GET",
		})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, out.Data.(map[string]any)["statusCode"])
	}

	assert.Equal(t, int32(1), newConns.Load(), "repeated Execute calls with identical config must reuse one TCP connection")
	assert.Equal(t, 1, p.pool().Len())

	require.NoError(t, p.Close())
	assert.Zero(t, p.pool().Len())

	// Zero-value and nil providers close safely and remain usable.
	var nilProvider *HTTPProvider
	require.NoError(t, nilProvider.Close())

	var zeroProvider HTTPProvider
	require.NoError(t, zeroProvider.Close())
	out, err := zeroProvider.Execute(ctx, map[string]any{
		"url":    srv.URL + "/items",
		"method": "GET",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, out.Data.(map[string]any)["statusCode"])
	require.NoError(t, zeroProvider.Close())
}

// multiTenantRefreshHandler issues stale initial tokens and fresh ForceRefresh
// tokens per profile so concurrent callers sharing one pooled client prove
// zero cross-tenant credential or context bleed.
type multiTenantRefreshHandler struct {
	*auth.MockHandler
	mu      sync.Mutex
	records []string
}

func newMultiTenantRefreshHandler() *multiTenantRefreshHandler {
	base := auth.NewMockHandler("entra")
	base.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}
	return &multiTenantRefreshHandler{MockHandler: base}
}

func (h *multiTenantRefreshHandler) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profile := auth.ProfileFromContext(ctx)
	prefix := "stale-"
	if opts.ForceRefresh {
		prefix = "fresh-"
	}
	h.mu.Lock()
	h.records = append(h.records, prefix+profile+"|"+opts.Scope)
	h.mu.Unlock()
	return &auth.Token{
		AccessToken: prefix + profile,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}, nil
}

func TestHTTPProvider_Execute_MultiTenant401RefreshIsolation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := r.URL.Query().Get("tenant")
		authHeader := r.Header.Get("Authorization")
		if authHeader == "Bearer stale-"+tenant {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"expired"}`))
			return
		}
		if authHeader == "Bearer fresh-"+tenant {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok-" + tenant))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("wrong-token:" + authHeader))
	}))
	defer srv.Close()

	handler := newMultiTenantRefreshHandler()
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(handler))
	baseCtx := testContextWithCLIAuth(t, reg)

	p := NewHTTPProvider()
	defer func() { _ = p.Close() }()

	const tenants = 16
	var wg sync.WaitGroup
	wg.Add(tenants)
	for i := range tenants {
		tenant := "tenant-" + strconv.Itoa(i)
		go func() {
			defer wg.Done()
			out, err := p.Execute(baseCtx, map[string]any{
				"url":          srv.URL + "/data?tenant=" + tenant,
				"method":       "GET",
				"authProvider": "entra@" + tenant,
				"scope":        "api://" + tenant + "/.default",
			})
			if !assert.NoError(t, err) {
				return
			}
			data := out.Data.(map[string]any)
			assert.Equal(t, http.StatusOK, data["statusCode"])
			assert.Equal(t, "ok-"+tenant, data["body"])
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, p.pool().Len(), "all tenants shared a single pooled client")
}

func TestHTTPProvider_Execute_CancelledPriorContextDoesNotBreakPooled401Refresh(t *testing.T) {
	t.Parallel()

	var callNum atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callNum.Add(1)
		if n == 2 && r.Header.Get("Authorization") == "Bearer stale-req2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.Header.Get("Authorization")))
	}))
	defer srv.Close()

	handler := newMultiTenantRefreshHandler()
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(handler))
	baseCtx := testContextWithCLIAuth(t, reg)

	p := NewHTTPProvider()
	defer func() { _ = p.Close() }()

	// Request 1 populates the pooled client, then cancels its context.
	ctx1, cancel1 := context.WithCancel(baseCtx)
	out1, err := p.Execute(ctx1, map[string]any{
		"url":          srv.URL + "/step1",
		"method":       "GET",
		"authProvider": "entra@req1",
		"scope":        "api://scope/.default",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, out1.Data.(map[string]any)["statusCode"])
	cancel1()

	// Request 2 reuses the same pooled client, receives 401, and must refresh
	// with ctx2 rather than failing with ctx1's cancellation error.
	out2, err := p.Execute(baseCtx, map[string]any{
		"url":          srv.URL + "/step2",
		"method":       "GET",
		"authProvider": "entra@req2",
		"scope":        "api://scope/.default",
	})
	require.NoError(t, err)
	data2 := out2.Data.(map[string]any)
	assert.Equal(t, http.StatusOK, data2["statusCode"])
	assert.Equal(t, "Bearer fresh-req2", data2["body"])
}

func TestHTTPProvider_Execute_UnauthenticatedAndHeaderToken401PassThroughOnPooledClient(t *testing.T) {
	t.Parallel()

	var unauthHits atomic.Int32
	var headerHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authed":
			w.WriteHeader(http.StatusOK)
		case "/unauthed":
			unauthHits.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
		case "/header":
			headerHits.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	handler := newMultiTenantRefreshHandler()
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(handler))
	baseCtx := testContextWithCLIAuth(t, reg)

	p := NewHTTPProvider()
	defer func() { _ = p.Close() }()

	// Seed the pooled client with an authProvider execution.
	_, err := p.Execute(baseCtx, map[string]any{
		"url":          srv.URL + "/authed",
		"method":       "GET",
		"authProvider": "entra@seed",
		"scope":        "api://seed/.default",
	})
	require.NoError(t, err)

	// Unauthenticated 401 must NOT invoke any refresh callback or retry.
	outUnauth, err := p.Execute(baseCtx, map[string]any{
		"url":    srv.URL + "/unauthed",
		"method": "GET",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, outUnauth.Data.(map[string]any)["statusCode"])
	assert.Equal(t, int32(1), unauthHits.Load())

	// Request-header pass-through token 401 must NOT invoke handler refresh either.
	headerCtx := middleware.WithContextTokens(baseCtx, map[string]string{"Entra": "caller-token"})
	outHeader, err := p.Execute(headerCtx, map[string]any{
		"url":          srv.URL + "/header",
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "api://seed/.default",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, outHeader.Data.(map[string]any)["statusCode"])
	assert.Equal(t, int32(1), headerHits.Load())

	handler.mu.Lock()
	totalHandlerCalls := len(handler.records)
	handler.mu.Unlock()
	assert.Equal(t, 1, totalHandlerCalls, "handler must only have been called for the initial seeded request")
}

func TestHTTPProvider_Execute_SSRFCrossPolicyIsolation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("loopback-ok"))
	}))
	defer srv.Close()

	p := NewHTTPProvider()
	defer func() { _ = p.Close() }()

	// Permissive policy opens an idle keep-alive connection to loopback.
	permissiveCtx := testContext(t)
	out, err := p.Execute(permissiveCtx, map[string]any{
		"url":    srv.URL,
		"method": "GET",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, out.Data.(map[string]any)["statusCode"])

	// Default restrictive policy on the SAME provider instance must NOT inherit
	// the permissive client or its open loopback connection.
	_, err = p.Execute(context.Background(), map[string]any{
		"url":    srv.URL,
		"method": "GET",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, httpc.ErrBlockedByPolicy)
}

func TestHTTPProvider_ClientPool_KeyedOnTimeoutAndRetry(t *testing.T) {
	t.Parallel()

	p := NewHTTPProvider()
	defer func() { _ = p.Close() }()

	ctx := testContext(t)
	r1 := &retryConfig{MaxAttempts: 3, Backoff: "exponential", InitialWait: time.Second, MaxWait: 5 * time.Second, RetryOn: []int{500, 503}}
	r2 := &retryConfig{MaxAttempts: 3, Backoff: "linear", InitialWait: time.Second, MaxWait: 5 * time.Second, RetryOn: []int{500, 503}}

	cBase := p.clientFor(ctx, 30*time.Second, nil)
	cSame := p.clientFor(ctx, 30*time.Second, nil)
	cDiffTimeout := p.clientFor(ctx, 10*time.Second, nil)
	cRetry1 := p.clientFor(ctx, 30*time.Second, r1)
	cRetry2 := p.clientFor(ctx, 30*time.Second, r2)

	assert.Same(t, cBase, cSame)
	assert.NotSame(t, cBase, cDiffTimeout)
	assert.NotSame(t, cBase, cRetry1)
	assert.NotSame(t, cRetry1, cRetry2)

	// Exceeding DefaultProviderClientPoolCap keeps the pool bounded.
	for i := range DefaultProviderClientPoolCap * 2 {
		_ = p.clientFor(ctx, time.Duration(i+1)*time.Second, nil)
	}
	assert.LessOrEqual(t, p.pool().Len(), DefaultProviderClientPoolCap)
}

func TestHTTPProvider_Execute_PaginatedReusesRefreshedTokenAcrossPages(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh-pag" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "p2" {
			_, _ = w.Write([]byte(`{"items":["b"],"next":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":["a"],"next":"p2"}`))
	}))
	defer srv.Close()

	handler := newMultiTenantRefreshHandler()
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(handler))
	ctx := testContextWithCLIAuth(t, reg)

	p := NewHTTPProvider()
	defer func() { _ = p.Close() }()

	out, err := p.Execute(ctx, map[string]any{
		"url":          srv.URL + "/items",
		"method":       "GET",
		"authProvider": "entra@pag",
		"scope":        "api://pag/.default",
		"pagination": map[string]any{
			"strategy":       "cursor",
			"maxPages":       5,
			"nextTokenPath":  "body.next",
			"nextTokenParam": "cursor",
			"collectPath":    "body.items",
			"stopWhen":       "body.next == null",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, `["a","b"]`, out.Data.(map[string]any)["body"])

	handler.mu.Lock()
	calls := len(handler.records)
	handler.mu.Unlock()
	assert.Equal(t, 2, calls, "page 2 must reuse the refreshed token from page 1 without triggering a second ForceRefresh")
}
