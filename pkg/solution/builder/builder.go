// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package builder provides the build pipeline for composing, discovering,
// vendoring, and bundling solution artifacts. This is the shared domain layer
// used by CLI, MCP, and future API consumers.
package builder

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// BuildBundleOptions holds configuration for the build bundle pipeline.
type BuildBundleOptions struct {
	// BundleMaxSize is the maximum total size for bundled files (e.g., "50MB").
	BundleMaxSize string `json:"bundleMaxSize,omitempty" yaml:"bundleMaxSize,omitempty" doc:"Maximum total size for bundled files"`

	// NoVendor skips catalog dependency vendoring.
	NoVendor bool `json:"noVendor,omitempty" yaml:"noVendor,omitempty" doc:"Skip catalog dependency vendoring"`

	// NoCache skips the build cache and forces a full rebuild.
	NoCache bool `json:"noCache,omitempty" yaml:"noCache,omitempty" doc:"Skip build cache and force a full rebuild"`

	// DryRun previews what would be bundled without writing anything.
	DryRun bool `json:"dryRun,omitempty" yaml:"dryRun,omitempty" doc:"Show what would be bundled without storing"`

	// Dedupe enables content-addressable deduplication.
	Dedupe bool `json:"dedupe,omitempty" yaml:"dedupe,omitempty" doc:"Enable content-addressable deduplication"`

	// DedupeThreshold is the minimum file size for individual layer extraction.
	DedupeThreshold string `json:"dedupeThreshold,omitempty" yaml:"dedupeThreshold,omitempty" doc:"Minimum file size for individual layer extraction"`

	// Logger is used for structured logging during the build.
	Logger logr.Logger
}

// BuildResult holds the output of the build bundle pipeline.
type BuildResult struct {
	// TarData is the traditional single tar archive (v1).
	TarData []byte

	// Dedup is the content-addressable dedup result (v2).
	Dedup *bundler.DedupeResult

	// CacheHit indicates the build was served from the build cache.
	// When true, the artifact already exists in the catalog and no store is needed.
	CacheHit bool

	// CacheEntry contains the cache metadata when CacheHit is true.
	CacheEntry *bundler.BuildCacheEntry

	// BuildFingerprint is the computed fingerprint for cache write after a successful build.
	BuildFingerprint string

	// BuildCacheDir is the directory where build cache entries are stored.
	BuildCacheDir string

	// InputFileCount is the number of input files that contributed to the fingerprint.
	InputFileCount int

	// ResolvedPlugins holds plugin lock entries from VendorPlugins,
	// to be merged into the lock file during the store step.
	ResolvedPlugins []bundler.LockPlugin

	// Discovery holds the file discovery results (always populated).
	Discovery *bundler.DiscoveryResult

	// Messages collects human-readable progress messages generated during
	// the pipeline. CLI consumers should display these to the user.
	Messages []string
}

