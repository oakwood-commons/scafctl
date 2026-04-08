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
	// When set, the handler is passed to RemoteCatalogConfig.AuthHandler for
	// automatic token bridging. May return nil if no handler is available.
	AuthHandlerFunc func(registry string) scafctlauth.Handler

	// Insecure allows HTTP connections to registries (for testing).
	Insecure bool

	// Logger for logging operations.
	Logger logr.Logger
}

// RemoteSolutionResolver fetches solutions from remote OCI registries given a
// full Docker-style reference (e.g., "ghcr.io/myorg/starter-kit@1.0.0").
// It implements the get.RemoteResolver interface.
type RemoteSolutionResolver struct {
	credStore       *CredentialStore
	authHandlerFunc func(registry string) scafctlauth.Handler
	insecure        bool
	logger          logr.Logger
}

// NewRemoteSolutionResolver creates a new RemoteSolutionResolver.
func NewRemoteSolutionResolver(cfg RemoteSolutionResolverConfig) *RemoteSolutionResolver {
	return &RemoteSolutionResolver{
		credStore:       cfg.CredentialStore,
		authHandlerFunc: cfg.AuthHandlerFunc,
		insecure:        cfg.Insecure,
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

	// Track whether the ref originally had an explicit kind path segment.
	// We default to solution kind for the Reference, but do NOT mutate
	// remoteRef.Kind so that buildRepositoryPath preserves the original
	// repository structure (no injected /solutions/ segment).
	refKind := remoteRef.Kind
	if refKind == "" {
		refKind = ArtifactKindSolution
	}

	// Resolve auth handler for this registry if available
	var authHandler scafctlauth.Handler
	if r.authHandlerFunc != nil {
		authHandler = r.authHandlerFunc(remoteRef.Registry)
	}

	remoteCatalog, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:            remoteRef.Registry,
		Registry:        remoteRef.Registry,
		Repository:      remoteRef.Repository,
		CredentialStore: r.credStore,
		AuthHandler:     authHandler,
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

	return content, bundleData, nil
}
