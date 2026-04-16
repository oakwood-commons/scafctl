// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
)

// ArtifactCacher defines the interface for an artifact cache used by SolutionResolver.
// This interface allows caching downloaded catalog artifacts to reduce repeated fetches.
type ArtifactCacher interface {
	// Get retrieves cached content and bundle data.
	// Returns (nil, nil, false, nil) on cache miss. Returns an error on read failure.
	Get(kind, name, version string) (content, bundleData []byte, ok bool, err error)
	// Put stores artifact content and bundle data in the cache.
	Put(kind, name, version, digest string, content, bundleData []byte) error
}

// SolutionResolverOption configures a SolutionResolver.
type SolutionResolverOption func(*SolutionResolver)

// WithResolverArtifactCache sets the artifact cache for the resolver.
// When set, fetched artifacts are stored in and served from this cache.
func WithResolverArtifactCache(c ArtifactCacher) SolutionResolverOption {
	return func(r *SolutionResolver) {
		r.artifactCache = c
	}
}

// WithResolverNoCache disables artifact caching for this resolver.
// When true, the cache is neither read nor written, ensuring fresh catalog fetches.
func WithResolverNoCache(noCache bool) SolutionResolverOption {
	return func(r *SolutionResolver) {
		r.noCache = noCache
	}
}

// WithResolverRemoteCatalogs sets fallback remote catalogs for the resolver.
// When the local catalog does not contain the requested artifact, these remotes
// are tried in order. On a remote hit the artifact is automatically pulled into
// the local catalog so subsequent runs are instant.
func WithResolverRemoteCatalogs(remotes []Catalog) SolutionResolverOption {
	return func(r *SolutionResolver) {
		r.remoteCatalogs = remotes
	}
}

// SolutionResolver wraps a Catalog to provide solution fetching by name[@version].
// It implements the CatalogResolver interface from pkg/solution/get.
type SolutionResolver struct {
	catalog        Catalog
	remoteCatalogs []Catalog
	logger         logr.Logger
	artifactCache  ArtifactCacher
	noCache        bool
}

