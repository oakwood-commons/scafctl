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

// ─── List Collaborators ──────────────────────────────────────────────────────

func (p *GitHubProvider) executeListCollaborators(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)
	restURL := fmt.Sprintf("%s/repos/%s/%s/collaborators?per_page=%d", apiBase, owner, repo, perPage)
	result, err := p.doRESTRequest(ctx, client, http.MethodGet, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing collaborators: %w", err)
	}
	return readOutput(result), nil
}

// ─── Add Collaborator ────────────────────────────────────────────────────────

func (p *GitHubProvider) executeAddCollaborator(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	username := getStringInput(inputs, "username")
	if username == "" {
		return nil, requiredInputError("add_collaborator", "username", inputs, "")
	}

	reqBody := map[string]any{}
	if permission := getStringInput(inputs, "permission"); permission != "" {
		reqBody["permission"] = permission
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/collaborators/%s", apiBase, owner, repo, url.PathEscape(username))
	result, err := p.doRESTRequest(ctx, client, http.MethodPut, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("adding collaborator: %w", err)
	}
	return actionOutput("add_collaborator", result), nil
}

// ─── Remove Collaborator ─────────────────────────────────────────────────────

func (p *GitHubProvider) executeRemoveCollaborator(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	username := getStringInput(inputs, "username")
	if username == "" {
		return nil, requiredInputError("remove_collaborator", "username", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/collaborators/%s", apiBase, owner, repo, url.PathEscape(username))
	result, err := p.doRESTRequest(ctx, client, http.MethodDelete, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("removing collaborator: %w", err)
	}
	return actionOutput("remove_collaborator", result), nil
}
