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

// Reaction content values supported by the GitHub API.
// See: https://docs.github.com/en/rest/reactions/reactions
var validReactionContents = map[string]bool{
	"+1":       true,
	"-1":       true,
	"laugh":    true,
	"confused": true,
	"heart":    true,
	"hooray":   true,
	"rocket":   true,
	"eyes":     true,
}

// ─── Add Reaction ────────────────────────────────────────────────────────────

// executeAddReaction adds a reaction to an issue, PR, or comment.
// The reaction_subject determines the API path:
//   - "issue" or "pull_request" → /repos/{owner}/{repo}/issues/{number}/reactions
//   - "issue_comment" → /repos/{owner}/{repo}/issues/comments/{comment_id}/reactions
func (p *GitHubProvider) executeAddReaction(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	content := getStringInput(inputs, "reaction_content")
	if content == "" {
		return nil, requiredInputError("add_reaction", "reaction_content", inputs, "must be one of: +1, -1, laugh, confused, heart, hooray, rocket, eyes")
	}
	if !validReactionContents[content] {
		return nil, fmt.Errorf("add_reaction: invalid reaction_content %q — must be one of: +1, -1, laugh, confused, heart, hooray, rocket, eyes", content)
	}

	subject := getStringInput(inputs, "reaction_subject")
	if subject == "" {
		subject = "issue"
	}

	reqBody := map[string]any{
		"content": content,
	}

	var restURL string
	switch subject {
	case "issue", "pull_request":
		number, ok := getIntInput(inputs, "number")
		if !ok || number == 0 {
			return nil, requiredInputError("add_reaction", "number", inputs, "required when reaction_subject is issue or pull_request")
		}
		// GitHub's REST API uses the issues endpoint for both issues and PRs.
		restURL = fmt.Sprintf("%s/repos/%s/%s/issues/%d/reactions", apiBase, owner, repo, number)
	case "issue_comment":
		commentID, ok := getIntInput(inputs, "comment_id")
		if !ok || commentID == 0 {
			return nil, requiredInputError("add_reaction", "comment_id", inputs, "required when reaction_subject is issue_comment")
		}
		restURL = fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d/reactions", apiBase, owner, repo, commentID)
	default:
		return nil, fmt.Errorf("add_reaction: unsupported reaction_subject %q — must be one of: issue, pull_request, issue_comment", subject)
	}

	result, err := p.doRESTRequest(ctx, client, http.MethodPost, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("adding reaction: %w", err)
	}
	return actionOutput("add_reaction", result), nil
}

// ─── List Reactions ──────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListReactions(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	subject := getStringInput(inputs, "reaction_subject")
	if subject == "" {
		subject = "issue"
	}
	perPage := getPerPage(inputs)

	var restURL string
	switch subject {
	case "issue", "pull_request":
		number, ok := getIntInput(inputs, "number")
		if !ok || number == 0 {
			return nil, requiredInputError("list_reactions", "number", inputs, "required when reaction_subject is issue or pull_request")
		}
		restURL = fmt.Sprintf("%s/repos/%s/%s/issues/%d/reactions?per_page=%d", apiBase, owner, repo, number, perPage)
	case "issue_comment":
		commentID, ok := getIntInput(inputs, "comment_id")
		if !ok || commentID == 0 {
			return nil, requiredInputError("list_reactions", "comment_id", inputs, "required when reaction_subject is issue_comment")
		}
		restURL = fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d/reactions?per_page=%d", apiBase, owner, repo, commentID, perPage)
	default:
		return nil, fmt.Errorf("list_reactions: unsupported reaction_subject %q — must be one of: issue, pull_request, issue_comment", subject)
	}

	result, err := p.doRESTRequest(ctx, client, http.MethodGet, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing reactions: %w", err)
	}
	return readOutput(result), nil
}

// ─── Delete Reaction ─────────────────────────────────────────────────────────

func (p *GitHubProvider) executeDeleteReaction(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	reactionID, ok := getIntInput(inputs, "reaction_id")
	if !ok || reactionID == 0 {
		return nil, requiredInputError("delete_reaction", "reaction_id", inputs, "")
	}

	subject := getStringInput(inputs, "reaction_subject")
	if subject == "" {
		subject = "issue"
	}

	var restURL string
	switch subject {
	case "issue", "pull_request":
		number, ok := getIntInput(inputs, "number")
		if !ok || number == 0 {
			return nil, requiredInputError("delete_reaction", "number", inputs, "required when reaction_subject is issue or pull_request")
		}
		restURL = fmt.Sprintf("%s/repos/%s/%s/issues/%d/reactions/%d", apiBase, owner, repo, number, reactionID)
	case "issue_comment":
		commentID, ok := getIntInput(inputs, "comment_id")
		if !ok || commentID == 0 {
			return nil, requiredInputError("delete_reaction", "comment_id", inputs, "required when reaction_subject is issue_comment")
		}
		restURL = fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d/reactions/%d", apiBase, owner, repo, commentID, reactionID)
	default:
		return nil, fmt.Errorf("delete_reaction: unsupported reaction_subject %q — must be one of: issue, pull_request, issue_comment", subject)
	}

	result, err := p.doRESTRequest(ctx, client, http.MethodDelete, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("deleting reaction: %w", err)
	}
	return actionOutput("delete_reaction", result), nil
}