// BuildBundle runs the compose → discover → vendor → tar/dedup pipeline.
//
// The solution (sol) may be mutated by the compose step. solutionContent is the
// raw YAML bytes of the original solution file. bundleRoot is the directory
// containing the solution file.
func BuildBundle(ctx context.Context, sol *solution.Solution, solutionContent []byte, bundleRoot string, opts BuildBundleOptions) (*BuildResult, error) {
	lgr := opts.Logger
	result := &BuildResult{}

	// Parse max bundle size
	maxSize, err := ParseByteSize(opts.BundleMaxSize)
	if err != nil {
		return nil, fmt.Errorf("invalid bundle max size: %w", err)
	}

	// Step 1: Compose multi-file solutions
	if len(sol.Compose) > 0 {
		lgr.V(1).Info("composing solution", "files", sol.Compose)
		composed, err := bundler.Compose(sol, bundleRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to compose solution: %w", err)
		}
		*sol = *composed
		result.Messages = append(result.Messages, fmt.Sprintf("Composed %d file(s) into solution", len(sol.Compose)+1))
	}

	// Step 2: Load .scafctlignore
	ignoreChecker, err := bundler.LoadScafctlIgnore(bundleRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load .scafctlignore: %w", err)
	}

	// Step 3: Discover files via static analysis + glob expansion
	discovery, err := bundler.DiscoverFiles(sol, bundleRoot, bundler.WithIgnoreChecker(ignoreChecker))
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}
	result.Discovery = discovery

	lgr.V(1).Info("discovered files",
		"localFiles", len(discovery.LocalFiles),
		"catalogRefs", len(discovery.CatalogRefs))

	// Step 3.5: Validate plugin dependencies
	if len(sol.Bundle.Plugins) > 0 {
		if err := bundler.ValidatePlugins(sol); err != nil {
			return nil, fmt.Errorf("plugin validation failed: %w", err)
		}
		lgr.V(1).Info("validated plugin dependencies", "count", len(sol.Bundle.Plugins))

		// Merge plugin defaults into provider inputs (before DAG construction)
		bundler.MergePluginDefaults(sol)
	}

	// Step 3.6: Vendor plugin dependencies (resolve versions and pin in lock file)
	lockPath := filepath.Join(bundleRoot, "solution.lock")

	// Create catalog registry for dependency and plugin resolution.
	// This is used by both plugin vendoring (Step 3.6) and catalog dependency vendoring (Step 4).
	localCat, catErr := catalog.NewLocalCatalog(lgr)
	if catErr != nil {
		return nil, fmt.Errorf("failed to open catalog: %w", catErr)
	}
	registry := catalog.NewRegistryWithLocal(localCat, lgr)
	registry.SetCacheRemoteArtifacts(true)

	if !opts.NoVendor && len(sol.Bundle.Plugins) > 0 {
		lgr.V(1).Info("vendoring plugin dependencies", "count", len(sol.Bundle.Plugins))

		// Load existing lock for plugin replay
		existingLock, _ := bundler.LoadLockFile(lockPath)

		// Create a plugin resolver from the catalog registry
		pluginResolver := &CatalogPluginResolver{Catalog: localCat}

		pluginResult, pluginErr := bundler.VendorPlugins(ctx, sol.Bundle.Plugins, existingLock, bundler.VendorPluginsOptions{
			PluginResolver: pluginResolver,
		})
		if pluginErr != nil {
			// Plugin resolution failure is a warning, not a hard error —
			// plugins may not be in the catalog yet (e.g., loaded from disk at runtime)
			lgr.V(1).Info("plugin vendoring skipped (non-fatal)", "error", pluginErr)
		} else if len(pluginResult.ResolvedPlugins) > 0 {
			// Store plugin entries for the lock file — they'll be written
			// together with dependency entries in Step 4
			result.ResolvedPlugins = pluginResult.ResolvedPlugins
			result.Messages = append(result.Messages, fmt.Sprintf("Pinned %d plugin(s) in lock file", len(pluginResult.ResolvedPlugins)))
		}
	}

	// Step 4: Vendor catalog dependencies
	if !opts.NoVendor && len(discovery.CatalogRefs) > 0 {
		lgr.V(1).Info("vendoring catalog dependencies", "count", len(discovery.CatalogRefs))

		vendorDir := filepath.Join(bundleRoot, ".scafctl", "vendor")

		vendorResult, err := bundler.VendorDependencies(ctx, sol, discovery.CatalogRefs, bundler.VendorOptions{
			BundleRoot:     bundleRoot,
			VendorDir:      vendorDir,
			LockPath:       lockPath,
			CatalogFetcher: &RegistryFetcherAdapter{Registry: registry},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to vendor catalog dependencies: %w", err)
		}

		// Append resolved plugins to the lock file
		if vendorResult.Lock != nil && len(result.ResolvedPlugins) > 0 {
			vendorResult.Lock.Plugins = append(vendorResult.Lock.Plugins, result.ResolvedPlugins...)
			// Re-write the lock file with plugin entries included
			if err := bundler.WriteLockFile(lockPath, vendorResult.Lock); err != nil {
				lgr.V(1).Info("failed to update lock file with plugins (non-fatal)", "error", err)
			}
		}

		// Add vendored files to the discovery result
		for _, vf := range vendorResult.VendoredFiles {
			discovery.LocalFiles = append(discovery.LocalFiles, bundler.FileEntry{
				RelPath: vf,
				Source:  bundler.ExplicitInclude,
			})
		}

		result.Messages = append(result.Messages, fmt.Sprintf("Vendored %d catalog dependency(ies)", len(vendorResult.VendoredFiles)))
	} else if len(result.ResolvedPlugins) > 0 {
		// No catalog deps, but we have resolved plugins — write a plugin-only lock file
		pluginLock := &bundler.LockFile{
			Version: bundler.LockFileVersion,
			Plugins: result.ResolvedPlugins,
		}
		if err := bundler.WriteLockFile(lockPath, pluginLock); err != nil {
			lgr.V(1).Info("failed to write plugin lock file (non-fatal)", "error", err)
		}
	}

	// Step 4.5: Build cache check
	// Compute fingerprint from all build inputs and check if we have a cached result.
	var buildCacheDir string
	var buildFingerprint string
	if !opts.NoCache && !opts.DryRun {
		buildCacheDir = settings.DefaultBuildCacheDir()

		// Collect plugin entries for fingerprinting
		var fpPlugins []bundler.BundlePluginEntry
		for _, p := range sol.Bundle.Plugins {
			fpPlugins = append(fpPlugins, bundler.BundlePluginEntry{
				Name:    p.Name,
				Kind:    string(p.Kind),
				Version: p.Version,
			})
		}

		// Compute lock file digest for fingerprinting
		lockDigest := ""
		if lockData, lockErr := os.ReadFile(lockPath); lockErr == nil {
			lockDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(lockData))
		}

		fp, fpErr := bundler.ComputeBuildFingerprint(solutionContent, bundleRoot, discovery.LocalFiles, fpPlugins, lockDigest)
		if fpErr != nil {
			lgr.V(1).Info("failed to compute build fingerprint (non-fatal)", "error", fpErr)
		} else {
			buildFingerprint = fp
			cacheEntry, hit := bundler.CheckBuildCache(buildCacheDir, fp)
			if hit {
				lgr.V(1).Info("build cache hit",
					"fingerprint", fp,
					"artifact", cacheEntry.ArtifactName,
					"version", cacheEntry.ArtifactVersion)
				return &BuildResult{CacheHit: true, CacheEntry: cacheEntry, Discovery: discovery}, nil
			}
			lgr.V(1).Info("build cache miss", "fingerprint", fp)
		}
	}

	// Step 5: Dry-run — return discovery for the caller to format
	if opts.DryRun {
		result.BuildFingerprint = buildFingerprint
		result.BuildCacheDir = buildCacheDir
		return result, nil
	}

	// Step 6: No files to bundle — return fingerprint info for cache but no tar data
	if len(discovery.LocalFiles) == 0 {
		lgr.V(1).Info("no files to bundle")
		if buildFingerprint != "" {
			result.BuildFingerprint = buildFingerprint
			result.BuildCacheDir = buildCacheDir
			result.InputFileCount = 0
			return result, nil
		}
		return nil, nil
	}

	// Step 7: Collect plugin entries from bundle.plugins
	var plugins []bundler.BundlePluginEntry
	for _, p := range sol.Bundle.Plugins {
		plugins = append(plugins, bundler.BundlePluginEntry{
			Name:    p.Name,
			Kind:    string(p.Kind),
			Version: p.Version,
		})
	}

	// Step 8: Create bundle (dedup v2 or tar v1)
	if opts.Dedupe {
		dedupeThreshold, err := ParseByteSize(opts.DedupeThreshold)
		if err != nil {
			return nil, fmt.Errorf("invalid dedupe threshold: %w", err)
		}

		dedupeResult, err := bundler.CreateDeduplicatedBundle(bundleRoot, discovery.LocalFiles, plugins,
			bundler.WithDedupeThreshold(dedupeThreshold),
			bundler.WithDedupeMaxSize(maxSize))
		if err != nil {
			return nil, fmt.Errorf("failed to create deduplicated bundle: %w", err)
		}

		result.Dedup = dedupeResult
		result.BuildFingerprint = buildFingerprint
		result.BuildCacheDir = buildCacheDir
		result.InputFileCount = len(discovery.LocalFiles)
		result.Messages = append(result.Messages, fmt.Sprintf("Bundled %d file(s) (%s, deduplicated: %d layer(s))",
			len(dedupeResult.Manifest.Files),
			FormatByteSize(dedupeResult.TotalSize),
			len(dedupeResult.LargeBlobs)+1)) // +1 for small files tar if present

		return result, nil
	}

	// Non-dedup path: create v1 tar
	tarData, manifest, err := bundler.CreateBundleTar(bundleRoot, discovery.LocalFiles, plugins,
		bundler.WithMaxBundleSize(maxSize))
	if err != nil {
		return nil, fmt.Errorf("failed to create bundle: %w", err)
	}

	result.TarData = tarData
	result.BuildFingerprint = buildFingerprint
	result.BuildCacheDir = buildCacheDir
	result.InputFileCount = len(discovery.LocalFiles)
	result.Messages = append(result.Messages, fmt.Sprintf("Bundled %d file(s) (%s)", len(manifest.Files), FormatByteSize(int64(len(tarData)))))

	return result, nil
}

