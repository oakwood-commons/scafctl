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

// ─── List Check Runs Tests ───────────────────────────────────────────────────

func TestGitHubProvider_Execute_ListCheckRuns(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/commits/abc123/check-runs", r.URL.Path)
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"total_count": float64(2),
			"check_runs": []any{
				map[string]any{
					"id":           float64(1001),
					"name":         "build",
					"status":       "completed",
					"conclusion":   "success",
					"started_at":   "2025-01-01T00:00:00Z",
					"completed_at": "2025-01-01T00:05:00Z",
					"html_url":     "https://github.com/test-org/test-repo/runs/1001",
					"output": map[string]any{
						"title":   "Build succeeded",
						"summary": "All checks passed",
					},
				},
				map[string]any{
					"id":           float64(1002),
					"name":         "lint",
					"status":       "completed",
					"conclusion":   "failure",
					"started_at":   "2025-01-01T00:00:00Z",
					"completed_at": "2025-01-01T00:03:00Z",
					"html_url":     "https://github.com/test-org/test-repo/runs/1002",
					"output": map[string]any{
						"title":   "Lint failed",
						"summary": "2 errors found",
					},
				},
			},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_check_runs",
		"owner":     "test-org",
		"repo":      "test-repo",
		"ref":       "abc123",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, float64(2), result["total_count"])
	runs := result["check_runs"].([]any)
	assert.Len(t, runs, 2)

	run1 := runs[0].(map[string]any)
	assert.Equal(t, "build", run1["name"])
	assert.Equal(t, "success", run1["conclusion"])
	outputData := run1["output"].(map[string]any)
	assert.Equal(t, "Build succeeded", outputData["title"])

	run2 := runs[1].(map[string]any)
	assert.Equal(t, "lint", run2["name"])
	assert.Equal(t, "failure", run2["conclusion"])
}

func TestGitHubProvider_Execute_ListCheckRuns_Empty(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"total_count": float64(0),
			"check_runs":  []any{},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_check_runs",
		"owner":     "test-org",
		"repo":      "test-repo",
		"ref":       "main",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, float64(0), result["total_count"])
	runs := result["check_runs"].([]any)
	assert.Empty(t, runs)
}

func TestExecuteListCheckRuns_MissingRef(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeListCheckRuns(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ref")
}

// ─── Get Workflow Run Tests ──────────────────────────────────────────────────

func TestGitHubProvider_Execute_GetWorkflowRun(t *testing.T) {
	t.Parallel()

	callCount := 0
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		callCount++

		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			// First call: workflow run
			assert.Equal(t, "/repos/test-org/test-repo/actions/runs/9999", r.URL.Path)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"id":            float64(9999),
				"name":          "CI",
				"status":        "completed",
				"conclusion":    "failure",
				"html_url":      "https://github.com/test-org/test-repo/actions/runs/9999",
				"run_number":    float64(42),
				"event":         "push",
				"created_at":    "2025-01-01T00:00:00Z",
				"updated_at":    "2025-01-01T00:10:00Z",
				"head_branch":   "main",
				"head_sha":      "abc123",
				"display_title": "CI",
			})
		} else {
			// Second call: jobs
			assert.Equal(t, "/repos/test-org/test-repo/actions/runs/9999/jobs", r.URL.Path)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"total_count": float64(2),
				"jobs": []any{
					map[string]any{
						"id":           float64(101),
						"name":         "build",
						"status":       "completed",
						"conclusion":   "success",
						"started_at":   "2025-01-01T00:01:00Z",
						"completed_at": "2025-01-01T00:05:00Z",
						"html_url":     "https://github.com/test-org/test-repo/runs/101",
					},
					map[string]any{
						"id":           float64(102),
						"name":         "test",
						"status":       "completed",
						"conclusion":   "failure",
						"started_at":   "2025-01-01T00:01:00Z",
						"completed_at": "2025-01-01T00:08:00Z",
						"html_url":     "https://github.com/test-org/test-repo/runs/102",
						"steps": []any{
							map[string]any{
								"name":       "Run tests",
								"status":     "completed",
								"conclusion": "failure",
								"number":     float64(3),
							},
						},
					},
				},
			})
		}
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "get_workflow_run",
		"owner":     "test-org",
		"repo":      "test-repo",
		"run_id":    float64(9999),
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, float64(9999), result["id"])
	assert.Equal(t, "failure", result["conclusion"])
	assert.Equal(t, "main", result["head_branch"])

	jobs := result["jobs"].([]any)
	assert.Len(t, jobs, 2)

	// Success job should not have steps
	job1 := jobs[0].(map[string]any)
	assert.Equal(t, "build", job1["name"])
	assert.Nil(t, job1["steps"])

	// Failed job should have steps
	job2 := jobs[1].(map[string]any)
	assert.Equal(t, "test", job2["name"])
	assert.NotNil(t, job2["steps"])
}

func TestExecuteGetWorkflowRun_MissingRunID(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeGetWorkflowRun(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "run_id")
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkExecuteListCheckRuns(b *testing.B) {
	p, baseURL := testProvider(b, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"total_count": float64(3),
			"check_runs": []any{
				map[string]any{"id": float64(1), "name": "build", "status": "completed", "conclusion": "success"},
				map[string]any{"id": float64(2), "name": "test", "status": "completed", "conclusion": "success"},
				map[string]any{"id": float64(3), "name": "lint", "status": "completed", "conclusion": "success"},
			},
		})
	})

	inputs := map[string]any{
		"operation": "list_check_runs",
		"owner":     "org",
		"repo":      "repo",
		"ref":       "main",
		"api_base":  baseURL,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(context.Background(), inputs)
	}
}
