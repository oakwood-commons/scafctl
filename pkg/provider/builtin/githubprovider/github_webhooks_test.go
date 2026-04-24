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

// ─── Webhook Tests ───────────────────────────────────────────────────────────

func TestGitHubProvider_Execute_ListWebhooks(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/test-org/test-repo/hooks")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{ //nolint:errcheck
			map[string]any{"id": float64(1), "name": "web"},
		})
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_webhooks",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"]
	hooks := result.([]any)
	assert.Len(t, hooks, 1)
}

func TestGitHubProvider_Execute_CreateWebhook(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/hooks", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		assert.Equal(t, "web", body["name"])
		config := body["config"].(map[string]any)
		assert.Equal(t, "https://example.com/webhook", config["url"])
		assert.Equal(t, "json", config["content_type"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": float64(1), "name": "web"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":      "create_webhook",
		"owner":          "test-org",
		"repo":           "test-repo",
		"api_base":       baseURL,
		"webhook_url":    "https://example.com/webhook",
		"webhook_events": []any{"push", "pull_request"},
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestExecuteCreateWebhook_MissingURL(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCreateWebhook(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook_url")
}

func TestExecuteUpdateWebhook_MissingHookID(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeUpdateWebhook(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hook_id")
}

func TestExecuteDeleteWebhook_MissingHookID(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeDeleteWebhook(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hook_id")
}

func TestGitHubProvider_Execute_UpdateWebhook(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/hooks/10", r.URL.Path)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		config := body["config"].(map[string]any)
		assert.Equal(t, "https://new.example.com/hook", config["url"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": float64(10), "name": "web"}) //nolint:errcheck
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation":      "update_webhook",
		"owner":          "test-org",
		"repo":           "test-repo",
		"api_base":       baseURL,
		"hook_id":        float64(10),
		"webhook_url":    "https://new.example.com/hook",
		"webhook_events": []any{"push"},
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func TestGitHubProvider_Execute_DeleteWebhook(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/repos/test-org/test-repo/hooks/10", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "delete_webhook",
		"owner":     "test-org",
		"repo":      "test-repo",
		"api_base":  baseURL,
		"hook_id":   float64(10),
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
}

func BenchmarkWebhookValidation(b *testing.B) {
	p := NewGitHubProvider()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.executeCreateWebhook(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
		_, _ = p.executeDeleteWebhook(context.Background(), nil, "https://api.github.com", "o", "r", map[string]any{})
	}
}
