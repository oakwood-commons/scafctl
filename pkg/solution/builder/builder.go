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
	"runtime"
	"strings"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
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

	// OfficialProviders is the official provider registry, used to suggest
	// correct bundle.plugins entries in error messages when a provider is
	// referenced but not declared. When nil, suggestions omit version info.
	OfficialProviders *official.Registry

	// BuiltinProviders lists provider names compiled into the binary (e.g.
	// "http", "file", "cel"). These do not require bundle.plugins entries.
	BuiltinProviders []string
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

	// LockData is the JSON-encoded lock file to attach as a dedicated OCI
	// layer (MediaTypeSolutionLock). Empty when the build produced no lock
	// (no vendored dependencies and no pinned plugins), in which case no lock
	// layer is stored.
	LockData []byte

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

	maxSize, err := ParseByteSize(opts.BundleMaxSize)
	if err != nil {
		return nil, fmt.Errorf("invalid bundle max size: %w", err)
	}

	// Step 1: Compose multi-file solutions
	if err := composeSolution(sol, bundleRoot, result, lgr); err != nil {
		return nil, err
	}

	// Step 2–3: Load .scafctlignore and discover files
	discovery, err := discoverBundleFiles(sol, bundleRoot, lgr)
	if err != nil {
		return nil, err
	}
	result.Discovery = discovery

	// Step 3.5: Validate and auto-inject plugin dependencies
	if err := preparePlugins(sol, opts, result, lgr); err != nil {
		return nil, err
	}

	// Step 3.6–4: Vendor plugins and catalog dependencies
	lockPath := filepath.Join(bundleRoot, "solution.lock")
	if err := vendorAll(ctx, sol, discovery, bundleRoot, lockPath, opts, result, lgr); err != nil {
		return nil, err
	}

	// Step 4.5: Build cache check
	buildFingerprint, buildCacheDir, cacheHit := checkBuildCache(sol, solutionContent, bundleRoot, lockPath, discovery, result, opts, lgr)
	if cacheHit != nil {
		return cacheHit, nil
	}

	// Step 5: Dry-run — return discovery for the caller to format
	if opts.DryRun {
		result.BuildFingerprint = buildFingerprint
		result.BuildCacheDir = buildCacheDir
		return result, nil
	}

	// Step 6: No files to bundle — return result with lock data but no tar/dedup payload.
	if len(discovery.LocalFiles) == 0 {
		lgr.V(1).Info("no files to bundle")
		if buildFingerprint != "" {
			result.BuildFingerprint = buildFingerprint
			result.BuildCacheDir = buildCacheDir
		}
		result.InputFileCount = 0
		return result, nil
	}

	// Steps 7–8: Create bundle archive (dedup v2 or tar v1)
	if err := createBundleArchive(result, sol, discovery, bundleRoot, buildFingerprint, buildCacheDir, maxSize, opts); err != nil {
		return nil, err
	}
	return result, nil
}

// composeSolution merges multi-file solutions when sol.Compose is set.
func composeSolution(sol *solution.Solution, bundleRoot string, result *BuildResult, lgr logr.Logger) error {
	if len(sol.Compose) == 0 {
		return nil
	}
	lgr.V(1).Info("composing solution", "files", sol.Compose)
	composed, err := bundler.Compose(sol, bundleRoot)
	if err != nil {
		return fmt.Errorf("failed to compose solution: %w", err)
	}
	*sol = *composed
	result.Messages = append(result.Messages, fmt.Sprintf("Composed %d file(s) into solution", len(sol.Compose)+1))
	return nil
}

// discoverBundleFiles loads .scafctlignore and runs static analysis + glob expansion.
func discoverBundleFiles(sol *solution.Solution, bundleRoot string, lgr logr.Logger) (*bundler.DiscoveryResult, error) {
	ignoreChecker, err := bundler.LoadScafctlIgnore(bundleRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load .scafctlignore: %w", err)
	}

	discovery, err := bundler.DiscoverFiles(sol, bundleRoot, bundler.WithIgnoreChecker(ignoreChecker))
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}

	lgr.V(1).Info("discovered files",
		"localFiles", len(discovery.LocalFiles),
		"catalogRefs", len(discovery.CatalogRefs))
	return discovery, nil
}

// preparePlugins validates bundle.plugins, merges defaults, and checks that
// every provider referenced in the solution has a corresponding plugin entry.
func preparePlugins(sol *solution.Solution, opts BuildBundleOptions, _ *BuildResult, lgr logr.Logger) error {
	if len(sol.Bundle.Plugins) > 0 {
		if err := bundler.ValidatePlugins(sol); err != nil {
			return fmt.Errorf("plugin validation failed: %w", err)
		}
		lgr.V(1).Info("validated plugin dependencies", "count", len(sol.Bundle.Plugins))
		bundler.MergePluginDefaults(sol)
	}

	return validateProviderCoverage(sol, opts.BuiltinProviders, opts.OfficialProviders)
}

