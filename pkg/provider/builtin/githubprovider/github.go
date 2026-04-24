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
	"slices"
	"sort"
	"strings"
	"time"

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

// Default retry configuration values. These are production defaults that can
// be overridden via WithRetryConfig for testing.
const (
	defaultCommitMaxAttempts    = 5
	defaultCommitRetryBackoff   = 3 * time.Second
	defaultWaitMaxAttempts      = 15
	defaultWaitPollInterval     = 2 * time.Second
	defaultInitRepoMaxRetries   = 3
	defaultInitRepoRetryBackoff = 1 * time.Second
)

// allOperations lists every supported operation name for error messages.
var allOperations = []string{
	// Read operations
	"get_repo", "get_file",
	"list_releases", "get_latest_release",
	"list_pull_requests", "get_pull_request",
	"list_issues", "get_issue", "list_issue_comments",
	"list_branches", "get_branch", "list_tags",
	"list_pr_comments",
	// Review thread operations
	"list_review_threads", "reply_to_review_thread", "resolve_review_thread",
	// CI/CD check operations (REST)
	"list_check_runs", "get_workflow_run",
	// Commit lookup operations (REST)
	"list_commit_pulls",
	// Issue write operations
	"create_issue", "update_issue", "create_issue_comment",
	// PR write operations
	"create_pull_request", "update_pull_request", "merge_pull_request", "close_pull_request",
	// Commit & ref operations
	"create_commit", "get_head_oid",
	"create_branch", "delete_branch", "create_tag", "delete_tag",
	// Release write operations (REST)
	"create_release", "update_release", "delete_release",
	// Repository management operations (GraphQL + REST)
	"create_repo", "update_repo", "create_ruleset",
	"enable_vulnerability_alerts", "enable_automated_security_fixes",
	// Label operations (REST)
	"list_labels", "create_label", "update_label", "delete_label",
	"add_labels_to_issue", "remove_label_from_issue",
	// Milestone operations (REST)
	"list_milestones", "create_milestone", "update_milestone", "delete_milestone",
	// Reaction operations (REST)
	"add_reaction", "list_reactions", "delete_reaction",
	// Collaborator operations (REST)
	"list_collaborators", "add_collaborator", "remove_collaborator",
	// Webhook operations (REST)
	"list_webhooks", "create_webhook", "update_webhook", "delete_webhook",
	// GitHub Actions operations (REST)
	"dispatch_workflow", "list_workflow_runs", "cancel_workflow_run", "rerun_workflow",
	"list_repo_variables", "create_or_update_variable", "delete_variable",
	"list_environments", "create_or_update_environment", "delete_environment",
	// Repository settings (REST)
	"list_topics", "replace_topics",
	"fork_repo", "create_from_template",
	// Custom properties (REST)
	"list_custom_properties", "set_custom_properties",
	// Generic API call (REST)
	"api_call",
}

// readOperations are operations that return data (CapabilityFrom/Transform).
var readOperations = map[string]bool{
	"get_repo": true, "get_file": true,
	"list_releases": true, "get_latest_release": true,
	"list_pull_requests": true, "get_pull_request": true,
	"list_issues": true, "get_issue": true, "list_issue_comments": true,
	"list_branches": true, "get_branch": true, "list_tags": true,
	"get_head_oid":        true,
	"list_pr_comments":    true,
	"list_review_threads": true,
	"list_check_runs":     true, "get_workflow_run": true,
	"list_commit_pulls":      true,
	"list_labels":            true,
	"list_milestones":        true,
	"list_reactions":         true,
	"list_collaborators":     true,
	"list_webhooks":          true,
	"list_workflow_runs":     true,
	"list_repo_variables":    true,
	"list_environments":      true,
	"list_topics":            true,
	"list_custom_properties": true,
}

// GitHubProvider implements GitHub API operations as a provider.
type GitHubProvider struct {
	descriptor *provider.Descriptor
	// client can be overridden for testing via WithClient option.
	client *httpc.Client

	// Retry configuration — overridable for testing.
	commitMaxAttempts    int
	commitRetryBackoff   time.Duration
	waitMaxAttempts      int
	waitPollInterval     time.Duration
	initRepoMaxRetries   int
	initRepoRetryBackoff time.Duration
}

