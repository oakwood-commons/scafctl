// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package packagecmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// preflightBlockedHint tells the user how to get past failed pre-flight checks.
const preflightBlockedHint = "build blocked by pre-flight checks (use --skip-lint/--skip-tests to skip, or --ignore-preflight to override)"

// packageRenderer prints the outcome of a package run and maps pipeline
// errors to exit codes.
type packageRenderer struct {
	ctx         context.Context
	w           *writer.Writer
	opts        *SolutionOptions
	catalogPath string
	src         *builder.SolutionSource
	id          *builder.Identity
}

// render prints the progress collected in res, then the outcome: a failure,
// a cache hit, a dry run, or a stored artifact. res is nil only when the
// Packager rejected its configuration.
func (r *packageRenderer) render(res *builder.PackageResult, err error) error {
	if res == nil {
		res = &builder.PackageResult{}
	}
	r.progress(res)
	switch {
	case err != nil:
		return r.failure(res, err)
	case res.CacheHit():
		return r.cacheHit(res)
	case res.DryRun:
		return r.dryRun(res)
	default:
		return r.stored(res)
	}
}

// progress prints the status lines the pipeline collected, in stage order.
func (r *packageRenderer) progress(res *builder.PackageResult) {
	if res.Preflight != nil {
		for _, msg := range res.Preflight.Messages {
			r.w.Infof("  %s", msg)
		}
	}
	for _, msg := range res.Messages {
		r.w.Infof("  %s", msg)
	}
	if res.Build != nil && res.Build.Discovery != nil {
		r.w.Verbosef("Discovered %d local file(s), %d catalog ref(s)",
			len(res.Build.Discovery.LocalFiles), len(res.Build.Discovery.CatalogRefs))
	}
	if res.ComposedOut != "" {
		r.w.SuccessStderrf("Composed solution written: %s", res.ComposedOut)
	}
}

