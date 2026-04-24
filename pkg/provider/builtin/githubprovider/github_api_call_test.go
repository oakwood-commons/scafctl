// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── api_call Tests ──────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_APICall_GET(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/labels", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{ //nolint:errcheck
			map[string]any{"name": "bug", "color": "d73a4a"},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "api_call",
		"api_base":  baseURL,
		"endpoint":  "/repos/test-org/test-repo/labels",
		"method":    "GET",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result := output.Data.(map[string]any)["result"]
	labels, ok := result.([]any)
	require.True(t, ok)
	assert.Len(t, labels, 1)
}

func TestGitHubProvider_Execute_APICall_POST(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/labels", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "enhancement", body["name"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"name": "enhancement", "id": float64(1)}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":    "api_call",
		"api_base":     baseURL,
		"endpoint":     "/repos/test-org/test-repo/labels",
		"method":       "POST",
		"request_body": map[string]any{"name": "enhancement", "color": "a2eeef"},
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "api_call", data["operation"])
}

func TestGitHubProvider_Execute_APICall_RejectsAbsoluteURL(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAPICall(t.Context(), nil, "https://api.github.com", map[string]any{
		"endpoint": "https://evil.example.com/steal",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "relative path starting with /")
}

func TestGitHubProvider_Execute_APICall_RejectsSchemeInPath(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAPICall(t.Context(), nil, "https://api.github.com", map[string]any{
		"endpoint": "/foo://bar",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "relative path")
}

func TestGitHubProvider_Execute_APICall_RejectsPathTraversal(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAPICall(t.Context(), nil, "https://api.github.com", map[string]any{
		"endpoint": "/repos/../../../etc/passwd",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestGitHubProvider_Execute_APICall_RejectsInvalidMethod(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAPICall(t.Context(), nil, "https://api.github.com", map[string]any{
		"endpoint": "/repos/owner/repo",
		"method":   "TRACE",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported HTTP method")
}

func TestGitHubProvider_Execute_APICall_MissingEndpoint(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAPICall(t.Context(), nil, "https://api.github.com", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint")
}

func TestGitHubProvider_Execute_APICall_DefaultsToGET(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "api_call",
		"api_base":  baseURL,
		"endpoint":  "/repos/test-org/test-repo",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
}

func TestGitHubProvider_Execute_APICall_QueryParams(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":    "api_call",
		"api_base":     baseURL,
		"endpoint":     "/repos/test-org/test-repo/labels",
		"query_params": map[string]any{"per_page": "100"},
	})

	require.NoError(t, err)
	require.NotNil(t, output)
}

func TestGitHubProvider_Execute_APICall_AllowsTripleDotEndpoint(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/compare/main...feature", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ahead"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "api_call",
		"api_base":  baseURL,
		"endpoint":  "/repos/owner/repo/compare/main...feature",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
}

func TestGitHubProvider_Execute_APICall_RejectsPercentEncodedTraversal(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAPICall(t.Context(), nil, "https://api.github.com", map[string]any{
		"endpoint": "/repos/%2e%2e/%2e%2e/etc/passwd",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestGitHubProvider_Execute_APICall_RejectsQueryStringInEndpoint(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAPICall(t.Context(), nil, "https://api.github.com", map[string]any{
		"endpoint": "/repos/owner/repo/labels?per_page=100",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query_params")
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkAPICallValidation(b *testing.B) {
	p := NewGitHubProvider()
	inputs := map[string]any{
		"endpoint": "/repos/owner/repo/labels",
		"method":   "GET",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		// Only tests validation path — will error on nil client but validates inputs first.
		_, _ = p.executeAPICall(context.Background(), nil, "https://api.github.com", inputs)
	}
}
