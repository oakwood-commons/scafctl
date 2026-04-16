// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// viewerPermissionResponse returns a mock GraphQL response for the waitForWriteAccess query.
func viewerPermissionResponse() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"viewerPermission": "ADMIN",
			},
		},
	}
}

// ─── Create Repository Tests ─────────────────────────────────────────────────

func TestGitHubProvider_Execute_CreateRepo(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphqlRequest
		json.Unmarshal(body, &req) //nolint:errcheck

		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		// Handle viewerPermission query (waitForWriteAccess)
		if strings.Contains(req.Query, "viewerPermission") {
			json.NewEncoder(w).Encode(viewerPermissionResponse()) //nolint:errcheck
			return
		}

		if n == 1 {
			// resolveOwnerID
			assert.Contains(t, req.Query, "repositoryOwner")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repositoryOwner": map[string]any{"id": "O_test123"},
				},
			})
			return
		}
		// createRepository
		assert.Contains(t, req.Query, "createRepository")
		input := req.Variables["input"].(map[string]any)
		assert.Equal(t, "my-new-repo", input["name"])
		assert.Equal(t, "PRIVATE", input["visibility"])
		assert.Equal(t, "A test repo", input["description"])
		assert.Equal(t, "O_test123", input["ownerId"])
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"data": map[string]any{
				"createRepository": map[string]any{
					"repository": map[string]any{
						"id":            "R_123",
						"name":          "my-new-repo",
						"nameWithOwner": "test-org/my-new-repo",
						"url":           "https://github.com/test-org/my-new-repo",
						"isPrivate":     true,
						"createdAt":     "2026-01-01T00:00:00Z",
					},
				},
			},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":   "create_repo",
		"owner":       "test-org",
		"repo":        "my-new-repo",
		"description": "A test repo",
		"visibility":  "private",
		"api_base":    baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "my-new-repo", result["name"])
	assert.Equal(t, true, result["isPrivate"])
}

func TestGitHubProvider_Execute_CreateRepo_DefaultVisibility(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, graphqlHandler(t,
		func(_ string, vars map[string]any) {
			input := vars["input"].(map[string]any)
			assert.Equal(t, "PUBLIC", input["visibility"])
		},
		map[string]any{
			"data": map[string]any{
				"createRepository": map[string]any{
					"repository": map[string]any{
						"id":            "R_456",
						"name":          "public-repo",
						"nameWithOwner": "testuser/public-repo",
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_repo",
		"repo":      "public-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_CreateRepo_MissingRepo(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_repo",
		"owner":     "test-org",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "'repo' is required")
}

func TestGitHubProvider_Execute_CreateRepo_MissingNameWithOwner(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphqlRequest
		json.Unmarshal(body, &req) //nolint:errcheck

		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		// Handle viewerPermission query (waitForWriteAccess)
		if strings.Contains(req.Query, "viewerPermission") {
			json.NewEncoder(w).Encode(viewerPermissionResponse()) //nolint:errcheck
			return
		}

		if strings.Contains(req.Query, "repositoryOwner") {
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repositoryOwner": map[string]any{"id": "O_test123"},
				},
			})
			return
		}

		// createRepository — deliberately omit nameWithOwner
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"data": map[string]any{
				"createRepository": map[string]any{
					"repository": map[string]any{
						"id":   "R_999",
						"name": "no-nwo-repo",
					},
				},
			},
		})
	})

	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_repo",
		"owner":     "test-org",
		"repo":      "no-nwo-repo",
		"api_base":  baseURL,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing nameWithOwner")
}

func TestExtractNameWithOwner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   *provider.Output
		expected string
	}{
		{
			name:     "nil output",
			output:   nil,
			expected: "",
		},
		{
			name: "valid output",
			output: &provider.Output{
				Data: map[string]any{
					"result": map[string]any{
						"nameWithOwner": "my-org/my-repo",
					},
				},
			},
			expected: "my-org/my-repo",
		},
		{
			name: "missing nameWithOwner",
			output: &provider.Output{
				Data: map[string]any{
					"result": map[string]any{
						"name": "my-repo",
					},
				},
			},
			expected: "",
		},
		{
			name: "missing result",
			output: &provider.Output{
				Data: map[string]any{},
			},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, extractNameWithOwner(tc.output))
		})
	}
}

