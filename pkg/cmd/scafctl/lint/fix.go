// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/fs"
	pkglint "github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// runLintFix implements 'lint --fix', 'lint --fix-dry-run', and '--diff'.
//
// Output separation: the fix outcome summary and the unified diff are workflow
// status, so the summary is written to stderr (never corrupting structured
// stdout) while residual findings are rendered to stdout through the shared
// RenderResult path (respecting -o and --severity). The diff is the primary
// artifact of --diff and goes to stdout so it can be piped to 'patch'.
//
// Write policy mirrors ruff: a file is written only for --fix WITHOUT --diff.
// --fix-dry-run and any --diff run preview without writing.
//
// Exit code: --fix returns non-zero only when residual errors remain after
// fixing. The preview modes (--fix-dry-run and --diff) return validation-failed
// (exit 2) when there are pending fixes, so they can gate CI (ruff --diff /
// --check parity); they exit 0 only when nothing would change.
func runLintFix(ctx context.Context, opts *Options) error {
	if opts.BinaryName == "" {
		opts.BinaryName = settings.CliBinaryName
	}
	w := writer.FromContext(ctx)

	if opts.Fix && opts.FixDryRun {
		return failFix(opts, exitcode.ValidationFailed, errors.New("--fix and --fix-dry-run are mutually exclusive"))
	}
	if opts.Diff && !opts.Fix && !opts.FixDryRun {
		return failFix(opts, exitcode.ValidationFailed, errors.New("--diff requires --fix or --fix-dry-run"))
	}

	lgr := logger.FromContext(ctx)
	getter := newLintGetter(ctx, lgr)

	// Fixing mutates a file, so ambiguous auto-discovery must error (high risk)
	// rather than silently pick one of several matches, matching 'refactor rename'.
	resolvedPath, resolveErr := get.Resolve(ctx, getter, opts.File, "", get.ResolveOptions{
		Risk: get.DiscoveryRiskHigh,
	})
	if resolveErr != nil {
		return failFix(opts, exitcode.FileNotFound, fmt.Errorf("failed to resolve solution: %w", resolveErr))
	}
	if resolvedPath == "-" {
		return failFix(opts, exitcode.ValidationFailed, errors.New("lint --fix requires a file path; stdin ('-') is not supported"))
	}
	opts.File = resolvedPath

	raw, err := os.ReadFile(resolvedPath) //nolint:gosec // path resolved from user-provided solution reference
	if err != nil {
		return failFix(opts, exitcode.FileNotFound, fmt.Errorf("failed to read %s: %w", resolvedPath, err))
	}

	registry := getRegistry(ctx)
	plan, err := pkglint.ComputeFixPlan(raw, resolvedPath, registry)
	if err != nil {
		return failFix(opts, exitcode.GeneralError, fmt.Errorf("failed to compute fixes: %w", err))
	}

	// --diff is a focused preview: emit only the diff (to stdout) plus the
	// outcome summary (to stderr). No write, no residual render.
	if opts.Diff {
		diff, derr := plan.UnifiedDiff(resolvedPath, raw)
		if derr != nil {
			return failFix(opts, exitcode.GeneralError, fmt.Errorf("failed to render diff: %w", derr))
		}
		if w != nil && diff != "" {
			w.Plain(diff)
		}
		reportFixOutcomes(w, plan, true)
		// Preview modes gate CI: a pending fix is a non-zero exit (ruff --diff
		// parity), so `lint --fix-dry-run` can fail a build that needs fixing.
		return pendingFixesExit(plan)
	}

	write := opts.Fix // reaching here means --diff is not set
	if write && plan.Changed {
		mode := os.FileMode(0o644)
		if fi, statErr := os.Stat(resolvedPath); statErr == nil {
			mode = fi.Mode().Perm()
		}
		if werr := fs.WriteFileAtomic(resolvedPath, plan.NewContent, mode); werr != nil {
			return failFix(opts, exitcode.GeneralError, fmt.Errorf("failed to write %s: %w", resolvedPath, werr))
		}
	}

	reportFixOutcomes(w, plan, !write)

	// Surface what still needs manual attention by linting the post-fix content
	// (in-memory, so --fix-dry-run reflects the would-be result accurately).
	residualErr := renderResidualFindings(ctx, opts, plan.NewContent, registry)
	if residualErr != nil {
		return residualErr
	}
	// --fix-dry-run is a preview: pending fixes gate CI even when no residual
	// errors remain (ruff --check parity). --fix already applied them, so the
	// plan reflects the written result and this is a no-op.
	if !write {
		return pendingFixesExit(plan)
	}
	return nil
}

