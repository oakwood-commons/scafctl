// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	scafctlauth "github.com/oakwood-commons/scafctl/pkg/auth"
)

// RemoteSolutionResolverConfig holds configuration for the remote solution resolver.
type RemoteSolutionResolverConfig struct {
	// CredentialStore provides authentication credentials for remote registries.
	CredentialStore *CredentialStore

	// AuthHandlerFunc returns an auth handler for a given registry host.
	// When set, the handler is passed to RemoteCatalogConfig.AuthHandler
	// for automatic token bridging. May return nil if no handler is available.
	AuthHandlerFunc func(registry string) scafctlauth.Handler

	// AuthScopeFunc returns an OAuth scope for a given registry host.
	// When set, the scope is passed to RemoteCatalogConfig.AuthScope so
	// providers like GCP/Entra can request appropriately scoped tokens.
	AuthScopeFunc func(registry string) string

	// Insecure allows HTTP connections to registries (for testing).
	Insecure bool

	// ArtifactCache enables caching of fetched remote solutions.
	// When set, fetched artifacts are stored and served from cache within TTL.
	ArtifactCache ArtifactCacher

	// NoCache disables artifact caching even when ArtifactCache is set.
	NoCache bool

	// Logger for logging operations.
	Logger logr.Logger
}

// RemoteSolutionResolver fetches solutions from remote OCI registries given a
// full Docker-style reference (e.g., "ghcr.io/myorg/starter-kit@1.0.0").
// It implements the get.RemoteResolver interface.
type RemoteSolutionResolver struct {
	credStore       *CredentialStore
	authHandlerFunc func(registry string) scafctlauth.Handler
	authScopeFunc   func(registry string) string
	insecure        bool
	artifactCache   ArtifactCacher
	noCache         bool
	logger          logr.Logger
}

// NewRemoteSolutionResolver creates a new RemoteSolutionResolver.
func NewRemoteSolutionResolver(cfg RemoteSolutionResolverConfig) *RemoteSolutionResolver {
	return &RemoteSolutionResolver{
		credStore:       cfg.CredentialStore,
		authHandlerFunc: cfg.AuthHandlerFunc,
		authScopeFunc:   cfg.AuthScopeFunc,
		insecure:        cfg.Insecure,
		artifactCache:   cfg.ArtifactCache,
		noCache:         cfg.NoCache,
		logger:          cfg.Logger.WithName("remote-solution-resolver"),
	}
}

// FetchRemoteSolution fetches a solution from a remote OCI reference.
// The ref is parsed via ParseRemoteReference. If no kind is specified in the
// path, the kind defaults to ArtifactKindSolution.
func (r *RemoteSolutionResolver) FetchRemoteSolution(ctx context.Context, rawRef string) ([]byte, []byte, error) {
	remoteRef, err := ParseRemoteReference(rawRef)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid remote reference %q: %w", rawRef, err)
	}

	// Check artifact cache before fetching from remote registry.
	if !r.noCache && r.artifactCache != nil {
		cacheKey := remoteRef.Registry + "/" + remoteRef.Repository + "/" + remoteRef.Name
		cacheVersion := remoteRef.Tag
		if cacheVersion == "" {
			cacheVersion = "latest"
		}
		cached, cachedLayers, ok, cacheErr := r.artifactCache.Get(string(ArtifactKindSolution), cacheKey, cacheVersion)
		if cacheErr != nil {
			r.logger.V(1).Info("artifact cache get error (ignoring)", "error", cacheErr)
		} else if ok {
			r.logger.V(1).Info("artifact cache hit for remote solution", "ref", rawRef)
			return cached, cachedLayers[MediaTypeSolutionBundle], nil
		}
	}

	// Track whether the ref originally had an explicit kind path segment.
	// We default to solution kind for the Reference, but do NOT mutate
	// remoteRef.Kind so that buildRepositoryPath preserves the original
	// repository structure (no injected /solutions/ segment).
	refKind := remoteRef.Kind
	if refKind == "" {
		refKind = ArtifactKindSolution
	}

	// Resolve auth handler and scope for this registry if available
	var authHandler scafctlauth.Handler
	if r.authHandlerFunc != nil {
		authHandler = r.authHandlerFunc(remoteRef.Registry)
	}
	var authScope string
	if r.authScopeFunc != nil {
		authScope = r.authScopeFunc(remoteRef.Registry)
	}

	remoteCatalog, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:            remoteRef.Registry,
		Registry:        remoteRef.Registry,
		Repository:      remoteRef.Repository,
		CredentialStore: r.credStore,
		AuthHandler:     authHandler,
		AuthScope:       authScope,
		Insecure:        r.insecure,
		Logger:          r.logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create remote catalog for %q: %w", remoteRef.Registry, err)
	}

	ref, err := remoteRef.ToReference()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid reference %q: %w", rawRef, err)
	}

	// Apply the defaulted kind to the Reference (not the RemoteReference)
	// so FetchWithBundle resolves correctly without altering the repo path.
	if ref.Kind == "" {
		ref.Kind = refKind
	}

	r.logger.V(1).Info("fetching solution from remote registry",
		"registry", remoteRef.Registry,
		"repository", remoteRef.Repository,
		"name", ref.Name,
		"version", ref.Version)

	content, bundleData, info, err := remoteCatalog.FetchWithBundle(ctx, ref)
	if err != nil {
		return nil, nil, err
	}

	r.logger.V(1).Info("fetched solution from remote registry",
		"name", info.Reference.Name,
		"version", info.Reference.Version,
		"digest", info.Digest,
		"hasBundle", len(bundleData) > 0)

	// Store in artifact cache for future reuse.
	// Use remoteRef.Tag as the version key to match the Get lookup above.
	if !r.noCache && r.artifactCache != nil {
		cacheKey := remoteRef.Registry + "/" + remoteRef.Repository + "/" + remoteRef.Name
		putVersion := remoteRef.Tag
		if putVersion == "" {
			putVersion = "latest"
		}
		if putErr := r.artifactCache.Put(string(ArtifactKindSolution), cacheKey, putVersion, info.Digest, content, map[string][]byte{MediaTypeSolutionBundle: bundleData}); putErr != nil {
			r.logger.V(1).Info("artifact cache put error (ignoring)", "error", putErr)
		}
	}

	return content, bundleData, nil
}

