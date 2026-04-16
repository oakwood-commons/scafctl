// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProvider creates a GitHubProvider wired to a test server.
func testProvider(t *testing.T, handler http.HandlerFunc) (*GitHubProvider, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := httpc.NewClient(&httpc.ClientConfig{
		EnableCache: false,
		RetryMax:    0,
	})
	p := NewGitHubProvider(
		WithClient(client),
		// Use near-zero delays for tests to avoid sleeping.
		WithRetryConfig(5, time.Millisecond, 15, time.Millisecond, 3, time.Millisecond),
	)
	return p, server.URL
}

// graphqlHandler creates an http handler that checks for GraphQL POST and returns a canned response.
func graphqlHandler(t *testing.T, checkQuery func(query string, vars map[string]any), response map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req graphqlRequest
		require.NoError(t, json.Unmarshal(body, &req))

		w.Header().Set("Content-Type", "application/json")

		// Intercept viewerPermission queries (from waitForWriteAccess)
		if strings.Contains(req.Query, "viewerPermission") {
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repository": map[string]any{"viewerPermission": "ADMIN"},
				},
			})
			return
		}

		if checkQuery != nil {
			checkQuery(req.Query, req.Variables)
		}

		json.NewEncoder(w).Encode(response) //nolint:errcheck
	}
}

// ─── Descriptor Tests ────────────────────────────────────────────────────────

func TestNewGitHubProvider(t *testing.T) {
	p := NewGitHubProvider()
	desc := p.Descriptor()
	assert.Equal(t, ProviderName, desc.Name)
	assert.Equal(t, "GitHub API", desc.DisplayName)
	assert.NotEmpty(t, desc.Examples)
	assert.NotEmpty(t, desc.Links)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)
	assert.Contains(t, desc.Capabilities, provider.CapabilityAction)
	assert.Contains(t, desc.Capabilities, provider.CapabilityTransform)

	err := provider.ValidateDescriptor(desc)
	assert.NoError(t, err)
}

func TestWithRetryConfig_ClampsValues(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider(
		WithRetryConfig(0, -time.Second, -1, -time.Millisecond, 0, -time.Second),
	)
	assert.Equal(t, 1, p.commitMaxAttempts, "commitMaxAttempts should be clamped to 1")
	assert.Equal(t, time.Duration(0), p.commitRetryBackoff, "commitRetryBackoff should be clamped to 0")
	assert.Equal(t, 1, p.waitMaxAttempts, "waitMaxAttempts should be clamped to 1")
	assert.Equal(t, time.Duration(0), p.waitPollInterval, "waitPollInterval should be clamped to 0")
	assert.Equal(t, 1, p.initRepoMaxRetries, "initRepoMaxRetries should be clamped to 1")
	assert.Equal(t, time.Duration(0), p.initRepoRetryBackoff, "initRepoRetryBackoff should be clamped to 0")
}

func TestWithRetryConfig_PositiveValuesUnchanged(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider(
		WithRetryConfig(3, 2*time.Second, 10, time.Second, 5, 500*time.Millisecond),
	)
	assert.Equal(t, 3, p.commitMaxAttempts)
	assert.Equal(t, 2*time.Second, p.commitRetryBackoff)
	assert.Equal(t, 10, p.waitMaxAttempts)
	assert.Equal(t, time.Second, p.waitPollInterval)
	assert.Equal(t, 5, p.initRepoMaxRetries)
	assert.Equal(t, 500*time.Millisecond, p.initRepoRetryBackoff)
}

// ─── Read Operation Tests ────────────────────────────────────────────────────

func TestGitHubProvider_Execute_GetRepo(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t,
		func(query string, vars map[string]any) {
			assert.Contains(t, query, "repository(owner:")
			assert.Equal(t, "octocat", vars["owner"])
			assert.Equal(t, "hello-world", vars["name"])
		},
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"name":          "hello-world",
					"nameWithOwner": "octocat/hello-world",
					"description":   "My first repository on GitHub!",
					"isPrivate":     false,
					"defaultBranchRef": map[string]any{
						"name": "main",
					},
					"stargazerCount": float64(42),
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_repo",
		"owner":     "octocat",
		"repo":      "hello-world",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, "hello-world", result["name"])
	assert.Equal(t, "main", result["default_branch"])
}

