// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubProvider_StateCapability(t *testing.T) {
	p := NewGitHubProvider()
	desc := p.Descriptor()
	assert.Contains(t, desc.Capabilities, provider.CapabilityState)
	assert.Contains(t, desc.OutputSchemas, provider.CapabilityState)
}

func TestGitHubProvider_StateLoad_Found(t *testing.T) {
	sd := state.NewData()
	sd.Values = map[string]*state.Entry{
		"key1": {Value: "val1", Type: "string"},
	}
	stateJSON, _ := json.Marshal(sd)

	p, baseURL := testProvider(t, graphqlHandler(t, nil, map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"object": map[string]any{
					"text": string(stateJSON),
				},
			},
		},
	}))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation": "state_load",
		"owner":     "org",
		"repo":      "repo",
		"path":      ".scafctl/state.json",
		"branch":    "main",
		"api_base":  baseURL,
	})
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))

	loaded, ok := data["data"].(*state.Data)
	require.True(t, ok)
	assert.Equal(t, "val1", loaded.Values["key1"].Value)
}

func TestGitHubProvider_StateLoad_NotFound(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t, nil, map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"object": nil,
			},
		},
	}))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation": "state_load",
		"owner":     "org",
		"repo":      "repo",
		"path":      ".scafctl/state.json",
		"branch":    "main",
		"api_base":  baseURL,
	})
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.NotNil(t, data["data"])
}

func TestGitHubProvider_StateSave(t *testing.T) {
	callCount := 0
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

		callCount++
		switch {
		case strings.Contains(req.Query, "qualifiedName"):
			// get_head_oid response
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repository": map[string]any{
						"ref": map[string]any{
							"target": map[string]any{"oid": "abc123"},
						},
					},
				},
			})
		case strings.Contains(req.Query, "createCommitOnBranch"):
			// create_commit response
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"createCommitOnBranch": map[string]any{
						"commit": map[string]any{"oid": "def456"},
					},
				},
			})
		default:
			t.Errorf("unexpected query: %s", req.Query)
		}
	})

	sd := state.NewData()
	sd.Values = map[string]*state.Entry{
		"env": {Value: "prod", Type: "string"},
	}

	result, err := p.Execute(context.Background(), map[string]any{
		"operation": "state_save",
		"owner":     "org",
		"repo":      "repo",
		"path":      ".scafctl/state.json",
		"branch":    "main",
		"api_base":  baseURL,
		"data":      sd,
	})
	require.NoError(t, err)
	assert.True(t, result.Data.(map[string]any)["success"].(bool))
	assert.Equal(t, 2, callCount) // get_head_oid + create_commit
}

func TestGitHubProvider_StateDelete_Found(t *testing.T) {
	callCount := 0
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

		callCount++
		switch {
		case strings.Contains(req.Query, "expression") && !strings.Contains(req.Query, "createCommit"):
			// File exists check
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repository": map[string]any{
						"object": map[string]any{"oid": "exists"},
					},
				},
			})
		case strings.Contains(req.Query, "qualifiedName"):
			// get_head_oid
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"repository": map[string]any{
						"ref": map[string]any{
							"target": map[string]any{"oid": "abc123"},
						},
					},
				},
			})
		case strings.Contains(req.Query, "createCommitOnBranch"):
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": map[string]any{
					"createCommitOnBranch": map[string]any{
						"commit": map[string]any{"oid": "del789"},
					},
				},
			})
		default:
			t.Errorf("unexpected query: %s", req.Query)
		}
	})

	result, err := p.Execute(context.Background(), map[string]any{
		"operation": "state_delete",
		"owner":     "org",
		"repo":      "repo",
		"path":      ".scafctl/state.json",
		"branch":    "main",
		"api_base":  baseURL,
	})
	require.NoError(t, err)
	assert.True(t, result.Data.(map[string]any)["success"].(bool))
	assert.Equal(t, 3, callCount) // check + get_head_oid + create_commit
}

func TestGitHubProvider_StateDelete_NotFound(t *testing.T) {
	p, baseURL := testProvider(t, graphqlHandler(t, nil, map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"object": nil,
			},
		},
	}))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation": "state_delete",
		"owner":     "org",
		"repo":      "repo",
		"path":      ".scafctl/state.json",
		"branch":    "main",
		"api_base":  baseURL,
	})
	require.NoError(t, err)
	assert.True(t, result.Data.(map[string]any)["success"].(bool))
}

func TestGitHubProvider_StateDryRun(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		hasData   bool
	}{
		{"load", "state_load", true},
		{"save", "state_save", false},
		{"delete", "state_delete", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewGitHubProvider()
			ctx := provider.WithDryRun(context.Background(), true)

			result, err := p.Execute(ctx, map[string]any{
				"operation": tt.operation,
				"owner":     "org",
				"repo":      "repo",
				"path":      "state.json",
				"branch":    "main",
			})
			require.NoError(t, err)
			data := result.Data.(map[string]any)
			assert.True(t, data["success"].(bool))
			if tt.hasData {
				assert.NotNil(t, data["data"])
			}
		})
	}
}

func TestGitHubProvider_StateMissingInputs(t *testing.T) {
	tests := []struct {
		name    string
		inputs  map[string]any
		errText string
	}{
		{
			name:    "missing path",
			inputs:  map[string]any{"operation": "state_load", "owner": "o", "repo": "r", "branch": "main"},
			errText: "path",
		},
		{
			name:    "missing branch",
			inputs:  map[string]any{"operation": "state_load", "owner": "o", "repo": "r", "path": "s.json"},
			errText: "branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, baseURL := testProvider(t, graphqlHandler(t, nil, map[string]any{}))
			tt.inputs["api_base"] = baseURL

			_, err := p.Execute(context.Background(), tt.inputs)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errText)
		})
	}
}

func TestStateCommitMessage(t *testing.T) {
	tests := []struct {
		name     string
		inputs   map[string]any
		fallback string
		expected string
	}{
		{"custom", map[string]any{"message": "custom msg"}, "default", "custom msg"},
		{"fallback", map[string]any{}, "default msg", "default msg"},
		{"empty", map[string]any{"message": ""}, "fallback", "fallback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, stateCommitMessage(tt.inputs, tt.fallback))
		})
	}
}
