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

// ─── Label Tests ─────────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_ListLabels(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/test-org/test-repo/labels")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{ //nolint:errcheck
			map[string]any{"name": "bug", "color": "d73a4a"},
			map[string]any{"name": "enhancement", "color": "a2eeef"},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_labels",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result := output.Data.(map[string]any)["result"]
	labels := result.([]any)
	assert.Len(t, labels, 2)
}

func TestGitHubProvider_Execute_CreateLabel(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/labels", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "bug", body["name"])
		assert.Equal(t, "d73a4a", body["color"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"name": "bug", "color": "d73a4a", "id": float64(1)}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":  "create_label",
		"owner":      "test-org",
		"repo":       "test-repo",
		"api_base":   baseURL,
		"label_name": "bug",
		"color":      "d73a4a",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_DeleteLabel(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/labels/bug", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":  "delete_label",
		"owner":      "test-org",
		"repo":       "test-repo",
		"api_base":   baseURL,
		"label_name": "bug",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteCreateLabel_MissingName(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCreateLabel(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "label_name")
}

func TestExecuteDeleteLabel_MissingName(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeDeleteLabel(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "label_name")
}

func TestGitHubProvider_Execute_AddLabelsToIssue(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/issues/5/labels", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{ //nolint:errcheck
			map[string]any{"name": "bug"},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "add_labels_to_issue",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
		"number":    float64(5),
		"labels":    []any{"bug"},
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteAddLabelsToIssue_MissingNumber(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeAddLabelsToIssue(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"labels": []any{"bug"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "number")
}

func TestExecuteRemoveLabelFromIssue_MissingLabel(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeRemoveLabelFromIssue(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"number": float64(5),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "label_name")
}

// ─── Milestone Tests ─────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_ListMilestones(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/test-org/test-repo/milestones")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{ //nolint:errcheck
			map[string]any{"title": "v1.0", "number": float64(1)},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_milestones",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result := output.Data.(map[string]any)["result"]
	milestones := result.([]any)
	assert.Len(t, milestones, 1)
}

func TestGitHubProvider_Execute_CreateMilestone(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/milestones", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "v1.0", body["title"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"title": "v1.0", "number": float64(1)}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "create_milestone",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
		"title":     "v1.0",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteCreateMilestone_MissingTitle(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCreateMilestone(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "title")
}

func TestExecuteUpdateMilestone_MissingNumber(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeUpdateMilestone(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"title": "v2.0",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "milestone_number")
}

func TestExecuteDeleteMilestone_MissingNumber(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeDeleteMilestone(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "milestone_number")
}

// ─── Update Label Tests ──────────────────────────────────────────────────────

func TestGitHubProvider_Execute_UpdateLabel(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/labels/old-name", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "new-name", body["new_name"])
		assert.Equal(t, "ff0000", body["color"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"name": "new-name", "color": "ff0000"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":      "update_label",
		"owner":          "test-org",
		"repo":           "test-repo",
		"api_base":       baseURL,
		"label_name":     "old-name",
		"new_label_name": "new-name",
		"color":          "ff0000",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteUpdateLabel_MissingName(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeUpdateLabel(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "label_name")
}

// ─── Remove Label from Issue Tests ───────────────────────────────────────────

func TestGitHubProvider_Execute_RemoveLabelFromIssue(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/issues/5/labels/bug", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":  "remove_label_from_issue",
		"owner":      "test-org",
		"repo":       "test-repo",
		"api_base":   baseURL,
		"number":     float64(5),
		"label_name": "bug",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Update Milestone Tests ──────────────────────────────────────────────────

func TestGitHubProvider_Execute_UpdateMilestone(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/milestones/1", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "v2.0", body["title"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"number": float64(1), "title": "v2.0"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":        "update_milestone",
		"owner":            "test-org",
		"repo":             "test-repo",
		"api_base":         baseURL,
		"milestone_number": float64(1),
		"title":            "v2.0",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Delete Milestone Tests ──────────────────────────────────────────────────

func TestGitHubProvider_Execute_DeleteMilestone(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/milestones/1", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":        "delete_milestone",
		"owner":            "test-org",
		"repo":             "test-repo",
		"api_base":         baseURL,
		"milestone_number": float64(1),
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkLabelMilestoneValidation(b *testing.B) {
	p := NewGitHubProvider()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.executeCreateLabel(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
		_, _ = p.executeCreateMilestone(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
	}
}
