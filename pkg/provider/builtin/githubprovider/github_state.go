// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/state"
)

// dispatchStateOperation handles the state capability branch in Execute.
func (p *GitHubProvider) dispatchStateOperation(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	operation := getStringInput(inputs, "operation")

	path := getStringInput(inputs, "path")
	if path == "" {
		return nil, fmt.Errorf("'path' is required for state operations")
	}
	branch := getStringInput(inputs, "branch")
	if branch == "" {
		return nil, fmt.Errorf("'branch' is required for state operations")
	}

	switch operation {
	case "state_load":
		return p.executeStateLoad(ctx, client, apiBase, owner, repo, path, branch)
	case "state_save":
		return p.executeStateSave(ctx, client, apiBase, owner, repo, path, branch, inputs)
	case "state_delete":
		return p.executeStateDelete(ctx, client, apiBase, owner, repo, path, branch, inputs)
	default:
		return nil, fmt.Errorf("unsupported state operation: %s", operation)
	}
}

// executeStateLoad fetches a state JSON file from the repo and parses it.
func (p *GitHubProvider) executeStateLoad(ctx context.Context, client *httpc.Client, apiBase, owner, repo, path, branch string) (*provider.Output, error) {
	expression := branch + ":" + path

	query := `query($owner: String!, $name: String!, $expression: String!) {
  repository(owner: $owner, name: $name) {
    object(expression: $expression) {
      ... on Blob {
        text
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "expression": expression}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, fmt.Errorf("state load: %w", err)
	}

	obj, err := extractNode(data, "repository.object")
	if err != nil {
		return nil, fmt.Errorf("state load: %w", err)
	}
	if obj == nil {
		// File not found -- first run, return empty state
		return &provider.Output{
			Data: map[string]any{
				"success": true,
				"data":    state.NewData(),
			},
		}, nil
	}

	blob, ok := obj.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("state load: unexpected object format")
	}

	text, _ := blob["text"].(string)
	var stateData state.Data
	if err := json.Unmarshal([]byte(text), &stateData); err != nil {
		return nil, fmt.Errorf("state load: unmarshal: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{
			"success": true,
			"data":    &stateData,
		},
	}, nil
}

// executeStateSave serializes state data and commits it to the repo.
func (p *GitHubProvider) executeStateSave(ctx context.Context, client *httpc.Client, apiBase, owner, repo, path, branch string, inputs map[string]any) (*provider.Output, error) {
	rawData, ok := inputs["data"]
	if !ok {
		return nil, fmt.Errorf("state save: 'data' is required")
	}

	jsonBytes, err := json.MarshalIndent(rawData, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("state save: marshal: %w", err)
	}

	headOID, err := p.getHeadOID(ctx, client, apiBase, owner, repo, branch)
	if err != nil {
		return nil, fmt.Errorf("state save: %w", err)
	}

	message := stateCommitMessage(inputs, "chore(state): update state")
	encoded := base64.StdEncoding.EncodeToString(jsonBytes)

	if err := p.createStateCommit(ctx, client, apiBase, owner, repo, branch, message, headOID,
		[]map[string]any{{"path": path, "contents": encoded}}, nil); err != nil {
		return nil, fmt.Errorf("state save: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{"success": true},
	}, nil
}

// executeStateDelete removes a state file from the repo via commit.
func (p *GitHubProvider) executeStateDelete(ctx context.Context, client *httpc.Client, apiBase, owner, repo, path, branch string, inputs map[string]any) (*provider.Output, error) {
	// Check if file exists first
	expression := branch + ":" + path
	query := `query($owner: String!, $name: String!, $expression: String!) {
  repository(owner: $owner, name: $name) {
    object(expression: $expression) {
      ... on Blob { oid }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "expression": expression}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, fmt.Errorf("state delete: check: %w", err)
	}

	obj, err := extractNode(data, "repository.object")
	if err != nil {
		return nil, fmt.Errorf("state delete: check: %w", err)
	}
	if obj == nil {
		// File doesn't exist -- nothing to delete
		return &provider.Output{
			Data: map[string]any{"success": true},
		}, nil
	}

	headOID, err := p.getHeadOID(ctx, client, apiBase, owner, repo, branch)
	if err != nil {
		return nil, fmt.Errorf("state delete: %w", err)
	}

	message := stateCommitMessage(inputs, "chore(state): delete state")
	if err := p.createStateCommit(ctx, client, apiBase, owner, repo, branch, message, headOID,
		nil, []map[string]any{{"path": path}}); err != nil {
		return nil, fmt.Errorf("state delete: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{"success": true},
	}, nil
}

// executeStateDryRun returns mock output for state operations during dry-run.
func (p *GitHubProvider) executeStateDryRun(operation string) (*provider.Output, error) {
	switch operation {
	case "state_load":
		return &provider.Output{
			Data: map[string]any{
				"success": true,
				"data":    state.NewData(),
			},
		}, nil
	case "state_save", "state_delete":
		return &provider.Output{
			Data: map[string]any{"success": true},
		}, nil
	default:
		return nil, fmt.Errorf("unknown state operation: %s", operation)
	}
}

// getHeadOID retrieves the HEAD commit OID for a branch.
func (p *GitHubProvider) getHeadOID(ctx context.Context, client *httpc.Client, apiBase, owner, repo, branch string) (string, error) {
	qualifiedName := "refs/heads/" + branch
	query := `query($owner: String!, $name: String!, $qualifiedName: String!) {
  repository(owner: $owner, name: $name) {
    ref(qualifiedName: $qualifiedName) {
      target { oid }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "qualifiedName": qualifiedName}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return "", fmt.Errorf("get head OID: %w", err)
	}

	ref, err := extractNode(data, "repository.ref")
	if err != nil {
		return "", fmt.Errorf("get head OID: %w", err)
	}
	if ref == nil {
		return "", fmt.Errorf("branch %q not found", branch)
	}

	refMap, ok := ref.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected ref format")
	}
	target, ok := refMap["target"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected ref target format")
	}
	oid, _ := target["oid"].(string)
	return oid, nil
}

// createStateCommit creates a commit with file additions and/or deletions.
func (p *GitHubProvider) createStateCommit(ctx context.Context, client *httpc.Client, apiBase, owner, repo, branch, message, headOID string, additions, deletions []map[string]any) error {
	nwo := owner + "/" + repo
	mutation := `mutation($input: CreateCommitOnBranchInput!) {
  createCommitOnBranch(input: $input) {
    commit { oid }
  }
}`
	fileChanges := map[string]any{}
	if len(additions) > 0 {
		fileChanges["additions"] = additions
	}
	if len(deletions) > 0 {
		fileChanges["deletions"] = deletions
	}

	input := map[string]any{
		"branch": map[string]any{
			"repositoryNameWithOwner": nwo,
			"branchName":              branch,
		},
		"message":         map[string]any{"headline": message},
		"fileChanges":     fileChanges,
		"expectedHeadOid": headOID,
	}
	vars := map[string]any{"input": input}

	_, err := graphqlDo(ctx, client, apiBase, mutation, vars)
	return err
}

// stateCommitMessage extracts a custom commit message from inputs or returns the fallback.
func stateCommitMessage(inputs map[string]any, fallback string) string {
	if msg, ok := inputs["message"].(string); ok && msg != "" {
		return msg
	}
	return fallback
}
