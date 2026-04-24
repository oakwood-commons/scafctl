// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Dispatch Workflow Tests ─────────────────────────────────────────────────

func TestGitHubProvider_Execute_DispatchWorkflow(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/actions/workflows/ci.yml/dispatches", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "main", body["ref"])

		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":   "dispatch_workflow",
		"owner":       "test-org",
		"repo":        "test-repo",
		"api_base":    baseURL,
		"workflow_id": "ci.yml",
		"ref":         "main",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteDispatchWorkflow_MissingWorkflowID(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeDispatchWorkflow(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"ref": "main",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workflow_id")
}

func TestExecuteDispatchWorkflow_MissingRef(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeDispatchWorkflow(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"workflow_id": "ci.yml",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ref")
}

// ─── List Workflow Runs Tests ────────────────────────────────────────────────

func TestGitHubProvider_Execute_ListWorkflowRuns(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/test-org/test-repo/actions/runs")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"total_count":   float64(1),
			"workflow_runs": []any{map[string]any{"id": float64(1), "status": "completed"}},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_workflow_runs",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, float64(1), result["total_count"])
}

// ─── Cancel/Rerun Workflow Tests ─────────────────────────────────────────────

func TestExecuteCancelWorkflowRun_MissingRunID(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCancelWorkflowRun(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "run_id")
}

func TestExecuteRerunWorkflow_MissingRunID(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeRerunWorkflow(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "run_id")
}

// ─── Variable Tests ──────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_ListRepoVariables(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/test-org/test-repo/actions/variables")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"total_count": float64(1),
			"variables":   []any{map[string]any{"name": "MY_VAR", "value": "hello"}},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_repo_variables",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, float64(1), result["total_count"])
}

func TestExecuteCreateOrUpdateVariable_MissingName(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCreateOrUpdateVariable(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"variable_value": "val",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "variable_name")
}

func TestExecuteDeleteVariable_MissingName(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeDeleteVariable(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "variable_name")
}

// ─── Environment Tests ───────────────────────────────────────────────────────

func TestGitHubProvider_Execute_ListEnvironments(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/environments", r.URL.Path)
		assert.Equal(t, "30", r.URL.Query().Get("per_page"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"total_count":  float64(1),
			"environments": []any{map[string]any{"name": "production"}},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_environments",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	assert.Equal(t, float64(1), result["total_count"])
}

func TestExecuteCreateOrUpdateEnvironment_MissingName(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCreateOrUpdateEnvironment(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "environment_name")
}

func TestExecuteDeleteEnvironment_MissingName(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeDeleteEnvironment(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "environment_name")
}

// ─── Repo Settings Tests ─────────────────────────────────────────────────────

func TestGitHubProvider_Execute_UpdateRepo(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "new description", body["description"])
		assert.Equal(t, true, body["has_wiki"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"name": "test-repo"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":   "update_repo",
		"owner":       "test-org",
		"repo":        "test-repo",
		"api_base":    baseURL,
		"description": "new description",
		"has_wiki":    true,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_ListTopics(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/topics", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"names": []any{"go", "cli"},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_topics",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].(map[string]any)
	names := result["names"].([]any)
	assert.Len(t, names, 2)
}

func TestGitHubProvider_Execute_ForkRepo(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/upstream-org/upstream-repo/forks", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{"full_name": "my-org/upstream-repo"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":    "fork_repo",
		"owner":        "upstream-org",
		"repo":         "upstream-repo",
		"api_base":     baseURL,
		"organization": "my-org",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteCreateFromTemplate_MissingNewName(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCreateFromTemplate(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "new_repo_name")
}

// ─── Cancel Workflow Run ─────────────────────────────────────────────────────

func TestGitHubProvider_Execute_CancelWorkflowRun(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/actions/runs/123/cancel", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{}`)) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "cancel_workflow_run",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
		"run_id":    float64(123),
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Rerun Workflow ──────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_RerunWorkflow(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/actions/runs/456/rerun", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`)) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "rerun_workflow",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
		"run_id":    float64(456),
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Create or Update Variable ───────────────────────────────────────────────

func TestGitHubProvider_Execute_CreateOrUpdateVariable(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		// First call is PATCH (update attempt), respond with success
		if r.Method == http.MethodPatch {
			assert.Equal(t, "/repos/test-org/test-repo/actions/variables/MY_VAR", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected method: %s", r.Method)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":      "create_or_update_variable",
		"owner":          "test-org",
		"repo":           "test-repo",
		"api_base":       baseURL,
		"variable_name":  "MY_VAR",
		"variable_value": "my-value",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_CreateOrUpdateVariable_CreateFallback(t *testing.T) {
	t.Parallel()

	callCount := 0
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodPatch {
			// Variable doesn't exist - return 404
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`)) //nolint:errcheck
			return
		}
		if r.Method == http.MethodPost {
			assert.Equal(t, "/repos/test-org/test-repo/actions/variables", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"name":"MY_VAR","value":"my-value"}`)) //nolint:errcheck
			return
		}
		t.Errorf("unexpected method: %s", r.Method)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":      "create_or_update_variable",
		"owner":          "test-org",
		"repo":           "test-repo",
		"api_base":       baseURL,
		"variable_name":  "MY_VAR",
		"variable_value": "my-value",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	assert.Equal(t, 2, callCount, "should have made PATCH then POST")
}

func TestGitHubProvider_Execute_CreateOrUpdateVariable_NoFallbackOnNon404(t *testing.T) {
	t.Parallel()

	callCount := 0
	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Return 403 Forbidden -- should NOT trigger create fallback
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Resource not accessible by integration"}`)) //nolint:errcheck
	})

	_, err := p.Execute(context.Background(), map[string]any{
		"operation":      "create_or_update_variable",
		"owner":          "test-org",
		"repo":           "test-repo",
		"api_base":       baseURL,
		"variable_name":  "MY_VAR",
		"variable_value": "my-value",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "updating variable")
	assert.Equal(t, 1, callCount, "should have made only the PATCH call, no POST fallback")
}

// ─── Delete Variable ─────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_DeleteVariable(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/actions/variables/MY_VAR", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":     "delete_variable",
		"owner":         "test-org",
		"repo":          "test-repo",
		"api_base":      baseURL,
		"variable_name": "MY_VAR",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Create or Update Environment ────────────────────────────────────────────

func TestGitHubProvider_Execute_CreateOrUpdateEnvironment(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/environments/staging", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"name": "staging"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":        "create_or_update_environment",
		"owner":            "test-org",
		"repo":             "test-repo",
		"api_base":         baseURL,
		"environment_name": "staging",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Delete Environment ──────────────────────────────────────────────────────

func TestGitHubProvider_Execute_DeleteEnvironment(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/environments/staging", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":        "delete_environment",
		"owner":            "test-org",
		"repo":             "test-repo",
		"api_base":         baseURL,
		"environment_name": "staging",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Replace Topics ──────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_ReplaceTopics(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/topics", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		names := body["names"].([]any)
		assert.Len(t, names, 2)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"names": []string{"go", "cli"}}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "replace_topics",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
		"topics":    []any{"go", "cli"},
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_ReplaceTopics_Empty(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"names": []string{}}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "replace_topics",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Create from Template ────────────────────────────────────────────────────

func TestGitHubProvider_Execute_CreateFromTemplate(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/template-org/template-repo/generate", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "new-project", body["name"])
		assert.Equal(t, "my-org", body["owner"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"full_name": "my-org/new-project"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":     "create_from_template",
		"owner":         "template-org",
		"repo":          "template-repo",
		"api_base":      baseURL,
		"new_repo_name": "new-project",
		"new_owner":     "my-org",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Update Repo ─────────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_UpdateRepo_BoolSettings(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, false, body["has_wiki"])
		assert.Equal(t, true, body["allow_squash_merge"])
		assert.Equal(t, true, body["has_issues"])
		assert.Equal(t, false, body["has_projects"])
		assert.Equal(t, true, body["allow_merge_commit"])
		assert.Equal(t, false, body["allow_rebase_merge"])
		assert.Equal(t, true, body["delete_branch_on_merge"])
		assert.Equal(t, false, body["archived"])
		assert.Equal(t, "Updated desc", body["description"])
		assert.Equal(t, "https://example.com", body["homepage"])
		assert.Equal(t, "private", body["visibility"])
		assert.Equal(t, "develop", body["default_branch"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"full_name": "test-org/test-repo"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":              "update_repo",
		"owner":                  "test-org",
		"repo":                   "test-repo",
		"api_base":               baseURL,
		"has_wiki":               false,
		"allow_squash_merge":     true,
		"has_issues":             true,
		"has_projects":           false,
		"allow_merge_commit":     true,
		"allow_rebase_merge":     false,
		"delete_branch_on_merge": true,
		"archived":               false,
		"description":            "Updated desc",
		"homepage":               "https://example.com",
		"visibility":             "private",
		"default_branch":         "develop",
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

// ─── Custom Properties Tests ─────────────────────────────────────────────────

func TestGitHubProvider_Execute_ListCustomProperties(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/properties/values", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{ //nolint:errcheck
			{"property_name": "environment", "value": "production"},
			{"property_name": "team", "value": "platform"},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_custom_properties",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"]
	props := result.([]any)
	assert.Len(t, props, 2)
}

func TestGitHubProvider_Execute_SetCustomProperties(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/properties/values", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req) //nolint:errcheck
		props := req["properties"].([]any)
		assert.NotEmpty(t, props)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "set_custom_properties",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
		"properties": map[string]any{
			"environment": "staging",
			"team":        "platform",
		},
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_SetCustomProperties_MissingProperties(t *testing.T) {
	t.Parallel()

	p, _ := testProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "set_custom_properties",
		"owner":     "test-org",
		"repo":      "test-repo",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "properties")
}

func BenchmarkActionsValidation(b *testing.B) {
	p := NewGitHubProvider()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.executeDispatchWorkflow(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
		_, _ = p.executeDeleteVariable(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
		_, _ = p.executeDeleteEnvironment(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
	}
}
