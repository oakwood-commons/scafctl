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
	// Get retrieves cached content and any stored auxiliary layers. layers is
	// keyed by media type (e.g. MediaTypeSolutionBundle, MediaTypeSolutionLock)
	// and is nil when the entry has no auxiliary layers. Returns
	// (nil, nil, false, nil) on cache miss. Returns an error on read failure.
	Get(kind, name, version string) (content []byte, layers map[string][]byte, ok bool, err error)
	// Put stores artifact content and optional auxiliary layers in the cache.
	// layers maps a media type to its bytes; nil or empty entries are skipped.
	Put(kind, name, version, digest string, content []byte, layers map[string][]byte) error
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
	catalog             Catalog
	remoteCatalogs      []Catalog
	logger              logr.Logger
	artifactCache       ArtifactCacher
	noCache             bool
	lastResolvedCatalog string // name of the catalog that satisfied the last fetch
}

// LastResolvedCatalog returns the name of the catalog that satisfied the most
// recent FetchSolution or FetchSolutionWithLayers call. Returns "" if no
// fetch has been performed yet.
func (r *SolutionResolver) LastResolvedCatalog() string {
	return r.lastResolvedCatalog
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
	} else if version == "" && len(r.remoteCatalogs) > 0 {
		// No version pinned → "latest" semantics. Check remotes for a newer
		// version than what the local catalog has (like `docker pull :latest`).
		if upgraded, upgradedInfo, ok := r.checkRemoteForNewer(ctx, ref, info); ok {
			content = upgraded
			info = upgradedInfo
		}
	}

	r.lastResolvedCatalog = info.Catalog
	r.logger.V(1).Info("fetched solution from catalog",
		"name", info.Reference.Name,
		"version", info.Reference.Version,
		"digest", info.Digest,
		"catalog", info.Catalog)

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

// FetchSolutionWithLayers retrieves a solution together with the requested
// auxiliary OCI layers from the catalog by name[@version]. The input format is
// "name" or "name@version" (e.g., "my-solution" or "my-solution@1.2.3"). The
// requested layer media types (e.g. MediaTypeSolutionBundle,
// MediaTypeSolutionLock) are returned in a map keyed by media type; a media type
// is absent from the map when the artifact has no such layer.
//
// The lookup delegates to the catalog's FetchWithLayer. On a local not-found it
// falls back to the configured remote catalogs in order, auto-pulling the hit
// into the local catalog so subsequent runs are instant. When no version is
// pinned it applies Docker-style "latest" semantics, checking remotes for a
// newer version than the local catalog holds.
//
// When an artifact cache is configured and noCache is false, a cache hit is
// served only when every requested layer is present in the cached entry;
// otherwise the catalog is fetched and the content together with all fetched
// layers is stored for future reuse.
func (r *SolutionResolver) FetchSolutionWithLayers(ctx context.Context, nameWithVersion string, mediaTypes ...string) ([]byte, map[string][]byte, error) {
	// Parse the name[@version] format.
	name, version := ParseNameVersion(nameWithVersion)

	// Check artifact cache (skip when --no-cache or no cache configured). Only
	// serve from cache when every requested layer is present, so a content-only
	// or partially-populated entry does not mask a missing layer.
	if !r.noCache && r.artifactCache != nil {
		cachedContent, cachedLayers, ok, err := r.artifactCache.Get(string(ArtifactKindSolution), name, version)
		if err != nil {
			r.logger.V(1).Info("artifact cache get error (ignoring)", "error", err)
		} else if ok && layersContainAll(cachedLayers, mediaTypes) {
			r.logger.V(1).Info("artifact cache hit (with layers)", "name", name, "version", version, "mediaTypes", mediaTypes)
			return cachedContent, filterLayers(cachedLayers, mediaTypes), nil
		}
	}

	// Build the reference string for parsing.
	refStr := name
	if version != "" {
		refStr = name + "@" + version
	}

	ref, err := ParseReference(ArtifactKindSolution, refStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid solution reference %q: %w", nameWithVersion, err)
	}

	r.logger.V(1).Info("fetching solution with layers from catalog",
		"name", name,
		"version", version,
		"mediaTypes", mediaTypes,
		"catalog", r.catalog.Name())

	content, layers, info, err := r.catalog.FetchWithLayer(ctx, ref, mediaTypes...)
	if err != nil {
		// Only fall back to remotes on not-found; propagate other errors
		// (e.g. corrupted OCI layout) immediately.
		if !IsNotFound(err) || len(r.remoteCatalogs) == 0 {
			return nil, nil, err
		}
		content, layers, info, err = r.fetchWithLayerFromRemotes(ctx, ref, mediaTypes...)
		if err != nil {
			return nil, nil, err
		}
	} else if version == "" && len(r.remoteCatalogs) > 0 {
		// No version pinned → "latest" semantics. Check remotes for a newer
		// version than what the local catalog has.
		if upgraded, upgradedLayers, upgradedInfo, ok := r.checkRemoteForNewerWithLayers(ctx, ref, info, mediaTypes...); ok {
			content = upgraded
			layers = upgradedLayers
			info = upgradedInfo
		}
	}

	r.lastResolvedCatalog = info.Catalog
	r.logger.V(1).Info("fetched solution with layers from catalog",
		"name", info.Reference.Name,
		"version", info.Reference.Version,
		"digest", info.Digest,
		"catalog", info.Catalog)

	// Store in artifact cache using the resolved version as the cache key version.
	if !r.noCache && r.artifactCache != nil {
		resolvedVersion := version
		if info.Reference.Version != nil {
			resolvedVersion = info.Reference.Version.String()
		}
		if err := r.artifactCache.Put(string(ArtifactKindSolution), name, resolvedVersion, info.Digest, content, layers); err != nil {
			r.logger.V(1).Info("artifact cache put error (ignoring)", "error", err)
		}
	}

	return content, layers, nil
}

