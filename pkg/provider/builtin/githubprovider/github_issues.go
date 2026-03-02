// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── Create Issue ────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreateIssue(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	title := getStringInput(inputs, "title")
	if title == "" {
		return nil, fmt.Errorf("'title' is required for create_issue operation")
	}

	// First, resolve the repository node ID
	repoID, err := p.resolveRepoID(ctx, client, apiBase, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("resolving repository ID: %w", err)
	}

	// Build mutation input
	mutInput := map[string]any{
		"repositoryId": repoID,
		"title":        title,
	}
	if body := getStringInput(inputs, "body"); body != "" {
		mutInput["body"] = body
	}

	// Resolve label IDs if provided
	labelNames := getStringSliceInput(inputs, "labels")
	if len(labelNames) > 0 {
		labelIDs, err := p.resolveLabelIDs(ctx, client, apiBase, owner, repo, labelNames)
		if err != nil {
			return nil, fmt.Errorf("resolving label IDs: %w", err)
		}
		if len(labelIDs) > 0 {
			mutInput["labelIds"] = labelIDs
		}
	}

	// Resolve assignee IDs if provided
	assigneeLogins := getStringSliceInput(inputs, "assignees")
	if len(assigneeLogins) > 0 {
		assigneeIDs, err := p.resolveUserIDs(ctx, client, apiBase, assigneeLogins)
		if err != nil {
			return nil, fmt.Errorf("resolving assignee IDs: %w", err)
		}
		if len(assigneeIDs) > 0 {
			mutInput["assigneeIds"] = assigneeIDs
		}
	}

	mutation := `mutation($input: CreateIssueInput!) {
  createIssue(input: $input) {
    issue {
      id
      number
      title
      url
      state
      createdAt
      author { login }
    }
  }
}`

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": mutInput})
	if err != nil {
		return nil, err
	}

	issue, err := extractNodeMap(data, "createIssue.issue")
	if err != nil {
		return nil, err
	}

	return actionOutput("create_issue", issue), nil
}

