// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
)

// registerPackageTools registers solution packaging and bundle verification MCP tools.
func (s *Server) registerPackageTools() {
	packageSolutionTool := mcp.NewTool("package_solution",
		mcp.WithDescription(fmt.Sprintf(
			"Verify solution bundle discovery or package a solution into the local catalog (MCP equivalent of '%s package solution'). "+
				"Defaults to dry_run=true, which composes the solution, discovers static file dependencies and bundle.include globs, "+
				"checks bundle root boundaries ('../' parent-relative paths fail with 'path escapes bundle root'), and reports unmatched globs "+
				"or dynamic path warnings without modifying files or the catalog. Set dry_run=false to build and store the OCI artifact locally.",
			s.name,
		)),
		mcp.WithTitleAnnotation("Package Solution"),
		mcp.WithToolIcons(toolIcons["solution"]),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Description("Path to the solution YAML file (optional; auto-discovered from cwd when omitted)"),
		),
		mcp.WithBoolean("dry_run",
			mcp.Description("Preview bundle discovery, globs, and artifact identity without writing lock/vendor files or storing into the local catalog (default: true). Set to false to package and store locally."),
		),
		mcp.WithString("name",
			mcp.Description("Artifact name override (default: metadata.name or solution filename)"),
		),
		mcp.WithString("version",
			mcp.Description("Semantic version override, e.g. '1.0.0' (default: metadata.version or auto-incremented patch)"),
		),
		mcp.WithString("base_dir",
			mcp.Description("Override the bundle root directory used to resolve compose partials, static file dependencies, and bundle.include globs (default: solution file's directory)"),
		),
		mcp.WithBoolean("no_bundle",
			mcp.Description("Skip file bundling and package only the solution YAML (default: false)"),
		),
		mcp.WithBoolean("no_vendor",
			mcp.Description("Skip vendoring catalog dependencies and pinning plugins in solution.lock (default: false)"),
		),
		mcp.WithBoolean("force",
			mcp.Description("Allow overriding metadata.name and overwriting an existing version in the local catalog (default: false)"),
		),
		mcp.WithBoolean("strict",
			mcp.Description("Fail when bundle verification or dry-run discovery produces completeness warnings such as unmatched bundle.include globs or unbundled file/catalog references (default: false)"),
		),
		mcp.WithBoolean("skip_lint",
			mcp.Description("Skip the pre-flight lint check (default: false)"),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescDefault),
		),
	)
	s.addTool(packageSolutionTool, s.handlePackageSolution)
}

// handlePackageSolution validates bundle discovery or packages a solution via pkg/solution/builder.
func (s *Server) handlePackageSolution(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	if path == builder.StdinPath {
		return newStructuredError(ErrCodeInvalidInput,
			"stdin ('-') is not supported over MCP; provide a file path or omit 'path' for auto-discovery",
			WithField("path"),
			WithSuggestion("Pass the path to a solution YAML file on disk"),
		), nil
	}

	if reqCtx == nil {
		reqCtx = context.Background()
	}
	ctx := s.freshConfigContext(reqCtx)

	if cwd := request.GetString("cwd", ""); cwd != "" {
		absCwd, err := provider.ValidateDirectory(cwd)
		if err != nil {
			return newStructuredError(ErrCodeInvalidInput, err.Error(),
				WithField("cwd"),
				WithSuggestion("Provide a valid existing directory path"),
			), nil
		}
		ctx = provider.WithWorkingDirectory(ctx, absCwd)
	}

	baseDir := request.GetString("base_dir", "")
	if baseDir != "" {
		resolvedBase, absErr := provider.AbsFromContext(ctx, baseDir)
		if absErr != nil {
			return newStructuredError(ErrCodeInvalidInput, absErr.Error(),
				WithField("base_dir"),
				WithSuggestion("Provide a valid existing directory path for base_dir"),
			), nil
		}
		validBase, valErr := provider.ValidateDirectory(resolvedBase)
		if valErr != nil {
			return newStructuredError(ErrCodeInvalidInput, valErr.Error(),
				WithField("base_dir"),
				WithSuggestion("Provide a valid existing directory path for base_dir"),
			), nil
		}
		baseDir = validBase
	}

	opts := builder.PackageSolutionOptions{
		Path:     path,
		BaseDir:  baseDir,
		Name:     request.GetString("name", ""),
		Version:  request.GetString("version", ""),
		DryRun:   request.GetBool("dry_run", true),
		NoBundle: request.GetBool("no_bundle", false),
		NoVendor: request.GetBool("no_vendor", false),
		Force:    request.GetBool("force", false),
		Strict:   request.GetBool("strict", false),
		SkipLint: request.GetBool("skip_lint", false),
		Registry: s.registry,
	}

	res, err := builder.PackageSolution(ctx, opts)
	if err != nil {
		return mapPackageSolutionError(res, err), nil
	}

	return mcp.NewToolResultJSON(res)
}

