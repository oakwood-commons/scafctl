// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/dryrun"
	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
)

// ── Request / Response types ──

// SolutionLintRequest is the request body for linting a solution.
type SolutionLintRequest struct {
	Body struct {
		Path string `json:"path" minLength:"1" maxLength:"4096" doc:"Path or URL to the solution file" example:"./solution.yaml"`
	}
}

// SolutionLintResponse wraps the lint result.
type SolutionLintResponse struct {
	Body *lint.Result
}

// SolutionInspectRequest is the request body for inspecting a solution.
type SolutionInspectRequest struct {
	Body struct {
		Path string `json:"path" minLength:"1" maxLength:"4096" doc:"Path or URL to the solution file" example:"./solution.yaml"`
	}
}

// SolutionInspectResponse wraps the inspection result.
type SolutionInspectResponse struct {
	Body *inspect.SolutionExplanation
}

// SolutionDryRunRequest is the request body for dry-running a solution.
type SolutionDryRunRequest struct {
	Body struct {
		Path    string `json:"path" minLength:"1" maxLength:"4096" doc:"Path or URL to the solution file" example:"./solution.yaml"`
		Verbose bool   `json:"verbose,omitempty" doc:"Include materialised inputs in the report"`
	}
}

// SolutionDryRunResponse wraps the dry-run report.
type SolutionDryRunResponse struct {
	Body *dryrun.Report
}

// SolutionRunRequest is the request body for running a solution.
type SolutionRunRequest struct {
	Body struct {
		Path      string         `json:"path" minLength:"1" maxLength:"4096" doc:"Path or URL to the solution file" example:"./solution.yaml"`
		Params    map[string]any `json:"params,omitempty" doc:"Parameters to pass to the solution"`
		OutputDir string         `json:"outputDir,omitempty" maxLength:"4096" doc:"Target directory for action output"`
	}
}

// SolutionRunResponse wraps the solution execution result.
type SolutionRunResponse struct {
	Body struct {
		ResolverData map[string]any `json:"resolverData" doc:"Resolved values from resolvers"`
		ActionResult any            `json:"actionResult,omitempty" doc:"Action execution result"`
	}
}

// SolutionRenderRequest is the request body for rendering solution templates.
type SolutionRenderRequest struct {
	Body struct {
		Path   string         `json:"path" minLength:"1" maxLength:"4096" doc:"Path or URL to the solution file" example:"./solution.yaml"`
		Params map[string]any `json:"params,omitempty" doc:"Parameters to pass to the solution"`
	}
}

// SolutionRenderResponse wraps the render result.
type SolutionRenderResponse struct {
	Body struct {
		ResolverData map[string]any `json:"resolverData" doc:"Resolved values from resolvers"`
		Validation   any            `json:"validation,omitempty" doc:"Solution validation result"`
	}
}

// SolutionTestRequest is the request body for running solution tests.
type SolutionTestRequest struct {
	Body struct {
		Path    string `json:"path" minLength:"1" maxLength:"4096" doc:"Path or URL to the solution file" example:"./solution.yaml"`
		DryRun  bool   `json:"dryRun,omitempty" doc:"Validate tests without executing commands"`
		Verbose bool   `json:"verbose,omitempty" doc:"Include extra output"`
	}
}

// SolutionTestResponse wraps the test execution result.
type SolutionTestResponse struct {
	Body struct {
		Validation *execute.SolutionValidationResult `json:"validation" doc:"Solution validation result"`
	}
}

// requireURLPath returns a 400 Huma error if path is not an HTTP or HTTPS URL.
// API-supplied solution paths must be URLs — local file system access is not
// permitted via the API, as the server cannot safely access arbitrary local
// paths supplied by remote callers.
func requireURLPath(path, opName string) error {
	lower := strings.ToLower(path)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return huma.NewError(http.StatusBadRequest,
			fmt.Sprintf("%s: path must be an HTTP or HTTPS URL", opName))
	}
	return nil
}

// rejectUnsafePath returns a 400 Huma error if path is unsafe for server-side
// local file access. Used for output directory parameters only.
func rejectUnsafePath(path, opName string) error {
	if strings.Contains(path, "..") || filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return huma.NewError(http.StatusBadRequest,
			fmt.Sprintf("%s: must be a relative path that does not contain '..'", opName))
	}
	return nil
}

// ── Registration ──

