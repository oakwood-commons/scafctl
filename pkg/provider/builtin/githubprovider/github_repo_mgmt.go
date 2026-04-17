// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// Repository management operations use the GraphQL API where mutations are
// available (create_repo) and the REST API for repository rulesets and
// security features (no GraphQL mutations).

// ─── Create Repository (GraphQL) ─────────────────────────────────────────────

func (p *GitHubProvider) executeCreateRepo(ctx context.Context, client *httpc.Client, apiBase string, inputs map[string]any) (*provider.Output, error) {
	name := getStringInput(inputs, "repo")
	if name == "" {
		return nil, fmt.Errorf("'repo' is required for create_repo operation")
	}

	owner := getStringInput(inputs, "owner")
	autoInit, _ := getBoolInput(inputs, "auto_init")

	// Try GraphQL first (lower permission requirement for most accounts).
	// Enterprise Managed Users (EMU) cannot use GraphQL mutations, so fall
	// back to REST if we get a FORBIDDEN error.
	output, err := p.executeCreateRepoGraphQL(ctx, client, apiBase, inputs, name)
	if err != nil && isGraphQLForbidden(err) {
		output, err = p.executeCreateRepoREST(ctx, client, apiBase, inputs, name, autoInit)
		if err != nil {
			return nil, err
		}
		// Derive the canonical owner from the REST response — /user/repos
		// creates the repo under the authenticated user, which may differ
		// from the caller-provided owner.
		restOwner := extractRESTOwner(output)
		if restOwner == "" {
			restOwner = owner
		}
		// Wait for GraphQL write access before returning — downstream
		// operations (create_commit) need it.
		if waitErr := p.waitForWriteAccess(ctx, client, apiBase, restOwner, name); waitErr != nil {
			return nil, waitErr
		}
		return output, nil
	}
	if err != nil {
		return nil, err
	}

	// Extract the resolved nameWithOwner from the GraphQL response — this
	// is the canonical "owner/repo" regardless of what the caller passed.
	nwo := extractNameWithOwner(output)
	if nwo == "" {
		return nil, fmt.Errorf("GraphQL createRepository response missing nameWithOwner")
	}
	resolvedOwner, _, _ := strings.Cut(nwo, "/")

	// GraphQL createRepository doesn't support auto_init, so create an
	// initial README via the Contents API to establish the default branch.
	if autoInit {
		if initErr := p.initRepoWithReadme(ctx, client, apiBase, nwo); initErr != nil {
			return nil, fmt.Errorf("initializing repository with README: %w", initErr)
		}
	}

	// Wait for GraphQL write access before returning — downstream
	// operations (create_commit) need it.
	if waitErr := p.waitForWriteAccess(ctx, client, apiBase, resolvedOwner, name); waitErr != nil {
		return nil, waitErr
	}

	return output, nil
}

// extractRESTOwner extracts the repository owner from a REST API create_repo
// response by parsing the "full_name" field ("owner/repo").
func extractRESTOwner(output *provider.Output) string {
	if output == nil {
		return ""
	}
	dataMap, ok := output.Data.(map[string]any)
	if !ok {
		return ""
	}
	resultData, ok := dataMap["result"].(map[string]any)
	if !ok {
		return ""
	}
	fullName, _ := resultData["full_name"].(string)
	if owner, _, ok := strings.Cut(fullName, "/"); ok {
		return owner
	}
	return ""
}

// extractNameWithOwner extracts the "owner/repo" string from a create_repo output.
func extractNameWithOwner(output *provider.Output) string {
	if output == nil {
		return ""
	}
	dataMap, ok := output.Data.(map[string]any)
	if !ok {
		return ""
	}
	resultData, ok := dataMap["result"].(map[string]any)
	if !ok {
		return ""
	}
	v, _ := resultData["nameWithOwner"].(string)
	return v
}

// isGraphQLForbidden checks whether an error is a GraphQL FORBIDDEN error
// (e.g., Enterprise Managed User restrictions).
func isGraphQLForbidden(err error) bool {
	var gqlErr *GraphQLError
	if errors.As(err, &gqlErr) {
		for _, e := range gqlErr.Errors {
			if e.Type == "FORBIDDEN" {
				return true
			}
		}
	}
	return false
}