// ─── Update Issue ────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeUpdateIssue(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, fmt.Errorf("'number' is required for update_issue operation")
	}

	// Resolve issue node ID
	issueID, err := p.resolveIssueID(ctx, client, apiBase, owner, repo, num)
	if err != nil {
		return nil, fmt.Errorf("resolving issue ID: %w", err)
	}

	mutInput := map[string]any{
		"id": issueID,
	}
	if title := getStringInput(inputs, "title"); title != "" {
		mutInput["title"] = title
	}
	if body := getStringInput(inputs, "body"); body != "" {
		mutInput["body"] = body
	}
	if state := getStringInput(inputs, "state"); state != "" {
		mutInput["state"] = mapIssueStateForMutation(state)
	}

	// Resolve label IDs if provided
	labelNames := getStringSliceInput(inputs, "labels")
	if len(labelNames) > 0 {
		labelIDs, err := p.resolveLabelIDs(ctx, client, apiBase, owner, repo, labelNames)
		if err != nil {
			return nil, fmt.Errorf("resolving label IDs: %w", err)
		}
		if len(labelIDs) > 0 {
			mutInput["labelIds"] = labelIDs
		}
	}

	// Resolve assignee IDs if provided
	assigneeLogins := getStringSliceInput(inputs, "assignees")
	if len(assigneeLogins) > 0 {
		assigneeIDs, err := p.resolveUserIDs(ctx, client, apiBase, assigneeLogins)
		if err != nil {
			return nil, fmt.Errorf("resolving assignee IDs: %w", err)
		}
		if len(assigneeIDs) > 0 {
			mutInput["assigneeIds"] = assigneeIDs
		}
	}

	mutation := `mutation($input: UpdateIssueInput!) {
  updateIssue(input: $input) {
    issue {
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

	issue, err := extractNodeMap(data, "updateIssue.issue")
	if err != nil {
		return nil, err
	}

	return actionOutput("update_issue", issue), nil
}

// ─── Create Issue Comment ────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreateIssueComment(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, fmt.Errorf("'number' is required for create_issue_comment operation")
	}
	body := getStringInput(inputs, "body")
	if body == "" {
		return nil, fmt.Errorf("'body' is required for create_issue_comment operation")
	}

	// Resolve issue node ID
	issueID, err := p.resolveIssueID(ctx, client, apiBase, owner, repo, num)
	if err != nil {
		return nil, fmt.Errorf("resolving issue ID: %w", err)
	}

	mutation := `mutation($input: AddCommentInput!) {
  addComment(input: $input) {
    commentEdge {
      node {
        id
        body
        createdAt
        author { login }
        url
      }
    }
  }
}`

	mutInput := map[string]any{
		"subjectId": issueID,
		"body":      body,
	}

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": mutInput})
	if err != nil {
		return nil, err
	}

	comment, err := extractNodeMap(data, "addComment.commentEdge.node")
	if err != nil {
		return nil, err
	}

	return actionOutput("create_issue_comment", comment), nil
}

// ─── Node ID Resolution Helpers ──────────────────────────────────────────────

// resolveRepoID fetches the GraphQL node ID for a repository.
func (p *GitHubProvider) resolveRepoID(ctx context.Context, client *httpc.Client, apiBase, owner, repo string) (string, error) {
	query := `query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) { id }
}`
	data, err := graphqlDo(ctx, client, apiBase, query, map[string]any{"owner": owner, "name": repo})
	if err != nil {
		return "", err
	}
	repoNode, err := extractNodeMap(data, "repository")
	if err != nil {
		return "", err
	}
	id, ok := repoNode["id"].(string)
	if !ok {
		return "", fmt.Errorf("repository ID not found")
	}
	return id, nil
}

// resolveIssueID fetches the GraphQL node ID for an issue by number.
func (p *GitHubProvider) resolveIssueID(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, number int) (string, error) {
	query := `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) { id }
  }
}`
	data, err := graphqlDo(ctx, client, apiBase, query, map[string]any{"owner": owner, "name": repo, "number": number})
	if err != nil {
		return "", err
	}
	issue, err := extractNodeMap(data, "repository.issue")
	if err != nil {
		return "", err
	}
	id, ok := issue["id"].(string)
	if !ok {
		return "", fmt.Errorf("issue #%d ID not found", number)
	}
	return id, nil
}

// resolvePullRequestID fetches the GraphQL node ID for a pull request by number.
func (p *GitHubProvider) resolvePullRequestID(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, number int) (string, error) {
	query := `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) { id }
  }
}`
	data, err := graphqlDo(ctx, client, apiBase, query, map[string]any{"owner": owner, "name": repo, "number": number})
	if err != nil {
		return "", err
	}
	pr, err := extractNodeMap(data, "repository.pullRequest")
	if err != nil {
		return "", err
	}
	id, ok := pr["id"].(string)
	if !ok {
		return "", fmt.Errorf("pull request #%d ID not found", number)
	}
	return id, nil
}

// resolveLabelIDs fetches GraphQL node IDs for labels by name.
func (p *GitHubProvider) resolveLabelIDs(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, names []string) ([]string, error) {
	// Fetch all labels for the repo and filter by name
	query := `query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    labels(first: 100) {
      nodes { id name }
    }
  }
}`
	data, err := graphqlDo(ctx, client, apiBase, query, map[string]any{"owner": owner, "name": repo})
	if err != nil {
		return nil, err
	}
	nodes, err := extractNodes(data, "repository.labels")
	if err != nil {
		return nil, err
	}

	// Build a name→ID map
	nameToID := make(map[string]string)
	for _, node := range nodes {
		if m, ok := node.(map[string]any); ok {
			if n, ok := m["name"].(string); ok {
				if id, ok := m["id"].(string); ok {
					nameToID[n] = id
				}
			}
		}
	}

	var ids []string
	for _, name := range names {
		if id, ok := nameToID[name]; ok {
			ids = append(ids, id)
		}
		// Silently skip labels that don't exist
	}
	return ids, nil
}

// resolveUserIDs fetches GraphQL node IDs for users by login.
func (p *GitHubProvider) resolveUserIDs(ctx context.Context, client *httpc.Client, apiBase string, logins []string) ([]string, error) {
	ids := make([]string, 0, len(logins))
	for _, login := range logins {
		query := `query($login: String!) {
  user(login: $login) { id }
}`
		data, err := graphqlDo(ctx, client, apiBase, query, map[string]any{"login": login})
		if err != nil {
			return nil, fmt.Errorf("resolving user %q: %w", login, err)
		}
		user, err := extractNodeMap(data, "user")
		if err != nil {
			return nil, fmt.Errorf("resolving user %q: %w", login, err)
		}
		if id, ok := user["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// resolveRefID fetches the GraphQL node ID for a ref (branch or tag).
func (p *GitHubProvider) resolveRefID(ctx context.Context, client *httpc.Client, apiBase, owner, repo, qualifiedName string) (string, error) {
	query := `query($owner: String!, $name: String!, $qualifiedName: String!) {
  repository(owner: $owner, name: $name) {
    ref(qualifiedName: $qualifiedName) { id }
  }
}`
	data, err := graphqlDo(ctx, client, apiBase, query, map[string]any{"owner": owner, "name": repo, "qualifiedName": qualifiedName})
	if err != nil {
		return "", err
	}
	ref, err := extractNode(data, "repository.ref")
	if err != nil {
		return "", err
	}
	if ref == nil {
		return "", fmt.Errorf("ref %q not found", qualifiedName)
	}
	refMap, ok := ref.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected ref format")
	}
	id, ok := refMap["id"].(string)
	if !ok {
		return "", fmt.Errorf("ref %q ID not found", qualifiedName)
	}
	return id, nil
}

// mapIssueStateForMutation converts user state to GraphQL IssueState enum for mutations.
func mapIssueStateForMutation(state string) string {
	switch state {
	case "open", "OPEN":
		return "OPEN"
	case "closed", "CLOSED":
		return "CLOSED"
	default:
		return state
	}
}
