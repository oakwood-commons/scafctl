// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ─── Dispatch Workflow ───────────────────────────────────────────────────────

func (p *GitHubProvider) executeDispatchWorkflow(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	workflowID := getStringInput(inputs, "workflow_id")
	if workflowID == "" {
		return nil, requiredInputError("dispatch_workflow", "workflow_id", inputs, "workflow filename (e.g. ci.yml) or numeric ID")
	}

	ref := getStringInput(inputs, "ref")
	if ref == "" {
		return nil, requiredInputError("dispatch_workflow", "ref", inputs, "branch or tag to run the workflow on")
	}

	reqBody := map[string]any{
		"ref": ref,
	}
	if workflowInputs := getMapInput(inputs, "workflow_inputs"); len(workflowInputs) > 0 {
		reqBody["inputs"] = workflowInputs
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/actions/workflows/%s/dispatches", apiBase, owner, repo, url.PathEscape(workflowID))
	_, err := p.doRESTRequest(ctx, client, http.MethodPost, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("dispatching workflow: %w", err)
	}
	// dispatch_workflow returns 204 No Content on success.
	return actionOutput("dispatch_workflow", map[string]any{"dispatched": true}), nil
}

// ─── List Workflow Runs ──────────────────────────────────────────────────────

func (p *GitHubProvider) executeListWorkflowRuns(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)
	restURL := fmt.Sprintf("%s/repos/%s/%s/actions/runs?per_page=%d", apiBase, owner, repo, perPage)

	if status := getStringInput(inputs, "workflow_status"); status != "" {
		restURL += "&status=" + url.QueryEscape(status)
	}
	if branch := getStringInput(inputs, "branch"); branch != "" {
		restURL += "&branch=" + url.QueryEscape(branch)
	}

	result, err := p.doRESTRequest(ctx, client, http.MethodGet, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing workflow runs: %w", err)
	}
	return readOutput(result), nil
}

// ─── Cancel Workflow Run ─────────────────────────────────────────────────────

func (p *GitHubProvider) executeCancelWorkflowRun(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	runID, ok := getIntInput(inputs, "run_id")
	if !ok || runID == 0 {
		return nil, requiredInputError("cancel_workflow_run", "run_id", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/cancel", apiBase, owner, repo, runID)
	_, err := p.doRESTRequest(ctx, client, http.MethodPost, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cancelling workflow run: %w", err)
	}
	return actionOutput("cancel_workflow_run", map[string]any{"cancelled": true}), nil
}

// ─── Rerun Workflow ──────────────────────────────────────────────────────────

func (p *GitHubProvider) executeRerunWorkflow(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	runID, ok := getIntInput(inputs, "run_id")
	if !ok || runID == 0 {
		return nil, requiredInputError("rerun_workflow", "run_id", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/rerun", apiBase, owner, repo, runID)
	_, err := p.doRESTRequest(ctx, client, http.MethodPost, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("rerunning workflow: %w", err)
	}
	return actionOutput("rerun_workflow", map[string]any{"rerun": true}), nil
}

// ─── List Repository Variables ───────────────────────────────────────────────

func (p *GitHubProvider) executeListRepoVariables(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)
	restURL := fmt.Sprintf("%s/repos/%s/%s/actions/variables?per_page=%d", apiBase, owner, repo, perPage)
	result, err := p.doRESTRequest(ctx, client, http.MethodGet, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing repo variables: %w", err)
	}
	return readOutput(result), nil
}

// ─── Create or Update Repository Variable ────────────────────────────────────

func (p *GitHubProvider) executeCreateOrUpdateVariable(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	varName := getStringInput(inputs, "variable_name")
	if varName == "" {
		return nil, requiredInputError("create_or_update_variable", "variable_name", inputs, "")
	}
	varValue := getStringInput(inputs, "variable_value")
	if varValue == "" {
		return nil, requiredInputError("create_or_update_variable", "variable_value", inputs, "")
	}

	reqBody := map[string]any{
		"name":  varName,
		"value": varValue,
	}

	// Try to update first (PATCH). If the variable doesn't exist (404), create it (POST).
	updateURL := fmt.Sprintf("%s/repos/%s/%s/actions/variables/%s", apiBase, owner, repo, url.PathEscape(varName))
	_, err := p.doRESTRequest(ctx, client, http.MethodPatch, updateURL, reqBody)
	if err != nil {
		var re *restError
		if !errors.As(err, &re) || re.StatusCode != http.StatusNotFound {
			return nil, fmt.Errorf("updating variable: %w", err)
		}
		// Variable not found -- create it.
		createURL := fmt.Sprintf("%s/repos/%s/%s/actions/variables", apiBase, owner, repo)
		result, createErr := p.doRESTRequest(ctx, client, http.MethodPost, createURL, reqBody)
		if createErr != nil {
			return nil, fmt.Errorf("creating variable: %w", createErr)
		}
		return actionOutput("create_or_update_variable", result), nil
	}
	return actionOutput("create_or_update_variable", map[string]any{"updated": true}), nil
}

// ─── Delete Repository Variable ──────────────────────────────────────────────

func (p *GitHubProvider) executeDeleteVariable(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	varName := getStringInput(inputs, "variable_name")
	if varName == "" {
		return nil, requiredInputError("delete_variable", "variable_name", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/actions/variables/%s", apiBase, owner, repo, url.PathEscape(varName))
	result, err := p.doRESTRequest(ctx, client, http.MethodDelete, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("deleting variable: %w", err)
	}
	return actionOutput("delete_variable", result), nil
}

// ─── List Environments ───────────────────────────────────────────────────────

func (p *GitHubProvider) executeListEnvironments(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	perPage := getPerPage(inputs)
	restURL := fmt.Sprintf("%s/repos/%s/%s/environments?per_page=%d", apiBase, owner, repo, perPage)
	result, err := p.doRESTRequest(ctx, client, http.MethodGet, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing environments: %w", err)
	}
	return readOutput(result), nil
}

// ─── Create or Update Environment ────────────────────────────────────────────

func (p *GitHubProvider) executeCreateOrUpdateEnvironment(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	envName := getStringInput(inputs, "environment_name")
	if envName == "" {
		return nil, requiredInputError("create_or_update_environment", "environment_name", inputs, "")
	}

	reqBody := map[string]any{}
	if reviewers, ok := inputs["reviewers"].([]any); ok && len(reviewers) > 0 {
		reqBody["reviewers"] = reviewers
	}
	if waitTimer, ok := getIntInput(inputs, "wait_timer"); ok {
		reqBody["wait_timer"] = waitTimer
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/environments/%s", apiBase, owner, repo, url.PathEscape(envName))
	result, err := p.doRESTRequest(ctx, client, http.MethodPut, restURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating/updating environment: %w", err)
	}
	return actionOutput("create_or_update_environment", result), nil
}

// ─── Delete Environment ──────────────────────────────────────────────────────

func (p *GitHubProvider) executeDeleteEnvironment(ctx context.Context, client *httpc.Client, apiBase, owner, repo string, inputs map[string]any) (*provider.Output, error) {
	envName := getStringInput(inputs, "environment_name")
	if envName == "" {
		return nil, requiredInputError("delete_environment", "environment_name", inputs, "")
	}

	restURL := fmt.Sprintf("%s/repos/%s/%s/environments/%s", apiBase, owner, repo, url.PathEscape(envName))
	result, err := p.doRESTRequest(ctx, client, http.MethodDelete, restURL, nil)
	if err != nil {
		return nil, fmt.Errorf("deleting environment: %w", err)
	}
	return actionOutput("delete_environment", result), nil
}