// waitForWriteAccess polls the repository's viewerPermission via GraphQL until
// the authenticated user has WRITE, MAINTAIN, or ADMIN access. This handles the
// eventual consistency window after repo creation (especially for EMU/org repos)
// where GraphQL read access propagates before write access.
func (p *GitHubProvider) waitForWriteAccess(ctx context.Context, client *httpc.Client, apiBase, owner, repo string) error {
	lgr := logger.FromContext(ctx)

	query := `query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    viewerPermission
  }
}`
	vars := map[string]any{"owner": owner, "name": repo}

	for attempt := range p.waitMaxAttempts {
		if attempt > 0 {
			timer := time.NewTimer(p.waitPollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		data, err := graphqlDo(ctx, client, apiBase, query, vars)
		if err != nil {
			lgr.V(1).Info("waitForWriteAccess: GraphQL query failed", "attempt", attempt+1, "error", err)
			continue
		}

		permNode, _ := extractNode(data, "repository.viewerPermission")
		perm, _ := permNode.(string)
		lgr.V(1).Info("waitForWriteAccess: checked permission", "attempt", attempt+1, "viewerPermission", perm)
		if perm == "ADMIN" || perm == "WRITE" || perm == "MAINTAIN" {
			return nil
		}
	}
	return fmt.Errorf("timed out waiting for write access to %s/%s via GraphQL after %d attempts", owner, repo, p.waitMaxAttempts)
}

// executeCreateRepoGraphQL creates a repository using the GraphQL createRepository mutation.
func (p *GitHubProvider) executeCreateRepoGraphQL(ctx context.Context, client *httpc.Client, apiBase string, inputs map[string]any, name string) (*provider.Output, error) {
	mutInput := map[string]any{
		"name":       name,
		"visibility": "PUBLIC",
	}

	if desc := getStringInput(inputs, "description"); desc != "" {
		mutInput["description"] = desc
	}

	if strings.EqualFold(getStringInput(inputs, "visibility"), "private") {
		mutInput["visibility"] = "PRIVATE"
	}

	// If owner is specified, resolve its node ID and set ownerId on the mutation input.
	// The repositoryOwner GraphQL field resolves both users and organizations.
	owner := getStringInput(inputs, "owner")
	if owner != "" {
		orgID, err := p.resolveOwnerID(ctx, client, apiBase, owner)
		if err != nil {
			return nil, fmt.Errorf("resolving owner ID for %q: %w", owner, err)
		}
		mutInput["ownerId"] = orgID
	}

	mutation := `mutation($input: CreateRepositoryInput!) {
  createRepository(input: $input) {
    repository {
      id
      name
      nameWithOwner
      url
      isPrivate
      defaultBranchRef { name }
      createdAt
    }
  }
}`

	data, err := graphqlDo(ctx, client, apiBase, mutation, map[string]any{"input": mutInput})
	if err != nil {
		return nil, err
	}

	repoNode, err := extractNodeMap(data, "createRepository.repository")
	if err != nil {
		return nil, err
	}

	return actionOutput("create_repo", repoNode), nil
}

// initRepoWithReadme creates an initial README.md via the Contents API to
// establish the default branch on an empty repository. This only requires
// `repo` scope, unlike POST /orgs/{org}/repos which requires org admin.
// nameWithOwner is the full "owner/repo" path (e.g. "oakwood-commons/my-repo").
func (p *GitHubProvider) initRepoWithReadme(ctx context.Context, client *httpc.Client, apiBase, nameWithOwner string) error {
	repoName := nameWithOwner
	if idx := strings.LastIndex(nameWithOwner, "/"); idx >= 0 {
		repoName = nameWithOwner[idx+1:]
	}

	content := base64.StdEncoding.EncodeToString([]byte("# " + repoName + "\n"))
	url := fmt.Sprintf("%s/repos/%s/contents/%s", apiBase, nameWithOwner, "README.md")
	reqBody := map[string]any{
		"message": "Initial commit",
		"content": content,
	}

	// The repository was just created — the REST API may not see it
	// immediately due to eventual consistency. Retry on 404 only.
	var lastErr error
	for attempt := range p.initRepoMaxRetries {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * p.initRepoRetryBackoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		_, lastErr = p.doRESTRequest(ctx, client, http.MethodPut, url, reqBody)
		if lastErr == nil {
			return nil
		}
		// Only retry on 404 (eventual consistency); fail immediately on other errors
		var restErr *restError
		if !errors.As(lastErr, &restErr) || restErr.StatusCode != http.StatusNotFound {
			return lastErr
		}
	}
	return lastErr
}

// executeCreateRepoREST creates a repository using the REST API.
// Used as fallback when GraphQL is forbidden (e.g., Enterprise Managed Users).
// Tries the org endpoint first; falls back to POST /user/repos on 404 (when
// the owner is a user account, not an organization).
func (p *GitHubProvider) executeCreateRepoREST(ctx context.Context, client *httpc.Client, apiBase string, inputs map[string]any, name string, autoInit bool) (*provider.Output, error) {
	owner := getStringInput(inputs, "owner")
	if owner == "" {
		return nil, fmt.Errorf("'owner' is required for REST repo creation (GraphQL createRepository was forbidden)")
	}

	reqBody := map[string]any{
		"name":      name,
		"auto_init": autoInit,
	}

	if desc := getStringInput(inputs, "description"); desc != "" {
		reqBody["description"] = desc
	}

	reqBody["private"] = strings.EqualFold(getStringInput(inputs, "visibility"), "private")

	// Try org endpoint first.
	orgURL := fmt.Sprintf("%s/orgs/%s/repos", apiBase, owner)
	result, err := p.doRESTRequest(ctx, client, http.MethodPost, orgURL, reqBody)
	if err != nil {
		// If 404, the owner is a user account — fall back to /user/repos.
		var restErr *restError
		if errors.As(err, &restErr) && restErr.StatusCode == http.StatusNotFound {
			userURL := fmt.Sprintf("%s/user/repos", apiBase)
			result, err = p.doRESTRequest(ctx, client, http.MethodPost, userURL, reqBody)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return actionOutput("create_repo", result), nil
}

// resolveOwnerID fetches the GraphQL node ID for a user or organization by login.
func (p *GitHubProvider) resolveOwnerID(ctx context.Context, client *httpc.Client, apiBase, login string) (string, error) {
	query := `query($login: String!) {
  repositoryOwner(login: $login) { id }
}`
	data, err := graphqlDo(ctx, client, apiBase, query, map[string]any{"login": login})
	if err != nil {
		return "", err
	}
	ownerNode, err := extractNodeMap(data, "repositoryOwner")
	if err != nil {
		return "", fmt.Errorf("owner %q not found: %w", login, err)
	}
	id, ok := ownerNode["id"].(string)
	if !ok {
		return "", fmt.Errorf("owner ID not found for %q", login)
	}
	return id, nil
}

// ─── Create Ruleset (REST) ───────────────────────────────────────────────────
//
// Repository rulesets replace legacy branch protection rules and tag protection.
// They support branch and tag targets with flexible rule composition.
// The REST API is used because the GraphQL mutations for rulesets have complex
// nested union input types that don't map cleanly to a flat input schema.

func (p *GitHubProvider) executeCreateRuleset(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	rulesetName := getStringInput(inputs, "ruleset_name")
	if rulesetName == "" {
		return nil, fmt.Errorf("'ruleset_name' is required for create_ruleset operation")
	}

	target := getStringInput(inputs, "target")
	if target == "" {
		target = "branch"
	}

	enforcement := getStringInput(inputs, "enforcement")
	if enforcement == "" {
		enforcement = "active"
	}

	// Build conditions from include/exclude ref patterns
	includeRefs := getStringSliceInput(inputs, "include_refs")
	excludeRefs := getStringSliceInput(inputs, "exclude_refs")
	if len(includeRefs) == 0 {
		return nil, fmt.Errorf("'include_refs' is required for create_ruleset operation (e.g. [\"refs/heads/main\"])")
	}

	// Ensure exclude is never null (API requires an array)
	if excludeRefs == nil {
		excludeRefs = []string{}
	}

	conditions := map[string]any{
		"ref_name": map[string]any{
			"include": includeRefs,
			"exclude": excludeRefs,
		},
	}

	// Build rules from individual boolean/parameter fields
	rules := buildRulesetRules(inputs)

	reqBody := map[string]any{
		"name":        rulesetName,
		"target":      target,
		"enforcement": enforcement,
		"conditions":  conditions,
		"rules":       rules,
	}

	url := fmt.Sprintf("%s/repos/%s/%s/rulesets", apiBase, owner, repo)
	result, err := p.doRESTRequest(ctx, client, http.MethodPost, url, reqBody)
	if err != nil {
		return nil, err
	}

	return actionOutput("create_ruleset", result), nil
}

// buildRulesetRules converts flat input fields into the GitHub Rulesets rules array.
func buildRulesetRules(inputs map[string]any) []map[string]any {
	rules := make([]map[string]any, 0)

	// Required status checks
	if contexts := getStringSliceInput(inputs, "required_status_checks_contexts"); len(contexts) > 0 {
		checks := make([]map[string]any, 0, len(contexts))
		for _, name := range contexts {
			checks = append(checks, map[string]any{"context": name})
		}
		rules = append(rules, map[string]any{
			"type": "required_status_checks",
			"parameters": map[string]any{
				"required_status_checks":               checks,
				"strict_required_status_checks_policy": true,
			},
		})
	}

	// Pull request reviews
	if approvals, ok := getIntInput(inputs, "required_approving_review_count"); ok && approvals > 0 {
		rules = append(rules, map[string]any{
			"type": "pull_request",
			"parameters": map[string]any{
				"required_approving_review_count":   approvals,
				"dismiss_stale_reviews_on_push":     true,
				"require_code_owner_review":         false,
				"require_last_push_approval":        false,
				"required_review_thread_resolution": true,
			},
		})
	}

	// Required signatures
	if v, ok := getBoolInput(inputs, "requires_commit_signatures"); ok && v {
		rules = append(rules, map[string]any{
			"type": "required_signatures",
		})
	}

	// Required linear history
	if v, ok := getBoolInput(inputs, "required_linear_history"); ok && v {
		rules = append(rules, map[string]any{
			"type": "required_linear_history",
		})
	}

	// Prevent force pushes (non_fast_forward)
	if v, ok := getBoolInput(inputs, "allow_force_pushes"); ok && !v {
		rules = append(rules, map[string]any{
			"type": "non_fast_forward",
		})
	}

	// Prevent deletion
	if v, ok := getBoolInput(inputs, "allow_deletions"); ok && !v {
		rules = append(rules, map[string]any{
			"type": "deletion",
		})
	}

	return rules
}

// ─── Enable Vulnerability Alerts (REST) ──────────────────────────────────────
//
// Vulnerability alerts have no GraphQL mutation — REST only.

func (p *GitHubProvider) executeEnableVulnerabilityAlerts(ctx context.Context, client *httpc.Client, apiBase, owner, repo string) (*provider.Output, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/vulnerability-alerts", apiBase, owner, repo)
	_, err := p.doRESTRequest(ctx, client, http.MethodPut, url, nil)
	if err != nil {
		return nil, err
	}

	return actionOutput("enable_vulnerability_alerts", map[string]any{
		"enabled": true,
	}), nil
}

// ─── Enable Automated Security Fixes (REST) ──────────────────────────────────
//
// Automated security fixes have no GraphQL mutation — REST only.

func (p *GitHubProvider) executeEnableAutomatedSecurityFixes(ctx context.Context, client *httpc.Client, apiBase, owner, repo string) (*provider.Output, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/automated-security-fixes", apiBase, owner, repo)
	_, err := p.doRESTRequest(ctx, client, http.MethodPut, url, nil)
	if err != nil {
		return nil, err
	}

	return actionOutput("enable_automated_security_fixes", map[string]any{
		"enabled": true,
	}), nil
}
