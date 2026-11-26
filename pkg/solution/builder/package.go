// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/git"
	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// PackageSolutionOptions configures a domain-level solution packaging or
// dry-run bundle verification run.
type PackageSolutionOptions struct {
	// Path is the local solution file path (empty auto-discovers from the working directory).
	Path string
	// BaseDir overrides the bundle root directory used to resolve relative paths.
	BaseDir string
	// Name overrides the artifact name (default: metadata.name or file stem).
	Name string
	// Version overrides the semantic version (default: metadata.version or auto-increment).
	Version string
	// DryRun stops before writing vendored files, lock files, or catalog artifacts.
	DryRun bool
	// NoBundle skips bundling and stores only the solution YAML.
	NoBundle bool
	// NoVendor skips catalog dependency vendoring and plugin pinning.
	NoVendor bool
	// Force permits overriding metadata.name and overwriting an existing built version.
	Force bool
	// Strict fails when bundle verification or dry-run discovery emits warnings.
	Strict bool
	// SkipLint skips the pre-flight lint check.
	SkipLint bool
	// Registry is the provider registry used for lint validation; defaults to builtin providers.
	Registry *provider.Registry
	// Catalog optionally injects a LocalCatalog (for example in tests); when nil,
	// catalog.NewLocalCatalog is opened from the configured XDG data path.
	Catalog *catalog.LocalCatalog
	// CacheDir optionally overrides the build-cache directory; empty uses default.
	CacheDir string
}

// PackageSolutionResult holds the structured build/dry-run report alongside
// pre-flight lint findings and dry-run bundle discovery warnings.
type PackageSolutionResult struct {
	*PackageReport `yaml:",inline"`

	// Warnings lists non-fatal bundle discovery warnings (such as unmatched
	// bundle.include globs, unbundled references, and dynamic file paths).
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Non-fatal bundle discovery and verification warnings" maxItems:"1000"`

	// LintResult holds pre-flight lint findings when lint ran.
	LintResult *lint.Result `json:"lintResult,omitempty" yaml:"lintResult,omitempty" doc:"Pre-flight lint findings when lint ran"`

	// Messages holds stage progress lines emitted by the packager.
	Messages []string `json:"messages,omitempty" yaml:"messages,omitempty" doc:"Stage progress lines emitted during packaging" maxItems:"1000"`
}

// PackageSolution loads, composes, validates, discovers bundle files, and (when
// DryRun is false) packages and stores a solution into the local catalog.
//
// NewPackageReport's dry-run contract (Verification == nil) is preserved;
// dry-run unmatched bundle.include globs and dynamic file path diagnostics are
// surfaced in PackageSolutionResult.Warnings.
func PackageSolution(ctx context.Context, opts PackageSolutionOptions) (*PackageSolutionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	idOpts := IdentityOptions{
		Name:    opts.Name,
		Version: opts.Version,
	}
	if err := idOpts.Validate(); err != nil {
		return nil, err
	}

	lgr := *logger.FromContext(ctx)
	cat := opts.Catalog
	if cat == nil {
		var err error
		cat, err = catalog.NewLocalCatalog(lgr)
		if err != nil {
			return nil, fmt.Errorf("failed to open catalog: %w", err)
		}
	}

	src, err := LoadSolutionSource(ctx, opts.Path, opts.BaseDir, nil, WithNoBundle(opts.NoBundle))
	if err != nil {
		return nil, err
	}

	id, err := ResolveIdentity(ctx, cat, src, idOpts)
	if err != nil {
		return nil, err
	}

	gitMeta, gitErr := ResolveGitMetadata(ctx, git.GetRepositoryStatus, src, !opts.DryRun)
	if gitErr != nil {
		lgr.V(1).Info("git metadata unavailable", "error", gitErr)
	}
	if _, err := src.Stamp(id, gitMeta, StampOptions{AllowRename: opts.Force}); err != nil {
		return nil, err
	}

	reg := opts.Registry
	if !opts.SkipLint && reg == nil {
		if defaultReg, regErr := builtin.DefaultRegistry(ctx); regErr == nil {
			reg = defaultReg
		} else {
			reg = provider.GetGlobalRegistry()
		}
	}
	pfOpts := PreflightOptions{
		SkipLint:  opts.SkipLint,
		SkipTests: true,
		Registry:  reg,
		Logger:    lgr,
	}

	packager, err := NewPackager(cat)
	if err != nil {
		return nil, err
	}

	pkgRes, pkgErr := packager.
		Solution(src).
		As(id.Ref).
		Preflight(pfOpts).
		Vendor(VendorOptions{Skip: opts.NoVendor}).
		Cache(CacheOptions{Dir: opts.CacheDir}).
		Archive(ArchiveOptions{
			Limits: BundleLimits{
				MaxSize:         bundler.DefaultMaxBundleSize,
				DedupeThreshold: bundler.DefaultDedupeThreshold,
			},
			Dedupe: true,
		}).
		DryRun(opts.DryRun).
		Store(opts.Force).
		Verify(VerifyOptions{Strict: opts.Strict}).
		Do(ctx)
	if pkgRes == nil {
		pkgRes = &PackageResult{DryRun: opts.DryRun}
	}

	var lintRes *lint.Result
	if pkgRes.Preflight != nil {
		lintRes = pkgRes.Preflight.LintResult
	}

	// Packager skips verification on a build-cache hit; reverify the cached
	// artifact against the requested policy so an earlier non-strict build never
	// bypasses Strict verification.
	if pkgErr == nil && !opts.DryRun && pkgRes.CacheHit() && pkgRes.Build != nil && pkgRes.Build.CacheEntry != nil {
		vr, verifyErr := verifyCachedArtifact(ctx, cat, pkgRes.Build.CacheEntry, lgr)
		if verifyErr != nil {
			pkgErr = verifyErr
		} else {
			pkgRes.Verify = vr
			pkgErr = (VerifyOptions{Strict: opts.Strict}).check(vr)
		}
	}

	// Packager skips preflight during dry-run; run lint after bundle discovery so
	// callers still receive lint validation unless SkipLint is set.
	if pkgErr == nil && opts.DryRun && !opts.SkipLint {
		pf, pfErr := RunPreflight(ctx, src.Solution, src.Path(), pfOpts)
		if pfErr != nil {
			return nil, fmt.Errorf("running pre-flight checks: %w", pfErr)
		}
		lintRes = pf.LintResult
		if pf.Blocked {
			pkgErr = ErrPreflightBlocked
		}
	}

	completenessWarnings, warnings := collectBundleWarnings(ctx, src.Solution, pkgRes, opts.DryRun, opts.NoBundle)

	reportIn := PackageReportInput{
		Name:     id.Ref.Name,
		Version:  id.Ref.Version.String(),
		Solution: src.Solution,
		Build:    pkgRes.Build,
		Verify:   pkgRes.Verify,
		DryRun:   pkgRes.DryRun || opts.DryRun,
	}
	if !reportIn.DryRun {
		reportIn.CatalogPath = cat.Path()
	}
	if pkgRes.Stored != nil {
		reportIn.Info = &pkgRes.Stored.Info
	}

	out := &PackageSolutionResult{
		PackageReport: NewPackageReport(reportIn),
		Warnings:      warnings,
		LintResult:    lintRes,
		Messages:      pkgRes.Messages,
	}

	if pkgErr != nil {
		return out, pkgErr
	}
	if opts.DryRun && opts.Strict && len(completenessWarnings) > 0 {
		return out, &verifyPolicyError{
			msg: fmt.Sprintf("bundle verification has %d warning(s) (strict)", len(completenessWarnings)),
		}
	}
	return out, nil
}

