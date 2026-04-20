// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── List Review Threads ─────────────────────────────────────────────────────

func (p *GitHubProvider) executeListReviewThreads(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, requiredInputError("list_review_threads", "number", inputs, "")
	}
	perPage := getPerPage(inputs)

	query := `query($owner: String!, $name: String!, $number: Int!, $first: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: $first) {
        nodes {
          id
          isResolved
          isOutdated
          path
          line
          comments(first: 20) {
            nodes {
              id
              body
              author { login }
              createdAt
            }
          }
        }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "number": num, "first": perPage}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	nodes, err := extractNodes(data, "repository.pullRequest.reviewThreads")
	if err != nil {
		return nil, err
	}
	return readOutput(nodes), nil
}

// ─── Reply to Review Thread ──────────────────────────────────────────────────

func (p *GitHubProvider) executeReplyToReviewThread(ctx context.Context, client *httpc.Client, apiBase, _, _ string, inputs map[string]any) (*provider.Output, error) {
	threadID := getStringInput(inputs, "thread_id")
	if threadID == "" {
		return nil, requiredInputError("reply_to_review_thread", "thread_id", inputs, "")
	}
	body := getStringInput(inputs, "body")
	if body == "" {
		return nil, requiredInputError("reply_to_review_thread", "body", inputs, "")
	}

	mutation := `mutation($id: ID!, $body: String!) {
  addPullRequestReviewThreadReply(input: {pullRequestReviewThreadId: $id, body: $body}) {
    comment {
      id
      body
      createdAt
      author { login }
    }
  }
}`

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"id": threadID, "body": body})
	if err != nil {
		return nil, err
	}

	comment, err := extractNodeMap(data, "addPullRequestReviewThreadReply.comment")
	if err != nil {
		return nil, err
	}

	return actionOutput("reply_to_review_thread", comment), nil
}

// ─── Resolve Review Thread ───────────────────────────────────────────────────

func (p *GitHubProvider) executeResolveReviewThread(ctx context.Context, client *httpc.Client, apiBase, _, _ string, inputs map[string]any) (*provider.Output, error) {
	threadID := getStringInput(inputs, "thread_id")
	if threadID == "" {
		return nil, requiredInputError("resolve_review_thread", "thread_id", inputs, "")
	}

	mutation := `mutation($threadId: ID!) {
  resolveReviewThread(input: {threadId: $threadId}) {
    thread {
      id
      isResolved
    }
  }
}`

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"threadId": threadID})
	if err != nil {
		return nil, err
	}

	thread, err := extractNodeMap(data, "resolveReviewThread.thread")
	if err != nil {
		return nil, err
	}

	return actionOutput("resolve_review_thread", thread), nil
}
