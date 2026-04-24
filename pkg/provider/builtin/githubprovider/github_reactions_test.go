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

// ─── Reaction Tests ──────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_AddReaction(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/issues/42/reactions", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "+1", body["content"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": float64(1), "content": "+1"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":        "add_reaction",
		"owner":            "test-org",
		"repo":             "test-repo",
		"api_base":         baseURL,
		"number":           float64(42),
		"reaction_content": "+1",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_AddReaction_Comment(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/issues/comments/123/reactions", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": float64(1), "content": "heart"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":        "add_reaction",
		"owner":            "test-org",
		"repo":             "test-repo",
		"api_base":         baseURL,
		"comment_id":       float64(123),
		"reaction_content": "heart",
		"reaction_subject": "issue_comment",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteAddReaction_MissingContent(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAddReaction(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"number": float64(1),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reaction_content")
}

func TestExecuteAddReaction_InvalidContent(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAddReaction(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"number":           float64(1),
		"reaction_content": "invalid",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid reaction_content")
}

func TestExecuteAddReaction_InvalidSubject(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAddReaction(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"reaction_content": "+1",
		"reaction_subject": "commit",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported reaction_subject")
}

func TestGitHubProvider_Execute_ListReactions(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/test-org/test-repo/issues/42/reactions")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{ //nolint:errcheck
			map[string]any{"id": float64(1), "content": "+1"},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_reactions",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
		"number":    float64(42),
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"]
	reactions := result.([]any)
	assert.Len(t, reactions, 1)
}

func TestExecuteDeleteReaction_MissingID(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeDeleteReaction(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"number": float64(1),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reaction_id")
}

func TestGitHubProvider_Execute_DeleteReaction(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/issues/42/reactions/100", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":   "delete_reaction",
		"owner":       "test-org",
		"repo":        "test-repo",
		"api_base":    baseURL,
		"number":      float64(42),
		"reaction_id": float64(100),
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_ListReactions_Comment(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/test-org/test-repo/issues/comments/99/reactions")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{ //nolint:errcheck
			map[string]any{"id": float64(200), "content": "rocket"},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":        "list_reactions",
		"owner":            "test-org",
		"repo":             "test-repo",
		"api_base":         baseURL,
		"comment_id":       float64(99),
		"reaction_subject": "issue_comment",
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"]
	reactions := result.([]any)
	assert.Len(t, reactions, 1)
}

func TestExecuteDeleteReaction_Comment(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/issues/comments/99/reactions/200", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":        "delete_reaction",
		"owner":            "test-org",
		"repo":             "test-repo",
		"api_base":         baseURL,
		"comment_id":       float64(99),
		"reaction_id":      float64(200),
		"reaction_subject": "issue_comment",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteListReactions_MissingNumber(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeListReactions(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "number")
}

func TestExecuteListReactions_InvalidSubject(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeListReactions(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"reaction_subject": "commit",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported reaction_subject")
}

func TestExecuteDeleteReaction_InvalidSubject(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeDeleteReaction(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"reaction_id":      float64(1),
		"reaction_subject": "commit",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported reaction_subject")
}

func BenchmarkReactionValidation(b *testing.B) {
	p := NewGitHubProvider()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.executeAddReaction(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
		_, _ = p.executeDeleteReaction(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
	}
}