func TestGitHubProvider_Execute_GetFile(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t,
		func(query string, vars map[string]any) {
			assert.Contains(t, query, "object(expression:")
			assert.Equal(t, "main:README.md", vars["expression"])
		},
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"object": map[string]any{
						"text":        "# Hello World\nThis is a test.",
						"byteSize":    float64(30),
						"oid":         "abc123",
						"isTruncated": false,
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_file",
		"owner":     "octocat",
		"repo":      "hello-world",
		"path":      "README.md",
		"ref":       "main",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, "README.md", result["name"])
	assert.Equal(t, "# Hello World\nThis is a test.", result["content"])
	assert.Equal(t, "abc123", result["sha"])
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
	p, baseURL := testProvider(t, graphqlHandler(t, nil,
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"releases": map[string]any{
						"nodes": []any{
							map[string]any{"tagName": "v1.0.0", "name": "Release 1.0"},
							map[string]any{"tagName": "v0.9.0", "name": "Release 0.9"},
						},
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_releases",
		"owner":     "cli",
		"repo":      "cli",
		"per_page":  float64(10),
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].([]any)
	assert.Len(t, result, 2)
}

func TestGitHubProvider_Execute_GetLatestRelease(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t, nil,
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"latestRelease": map[string]any{
						"tagName": "v2.50.0",
						"name":    "Release 2.50.0",
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_latest_release",
		"owner":     "cli",
		"repo":      "cli",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, "v2.50.0", result["tagName"])
}

func TestGitHubProvider_Execute_ListPullRequests(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t,
		func(query string, vars map[string]any) {
			assert.Contains(t, query, "pullRequests(")
		},
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequests": map[string]any{
						"nodes": []any{
							map[string]any{"number": float64(1), "title": "Fix bug", "state": "OPEN"},
						},
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_pull_requests",
		"owner":     "golang",
		"repo":      "go",
		"state":     "open",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].([]any)
	assert.Len(t, result, 1)
}

func TestGitHubProvider_Execute_GetPullRequest(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t,
		func(query string, vars map[string]any) {
			assert.Contains(t, query, "pullRequest(number:")
			assert.Equal(t, float64(42), vars["number"]) // JSON unmarshal produces float64
		},
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"id":     "PR_123",
						"number": float64(42),
						"title":  "Great PR",
						"state":  "OPEN",
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_pull_request",
		"owner":     "golang",
		"repo":      "go",
		"number":    float64(42),
		"api_base":  baseURL,
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

func TestGitHubProvider_Execute_ListIssues(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t, nil,
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"issues": map[string]any{
						"nodes": []any{
							map[string]any{"number": float64(1), "title": "Bug report", "state": "OPEN"},
							map[string]any{"number": float64(2), "title": "Feature request", "state": "OPEN"},
						},
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_issues",
		"owner":     "test-org",
		"repo":      "test-repo",
		"state":     "open",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].([]any)
	assert.Len(t, result, 2)
}

func TestGitHubProvider_Execute_GetIssue(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t, nil,
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"issue": map[string]any{
						"id":     "I_123",
						"number": float64(1),
						"title":  "Bug report",
						"state":  "OPEN",
						"body":   "Description of the bug",
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_issue",
		"owner":     "test-org",
		"repo":      "test-repo",
		"number":    float64(1),
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, float64(1), result["number"])
	assert.Equal(t, "Bug report", result["title"])
}

func TestGitHubProvider_Execute_ListBranches(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t, nil,
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"refs": map[string]any{
						"nodes": []any{
							map[string]any{"name": "main", "target": map[string]any{"oid": "abc123"}},
							map[string]any{"name": "dev", "target": map[string]any{"oid": "def456"}},
						},
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_branches",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].([]any)
	assert.Len(t, result, 2)
}

func TestGitHubProvider_Execute_GetHeadOID(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t,
		func(query string, vars map[string]any) {
			assert.Equal(t, "refs/heads/main", vars["qualifiedName"])
		},
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"ref": map[string]any{
						"target": map[string]any{"oid": "abc123def456789012345678901234567890abcd"},
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_head_oid",
		"owner":     "test-org",
		"repo":      "test-repo",
		"branch":    "main",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, "abc123def456789012345678901234567890abcd", result["oid"])
	assert.Equal(t, "main", result["branch"])
}

// ─── Write Operation Tests ───────────────────────────────────────────────────

func TestGitHubProvider_Execute_CreateIssue(t *testing.T) {
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphqlRequest
		json.Unmarshal(body, &req) //nolint:errcheck

		var resp map[string]any
		if strings.Contains(req.Query, "repository") && strings.Contains(req.Query, "{ id }") && !strings.Contains(req.Query, "labels") {
			// repo ID query
			resp = map[string]any{"data": map[string]any{"repository": map[string]any{"id": "R_123"}}}
		} else {
			// create issue mutation
			resp = map[string]any{"data": map[string]any{"createIssue": map[string]any{"issue": map[string]any{
				"id":     "I_456",
				"number": float64(10),
				"title":  "Test Issue",
				"url":    "https://github.com/test-org/test-repo/issues/10",
				"state":  "OPEN",
			}}}}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_issue",
		"owner":     "test-org",
		"repo":      "test-repo",
		"title":     "Test Issue",
		"body":      "This is a test issue",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "create_issue", data["operation"])
	result := data["result"].(map[string]any)
	assert.Equal(t, float64(10), result["number"])
}

func TestGitHubProvider_Execute_CreatePullRequest(t *testing.T) {
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphqlRequest
		json.Unmarshal(body, &req) //nolint:errcheck

		var resp map[string]any
		if strings.Contains(req.Query, "{ id }") && !strings.Contains(req.Query, "pullRequest") {
			resp = map[string]any{"data": map[string]any{"repository": map[string]any{"id": "R_123"}}}
		} else {
			resp = map[string]any{"data": map[string]any{"createPullRequest": map[string]any{"pullRequest": map[string]any{
				"id":          "PR_789",
				"number":      float64(5),
				"title":       "New Feature",
				"url":         "https://github.com/test/test/pull/5",
				"state":       "OPEN",
				"headRefName": "feature",
				"baseRefName": "main",
				"isDraft":     true,
			}}}}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_pull_request",
		"owner":     "test",
		"repo":      "test",
		"title":     "New Feature",
		"head":      "feature",
		"base":      "main",
		"draft":     true,
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, float64(5), result["number"])
	assert.Equal(t, true, result["isDraft"])
}

func TestGitHubProvider_Execute_CreateCommit(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t,
		func(query string, vars map[string]any) {
			assert.Contains(t, query, "createCommitOnBranch")
			input := vars["input"].(map[string]any)
			assert.Equal(t, "abc123def456789012345678901234567890abcd", input["expectedHeadOid"])
			branch := input["branch"].(map[string]any)
			assert.Equal(t, "test-org/test-repo", branch["repositoryNameWithOwner"])
			assert.Equal(t, "feature", branch["branchName"])
		},
		map[string]any{
			"data": map[string]any{
				"createCommitOnBranch": map[string]any{
					"commit": map[string]any{
						"oid":           "new456def",
						"url":           "https://github.com/test-org/test-repo/commit/new456",
						"committedDate": "2026-03-01T00:00:00Z",
						"message":       "feat: add files",
						"signature": map[string]any{
							"isValid": true,
							"signer":  map[string]any{"login": "web-flow"},
						},
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":         "create_commit",
		"owner":             "test-org",
		"repo":              "test-repo",
		"branch":            "feature",
		"message":           "feat: add files",
		"expected_head_oid": "abc123def456789012345678901234567890abcd",
		"additions": []any{
			map[string]any{"path": "src/main.go", "content": "package main"},
		},
		"api_base": baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "new456def", result["oid"])
	sig := result["signature"].(map[string]any)
	assert.Equal(t, true, sig["isValid"])
}

func TestGitHubProvider_Execute_CreateCommit_MissingFields(t *testing.T) {
	p := NewGitHubProvider()

	tests := []struct {
		name    string
		inputs  map[string]any
		wantErr string
	}{
		{
			name:    "missing branch",
			inputs:  map[string]any{"operation": "create_commit", "owner": "o", "repo": "r", "message": "m", "expected_head_oid": "abc", "additions": []any{map[string]any{"path": "f", "content": "c"}}},
			wantErr: "'branch' is required",
		},
		{
			name:    "missing message",
			inputs:  map[string]any{"operation": "create_commit", "owner": "o", "repo": "r", "branch": "b", "expected_head_oid": "abc", "additions": []any{map[string]any{"path": "f", "content": "c"}}},
			wantErr: "'message' is required",
		},
		{
			name:    "missing expected_head_oid",
			inputs:  map[string]any{"operation": "create_commit", "owner": "o", "repo": "r", "branch": "b", "message": "m", "additions": []any{map[string]any{"path": "f", "content": "c"}}},
			wantErr: "'expected_head_oid' is required",
		},
		{
			name:    "no changes",
			inputs:  map[string]any{"operation": "create_commit", "owner": "o", "repo": "r", "branch": "b", "message": "m", "expected_head_oid": "abc"},
			wantErr: "at least one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Execute(context.Background(), tt.inputs)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseFileChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		inputs   map[string]any
		wantAdds int
		wantDels int
		wantErr  string
	}{
		{
			name:     "valid additions and deletions",
			inputs:   map[string]any{"additions": []any{map[string]any{"path": "a.go", "content": "pkg"}}, "deletions": []any{map[string]any{"path": "b.go"}}},
			wantAdds: 1,
			wantDels: 1,
		},
		{
			name:   "empty inputs",
			inputs: map[string]any{},
		},
		{
			name:    "non-map addition entry",
			inputs:  map[string]any{"additions": []any{42}},
			wantErr: "each addition entry must be an object",
		},
		{
			name:    "addition missing path",
			inputs:  map[string]any{"additions": []any{map[string]any{"content": "c"}}},
			wantErr: "each addition must have 'path' and 'content'",
		},
		{
			name:    "addition missing content",
			inputs:  map[string]any{"additions": []any{map[string]any{"path": "f"}}},
			wantErr: "each addition must have 'path' and 'content'",
		},
		{
			name:    "non-map deletion entry",
			inputs:  map[string]any{"deletions": []any{"not-a-map"}},
			wantErr: "each deletion entry must be an object",
		},
		{
			name:    "deletion missing path",
			inputs:  map[string]any{"deletions": []any{map[string]any{"other": "val"}}},
			wantErr: "each deletion must have 'path'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			adds, dels, err := parseFileChanges(tt.inputs)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, adds, tt.wantAdds)
			assert.Len(t, dels, tt.wantDels)
		})
	}
}

// ─── Create Commit Retry Tests ───────────────────────────────────────────────

func TestGitHubProvider_Execute_CreateCommit_RetryOnForbidden(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphqlRequest
		json.Unmarshal(body, &req) //nolint:errcheck

		w.Header().Set("Content-Type", "application/json")

		// Intercept viewerPermission queries
		if strings.Contains(req.Query, "viewerPermission") {
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repository": map[string]any{"viewerPermission": "ADMIN"},
				},
			})
			return
		}

		n := callCount.Add(1)
		if n < 3 {
			// First two attempts: FORBIDDEN
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"errors": []any{
					map[string]any{
						"message": "Resource not accessible by personal access token",
						"type":    "FORBIDDEN",
					},
				},
			})
			return
		}
		// Third attempt: success
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"data": map[string]any{
				"createCommitOnBranch": map[string]any{
					"commit": map[string]any{
						"oid":           "new789",
						"url":           "https://github.com/o/r/commit/new789",
						"committedDate": "2026-01-01T00:00:00Z",
						"message":       "test commit",
					},
				},
			},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":         "create_commit",
		"owner":             "test-org",
		"repo":              "test-repo",
		"branch":            "main",
		"message":           "test commit",
		"expected_head_oid": "abc123def456789012345678901234567890abcd",
		"additions":         []any{map[string]any{"path": "f.go", "content": "pkg"}},
		"api_base":          baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	assert.Equal(t, int32(3), callCount.Load())
}

func TestGitHubProvider_Execute_CreateCommit_NonForbiddenError_NoRetry(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphqlRequest
		json.Unmarshal(body, &req) //nolint:errcheck

		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(req.Query, "viewerPermission") {
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repository": map[string]any{"viewerPermission": "ADMIN"},
				},
			})
			return
		}

		callCount.Add(1)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"errors": []any{
				map[string]any{
					"message": "Branch not found",
					"type":    "NOT_FOUND",
				},
			},
		})
	})

	_, err := p.Execute(context.Background(), map[string]any{
		"operation":         "create_commit",
		"owner":             "test-org",
		"repo":              "test-repo",
		"branch":            "nonexistent",
		"message":           "test",
		"expected_head_oid": "abc123def456789012345678901234567890abcd",
		"additions":         []any{map[string]any{"path": "f.go", "content": "pkg"}},
		"api_base":          baseURL,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Branch not found")
	assert.Equal(t, int32(1), callCount.Load(), "should not retry on non-FORBIDDEN errors")
}

func TestGitHubProvider_Execute_CreateCommit_RetryContextCancelled(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphqlRequest
		json.Unmarshal(body, &req) //nolint:errcheck

		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(req.Query, "viewerPermission") {
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repository": map[string]any{"viewerPermission": "ADMIN"},
				},
			})
			return
		}

		// Always return FORBIDDEN to trigger retry loop
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"errors": []any{
				map[string]any{
					"message": "Resource not accessible",
					"type":    "FORBIDDEN",
				},
			},
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := p.Execute(ctx, map[string]any{
		"operation":         "create_commit",
		"owner":             "test-org",
		"repo":              "test-repo",
		"branch":            "main",
		"message":           "test",
		"expected_head_oid": "abc123def456789012345678901234567890abcd",
		"additions":         []any{map[string]any{"path": "f.go", "content": "pkg"}},
		"api_base":          baseURL,
	})

	require.Error(t, err)
}