// NewSolutionResolver creates a resolver that fetches solutions from the given catalog.
// Optional SolutionResolverOption values may be provided to configure artifact caching
// and cache bypass behavior.
func NewSolutionResolver(catalog Catalog, logger logr.Logger, opts ...SolutionResolverOption) *SolutionResolver {
	r := &SolutionResolver{
		catalog: catalog,
		logger:  logger.WithName("solution-resolver"),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// FetchSolution retrieves a solution from the catalog by name[@version].
// The input format is "name" or "name@version" (e.g., "my-solution" or "my-solution@1.2.3").
// Returns the solution content as bytes.
//
// When an artifact cache is configured and noCache is false, the result is served
// from cache on a hit (within TTL), otherwise the catalog is fetched and the
// result is stored for future use.
func (r *SolutionResolver) FetchSolution(ctx context.Context, nameWithVersion string) ([]byte, error) {
	// Parse the name[@version] format
	name, version := ParseNameVersion(nameWithVersion)

	// Check artifact cache (skip when --no-cache or no cache configured)
	if !r.noCache && r.artifactCache != nil {
		cached, _, ok, err := r.artifactCache.Get(string(ArtifactKindSolution), name, version)
		if err != nil {
			r.logger.V(1).Info("artifact cache get error (ignoring)", "error", err)
		} else if ok {
			r.logger.V(1).Info("artifact cache hit", "name", name, "version", version)
			return cached, nil
		}
	}

	// Build the reference string for parsing
	refStr := name
	if version != "" {
		refStr = name + "@" + version
	}

	ref, err := ParseReference(ArtifactKindSolution, refStr)
	if err != nil {
		return nil, fmt.Errorf("invalid solution reference %q: %w", nameWithVersion, err)
	}

	r.logger.V(1).Info("fetching solution from catalog",
		"name", name,
		"version", version,
		"catalog", r.catalog.Name())

	content, info, err := r.catalog.Fetch(ctx, ref)
	if err != nil {
		// Only fall back to remotes on not-found; propagate other errors
		// (e.g. corrupted OCI layout) immediately.
		if !IsNotFound(err) || len(r.remoteCatalogs) == 0 {
			return nil, err
		}
		content, info, err = r.fetchFromRemotes(ctx, ref)
		if err != nil {
			return nil, err
		}
	}

	r.logger.V(1).Info("fetched solution from catalog",
		"name", info.Reference.Name,
		"version", info.Reference.Version,
		"digest", info.Digest,
		"catalog", r.catalog.Name())

	// Store in artifact cache using the resolved version as the cache key version.
	if !r.noCache && r.artifactCache != nil {
		resolvedVersion := version
		if info.Reference.Version != nil {
			resolvedVersion = info.Reference.Version.String()
		}
		if err := r.artifactCache.Put(string(ArtifactKindSolution), name, resolvedVersion, info.Digest, content, nil); err != nil {
			r.logger.V(1).Info("artifact cache put error (ignoring)", "error", err)
		}
	}

	return content, nil
}

// FetchSolutionWithBundle retrieves a solution and its bundle from the catalog by name[@version].
// The input format is "name" or "name@version" (e.g., "my-solution" or "my-solution@1.2.3").
// Returns the solution content bytes, bundle tar bytes (nil if no bundle), and any error.
//
// When an artifact cache is configured and noCache is false, both content and bundle
// are cached together for TTL-based reuse.
func (r *SolutionResolver) FetchSolutionWithBundle(ctx context.Context, nameWithVersion string) ([]byte, []byte, error) {
	// Parse the name[@version] format
	name, version := ParseNameVersion(nameWithVersion)

	// Check artifact cache (skip when --no-cache or no cache configured)
	if !r.noCache && r.artifactCache != nil {
		cachedContent, cachedBundle, ok, err := r.artifactCache.Get(string(ArtifactKindSolution), name, version)
		if err != nil {
			r.logger.V(1).Info("artifact cache get error (ignoring)", "error", err)
		} else if ok {
			r.logger.V(1).Info("artifact cache hit (with bundle)", "name", name, "version", version, "hasBundle", len(cachedBundle) > 0)
			return cachedContent, cachedBundle, nil
		}
	}

	// Build the reference string for parsing
	refStr := name
	if version != "" {
		refStr = name + "@" + version
	}

	ref, err := ParseReference(ArtifactKindSolution, refStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid solution reference %q: %w", nameWithVersion, err)
	}

	r.logger.V(1).Info("fetching solution with bundle from catalog",
		"name", name,
		"version", version,
		"catalog", r.catalog.Name())

	content, bundleData, info, err := r.catalog.FetchWithBundle(ctx, ref)
	if err != nil {
		// Only fall back to remotes on not-found; propagate other errors
		// (e.g. corrupted OCI layout) immediately.
		if !IsNotFound(err) || len(r.remoteCatalogs) == 0 {
			return nil, nil, err
		}
		content, bundleData, info, err = r.fetchWithBundleFromRemotes(ctx, ref)
		if err != nil {
			return nil, nil, err
		}
	}

	r.logger.V(1).Info("fetched solution with bundle from catalog",
		"name", info.Reference.Name,
		"version", info.Reference.Version,
		"digest", info.Digest,
		"hasBundle", len(bundleData) > 0,
		"catalog", r.catalog.Name())

	// Store in artifact cache using the resolved version as the cache key version.
	if !r.noCache && r.artifactCache != nil {
		resolvedVersion := version
		if info.Reference.Version != nil {
			resolvedVersion = info.Reference.Version.String()
		}
		if err := r.artifactCache.Put(string(ArtifactKindSolution), name, resolvedVersion, info.Digest, content, bundleData); err != nil {
			r.logger.V(1).Info("artifact cache put error (ignoring)", "error", err)
		}
	}

	return content, bundleData, nil
}

// ParseNameVersion splits "name@version" into (name, version).
// If no @ is present, returns (input, "").
// Handles digest references (e.g., "name@sha256:abc123").
func ParseNameVersion(input string) (string, string) {
	// Handle digest references (sha256:...)
	if strings.Contains(input, "@sha256:") {
		parts := strings.SplitN(input, "@sha256:", 2)
		return parts[0], "sha256:" + parts[1]
	}

	// Handle version references
	if idx := strings.LastIndex(input, "@"); idx != -1 {
		return input[:idx], input[idx+1:]
	}

	return input, ""
}

// fetchFromRemotes tries each remote catalog in order. On the first hit it
// stores the artifact into the local catalog so future runs are instant.
func (r *SolutionResolver) fetchFromRemotes(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	var firstErr error
	for _, remote := range r.remoteCatalogs {
		r.logger.V(1).Info("trying remote catalog", "name", ref.Name, "catalog", remote.Name())

		content, info, err := remote.Fetch(ctx, ref)
		if err != nil {
			if !IsNotFound(err) {
				r.logger.Info("remote catalog error, trying next",
					"catalog", remote.Name(), "error", err)
				if firstErr == nil {
					firstErr = fmt.Errorf("remote catalog %q: %w", remote.Name(), err)
				}
			} else {
				r.logger.V(1).Info("remote catalog miss", "catalog", remote.Name(), "error", err)
			}
			continue
		}

		r.logger.Info("auto-pulled from remote catalog",
			"name", ref.Name, "version", info.Reference.Version, "catalog", remote.Name())

		// Store into local catalog for future runs (best effort).
		r.storeLocally(ctx, ref, info, content, nil, remote.Name())

		return content, info, nil
	}
	if firstErr != nil {
		return nil, ArtifactInfo{}, firstErr
	}
	return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: r.catalog.Name()}
}

// fetchWithBundleFromRemotes is the bundle-aware variant of fetchFromRemotes.
func (r *SolutionResolver) fetchWithBundleFromRemotes(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	var firstErr error
	for _, remote := range r.remoteCatalogs {
		r.logger.V(1).Info("trying remote catalog (with bundle)", "name", ref.Name, "catalog", remote.Name())

		content, bundleData, info, err := remote.FetchWithBundle(ctx, ref)
		if err != nil {
			if !IsNotFound(err) {
				r.logger.Info("remote catalog error, trying next",
					"catalog", remote.Name(), "error", err)
				if firstErr == nil {
					firstErr = fmt.Errorf("remote catalog %q: %w", remote.Name(), err)
				}
			} else {
				r.logger.V(1).Info("remote catalog miss", "catalog", remote.Name(), "error", err)
			}
			continue
		}

		r.logger.Info("auto-pulled from remote catalog",
			"name", ref.Name, "version", info.Reference.Version, "catalog", remote.Name())

		r.storeLocally(ctx, ref, info, content, bundleData, remote.Name())

		return content, bundleData, info, nil
	}
	if firstErr != nil {
		return nil, nil, ArtifactInfo{}, firstErr
	}
	return nil, nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: r.catalog.Name()}
}

// storeLocally persists a remotely-fetched artifact into the local catalog.
// Errors are logged but not propagated — the remote fetch already succeeded.
func (r *SolutionResolver) storeLocally(ctx context.Context, ref Reference, info ArtifactInfo, content, bundleData []byte, sourceCatalog string) {
	storeRef := ref
	if info.Reference.Version != nil {
		storeRef.Version = info.Reference.Version
	}

	annotations := map[string]string{
		AnnotationArtifactName: storeRef.Name,
		AnnotationArtifactType: storeRef.Kind.String(),
		AnnotationOrigin:       fmt.Sprintf("auto-cached from %s", sourceCatalog),
	}
	if storeRef.Version != nil {
		annotations[AnnotationVersion] = storeRef.Version.String()
	}

	if _, err := r.catalog.Store(ctx, storeRef, content, bundleData, annotations, false); err != nil {
		r.logger.V(1).Info("failed to store auto-pulled artifact locally (ignoring)", "error", err)
	}
}
