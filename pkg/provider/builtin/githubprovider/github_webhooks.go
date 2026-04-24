// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── List Webhooks ───────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListWebhooks(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)
	restURL := fmt.Sprintf("%s/repos/%s/%s/hooks?per_page=%d", apiBase, owner, repo, perPage)
	result, err := p.doRESTRequest(ctx, client, http.MethodGet, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing webhooks: %w", err)
	}
	return readOutput(result), nil
}

// ─── Create Webhook ──────────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreateWebhook(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	webhookURL := getStringInput(inputs, "webhook_url")
	if webhookURL == "" {
		return nil, requiredInputError("create_webhook", "webhook_url", inputs, "")
	}

	events := getStringSliceInput(inputs, "webhook_events")
	if len(events) == 0 {
		events = []string{"push"}
	}

	contentType := getStringInput(inputs, "webhook_content_type")
	if contentType == "" {
		contentType = "json"
	}

	config := map[string]any{
		"url":          webhookURL,
		"content_type": contentType,
	}
	if secret := getStringInput(inputs, "webhook_secret"); secret != "" {
		config["secret"] = secret
	}

	active := true
	if v, ok := getBoolInput(inputs, "webhook_active"); ok {
		active = v
	}

	reqBody := map[string]any{
		"name":   "web",
		"active": active,
		"events": events,
		"config": config,
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/hooks", apiBase, owner, repo)
	result, err := p.doRESTRequest(ctx, client, http.MethodPost, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating webhook: %w", err)
	}
	return actionOutput("create_webhook", result), nil
}

// ─── Update Webhook ──────────────────────────────────────────────────────────

func (p *GitHubProvider) executeUpdateWebhook(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	hookID, ok := getIntInput(inputs, "hook_id")
	if !ok || hookID == 0 {
		return nil, requiredInputError("update_webhook", "hook_id", inputs, "")
	}

	reqBody := map[string]any{}

	config := map[string]any{}
	if webhookURL := getStringInput(inputs, "webhook_url"); webhookURL != "" {
		config["url"] = webhookURL
	}
	if contentType := getStringInput(inputs, "webhook_content_type"); contentType != "" {
		config["content_type"] = contentType
	}
	if secret := getStringInput(inputs, "webhook_secret"); secret != "" {
		config["secret"] = secret
	}
	if len(config) > 0 {
		reqBody["config"] = config
	}

	if events := getStringSliceInput(inputs, "webhook_events"); len(events) > 0 {
		reqBody["events"] = events
	}
	if active, ok := getBoolInput(inputs, "webhook_active"); ok {
		reqBody["active"] = active
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/hooks/%d", apiBase, owner, repo, hookID)
	result, err := p.doRESTRequest(ctx, client, http.MethodPatch, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("updating webhook: %w", err)
	}
	return actionOutput("update_webhook", result), nil
}

// ─── Delete Webhook ──────────────────────────────────────────────────────────

func (p *GitHubProvider) executeDeleteWebhook(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	hookID, ok := getIntInput(inputs, "hook_id")
	if !ok || hookID == 0 {
		return nil, requiredInputError("delete_webhook", "hook_id", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/hooks/%d", apiBase, owner, repo, hookID)
	result, err := p.doRESTRequest(ctx, client, http.MethodDelete, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("deleting webhook: %w", err)
	}
	return actionOutput("delete_webhook", result), nil
}