// layersContainAll reports whether layers holds a non-empty entry for every
// requested media type. An empty mediaTypes slice is trivially satisfied.
func layersContainAll(layers map[string][]byte, mediaTypes []string) bool {
	for _, mt := range mediaTypes {
		if len(layers[mt]) == 0 {
			return false
		}
	}
	return true
}

// filterLayers returns a new map containing only the requested media types that
// have a non-empty entry in layers. It returns nil when nothing matches.
func filterLayers(layers map[string][]byte, mediaTypes []string) map[string][]byte {
	var out map[string][]byte
	for _, mt := range mediaTypes {
		if data := layers[mt]; len(data) > 0 {
			if out == nil {
				out = make(map[string][]byte, len(mediaTypes))
			}
			out[mt] = data
		}
	}
	return out
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

// fetchWithLayerFromRemotes is the layer-aware variant of fetchFromRemotes. It
// tries each remote catalog in order, requesting the given auxiliary layer media
// types. On the first hit it stores the artifact into the local catalog so
// future runs are instant, then returns the content and layer map.
func (r *SolutionResolver) fetchWithLayerFromRemotes(ctx context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error) {
	var firstErr error
	for _, remote := range r.remoteCatalogs {
		r.logger.V(1).Info("trying remote catalog (with layer)",
			"name", ref.Name, "catalog", remote.Name(), "mediaTypes", mediaTypes)

		content, layers, info, err := remote.FetchWithLayer(ctx, ref, mediaTypes...)
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

		// Store into local catalog for future runs (best effort). Extract
		// MediaTypeSolutionBundle into the dedicated bundleData slot so the
		// local store keeps the correct OCI layer structure; remaining layers
		// (e.g. lock) are persisted as extra layers.
		bundleData := layers[MediaTypeSolutionBundle]
		var extraLayers []Layer
		for mt, data := range layers {
			if mt == MediaTypeSolutionBundle {
				continue // handled via bundleData param above
			}
			if len(data) > 0 {
				extraLayers = append(extraLayers, Layer{MediaType: mt, Data: data})
			}
		}
		r.storeLocally(ctx, ref, info, content, bundleData, remote.Name(), extraLayers...)

		return content, layers, info, nil
	}
	if firstErr != nil {
		return nil, nil, ArtifactInfo{}, firstErr
	}
	return nil, nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: r.catalog.Name()}
}

// storeLocally persists a remotely-fetched artifact into the local catalog.
// Any non-empty extraLayers (e.g. a solution lock) are stored alongside the
// primary content so subsequent local reads via FetchWithLayer are satisfied.
// Errors are logged but not propagated — the remote fetch already succeeded.
func (r *SolutionResolver) storeLocally(ctx context.Context, ref Reference, info ArtifactInfo, content, bundleData []byte, sourceCatalog string, extraLayers ...Layer) {
	storeRef := ref
	if info.Reference.Version != nil {
		storeRef.Version = info.Reference.Version
	}

	annotations := map[string]string{
		AnnotationArtifactName: storeRef.Name,
		AnnotationArtifactType: storeRef.Kind.String(),
		AnnotationOrigin:       fmt.Sprintf("auto-cached from %s", sourceCatalog),
	}
	if info.Canonical != "" {
		annotations[AnnotationSourceCanonical] = info.Canonical
	}
	if storeRef.Version != nil {
		annotations[AnnotationVersion] = storeRef.Version.String()
	}

	if _, err := r.catalog.Store(ctx, storeRef, content, bundleData, annotations, false, extraLayers...); err != nil {
		r.logger.V(1).Info("failed to store auto-pulled artifact locally (ignoring)", "error", err)
	}
}