// ParseByteSize parses a human-readable byte size string (e.g., "50MB", "100KB").
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)

	// Check longer suffixes first to avoid "KB" matching "B"
	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, entry := range suffixes {
		if strings.HasSuffix(s, entry.suffix) {
			numStr := strings.TrimSuffix(s, entry.suffix)
			numStr = strings.TrimSpace(numStr)
			if numStr == "" {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			var n int64
			if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			return n * entry.mult, nil
		}
	}

	// Plain number — treat as bytes
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return n, nil
}

// FormatByteSize formats bytes as a human-readable string.
func FormatByteSize(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// CatalogPluginResolver adapts a catalog.Catalog to the bundler.PluginResolver interface.
type CatalogPluginResolver struct {
	Catalog catalog.Catalog
}

// ResolvePlugin resolves a plugin artifact from the catalog by name and kind.
func (r *CatalogPluginResolver) ResolvePlugin(ctx context.Context, name string, kind catalog.ArtifactKind, _ string) (catalog.ArtifactInfo, error) {
	ref := catalog.Reference{
		Kind: kind,
		Name: name,
	}
	return r.Catalog.Resolve(ctx, ref)
}

// RegistryFetcherAdapter adapts a catalog.Registry to the bundler.CatalogFetcher interface.
// It supports both exact version fetches and listing all versions for constraint resolution.
type RegistryFetcherAdapter struct {
	Registry *catalog.Registry
}

// FetchSolution retrieves a solution by name[@version] from the registry.
func (a *RegistryFetcherAdapter) FetchSolution(ctx context.Context, nameWithVersion string) ([]byte, catalog.ArtifactInfo, error) {
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, nameWithVersion)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, fmt.Errorf("invalid reference %q: %w", nameWithVersion, err)
	}

	content, info, err := a.Registry.Fetch(ctx, ref)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, err
	}

	return content, info, nil
}

// ListSolutions returns all available versions of a named solution artifact.
func (a *RegistryFetcherAdapter) ListSolutions(ctx context.Context, name string) ([]catalog.ArtifactInfo, error) {
	return a.Registry.List(ctx, catalog.ArtifactKindSolution, name)
}
