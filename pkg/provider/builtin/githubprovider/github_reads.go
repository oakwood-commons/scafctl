// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── Repository ──────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeGetRepo(ctx context.Context, client *httpc.Client, apiBase, owner, repo string) (*provider.Output, error) {
	query := `query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    name
    nameWithOwner
    description
    url
    homepageUrl
    isPrivate
    isFork
    isArchived
    defaultBranchRef { name }
    stargazerCount
    forkCount
    primaryLanguage { name }
    licenseInfo { name spdxId }
    createdAt
    updatedAt
    pushedAt
    diskUsage
    owner { login url }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	repoNode, err := extractNodeMap(data, "repository")
	if err != nil {
		return nil, err
	}

	// Flatten defaultBranchRef for convenience
	if dbr, ok := repoNode["defaultBranchRef"].(map[string]any); ok {
		repoNode["default_branch"] = dbr["name"]
	}

	// Alias nameWithOwner → full_name for familiarity
	if now, ok := repoNode["nameWithOwner"]; ok {
		repoNode["full_name"] = now
	}

	return readOutput(repoNode), nil
}

// ─── File Content ────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeGetFile(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	path := getStringInput(inputs, "path")
	if path == "" {
		return nil, fmt.Errorf("'path' is required for get_file operation")
	}
	ref := getStringInput(inputs, "ref")

	// Build the expression: "branch:path" or "HEAD:path" for the default branch
	var expression string
	if ref != "" {
		expression = ref + ":" + path
	} else {
		expression = "HEAD:" + path
	}

	query := `query($owner: String!, $name: String!, $expression: String!) {
  repository(owner: $owner, name: $name) {
    object(expression: $expression) {
      ... on Blob {
        text
        byteSize
        oid
        isTruncated
      }
    }
  }
}`
	vars := map[string]any{
		"owner":      owner,
		"name":       repo,
		"expression": expression,
	}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	obj, err := extractNode(data, "repository.object")
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("file not found: %s", path)
	}

	blob, ok := obj.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("object at %s is not a file (blob)", path)
	}

	// Build a clean result
	result := map[string]any{
		"name":    pathBasename(path),
		"path":    path,
		"content": blob["text"],
		"size":    blob["byteSize"],
		"sha":     blob["oid"],
	}
	if truncated, ok := blob["isTruncated"].(bool); ok && truncated {
		result["truncated"] = true
	}

	return readOutput(result), nil
}

// ─── Releases ────────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListReleases(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)

	query := `query($owner: String!, $name: String!, $first: Int!) {
  repository(owner: $owner, name: $name) {
    releases(first: $first, orderBy: {field: CREATED_AT, direction: DESC}) {
      nodes {
        name
        tagName
        description
        url
        isDraft
        isPrerelease
        isLatest
        publishedAt
        createdAt
        author { login }
        tagCommit { oid }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "first": perPage}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	nodes, err := extractNodes(data, "repository.releases")
	if err != nil {
		return nil, err
	}
	return readOutput(nodes), nil
}

func (p *GitHubProvider) executeGetLatestRelease(ctx context.Context, client *httpc.Client, apiBase, owner, repo string) (*provider.Output, error) {
	query := `query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    latestRelease {
      name
      tagName
      description
      url
      isDraft
      isPrerelease
      publishedAt
      createdAt
      author { login }
      tagCommit { oid }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	release, err := extractNode(data, "repository.latestRelease")
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, fmt.Errorf("no releases found for %s/%s", owner, repo)
	}
	return readOutput(release), nil
}

// ─── Pull Requests ───────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListPullRequests(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)
	state := mapPRState(getStringInput(inputs, "state"))

	query := `query($owner: String!, $name: String!, $first: Int!, $states: [PullRequestState!]) {
  repository(owner: $owner, name: $name) {
    pullRequests(first: $first, states: $states, orderBy: {field: CREATED_AT, direction: DESC}) {
      nodes {
        number
        title
        state
        url
        body
        createdAt
        updatedAt
        author { login }
        headRefName
        baseRefName
        isDraft
        mergeable
        additions
        deletions
        changedFiles
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "first": perPage}
	if state != nil {
		vars["states"] = state
	}

	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	nodes, err := extractNodes(data, "repository.pullRequests")
	if err != nil {
		return nil, err
	}
	return readOutput(nodes), nil
}

func (p *GitHubProvider) executeGetPullRequest(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, fmt.Errorf("'number' is required for get_pull_request operation")
	}

	query := `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      id
      number
      title
      state
      url
      body
      createdAt
      updatedAt
      mergedAt
      closedAt
      author { login }
      headRefName
      baseRefName
      isDraft
      mergeable
      merged
      additions
      deletions
      changedFiles
      reviewDecision
      commits { totalCount }
      comments { totalCount }
      reviews { totalCount }
      statusCheckRollup: commits(last: 1) {
        nodes {
          commit {
            statusCheckRollup {
              state
              contexts(first: 50) {
                nodes {
                  ... on CheckRun {
                    __typename
                    name
                    status
                    conclusion
                    detailsUrl
                  }
                  ... on StatusContext {
                    __typename
                    context
                    state
                    targetUrl
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "number": num}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	pr, err := extractNodeMap(data, "repository.pullRequest")
	if err != nil {
		return nil, err
	}

	// Flatten statusCheckRollup into a top-level field
	flattenStatusCheckRollup(pr)

	return readOutput(pr), nil
}

// ─── Issues ──────────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListIssues(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)
	state := mapIssueState(getStringInput(inputs, "state"))

	query := `query($owner: String!, $name: String!, $first: Int!, $states: [IssueState!]) {
  repository(owner: $owner, name: $name) {
    issues(first: $first, states: $states, orderBy: {field: CREATED_AT, direction: DESC}) {
      nodes {
        number
        title
        state
        url
        body
        createdAt
        updatedAt
        closedAt
        author { login }
        labels(first: 20) { nodes { name color } }
        assignees(first: 10) { nodes { login } }
        comments { totalCount }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "first": perPage}
	if state != nil {
		vars["states"] = state
	}

	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	nodes, err := extractNodes(data, "repository.issues")
	if err != nil {
		return nil, err
	}
	return readOutput(nodes), nil
}

func (p *GitHubProvider) executeGetIssue(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, fmt.Errorf("'number' is required for get_issue operation")
	}

	query := `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      id
      number
      title
      state
      url
      body
      createdAt
      updatedAt
      closedAt
      author { login }
      labels(first: 20) { nodes { name color } }
      assignees(first: 10) { nodes { login } }
      comments { totalCount }
      milestone { title number }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "number": num}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	issue, err := extractNodeMap(data, "repository.issue")
	if err != nil {
		return nil, err
	}
	return readOutput(issue), nil
}

func (p *GitHubProvider) executeListIssueComments(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, fmt.Errorf("'number' is required for list_issue_comments operation")
	}
	perPage := getPerPage(inputs)

	query := `query($owner: String!, $name: String!, $number: Int!, $first: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      comments(first: $first) {
        nodes {
          id
          body
          createdAt
          updatedAt
          author { login }
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

	nodes, err := extractNodes(data, "repository.issue.comments")
	if err != nil {
		return nil, err
	}
	return readOutput(nodes), nil
}

// ─── Branches ────────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListBranches(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)

	query := `query($owner: String!, $name: String!, $first: Int!) {
  repository(owner: $owner, name: $name) {
    refs(refPrefix: "refs/heads/", first: $first, orderBy: {field: ALPHABETICAL, direction: ASC}) {
      nodes {
        name
        target {
          ... on Commit {
            oid
            message
            committedDate
            author { name email }
          }
        }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "first": perPage}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	nodes, err := extractNodes(data, "repository.refs")
	if err != nil {
		return nil, err
	}
	return readOutput(nodes), nil
}

func (p *GitHubProvider) executeGetBranch(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	branchName := getStringInput(inputs, "branch")
	if branchName == "" {
		return nil, fmt.Errorf("'branch' is required for get_branch operation")
	}

	qualifiedName := "refs/heads/" + branchName

	query := `query($owner: String!, $name: String!, $qualifiedName: String!) {
  repository(owner: $owner, name: $name) {
    ref(qualifiedName: $qualifiedName) {
      name
      target {
        ... on Commit {
          oid
          message
          committedDate
          author { name email }
          tree { oid }
        }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "qualifiedName": qualifiedName}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	ref, err := extractNode(data, "repository.ref")
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, fmt.Errorf("branch %q not found", branchName)
	}
	return readOutput(ref), nil
}

// ─── Tags ────────────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListTags(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)

	query := `query($owner: String!, $name: String!, $first: Int!) {
  repository(owner: $owner, name: $name) {
    refs(refPrefix: "refs/tags/", first: $first, orderBy: {field: ALPHABETICAL, direction: DESC}) {
      nodes {
        name
        target {
          ... on Commit { oid committedDate }
          ... on Tag { name target { ... on Commit { oid committedDate } } }
        }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "first": perPage}
	data, err := graphqlDo(ctx, client, apiBase, query, vars)
	if err != nil {
		return nil, err
	}

	nodes, err := extractNodes(data, "repository.refs")
	if err != nil {
		return nil, err
	}
	return readOutput(nodes), nil
}

// ─── HEAD OID ────────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeGetHeadOID(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	branchName := getStringInput(inputs, "branch")
	if branchName == "" {
		return nil, fmt.Errorf("'branch' is required for get_head_oid operation")
	}

	qualifiedName := "refs/heads/" + branchName

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
		return nil, err
	}

	ref, err := extractNode(data, "repository.ref")
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, fmt.Errorf("branch %q not found", branchName)
	}

	refMap, ok := ref.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected ref format")
	}
	target, ok := refMap["target"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected ref target format")
	}

	return readOutput(map[string]any{
		"oid":    target["oid"],
		"branch": branchName,
	}), nil
}

// ─── PR Comments ─────────────────────────────────────────────────────────────

func (p *GitHubProvider) executeListPRComments(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	num, ok := getIntInput(inputs, "number")
	if !ok || num == 0 {
		return nil, fmt.Errorf("'number' is required for list_pr_comments operation")
	}
	perPage := getPerPage(inputs)

	query := `query($owner: String!, $name: String!, $number: Int!, $first: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      comments(first: $first) {
        nodes {
          id
          body
          createdAt
          updatedAt
          author { login }
          url
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

	nodes, err := extractNodes(data, "repository.pullRequest.comments")
	if err != nil {
		return nil, err
	}
	return readOutput(nodes), nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// mapPRState converts user-friendly state strings to GitHub GraphQL PullRequestState enums.
func mapPRState(state string) []string {
	switch strings.ToLower(state) {
	case "open":
		return []string{"OPEN"}
	case "closed":
		return []string{"CLOSED"}
	case "merged":
		return []string{"MERGED"}
	case "all", "":
		return nil // no filter
	default:
		return []string{strings.ToUpper(state)}
	}
}

// mapIssueState converts user-friendly state strings to GitHub GraphQL IssueState enums.
func mapIssueState(state string) []string {
	switch strings.ToLower(state) {
	case "open":
		return []string{"OPEN"}
	case "closed":
		return []string{"CLOSED"}
	case "all", "":
		return nil
	default:
		return []string{strings.ToUpper(state)}
	}
}

// pathBasename returns the last component of a file path.
func pathBasename(p string) string {
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

// flattenStatusCheckRollup extracts the nested statusCheckRollup from the aliased
// commits field and replaces it with a flat "statusCheckRollup" key on the PR map.
// The original "statusCheckRollup" alias (which shadows "commits") is removed.
func flattenStatusCheckRollup(pr map[string]any) {
	raw, ok := pr["statusCheckRollup"]
	if !ok {
		return
	}
	// Remove the alias so it doesn't shadow the real commits field
	delete(pr, "statusCheckRollup")

	nodesWrapper, ok := raw.(map[string]any)
	if !ok {
		return
	}
	nodes, ok := nodesWrapper["nodes"].([]any)
	if !ok || len(nodes) == 0 {
		return
	}
	commitWrapper, ok := nodes[0].(map[string]any)
	if !ok {
		return
	}
	commit, ok := commitWrapper["commit"].(map[string]any)
	if !ok {
		return
	}
	rollup, ok := commit["statusCheckRollup"].(map[string]any)
	if !ok {
		return
	}

	result := map[string]any{
		"state": rollup["state"],
	}
	if contexts, ok := rollup["contexts"].(map[string]any); ok {
		if ctxNodes, ok := contexts["nodes"].([]any); ok {
			result["checks"] = ctxNodes
		}
	}
	pr["statusCheckRollup"] = result
}