// vendorAll orchestrates plugin vendoring (Step 3.6) and catalog dependency
// vendoring (Step 4), writing the lock file and capturing LockData.
func vendorAll(ctx context.Context, sol *solution.Solution, discovery *bundler.DiscoveryResult, bundleRoot, lockPath string, opts BuildBundleOptions, result *BuildResult, lgr logr.Logger) error {
	localCat, catErr := catalog.NewLocalCatalog(lgr)
	if catErr != nil {
		return fmt.Errorf("failed to open catalog: %w", catErr)
	}
	registry := catalog.NewRegistryWithLocal(localCat, lgr)
	registry.SetCacheRemoteArtifacts(true)

	// Vendor plugin dependencies
	if !opts.NoVendor && len(sol.Bundle.Plugins) > 0 {
		if err := vendorPlugins(ctx, sol, localCat, lockPath, result, lgr); err != nil {
			return err
		}
	}

	// Vendor catalog dependencies
	if !opts.NoVendor && len(discovery.CatalogRefs) > 0 {
		return vendorCatalogDeps(ctx, sol, discovery, bundleRoot, lockPath, registry, result, lgr)
	}

	// No catalog deps but we have resolved plugins — write plugin-only lock
	if len(result.ResolvedPlugins) > 0 {
		writePluginOnlyLock(result, lockPath, lgr)
	}
	return nil
}

// vendorPlugins resolves and pins plugin versions from the local and remote catalogs.
func vendorPlugins(ctx context.Context, sol *solution.Solution, localCat *catalog.LocalCatalog, lockPath string, result *BuildResult, lgr logr.Logger) error {
	lgr.V(1).Info("vendoring plugin dependencies", "count", len(sol.Bundle.Plugins))

	existingLock, _ := bundler.LoadLockFile(lockPath)
	noRegistry, hasRegistry := sol.Bundle.PartitionPlugins()
	platform := runtime.GOOS + "/" + runtime.GOARCH

	// Unsourced plugins: vendor from the local catalog.
	if len(noRegistry) > 0 {
		pluginResolver := &CatalogPluginResolver{Catalog: localCat}
		pluginResult, pluginErr := bundler.VendorPlugins(ctx, noRegistry, existingLock, bundler.VendorPluginsOptions{
			PluginResolver:  pluginResolver,
			PlatformCatalog: localCat,
			Platform:        platform,
			VerifySignature: buildLockSignatureVerifier(ctx, lgr),
		})
		if pluginErr != nil {
			return fmt.Errorf("failed to vendor plugin dependencies: %w", pluginErr)
		}
		if len(pluginResult.ResolvedPlugins) > 0 {
			result.ResolvedPlugins = append(result.ResolvedPlugins, pluginResult.ResolvedPlugins...)
		}
	}

	// Sourced plugins: vendor from their remote catalogs (fatal on failure).
	if len(hasRegistry) > 0 {
		fqnResult, fqnErr := bundler.VendorPluginsFQN(ctx, hasRegistry, existingLock, bundler.VendorPluginFQNOptions{
			SourcedCatalogs:      buildSourcedCatalogs(ctx, lgr),
			CatalogAliasResolver: catalogindex.FromConfig(config.FromContext(ctx)),
			Platform:             platform,
			VerifySignature:      buildLockSignatureVerifier(ctx, lgr),
		})
		if fqnErr != nil {
			return fmt.Errorf("failed to vendor sourced plugin dependencies: %w", fqnErr)
		}
		result.ResolvedPlugins = append(result.ResolvedPlugins, fqnResult.ResolvedPlugins...)
	}

	if len(result.ResolvedPlugins) > 0 {
		result.Messages = append(result.Messages, fmt.Sprintf("Pinned %d plugin(s) in lock file", len(result.ResolvedPlugins)))
	}
	return nil
}