// failure prints a pipeline error and maps it to an exit code.
func (r *packageRenderer) failure(res *builder.PackageResult, err error) error {
	switch {
	case errors.Is(err, builder.ErrPreflightBlocked):
		r.w.Error(preflightBlockedHint)
		return exitcode.WithCode(err, exitcode.GeneralError)
	case errors.Is(err, builder.ErrVerifyFailed):
		return r.verifyFailed(res, err)
	case errors.Is(err, builder.ErrStoreFailed) && catalog.IsExists(err):
		r.w.Errorf("%v\nUse --force to overwrite", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	case errors.Is(err, builder.ErrStoreFailed):
		return fail(r.w, err, exitcode.CatalogError)
	default:
		return fail(r.w, err, exitcode.GeneralError)
	}
}

// verifyFailed prints why the bundle failed verification. Nothing was
// stored, but a report is still emitted so machine consumers see the
// verification errors alongside the non-zero exit.
func (r *packageRenderer) verifyFailed(res *builder.PackageResult, err error) error {
	cmdutil.RenderVerifyResult(r.w, res.Verify, false)
	r.w.Errorf("%v", err)
	if rerr := emitPackageReport(r.ctx, r.opts, r.reportInput(res)); rerr != nil {
		logger.FromContext(r.ctx).V(1).Info("failed to emit failure report", "error", rerr)
	}
	return exitcode.WithCode(err, exitcode.GeneralError)
}

// cacheHit prints the reused artifact.
func (r *packageRenderer) cacheHit(res *builder.PackageResult) error {
	entry := res.Build.CacheEntry
	r.w.Successf("Build cache hit: %s@%s (unchanged)", entry.ArtifactName, entry.ArtifactVersion)
	r.w.Infof("  Digest: %s", entry.ArtifactDigest)
	r.w.Infof("  Use --no-cache to force a full rebuild")
	r.w.Verbosef("Fingerprint: %s (%d input files)", entry.Fingerprint, entry.InputFiles)
	return emitPackageReport(r.ctx, r.opts, r.reportInput(res))
}

// dryRun prints what would be built.
func (r *packageRenderer) dryRun(res *builder.PackageResult) error {
	if r.opts.Output == "" && res.Build != nil && res.Build.Discovery != nil {
		printDryRunOutput(r.w, dryRunView{
			file:      r.sourceName(),
			solution:  r.src.Solution,
			compose:   r.src.ComposeFiles(),
			discovery: res.Build.Discovery,
			noVendor:  r.opts.NoVendor,
		})
	}
	r.w.Infof("Dry run: would build %s@%s", r.id.Ref.Name, r.id.Ref.Version)
	return emitPackageReport(r.ctx, r.opts, r.reportInput(res))
}

// stored prints the verification result and the stored artifact.
func (r *packageRenderer) stored(res *builder.PackageResult) error {
	if res.Verify != nil {
		cmdutil.RenderVerifyResult(r.w, res.Verify, false)
	}
	info := res.Stored.Info
	r.w.Successf("Built %s@%s", info.Reference.Name, info.Reference.Version)
	r.w.Infof("  Digest: %s", info.Digest)
	r.w.Infof("  Catalog: %s", r.catalogPath)
	if res.Build != nil && res.Build.BuildFingerprint != "" {
		r.w.Verbosef("Fingerprint: %s (%d input files)", res.Build.BuildFingerprint, res.Build.InputFileCount)
	}
	if r.opts.NoVerify {
		r.w.Verbose("Skipping build-cache entry (--no-verify): unverified artifacts are not cached")
	}
	return emitPackageReport(r.ctx, r.opts, r.reportInput(res))
}

// sourceName names the solution in the dry-run summary.
func (r *packageRenderer) sourceName() string {
	if r.src.IsStdin() {
		return builder.StdinPath
	}
	return r.src.Path()
}

// reportInput collects the machine-readable report fields for res.
func (r *packageRenderer) reportInput(res *builder.PackageResult) builder.PackageReportInput {
	in := builder.PackageReportInput{
		Name:            r.id.Ref.Name,
		Version:         r.id.Ref.Version.String(),
		Solution:        r.src.Solution,
		Build:           res.Build,
		Verify:          res.Verify,
		DryRun:          res.DryRun,
		IncludeSolution: true,
	}
	if !res.DryRun {
		in.CatalogPath = r.catalogPath
	}
	if res.Stored != nil {
		in.Info = &res.Stored.Info
	}
	return in
}

// emitPackageReport writes a machine-readable package report to stdout when a
// structured output format is requested. It is a no-op when -o/--output is unset.
func emitPackageReport(ctx context.Context, opts *SolutionOptions, in builder.PackageReportInput) error {
	if opts.Output == "" {
		return nil
	}
	report := builder.NewPackageReport(in)
	if err := output.WriteOutput(opts.IOStreams, opts.Output, report, nil); err != nil {
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("failed to write %s output: %v", opts.Output, err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	return nil
}

// dryRunView is what the dry-run summary describes.
type dryRunView struct {
	// file names the solution being packaged.
	file string
	// solution is the solution as it would be stored.
	solution *solution.Solution
	// compose are the compose: parts merged into the solution.
	compose []string
	// discovery is what the bundle would contain.
	discovery *bundler.DiscoveryResult
	// noVendor is --no-vendor.
	noVendor bool
}

// printDryRunOutput produces a formatted summary of what would be bundled.
func printDryRunOutput(w *writer.Writer, v dryRunView) {
	sol, discovery := v.solution, v.discovery

	w.Plainf("")
	w.Plainf("Bundle analysis for %s:", v.file)
	w.Plainf("")

	// Composed files
	if len(v.compose) > 0 {
		w.Plainf("  Composed files:")
		for _, cf := range v.compose {
			w.Plainf("    %s  → merged into solution", cf)
		}
		w.Plainf("")
	}

	// Static analysis discovered files
	var staticFiles, explicitFiles, testFiles []bundler.FileEntry
	for _, f := range discovery.LocalFiles {
		switch f.Source {
		case bundler.StaticAnalysis:
			staticFiles = append(staticFiles, f)
		case bundler.ExplicitInclude:
			explicitFiles = append(explicitFiles, f)
		case bundler.TestInclude:
			testFiles = append(testFiles, f)
		}
	}

	if len(staticFiles) > 0 {
		w.Plainf("  Static analysis discovered:")
		for _, f := range staticFiles {
			w.Plainf("    %s", f.RelPath)
		}
		w.Plainf("")
	}

	// Explicit includes
	if len(explicitFiles) > 0 {
		w.Plainf("  Explicit includes (bundle.include):")
		for _, f := range explicitFiles {
			w.Plainf("    %s", f.RelPath)
		}
		w.Plainf("")
	}

	// Test file includes
	if len(testFiles) > 0 {
		w.Plainf("  Test file includes:")
		for _, f := range testFiles {
			w.Plainf("    %s", f.RelPath)
		}
		w.Plainf("")
	}

	// Catalog references
	if len(discovery.CatalogRefs) > 0 {
		if v.noVendor {
			w.Plainf("  Catalog references (not vendored):")
		} else {
			w.Plainf("  Vendored catalog dependencies:")
		}
		for _, ref := range discovery.CatalogRefs {
			w.Plainf("    %s  → %s", ref.Ref, ref.VendorPath)
		}
		w.Plainf("")
	}

	// Plugin dependencies
	if len(sol.Bundle.Plugins) > 0 {
		w.Plainf("  Plugin dependencies:")
		for _, p := range sol.Bundle.Plugins {
			defaults := ""
			if len(p.Defaults) > 0 {
				keys := make([]string, 0, len(p.Defaults))
				for k := range p.Defaults {
					keys = append(keys, k)
				}
				defaults = fmt.Sprintf("   defaults: %s", strings.Join(keys, ", "))
			}
			w.Plainf("    %s (%s)        %s%s", p.DisplayName(), p.Kind, p.Version, defaults)
		}
		w.Plainf("")
	}

	// Dynamic path warnings
	dynamicWarnings := bundler.DetectDynamicPaths(sol)
	if len(dynamicWarnings) > 0 {
		w.Warningf("Dynamic paths detected (ensure these are covered by bundle.include):")
		for _, dw := range dynamicWarnings {
			w.Plainf("    %s: %s in %s", dw.Location, dw.Kind, dw.Expression)
		}
		w.Plainf("")
	}

	// Summary
	total := len(discovery.LocalFiles)
	vendored := len(discovery.CatalogRefs)
	pluginCount := len(sol.Bundle.Plugins)
	w.Plainf("  Total: %d bundled file(s)", total)
	if vendored > 0 {
		w.Plainf("       + %d vendored dependency(ies)", vendored)
	}
	if pluginCount > 0 {
		w.Plainf("       + %d plugin(s)", pluginCount)
	}
	w.Plainf("")
}
