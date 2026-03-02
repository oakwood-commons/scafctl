// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── Create Commit (createCommitOnBranch) ────────────────────────────────────

// executeCreateCommit creates a signed commit on a branch using the GitHub GraphQL
// createCommitOnBranch mutation. This produces GPG-signed commits automatically
// (signed by GitHub on behalf of the authenticated user). Supports multi-file
// atomic commits with additions and deletions.
func (p *GitHubProvider) executeCreateCommit(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	branch := getStringInput(inputs, "branch")
	if branch == "" {
		return nil, fmt.Errorf("'branch' is required for create_commit operation")
	}
	message := getStringInput(inputs, "message")
	if message == "" {
		return nil, fmt.Errorf("'message' is required for create_commit operation")
	}
	expectedHeadOID := getStringInput(inputs, "expected_head_oid")
	if expectedHeadOID == "" {
		return nil, fmt.Errorf("'expected_head_oid' is required for create_commit operation (use get_head_oid to fetch it)")
	}

	// Build fileChanges
	fileChanges := map[string]any{}

	// Process additions
	if additionsRaw, ok := inputs["additions"].([]any); ok && len(additionsRaw) > 0 {
		additions := make([]map[string]any, 0, len(additionsRaw))
		for _, item := range additionsRaw {
			if m, ok := item.(map[string]any); ok {
				path, _ := m["path"].(string)
				content, _ := m["content"].(string)
				if path == "" || content == "" {
					return nil, fmt.Errorf("each addition must have 'path' and 'content'")
				}
				// The mutation requires base64-encoded content
				encoded := base64.StdEncoding.EncodeToString([]byte(content))
				additions = append(additions, map[string]any{
					"path":     path,
					"contents": encoded,
				})
			}
		}
		if len(additions) > 0 {
			fileChanges["additions"] = additions
		}
	}

	// Process deletions
	if deletionsRaw, ok := inputs["deletions"].([]any); ok && len(deletionsRaw) > 0 {
		deletions := make([]map[string]any, 0, len(deletionsRaw))
		for _, item := range deletionsRaw {
			if m, ok := item.(map[string]any); ok {
				path, _ := m["path"].(string)
				if path == "" {
					return nil, fmt.Errorf("each deletion must have 'path'")
				}
				deletions = append(deletions, map[string]any{
					"path": path,
				})
			}
		}
		if len(deletions) > 0 {
			fileChanges["deletions"] = deletions
		}
	}

	if len(fileChanges) == 0 {
		return nil, fmt.Errorf("create_commit requires at least one 'additions' or 'deletions' entry")
	}

	// Build the mutation input
	messageInput := map[string]any{
		"headline": message,
	}
	if messageBody := getStringInput(inputs, "message_body"); messageBody != "" {
		messageInput["body"] = messageBody
	}

	mutInput := map[string]any{
		"branch": map[string]any{
			"repositoryNameWithOwner": owner + "/" + repo,
			"branchName":              branch,
		},
		"expectedHeadOid": expectedHeadOID,
		"message":         messageInput,
		"fileChanges":     fileChanges,
	}

	mutation := `mutation($input: CreateCommitOnBranchInput!) {
  createCommitOnBranch(input: $input) {
    commit {
      oid
      url
      committedDate
      message
      additions(first: 0) { totalCount }
      deletions(first: 0) { totalCount }
      signature {
        isValid
        signer { login }
      }
    }
  }
}`

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": mutInput})
	if err != nil {
		return nil, err
	}

	commit, err := extractNodeMap(data, "createCommitOnBranch.commit")
	if err != nil {
		return nil, err
	}

	return actionOutput("create_commit", commit), nil
}

// ─── createRef (shared helper) ───────────────────────────────────────────────

// executeCreateRef is shared by executeCreateBranch and executeCreateTag.
// refName must be the fully-qualified ref (e.g. "refs/heads/main" or "refs/tags/v1.0.0").
func (p *GitHubProvider) executeCreateRef(ctx context.Context, client *httpc.Client, apiBase, owner, repo, operation, refName, oid string) (*provider.Output, error) {
	repoID, err := p.resolveRepoID(ctx, client, apiBase, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("resolving repository ID: %w", err)
	}

	mutation := `mutation($input: CreateRefInput!) {
  createRef(input: $input) {
    ref {
      name
      target { ... on Commit { oid } }
    }
  }
}`

	mutInput := map[string]any{
		"repositoryId": repoID,
		"name":         refName,
		"oid":          oid,
	}

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": mutInput})
	if err != nil {
		return nil, err
	}

	ref, err := extractNodeMap(data, "createRef.ref")
	if err != nil {
		return nil, err
	}

	return actionOutput(operation, ref), nil
}

// ─── Create Branch ───────────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreateBranch(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	branch := getStringInput(inputs, "branch")
	if branch == "" {
		return nil, fmt.Errorf("'branch' is required for create_branch operation")
	}
	oid := getStringInput(inputs, "oid")
	if oid == "" {
		return nil, fmt.Errorf("'oid' is required for create_branch operation (commit SHA to point the branch at)")
	}
	return p.executeCreateRef(ctx, client, apiBase, owner, repo, "create_branch", "refs/heads/"+branch, oid)
}

// ─── Delete Branch ───────────────────────────────────────────────────────────

func (p *GitHubProvider) executeDeleteBranch(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	branch := getStringInput(inputs, "branch")
	if branch == "" {
		return nil, fmt.Errorf("'branch' is required for delete_branch operation")
	}

	refID, err := p.resolveRefID(ctx, client, apiBase, owner, repo, "refs/heads/"+branch)
	if err != nil {
		return nil, fmt.Errorf("resolving branch ref ID: %w", err)
	}

	mutation := `mutation($input: DeleteRefInput!) {
  deleteRef(input: $input) {
    clientMutationId
  }
}`

	_, err = graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": map[string]any{"refId": refID}})
	if err != nil {
		return nil, err
	}

	return actionOutput("delete_branch", map[string]any{
		"branch":  branch,
		"deleted": true,
	}), nil
}

// ─── Create Tag ──────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreateTag(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	tag := getStringInput(inputs, "tag")
	if tag == "" {
		return nil, fmt.Errorf("'tag' is required for create_tag operation")
	}
	oid := getStringInput(inputs, "oid")
	if oid == "" {
		return nil, fmt.Errorf("'oid' is required for create_tag operation (commit SHA to point the tag at)")
	}
	return p.executeCreateRef(ctx, client, apiBase, owner, repo, "create_tag", "refs/tags/"+tag, oid)
}

// ─── Delete Tag ──────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeDeleteTag(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	tag := getStringInput(inputs, "tag")
	if tag == "" {
		return nil, fmt.Errorf("'tag' is required for delete_tag operation")
	}

	refID, err := p.resolveRefID(ctx, client, apiBase, owner, repo, "refs/tags/"+tag)
	if err != nil {
		return nil, fmt.Errorf("resolving tag ref ID: %w", err)
	}

	mutation := `mutation($input: DeleteRefInput!) {
  deleteRef(input: $input) {
    clientMutationId
  }
}`

	_, err = graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": map[string]any{"refId": refID}})
	if err != nil {
		return nil, err
	}

	return actionOutput("delete_tag", map[string]any{
		"tag":     tag,
		"deleted": true,
	}), nil
}