func TestExtractRESTOwner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   *provider.Output
		expected string
	}{
		{
			name:     "nil output",
			output:   nil,
			expected: "",
		},
		{
			name: "valid full_name",
			output: &provider.Output{
				Data: map[string]any{
					"result": map[string]any{
						"full_name": "actual-user/my-repo",
					},
				},
			},
			expected: "actual-user",
		},
		{
			name: "missing full_name",
			output: &provider.Output{
				Data: map[string]any{
					"result": map[string]any{
						"name": "my-repo",
					},
				},
			},
			expected: "",
		},
		{
			name: "missing result",
			output: &provider.Output{
				Data: map[string]any{},
			},
			expected: "",
		},
		{
			name: "full_name without slash",
			output: &provider.Output{
				Data: map[string]any{
					"result": map[string]any{
						"full_name": "noslash",
					},
				},
			},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, extractRESTOwner(tc.output))
		})
	}
}

func TestGitHubProvider_Execute_CreateRepo_WithOwner(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphqlRequest
		json.Unmarshal(body, &req) //nolint:errcheck

		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		// Handle viewerPermission query (waitForWriteAccess)
		if strings.Contains(req.Query, "viewerPermission") {
			json.NewEncoder(w).Encode(viewerPermissionResponse()) //nolint:errcheck
			return
		}

		if n == 1 {
			// First call: resolveOwnerID
			assert.Contains(t, req.Query, "repositoryOwner")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repositoryOwner": map[string]any{
						"id": "O_org123",
					},
				},
			})
			return
		}
		// Second call: createRepository
		assert.Contains(t, req.Query, "createRepository")
		input := req.Variables["input"].(map[string]any)
		assert.Equal(t, "O_org123", input["ownerId"])
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"data": map[string]any{
				"createRepository": map[string]any{
					"repository": map[string]any{
						"id":            "R_789",
						"name":          "org-repo",
						"nameWithOwner": "my-org/org-repo",
					},
				},
			},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_repo",
		"owner":     "my-org",
		"repo":      "org-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "org-repo", result["name"])
}

func TestGitHubProvider_Execute_CreateRepo_GraphQLForbidden_FallsBackToREST(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		// Handle GraphQL requests
		if r.URL.Path == "/graphql" {
			body, _ := io.ReadAll(r.Body)
			var req graphqlRequest
			json.Unmarshal(body, &req) //nolint:errcheck

			// Handle viewerPermission query (waitForWriteAccess)
			if strings.Contains(req.Query, "viewerPermission") {
				json.NewEncoder(w).Encode(viewerPermissionResponse()) //nolint:errcheck
				return
			}

			// GraphQL createRepository returns FORBIDDEN (EMU user)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"errors": []any{
					map[string]any{
						"message": "As an Enterprise Managed User, you cannot access this content",
						"type":    "FORBIDDEN",
					},
				},
			})
			return
		}

		// REST fallback: POST /orgs/{org}/repos
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/orgs/emu-org/repos", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		json.Unmarshal(body, &reqBody) //nolint:errcheck
		assert.Equal(t, "emu-repo", reqBody["name"])
		assert.Equal(t, true, reqBody["auto_init"])

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":        float64(200),
			"name":      "emu-repo",
			"full_name": "emu-org/emu-repo",
			"private":   false,
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_repo",
		"owner":     "emu-org",
		"repo":      "emu-repo",
		"auto_init": true,
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "emu-repo", result["name"])
	assert.Equal(t, int32(3), callCount.Load()) // GraphQL FORBIDDEN + REST create + viewerPermission
}

