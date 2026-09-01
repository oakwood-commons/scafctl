// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/dryrun"
	scafpath "github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/executionregistry"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
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
		Path     string `json:"path" minLength:"1" maxLength:"4096" doc:"Path or URL to the solution file" example:"./solution.yaml"`
		Verbose  bool   `json:"verbose,omitempty" doc:"Include materialised inputs in the report"`
		LockMode string `json:"lockMode,omitempty" enum:"strict,constrained,bestEffort" doc:"Lock file resolution mode (default: strict)" example:"strict"`
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
		LockMode  string         `json:"lockMode,omitempty" enum:"strict,constrained,bestEffort" doc:"Lock file resolution mode (default: strict)" example:"strict"`
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
		Path     string         `json:"path" minLength:"1" maxLength:"4096" doc:"Path or URL to the solution file" example:"./solution.yaml"`
		Params   map[string]any `json:"params,omitempty" doc:"Parameters to pass to the solution"`
		LockMode string         `json:"lockMode,omitempty" enum:"strict,constrained,bestEffort" doc:"Lock file resolution mode (default: strict)" example:"strict"`
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

// requireRemotePath returns a 400 Huma error if path looks like a local
// filesystem reference. The API server accepts HTTP/HTTPS URLs and catalog
// names. Local file paths are blocked because the server cannot safely
// access arbitrary paths supplied by remote callers.
func requireRemotePath(path, opName string) error {
	if path == "" {
		return huma.NewError(http.StatusBadRequest,
			fmt.Sprintf("%s: path is required", opName))
	}

	// Reject path-traversal segments regardless of surrounding context.
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return huma.NewError(http.StatusBadRequest,
				fmt.Sprintf("%s: path must not contain '..' segments", opName))
		}
	}

	lower := strings.ToLower(path)

	// Explicitly allowed schemes.
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return nil
	}

	// Block all other URL schemes (file://, ftp://, data://, oci://, etc.).
	if strings.Contains(lower, "://") {
		return huma.NewError(http.StatusBadRequest,
			fmt.Sprintf("%s: unsupported URL scheme; only http:// and https:// are allowed", opName))
	}

	// Block obvious local-path indicators.
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, ".") ||
		strings.HasPrefix(path, "~") || strings.Contains(path, "\\") ||
		scafpath.HasWindowsDrivePrefix(path) {
		return huma.NewError(http.StatusBadRequest,
			fmt.Sprintf("%s: path must be a URL or catalog reference, not a local file path", opName))
	}

	// Bare filenames ending in .yaml/.yml/.json are almost certainly local files.
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json") {
		return huma.NewError(http.StatusBadRequest,
			fmt.Sprintf("%s: path must be a URL or catalog reference, not a local file path", opName))
	}

	// Paths containing "/" are either registry refs (ghcr.io/org/sol) or
	// relative local paths (configs/secret, dir/../../etc/passwd). Registry
	// hostnames always contain "." or ":" in the first segment; plain
	// directory names do not. Block the latter to prevent path traversal.
	if strings.Contains(path, "/") {
		firstSegment := strings.SplitN(path, "/", 2)[0]
		if !strings.Contains(firstSegment, ".") && !strings.Contains(firstSegment, ":") {
			return huma.NewError(http.StatusBadRequest,
				fmt.Sprintf("%s: path must be a URL or catalog reference, not a local file path", opName))
		}
	}

	// Anything else is treated as a catalog reference (e.g. "my-app",
	// "my-app@1.0.0", "ghcr.io/org/solution").
	return nil
}