// checkRemoteForNewer resolves the latest version from remote catalogs and
// compares it against the locally held version. If a remote has a newer
// version (by semver), it fetches the content and auto-caches it locally.
// Returns (content, info, true) on upgrade, or (nil, zero, false) when no
// upgrade is available or remotes are unreachable.
//
// This implements Docker-style "latest" semantics: unversioned requests always
// check for newer versions when the artifact cache TTL has expired. Pinned
// versions (explicit @version) skip this check entirely.
func (r *SolutionResolver) checkRemoteForNewer(ctx context.Context, ref Reference, localInfo ArtifactInfo) ([]byte, ArtifactInfo, bool) {
	for _, remote := range r.remoteCatalogs {
		remoteInfo, err := remote.Resolve(ctx, ref)
		if err != nil {
			r.logger.V(1).Info("remote version check failed (using local)", "catalog", remote.Name(), "error", err)
			continue
		}

		if !isNewerVersion(remoteInfo, localInfo) {
			r.logger.V(1).Info("local is up-to-date", "local", localInfo.Reference.Version, "remote", remoteInfo.Reference.Version)
			continue
		}

		r.logger.Info("newer version available, pulling from remote",
			"name", ref.Name,
			"localVersion", localInfo.Reference.Version,
			"remoteVersion", remoteInfo.Reference.Version,
			"catalog", remote.Name())

		content, info, err := remote.Fetch(ctx, ref)
		if err != nil {
			r.logger.V(1).Info("remote fetch failed after version check, trying next remote", "catalog", remote.Name(), "error", err)
			continue
		}

		r.storeLocally(ctx, ref, info, content, nil, remote.Name())
		return content, info, true
	}
	return nil, ArtifactInfo{}, false
}

// checkRemoteForNewerWithLayers is the layer-aware variant of checkRemoteForNewer.
func (r *SolutionResolver) checkRemoteForNewerWithLayers(ctx context.Context, ref Reference, localInfo ArtifactInfo, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, bool) {
	for _, remote := range r.remoteCatalogs {
		remoteInfo, err := remote.Resolve(ctx, ref)
		if err != nil {
			r.logger.V(1).Info("remote version check failed (using local)", "catalog", remote.Name(), "error", err)
			continue
		}

		if !isNewerVersion(remoteInfo, localInfo) {
			r.logger.V(1).Info("local is up-to-date", "local", localInfo.Reference.Version, "remote", remoteInfo.Reference.Version)
			continue
		}

		r.logger.Info("newer version available, pulling from remote",
			"name", ref.Name,
			"localVersion", localInfo.Reference.Version,
			"remoteVersion", remoteInfo.Reference.Version,
			"catalog", remote.Name())

		content, layers, info, err := remote.FetchWithLayer(ctx, ref, mediaTypes...)
		if err != nil {
			r.logger.V(1).Info("remote fetch failed after version check, trying next remote", "catalog", remote.Name(), "error", err)
			continue
		}

		// Extract MediaTypeSolutionBundle into the dedicated bundleData slot so
		// the local store keeps the correct OCI layer structure; remaining
		// layers (e.g. lock) are persisted as extra layers.
		bundleData := layers[MediaTypeSolutionBundle]
		var extraLayers []Layer
		for mt, data := range layers {
			if mt == MediaTypeSolutionBundle {
				continue
			}
			if len(data) > 0 {
				extraLayers = append(extraLayers, Layer{MediaType: mt, Data: data})
			}
		}
		r.storeLocally(ctx, ref, info, content, bundleData, remote.Name(), extraLayers...)
		return content, layers, info, true
	}
	return nil, nil, ArtifactInfo{}, false
}

// isNewerVersion returns true when remoteInfo has a strictly higher semver
// version than localInfo, or when the remote has a version while local does not.
func isNewerVersion(remoteInfo, localInfo ArtifactInfo) bool {
	if remoteInfo.Reference.Version == nil {
		return false
	}
	if localInfo.Reference.Version == nil {
		return true
	}
	return remoteInfo.Reference.Version.GreaterThan(localInfo.Reference.Version)
}
