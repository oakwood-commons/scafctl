// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package githubprovider implements a provider for GitHub API operations.
//
// It supports read operations (repos, files, issues, PRs, releases, branches, tags)
// via the GitHub GraphQL API, write operations (issues, PRs, commits, branches, tags)
// via GraphQL mutations, and release writes via the REST API (no GraphQL mutation exists).
//
// Commit operations use the createCommitOnBranch GraphQL mutation which produces
// GPG-signed commits automatically — no local key management required.
//
// Authentication is handled automatically using the configured GitHub auth handler.
package githubprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

// ProviderName is the registered name for this provider.
const ProviderName = "github"

// defaultAPIBase is the default GitHub API base URL.
const defaultAPIBase = "https://api.github.com"

// allOperations lists every supported operation name for error messages.
var allOperations = []string{
	// Read operations
	"get_repo", "get_file",
	"list_releases", "get_latest_release",
	"list_pull_requests", "get_pull_request",
	"list_issues", "get_issue", "list_issue_comments",
	"list_branches", "get_branch", "list_tags",
	// Issue write operations
	"create_issue", "update_issue", "create_issue_comment",
	// PR write operations
	"create_pull_request", "update_pull_request", "merge_pull_request", "close_pull_request",
	// Commit & ref operations
	"create_commit", "get_head_oid",
	"create_branch", "delete_branch", "create_tag", "delete_tag",
	// Release write operations (REST)
	"create_release", "update_release", "delete_release",
}

// readOperations are operations that return data (CapabilityFrom/Transform).
var readOperations = map[string]bool{
	"get_repo": true, "get_file": true,
	"list_releases": true, "get_latest_release": true,
	"list_pull_requests": true, "get_pull_request": true,
	"list_issues": true, "get_issue": true, "list_issue_comments": true,
	"list_branches": true, "get_branch": true, "list_tags": true,
	"get_head_oid": true,
}

// GitHubProvider implements GitHub API operations as a provider.
type GitHubProvider struct {
	descriptor *provider.Descriptor
	// client can be overridden for testing via WithClient option.
	client *httpc.Client
}

// Option configures a GitHubProvider.
type Option func(*GitHubProvider)

// WithClient sets a custom httpc.Client (useful for testing).
func WithClient(c *httpc.Client) Option {
	return func(p *GitHubProvider) {
		p.client = c
	}
}

// NewGitHubProvider creates a new GitHub API provider.
func NewGitHubProvider(opts ...Option) *GitHubProvider {
	version, _ := semver.NewVersion("2.0.0")

	p := &GitHubProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "GitHub API",
			APIVersion:  "v1",
			Version:     version,
			Description: "Interact with GitHub via GraphQL (reads, issues, PRs, signed commits, branches, tags) " +
				"and REST (releases). Uses the configured GitHub auth handler automatically. " +
				"Commit operations use createCommitOnBranch for GPG-signed multi-file atomic commits.",
			Category:     "data",
			MockBehavior: "Returns mock data for the requested operation without making real API calls",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityTransform,
				provider.CapabilityAction,
			},
			Schema: buildInputSchema(),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.AnyProp("The API response data — structure varies by operation"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.AnyProp("The API response data — structure varies by operation"),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
					"success":   schemahelper.BoolProp("Whether the operation succeeded"),
					"result":    schemahelper.AnyProp("The API response data — structure varies by operation"),
					"error":     schemahelper.StringProp("Error message if the operation failed"),
					"operation": schemahelper.StringProp("The operation that was performed"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Get repository info",
					Description: "Fetch metadata for a GitHub repository",
					YAML: `operation: get_repo
owner: octocat
repo: hello-world`,
				},
				{
					Name:        "Get file content",
					Description: "Retrieve a file from a repository at a specific branch",
					YAML: `operation: get_file
owner: octocat
repo: hello-world
path: README.md
ref: main`,
				},
				{
					Name:        "Create a signed commit with multiple files",
					Description: "Atomically commit multiple files to a branch with GPG signing",
					YAML: `operation: create_commit
owner: my-org
repo: my-repo
branch: feature-branch
message: "feat: add scaffolded files"
expected_head_oid: abc123def456
additions:
  - path: src/main.go
    content: "package main\n\nfunc main() {}\n"
  - path: README.md
    content: "# My Project\n"`,
				},
				{
					Name:        "Create an issue",
					Description: "Create a new issue in a repository",
					YAML: `operation: create_issue
owner: my-org
repo: my-repo
title: "Bug: something is broken"
body: "Steps to reproduce..."
labels:
  - bug
  - priority/high`,
				},
				{
					Name:        "Create a pull request",
					Description: "Open a new pull request",
					YAML: `operation: create_pull_request
owner: my-org
repo: my-repo
title: "feat: add new feature"
body: "This PR adds..."
head: feature-branch
base: main
draft: true`,
				},
				{
					Name:        "Create a release",
					Description: "Create a new release (uses REST API)",
					YAML: `operation: create_release
owner: my-org
repo: my-repo
tag_name: v1.0.0
name: "Release 1.0.0"
body: "First stable release"`,
				},
			},
			Links: []provider.Link{
				{Name: "GitHub GraphQL API", URL: "https://docs.github.com/en/graphql"},
				{Name: "GitHub REST API", URL: "https://docs.github.com/en/rest"},
			},
			Tags: []string{"github", "api", "data", "graphql", "git"},
		},
	}

	for _, opt := range opts {
		opt(p)
	}
	return p
}