// rejectUnsafePath returns a 400 Huma error if path is unsafe for server-side
// local file access. Used for output directory parameters only.
func rejectUnsafePath(path, opName string) error {
	if strings.Contains(path, "..") || filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") || strings.HasPrefix(path, "~") || scafpath.HasWindowsDrivePrefix(path) {
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
		if err := requireRemotePath(input.Body.Path, "solution-lint"); err != nil {
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
		if err := requireRemotePath(input.Body.Path, "solution-inspect"); err != nil {
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
		if err := requireRemotePath(input.Body.Path, "solution-dryrun"); err != nil {
			return nil, err
		}

		sol, executionRegistry, release, err := loadAndPrepareSolution(ctx, hctx, input.Body.Path, input.Body.LockMode, "solution-dryrun")
		defer releaseFunc(release)
		if err != nil {
			return nil, err
		}

		opts := dryrun.Options{
			Registry: executionRegistry,
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
		if err := requireRemotePath(input.Body.Path, "solution-run"); err != nil {
			return nil, err
		}
		if input.Body.OutputDir != "" {
			if err := rejectUnsafePath(input.Body.OutputDir, "solution-run outputDir"); err != nil {
				return nil, err
			}
		}

		sol, executionRegistry, release, err := loadAndPrepareSolution(ctx, hctx, input.Body.Path, input.Body.LockMode, "solution-run")
		defer releaseFunc(release)
		if err != nil {
			return nil, err
		}
		// Validate solution first
		validation := execute.ValidateSolution(ctx, sol, executionRegistry)
		if !validation.Valid {
			return nil, huma.NewError(http.StatusUnprocessableEntity, fmt.Sprintf("solution validation failed: %v", validation.Errors))
		}

		// Execute resolvers
		resolverCfg := execute.ResolverExecutionConfigFromContext(ctx)
		resolverResult, err := execute.Resolvers(ctx, sol, input.Body.Params, executionRegistry, resolverCfg)
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
			actionResult, err := execute.Actions(ctx, sol, resolverResult.Data, executionRegistry, actionCfg)
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
		if err := requireRemotePath(input.Body.Path, "solution-render"); err != nil {
			return nil, err
		}

		sol, executionRegistry, release, err := loadAndPrepareSolution(ctx, hctx, input.Body.Path, input.Body.LockMode, "solution-render")
		defer releaseFunc(release)
		if err != nil {
			return nil, err
		}

		validation := execute.ValidateSolution(ctx, sol, executionRegistry)

		resolverData, err := execute.ResolversForPreview(ctx, sol, input.Body.Params, executionRegistry)
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
		if err := requireRemotePath(input.Body.Path, "solution-test"); err != nil {
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

type ensureAndAcquire interface {
	EnsureAndAcquire(ctx context.Context, deps []solution.PluginDependency) (release func(), err error)
}

// Nil-guard errors returned by ensureProviderDependencies. They are sentinels so
// callers (and tests) can match with errors.Is rather than string comparison.
// Only solution and composite registry are validated here; the plugin fetcher,
// acquire function, and lock file are validated lazily downstream by
// BuildExecutionRegistryAPI. The missing-lock case surfaces there as
// prepare.ErrMissingLockFile (mapped to 400 by handleEnsureError) only when a
// referenced external provider actually needs a lock, so builtin-only and
// pure-CEL solutions need none of them.
var (
	errEnsureNilSolution     = errors.New("ensure provider dependencies: solution is nil")
	errEnsureNilCompositeReg = errors.New("ensure provider dependencies: composite registry is nil")
)

func parseLockMode(s string) (prepare.LockMode, error) {
	switch s {
	case "", "strict":
		return prepare.LockModeStrict, nil
	case "constrained":
		return prepare.LockModeConstrained, nil
	case "bestEffort":
		return prepare.LockModeBestEffort, nil
	default:
		return 0, huma.NewError(http.StatusBadRequest,
			fmt.Sprintf("solution-run: invalid lockMode %q; must be one of: strict, constrained, bestEffort", s))
	}
}

// loadAndPrepareSolution runs the shared preparation pipeline for the dryrun,
// run, and render endpoints: it parses the lock mode, loads the solution with
// its lock file, and builds the execution registry with provider dependencies
// resolved. On success it returns the loaded solution and its execution
// registry; on failure it returns an already-mapped Huma error (400 for a bad
// lock mode or a load failure, or the classification chosen by
// handleEnsureError for a dependency-resolution failure).
//
// The returned release function is always safe to pass to releaseFunc and MUST
// be deferred by the caller regardless of the returned error, since provider
// dependencies may have been partially acquired before a later failure.
func loadAndPrepareSolution(
	ctx context.Context,
	hctx *api.HandlerContext,
	path, lockModeStr, operation string,
) (*solution.Solution, *executionregistry.ExecutionRegistry[solution.PluginDependency], func(), error) {
	lockMode, err := parseLockMode(lockModeStr)
	if err != nil {
		return nil, nil, nil, err
	}

	sol, lock, err := inspect.LoadSolutionWithLock(ctx, path)
	if err != nil {
		return nil, nil, nil, api.HandleError(ctx, err, operation, http.StatusBadRequest, "failed to load solution")
	}

	executionRegistry, release, err := ensureProviderDependencies(
		ctx, sol, hctx.CompositeRegistry, hctx.CatalogIndex,
		hctx.PluginFetcher, hctx.PluginPool, lock, lockMode,
	)
	if err != nil {
		return nil, nil, release, handleEnsureError(ctx, err, operation)
	}
	return sol, executionRegistry, release, nil
}

func ensureProviderDependencies(
	ctx context.Context,
	sol *solution.Solution,
	compositeRegistry *provider.CompositeRegistry,
	resolver prepare.RegistryAliasResolver,
	fetcher *plugin.Fetcher,
	ensureAndAcquire ensureAndAcquire,
	locked *bundler.LockFile,
	mode prepare.LockMode,
) (*executionregistry.ExecutionRegistry[solution.PluginDependency], func(), error) {
	if sol == nil {
		return nil, nil, errEnsureNilSolution
	}
	if compositeRegistry == nil {
		return nil, nil, errEnsureNilCompositeReg
	}

	var ensureFunc prepare.EnsureAndAcquireFunc
	if ensureAndAcquire != nil {
		ensureFunc = ensureAndAcquire.EnsureAndAcquire
	}
	var resolvePlugin prepare.ResolvePluginsFunc
	if fetcher != nil {
		resolvePlugin = fetcher.ResolvePlugins
	}
	return prepare.BuildExecutionRegistryAPI(
		ctx, sol, compositeRegistry, resolver,
		resolvePlugin, ensureFunc,
		mode.OrDefault(), locked,
	)
}

func releaseFunc(releaseFunc func()) {
	if releaseFunc != nil {
		releaseFunc()
	}
}

// handleEnsureError maps errors from ensureProviderDependencies to the
// appropriate HTTP status code. Pool policy errors (disabled, not allowed,
// full) get their dedicated status; everything else falls back to 500.
func handleEnsureError(ctx context.Context, err error, operation string) error {
	// A missing lock file is a client/config condition (the solution references
	// an external provider under strict/constrained mode but carries no lock),
	// not an upstream plugin failure. Classify it as 400 before the pool-error
	// mapping, whose default is 502.
	if errors.Is(err, prepare.ErrMissingLockFile) {
		return api.HandleError(ctx, err, operation, http.StatusBadRequest, err.Error())
	}
	if status := plugin.PoolErrorHTTPStatus(err); status != 0 {
		return api.HandleError(ctx, err, operation, status, err.Error())
	}
	return api.HandleError(ctx, err, operation, http.StatusInternalServerError, "failed to ensure provider dependencies")
}
