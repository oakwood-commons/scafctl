// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── List Milestones ─────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListMilestones(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)
	state := getStringInput(inputs, "state")
	if state == "" {
		state = "open"
	}
	restURL := fmt.Sprintf("%s/repos/%s/%s/milestones?state=%s&per_page=%d", apiBase, owner, repo, url.QueryEscape(state), perPage)
	result, err := p.doRESTRequest(ctx, client, http.MethodGet, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing milestones: %w", err)
	}
	return readOutput(result), nil
}

// ─── Create Milestone ────────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreateMilestone(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	title := getStringInput(inputs, "title")
	if title == "" {
		return nil, requiredInputError("create_milestone", "title", inputs, "")
	}

	reqBody := map[string]any{
		"title": title,
	}
	if desc := getStringInput(inputs, "description"); desc != "" {
		reqBody["description"] = desc
	}
	if state := getStringInput(inputs, "state"); state != "" {
		reqBody["state"] = state
	}
	if dueOn := getStringInput(inputs, "due_on"); dueOn != "" {
		reqBody["due_on"] = dueOn
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/milestones", apiBase, owner, repo)
	result, err := p.doRESTRequest(ctx, client, http.MethodPost, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating milestone: %w", err)
	}
	return actionOutput("create_milestone", result), nil
}

// ─── Update Milestone ────────────────────────────────────────────────────────

func (p *GitHubProvider) executeUpdateMilestone(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	milestoneNumber, ok := getIntInput(inputs, "milestone_number")
	if !ok || milestoneNumber == 0 {
		return nil, requiredInputError("update_milestone", "milestone_number", inputs, "")
	}

	reqBody := map[string]any{}
	if title := getStringInput(inputs, "title"); title != "" {
		reqBody["title"] = title
	}
	if desc := getStringInput(inputs, "description"); desc != "" {
		reqBody["description"] = desc
	}
	if state := getStringInput(inputs, "state"); state != "" {
		reqBody["state"] = state
	}
	if dueOn := getStringInput(inputs, "due_on"); dueOn != "" {
		reqBody["due_on"] = dueOn
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/milestones/%d", apiBase, owner, repo, milestoneNumber)
	result, err := p.doRESTRequest(ctx, client, http.MethodPatch, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("updating milestone: %w", err)
	}
	return actionOutput("update_milestone", result), nil
}

// ─── Delete Milestone ────────────────────────────────────────────────────────

func (p *GitHubProvider) executeDeleteMilestone(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	milestoneNumber, ok := getIntInput(inputs, "milestone_number")
	if !ok || milestoneNumber == 0 {
		return nil, requiredInputError("delete_milestone", "milestone_number", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/milestones/%d", apiBase, owner, repo, milestoneNumber)
	result, err := p.doRESTRequest(ctx, client, http.MethodDelete, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("deleting milestone: %w", err)
	}
	return actionOutput("delete_milestone", result), nil
}
