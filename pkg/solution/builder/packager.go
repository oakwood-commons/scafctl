// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"errors"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

var (
	// ErrNilCatalog is returned by NewPackager when no local catalog is supplied.
	ErrNilCatalog = errors.New("packager: local catalog is required")

	// ErrNoSource is returned by Do when Solution was not called.
	ErrNoSource = errors.New("packager: solution source is required")

	// ErrInvalidRef is returned by Do when As was not given a built solution
	// reference with a name and version.
	ErrInvalidRef = errors.New("packager: a built solution reference with name and version is required")

	// ErrPreflightBlocked is returned by Do when lint or functional tests
	// found errors and PreflightOptions.IgnorePreflight is not set.
	ErrPreflightBlocked = errors.New("build blocked by pre-flight checks")

	// ErrVerifyFailed is returned by Do when the built bundle fails the
	// completeness policy. Nothing is stored.
	ErrVerifyFailed = errors.New("built bundle failed verification")

	// ErrStoreFailed wraps catalog errors from publishing the artifact.
	// catalog.IsExists still matches an existing-version error.
	ErrStoreFailed = errors.New("failed to store solution")
)

// VendorOptions configures plugin and catalog dependency vendoring.
type VendorOptions struct {
	// Skip disables vendoring (--no-vendor).
	Skip bool
}

// CacheOptions configures the build cache.
type CacheOptions struct {
	// Skip disables the build cache (--no-cache).
	Skip bool
	// Dir is the build cache directory; empty uses
	// settings.DefaultBuildCacheDir.
	Dir string
}

// ArchiveOptions configures the bundle archive.
type ArchiveOptions struct {
	// Limits are the bundle size limits (--bundle-max-size,
	// --dedupe-threshold), parsed by ParseBundleLimits.
	Limits BundleLimits
	// Dedupe enables content-addressable deduplication (--dedupe).
	Dedupe bool
}

// VerifyOptions configures completeness verification of the staged bundle, run before it is stored.
type VerifyOptions struct {
	// Skip disables verification (--no-verify).
	Skip bool
	// Strict also fails the build on completeness warnings (--strict).
	Strict bool
}

// PackageResult holds the output of each pipeline stage. Do returns it on
// error too, holding the stages that ran.
type PackageResult struct {
	// Preflight is the lint/test result; nil when preflight did not run.
	Preflight *PreflightResult
	// Build is the bundle result; nil when the source is not bundled.
	Build *BuildResult
	// Stored is the store result; nil on a cache hit or dry run.
	Stored *StoreResult
	// Verify is the completeness result; nil when verification did not run.
	Verify *bundler.VerifyResult
	// DryRun reports that the pipeline stopped before the first write.
	DryRun bool
	// ComposedOut is the path the composed solution was written to; empty
	// when ComposedOut was not set.
	ComposedOut string
	// Messages are human-readable progress lines, in stage order.
	Messages []string
}

// CacheHit reports that an unchanged built artifact was reused.
func (r *PackageResult) CacheHit() bool {
	return r.Build != nil && r.Build.CacheHit
}

// Packager runs the solution package pipeline against a single local catalog
// handle. Every stage that reads or writes the catalog (version bump, plugin
// resolution, vendoring, cache-hit confirmation, store, verify) shares this
// handle instead of opening its own.
//
// Configure it with the chained setters, then call Do:
//
//	res, err := p.
//		Solution(src).
//		As(ref).
//		Preflight(pfOpts).
//		Vendor(vendorOpts).
//		Cache(cacheOpts).
//		Archive(archiveOpts).
//		ComposedOut(path).
//		DryRun(dryRun).
//		Store(force).
//		Verify(verifyOpts).
//		Do(ctx)
//
// Flag mapping for `package solution`:
//
//	-f, --base-dir, --no-bundle              -> Solution (LoadSolutionSource)
//	--name, --version, --tag, --bump,
//	  --allow-dev-version                    -> As (ResolveIdentity)
//	--no-git-metadata, --allow-dirty         -> ResolveGitMetadata, before As
//	--force (rename)                         -> StampSolution, before As
//	--skip-lint, --skip-tests,
//	  --ignore-preflight                     -> Preflight
//	--no-vendor                              -> Vendor
//	--no-cache                               -> Cache
//	--dedupe, --bundle-max-size,
//	  --dedupe-threshold                     -> Archive
//	--composed-out                           -> ComposedOut
//	--dry-run                                -> DryRun
//	--force (overwrite)                      -> Store
//	--no-verify, --strict                    -> Verify
//	-o/--output                              -> CLI renders PackageResult
type Packager struct {
	catalog     *catalog.LocalCatalog
	src         *SolutionSource
	ref         catalog.Reference
	preflight   *PreflightOptions
	vendor      VendorOptions
	cache       CacheOptions
	archive     ArchiveOptions
	composedOut string
	dryRun      bool
	force       bool
	verify      VerifyOptions
}

