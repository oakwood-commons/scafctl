// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── List Check Runs ─────────────────────────────────────────────────────────

// executeListCheckRuns lists check runs for a commit ref via the REST API.
// This replaces `gh pr checks <number>`.
func (p *GitHubProvider) executeListCheckRuns(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	ref := getStringInput(inputs, "ref")
	if ref == "" {
		return nil, requiredInputError("list_check_runs", "ref", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs", apiBase, owner, repo, url.PathEscape(ref))
	result, err := p.doRESTRequest(ctx, client, "GET", restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing check runs: %w", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected check runs response format")
	}

	checkRuns, _ := resultMap["check_runs"].([]any)
	// Slim down each check run to the fields the agents need
	runs := make([]any, 0, len(checkRuns))
	for _, cr := range checkRuns {
		crMap, ok := cr.(map[string]any)
		if !ok {
			continue
		}
		run := map[string]any{
			"id":           crMap["id"],
			"name":         crMap["name"],
			"status":       crMap["status"],
			"conclusion":   crMap["conclusion"],
			"started_at":   crMap["started_at"],
			"completed_at": crMap["completed_at"],
			"html_url":     crMap["html_url"],
		}
		if output, ok := crMap["output"].(map[string]any); ok {
			run["output"] = map[string]any{
				"title":   output["title"],
				"summary": output["summary"],
			}
		}
		runs = append(runs, run)
	}

	return readOutput(map[string]any{
		"total_count": resultMap["total_count"],
		"check_runs":  runs,
	}), nil
}

// ─── Get Workflow Run ────────────────────────────────────────────────────────

// executeGetWorkflowRun fetches a workflow run and its jobs via the REST API.
// This replaces `gh run view <run_id> --log-failed`.
func (p *GitHubProvider) executeGetWorkflowRun(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	runID, ok := getIntInput(inputs, "run_id")
	if !ok || runID == 0 {
		return nil, requiredInputError("get_workflow_run", "run_id", inputs, "")
	}

	// Fetch the workflow run
	runURL := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d", apiBase, owner, repo, runID)
	runResult, err := p.doRESTRequest(ctx, client, "GET", runURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching workflow run: %w", err)
	}

	runMap, ok := runResult.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected workflow run response format")
	}

	// Fetch jobs for the run
	jobsURL := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/jobs", apiBase, owner, repo, runID)
	jobsResult, err := p.doRESTRequest(ctx, client, "GET", jobsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching workflow run jobs: %w", err)
	}

	var jobs []any
	if jobsMap, ok := jobsResult.(map[string]any); ok {
		if jobsList, ok := jobsMap["jobs"].([]any); ok {
			jobs = make([]any, 0, len(jobsList))
			for _, j := range jobsList {
				jMap, ok := j.(map[string]any)
				if !ok {
					continue
				}
				job := map[string]any{
					"id":           jMap["id"],
					"name":         jMap["name"],
					"status":       jMap["status"],
					"conclusion":   jMap["conclusion"],
					"started_at":   jMap["started_at"],
					"completed_at": jMap["completed_at"],
					"html_url":     jMap["html_url"],
				}
				// Include steps for failed jobs
				if jMap["conclusion"] == "failure" {
					if steps, ok := jMap["steps"].([]any); ok {
						job["steps"] = steps
					}
				}
				jobs = append(jobs, job)
			}
		}
	}

	return readOutput(map[string]any{
		"id":            runMap["id"],
		"name":          runMap["name"],
		"status":        runMap["status"],
		"conclusion":    runMap["conclusion"],
		"html_url":      runMap["html_url"],
		"run_number":    runMap["run_number"],
		"event":         runMap["event"],
		"created_at":    runMap["created_at"],
		"updated_at":    runMap["updated_at"],
		"head_branch":   runMap["head_branch"],
		"head_sha":      runMap["head_sha"],
		"display_title": runMap["display_title"],
		"jobs":          jobs,
	}), nil
}

// ─── List Commit Pulls ───────────────────────────────────────────────────────

// executeListCommitPulls lists pull requests associated with a commit via the REST API.
// Uses: GET /repos/{owner}/{repo}/commits/{commit_sha}/pulls
func (p *GitHubProvider) executeListCommitPulls(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	commitSHA := getStringInputWithAliases(inputs, "commit_sha", "sha")
	if commitSHA == "" {
		return nil, requiredInputError("list_commit_pulls", "commit_sha", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s/pulls", apiBase, owner, repo, url.PathEscape(commitSHA))
	result, err := p.doRESTRequest(ctx, client, "GET", restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing commit pulls: %w", err)
	}

	pulls, ok := result.([]any)
	if !ok {
		return readOutput(map[string]any{
			"total_count":   0,
			"pull_requests": []any{},
		}), nil
	}

	// Slim down each PR to essential fields.
	prs := make([]any, 0, len(pulls))
	for _, pr := range pulls {
		prMap, ok := pr.(map[string]any)
		if !ok {
			continue
		}
		slim := map[string]any{
			"number":     prMap["number"],
			"title":      prMap["title"],
			"state":      prMap["state"],
			"html_url":   prMap["html_url"],
			"created_at": prMap["created_at"],
			"merged_at":  prMap["merged_at"],
			"draft":      prMap["draft"],
		}
		if user, ok := prMap["user"].(map[string]any); ok {
			slim["user"] = user["login"]
		}
		if head, ok := prMap["head"].(map[string]any); ok {
			slim["head_ref"] = head["ref"]
		}
		if base, ok := prMap["base"].(map[string]any); ok {
			slim["base_ref"] = base["ref"]
		}
		prs = append(prs, slim)
	}

	return readOutput(map[string]any{
		"total_count":   len(prs),
		"pull_requests": prs,
	}), nil
}