// buildInputSchema constructs the JSON Schema for all GitHub provider operations.
func buildInputSchema() *jsonschema.Schema {
	operationEnums := make([]any, len(allOperations))
	for i, op := range allOperations {
		operationEnums[i] = op
	}

	return schemahelper.ObjectSchema(
		[]string{"operation", "owner", "repo"},
		map[string]*jsonschema.Schema{
			// --- Common fields ---
			"operation": schemahelper.StringProp("GitHub API operation to perform",
				schemahelper.WithEnum(operationEnums...),
				schemahelper.WithExample("get_repo"),
			),
			"owner": schemahelper.StringProp("Repository owner (user or organization)",
				schemahelper.WithExample("octocat"),
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"repo": schemahelper.StringProp("Repository name",
				schemahelper.WithExample("hello-world"),
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"api_base": schemahelper.StringProp("GitHub API base URL. Defaults to https://api.github.com. Set for GitHub Enterprise.",
				schemahelper.WithDefault(defaultAPIBase),
				schemahelper.WithFormat("uri"),
			),

			// --- Read operation fields ---
			"path": schemahelper.StringProp("File path within the repository (for get_file, create_or_update_file, delete_file)",
				schemahelper.WithExample("README.md"),
				schemahelper.WithMaxLength(*ptrs.IntPtr(1000)),
			),
			"ref": schemahelper.StringProp("Git reference (branch, tag, or commit SHA). Defaults to the repo's default branch.",
				schemahelper.WithExample("main"),
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"number": schemahelper.IntProp("Issue or pull request number",
				schemahelper.WithMinimum(1),
				schemahelper.WithExample(42),
			),
			"state": schemahelper.StringProp("Filter by state for list operations",
				schemahelper.WithEnum("open", "closed", "all", "OPEN", "CLOSED", "MERGED"),
				schemahelper.WithDefault("open"),
			),
			"per_page": schemahelper.IntProp("Number of results per page (max 100)",
				schemahelper.WithMinimum(1),
				schemahelper.WithMaximum(100),
				schemahelper.WithDefault(30),
			),

			// --- Issue fields ---
			"title": schemahelper.StringProp("Title for issue or pull request create/update",
				schemahelper.WithMaxLength(*ptrs.IntPtr(1000)),
			),
			"body": schemahelper.StringProp("Body text for issue, pull request, release, or comment",
				schemahelper.WithMaxLength(*ptrs.IntPtr(65536)),
			),
			"labels": schemahelper.ArrayProp("Labels to apply (names for issues, IDs resolved internally)",
				schemahelper.WithItems(schemahelper.StringProp("Label name")),
				schemahelper.WithMaxItems(100),
			),
			"assignees": schemahelper.ArrayProp("Assignee login usernames",
				schemahelper.WithItems(schemahelper.StringProp("Username")),
				schemahelper.WithMaxItems(10),
			),

			// --- PR fields ---
			"head": schemahelper.StringProp("Head branch name for creating a pull request",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"base": schemahelper.StringProp("Base branch name for creating/updating a pull request",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"draft": schemahelper.BoolProp("Whether to create the pull request as a draft"),
			"merge_method": schemahelper.StringProp("Merge method for merge_pull_request",
				schemahelper.WithEnum("MERGE", "SQUASH", "REBASE"),
				schemahelper.WithDefault("MERGE"),
			),
			"commit_title": schemahelper.StringProp("Commit title for merge_pull_request",
				schemahelper.WithMaxLength(*ptrs.IntPtr(500)),
			),
			"commit_message": schemahelper.StringProp("Commit message body for merge_pull_request",
				schemahelper.WithMaxLength(*ptrs.IntPtr(65536)),
			),

			// --- Commit fields ---
			"branch": schemahelper.StringProp("Branch name for commit, branch, or tag operations",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"message": schemahelper.StringProp("Commit message headline",
				schemahelper.WithMaxLength(*ptrs.IntPtr(500)),
			),
			"message_body": schemahelper.StringProp("Commit message body (optional extended description)",
				schemahelper.WithMaxLength(*ptrs.IntPtr(65536)),
			),
			"expected_head_oid": schemahelper.StringProp("Expected HEAD OID for optimistic locking in create_commit",
				schemahelper.WithPattern("^[0-9a-f]{40}$"),
			),
			"additions": schemahelper.ArrayProp("Files to add/update in a commit. Each item: {path, content}",
				schemahelper.WithItems(schemahelper.ObjectProp(
					"File addition",
					[]string{"path", "content"},
					map[string]*jsonschema.Schema{
						"path":    schemahelper.StringProp("File path relative to repository root"),
						"content": schemahelper.StringProp("File content (plain text, auto-encoded to base64)"),
					},
				)),
				schemahelper.WithMaxItems(500),
			),
			"deletions": schemahelper.ArrayProp("File paths to delete in a commit",
				schemahelper.WithItems(schemahelper.ObjectProp(
					"File deletion",
					[]string{"path"},
					map[string]*jsonschema.Schema{
						"path": schemahelper.StringProp("File path relative to repository root"),
					},
				)),
				schemahelper.WithMaxItems(500),
			),

			// --- Ref fields ---
			"oid": schemahelper.StringProp("Git object ID (commit SHA) for branch/tag creation",
				schemahelper.WithPattern("^[0-9a-f]{40}$"),
			),
			"tag": schemahelper.StringProp("Tag name for create_tag/delete_tag",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),

			// --- Release fields (REST) ---
			"tag_name": schemahelper.StringProp("Tag name for the release",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"target_commitish": schemahelper.StringProp("Commitish value for the release tag (branch or SHA)",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"name": schemahelper.StringProp("Release name/title",
				schemahelper.WithMaxLength(*ptrs.IntPtr(500)),
			),
			"prerelease": schemahelper.BoolProp("Whether this is a prerelease"),
			"release_id": schemahelper.IntProp("Release ID for update_release/delete_release",
				schemahelper.WithMinimum(1),
			),
		},
	)
}

// Descriptor returns the provider metadata, schema, and capabilities.
func (p *GitHubProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// getClient returns the provider's httpc client, creating a default one if needed.
func (p *GitHubProvider) getClient(ctx context.Context) *httpc.Client {
	if p.client != nil {
		return p.client
	}
	cfg := httpc.DefaultConfig()
	cfg.EnableCache = false
	cfg.OnUnauthorized = func(innerCtx context.Context) (string, error) {
		handler, err := auth.GetHandler(innerCtx, "github")
		if err != nil {
			return "", fmt.Errorf("github auth handler unavailable: %w", err)
		}
		token, err := handler.GetToken(innerCtx, auth.TokenOptions{})
		if err != nil {
			return "", fmt.Errorf("refreshing github token: %w", err)
		}
		return "Bearer " + token.AccessToken, nil
	}
	cfg.RequestHooks = append(cfg.RequestHooks, func(req *http.Request) error {
		// Inject auth token on every request
		handler, err := auth.GetHandler(ctx, "github")
		if err != nil {
			lgr := logger.FromContext(ctx)
			lgr.V(1).Info("GitHub auth handler not available, making unauthenticated request", "error", err)
			return nil
		}
		token, err := handler.GetToken(ctx, auth.TokenOptions{})
		if err != nil {
			lgr := logger.FromContext(ctx)
			lgr.V(1).Info("GitHub auth token unavailable, making unauthenticated request", "error", err)
			return nil
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		return nil
	})
	return httpc.NewClient(cfg)
}

// Execute runs the requested GitHub API operation.
func (p *GitHubProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map input, got %T", ProviderName, input)
	}

	operation, _ := inputs["operation"].(string)
	lgr.V(1).Info("executing provider", "provider", ProviderName, "operation", operation)

	// Dry-run support: return mock data for write operations
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		return p.executeDryRun(operation, inputs)
	}

	owner, _ := inputs["owner"].(string)
	repo, _ := inputs["repo"].(string)
	apiBase, _ := inputs["api_base"].(string)
	if apiBase == "" {
		apiBase = defaultAPIBase
	}
	apiBase = strings.TrimRight(apiBase, "/")

	client := p.getClient(ctx)

	var result *provider.Output
	var err error

	switch operation {
	// --- Read operations (GraphQL) ---
	case "get_repo":
		result, err = p.executeGetRepo(ctx, client, apiBase, owner, repo)
	case "get_file":
		result, err = p.executeGetFile(ctx, client, apiBase, owner, repo, inputs)
	case "list_releases":
		result, err = p.executeListReleases(ctx, client, apiBase, owner, repo, inputs)
	case "get_latest_release":
		result, err = p.executeGetLatestRelease(ctx, client, apiBase, owner, repo)
	case "list_pull_requests":
		result, err = p.executeListPullRequests(ctx, client, apiBase, owner, repo, inputs)
	case "get_pull_request":
		result, err = p.executeGetPullRequest(ctx, client, apiBase, owner, repo, inputs)
	case "list_issues":
		result, err = p.executeListIssues(ctx, client, apiBase, owner, repo, inputs)
	case "get_issue":
		result, err = p.executeGetIssue(ctx, client, apiBase, owner, repo, inputs)
	case "list_issue_comments":
		result, err = p.executeListIssueComments(ctx, client, apiBase, owner, repo, inputs)
	case "list_branches":
		result, err = p.executeListBranches(ctx, client, apiBase, owner, repo, inputs)
	case "get_branch":
		result, err = p.executeGetBranch(ctx, client, apiBase, owner, repo, inputs)
	case "list_tags":
		result, err = p.executeListTags(ctx, client, apiBase, owner, repo, inputs)
	case "get_head_oid":
		result, err = p.executeGetHeadOID(ctx, client, apiBase, owner, repo, inputs)

	// --- Issue write operations (GraphQL mutations) ---
	case "create_issue":
		result, err = p.executeCreateIssue(ctx, client, apiBase, owner, repo, inputs)
	case "update_issue":
		result, err = p.executeUpdateIssue(ctx, client, apiBase, owner, repo, inputs)
	case "create_issue_comment":
		result, err = p.executeCreateIssueComment(ctx, client, apiBase, owner, repo, inputs)

	// --- PR write operations (GraphQL mutations) ---
	case "create_pull_request":
		result, err = p.executeCreatePullRequest(ctx, client, apiBase, owner, repo, inputs)
	case "update_pull_request":
		result, err = p.executeUpdatePullRequest(ctx, client, apiBase, owner, repo, inputs)
	case "merge_pull_request":
		result, err = p.executeMergePullRequest(ctx, client, apiBase, owner, repo, inputs)
	case "close_pull_request":
		result, err = p.executeClosePullRequest(ctx, client, apiBase, owner, repo, inputs)

	// --- Commit & ref operations (GraphQL mutations) ---
	case "create_commit":
		result, err = p.executeCreateCommit(ctx, client, apiBase, owner, repo, inputs)
	case "create_branch":
		result, err = p.executeCreateBranch(ctx, client, apiBase, owner, repo, inputs)
	case "delete_branch":
		result, err = p.executeDeleteBranch(ctx, client, apiBase, owner, repo, inputs)
	case "create_tag":
		result, err = p.executeCreateTag(ctx, client, apiBase, owner, repo, inputs)
	case "delete_tag":
		result, err = p.executeDeleteTag(ctx, client, apiBase, owner, repo, inputs)

	// --- Release write operations (REST) ---
	case "create_release":
		result, err = p.executeCreateRelease(ctx, client, apiBase, owner, repo, inputs)
	case "update_release":
		result, err = p.executeUpdateRelease(ctx, client, apiBase, owner, repo, inputs)
	case "delete_release":
		result, err = p.executeDeleteRelease(ctx, client, apiBase, owner, repo, inputs)

	default:
		return nil, fmt.Errorf("%s: unknown operation %q — supported: %s", ProviderName, operation, strings.Join(allOperations, ", "))
	}

	if err != nil {
		// For action operations, return success=false rather than a Go error
		if !readOperations[operation] {
			return &provider.Output{
				Data: map[string]any{
					"success":   false,
					"operation": operation,
					"error":     err.Error(),
				},
			}, nil
		}
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", operation)
	return result, nil
}

// executeDryRun returns mock output without making API calls.
func (p *GitHubProvider) executeDryRun(operation string, _ map[string]any) (*provider.Output, error) {
	if readOperations[operation] {
		return &provider.Output{
			Data: map[string]any{
				"result": map[string]any{
					"dry_run":   true,
					"operation": operation,
				},
			},
		}, nil
	}
	return &provider.Output{
		Data: map[string]any{
			"success":   true,
			"operation": operation,
			"result": map[string]any{
				"dry_run": true,
			},
		},
	}, nil
}

// readOutput wraps a result in the standard read output shape.
func readOutput(result any) *provider.Output {
	return &provider.Output{
		Data: map[string]any{
			"result": result,
		},
	}
}

// actionOutput wraps a result in the standard action output shape.
func actionOutput(operation string, result any) *provider.Output {
	return &provider.Output{
		Data: map[string]any{
			"success":   true,
			"operation": operation,
			"result":    result,
		},
	}
}

// getPerPage extracts the per_page value from inputs, defaulting to 30.
func getPerPage(inputs map[string]any) int {
	if v, ok := getIntInput(inputs, "per_page"); ok && v > 0 {
		return v
	}
	return 30
}

// getStringInput extracts a string from the input map.
func getStringInput(inputs map[string]any, key string) string {
	v, _ := inputs[key].(string)
	return v
}

// getStringSliceInput extracts a string slice from the input map.
func getStringSliceInput(inputs map[string]any, key string) []string {
	v, ok := inputs[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// getBoolInput extracts a bool from the input map.
func getBoolInput(inputs map[string]any, key string) (bool, bool) {
	v, ok := inputs[key].(bool)
	return v, ok
}

// getIntInput extracts an integer from the input map, handling both float64 (from JSON) and int.
func getIntInput(inputs map[string]any, key string) (int, bool) {
	v, ok := inputs[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// doRESTRequest performs an authenticated REST API request and returns the parsed JSON.
// Used for release mutations (no GraphQL mutation available).
func (p *GitHubProvider) doRESTRequest(ctx context.Context, client *httpc.Client, method, url string, body any) (any, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating REST request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("REST request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading REST response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var ghErr map[string]any
		if json.Unmarshal(respBody, &ghErr) == nil {
			if msg, ok := ghErr["message"].(string); ok {
				return nil, fmt.Errorf("GitHub API error (HTTP %d): %s", resp.StatusCode, msg)
			}
		}
		return nil, fmt.Errorf("GitHub API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	// DELETE with 204 No Content
	if resp.StatusCode == http.StatusNoContent {
		return map[string]any{}, nil
	}

	var result any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing REST response JSON: %w", err)
	}

	return result, nil
}