// NewPackager returns a Packager bound to localCatalog.
func NewPackager(localCatalog *catalog.LocalCatalog) (*Packager, error) {
	if localCatalog == nil {
		return nil, ErrNilCatalog
	}
	return &Packager{
		catalog: localCatalog,
		archive: ArchiveOptions{Limits: BundleLimits{
			MaxSize:         bundler.DefaultMaxBundleSize,
			DedupeThreshold: bundler.DefaultDedupeThreshold,
		}},
	}, nil
}

// Catalog returns the local catalog the Packager reads from and writes to.
func (p *Packager) Catalog() *catalog.LocalCatalog {
	return p.catalog
}

// Solution sets the loaded source: the solution, its raw content, and the
// bundle root.
func (p *Packager) Solution(src *SolutionSource) *Packager {
	p.src = src
	return p
}

// As sets the artifact identity, as resolved by ResolveIdentity.
func (p *Packager) As(ref catalog.Reference) *Packager {
	p.ref = ref
	return p
}

// Preflight enables lint and functional tests. Skipped on dry run.
func (p *Packager) Preflight(opts PreflightOptions) *Packager {
	p.preflight = &opts
	return p
}

// Vendor configures dependency vendoring. Only runs when the source
// is bundled (see SolutionSource.ShouldBundle).
func (p *Packager) Vendor(opts VendorOptions) *Packager {
	p.vendor = opts
	return p
}

// Cache configures the build cache.
func (p *Packager) Cache(opts CacheOptions) *Packager {
	p.cache = opts
	return p
}

// Archive configures the bundle archive.
func (p *Packager) Archive(opts ArchiveOptions) *Packager {
	p.archive = opts
	return p
}

// ComposedOut writes the composed solution document to path.
// Empty disables it.
func (p *Packager) ComposedOut(path string) *Packager {
	p.composedOut = path
	return p
}

// DryRun stops the pipeline before the first write.
func (p *Packager) DryRun(dryRun bool) *Packager {
	p.dryRun = dryRun
	return p
}

// Store sets whether an existing artifact with the same name and version
// is overwritten.
func (p *Packager) Store(force bool) *Packager {
	p.force = force
	return p
}

// Verify configures completeness verification of the staged bundle, run before it is stored.
func (p *Packager) Verify(opts VerifyOptions) *Packager {
	p.verify = opts
	return p
}

// Do validates the configuration, then runs the enabled stages in order
// until one fails or finishes the pipeline early (dry run, cache hit). On
// error the result holds the stages that ran.
func (p *Packager) Do(ctx context.Context) (*PackageResult, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	lgr := logger.FromContext(ctx)
	st := p.newState()
	for _, s := range p.stages() {
		if !s.when(st) {
			continue
		}
		lgr.V(1).Info("package stage", "stage", s.name)
		done, err := s.run(ctx, st)
		if err != nil {
			return st.result, err
		}
		if done {
			break
		}
	}
	return st.result, nil
}

// validate checks the configuration set by the chained setters.
func (p *Packager) validate() error {
	if p.src == nil || p.src.MaterializedSolution == nil || p.src.Solution == nil {
		return ErrNoSource
	}
	if p.ref.Kind != catalog.ArtifactKindSolution || !p.ref.Built || p.ref.Name == "" || p.ref.Version == nil {
		return ErrInvalidRef
	}
	return nil
}
