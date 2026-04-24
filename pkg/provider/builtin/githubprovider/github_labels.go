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

// ─── List Labels ─────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListLabels(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)
	restURL := fmt.Sprintf("%s/repos/%s/%s/labels?per_page=%d", apiBase, owner, repo, perPage)
	result, err := p.doRESTRequest(ctx, client, http.MethodGet, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing labels: %w", err)
	}
	return readOutput(result), nil
}

// ─── Create Label ────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreateLabel(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	name := getStringInput(inputs, "label_name")
	if name == "" {
		return nil, requiredInputError("create_label", "label_name", inputs, "")
	}

	reqBody := map[string]any{
		"name": name,
	}
	if color := getStringInput(inputs, "color"); color != "" {
		reqBody["color"] = color
	}
	if desc := getStringInput(inputs, "label_description"); desc != "" {
		reqBody["description"] = desc
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/labels", apiBase, owner, repo)
	result, err := p.doRESTRequest(ctx, client, http.MethodPost, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating label: %w", err)
	}
	return actionOutput("create_label", result), nil
}

// ─── Update Label ────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeUpdateLabel(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	name := getStringInput(inputs, "label_name")
	if name == "" {
		return nil, requiredInputError("update_label", "label_name", inputs, "")
	}

	reqBody := map[string]any{}
	if newName := getStringInput(inputs, "new_label_name"); newName != "" {
		reqBody["new_name"] = newName
	}
	if color := getStringInput(inputs, "color"); color != "" {
		reqBody["color"] = color
	}
	if desc := getStringInput(inputs, "label_description"); desc != "" {
		reqBody["description"] = desc
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/labels/%s", apiBase, owner, repo, url.PathEscape(name))
	result, err := p.doRESTRequest(ctx, client, http.MethodPatch, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("updating label: %w", err)
	}
	return actionOutput("update_label", result), nil
}

// ─── Delete Label ────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeDeleteLabel(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	name := getStringInput(inputs, "label_name")
	if name == "" {
		return nil, requiredInputError("delete_label", "label_name", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/labels/%s", apiBase, owner, repo, url.PathEscape(name))
	result, err := p.doRESTRequest(ctx, client, http.MethodDelete, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("deleting label: %w", err)
	}
	return actionOutput("delete_label", result), nil
}

// ─── Add Labels to Issue ─────────────────────────────────────────────────────

func (p *GitHubProvider) executeAddLabelsToIssue(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	number, ok := getIntInput(inputs, "number")
	if !ok || number == 0 {
		return nil, requiredInputError("add_labels_to_issue", "number", inputs, "")
	}
	labels := getStringSliceInput(inputs, "labels")
	if len(labels) == 0 {
		return nil, requiredInputError("add_labels_to_issue", "labels", inputs, "")
	}

	reqBody := map[string]any{
		"labels": labels,
	}
	restURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels", apiBase, owner, repo, number)
	result, err := p.doRESTRequest(ctx, client, http.MethodPost, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("adding labels to issue: %w", err)
	}
	return actionOutput("add_labels_to_issue", result), nil
}

// ─── Remove Label from Issue ─────────────────────────────────────────────────

func (p *GitHubProvider) executeRemoveLabelFromIssue(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	number, ok := getIntInput(inputs, "number")
	if !ok || number == 0 {
		return nil, requiredInputError("remove_label_from_issue", "number", inputs, "")
	}
	labelName := getStringInput(inputs, "label_name")
	if labelName == "" {
		return nil, requiredInputError("remove_label_from_issue", "label_name", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels/%s", apiBase, owner, repo, number, url.PathEscape(labelName))
	result, err := p.doRESTRequest(ctx, client, http.MethodDelete, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("removing label from issue: %w", err)
	}
	return actionOutput("remove_label_from_issue", result), nil
}