// vendorCatalogDeps vendors catalog dependencies, merges plugin entries into
// the lock, and captures LockData for the OCI lock layer.
func vendorCatalogDeps(ctx context.Context, sol *solution.Solution, discovery *bundler.DiscoveryResult, bundleRoot, lockPath string, registry *catalog.Registry, result *BuildResult, lgr logr.Logger) error {
	lgr.V(1).Info("vendoring catalog dependencies", "count", len(discovery.CatalogRefs))

	vendorDir := filepath.Join(bundleRoot, ".scafctl", "vendor")
	vendorResult, err := bundler.VendorDependencies(ctx, sol, discovery.CatalogRefs, bundler.VendorOptions{
		BundleRoot:     bundleRoot,
		VendorDir:      vendorDir,
		LockPath:       lockPath,
		CatalogFetcher: &RegistryFetcherAdapter{Registry: registry},
	})
	if err != nil {
		return fmt.Errorf("failed to vendor catalog dependencies: %w", err)
	}

	// Append resolved plugins to the lock file
	if vendorResult.Lock != nil && len(result.ResolvedPlugins) > 0 {
		vendorResult.Lock.Plugins = append(vendorResult.Lock.Plugins, result.ResolvedPlugins...)
		if err := bundler.WriteLockFile(lockPath, vendorResult.Lock); err != nil {
			lgr.V(1).Info("failed to update lock file with plugins (non-fatal)", "error", err)
		}
	}

	// Capture the lock as JSON for the dedicated OCI lock layer.
	captureLockData(result, vendorResult.Lock, lgr)

	// Add vendored files to the discovery result
	for _, vf := range vendorResult.VendoredFiles {
		discovery.LocalFiles = append(discovery.LocalFiles, bundler.FileEntry{
			RelPath: vf,
			Source:  bundler.ExplicitInclude,
		})
	}

	result.Messages = append(result.Messages, fmt.Sprintf("Vendored %d catalog dependency(ies)", len(vendorResult.VendoredFiles)))
	return nil
}

// writePluginOnlyLock writes and captures a lock containing only plugin entries
// (used when there are no catalog dependencies to vendor).
func writePluginOnlyLock(result *BuildResult, lockPath string, lgr logr.Logger) {
	pluginLock := &bundler.LockFile{
		Version: bundler.LockFileVersion,
		Plugins: result.ResolvedPlugins,
	}
	if err := bundler.WriteLockFile(lockPath, pluginLock); err != nil {
		lgr.V(1).Info("failed to write plugin lock file (non-fatal)", "error", err)
	}
	captureLockData(result, pluginLock, lgr)
}

// captureLockData marshals a lock file to JSON and stores it in result.LockData.
func captureLockData(result *BuildResult, lock *bundler.LockFile, lgr logr.Logger) {
	if lock == nil {
		return
	}
	data, err := bundler.MarshalLockJSON(lock)
	if err != nil {
		lgr.V(1).Info("failed to marshal lock for artifact layer (non-fatal)", "error", err)
		return
	}
	result.LockData = data
}