func TestGitHubProvider_Execute_CreateBranch(t *testing.T) {
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphqlRequest
		json.Unmarshal(body, &req) //nolint:errcheck

		var resp map[string]any
		if strings.Contains(req.Query, "{ id }") && !strings.Contains(req.Query, "createRef") {
			resp = map[string]any{"data": map[string]any{"repository": map[string]any{"id": "R_123"}}}
		} else {
			resp = map[string]any{"data": map[string]any{"createRef": map[string]any{"ref": map[string]any{
				"name":   "refs/heads/new-branch",
				"target": map[string]any{"oid": "abc123"},
			}}}}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_branch",
		"owner":     "test-org",
		"repo":      "test-repo",
		"branch":    "new-branch",
		"oid":       "abc123def456789012345678901234567890abcd",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_CreateRelease(t *testing.T) {
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		// REST endpoint
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/releases", r.URL.Path)
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		json.Unmarshal(body, &reqBody) //nolint:errcheck
		assert.Equal(t, "v1.0.0", reqBody["tag_name"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":       float64(1),
			"tag_name": "v1.0.0",
			"name":     "Release 1.0.0",
			"url":      "https://api.github.com/repos/test-org/test-repo/releases/1",
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_release",
		"owner":     "test-org",
		"repo":      "test-repo",
		"tag_name":  "v1.0.0",
		"name":      "Release 1.0.0",
		"body":      "First release",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "v1.0.0", result["tag_name"])
}

func TestGitHubProvider_Execute_DeleteRelease(t *testing.T) {
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/releases/42", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":  "delete_release",
		"owner":      "test-org",
		"repo":       "test-repo",
		"release_id": float64(42),
		"api_base":   baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, true, result["deleted"])
}

// ─── Error Handling Tests ────────────────────────────────────────────────────

func TestGitHubProvider_Execute_MissingOperation(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.Execute(context.Background(), map[string]any{
		"owner": "test",
		"repo":  "test",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "'operation' is required")
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

func TestGitHubProvider_Execute_GraphQLError(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t, nil,
		map[string]any{
			"errors": []any{
				map[string]any{"message": "Could not resolve to a Repository", "type": "NOT_FOUND"},
			},
		},
	))

	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_repo",
		"owner":     "nonexistent",
		"repo":      "repo",
		"api_base":  baseURL,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Could not resolve to a Repository")
}

func TestGitHubProvider_Execute_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"message": "Bad credentials"}) //nolint:errcheck
	}))
	defer server.Close()

	client := httpc.NewClient(&httpc.ClientConfig{EnableCache: false, RetryMax: 0})
	p := NewGitHubProvider(WithClient(client))

	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_repo",
		"owner":     "test",
		"repo":      "test",
		"api_base":  server.URL,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Bad credentials")
}