func TestGitHubProvider_Execute_CreateRepo_AutoInit_UserRepo(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		// Handle GraphQL requests
		if r.URL.Path == "/graphql" {
			body, _ := io.ReadAll(r.Body)
			var req graphqlRequest
			json.Unmarshal(body, &req) //nolint:errcheck

			// Handle viewerPermission query (waitForWriteAccess)
			if strings.Contains(req.Query, "viewerPermission") {
				json.NewEncoder(w).Encode(viewerPermissionResponse()) //nolint:errcheck
				return
			}

			// GraphQL createRepository
			assert.Contains(t, req.Query, "createRepository")
			input := req.Variables["input"].(map[string]any)
			assert.Equal(t, "auto-init-repo", input["name"])
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"createRepository": map[string]any{
						"repository": map[string]any{
							"id":            "R_100",
							"name":          "auto-init-repo",
							"nameWithOwner": "testuser/auto-init-repo",
						},
					},
				},
			})
			return
		}
		// REST Contents API to create README.md
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/testuser/auto-init-repo/contents/README.md", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"content": map[string]any{"name": "README.md"}}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":   "create_repo",
		"repo":        "auto-init-repo",
		"description": "An auto-init repo",
		"auto_init":   true,
		"api_base":    baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "auto-init-repo", result["name"])
	assert.Equal(t, int32(3), callCount.Load()) // createRepo + viewerPermission + README
}

func TestGitHubProvider_Execute_CreateRepo_AutoInit_OrgRepo(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		// Handle GraphQL requests
		if r.URL.Path == "/graphql" {
			body, _ := io.ReadAll(r.Body)
			var req graphqlRequest
			json.Unmarshal(body, &req) //nolint:errcheck

			// Handle viewerPermission query (waitForWriteAccess)
			if strings.Contains(req.Query, "viewerPermission") {
				json.NewEncoder(w).Encode(viewerPermissionResponse()) //nolint:errcheck
				return
			}

			if strings.Contains(req.Query, "repositoryOwner") {
				// resolveOwnerID
				json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
					"data": map[string]any{
						"repositoryOwner": map[string]any{"id": "O_org456"},
					},
				})
				return
			}

			// GraphQL createRepository
			assert.Contains(t, req.Query, "createRepository")
			input := req.Variables["input"].(map[string]any)
			assert.Equal(t, "org-auto-repo", input["name"])
			assert.Equal(t, "O_org456", input["ownerId"])
			assert.Equal(t, "PRIVATE", input["visibility"])
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"createRepository": map[string]any{
						"repository": map[string]any{
							"id":            "R_101",
							"name":          "org-auto-repo",
							"nameWithOwner": "my-org/org-auto-repo",
						},
					},
				},
			})
			return
		}
		// REST Contents API to create README.md
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/my-org/org-auto-repo/contents/README.md", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"content": map[string]any{"name": "README.md"}}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":  "create_repo",
		"owner":      "my-org",
		"repo":       "org-auto-repo",
		"visibility": "private",
		"auto_init":  true,
		"api_base":   baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "org-auto-repo", result["name"])
	assert.Equal(t, int32(4), callCount.Load()) // resolveOwnerID + createRepo + viewerPermission + README
}

// ─── Wait for Write Access Tests ─────────────────────────────────────────────

func TestGitHubProvider_WaitForWriteAccess_ImmediateAdmin(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(viewerPermissionResponse()) //nolint:errcheck
	})

	client := p.getClient(context.Background())
	err := p.waitForWriteAccess(context.Background(), client, baseURL, "test-org", "test-repo")
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestGitHubProvider_WaitForWriteAccess_EventualWrite(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			// First two calls: READ only
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repository": map[string]any{"viewerPermission": "READ"},
				},
			})
			return
		}
		// Third call: WRITE
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"data": map[string]any{
				"repository": map[string]any{"viewerPermission": "WRITE"},
			},
		})
	})

	client := p.getClient(context.Background())
	err := p.waitForWriteAccess(context.Background(), client, baseURL, "test-org", "test-repo")
	require.NoError(t, err)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestGitHubProvider_WaitForWriteAccess_ContextCancelled(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"data": map[string]any{
				"repository": map[string]any{"viewerPermission": "READ"},
			},
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	client := p.getClient(ctx)
	err := p.waitForWriteAccess(ctx, client, baseURL, "test-org", "test-repo")
	require.Error(t, err)
}