// checkBuildCache computes a build fingerprint and checks the cache. Returns a
// non-nil *BuildResult when there is a cache hit that should be returned directly.
func checkBuildCache(sol *solution.Solution, solutionContent []byte, bundleRoot, lockPath string, discovery *bundler.DiscoveryResult, result *BuildResult, opts BuildBundleOptions, lgr logr.Logger) (fingerprint, cacheDir string, cacheHit *BuildResult) {
	if opts.NoCache || opts.DryRun {
		return "", "", nil
	}

	cacheDir = settings.DefaultBuildCacheDir()

	// Collect plugin entries for fingerprinting
	var fpPlugins []bundler.BundlePluginEntry
	for _, p := range sol.Bundle.Plugins {
		fpPlugins = append(fpPlugins, bundler.BundlePluginEntry{
			Name:     p.ArtifactName(),
			Kind:     string(p.Kind),
			Version:  p.Version,
			Registry: p.Registry(),
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
		return "", cacheDir, nil
	}

	fingerprint = fp
	cacheEntry, hit := bundler.CheckBuildCache(cacheDir, fp)
	if hit {
		lgr.V(1).Info("build cache hit",
			"fingerprint", fp,
			"artifact", cacheEntry.ArtifactName,
			"version", cacheEntry.ArtifactVersion)
		return fingerprint, cacheDir, &BuildResult{CacheHit: true, CacheEntry: cacheEntry, Discovery: discovery, LockData: result.LockData}
	}
	lgr.V(1).Info("build cache miss", "fingerprint", fp)
	return fingerprint, cacheDir, nil
}

// createBundleArchive produces either a deduplicated v2 bundle or a traditional
// v1 tar archive and populates the result.
func createBundleArchive(result *BuildResult, sol *solution.Solution, discovery *bundler.DiscoveryResult, bundleRoot, buildFingerprint, buildCacheDir string, maxSize int64, opts BuildBundleOptions) error {
	plugins := collectPluginEntries(sol)

	if opts.Dedupe {
		dedupeThreshold, err := ParseByteSize(opts.DedupeThreshold)
		if err != nil {
			return fmt.Errorf("invalid dedupe threshold: %w", err)
		}

		dedupeResult, err := bundler.CreateDeduplicatedBundle(bundleRoot, discovery.LocalFiles, plugins,
			bundler.WithDedupeThreshold(dedupeThreshold),
			bundler.WithDedupeMaxSize(maxSize))
		if err != nil {
			return fmt.Errorf("failed to create deduplicated bundle: %w", err)
		}

		result.Dedup = dedupeResult
		result.BuildFingerprint = buildFingerprint
		result.BuildCacheDir = buildCacheDir
		result.InputFileCount = len(discovery.LocalFiles)
		result.Messages = append(result.Messages, fmt.Sprintf("Bundled %d file(s) (%s, deduplicated: %d layer(s))",
			len(dedupeResult.Manifest.Files),
			format.Bytes(dedupeResult.TotalSize),
			len(dedupeResult.LargeBlobs)+1)) // +1 for small files tar if present
		return nil
	}

	// Non-dedup path: create v1 tar
	tarData, manifest, err := bundler.CreateBundleTar(bundleRoot, discovery.LocalFiles, plugins,
		bundler.WithMaxBundleSize(maxSize))
	if err != nil {
		return fmt.Errorf("failed to create bundle: %w", err)
	}

	result.TarData = tarData
	result.BuildFingerprint = buildFingerprint
	result.BuildCacheDir = buildCacheDir
	result.InputFileCount = len(discovery.LocalFiles)
	result.Messages = append(result.Messages, fmt.Sprintf("Bundled %d file(s) (%s)", len(manifest.Files), format.Bytes(int64(len(tarData)))))
	return nil
}

// collectPluginEntries converts solution bundle plugins to bundler entries.
func collectPluginEntries(sol *solution.Solution) []bundler.BundlePluginEntry {
	plugins := make([]bundler.BundlePluginEntry, 0, len(sol.Bundle.Plugins))
	for _, p := range sol.Bundle.Plugins {
		plugins = append(plugins, bundler.BundlePluginEntry{
			Name:     p.ArtifactName(),
			Kind:     string(p.Kind),
			Version:  p.Version,
			Registry: p.Registry(),
		})
	}
	return plugins
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

// CatalogPluginResolver adapts a catalog.Catalog to the bundler.PluginResolver interface.
type CatalogPluginResolver struct {
	Catalog catalog.Catalog
}

// ResolvePlugin resolves a plugin artifact from the catalog by name and kind.
func (r *CatalogPluginResolver) ResolvePlugin(ctx context.Context, name string, kind catalog.ArtifactKind, version string) (catalog.ArtifactInfo, error) {
	ref := catalog.Reference{
		Kind: kind,
		Name: name,
	}

	v, err := bundler.ResolveVersionConstraint(ctx, r.Catalog, kind, name, version)
	if err != nil {
		return catalog.ArtifactInfo{}, err
	}
	ref.Version = v

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

// validateProviderCoverage checks that every provider referenced in the
// solution's resolvers, actions, and calls is either a builtin (compiled into
// the binary) or explicitly declared in bundle.plugins. Returns an actionable
// error listing missing providers and suggesting the YAML to add.
func validateProviderCoverage(sol *solution.Solution, builtins []string, officialReg *official.Registry) error {
	referenced := sol.Spec.ReferencedProviderNames()
	if len(referenced) == 0 {
		return nil
	}

	builtinSet := make(map[string]struct{}, len(builtins))
	for _, name := range builtins {
		builtinSet[name] = struct{}{}
	}

	declared := make(map[string]struct{}, len(sol.Bundle.Plugins))
	for _, p := range sol.Bundle.Plugins {
		declared[p.LocalName()] = struct{}{}
	}

	var missing []string
	for _, name := range referenced {
		if _, isBuiltin := builtinSet[name]; isBuiltin {
			continue
		}
		if _, isDeclared := declared[name]; isDeclared {
			continue
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 {
		return nil
	}

	// Build actionable suggestion.
	var suggestions []string
	for _, name := range missing {
		version := "<version constraint>"
		if officialReg != nil {
			if p, ok := officialReg.Get(name); ok {
				version = p.DefaultVersion
			}
		}
		suggestions = append(suggestions, fmt.Sprintf("    - name: %s\n      kind: provider\n      version: %s", name, version))
	}

	return fmt.Errorf(
		"providers %v are referenced in the solution but not declared in bundle.plugins;\n"+
			"add to your solution:\n\n"+
			"bundle:\n  plugins:\n%s",
		missing, strings.Join(suggestions, "\n"),
	)
}

// buildLockSignatureVerifier returns a VerifySignature callback for the bundler
// when a signature policy is active. It checks the context for an explicit
// policy override first, then falls back to the app configuration
// (config.Plugins.Signatures). Returns nil when signatures are disabled,
// which causes VendorPlugins to skip verification.
func buildLockSignatureVerifier(ctx context.Context, lgr logr.Logger) func(context.Context, string) (*bundler.LockPluginSignature, error) {
	policy := plugin.SignaturePolicyFromContext(ctx)
	if policy == nil {
		policy = signaturePolicyFromAppConfig(config.FromContext(ctx), lgr)
	}
	if !policy.IsEnabled() {
		return nil
	}
	verifier := plugin.NewSignatureVerifier()
	return func(ctx context.Context, imageRef string) (*bundler.LockPluginSignature, error) {
		result, err := verifier.VerifySignature(ctx, imageRef, policy)
		if err != nil {
			if handleErr := plugin.HandleVerificationError(policy, err, lgr,
				"imageRef", imageRef); handleErr != nil {
				return nil, handleErr
			}
			return nil, nil
		}
		if result == nil || !result.Verified {
			return nil, nil
		}
		return &bundler.LockPluginSignature{
			Issuer:   result.Issuer,
			Identity: result.Identity,
			SignedAt: result.SignedAt,
		}, nil
	}
}

// signaturePolicyFromAppConfig derives a SignaturePolicy from the app
// configuration using the shared plugin.SignaturePolicyFromRaw helper.
// Returns nil when the config is nil, mode is "off", empty, or invalid.
func signaturePolicyFromAppConfig(appCfg *config.Config, lgr logr.Logger) *plugin.SignaturePolicy {
	if appCfg == nil {
		return nil
	}
	sigCfg := appCfg.Plugins.Signatures
	policy, err := plugin.SignaturePolicyFromRaw(sigCfg.Mode, sigCfg.TrustedIssuers, sigCfg.TrustedIdentities)
	if err != nil {
		lgr.Info("invalid plugin signature mode in config, defaulting to off",
			"mode", sigCfg.Mode, "error", err)
		return nil
	}
	return policy
}

// buildSourcedCatalogs constructs one remote catalog per URL-bearing configured
// catalog, keyed by the lowercased catalog alias. It mirrors
// catalogindex.FromConfig's remote enumeration exactly -- only URL-bearing
// (remote/OCI) catalogs, keyed by strings.ToLower(catCfg.Name) -- so the
// alias->origin map (used to bind a sourced plugin's origin to an alias)
// and this alias->catalog map (used to resolve that alias to a live catalog)
// stay in lockstep. Filesystem catalogs are skipped: they have no registry
// origin and cannot serve a fully-qualified source.
//
// Each catalog is built via catalog.BuildRemoteCatalogFromConfig, which wires
// config-matched auth (the catalog's authProvider handler + authScope) when
// available and otherwise falls back to anonymous/Docker credentials. A single
// shared CredentialStore is threaded through all catalogs. A catalog that fails
// to build is skipped with a debug log rather than failing the whole build --
// an unrelated misconfigured catalog must not block vendoring a plugin sourced
// from a different, healthy catalog.
//
// Returns nil when the config is absent or no remote catalogs are configured,
// which causes VendorPluginsFQN to skip sourced vendoring.
func buildSourcedCatalogs(ctx context.Context, lgr logr.Logger) map[string]bundler.SourcedCatalog {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return nil
	}

	authReg := auth.RegistryFromContext(ctx)
	credStore, credErr := catalog.NewCredentialStore(lgr)
	if credErr != nil {
		// Non-fatal: BuildRemoteCatalogFromConfig tolerates a nil store and
		// falls back to anonymous access.
		lgr.V(1).Info("credential store unavailable for sourced plugin catalogs", "error", credErr)
	}

	out := make(map[string]bundler.SourcedCatalog)
	for _, catCfg := range cfg.Catalogs {
		if catCfg.URL == "" {
			continue // filesystem catalog: no registry origin
		}
		alias := strings.ToLower(catCfg.Name)
		if _, exists := out[alias]; exists {
			// Mirror catalogindex.FromConfig's "first alias wins" behavior so
			// the two maps agree on which catalog an alias binds to.
			continue
		}
		rc, buildErr := catalog.BuildRemoteCatalogFromConfig(catCfg, credStore, authReg, lgr)
		if buildErr != nil {
			lgr.V(1).Info("skipping catalog for sourced plugin vendoring",
				"catalog", catCfg.Name, "error", buildErr)
			continue
		}
		out[alias] = rc
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