// FetchRemoteSolutionWithLayers fetches a solution's primary content together
// with one or more auxiliary layers (each located by media type) from a remote
// OCI reference in a single manifest round-trip. The rawRef is parsed via
// ParseRemoteReference; when no kind is specified in the path, the kind defaults
// to ArtifactKindSolution. The returned map is keyed by the requested media
// type; absent layers are omitted from the map (not an error).
//
// The artifact cache is consulted and populated: a cache hit is only served
// when it contains every requested layer, otherwise the registry is queried and
// the fetched content plus layers are written back to the cache.
func (r *RemoteSolutionResolver) FetchRemoteSolutionWithLayers(ctx context.Context, rawRef string, mediaTypes ...string) ([]byte, map[string][]byte, error) {
	remoteRef, err := ParseRemoteReference(rawRef)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid remote reference %q: %w", rawRef, err)
	}

	cacheKey := remoteRef.Registry + "/" + remoteRef.Repository + "/" + remoteRef.Name
	cacheVersion := remoteRef.Tag
	if cacheVersion == "" {
		cacheVersion = "latest"
	}

	// Check the artifact cache before hitting the registry. A hit is only usable
	// when it contains every requested layer; otherwise fall through to fetch.
	if !r.noCache && r.artifactCache != nil {
		cached, cachedLayers, ok, cacheErr := r.artifactCache.Get(string(ArtifactKindSolution), cacheKey, cacheVersion)
		if cacheErr != nil {
			r.logger.V(1).Info("artifact cache get error (ignoring)", "error", cacheErr)
		} else if ok && hasAllLayers(cachedLayers, mediaTypes) {
			r.logger.V(1).Info("artifact cache hit for remote solution with layers", "ref", rawRef)
			return cached, selectLayers(cachedLayers, mediaTypes), nil
		}
	}

	refKind := remoteRef.Kind
	if refKind == "" {
		refKind = ArtifactKindSolution
	}

	var authHandler scafctlauth.Handler
	if r.authHandlerFunc != nil {
		authHandler = r.authHandlerFunc(remoteRef.Registry)
	}
	var authScope string
	if r.authScopeFunc != nil {
		authScope = r.authScopeFunc(remoteRef.Registry)
	}

	remoteCatalog, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:            remoteRef.Registry,
		Registry:        remoteRef.Registry,
		Repository:      remoteRef.Repository,
		CredentialStore: r.credStore,
		AuthHandler:     authHandler,
		AuthScope:       authScope,
		Insecure:        r.insecure,
		Logger:          r.logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create remote catalog for %q: %w", remoteRef.Registry, err)
	}

	ref, err := remoteRef.ToReference()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid reference %q: %w", rawRef, err)
	}

	if ref.Kind == "" {
		ref.Kind = refKind
	}

	r.logger.V(1).Info("fetching solution from remote registry",
		"registry", remoteRef.Registry,
		"repository", remoteRef.Repository,
		"name", ref.Name,
		"version", ref.Version)

	content, layers, info, err := remoteCatalog.FetchWithLayer(ctx, ref, mediaTypes...)
	if err != nil {
		return nil, nil, err
	}
	r.logger.V(1).Info("fetched solution from remote registry",
		"name", info.Reference.Name,
		"version", info.Reference.Version,
		"digest", info.Digest,
		"hasLayers", len(layers) > 0)

	// Store content and the fetched layers in the artifact cache for reuse.
	if !r.noCache && r.artifactCache != nil {
		if putErr := r.artifactCache.Put(string(ArtifactKindSolution), cacheKey, cacheVersion, info.Digest, content, layers); putErr != nil {
			r.logger.V(1).Info("artifact cache put error (ignoring)", "error", putErr)
		}
	}

	return content, layers, nil
}

// hasAllLayers reports whether layers contains a non-empty entry for every
// requested media type. An empty mediaTypes slice is trivially satisfied.
func hasAllLayers(layers map[string][]byte, mediaTypes []string) bool {
	for _, mt := range mediaTypes {
		if len(layers[mt]) == 0 {
			return false
		}
	}
	return true
}

// selectLayers returns a new map containing only the requested media types that
// are present in layers, mirroring FetchWithLayer's "keyed by requested media
// type" contract. Returns nil when no media types are requested.
func selectLayers(layers map[string][]byte, mediaTypes []string) map[string][]byte {
	if len(mediaTypes) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(mediaTypes))
	for _, mt := range mediaTypes {
		if data, ok := layers[mt]; ok {
			out[mt] = data
		}
	}
	return out
}