// RegisterSolutionEndpoints registers solution-related API endpoints.
func RegisterSolutionEndpoints(humaAPI huma.API, hctx *api.HandlerContext, prefix string) {
	// POST /solutions/lint
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "solution-lint",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/solutions/lint", prefix),
		Summary:     "Lint a solution",
		Description: "Validates a solution file and returns findings (errors, warnings, info).",
		Tags:        []string{"Solutions"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *SolutionLintRequest) (*SolutionLintResponse, error) {
		if err := requireURLPath(input.Body.Path, "solution-lint"); err != nil {
			return nil, err
		}
		sol, err := inspect.LoadSolution(ctx, input.Body.Path)
		if err != nil {
			return nil, api.HandleError(ctx, err, "solution-lint", http.StatusBadRequest, "failed to load solution")
		}

		result := lint.Solution(sol, input.Body.Path, hctx.ProviderRegistry)
		return &SolutionLintResponse{Body: result}, nil
	})

	// POST /solutions/inspect
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "solution-inspect",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/solutions/inspect", prefix),
		Summary:     "Inspect a solution",
		Description: "Loads a solution and returns its full structural explanation.",
		Tags:        []string{"Solutions"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *SolutionInspectRequest) (*SolutionInspectResponse, error) {
		if err := requireURLPath(input.Body.Path, "solution-inspect"); err != nil {
			return nil, err
		}
		sol, err := inspect.LoadSolution(ctx, input.Body.Path)
		if err != nil {
			return nil, api.HandleError(ctx, err, "solution-inspect", http.StatusBadRequest, "failed to load solution")
		}

		explanation := inspect.BuildSolutionExplanation(sol)
		return &SolutionInspectResponse{Body: explanation}, nil
	})

	// POST /solutions/dryrun
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "solution-dryrun",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/solutions/dryrun", prefix),
		Summary:     "Dry-run a solution",
		Description: "Performs a dry run of the solution, showing what actions would be taken without executing them.",
		Tags:        []string{"Solutions"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *SolutionDryRunRequest) (*SolutionDryRunResponse, error) {
		if err := requireURLPath(input.Body.Path, "solution-dryrun"); err != nil {
			return nil, err
		}
		sol, err := inspect.LoadSolution(ctx, input.Body.Path)
		if err != nil {
			return nil, api.HandleError(ctx, err, "solution-dryrun", http.StatusBadRequest, "failed to load solution")
		}

		opts := dryrun.Options{
			Registry: hctx.ProviderRegistry,
			Verbose:  input.Body.Verbose,
		}
		report, err := dryrun.Generate(ctx, sol, opts)
		if err != nil {
			return nil, api.HandleError(ctx, err, "solution-dryrun", http.StatusInternalServerError, "dry run failed")
		}

		return &SolutionDryRunResponse{Body: report}, nil
	})

	// POST /solutions/run
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "solution-run",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/solutions/run", prefix),
		Summary:     "Run a solution",
		Description: "Executes a solution: resolves all inputs and runs the action workflow.",
		Tags:        []string{"Solutions"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *SolutionRunRequest) (*SolutionRunResponse, error) {
		if err := requireURLPath(input.Body.Path, "solution-run"); err != nil {
			return nil, err
		}
		if input.Body.OutputDir != "" {
			if err := rejectUnsafePath(input.Body.OutputDir, "solution-run outputDir"); err != nil {
				return nil, err
			}
		}
		sol, err := inspect.LoadSolution(ctx, input.Body.Path)
		if err != nil {
			return nil, api.HandleError(ctx, err, "solution-run", http.StatusBadRequest, "failed to load solution")
		}

		// Validate solution first
		validation := execute.ValidateSolution(ctx, sol, hctx.ProviderRegistry)
		if !validation.Valid {
			return nil, huma.NewError(http.StatusUnprocessableEntity, fmt.Sprintf("solution validation failed: %v", validation.Errors))
		}

		// Execute resolvers
		resolverCfg := execute.ResolverExecutionConfigFromContext(ctx)
		resolverResult, err := execute.Resolvers(ctx, sol, input.Body.Params, hctx.ProviderRegistry, resolverCfg)
		if err != nil {
			return nil, api.HandleError(ctx, err, "solution-run", http.StatusInternalServerError, "resolver execution failed")
		}

		resp := &SolutionRunResponse{}
		resp.Body.ResolverData = resolverResult.Data

		// Execute actions if workflow exists
		if sol.Spec.HasWorkflow() {
			actionCfg := execute.ActionExecutionConfigFromContext(ctx)
			if input.Body.OutputDir != "" {
				actionCfg.OutputDir = input.Body.OutputDir
			}
			actionResult, err := execute.Actions(ctx, sol, resolverResult.Data, hctx.ProviderRegistry, actionCfg)
			if err != nil {
				return nil, api.HandleError(ctx, err, "solution-run", http.StatusInternalServerError, "action execution failed")
			}
			resp.Body.ActionResult = actionResult.Result
		}

		return resp, nil
	})

	// POST /solutions/render
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "solution-render",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/solutions/render", prefix),
		Summary:     "Render solution templates",
		Description: "Resolves all inputs in a solution without executing actions. Returns the resolved values.",
		Tags:        []string{"Solutions"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *SolutionRenderRequest) (*SolutionRenderResponse, error) {
		if err := requireURLPath(input.Body.Path, "solution-render"); err != nil {
			return nil, err
		}
		sol, err := inspect.LoadSolution(ctx, input.Body.Path)
		if err != nil {
			return nil, api.HandleError(ctx, err, "solution-render", http.StatusBadRequest, "failed to load solution")
		}

		validation := execute.ValidateSolution(ctx, sol, hctx.ProviderRegistry)

		resolverData, err := execute.ResolversForPreview(ctx, sol, input.Body.Params, hctx.ProviderRegistry)
		if err != nil {
			return nil, api.HandleError(ctx, err, "solution-render", http.StatusInternalServerError, "resolver execution failed")
		}

		resp := &SolutionRenderResponse{}
		resp.Body.ResolverData = resolverData
		resp.Body.Validation = validation
		return resp, nil
	})

	// POST /solutions/test
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "solution-test",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/solutions/test", prefix),
		Summary:     "Validate a solution",
		Description: "Validates a solution's structure and workflow against the provider registry.",
		Tags:        []string{"Solutions"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *SolutionTestRequest) (*SolutionTestResponse, error) {
		if err := requireURLPath(input.Body.Path, "solution-test"); err != nil {
			return nil, err
		}
		sol, err := inspect.LoadSolution(ctx, input.Body.Path)
		if err != nil {
			return nil, api.HandleError(ctx, err, "solution-test", http.StatusBadRequest, "failed to load solution")
		}

		validation := execute.ValidateSolution(ctx, sol, hctx.ProviderRegistry)

		resp := &SolutionTestResponse{}
		resp.Body.Validation = validation
		return resp, nil
	})
}
