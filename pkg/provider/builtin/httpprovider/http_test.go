package httpprovider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	assert.Contains(t, err.Error(), "context deadline exceeded")
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