// ─── Create Ruleset Tests ────────────────────────────────────────────────────

func TestGitHubProvider_Execute_CreateRuleset_BranchProtection(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/rulesets", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		json.Unmarshal(body, &reqBody) //nolint:errcheck

		assert.Equal(t, "main branch protection", reqBody["name"])
		assert.Equal(t, "branch", reqBody["target"])
		assert.Equal(t, "active", reqBody["enforcement"])

		conditions := reqBody["conditions"].(map[string]any)
		refName := conditions["ref_name"].(map[string]any)
		includes := refName["include"].([]any)
		assert.Contains(t, includes, "refs/heads/main")

		rules := reqBody["rules"].([]any)
		assert.GreaterOrEqual(t, len(rules), 3)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":          float64(1),
			"name":        "main branch protection",
			"target":      "branch",
			"enforcement": "active",
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":                       "create_ruleset",
		"owner":                           "test-org",
		"repo":                            "test-repo",
		"ruleset_name":                    "main branch protection",
		"target":                          "branch",
		"enforcement":                     "active",
		"include_refs":                    []any{"refs/heads/main"},
		"required_status_checks_contexts": []any{"test", "lint"},
		"required_approving_review_count": float64(1),
		"required_linear_history":         true,
		"requires_commit_signatures":      true,
		"allow_force_pushes":              false,
		"allow_deletions":                 false,
		"api_base":                        baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "main branch protection", result["name"])
}

func TestGitHubProvider_Execute_CreateRuleset_TagProtection(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/rulesets", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		json.Unmarshal(body, &reqBody) //nolint:errcheck

		assert.Equal(t, "tag", reqBody["target"])
		conditions := reqBody["conditions"].(map[string]any)
		refName := conditions["ref_name"].(map[string]any)
		includes := refName["include"].([]any)
		assert.Contains(t, includes, "refs/tags/v*")

		rules := reqBody["rules"].([]any)
		ruleTypes := make([]string, 0, len(rules))
		for _, r := range rules {
			rm := r.(map[string]any)
			ruleTypes = append(ruleTypes, rm["type"].(string))
		}
		assert.Contains(t, ruleTypes, "deletion")
		assert.Contains(t, ruleTypes, "non_fast_forward")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":          float64(2),
			"name":        "version tag protection",
			"target":      "tag",
			"enforcement": "active",
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":          "create_ruleset",
		"owner":              "test-org",
		"repo":               "test-repo",
		"ruleset_name":       "version tag protection",
		"target":             "tag",
		"include_refs":       []any{"refs/tags/v*"},
		"allow_force_pushes": false,
		"allow_deletions":    false,
		"api_base":           baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "tag", result["target"])
}

