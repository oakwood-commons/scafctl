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

// Release mutations use the REST API because GitHub's GraphQL API does not
// have createRelease, updateRelease, or deleteRelease mutations.

// ─── Create Release ──────────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreateRelease(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	tagName := getStringInput(inputs, "tag_name")
	if tagName == "" {
		return nil, fmt.Errorf("'tag_name' is required for create_release operation")
	}

	reqBody := map[string]any{
		"tag_name": tagName,
	}
	if name := getStringInput(inputs, "name"); name != "" {
		reqBody["name"] = name
	}
	if body := getStringInput(inputs, "body"); body != "" {
		reqBody["body"] = body
	}
	if target := getStringInput(inputs, "target_commitish"); target != "" {
		reqBody["target_commitish"] = target
	}
	if draft, ok := getBoolInput(inputs, "draft"); ok {
		reqBody["draft"] = draft
	}
	if prerelease, ok := getBoolInput(inputs, "prerelease"); ok {
		reqBody["prerelease"] = prerelease
	}

	url := fmt.Sprintf("%s/repos/%s/%s/releases", apiBase, owner, repo)
	result, err := p.doRESTRequest(ctx, client, http.MethodPost, url, reqBody)
	if err != nil {
		return nil, err
	}

	return actionOutput("create_release", result), nil
}

// ─── Update Release ──────────────────────────────────────────────────────────

func (p *GitHubProvider) executeUpdateRelease(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	releaseID, ok := getIntInput(inputs, "release_id")
	if !ok || releaseID == 0 {
		return nil, fmt.Errorf("'release_id' is required for update_release operation")
	}

	reqBody := map[string]any{}
	if tagName := getStringInput(inputs, "tag_name"); tagName != "" {
		reqBody["tag_name"] = tagName
	}
	if name := getStringInput(inputs, "name"); name != "" {
		reqBody["name"] = name
	}
	if body := getStringInput(inputs, "body"); body != "" {
		reqBody["body"] = body
	}
	if target := getStringInput(inputs, "target_commitish"); target != "" {
		reqBody["target_commitish"] = target
	}
	if draft, ok := getBoolInput(inputs, "draft"); ok {
		reqBody["draft"] = draft
	}
	if prerelease, ok := getBoolInput(inputs, "prerelease"); ok {
		reqBody["prerelease"] = prerelease
	}

	url := fmt.Sprintf("%s/repos/%s/%s/releases/%d", apiBase, owner, repo, releaseID)
	result, err := p.doRESTRequest(ctx, client, http.MethodPatch, url, reqBody)
	if err != nil {
		return nil, err
	}

	return actionOutput("update_release", result), nil
}

// ─── Delete Release ──────────────────────────────────────────────────────────

func (p *GitHubProvider) executeDeleteRelease(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	releaseID, ok := getIntInput(inputs, "release_id")
	if !ok || releaseID == 0 {
		return nil, fmt.Errorf("'release_id' is required for delete_release operation")
	}

	url := fmt.Sprintf("%s/repos/%s/%s/releases/%d", apiBase, owner, repo, releaseID)
	_, err := p.doRESTRequest(ctx, client, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	return actionOutput("delete_release", map[string]any{
		"release_id": releaseID,
		"deleted":    true,
	}), nil
}
