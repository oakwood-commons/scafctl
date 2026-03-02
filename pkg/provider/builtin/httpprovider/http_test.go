// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPProvider(t *testing.T) {
	p := NewHTTPProvider()

	require.NotNil(t, p)
	require.NotNil(t, p.Descriptor())
	assert.Equal(t, ProviderName, p.Descriptor().Name)
	assert.NotNil(t, p.Descriptor().Version)
	assert.Contains(t, p.Descriptor().Capabilities, provider.CapabilityFrom)
	assert.Contains(t, p.Descriptor().Capabilities, provider.CapabilityAction)
}

func TestHTTPProvider_Execute_GET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()
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
	p := NewHTTPProvider()
	ctx := provider.WithDryRun(context.Background(), true)
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
	ctx := context.Background()

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

func TestHTTPProvider_Execute_CustomHeaders(t *testing.T) {
	receivedHeaders := http.Header{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Store received headers
		receivedHeaders = r.Header.Clone()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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
	ctx := context.Background()

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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

func TestHTTPProvider_Execute_MultipleHeaderValues(t *testing.T) {
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
	ctx := context.Background()

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the timeout
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the timeout
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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
	p := NewHTTPProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"url": "://invalid-url",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
}

func TestHTTPProvider_Execute_DefaultMethod(t *testing.T) {
	receivedMethod := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		// Always return 503
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Service Unavailable"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		// Always return 500 to trigger retry
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx, cancel := context.WithCancel(context.Background())

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
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attemptCount++
		// Return 400 which is not in the default retryOn list
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad Request"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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
	ctx := context.Background()

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

	// Create context with registry
	ctx := auth.WithRegistry(context.Background(), registry)

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
	// Scope is required for handlers with CapScopesOnTokenRequest (e.g., entra)
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

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
	ctx := auth.WithRegistry(context.Background(), registry)

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
	p := NewHTTPProvider()
	// Context without auth registry
	ctx := context.Background()

	inputs := map[string]any{
		"url":          "https://example.com/api",
		"method":       "GET",
		"authProvider": "entra",
		"scope":        "https://graph.microsoft.com/.default",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "auth handler not found")
}

func TestHTTPProvider_Execute_AuthProvider_UnknownHandler(t *testing.T) {
	// Create empty registry
	registry := auth.NewRegistry()
	ctx := auth.WithRegistry(context.Background(), registry)

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
	assert.Contains(t, err.Error(), "auth handler not found")
}

func TestHTTPProvider_Execute_AuthProvider_TokenError(t *testing.T) {
	// Set up mock auth handler that returns error
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}
	mockHandler.SetTokenError(auth.ErrNotAuthenticated)

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

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
	ctx := auth.WithRegistry(context.Background(), registry)

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
	ctx := auth.WithRegistry(context.Background(), registry)

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

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
	ctx := auth.WithRegistry(context.Background(), registry)

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"users":[{"name":"alice"},{"name":"bob"}],"count":2}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := context.Background()

	t.Run("enabled", func(t *testing.T) {
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
		textServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
		}))
		defer textServer.Close()

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
			assert.Equal(t, tt.expected, isJSONContentType(tt.contentType))
		})
	}
}