func TestGitHubProvider_Execute_CreateRuleset_MissingName(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.Execute(context.Background(), map[string]any{
		"operation":    "create_ruleset",
		"owner":        "test-org",
		"repo":         "test-repo",
		"include_refs": []any{"refs/heads/main"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "'ruleset_name' is required")
}

func TestGitHubProvider_Execute_CreateRuleset_MissingIncludeRefs(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.Execute(context.Background(), map[string]any{
		"operation":    "create_ruleset",
		"owner":        "test-org",
		"repo":         "test-repo",
		"ruleset_name": "test",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "'include_refs' is required")
}

func TestBuildRulesetRules_AllRules(t *testing.T) {
	t.Parallel()

	inputs := map[string]any{
		"required_status_checks_contexts": []any{"test", "lint"},
		"required_approving_review_count": float64(2),
		"requires_commit_signatures":      true,
		"required_linear_history":         true,
		"allow_force_pushes":              false,
		"allow_deletions":                 false,
	}

	rules := buildRulesetRules(inputs)
	assert.Len(t, rules, 6)

	ruleTypes := make([]string, 0, len(rules))
	for _, r := range rules {
		ruleTypes = append(ruleTypes, r["type"].(string))
	}
	assert.Contains(t, ruleTypes, "required_status_checks")
	assert.Contains(t, ruleTypes, "pull_request")
	assert.Contains(t, ruleTypes, "required_signatures")
	assert.Contains(t, ruleTypes, "required_linear_history")
	assert.Contains(t, ruleTypes, "non_fast_forward")
	assert.Contains(t, ruleTypes, "deletion")
}

func TestBuildRulesetRules_Empty(t *testing.T) {
	t.Parallel()

	rules := buildRulesetRules(map[string]any{})
	assert.Empty(t, rules)
}

func TestBuildRulesetRules_AllowForcePush_True(t *testing.T) {
	t.Parallel()

	// When allow_force_pushes is true, non_fast_forward rule should NOT be added
	rules := buildRulesetRules(map[string]any{
		"allow_force_pushes": true,
	})
	for _, r := range rules {
		assert.NotEqual(t, "non_fast_forward", r["type"])
	}
}

// ─── Enable Vulnerability Alerts Tests ───────────────────────────────────────

func TestGitHubProvider_Execute_EnableVulnerabilityAlerts(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/vulnerability-alerts", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "enable_vulnerability_alerts",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, true, result["enabled"])
}

// ─── Enable Automated Security Fixes Tests ───────────────────────────────────

func TestGitHubProvider_Execute_EnableAutomatedSecurityFixes(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/automated-security-fixes", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "enable_automated_security_fixes",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, true, result["enabled"])
}

// ─── REST User Fallback Tests ────────────────────────────────────────────────

func TestGitHubProvider_Execute_CreateRepo_RESTFallback_UserRepo(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		// Handle GraphQL requests
		if r.URL.Path == "/graphql" {
			body, _ := io.ReadAll(r.Body)
			var req graphqlRequest
			json.Unmarshal(body, &req) //nolint:errcheck

			// Handle viewerPermission query (waitForWriteAccess)
			if strings.Contains(req.Query, "viewerPermission") {
				json.NewEncoder(w).Encode(viewerPermissionResponse()) //nolint:errcheck
				return
			}

			// GraphQL createRepository returns FORBIDDEN (EMU)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"errors": []any{
					map[string]any{
						"message": "forbidden",
						"type":    "FORBIDDEN",
					},
				},
			})
			return
		}

		// REST: org endpoint returns 404 (owner is a user, not an org)
		if r.URL.Path == "/orgs/my-user/repos" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"}) //nolint:errcheck
			return
		}

		// REST: user endpoint succeeds
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/user/repos", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":        float64(300),
			"name":      "user-repo",
			"full_name": "my-user/user-repo",
			"private":   false,
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_repo",
		"owner":     "my-user",
		"repo":      "user-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "user-repo", result["name"])
}

func TestGitHubProvider_Execute_CreateRepo_RESTFallback_OwnerDerivedFromResponse(t *testing.T) {
	t.Parallel()

	// Verifies that waitForWriteAccess uses the canonical owner from the
	// REST response (full_name), not the caller-provided owner.
	var mu sync.Mutex
	var permOwner string
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/graphql" {
			body, _ := io.ReadAll(r.Body)
			var req graphqlRequest
			json.Unmarshal(body, &req) //nolint:errcheck

			if strings.Contains(req.Query, "viewerPermission") {
				// Capture the owner used in the permission check
				mu.Lock()
				permOwner, _ = req.Variables["owner"].(string)
				mu.Unlock()
				json.NewEncoder(w).Encode(viewerPermissionResponse()) //nolint:errcheck
				return
			}

			// GraphQL createRepository returns FORBIDDEN (EMU)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"errors": []any{
					map[string]any{"message": "forbidden", "type": "FORBIDDEN"},
				},
			})
			return
		}

		// REST: org endpoint returns 404
		if r.URL.Path == "/orgs/wrong-owner/repos" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"}) //nolint:errcheck
			return
		}

		// REST: user endpoint succeeds — actual owner is "actual-user"
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":        float64(400),
			"name":      "my-repo",
			"full_name": "actual-user/my-repo",
			"private":   false,
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_repo",
		"owner":     "wrong-owner",
		"repo":      "my-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	// waitForWriteAccess should have polled "actual-user", not "wrong-owner"
	mu.Lock()
	assert.Equal(t, "actual-user", permOwner)
	mu.Unlock()
}