func mapPackageSolutionError(res *builder.PackageSolutionResult, err error) *mcp.CallToolResult {
	switch {
	case errors.Is(err, builder.ErrDevVersion):
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("version"),
			WithSuggestion("Set metadata.version in the solution YAML or pass a concrete semver via 'version' (e.g., '1.0.0')"),
		)
	case errors.Is(err, builder.ErrInvalidIdentity):
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithSuggestion("Provide valid 'name' and 'version' values or set metadata.name and metadata.version in the solution YAML"),
		)
	case errors.Is(err, builder.ErrNameMismatch):
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("name"),
			WithSuggestion("Pass force: true to override metadata.name, or omit 'name' to use metadata.name"),
		)
	case errors.Is(err, builder.ErrPreflightBlocked):
		msg := err.Error()
		if res != nil && res.LintResult != nil && res.LintResult.ErrorCount > 0 {
			msg = fmt.Sprintf("%s: %d lint error(s)", msg, res.LintResult.ErrorCount)
		}
		return newStructuredError(ErrCodeValidationError, msg,
			WithSuggestion("Call 'lint_solution' to inspect and fix lint errors, or pass skip_lint: true to bypass pre-flight lint"),
			WithRelatedTools("lint_solution"),
		)
	case errors.Is(err, builder.ErrVerifyFailed):
		msg := err.Error()
		if res != nil && len(res.Warnings) > 0 {
			msg = fmt.Sprintf("%s: %s", msg, strings.Join(res.Warnings, "; "))
		}
		return newStructuredError(ErrCodeValidationError, msg,
			WithSuggestion("Ensure all referenced files and bundle.include globs match files under the bundle root, or omit strict: true if warnings are expected"),
			WithRelatedTools("inspect_solution", "lint_solution"),
		)
	case errors.Is(err, builder.ErrStoreFailed) && catalog.IsExists(err):
		return newStructuredError(ErrCodeExecFailed, err.Error(),
			WithSuggestion("Pass force: true to overwrite the existing version, or specify a new version"),
			WithRelatedTools("catalog_list", "catalog_inspect"),
		)
	case errors.Is(err, load.ErrAmbiguousSolution):
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Multiple solution files exist in the working directory; pass an explicit 'path' (for example 'solution.yaml')"),
			WithRelatedTools("list_solutions"),
		)
	case strings.Contains(err.Error(), "escapes bundle root") ||
		strings.Contains(err.Error(), "escapes solution directory") ||
		strings.Contains(err.Error(), "absolute path not allowed"):
		return newStructuredError(ErrCodeValidationError, err.Error(),
			WithSuggestion("Move referenced or composed files inside the solution directory (bundle root), or set base_dir to a parent directory that encloses all bundled files"),
			WithRelatedTools("inspect_solution"),
		)
	case strings.Contains(err.Error(), "loading solution:"):
		return newStructuredError(ErrCodeLoadFailed, err.Error(),
			WithField("path"),
			WithSuggestion("Check that the solution file exists and contains valid YAML"),
			WithRelatedTools("lint_solution", "inspect_solution"),
		)
	default:
		return newStructuredError(ErrCodeExecFailed, err.Error(),
			WithSuggestion("Check solution file paths, bundle configuration, and catalog settings"),
			WithRelatedTools("lint_solution", "inspect_solution"),
		)
	}
}
