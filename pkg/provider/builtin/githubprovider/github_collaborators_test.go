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

// ─── Collaborator Tests ──────────────────────────────────────────────────────

func TestGitHubProvider_Execute_ListCollaborators(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/test-org/test-repo/collaborators")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{ //nolint:errcheck
			map[string]any{"login": "user1"},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_collaborators",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"]
	collabs := result.([]any)
	assert.Len(t, collabs, 1)
}

func TestGitHubProvider_Execute_AddCollaborator(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/collaborators/new-user", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": float64(1)}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":  "add_collaborator",
		"owner":      "test-org",
		"repo":       "test-repo",
		"api_base":   baseURL,
		"username":   "new-user",
		"permission": "push",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteAddCollaborator_MissingUsername(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAddCollaborator(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "username")
}

func TestExecuteRemoveCollaborator_MissingUsername(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeRemoveCollaborator(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "username")
}

func TestGitHubProvider_Execute_RemoveCollaborator(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/collaborators/old-user", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "remove_collaborator",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
		"username":  "old-user",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func BenchmarkCollaboratorValidation(b *testing.B) {
	p := NewGitHubProvider()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.executeAddCollaborator(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
		_, _ = p.executeRemoveCollaborator(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
	}
}
