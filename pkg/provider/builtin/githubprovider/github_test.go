// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitHubProvider(t *testing.T) {
	p := NewGitHubProvider()
	desc := p.Descriptor()
	assert.Equal(t, ProviderName, desc.Name)
	assert.Equal(t, "GitHub API", desc.DisplayName)
	assert.NotEmpty(t, desc.Examples)
	assert.NotEmpty(t, desc.Links)
}

func TestGitHubProvider_Execute_GetRepo(t *testing.T) {
	repoData := map[string]any{
		"name":        "hello-world",
		"full_name":   "octocat/hello-world",
		"description": "My first repository on GitHub!",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/octocat/hello-world", r.URL.Path)
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(repoData)
	}))
	defer server.Close()

	p := NewGitHubProvider(WithHTTPClient(server.Client()))
	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_repo",
		"owner":     "octocat",
		"repo":      "hello-world",
		"api_base":  server.URL,
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, "hello-world", result["name"])
}

func TestGitHubProvider_Execute_GetFile(t *testing.T) {
	content := "# Hello World\nThis is a test."
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/octocat/hello-world/contents/README.md", r.URL.Path)
		assert.Equal(t, "main", r.URL.Query().Get("ref"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":    "README.md",
			"content": encoded,
			"type":    "file",
		})
	}))
	defer server.Close()

	p := NewGitHubProvider(WithHTTPClient(server.Client()))
	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_file",
		"owner":     "octocat",
		"repo":      "hello-world",
		"path":      "README.md",
		"ref":       "main",
		"api_base":  server.URL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, "README.md", result["name"])
	assert.Equal(t, content, result["decoded_content"])
}

func TestGitHubProvider_Execute_GetFile_MissingPath(t *testing.T) {
	p := NewGitHubProvider()
	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_file",
		"owner":     "octocat",
		"repo":      "hello-world",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "'path' is required")
}

func TestGitHubProvider_Execute_ListReleases(t *testing.T) {
	releases := []map[string]any{
		{"tag_name": "v1.0.0"},
		{"tag_name": "v0.9.0"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/cli/cli/releases", r.URL.Path)
		assert.Equal(t, "10", r.URL.Query().Get("per_page"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

	p := NewGitHubProvider(WithHTTPClient(server.Client()))
	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_releases",
		"owner":     "cli",
		"repo":      "cli",
		"per_page":  float64(10),
		"api_base":  server.URL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].([]any)
	assert.Len(t, result, 2)
}

func TestGitHubProvider_Execute_GetLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/cli/cli/releases/latest", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.50.0"})
	}))
	defer server.Close()

	p := NewGitHubProvider(WithHTTPClient(server.Client()))
	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_latest_release",
		"owner":     "cli",
		"repo":      "cli",
		"api_base":  server.URL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, "v2.50.0", result["tag_name"])
}

func TestGitHubProvider_Execute_ListPullRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/golang/go/pulls", r.URL.Path)
		assert.Equal(t, "open", r.URL.Query().Get("state"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "Fix bug"},
		})
	}))
	defer server.Close()

	p := NewGitHubProvider(WithHTTPClient(server.Client()))
	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_pull_requests",
		"owner":     "golang",
		"repo":      "go",
		"state":     "open",
		"api_base":  server.URL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].([]any)
	assert.Len(t, result, 1)
}

func TestGitHubProvider_Execute_GetPullRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/golang/go/pulls/42", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"number": 42, "title": "Great PR"})
	}))
	defer server.Close()

	p := NewGitHubProvider(WithHTTPClient(server.Client()))
	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_pull_request",
		"owner":     "golang",
		"repo":      "go",
		"number":    float64(42),
		"api_base":  server.URL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, float64(42), result["number"])
}

func TestGitHubProvider_Execute_GetPullRequest_MissingNumber(t *testing.T) {
	p := NewGitHubProvider()
	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_pull_request",
		"owner":     "golang",
		"repo":      "go",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "'number' is required")
}

func TestGitHubProvider_Execute_UnknownOperation(t *testing.T) {
	p := NewGitHubProvider()
	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "delete_everything",
		"owner":     "test",
		"repo":      "test",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown operation")
}

func TestGitHubProvider_Execute_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"})
	}))
	defer server.Close()

	p := NewGitHubProvider(WithHTTPClient(server.Client()))
	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_repo",
		"owner":     "nonexistent",
		"repo":      "repo",
		"api_base":  server.URL,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Contains(t, err.Error(), "Not Found")
}

func TestGitHubProvider_Execute_InvalidInput(t *testing.T) {
	p := NewGitHubProvider()
	_, err := p.Execute(context.Background(), "not a map")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected map input")
}

func TestGetIntInput(t *testing.T) {
	tests := []struct {
		name   string
		inputs map[string]any
		key    string
		want   int
		wantOK bool
	}{
		{"float64", map[string]any{"n": float64(42)}, "n", 42, true},
		{"int", map[string]any{"n": 42}, "n", 42, true},
		{"int64", map[string]any{"n": int64(42)}, "n", 42, true},
		{"missing", map[string]any{}, "n", 0, false},
		{"string", map[string]any{"n": "42"}, "n", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := getIntInput(tt.inputs, tt.key)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}
