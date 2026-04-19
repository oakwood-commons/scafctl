// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── List Review Threads Tests ───────────────────────────────────────────────

func TestGitHubProvider_Execute_ListReviewThreads(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, graphqlHandler(t,
		func(query string, vars map[string]any) {
			assert.Contains(t, query, "reviewThreads")
			assert.Equal(t, "test-org", vars["owner"])
			assert.Equal(t, "test-repo", vars["name"])
			assert.Equal(t, float64(42), vars["number"])
		},
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"reviewThreads": map[string]any{
							"nodes": []any{
								map[string]any{
									"id":         "PRT_abc123",
									"isResolved": false,
									"isOutdated": false,
									"path":       "pkg/main.go",
									"line":       float64(10),
									"comments": map[string]any{
										"nodes": []any{
											map[string]any{
												"id":        "PRRC_001",
												"body":      "Should use Writer here",
												"author":    map[string]any{"login": "reviewer1"},
												"createdAt": "2025-01-01T00:00:00Z",
											},
										},
									},
								},
								map[string]any{
									"id":         "PRT_def456",
									"isResolved": true,
									"isOutdated": true,
									"path":       "pkg/other.go",
									"line":       float64(25),
									"comments": map[string]any{
										"nodes": []any{},
									},
								},
							},
						},
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_review_threads",
		"owner":     "test-org",
		"repo":      "test-repo",
		"number":    float64(42),
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result := output.Data.(map[string]any)["result"].([]any)
	assert.Len(t, result, 2)

	thread1 := result[0].(map[string]any)
	assert.Equal(t, "PRT_abc123", thread1["id"])
	assert.Equal(t, false, thread1["isResolved"])
	assert.Equal(t, "pkg/main.go", thread1["path"])

	thread2 := result[1].(map[string]any)
	assert.Equal(t, true, thread2["isResolved"])
}

func TestGitHubProvider_Execute_ListReviewThreads_Empty(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, graphqlHandler(t, nil,
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"reviewThreads": map[string]any{
							"nodes": []any{},
						},
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "list_review_threads",
		"owner":     "test-org",
		"repo":      "test-repo",
		"number":    float64(1),
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)["result"].([]any)
	assert.Empty(t, result)
}

func TestExecuteListReviewThreads_MissingNumber(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeListReviewThreads(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "number")
}

// ─── Reply to Review Thread Tests ────────────────────────────────────────────

func TestGitHubProvider_Execute_ReplyToReviewThread(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, graphqlHandler(t,
		func(query string, vars map[string]any) {
			assert.Contains(t, query, "addPullRequestReviewThreadReply")
			assert.Equal(t, "PRT_abc123", vars["id"])
			assert.Equal(t, "Fixed, thanks!", vars["body"])
		},
		map[string]any{
			"data": map[string]any{
				"addPullRequestReviewThreadReply": map[string]any{
					"comment": map[string]any{
						"id":        "PRRC_new",
						"body":      "Fixed, thanks!",
						"createdAt": "2025-01-02T00:00:00Z",
						"author":    map[string]any{"login": "bot"},
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "reply_to_review_thread",
		"owner":     "test-org",
		"repo":      "test-repo",
		"thread_id": "PRT_abc123",
		"body":      "Fixed, thanks!",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, "PRRC_new", result["id"])
	assert.Equal(t, "Fixed, thanks!", result["body"])
}

func TestExecuteReplyToReviewThread_MissingThreadID(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeReplyToReviewThread(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"body": "reply text",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "thread_id")
}

func TestExecuteReplyToReviewThread_MissingBody(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeReplyToReviewThread(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"thread_id": "PRT_abc123",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "body")
}

// ─── Resolve Review Thread Tests ─────────────────────────────────────────────

func TestGitHubProvider_Execute_ResolveReviewThread(t *testing.T) {
	t.Parallel()

	p, baseURL := testProvider(t, graphqlHandler(t,
		func(query string, vars map[string]any) {
			assert.Contains(t, query, "resolveReviewThread")
			assert.Equal(t, "PRT_abc123", vars["threadId"])
		},
		map[string]any{
			"data": map[string]any{
				"resolveReviewThread": map[string]any{
					"thread": map[string]any{
						"id":         "PRT_abc123",
						"isResolved": true,
					},
				},
			},
		},
	))

	output, err := p.Execute(context.Background(), map[string]any{
		"operation": "resolve_review_thread",
		"owner":     "test-org",
		"repo":      "test-repo",
		"thread_id": "PRT_abc123",
		"api_base":  baseURL,
	})

	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["success"])
	result := data["result"].(map[string]any)
	assert.Equal(t, true, result["isResolved"])
}

func TestExecuteResolveReviewThread_MissingThreadID(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeResolveReviewThread(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "thread_id")
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkExecuteListReviewThreads(b *testing.B) {
	p, baseURL := testProvider(b, graphqlHandler(b, nil,
		map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"reviewThreads": map[string]any{
							"nodes": []any{
								map[string]any{
									"id":         "PRT_001",
									"isResolved": false,
									"path":       "file.go",
									"line":       float64(1),
									"comments":   map[string]any{"nodes": []any{}},
								},
							},
						},
					},
				},
			},
		},
	))

	inputs := map[string]any{
		"operation": "list_review_threads",
		"owner":     "org",
		"repo":      "repo",
		"number":    float64(1),
		"api_base":  baseURL,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(context.Background(), inputs)
	}
}