// pendingFixesExit returns a validation-failed exit when a preview run still has
// fixes to apply, so `--fix-dry-run` / `--diff` can gate CI. It returns nil when
// nothing would change. The error carries only an exit code (root silences the
// message), so a clean preview of pending changes is not printed as an error.
func pendingFixesExit(plan *pkglint.FixPlan) error {
	if plan == nil || !plan.Changed {
		return nil
	}
	return exitcode.WithCode(
		errors.New("fixable findings remain; re-run with --fix to apply"),
		exitcode.ValidationFailed,
	)
}

// reportFixOutcomes writes the per-finding fix summary to stderr so it never
// corrupts structured stdout. dryRun switches the phrasing to the conditional
// ("would fix") used by --fix-dry-run and --diff.
func reportFixOutcomes(w *writer.Writer, plan *pkglint.FixPlan, dryRun bool) {
	if w == nil {
		return
	}

	applied := plan.AppliedCount()
	skipped := plan.SkippedCount()

	if applied == 0 && skipped == 0 {
		w.SuccessStderr("No auto-fixable findings.")
		return
	}

	fixedVerb := "fixed"
	summaryVerb := "Fixed"
	if dryRun {
		fixedVerb = "would fix"
		summaryVerb = "Would fix"
	}

	for _, o := range plan.Outcomes {
		if o.Applied {
			w.SuccessStderrf("  %s: %s", fixedVerb, o.Detail)
		} else {
			w.WarnStderrf("  skipped %s: %s", o.Location, trimNotFixable(o.Detail))
		}
	}
	w.PlainStderr(fmt.Sprintf("%s %d finding(s); %d skipped.", summaryVerb, applied, skipped))
}

// trimNotFixable strips the trailing ErrNotFixable sentinel text from a skip
// reason so the user sees the underlying cause without the internal marker.
func trimNotFixable(detail string) string {
	suffix := ": " + pkglint.ErrNotFixable.Error()
	return strings.TrimSuffix(detail, suffix)
}

// renderResidualFindings lints the post-fix content and renders remaining
// findings through the shared RenderResult path so the fix flow surfaces the
// same output as a plain 'lint'. Findings are rendered to stdout; a non-zero
// error count yields the ValidationFailed exit code.
func renderResidualFindings(ctx context.Context, opts *Options, content []byte, registry *provider.Registry) error {
	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes(content); err != nil {
		writeError(opts, fmt.Sprintf("failed to re-lint fixed content: %v", err))
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	result := pkglint.Solution(sol, opts.File, registry)
	result = pkglint.FilterBySeverity(result, opts.Severity)

	if opts.KvxOutputFlags.Output == "quiet" {
		if result.ErrorCount > 0 {
			return exitcode.WithCode(fmt.Errorf("found %d errors", result.ErrorCount), exitcode.ValidationFailed)
		}
		return nil
	}

	if renderErr := RenderResult(ctx, result, opts.BinaryName+" lint", opts.IOStreams, opts.KvxOutputFlags, opts.CliParams.NoColor); renderErr != nil {
		writeError(opts, fmt.Sprintf("failed to write output: %v", renderErr))
		return renderErr
	}

	if result.ErrorCount > 0 {
		return exitcode.WithCode(fmt.Errorf("found %d errors", result.ErrorCount), exitcode.ValidationFailed)
	}
	return nil
}

// failFix reports err to stderr and returns it wrapped with the given exit code.
func failFix(opts *Options, code int, err error) error {
	writeError(opts, err.Error())
	return exitcode.WithCode(err, code)
}