// Option configures a GitHubProvider.
type Option func(*GitHubProvider)

// WithClient sets a custom httpc.Client (useful for testing).
func WithClient(c *httpc.Client) Option {
	return func(p *GitHubProvider) {
		p.client = c
	}
}

// WithRetryConfig overrides the default retry timing (useful for testing).
// Attempt counts are clamped to a minimum of 1; durations are clamped to >= 0.
func WithRetryConfig(commitMaxAttempts int, commitRetryBackoff time.Duration, waitMaxAttempts int, waitPollInterval time.Duration, initRepoMaxRetries int, initRepoRetryBackoff time.Duration) Option {
	return func(p *GitHubProvider) {
		p.commitMaxAttempts = max(1, commitMaxAttempts)
		p.commitRetryBackoff = max(0, commitRetryBackoff)
		p.waitMaxAttempts = max(1, waitMaxAttempts)
		p.waitPollInterval = max(0, waitPollInterval)
		p.initRepoMaxRetries = max(1, initRepoMaxRetries)
		p.initRepoRetryBackoff = max(0, initRepoRetryBackoff)
	}
}

// NewGitHubProvider creates a new GitHub API provider.
func NewGitHubProvider(opts ...Option) *GitHubProvider {
	version, _ := semver.NewVersion("2.1.0")

	p := &GitHubProvider{
		commitMaxAttempts:    defaultCommitMaxAttempts,
		commitRetryBackoff:   defaultCommitRetryBackoff,
		waitMaxAttempts:      defaultWaitMaxAttempts,
		waitPollInterval:     defaultWaitPollInterval,
		initRepoMaxRetries:   defaultInitRepoMaxRetries,
		initRepoRetryBackoff: defaultInitRepoRetryBackoff,
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "GitHub API",
			APIVersion:  "v1",
			Version:     version,
			Description: "Interact with GitHub via GraphQL (reads, issues, PRs, review threads, signed commits, branches, tags, " +
				"repos, branch protection) and REST (releases, CI check runs, workflow runs, labels, milestones, reactions, " +
				"collaborators, webhooks, Actions workflows, variables, environments, repo settings, tag protection, security settings). " +
				"Includes a generic api_call operation for arbitrary GitHub REST endpoints. " +
				"Uses the configured GitHub auth handler automatically. " +
				"Commit operations use createCommitOnBranch for GPG-signed multi-file atomic commits.",
			Category: "data",
			WhatIf: func(_ context.Context, input any) (string, error) {
				inputs, ok := input.(map[string]any)
				if !ok {
					return "", nil
				}
				operation, _ := inputs["operation"].(string)
				owner, _ := inputs["owner"].(string)
				repo, _ := inputs["repo"].(string)
				target := owner + "/" + repo
				switch operation {
				case "state_load":
					return fmt.Sprintf("Would load state from %s", target), nil
				case "state_save":
					return fmt.Sprintf("Would save state to %s (creating a commit)", target), nil
				case "state_delete":
					return fmt.Sprintf("Would delete state at %s (creating a commit)", target), nil
				default:
					return fmt.Sprintf("Would perform GitHub %s on %s", operation, target), nil
				}
			},
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityTransform,
				provider.CapabilityAction,
				provider.CapabilityState,
			},
			// WriteOperations lists operations that mutate state.
			// These are rejected in resolver (CapabilityFrom) context.
			// api_call is intentionally excluded: the user controls the HTTP method,
			// so it cannot be classified as read or write at the provider level.
			WriteOperations: []string{
				// Review thread mutations
				"reply_to_review_thread", "resolve_review_thread",
				// Issue mutations
				"create_issue", "update_issue", "create_issue_comment",
				// PR mutations
				"create_pull_request", "update_pull_request", "merge_pull_request", "close_pull_request",
				// Commit & ref mutations
				"create_commit",
				"create_branch", "delete_branch", "create_tag", "delete_tag",
				// Release mutations
				"create_release", "update_release", "delete_release",
				// Repository management
				"create_repo", "update_repo", "create_ruleset",
				"enable_vulnerability_alerts", "enable_automated_security_fixes",
				// Label mutations
				"create_label", "update_label", "delete_label",
				"add_labels_to_issue", "remove_label_from_issue",
				// Milestone mutations
				"create_milestone", "update_milestone", "delete_milestone",
				// Reaction mutations
				"add_reaction", "delete_reaction",
				// Collaborator mutations
				"add_collaborator", "remove_collaborator",
				// Webhook mutations
				"create_webhook", "update_webhook", "delete_webhook",
				// GitHub Actions mutations
				"dispatch_workflow", "cancel_workflow_run", "rerun_workflow",
				"create_or_update_variable", "delete_variable",
				"create_or_update_environment", "delete_environment",
				// Repository settings mutations
				"replace_topics",
				"fork_repo", "create_from_template",
				// Custom properties mutations
				"set_custom_properties",
			},
			SensitiveFields: []string{"webhook_secret"},
			Schema:          buildInputSchema(),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityState: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
					"success": schemahelper.BoolProp("Whether the state operation succeeded"),
					"data":    schemahelper.AnyProp("The loaded state data (for state_load operation)"),
				}),
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.AnyProp("The API response data — structure varies by operation"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.AnyProp("The API response data — structure varies by operation"),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
					"success":   schemahelper.BoolProp("Whether the operation succeeded"),
					"result":    schemahelper.AnyProp("The API response data — structure varies by operation"),
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
				{
					Name:        "Create a repository",
					Description: "Create a new GitHub repository with auto-init",
					YAML: `operation: create_repo
owner: my-org
repo: my-new-repo
description: "A new project"
visibility: private
auto_init: true`,
				},
				{
					Name:        "Create branch ruleset",
					Description: "Configure branch protection via repository rulesets",
					YAML: `operation: create_ruleset
owner: my-org
repo: my-repo
ruleset_name: main branch protection
target: branch
enforcement: active
include_refs: ["refs/heads/main"]
required_status_checks_contexts: [test, lint]
required_approving_review_count: 1
required_linear_history: true
requires_commit_signatures: true
allow_force_pushes: false
allow_deletions: false`,
				},
				{
					Name:        "Create tag ruleset",
					Description: "Protect version tags from deletion and force pushes",
					YAML: `operation: create_ruleset
owner: my-org
repo: my-repo
ruleset_name: version tag protection
target: tag
enforcement: active
include_refs: ["refs/tags/v*"]
allow_deletions: false
allow_force_pushes: false`,
				},
				{
					Name:        "List PR review threads",
					Description: "Fetch all review threads for a pull request",
					YAML: `operation: list_review_threads
owner: my-org
repo: my-repo
number: 42`,
				},
				{
					Name:        "Reply to a review thread",
					Description: "Post a reply to a PR review thread",
					YAML: `operation: reply_to_review_thread
owner: my-org
repo: my-repo
thread_id: "PRT_kwDOABC123"
body: "Fixed, thanks!"`,
				},
				{
					Name:        "Check CI status",
					Description: "List check runs for a commit or branch",
					YAML: `operation: list_check_runs
owner: my-org
repo: my-repo
ref: main`,
				},
				{
					Name:        "Get workflow run details",
					Description: "Fetch a workflow run with job details (replaces gh run view)",
					YAML: `operation: get_workflow_run
owner: my-org
repo: my-repo
run_id: 12345678`,
				},
				{
					Name:        "Generic API call",
					Description: "Call any GitHub REST endpoint using the authenticated client",
					YAML: `operation: api_call
api_base: https://api.github.com
endpoint: /repos/my-org/my-repo/labels
method: POST
request_body:
  name: help-wanted
  color: "00ff00"
  description: "Extra attention is needed"`,
				},
				{
					Name:        "Create a label",
					Description: "Create a repository label with color",
					YAML: `operation: create_label
owner: my-org
repo: my-repo
label_name: bug
color: d73a4a
label_description: "Something isn't working"`,
				},
				{
					Name:        "Add reaction to issue",
					Description: "React to an issue with a thumbs up",
					YAML: `operation: add_reaction
owner: my-org
repo: my-repo
number: 42
reaction_content: "+1"`,
				},
				{
					Name:        "Dispatch a workflow",
					Description: "Trigger a GitHub Actions workflow",
					YAML: `operation: dispatch_workflow
owner: my-org
repo: my-repo
workflow_id: ci.yml
ref: main
workflow_inputs:
  environment: production`,
				},
				{
					Name:        "Create a webhook",
					Description: "Set up a repository webhook",
					YAML: `operation: create_webhook
owner: my-org
repo: my-repo
webhook_url: https://example.com/webhook
webhook_events:
  - push
  - pull_request
webhook_secret: my-secret`,
				},
				{
					Name:        "Update repository settings",
					Description: "Configure repository features and merge settings",
					YAML: `operation: update_repo
owner: my-org
repo: my-repo
description: "Updated project description"
has_wiki: false
delete_branch_on_merge: true
allow_squash_merge: true`,
				},
				{
					Name:        "Fork a repository",
					Description: "Fork a repository into an organization",
					YAML: `operation: fork_repo
owner: upstream-org
repo: upstream-repo
organization: my-org
default_branch_only: true`,
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
		[]string{"operation"},
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
			"commit_sha": schemahelper.StringProp("Commit SHA for list_commit_pulls operation (alias: sha)",
				schemahelper.WithExample("abc123def456"),
				schemahelper.WithMaxLength(*ptrs.IntPtr(40)),
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
			"state_reason": schemahelper.StringProp("Reason for closing an issue (for update_issue with state=closed)",
				schemahelper.WithEnum("completed", "not_planned", "reopened"),
			),

			// --- Review thread fields ---
			"thread_id": schemahelper.StringProp("Review thread node ID (for reply_to_review_thread and resolve_review_thread operations)"),

			// --- CI/CD fields ---
			"run_id": schemahelper.IntProp("Workflow run ID for get_workflow_run",
				schemahelper.WithMinimum(1),
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

			// --- Repo management fields (GraphQL + REST) ---
			"description": schemahelper.StringProp("Repository or resource description",
				schemahelper.WithMaxLength(*ptrs.IntPtr(1000)),
			),
			"visibility": schemahelper.StringProp("Repository visibility for create_repo",
				schemahelper.WithEnum("public", "private"),
				schemahelper.WithDefault("public"),
			),
			"auto_init": schemahelper.BoolProp("Initialize repo with a README (uses REST API; GraphQL lacks auto_init support)"),
			"ruleset_name": schemahelper.StringProp("Name for the repository ruleset",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"target": schemahelper.StringProp("Ruleset target type",
				schemahelper.WithEnum("branch", "tag"),
				schemahelper.WithDefault("branch"),
			),
			"enforcement": schemahelper.StringProp("Ruleset enforcement level",
				schemahelper.WithEnum("active", "disabled", "evaluate"),
				schemahelper.WithDefault("active"),
			),
			"include_refs": schemahelper.ArrayProp("Ref patterns to include (e.g. refs/heads/main, refs/tags/v*)",
				schemahelper.WithItems(schemahelper.StringProp("Ref pattern")),
				schemahelper.WithMaxItems(100),
			),
			"exclude_refs": schemahelper.ArrayProp("Ref patterns to exclude",
				schemahelper.WithItems(schemahelper.StringProp("Ref pattern")),
				schemahelper.WithMaxItems(100),
			),
			"required_status_checks_contexts": schemahelper.ArrayProp("Required status check context names",
				schemahelper.WithItems(schemahelper.StringProp("Context name")),
				schemahelper.WithMaxItems(50),
			),
			"required_approving_review_count": schemahelper.IntProp("Minimum approving reviews required",
				schemahelper.WithMinimum(0),
				schemahelper.WithMaximum(10),
			),
			"required_linear_history":    schemahelper.BoolProp("Require linear commit history"),
			"allow_force_pushes":         schemahelper.BoolProp("Allow force pushes to matching refs"),
			"allow_deletions":            schemahelper.BoolProp("Allow deletion of matching refs"),
			"requires_commit_signatures": schemahelper.BoolProp("Require signed commits"),

			// --- Label fields ---
			"label_name": schemahelper.StringProp("Label name for create/update/delete label operations",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"new_label_name": schemahelper.StringProp("New name when renaming a label (update_label)",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"color": schemahelper.StringProp("Hex color code for labels (without #)",
				schemahelper.WithPattern("^[0-9a-fA-F]{6}$"),
				schemahelper.WithExample("00ff00"),
			),
			"label_description": schemahelper.StringProp("Description for a label",
				schemahelper.WithMaxLength(*ptrs.IntPtr(1000)),
			),

			// --- Milestone fields ---
			"milestone_number": schemahelper.IntProp("Milestone number for update/delete operations",
				schemahelper.WithMinimum(1),
			),
			"due_on": schemahelper.StringProp("Due date for milestone (ISO 8601: YYYY-MM-DDTHH:MM:SSZ)",
				schemahelper.WithFormat("date-time"),
			),

			// --- Reaction fields ---
			"reaction_content": schemahelper.StringProp("Reaction emoji to add",
				schemahelper.WithEnum("+1", "-1", "laugh", "confused", "heart", "hooray", "rocket", "eyes"),
			),
			"reaction_subject": schemahelper.StringProp("Subject type for the reaction",
				schemahelper.WithEnum("issue", "pull_request", "issue_comment"),
				schemahelper.WithDefault("issue"),
			),
			"reaction_id": schemahelper.IntProp("Reaction ID for delete_reaction",
				schemahelper.WithMinimum(1),
			),
			"comment_id": schemahelper.IntProp("Comment ID for reaction/comment operations",
				schemahelper.WithMinimum(1),
			),

			// --- Collaborator fields ---
			"username": schemahelper.StringProp("GitHub username for collaborator operations",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"permission": schemahelper.StringProp("Permission level for collaborators",
				schemahelper.WithEnum("pull", "triage", "push", "maintain", "admin"),
			),

			// --- Webhook fields ---
			"webhook_url": schemahelper.StringProp("Payload URL for the webhook",
				schemahelper.WithFormat("uri"),
			),
			"webhook_events": schemahelper.ArrayProp("Events that trigger the webhook",
				schemahelper.WithItems(schemahelper.StringProp("Event name")),
				schemahelper.WithMaxItems(50),
			),
			"webhook_content_type": schemahelper.StringProp("Content type for webhook payloads",
				schemahelper.WithEnum("json", "form"),
				schemahelper.WithDefault("json"),
			),
			"webhook_secret": schemahelper.StringProp("Secret for webhook signature verification"),
			"webhook_active": schemahelper.BoolProp("Whether the webhook is active"),
			"hook_id": schemahelper.IntProp("Webhook ID for update/delete operations",
				schemahelper.WithMinimum(1),
			),

			// --- GitHub Actions fields ---
			"workflow_id": schemahelper.StringProp("Workflow file name or ID (e.g. ci.yml or numeric ID)",
				schemahelper.WithExample("ci.yml"),
			),
			"workflow_inputs": schemahelper.ObjectProp("Input parameters for workflow dispatch",
				nil, nil,
			),
			"workflow_status": schemahelper.StringProp("Filter workflow runs by status",
				schemahelper.WithEnum("completed", "action_required", "cancelled", "failure", "neutral",
					"skipped", "stale", "success", "timed_out", "in_progress", "queued", "requested", "waiting"),
			),

			// --- Variable fields ---
			"variable_name": schemahelper.StringProp("Repository variable name",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"variable_value": schemahelper.StringProp("Repository variable value",
				schemahelper.WithMaxLength(*ptrs.IntPtr(65536)),
			),

			// --- Environment fields ---
			"environment_name": schemahelper.StringProp("Deployment environment name",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"wait_timer": schemahelper.IntProp("Wait timer in minutes before deployments proceed",
				schemahelper.WithMinimum(0),
				schemahelper.WithMaximum(43200),
			),
			"reviewers": schemahelper.ArrayProp("Required reviewers for environment (objects with 'type' and 'id', e.g. [{\"type\": \"User\", \"id\": 123}])",
				schemahelper.WithItems(schemahelper.ObjectProp("Reviewer object", nil, nil)),
				schemahelper.WithMaxItems(6),
			),

			// --- Repo settings fields ---
			"homepage": schemahelper.StringProp("Repository homepage URL",
				schemahelper.WithFormat("uri"),
			),
			"default_branch": schemahelper.StringProp("Default branch name",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"has_issues":             schemahelper.BoolProp("Enable issues feature"),
			"has_projects":           schemahelper.BoolProp("Enable projects feature"),
			"has_wiki":               schemahelper.BoolProp("Enable wiki feature"),
			"allow_squash_merge":     schemahelper.BoolProp("Allow squash merging"),
			"allow_merge_commit":     schemahelper.BoolProp("Allow merge commits"),
			"allow_rebase_merge":     schemahelper.BoolProp("Allow rebase merging"),
			"delete_branch_on_merge": schemahelper.BoolProp("Automatically delete head branches after merge"),
			"archived":               schemahelper.BoolProp("Archive the repository"),
			"topics": schemahelper.ArrayProp("Repository topics/tags",
				schemahelper.WithItems(schemahelper.StringProp("Topic name")),
				schemahelper.WithMaxItems(20),
			),
			"organization": schemahelper.StringProp("Organization to fork into (fork_repo)",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"default_branch_only": schemahelper.BoolProp("Fork only the default branch"),
			"new_repo_name": schemahelper.StringProp("Name for new repository (create_from_template)",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"new_owner": schemahelper.StringProp("Owner for new repository (create_from_template)",
				schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
			),
			"include_all_branches": schemahelper.BoolProp("Include all branches when creating from template"),

			// --- Custom properties fields ---
			"properties": schemahelper.ObjectProp("Custom properties to set (key-value map, e.g. {\"env\": \"prod\", \"team\": \"platform\"})",
				nil, nil,
			),

			// --- Generic API call fields ---
			"endpoint": schemahelper.StringProp("Relative API path for api_call (e.g. /repos/{owner}/{repo}/labels). Must start with /.",
				schemahelper.WithExample("/repos/octocat/hello-world/labels"),
				schemahelper.WithMaxLength(*ptrs.IntPtr(2000)),
			),
			"method": schemahelper.StringProp("HTTP method for api_call",
				schemahelper.WithEnum("GET", "POST", "PUT", "PATCH", "DELETE"),
				schemahelper.WithDefault("GET"),
			),
			"query_params": schemahelper.ObjectProp("Query parameters for api_call",
				nil, nil,
			),
			"request_body": schemahelper.ObjectProp("Request body (JSON object) for api_call POST/PUT/PATCH",
				nil, nil,
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

	if operation == "" {
		return nil, fmt.Errorf("%s: 'operation' is required", ProviderName)
	}

	// Dry-run support: return mock data for write operations
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		if strings.HasPrefix(operation, "state_") {
			return p.executeStateDryRun(operation)
		}
		return p.executeDryRun(operation, inputs)
	}

	owner, _ := inputs["owner"].(string)
	repo, _ := inputs["repo"].(string)
	apiBase, _ := inputs["api_base"].(string)
	if apiBase == "" {
		apiBase = defaultAPIBase
	}
	apiBase = strings.TrimRight(apiBase, "/")

	// Most operations require owner and repo. Only create_repo and api_call handle
	// these fields internally (owner is optional, repo validated inside).
	if operation != "create_repo" && operation != "api_call" && (owner == "" || repo == "") {
		return nil, fmt.Errorf("%s: 'owner' and 'repo' are required for %s operation", ProviderName, operation)
	}

	client := p.getClient(ctx)

	// State operations use dedicated dispatch
	if strings.HasPrefix(operation, "state_") {
		return p.dispatchStateOperation(ctx, client, apiBase, owner, repo, inputs)
	}

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
	case "list_pr_comments":
		result, err = p.executeListPRComments(ctx, client, apiBase, owner, repo, inputs)
	case "list_branches":
		result, err = p.executeListBranches(ctx, client, apiBase, owner, repo, inputs)
	case "get_branch":
		result, err = p.executeGetBranch(ctx, client, apiBase, owner, repo, inputs)
	case "list_tags":
		result, err = p.executeListTags(ctx, client, apiBase, owner, repo, inputs)
	case "get_head_oid":
		result, err = p.executeGetHeadOID(ctx, client, apiBase, owner, repo, inputs)

	// --- Review thread operations (GraphQL) ---
	case "list_review_threads":
		result, err = p.executeListReviewThreads(ctx, client, apiBase, owner, repo, inputs)
	case "reply_to_review_thread":
		result, err = p.executeReplyToReviewThread(ctx, client, apiBase, owner, repo, inputs)
	case "resolve_review_thread":
		result, err = p.executeResolveReviewThread(ctx, client, apiBase, owner, repo, inputs)

	// --- CI/CD check operations (REST) ---
	case "list_check_runs":
		result, err = p.executeListCheckRuns(ctx, client, apiBase, owner, repo, inputs)
	case "get_workflow_run":
		result, err = p.executeGetWorkflowRun(ctx, client, apiBase, owner, repo, inputs)

	// --- Commit lookup operations (REST) ---
	case "list_commit_pulls":
		result, err = p.executeListCommitPulls(ctx, client, apiBase, owner, repo, inputs)

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

	// --- Repository management operations (GraphQL + REST) ---
	case "create_repo":
		result, err = p.executeCreateRepo(ctx, client, apiBase, inputs)
	case "update_repo":
		result, err = p.executeUpdateRepo(ctx, client, apiBase, owner, repo, inputs)
	case "create_ruleset":
		result, err = p.executeCreateRuleset(ctx, client, apiBase, owner, repo, inputs)
	case "enable_vulnerability_alerts":
		result, err = p.executeEnableVulnerabilityAlerts(ctx, client, apiBase, owner, repo)
	case "enable_automated_security_fixes":
		result, err = p.executeEnableAutomatedSecurityFixes(ctx, client, apiBase, owner, repo)

	// --- Label operations (REST) ---
	case "list_labels":
		result, err = p.executeListLabels(ctx, client, apiBase, owner, repo, inputs)
	case "create_label":
		result, err = p.executeCreateLabel(ctx, client, apiBase, owner, repo, inputs)
	case "update_label":
		result, err = p.executeUpdateLabel(ctx, client, apiBase, owner, repo, inputs)
	case "delete_label":
		result, err = p.executeDeleteLabel(ctx, client, apiBase, owner, repo, inputs)
	case "add_labels_to_issue":
		result, err = p.executeAddLabelsToIssue(ctx, client, apiBase, owner, repo, inputs)
	case "remove_label_from_issue":
		result, err = p.executeRemoveLabelFromIssue(ctx, client, apiBase, owner, repo, inputs)

	// --- Milestone operations (REST) ---
	case "list_milestones":
		result, err = p.executeListMilestones(ctx, client, apiBase, owner, repo, inputs)
	case "create_milestone":
		result, err = p.executeCreateMilestone(ctx, client, apiBase, owner, repo, inputs)
	case "update_milestone":
		result, err = p.executeUpdateMilestone(ctx, client, apiBase, owner, repo, inputs)
	case "delete_milestone":
		result, err = p.executeDeleteMilestone(ctx, client, apiBase, owner, repo, inputs)

	// --- Reaction operations (REST) ---
	case "add_reaction":
		result, err = p.executeAddReaction(ctx, client, apiBase, owner, repo, inputs)
	case "list_reactions":
		result, err = p.executeListReactions(ctx, client, apiBase, owner, repo, inputs)
	case "delete_reaction":
		result, err = p.executeDeleteReaction(ctx, client, apiBase, owner, repo, inputs)

	// --- Collaborator operations (REST) ---
	case "list_collaborators":
		result, err = p.executeListCollaborators(ctx, client, apiBase, owner, repo, inputs)
	case "add_collaborator":
		result, err = p.executeAddCollaborator(ctx, client, apiBase, owner, repo, inputs)
	case "remove_collaborator":
		result, err = p.executeRemoveCollaborator(ctx, client, apiBase, owner, repo, inputs)

	// --- Webhook operations (REST) ---
	case "list_webhooks":
		result, err = p.executeListWebhooks(ctx, client, apiBase, owner, repo, inputs)
	case "create_webhook":
		result, err = p.executeCreateWebhook(ctx, client, apiBase, owner, repo, inputs)
	case "update_webhook":
		result, err = p.executeUpdateWebhook(ctx, client, apiBase, owner, repo, inputs)
	case "delete_webhook":
		result, err = p.executeDeleteWebhook(ctx, client, apiBase, owner, repo, inputs)

	// --- GitHub Actions operations (REST) ---
	case "dispatch_workflow":
		result, err = p.executeDispatchWorkflow(ctx, client, apiBase, owner, repo, inputs)
	case "list_workflow_runs":
		result, err = p.executeListWorkflowRuns(ctx, client, apiBase, owner, repo, inputs)
	case "cancel_workflow_run":
		result, err = p.executeCancelWorkflowRun(ctx, client, apiBase, owner, repo, inputs)
	case "rerun_workflow":
		result, err = p.executeRerunWorkflow(ctx, client, apiBase, owner, repo, inputs)
	case "list_repo_variables":
		result, err = p.executeListRepoVariables(ctx, client, apiBase, owner, repo, inputs)
	case "create_or_update_variable":
		result, err = p.executeCreateOrUpdateVariable(ctx, client, apiBase, owner, repo, inputs)
	case "delete_variable":
		result, err = p.executeDeleteVariable(ctx, client, apiBase, owner, repo, inputs)
	case "list_environments":
		result, err = p.executeListEnvironments(ctx, client, apiBase, owner, repo, inputs)
	case "create_or_update_environment":
		result, err = p.executeCreateOrUpdateEnvironment(ctx, client, apiBase, owner, repo, inputs)
	case "delete_environment":
		result, err = p.executeDeleteEnvironment(ctx, client, apiBase, owner, repo, inputs)

	// --- Repo settings (REST) ---
	case "list_topics":
		result, err = p.executeListTopics(ctx, client, apiBase, owner, repo)
	case "replace_topics":
		result, err = p.executeReplaceTopics(ctx, client, apiBase, owner, repo, inputs)
	case "fork_repo":
		result, err = p.executeForkRepo(ctx, client, apiBase, owner, repo, inputs)
	case "create_from_template":
		result, err = p.executeCreateFromTemplate(ctx, client, apiBase, owner, repo, inputs)

	// --- Custom properties (REST) ---
	case "list_custom_properties":
		result, err = p.executeListCustomProperties(ctx, client, apiBase, owner, repo)
	case "set_custom_properties":
		result, err = p.executeSetCustomProperties(ctx, client, apiBase, owner, repo, inputs)

	// --- Generic API call (REST) ---
	case "api_call":
		result, err = p.executeAPICall(ctx, client, apiBase, inputs)

	default:
		return nil, fmt.Errorf("%s: unknown operation %q — supported: %s", ProviderName, operation, strings.Join(allOperations, ", "))
	}

	if err != nil {
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

// getStringInputWithAliases extracts a string from the input map, trying the
// primary key first then falling back to aliases in order.
func getStringInputWithAliases(inputs map[string]any, key string, aliases ...string) string {
	if v := getStringInput(inputs, key); v != "" {
		return v
	}
	for _, alias := range aliases {
		if v := getStringInput(inputs, alias); v != "" {
			return v
		}
	}
	return ""
}

// commonInputKeys are top-level inputs handled by Execute before dispatch.
// They are excluded from the "received inputs" list in error messages.
var commonInputKeys = []string{"operation", "owner", "repo", "api_base", "token"}

// requiredInputError builds an error for a missing required input, listing the
// operation-specific keys the caller provided so the user can spot typos.
func requiredInputError(operation, field string, inputs map[string]any, hint string) error {
	var userKeys []string
	for k := range inputs {
		if !slices.Contains(commonInputKeys, k) {
			userKeys = append(userKeys, k)
		}
	}
	sort.Strings(userKeys)

	msg := fmt.Sprintf("'%s' is required for %s operation", field, operation)
	if len(userKeys) > 0 {
		msg += fmt.Sprintf(" (received inputs: %s)", strings.Join(userKeys, ", "))
	}
	if hint != "" {
		msg += "; " + hint
	}
	return fmt.Errorf("%s", msg)
}

// getMapInput extracts a map[string]any from the input map.
func getMapInput(inputs map[string]any, key string) map[string]any {
	v, ok := inputs[key].(map[string]any)
	if !ok {
		return nil
	}
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

// restError is returned when the GitHub REST API responds with an HTTP error status.
type restError struct {
	StatusCode int
	Message    string
}

// Error implements the error interface.
func (e *restError) Error() string {
	return fmt.Sprintf("GitHub API error (HTTP %d): %s", e.StatusCode, e.Message)
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
		msg := string(respBody)
		var ghErr map[string]any
		if json.Unmarshal(respBody, &ghErr) == nil {
			if m, ok := ghErr["message"].(string); ok {
				msg = m
			}
		}
		return nil, &restError{StatusCode: resp.StatusCode, Message: msg}
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
