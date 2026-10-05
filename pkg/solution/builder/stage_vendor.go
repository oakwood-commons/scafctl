// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// readLock reads the solution.lock loaded with the source, so pinned
// versions are replayed rather than re-resolved.
func (p *Packager) readLock(_ context.Context, st *pipelineState) (bool, error) {
	lock, err := p.src.LockFile()
	if err != nil {
		return false, err
	}
	st.existingLock = lock
	return false, nil
}

// resolvePlugins pins bundle.plugins to exact versions and digests.
func (p *Packager) resolvePlugins(ctx context.Context, st *pipelineState) (bool, error) {
	lgr := *logger.FromContext(ctx)
	res, err := bundler.VendorAllPlugins(ctx, p.src.Solution.Bundle.Plugins, st.existingLock, bundler.VendorAllPluginsOptions{
		CatalogResolver: catalogindex.FromConfig(config.FromContext(ctx)),
		Remotes:         buildSourcedCatalogs(ctx, lgr),
		Local:           p.catalog,
		Platform:        runtime.GOOS + "/" + runtime.GOARCH,
		VerifySignature: buildLockSignatureVerifier(ctx, lgr),
	})
	if err != nil {
		return false, fmt.Errorf("failed to vendor plugin dependencies: %w", err)
	}
	st.plugins = res.ResolvedPlugins
	if len(st.plugins) > 0 {
		st.addMessagef("Pinned %d plugin(s) in lock file", len(st.plugins))
	}
	return false, nil
}

// resolveCatalogDeps resolves the catalog references found by discovery
// into vendored files, held in memory until written, and adds them to the
// files to bundle.
func (p *Packager) resolveCatalogDeps(ctx context.Context, st *pipelineState) (bool, error) {
	// TODO: catalog dependency resolution is origin-ambiguous.
	//   - Only the local catalog is searched (no remotes are added), so
	//     SetCacheRemoteArtifacts below is a no-op and results depend on what
	//     happens to be pulled/built on this machine. Plugins, by contrast,
	//     resolve against buildSourcedCatalogs.
	//   - Refs carry no origin. Exact/no-version refs fail on mixed origins via
	//     checkAmbiguity, but semver-range refs (bundler.parseAndResolveRef)
	//     pick the highest version across all origins with no ambiguity check.
	//   - The lock records ResolvedFrom as "local", not the canonical origin
	//     (plugins record ResolvedCanonical), so replay cannot verify origin.
	//   Consider explicit, origin-qualified dependencies (e.g. spec.dependencies)
	//   instead of inferring them from solution provider `source` values.
	registry := catalog.NewRegistryWithLocal(p.catalog, *logger.FromContext(ctx))
	registry.SetCacheRemoteArtifacts(!p.dryRun)

	discovery := st.discovery
	deps, err := bundler.ResolveDependencies(ctx, p.src.Solution, discovery.CatalogRefs, bundler.VendorOptions{
		BundleRoot:     p.src.BundleRoot,
		ExistingLock:   st.existingLock,
		CatalogFetcher: &RegistryFetcherAdapter{Registry: registry},
	})
	if err != nil {
		return false, fmt.Errorf("failed to vendor catalog dependencies: %w", err)
	}
	st.deps = deps
	for _, relPath := range deps.VendoredFiles {
		discovery.LocalFiles = append(discovery.LocalFiles, bundler.FileEntry{RelPath: relPath, Source: bundler.ExplicitInclude})
	}
	st.addMessagef("Vendored %d catalog dependency(ies)", len(deps.VendoredFiles))
	return false, nil
}

// assembleLock builds the lock to store and write, and encodes it for the
// artifact's lock layer.
func (p *Packager) assembleLock(_ context.Context, st *pipelineState) (bool, error) {
	st.lock = newLock(st.deps, st.plugins)
	if st.lock == nil {
		return false, nil
	}
	data, err := bundler.MarshalLockJSON(st.lock)
	if err != nil {
		return false, fmt.Errorf("encoding lock file: %w", err)
	}
	st.result.Build.LockData = data
	return false, nil
}

// newLock returns the vendored dependencies' lock with the pinned plugins,
// a plugin-only lock, or nil when nothing was pinned or vendored.
func newLock(deps *bundler.VendorResult, plugins []bundler.LockPlugin) *bundler.LockFile {
	switch {
	case deps != nil && deps.Lock != nil:
		deps.Lock.Plugins = append(deps.Lock.Plugins, plugins...)
		return deps.Lock
	case len(plugins) > 0:
		return &bundler.LockFile{Version: bundler.LockFileVersion, Plugins: plugins}
	default:
		return nil
	}
}

// writeVendoredFiles writes the fetched catalog dependencies under the
// bundle root.
func (p *Packager) writeVendoredFiles(_ context.Context, st *pipelineState) (bool, error) {
	return false, st.deps.WriteFiles(p.src.BundleRoot)
}

// writeLock writes solution.lock next to the solution.
func (p *Packager) writeLock(_ context.Context, st *pipelineState) (bool, error) {
	path := filepath.Join(p.src.BundleRoot, bundler.DefaultLockFileName)
	if err := bundler.WriteLockFile(path, st.lock); err != nil {
		return false, fmt.Errorf("writing lock file: %w", err)
	}
	return false, nil
}