func verifyCachedArtifact(ctx context.Context, cat *catalog.LocalCatalog, entry *bundler.BuildCacheEntry, lgr logr.Logger) (*bundler.VerifyResult, error) {
	ver, err := semver.NewVersion(entry.ArtifactVersion)
	if err != nil {
		return nil, fmt.Errorf("parsing cached artifact version %q: %w", entry.ArtifactVersion, err)
	}
	ref := BuiltSolutionRef(entry.ArtifactName, ver)
	content, bundleData, _, err := cat.FetchWithBundle(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("fetching cached artifact for verification: %w", err)
	}
	var cachedSol solution.Solution
	if err := cachedSol.LoadFromBytes(content); err != nil {
		return nil, fmt.Errorf("parsing cached solution for verification: %w", err)
	}
	vr, err := bundler.VerifyBundle(ctx, &cachedSol, bundleData, lgr)
	if err != nil {
		return nil, fmt.Errorf("verifying cached bundle: %w", err)
	}
	return vr, nil
}

// collectBundleWarnings separates bundle completeness warnings (unmatched
// bundle.include globs across the root and recursively discovered local child
// solutions, or unbundled file/catalog references under NoBundle, which fail
// Strict mode) from informational dynamic path reminders.
func collectBundleWarnings(ctx context.Context, sol *solution.Solution, pkgRes *PackageResult, dryRun, noBundle bool) ([]string, []string) {
	var completeness []string
	seenCompleteness := make(map[string]bool)
	addCompleteness := func(w string) {
		if !seenCompleteness[w] {
			seenCompleteness[w] = true
			completeness = append(completeness, w)
		}
	}

	if dryRun && sol != nil {
		switch {
		case noBundle:
			if vr, err := bundler.VerifyBundle(ctx, sol, nil, logr.Discard()); err == nil && vr != nil {
				for _, w := range vr.Warnings {
					addCompleteness(w)
				}
			}
		case pkgRes != nil && pkgRes.Build != nil && pkgRes.Build.Discovery != nil:
			for _, ui := range pkgRes.Build.Discovery.UnmatchedIncludes {
				addCompleteness(ui.String())
			}
		}
	}

	if !dryRun && pkgRes != nil && pkgRes.Verify != nil {
		for _, w := range pkgRes.Verify.Warnings {
			addCompleteness(w)
		}
	}

	var dynamicWarnings []string
	if !noBundle && pkgRes != nil && pkgRes.Build != nil && pkgRes.Build.Discovery != nil {
		for _, dw := range pkgRes.Build.Discovery.DynamicPaths {
			dynamicWarnings = append(dynamicWarnings, fmt.Sprintf("dynamic path in %s (%s: %s); ensure covered by bundle.include", dw.Location, dw.Kind, dw.Expression))
		}
	}

	all := make([]string, 0, len(completeness)+len(dynamicWarnings))
	all = append(all, completeness...)
	all = append(all, dynamicWarnings...)

	return completeness, all
}
