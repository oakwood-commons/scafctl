// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── Create Commit ───────────────────────────────────────────────────────────

// executeCreateCommit creates a signed commit on a branch using the GitHub
// GraphQL createCommitOnBranch mutation. This produces GPG-signed commits
// automatically (signed by GitHub on behalf of the authenticated user).
// Supports multi-file atomic commits with additions and deletions.
//
// On freshly created repositories, the user's push permissions may not be
// fully propagated in the GraphQL layer yet (FORBIDDEN). The function retries
// up to p.commitMaxAttempts times with backoff to handle this eventual
// consistency window.
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

	additions, deletions, err := parseFileChanges(inputs)
	if err != nil {
		return nil, err
	}
	if len(additions) == 0 && len(deletions) == 0 {
		return nil, fmt.Errorf("create_commit requires at least one 'additions' or 'deletions' entry")
	}

	// Retry on FORBIDDEN — when a repo was just created, GraphQL write
	// permissions may take a few seconds to propagate.
	lgr := logger.FromContext(ctx)
	var lastErr error
	for attempt := range p.commitMaxAttempts {
		if attempt > 0 {
			delay := time.Duration(attempt) * p.commitRetryBackoff
			lgr.V(1).Info("retrying createCommitOnBranch after FORBIDDEN",
				"attempt", attempt+1,
				"delay", delay,
				"owner", owner,
				"repo", repo,
			)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		output, err := p.executeCreateCommitGraphQL(ctx, client, apiBase, owner, repo, branch, message, expectedHeadOID, additions, deletions, inputs)
		if err == nil {
			return output, nil
		}
		lastErr = err

		// Only retry on FORBIDDEN (permission propagation)
		if !isGraphQLForbidden(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// fileAddition holds a parsed addition entry.
type fileAddition struct {
	Path    string
	Content string // raw (unencoded) content
}

// fileDeletion holds a parsed deletion entry.
type fileDeletion struct {
	Path string
}

// parseFileChanges extracts additions and deletions from inputs.
func parseFileChanges(inputs map[string]any) ([]fileAddition, []fileDeletion, error) {
	var additions []fileAddition
	if additionsRaw, ok := inputs["additions"].([]any); ok {
		for _, item := range additionsRaw {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("each addition entry must be an object, got %T", item)
			}
			path, _ := m["path"].(string)
			content, _ := m["content"].(string)
			if path == "" || content == "" {
				return nil, nil, fmt.Errorf("each addition must have 'path' and 'content'")
			}
			additions = append(additions, fileAddition{Path: path, Content: content})
		}
	}

	var deletions []fileDeletion
	if deletionsRaw, ok := inputs["deletions"].([]any); ok {
		for _, item := range deletionsRaw {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("each deletion entry must be an object, got %T", item)
			}
			path, _ := m["path"].(string)
			if path == "" {
				return nil, nil, fmt.Errorf("each deletion must have 'path'")
			}
			deletions = append(deletions, fileDeletion{Path: path})
		}
	}

	return additions, deletions, nil
}

// ─── GraphQL path ────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeCreateCommitGraphQL(
	ctx context.Context, client *httpc.Client, apiBase, owner, repo, branch, message, expectedHeadOID string,
	additions []fileAddition, deletions []fileDeletion, inputs map[string]any,
) (*provider.Output, error) {
	fileChanges := map[string]any{}

	if len(additions) > 0 {
		gqlAdditions := make([]map[string]any, 0, len(additions))
		for _, a := range additions {
			gqlAdditions = append(gqlAdditions, map[string]any{
				"path":     a.Path,
				"contents": base64.StdEncoding.EncodeToString([]byte(a.Content)),
			})
		}
		fileChanges["additions"] = gqlAdditions
	}

	if len(deletions) > 0 {
		gqlDeletions := make([]map[string]any, 0, len(deletions))
		for _, d := range deletions {
			gqlDeletions = append(gqlDeletions, map[string]any{
				"path": d.Path,
			})
		}
		fileChanges["deletions"] = gqlDeletions
	}

	messageInput := map[string]any{"headline": message}
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
      additions
      deletions
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