// ─── Dry Run Tests ───────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_CreateRepo_DryRun(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "create_repo",
		"owner":     "test-org",
		"repo":      "test-repo",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_CreateRuleset_DryRun(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	output, err := p.Execute(ctx, map[string]any{
		"operation":    "create_ruleset",
		"owner":        "test-org",
		"repo":         "test-repo",
		"ruleset_name": "test",
		"include_refs": []any{"refs/heads/main"},
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Init Repo With README Tests ─────────────────────────────────────────────

func TestGitHubProvider_InitRepoWithReadme_Success(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/my-org/my-repo/contents/README.md", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"content": map[string]any{"name": "README.md"}}) //nolint:errcheck
	})

	client := p.getClient(context.Background())
	err := p.initRepoWithReadme(context.Background(), client, baseURL, "my-org/my-repo")
	require.NoError(t, err)
}

func TestGitHubProvider_InitRepoWithReadme_RetryOn404(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"}) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"content": map[string]any{"name": "README.md"}}) //nolint:errcheck
	})

	client := p.getClient(context.Background())
	err := p.initRepoWithReadme(context.Background(), client, baseURL, "my-org/my-repo")
	require.NoError(t, err)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestGitHubProvider_InitRepoWithReadme_ContextCancelled(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"}) //nolint:errcheck
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	client := p.getClient(ctx)
	err := p.initRepoWithReadme(ctx, client, baseURL, "my-org/my-repo")
	require.Error(t, err)
}

func TestGitHubProvider_InitRepoWithReadme_NonRetryableError(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{"message": "Forbidden"}) //nolint:errcheck
	})

	client := p.getClient(context.Background())
	err := p.initRepoWithReadme(context.Background(), client, baseURL, "my-org/my-repo")
	require.Error(t, err)
	assert.Equal(t, int32(1), callCount.Load(), "should not retry on non-404 errors")
}

// ─── RESTError Tests ─────────────────────────────────────────────────────────

func TestRESTError(t *testing.T) {
	t.Parallel()

	err := &restError{StatusCode: 404, Message: "Not Found"}
	assert.Equal(t, "GitHub API error (HTTP 404): Not Found", err.Error())
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkBuildRulesetRules(b *testing.B) {
	inputs := map[string]any{
		"required_status_checks_contexts": []any{"test", "lint", "build"},
		"required_approving_review_count": float64(2),
		"requires_commit_signatures":      true,
		"required_linear_history":         true,
		"allow_force_pushes":              false,
		"allow_deletions":                 false,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buildRulesetRules(inputs)
	}
}

func BenchmarkIsGraphQLForbidden(b *testing.B) {
	forbidden := &GraphQLError{Errors: []graphqlError{{Message: "forbidden", Type: "FORBIDDEN"}}}
	notForbidden := &GraphQLError{Errors: []graphqlError{{Message: "not found", Type: "NOT_FOUND"}}}

	b.Run("forbidden", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			isGraphQLForbidden(forbidden)
		}
	})

	b.Run("not_forbidden", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			isGraphQLForbidden(notForbidden)
		}
	})
}

func BenchmarkGitHubProvider_Execute_CreateRepo_DryRun(b *testing.B) {
	p := NewGitHubProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation": "create_repo",
		"owner":     "test-org",
		"repo":      "bench-repo",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}

func BenchmarkGitHubProvider_Execute_CreateRuleset_DryRun(b *testing.B) {
	p := NewGitHubProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation":    "create_ruleset",
		"owner":        "test-org",
		"repo":         "bench-repo",
		"ruleset_name": "bench protection",
		"include_refs": []any{"refs/heads/main"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}
