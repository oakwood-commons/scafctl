// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── Create Pull Request ─────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreatePullRequest(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	title := getStringInput(inputs, "title")
	if title == "" {
		return nil, requiredInputError("create_pull_request", "title", inputs, "")
	}
	head := getStringInput(inputs, "head")
	if head == "" {
		return nil, requiredInputError("create_pull_request", "head", inputs, "")
	}
	base := getStringInput(inputs, "base")
	if base == "" {
		return nil, requiredInputError("create_pull_request", "base", inputs, "")
	}

	repoID, err := p.resolveRepoID(ctx, client, apiBase, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("resolving repository ID: %w", err)
	}

	mutInput := map[string]any{
		"repositoryId": repoID,
		"title":        title,
		"headRefName":  head,
		"baseRefName":  base,
	}
	if body := getStringInput(inputs, "body"); body != "" {
		mutInput["body"] = body
	}
	if draft, ok := getBoolInput(inputs, "draft"); ok {
		mutInput["draft"] = draft
	}

	mutation := `mutation($input: CreatePullRequestInput!) {
  createPullRequest(input: $input) {
    pullRequest {
      id
      number
      title
      url
      state
      headRefName
      baseRefName
      isDraft
      createdAt
      author { login }
    }
  }
}`

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": mutInput})
	if err != nil {
		return nil, err
	}

	pr, err := extractNodeMap(data, "createPullRequest.pullRequest")
	if err != nil {
		return nil, err
	}

	return actionOutput("create_pull_request", pr), nil
}

// ─── Update Pull Request ─────────────────────────────────────────────────────

func (p *GitHubProvider) executeUpdatePullRequest(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, requiredInputError("update_pull_request", "number", inputs, "")
	}

	prID, err := p.resolvePullRequestID(ctx, client, apiBase, owner, repo, num)
	if err != nil {
		return nil, fmt.Errorf("resolving pull request ID: %w", err)
	}

	mutInput := map[string]any{
		"pullRequestId": prID,
	}
	if title := getStringInput(inputs, "title"); title != "" {
		mutInput["title"] = title
	}
	if body := getStringInput(inputs, "body"); body != "" {
		mutInput["body"] = body
	}
	if base := getStringInput(inputs, "base"); base != "" {
		mutInput["baseRefName"] = base
	}

	mutation := `mutation($input: UpdatePullRequestInput!) {
  updatePullRequest(input: $input) {
    pullRequest {
      id
      number
      title
      url
      state
      updatedAt
    }
  }
}`

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": mutInput})
	if err != nil {
		return nil, err
	}

	pr, err := extractNodeMap(data, "updatePullRequest.pullRequest")
	if err != nil {
		return nil, err
	}

	return actionOutput("update_pull_request", pr), nil
}

// ─── Merge Pull Request ──────────────────────────────────────────────────────

func (p *GitHubProvider) executeMergePullRequest(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, requiredInputError("merge_pull_request", "number", inputs, "")
	}

	prID, err := p.resolvePullRequestID(ctx, client, apiBase, owner, repo, num)
	if err != nil {
		return nil, fmt.Errorf("resolving pull request ID: %w", err)
	}

	mutInput := map[string]any{
		"pullRequestId": prID,
	}

	mergeMethod := getStringInput(inputs, "merge_method")
	if mergeMethod == "" {
		mergeMethod = "MERGE"
	}
	mutInput["mergeMethod"] = mergeMethod

	if commitTitle := getStringInput(inputs, "commit_title"); commitTitle != "" {
		mutInput["commitHeadline"] = commitTitle
	}
	if commitMessage := getStringInput(inputs, "commit_message"); commitMessage != "" {
		mutInput["commitBody"] = commitMessage
	}

	mutation := `mutation($input: MergePullRequestInput!) {
  mergePullRequest(input: $input) {
    pullRequest {
      id
      number
      title
      url
      state
      merged
      mergedAt
      mergeCommit { oid }
    }
  }
}`

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": mutInput})
	if err != nil {
		return nil, err
	}

	pr, err := extractNodeMap(data, "mergePullRequest.pullRequest")
	if err != nil {
		return nil, err
	}

	return actionOutput("merge_pull_request", pr), nil
}

// ─── Close Pull Request ──────────────────────────────────────────────────────

func (p *GitHubProvider) executeClosePullRequest(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, requiredInputError("close_pull_request", "number", inputs, "")
	}

	prID, err := p.resolvePullRequestID(ctx, client, apiBase, owner, repo, num)
	if err != nil {
		return nil, fmt.Errorf("resolving pull request ID: %w", err)
	}

	mutation := `mutation($input: ClosePullRequestInput!) {
  closePullRequest(input: $input) {
    pullRequest {
      id
      number
      title
      url
      state
      closedAt
    }
  }
}`

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": map[string]any{"pullRequestId": prID}})
	if err != nil {
		return nil, err
	}

	pr, err := extractNodeMap(data, "closePullRequest.pullRequest")
	if err != nil {
		return nil, err
	}

	return actionOutput("close_pull_request", pr), nil
}