func TestGitHubProvider_Execute_InvalidInput(t *testing.T) {
	p := NewGitHubProvider()
	_, err := p.Execute(context.Background(), "not a map")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected map input")
}

func TestGitHubProvider_Execute_MissingOwnerRepo(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()

	tests := []struct {
		name   string
		inputs map[string]any
	}{
		{"missing both", map[string]any{"operation": "get_repo"}},
		{"missing repo", map[string]any{"operation": "create_issue", "owner": "org"}},
		{"missing owner", map[string]any{"operation": "create_release", "repo": "r"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := p.Execute(context.Background(), tt.inputs)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "'owner' and 'repo' are required")
		})
	}
}

func TestGitHubProvider_Execute_CreateRepo_SkipsOwnerRepoValidation(t *testing.T) {
	t.Parallel()

	// create_repo should NOT fail the owner/repo validation even without owner
	p := NewGitHubProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	output, err := p.Execute(ctx, map[string]any{
		"operation": "create_repo",
		"repo":      "test-repo",
	})
	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_ActionErrorReturnsError(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t, nil,
		map[string]any{
			"errors": []any{
				map[string]any{"message": "Repository not found", "type": "NOT_FOUND"},
			},
		},
	))

	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_issue",
		"owner":     "nonexistent",
		"repo":      "repo",
		"title":     "Test",
		"api_base":  baseURL,
	})

	// Action operations now return Go errors so the executor can stop downstream actions
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Repository not found")
}

// ─── Dry Run Tests ───────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_DryRun_ReadOperation(t *testing.T) {
	p := NewGitHubProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "get_repo",
		"owner":     "test",
		"repo":      "test",
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, "get_repo", result["operation"])
}

func TestGitHubProvider_Execute_DryRun_WriteOperation(t *testing.T) {
	p := NewGitHubProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "create_issue",
		"owner":     "test",
		"repo":      "test",
		"title":     "Test Issue",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "create_issue", data["operation"])
}

func TestGitHubProvider_Execute_DryRun_EmptyOperation(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	_, err := p.Execute(ctx, map[string]any{
		"owner": "test",
		"repo":  "test",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "'operation' is required")
}

// ─── Helper Tests ────────────────────────────────────────────────────────────

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

func TestMapPRState(t *testing.T) {
	assert.Equal(t, []string{"OPEN"}, mapPRState("open"))
	assert.Equal(t, []string{"CLOSED"}, mapPRState("closed"))
	assert.Equal(t, []string{"MERGED"}, mapPRState("merged"))
	assert.Nil(t, mapPRState("all"))
	assert.Nil(t, mapPRState(""))
}

func TestMapIssueState(t *testing.T) {
	assert.Equal(t, []string{"OPEN"}, mapIssueState("open"))
	assert.Equal(t, []string{"CLOSED"}, mapIssueState("closed"))
	assert.Nil(t, mapIssueState("all"))
	assert.Nil(t, mapIssueState(""))
}

func TestGraphqlEndpoint(t *testing.T) {
	assert.Equal(t, "https://api.github.com/graphql", graphqlEndpoint("https://api.github.com"))
	assert.Equal(t, "https://api.github.com/graphql", graphqlEndpoint("https://api.github.com/"))
	assert.Equal(t, "https://ghe.example.com/api/graphql", graphqlEndpoint("https://ghe.example.com/api/v3"))
	assert.Equal(t, "https://ghe.example.com/graphql", graphqlEndpoint("https://ghe.example.com"))
}

func TestPathBasename(t *testing.T) {
	assert.Equal(t, "file.go", pathBasename("src/main/file.go"))
	assert.Equal(t, "README.md", pathBasename("README.md"))
	assert.Equal(t, "file.go", pathBasename("a/b/c/file.go"))
}

func TestGraphQLError(t *testing.T) {
	single := &GraphQLError{Errors: []graphqlError{{Message: "not found", Type: "NOT_FOUND"}}}
	assert.Contains(t, single.Error(), "not found")
	assert.Contains(t, single.Error(), "NOT_FOUND")

	multi := &GraphQLError{Errors: []graphqlError{{Message: "err1"}, {Message: "err2"}}}
	assert.Contains(t, multi.Error(), "err1")
	assert.Contains(t, multi.Error(), "err2")

	empty := &GraphQLError{}
	assert.Equal(t, "unknown GraphQL error", empty.Error())
}

func TestMapIssueStateForMutation(t *testing.T) {
	assert.Equal(t, "OPEN", mapIssueStateForMutation("open"))
	assert.Equal(t, "OPEN", mapIssueStateForMutation("OPEN"))
	assert.Equal(t, "CLOSED", mapIssueStateForMutation("closed"))
	assert.Equal(t, "CLOSED", mapIssueStateForMutation("CLOSED"))
	assert.Equal(t, "OTHER", mapIssueStateForMutation("OTHER"))
}
