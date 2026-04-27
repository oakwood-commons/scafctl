// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	scafctlauth "github.com/oakwood-commons/scafctl/pkg/auth"
	config "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// RemoteCatalog implements Catalog interface for OCI registries.
type RemoteCatalog struct {
	name              string
	registry          string
	repository        string
	client            *auth.Client
	insecure          bool
	logger            logr.Logger
	enumerator        registryEnumerator
	discoveryStrategy config.DiscoveryStrategy
	authHandlerUsed   string
	credentialSource  atomic.Value // stores string; written from credential callbacks

	// staleCredentials is set to true when OCI operations fall back to
	// anonymous access because stored credentials were rejected by the
	// registry. Used by the CLI layer to emit a user-facing warning.
	staleCredentials atomic.Bool

	// anonOnce ensures switchToAnonymous replaces the client exactly once.
	anonOnce sync.Once
}

// RemoteCatalogConfig holds configuration for creating a remote catalog.
type RemoteCatalogConfig struct {
	// Name is the catalog identifier (e.g., "company-registry")
	Name string

	// RegistryURL is the registry address (e.g., "ghcr.io", "registry.example.com")
	Registry string

	// Repository is the base repository path (e.g., "myorg/scafctl")
	Repository string

	// CredentialStore provides authentication credentials
	CredentialStore *CredentialStore

	// AuthHandler provides dynamic token injection for this catalog.
	// When set, if the CredentialStore has no credentials for the registry,
	// the handler's token is bridged to OCI registry credentials.
	AuthHandler scafctlauth.Handler

	// AuthScope is the OAuth scope for auth handler token requests.
	AuthScope string

	// Insecure allows HTTP connections (for testing)
	Insecure bool

	// DiscoveryStrategy controls how artifacts are discovered.
	// Empty or "auto" uses API then index fallback (default).
	// "index" skips API and fetches the catalog-index directly.
	// "api" uses API only, no index fallback.
	DiscoveryStrategy config.DiscoveryStrategy

	// Logger for logging operations
	Logger logr.Logger
}

// NewRemoteCatalog creates a remote catalog client.
func NewRemoteCatalog(cfg RemoteCatalogConfig) (*RemoteCatalog, error) {
	if cfg.Name == "" {
		cfg.Name = cfg.Registry
	}

	rc := &RemoteCatalog{
		name:              cfg.Name,
		registry:          cfg.Registry,
		repository:        cfg.Repository,
		insecure:          cfg.Insecure,
		discoveryStrategy: cfg.DiscoveryStrategy,
		authHandlerUsed:   authHandlerName(cfg.AuthHandler),
	}

	// Create auth client with retry
	client := &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
	}

	if cfg.CredentialStore != nil {
		if cfg.AuthHandler != nil {
			// Composite credential function: try static credentials first,
			// fall back to dynamic auth handler bridge
			client.Credential = func(ctx context.Context, host string) (auth.Credential, error) {
				cred, source, err := cfg.CredentialStore.CredentialWithSource(ctx, host)
				if err == nil && cred != auth.EmptyCredential {
					rc.credentialSource.Store(source)
					return cred, nil
				}
				// Fall back to auth handler bridge
				username, password, bridgeErr := BridgeAuthToRegistry(ctx, cfg.AuthHandler, host, cfg.AuthScope)
				if bridgeErr != nil {
					cfg.Logger.V(1).Info("auth handler bridge failed, using anonymous",
						"handler", cfg.AuthHandler.Name(),
						"host", host,
						"error", bridgeErr.Error())
					return auth.EmptyCredential, nil
				}
				rc.credentialSource.Store(fmt.Sprintf("%s auth handler token", cfg.AuthHandler.Name()))
				return auth.Credential{
					Username: username,
					Password: password,
				}, nil
			}
		} else {
			client.Credential = func(ctx context.Context, host string) (auth.Credential, error) {
				cred, source, err := cfg.CredentialStore.CredentialWithSource(ctx, host)
				if err == nil && source != "" {
					rc.credentialSource.Store(source)
				}
				return cred, err
			}
		}
	} else if cfg.AuthHandler != nil {
		// No credential store, use auth handler directly
		client.Credential = func(ctx context.Context, host string) (auth.Credential, error) {
			username, password, bridgeErr := BridgeAuthToRegistry(ctx, cfg.AuthHandler, host, cfg.AuthScope)
			if bridgeErr != nil {
				cfg.Logger.V(1).Info("auth handler bridge failed, using anonymous",
					"handler", cfg.AuthHandler.Name(),
					"host", host,
					"error", bridgeErr.Error())
				return auth.EmptyCredential, nil //nolint:nilerr // graceful degradation to anonymous auth
			}
			rc.credentialSource.Store(fmt.Sprintf("%s auth handler token", cfg.AuthHandler.Name()))
			return auth.Credential{
				Username: username,
				Password: password,
			}, nil
		}
	}

	// Set insecure for local development/testing
	if cfg.Insecure {
		client.Client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // Opt-in via --insecure flag for local dev/testing only
				},
			},
		}
	}

	catalogLogger := cfg.Logger.WithName("remote-catalog").WithValues("catalog", cfg.Name)

	enumCfg := enumeratorConfig{
		authHandlerName: authHandlerName(cfg.AuthHandler),
		authHandler:     cfg.AuthHandler,
		authScope:       cfg.AuthScope,
		registry:        cfg.Registry,
		repository:      cfg.Repository,
		client:          client,
		insecure:        cfg.Insecure,
		logger:          catalogLogger,
	}

	rc.client = client
	rc.logger = catalogLogger
	rc.enumerator = selectEnumerator(enumCfg)

	return rc, nil
}

// authHandlerName returns the handler name or empty string if nil.
func authHandlerName(h scafctlauth.Handler) string {
	if h == nil {
		return ""
	}
	return h.Name()
}

// Name returns the catalog identifier.
func (c *RemoteCatalog) Name() string {
	return c.name
}

// Registry returns the registry address.
func (c *RemoteCatalog) Registry() string {
	return c.registry
}

// Repository returns the base repository path.
func (c *RemoteCatalog) Repository() string {
	return c.repository
}

// clientUpdatable is implemented by enumerators that can update the OCI
// auth client they use for registry operations.
type clientUpdatable interface {
	setClient(client *auth.Client)
}

// SetClient overrides the OCI auth client used for registry operations.
// This is useful for injecting credentials in tooling (e.g. push-index).
// When the catalog enumerator also supports client replacement, it is
// updated so that enumeration and repository access use the same credentials.
func (c *RemoteCatalog) SetClient(client *auth.Client) {
	c.client = client

	if updatable, ok := c.enumerator.(clientUpdatable); ok {
		updatable.setClient(client)
	}
}

// isOCIAuthError returns true if the error looks like a registry
// authentication/authorization failure (401 or 403).
func isOCIAuthError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "401") ||
		strings.Contains(s, "403") ||
		strings.Contains(s, "unauthorized") ||
		strings.Contains(s, "denied")
}

// anonymousClient returns a plain auth.Client with no credentials.
// Used to retry OCI operations when the stored credentials are rejected
// (e.g. expired token) but the resource may be publicly accessible.
func (c *RemoteCatalog) anonymousClient() *auth.Client {
	client := &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
	}
	if c.insecure {
		client.Client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // Opt-in via --insecure flag for local dev/testing only
				},
			},
		}
	}
	return client
}

// switchToAnonymous replaces the OCI client with an anonymous (no-credentials)
// client and records the fact that stale credentials were detected. All
// subsequent OCI operations on this RemoteCatalog will use anonymous access.
// The replacement is guarded by sync.Once so concurrent callers do not race.
func (c *RemoteCatalog) switchToAnonymous() {
	c.anonOnce.Do(func() {
		c.client = c.anonymousClient()
		c.staleCredentials.Store(true)
		c.logger.Info("switched to anonymous OCI client — stored credentials were rejected")
	})
}

// HasStaleCredentials returns true if any OCI operation during the lifetime
// of this catalog client fell back to anonymous access because the stored
// credentials were rejected by the registry. Callers can use this to show
// a user-facing hint suggesting re-authentication.
func (c *RemoteCatalog) HasStaleCredentials() bool {
	return c.staleCredentials.Load()
}

// SetStaleForTesting marks the catalog as having stale credentials.
// This is intended for use by CLI-layer tests that need to exercise
// the user-facing warning path.
func (c *RemoteCatalog) SetStaleForTesting() {
	c.staleCredentials.Store(true)
}

// SetCredentialSourceForTest stores a credential source string.
// This is intended for use by CLI-layer tests that need to exercise
// the credential-source display path.
func (c *RemoteCatalog) SetCredentialSourceForTest(source string) {
	c.credentialSource.Store(source)
}

// AuthHandlerUsed returns the name of the auth handler configured for this
// catalog (e.g. "github", "gcp", "entra"). Returns empty if no handler.
func (c *RemoteCatalog) AuthHandlerUsed() string {
	return c.authHandlerUsed
}

// CredentialSource returns a human-readable description of the credential
// source that was last resolved for registry authentication (e.g.
// "docker credential helper (desktop)", "github auth handler token",
// "native credential store"). Empty when no credentials were resolved.
func (c *RemoteCatalog) CredentialSource() string {
	if v, ok := c.credentialSource.Load().(string); ok {
		return v
	}
	return ""
}

// getRepository creates a remote.Repository for an artifact.
func (c *RemoteCatalog) getRepository(ref Reference) (*remote.Repository, error) {
	// Build full repository path: registry/repository/name
	repoPath := c.buildRepositoryPath(ref)

	repo, err := remote.NewRepository(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	repo.Client = c.client

	return repo, nil
}

// RepositoryPath returns the full OCI repository path for an artifact reference.
// This is useful for displaying the resolved path in CLI output.
func (c *RemoteCatalog) RepositoryPath(ref Reference) string {
	return c.buildRepositoryPath(ref)
}

// buildRepositoryPath builds the full OCI repository path for an artifact.
// The path includes the pluralized kind segment for namespace isolation:
//
//	registry/repository/solutions/name
//	registry/repository/providers/name
//	registry/repository/auth-handlers/name
//
// When kind is empty, the kind segment is omitted (used for kindless lookups).
func (c *RemoteCatalog) buildRepositoryPath(ref Reference) string {
	parts := []string{c.registry}
	if c.repository != "" {
		parts = append(parts, c.repository)
	}
	if ref.Kind != "" {
		parts = append(parts, ref.Kind.Plural())
	}
	parts = append(parts, ref.Name)

	return strings.Join(parts, "/")
}

// Store saves an artifact to the remote catalog.
// For solutions with bundled files, bundleData contains the tar archive.
// If bundleData is nil, only the primary content layer is stored.
func (c *RemoteCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool) (ArtifactInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	// Check if artifact already exists (unless force is set)
	if !force {
		exists, err := c.Exists(ctx, ref)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("failed to check existence: %w", err)
		}
		if exists {
			return ArtifactInfo{}, &ArtifactExistsError{Reference: ref, Catalog: c.name}
		}
	}

	// Create the OCI artifact
	now := time.Now().UTC()

	// Merge annotations with required fields
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[AnnotationArtifactType] = ref.Kind.String()
	annotations[AnnotationArtifactName] = ref.Name
	if ref.Version != nil {
		annotations[AnnotationVersion] = ref.Version.String()
	}
	annotations[AnnotationCreated] = now.Format(time.RFC3339)

	// Create content layer
	contentDesc := ocispec.Descriptor{
		MediaType: MediaTypeForKind(ref.Kind),
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}

	if err := repo.Push(ctx, contentDesc, bytes.NewReader(content)); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push content blob: %w", err)
	}

	// Create config blob with metadata
	configData, err := json.Marshal(map[string]any{
		"kind":        ref.Kind.String(),
		"name":        ref.Name,
		"version":     ref.Version.String(),
		"createdAt":   now.Format(time.RFC3339),
		"annotations": annotations,
	})
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to marshal config: %w", err)
	}

	configDesc := ocispec.Descriptor{
		MediaType: ConfigMediaTypeForKind(ref.Kind),
		Digest:    digest.FromBytes(configData),
		Size:      int64(len(configData)),
	}

	if err := repo.Push(ctx, configDesc, bytes.NewReader(configData)); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push config blob: %w", err)
	}

	// Create manifest layers
	layers := []ocispec.Descriptor{contentDesc}

	// Add bundle layer if present
	if len(bundleData) > 0 {
		bundleDesc := ocispec.Descriptor{
			MediaType: MediaTypeSolutionBundle,
			Digest:    digest.FromBytes(bundleData),
			Size:      int64(len(bundleData)),
		}

		if err := repo.Push(ctx, bundleDesc, bytes.NewReader(bundleData)); err != nil {
			return ArtifactInfo{}, fmt.Errorf("failed to push bundle blob: %w", err)
		}

		layers = append(layers, bundleDesc)
	}

	// Pack and push the OCI manifest
	manifestDesc, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, MediaTypeForKind(ref.Kind), oras.PackManifestOptions{
		Layers:              layers,
		ManifestAnnotations: annotations,
		ConfigDescriptor:    &configDesc,
	})
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to pack manifest: %w", err)
	}

	// Tag the manifest with version
	tag := c.tagForRef(ref)
	if err := repo.Tag(ctx, manifestDesc, tag); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to tag manifest: %w", err)
	}

	c.logger.V(1).Info("stored artifact",
		"name", ref.Name,
		"version", ref.Version.String(),
		"digest", manifestDesc.Digest.String())

	return ArtifactInfo{
		Reference:   ref,
		Digest:      manifestDesc.Digest.String(),
		CreatedAt:   now,
		Size:        int64(len(content)),
		Annotations: annotations,
		Catalog:     c.name,
	}, nil
}

// Fetch retrieves an artifact from the remote catalog.
func (c *RemoteCatalog) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	data, info, err := c.fetchInternal(ctx, ref)
	if err != nil && isOCIAuthError(err) {
		c.logger.V(1).Info("fetch rejected by registry, retrying anonymously",
			"kind", ref.Kind, "name", ref.Name, "error", err.Error())
		c.switchToAnonymous()
		data, info, retryErr := c.fetchInternal(ctx, ref)
		if retryErr != nil {
			return nil, ArtifactInfo{}, fmt.Errorf("anonymous retry failed (%w) after auth error: %w", retryErr, err)
		}
		return data, info, nil
	}
	return data, info, err
}

func (c *RemoteCatalog) fetchInternal(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	// When no version is specified, resolve to the latest version first.
	if !ref.HasVersion() && !ref.HasDigest() {
		resolved, err := c.resolveWithKind(ctx, ref)
		if err != nil {
			return nil, ArtifactInfo{}, err
		}
		ref = resolved.Reference
	}

	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	// Resolve to get the manifest descriptor
	tag := c.tagForRef(ref)
	manifestDesc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Fetch manifest with digest verification
	manifestData, err := content.FetchAll(ctx, repo, manifestDesc)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to fetch manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, ArtifactInfo{}, fmt.Errorf("manifest has no content layers")
	}

	// Fetch content layer with digest verification
	contentData, err := content.FetchAll(ctx, repo, manifest.Layers[0])
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to fetch content: %w", err)
	}

	// Parse annotations for metadata
	createdAt := time.Now()
	if created, ok := manifest.Annotations[AnnotationCreated]; ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			createdAt = t
		}
	}

	info := ArtifactInfo{
		Reference:   ref,
		Digest:      manifestDesc.Digest.String(),
		CreatedAt:   createdAt,
		Size:        int64(len(contentData)),
		Annotations: manifest.Annotations,
		Catalog:     c.name,
	}

	return contentData, info, nil
}

// FetchWithBundle retrieves an artifact's primary content and bundle layer.
// If the artifact has no bundle layer, bundleData is nil.
func (c *RemoteCatalog) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	data, bundle, info, err := c.fetchWithBundleInternal(ctx, ref)
	if err != nil && isOCIAuthError(err) {
		c.logger.V(1).Info("fetch rejected by registry, retrying anonymously",
			"kind", ref.Kind, "name", ref.Name, "error", err.Error())
		c.switchToAnonymous()
		data, bundle, info, retryErr := c.fetchWithBundleInternal(ctx, ref)
		if retryErr != nil {
			return nil, nil, ArtifactInfo{}, fmt.Errorf("anonymous retry failed (%w) after auth error: %w", retryErr, err)
		}
		return data, bundle, info, nil
	}
	return data, bundle, info, err
}

func (c *RemoteCatalog) fetchWithBundleInternal(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	// When no version is specified, resolve to the latest version first.
	if !ref.HasVersion() && !ref.HasDigest() {
		resolved, err := c.resolveWithKind(ctx, ref)
		if err != nil {
			return nil, nil, ArtifactInfo{}, err
		}
		ref = resolved.Reference
	}

	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, nil, ArtifactInfo{}, err
	}

	// Resolve to get the manifest descriptor
	tag := c.tagForRef(ref)
	manifestDesc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Fetch manifest with digest verification
	manifestData, err := content.FetchAll(ctx, repo, manifestDesc)
	if err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("manifest has no content layers")
	}

	// Fetch content layer with digest verification
	contentData, err := content.FetchAll(ctx, repo, manifest.Layers[0])
	if err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch content: %w", err)
	}

	// Fetch bundle layer if present
	var bundleData []byte
	if len(manifest.Layers) > 1 {
		switch manifest.Layers[1].MediaType {
		case MediaTypeSolutionBundle:
			// Version 1: single tar layer
			bundleData, err = content.FetchAll(ctx, repo, manifest.Layers[1])
			if err != nil {
				return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch bundle: %w", err)
			}
		case MediaTypeSolutionBundleManifest:
			// Version 2: deduplicated — reassemble into a v1-compatible tar
			fetchBlob := func(desc ocispec.Descriptor) ([]byte, error) {
				return content.FetchAll(ctx, repo, desc)
			}
			bundleData, err = reassembleDedupBundle(manifest, fetchBlob)
			if err != nil {
				return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to reassemble dedup bundle: %w", err)
			}
		}
	}

	// Parse annotations for metadata
	createdAt := time.Now()
	if created, ok := manifest.Annotations[AnnotationCreated]; ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			createdAt = t
		}
	}

	info := ArtifactInfo{
		Reference:   ref,
		Digest:      manifestDesc.Digest.String(),
		CreatedAt:   createdAt,
		Size:        int64(len(contentData)),
		Annotations: manifest.Annotations,
		Catalog:     c.name,
	}

	return contentData, bundleData, info, nil
}

// Resolve finds the best matching version for a reference.
func (c *RemoteCatalog) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	// When kind is empty, try resolving across all kind paths (solutions/,
	// providers/, auth-handlers/). This handles the common case where a user
	// does `catalog pull my-solution@1.0.0` without specifying --kind.
	if ref.Kind == "" {
		info, err := c.resolveAcrossKinds(ctx, ref)
		if err == nil {
			return info, nil
		}
		// Auth/network errors should not fall through to kindless resolution —
		// the registry is reachable but failing, so retrying without a kind
		// prefix will likely hit the same error.
		if !IsNotFound(err) {
			return ArtifactInfo{}, err
		}
		// Fall through to kindless resolution as a last resort (for registries
		// that don't use kind path prefixes).
	}

	return c.resolveWithKind(ctx, ref)
}

// resolveAcrossKinds tries to resolve a reference by probing each known kind
// path prefix. Returns the first successful match.
func (c *RemoteCatalog) resolveAcrossKinds(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	kinds := []ArtifactKind{ArtifactKindSolution, ArtifactKindProvider, ArtifactKindAuthHandler}
	var firstErr error
	for _, k := range kinds {
		candidate := ref
		candidate.Kind = k
		info, err := c.resolveWithKind(ctx, candidate)
		if err == nil {
			c.logger.V(1).Info("resolved artifact kind by probing remote",
				"name", ref.Name, "kind", k, "version", info.Reference.Version)
			return info, nil
		}
		if !IsNotFound(err) {
			c.logger.V(1).Info("remote catalog kind probe error",
				"catalog", c.name, "kind", k, "error", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("remote catalog %q kind %q: %w", c.name, k, err)
			}
		}
	}
	if firstErr != nil {
		return ArtifactInfo{}, firstErr
	}
	return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
}

// resolveWithKind resolves a reference that has a known kind (or is intentionally kindless).
func (c *RemoteCatalog) resolveWithKind(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	// If version is specified, resolve directly
	if ref.HasVersion() || ref.HasDigest() {
		repo, err := c.getRepository(ref)
		if err != nil {
			return ArtifactInfo{}, err
		}

		tag := c.tagForRef(ref)
		desc, err := repo.Resolve(ctx, tag)
		if err != nil && isOCIAuthError(err) {
			c.logger.V(1).Info("resolve rejected by registry, retrying anonymously",
				"kind", ref.Kind, "name", ref.Name, "error", err.Error())
			c.switchToAnonymous()
			repo.Client = c.client
			desc, err = repo.Resolve(ctx, tag)
		}
		if err != nil {
			return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
		}

		return ArtifactInfo{
			Reference: ref,
			Digest:    desc.Digest.String(),
			Size:      desc.Size,
			Catalog:   c.name,
		}, nil
	}

	// No version specified - find the latest
	versions, err := c.listVersions(ctx, ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	if len(versions) == 0 {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Filter out pre-release versions unless explicitly included
	if !IncludePreReleaseFromContext(ctx) {
		stable := make([]*semver.Version, 0, len(versions))
		for _, v := range versions {
			if IsPreRelease(v) {
				c.logger.V(1).Info("skipping pre-release version",
					"name", ref.Name,
					"version", v.String())
				continue
			}
			stable = append(stable, v)
		}
		if len(stable) > 0 {
			versions = stable
		} else {
			c.logger.V(1).Info("no stable versions found, falling back to pre-release",
				"name", ref.Name)
		}
	}

	// Sort versions descending
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].GreaterThan(versions[j])
	})

	// Return highest version
	ref.Version = versions[0]
	c.logger.V(1).Info("resolved latest version",
		"name", ref.Name,
		"version", ref.Version.String(),
		"catalog", c.name)
	return c.resolveWithKind(ctx, ref)
}

// listVersions lists all versions of an artifact.
func (c *RemoteCatalog) listVersions(ctx context.Context, ref Reference) ([]*semver.Version, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, err
	}

	versions, err := c.fetchTags(ctx, repo)
	if err != nil && isOCIAuthError(err) {
		c.logger.V(1).Info("tag fetch rejected by registry, retrying anonymously",
			"kind", ref.Kind, "name", ref.Name, "error", err.Error())
		c.switchToAnonymous()
		repo.Client = c.client
		anonVersions, anonErr := c.fetchTags(ctx, repo)
		if anonErr != nil {
			return nil, fmt.Errorf("failed to list tags: %w", err)
		}
		return anonVersions, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}

	return versions, nil
}

// fetchTags enumerates semver tags from a repository.
func (c *RemoteCatalog) fetchTags(ctx context.Context, repo *remote.Repository) ([]*semver.Version, error) {
	var versions []*semver.Version

	err := repo.Tags(ctx, "", func(tags []string) error {
		for _, tag := range tags {
			if v, err := semver.NewVersion(tag); err == nil {
				versions = append(versions, v)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return versions, nil
}

// ResolveLatestVersions enriches a slice of discovered artifacts with their
// latest semver version by fetching tags from the registry. Uses bounded
// concurrency to avoid overwhelming the registry. Artifacts whose tags
// cannot be fetched are left with an empty LatestVersion.
func (c *RemoteCatalog) ResolveLatestVersions(ctx context.Context, artifacts []DiscoveredArtifact) {
	sem := make(chan struct{}, settings.DefaultRegistryConcurrency)
	var wg sync.WaitGroup

	for i := range artifacts {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			ref := Reference{Kind: artifacts[idx].Kind, Name: artifacts[idx].Name}
			versions, err := c.listVersions(ctx, ref)
			if err != nil {
				c.logger.V(1).Info("failed to resolve latest version",
					"kind", artifacts[idx].Kind, "name", artifacts[idx].Name, "error", err.Error())
				return
			}

			if latest := latestVersion(versions); latest != nil {
				artifacts[idx].LatestVersion = latest.String()
			}
		}(i)
	}
	wg.Wait()
}

// latestVersion returns the highest semver version from the slice, or nil.
func latestVersion(versions []*semver.Version) *semver.Version {
	if len(versions) == 0 {
		return nil
	}
	best := versions[0]
	for _, v := range versions[1:] {
		if v.GreaterThan(best) {
			best = v
		}
	}
	return best
}

// List returns all artifacts matching the criteria.
func (c *RemoteCatalog) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	// If name is specified, list versions of that artifact
	if name != "" {
		// When kind is not specified, search across all kind paths
		if kind == "" {
			return c.listAcrossKinds(ctx, name)
		}

		ref := Reference{Kind: kind, Name: name}
		versions, err := c.listVersions(ctx, ref)
		if err != nil {
			return nil, err
		}

		var infos []ArtifactInfo
		for _, v := range versions {
			infos = append(infos, ArtifactInfo{
				Reference: Reference{Kind: kind, Name: name, Version: v},
				Catalog:   c.name,
			})
		}
		return infos, nil
	}

	// Enumerate all artifacts in the registry using the _catalog endpoint.
	return c.listAllArtifacts(ctx, kind)
}

// listAcrossKinds searches for an artifact name across all kind paths
// (solutions/, providers/, auth-handlers/) and returns combined results.
func (c *RemoteCatalog) listAcrossKinds(ctx context.Context, name string) ([]ArtifactInfo, error) {
	kinds := []ArtifactKind{ArtifactKindSolution, ArtifactKindProvider, ArtifactKindAuthHandler}
	var allInfos []ArtifactInfo

	for _, k := range kinds {
		ref := Reference{Kind: k, Name: name}
		versions, err := c.listVersions(ctx, ref)
		if err != nil {
			c.logger.V(1).Info("no artifacts found under kind", "kind", k, "name", name)
			continue
		}
		for _, v := range versions {
			allInfos = append(allInfos, ArtifactInfo{
				Reference: Reference{Kind: k, Name: name, Version: v},
				Catalog:   c.name,
			})
		}
	}

	return allInfos, nil
}

// DiscoveredArtifact represents an artifact discovered via registry enumeration.
// Exported for use by embedders that call ListRepositories directly.
type DiscoveredArtifact struct {
	Kind          ArtifactKind `json:"kind"           yaml:"kind"           doc:"Artifact kind" example:"solution" enum:"solution,provider,auth-handler"`
	Name          string       `json:"name"           yaml:"name"           doc:"Artifact name" example:"starter-kit" maxLength:"255"`
	LatestVersion string       `json:"latestVersion"  yaml:"latestVersion"  doc:"Latest semver version" example:"1.2.0"`

	// Enriched metadata fields, populated from solution YAML when available.
	// Empty for old indexes or non-solution artifacts.
	Description string   `json:"description,omitempty" yaml:"description,omitempty" doc:"Solution description" maxLength:"5000"`
	DisplayName string   `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-friendly display name" maxLength:"80"`
	Category    string   `json:"category,omitempty"    yaml:"category,omitempty"    doc:"Solution category" example:"deployment" maxLength:"30"`
	Tags        []string `json:"tags,omitempty"        yaml:"tags,omitempty"        doc:"Searchable keywords" maxItems:"100"`

	// Extended metadata for MCP and rich discovery. Populated during index push
	// from the solution YAML's metadata and resolver sections.
	Maintainers []string         `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"Maintainer names" maxItems:"10"`
	Links       []DiscoveredLink `json:"links,omitempty"       yaml:"links,omitempty"       doc:"Related links" maxItems:"10"`
	Providers   []string         `json:"providers,omitempty"   yaml:"providers,omitempty"   doc:"Providers used by the solution" maxItems:"50"`
	Parameters  []string         `json:"parameters,omitempty"  yaml:"parameters,omitempty"  doc:"Parameter resolver names (user inputs)" maxItems:"50"`
}

// ToAnnotations converts enriched metadata fields into an OCI annotation map.
// Only non-empty fields are included.
func (d DiscoveredArtifact) ToAnnotations() map[string]string {
	return NewAnnotationBuilder().
		Set(AnnotationDisplayName, d.DisplayName).
		Set(AnnotationDescription, d.Description).
		Set(AnnotationCategory, d.Category).
		SetTags(d.Tags).
		Build()
}

// DiscoveredLink is a named URL reference (documentation, homepage, etc.).
type DiscoveredLink struct {
	Name string `json:"name" yaml:"name" doc:"Link label" example:"Documentation" maxLength:"30"`
	URL  string `json:"url"  yaml:"url"  doc:"Link URL" example:"https://example.com/docs" maxLength:"500"`
}

// listAllArtifacts enumerates repositories under this catalog's prefix,
// parses artifact kind+name from the repository paths, and fetches version
// info for each discovered artifact using bounded concurrency.
//
// Filters applied before the expensive tag-fetch step:
//   - kind: only artifacts of the specified kind
//   - search pattern: only artifacts whose name contains the query (from context)
func (c *RemoteCatalog) listAllArtifacts(ctx context.Context, kind ArtifactKind) ([]ArtifactInfo, error) {
	discovered, err := c.ListRepositories(ctx)
	if err != nil {
		return nil, err
	}

	// Pre-filter before the expensive tag-fetch step.
	searchPattern := SearchPatternFromContext(ctx)
	var filtered []DiscoveredArtifact
	for _, d := range discovered {
		if kind != "" && d.Kind != kind {
			continue
		}
		if !matchesSearchPattern(searchPattern, d.Name) {
			c.logger.V(1).Info("skipping artifact (search filter)",
				"name", d.Name, "pattern", searchPattern)
			continue
		}
		filtered = append(filtered, d)
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	// Fetch tags concurrently with bounded parallelism.
	type result struct {
		artifact DiscoveredArtifact
		versions []*semver.Version
	}

	results := make([]result, len(filtered))
	sem := make(chan struct{}, settings.DefaultRegistryConcurrency)
	var wg sync.WaitGroup
	var failCount atomic.Int32

	for i, d := range filtered {
		wg.Add(1)
		go func(idx int, art DiscoveredArtifact) {
			defer wg.Done()

			// Acquire semaphore slot (or bail on context cancellation).
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			ref := Reference{Kind: art.Kind, Name: art.Name}
			versions, err := c.listVersions(ctx, ref)
			if err != nil {
				failCount.Add(1)
				c.logger.V(1).Info("failed to list versions for discovered artifact",
					"kind", art.Kind, "name", art.Name, "error", err.Error())
				return
			}
			results[idx] = result{artifact: art, versions: versions}
		}(i, d)
	}
	wg.Wait()

	if n := failCount.Load(); n > 0 {
		c.logger.Info("some artifacts could not be listed (version fetch failed)",
			"failed", n, "total", len(filtered))
	}

	// Collect results (order preserved by index).
	var allInfos []ArtifactInfo
	for _, r := range results {
		annotations := r.artifact.ToAnnotations()
		for _, v := range r.versions {
			allInfos = append(allInfos, ArtifactInfo{
				Reference:   Reference{Kind: r.artifact.Kind, Name: r.artifact.Name, Version: v},
				Catalog:     c.name,
				Annotations: annotations,
			})
		}
	}

	// When all version fetches failed (e.g. 403 on private GHCR packages),
	// use the LatestVersion already carried by DiscoveredArtifact (populated
	// by the index fallback in ListRepositories).
	if len(allInfos) == 0 && len(filtered) > 0 && c.discoveryStrategy != config.DiscoveryStrategyAPI {
		c.logger.V(1).Info("all version fetches failed, using discovered artifact metadata")
		for _, d := range filtered {
			if d.LatestVersion == "" {
				continue
			}
			v, err := semver.NewVersion(d.LatestVersion)
			if err != nil {
				c.logger.V(1).Info("skipping artifact with invalid version",
					"name", d.Name, "version", d.LatestVersion, "error", err.Error())
				continue
			}
			allInfos = append(allInfos, ArtifactInfo{
				Reference:   Reference{Kind: d.Kind, Name: d.Name, Version: v},
				Catalog:     c.name,
				Annotations: d.ToAnnotations(),
			})
		}
	}

	return allInfos, nil
}

// ListRepositories enumerates all artifact repositories in this catalog using
// a registry-specific enumerator. The enumerator is selected automatically
// based on the auth handler name and registry hostname.
//
// Returns ErrEnumerationNotSupported if the registry cannot be enumerated.
func (c *RemoteCatalog) ListRepositories(ctx context.Context) ([]DiscoveredArtifact, error) {
	// Apply a timeout to the entire enumeration flow so slow or
	// non-responsive endpoints do not block indefinitely.
	ctx, cancel := context.WithTimeout(ctx, settings.DefaultHTTPTimeout)
	defer cancel()

	// Index-only strategy: skip API enumeration entirely.
	if c.discoveryStrategy == config.DiscoveryStrategyIndex {
		c.logger.V(1).Info("using index-only discovery strategy")
		indexed, err := c.FetchIndex(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetching catalog index for %s: %w", c.name, err)
		}
		return indexed, nil
	}

	repos, err := c.enumerator.enumerate(ctx)
	if err != nil {
		// If enumeration is not supported (e.g. GHCR without org auth),
		// fall back to the well-known catalog-index artifact (unless api-only).
		if IsEnumerationNotSupported(err) && c.discoveryStrategy != config.DiscoveryStrategyAPI {
			c.logger.V(1).Info("enumeration not supported, trying catalog index fallback")
			indexed, indexErr := c.FetchIndex(ctx)
			if indexErr != nil {
				c.logger.V(1).Info("catalog index fallback failed",
					"error", indexErr.Error())
				return nil, fmt.Errorf("enumerating repositories in %s: %w", c.name, err)
			}
			return indexed, nil
		}
		return nil, fmt.Errorf("enumerating repositories in %s: %w", c.name, err)
	}

	var discovered []DiscoveredArtifact
	seen := make(map[string]bool)

	for _, repo := range repos {
		d, ok := c.parseRepositoryPath(repo)
		if !ok {
			continue
		}
		key := string(d.Kind) + "/" + d.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		discovered = append(discovered, d)
	}

	// Auto strategy: when API enumeration returns zero results (e.g. GHCR
	// without org-level auth), fall back to the catalog index artifact.
	if len(discovered) == 0 && c.discoveryStrategy != config.DiscoveryStrategyAPI {
		c.logger.V(1).Info("API enumeration returned no results, trying catalog index fallback")
		indexed, indexErr := c.FetchIndex(ctx)
		if indexErr != nil {
			c.logger.V(1).Info("catalog index fallback failed", "error", indexErr.Error())
			// Not an error — enumeration legitimately returned nothing.
			return discovered, nil
		}
		return indexed, nil
	}

	// Auto strategy: enrich API-discovered artifacts with index metadata
	// (LatestVersion, Description, etc.) so callers can use it when per-repo
	// version fetches are unavailable (e.g. 403 on private GHCR packages).
	if c.discoveryStrategy != config.DiscoveryStrategyAPI {
		indexed, indexErr := c.FetchIndex(ctx)
		if indexErr != nil {
			c.logger.V(1).Info("index enrichment failed", "error", indexErr.Error())
		} else {
			indexMap := make(map[string]DiscoveredArtifact, len(indexed))
			for _, a := range indexed {
				indexMap[string(a.Kind)+"/"+a.Name] = a
			}
			for i, d := range discovered {
				key := string(d.Kind) + "/" + d.Name
				if enriched, ok := indexMap[key]; ok {
					discovered[i] = enriched
				}
			}
		}
	}

	c.logger.V(1).Info("discovered artifacts",
		"count", len(discovered), "registry", c.registry)
	return discovered, nil
}

// parseRepositoryPath extracts the artifact kind and name from a full
// repository path by stripping the catalog's base prefix and splitting
// the remainder into kind-plural/name segments.
//
// Example: given repository="myorg/scafctl" and path="myorg/scafctl/solutions/myapp",
// returns (ArtifactKindSolution, "myapp", true).
func (c *RemoteCatalog) parseRepositoryPath(repoPath string) (DiscoveredArtifact, bool) {
	prefix := c.repository
	if prefix != "" {
		if !strings.HasPrefix(repoPath, prefix+"/") {
			return DiscoveredArtifact{}, false
		}
		repoPath = repoPath[len(prefix)+1:]
	}

	// Expect exactly "kind-plural/name"
	parts := strings.SplitN(repoPath, "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return DiscoveredArtifact{}, false
	}

	kind, ok := ParseArtifactKindFromPlural(parts[0])
	if !ok {
		return DiscoveredArtifact{}, false
	}

	return DiscoveredArtifact{Kind: kind, Name: parts[1]}, true
}

// TagInfo represents a single tag in a remote OCI repository.
type TagInfo struct {
	Tag      string `json:"tag" yaml:"tag"`
	IsSemver bool   `json:"isSemver" yaml:"isSemver"`
	Version  string `json:"version,omitempty" yaml:"version,omitempty"`
}

// ListTags returns all tags (semver versions and aliases) for an artifact
// in the remote registry.
func (c *RemoteCatalog) ListTags(ctx context.Context, ref Reference) ([]TagInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, err
	}

	tags, err := c.listTagsFromRepo(ctx, repo, ref)
	if err != nil && isOCIAuthError(err) {
		c.logger.V(1).Info("tag list rejected by registry, retrying anonymously",
			"kind", ref.Kind, "name", ref.Name, "error", err.Error())
		c.switchToAnonymous()
		repo.Client = c.client
		tags, retryErr := c.listTagsFromRepo(ctx, repo, ref)
		if retryErr != nil {
			return nil, fmt.Errorf("anonymous retry failed (%w) after auth error: %w", retryErr, err)
		}
		return tags, nil
	}
	return tags, err
}

func (c *RemoteCatalog) listTagsFromRepo(ctx context.Context, repo *remote.Repository, ref Reference) ([]TagInfo, error) {
	var tags []TagInfo

	err := repo.Tags(ctx, "", func(rawTags []string) error {
		for _, tag := range rawTags {
			info := TagInfo{Tag: tag}
			if v, parseErr := semver.NewVersion(tag); parseErr == nil {
				info.IsSemver = true
				info.Version = v.String()
			}
			tags = append(tags, info)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for %q: %w", ref.Name, err)
	}

	// Sort: semver tags first (descending), then aliases alphabetically
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].IsSemver != tags[j].IsSemver {
			return tags[i].IsSemver
		}
		if tags[i].IsSemver {
			vi, _ := semver.NewVersion(tags[i].Version)
			vj, _ := semver.NewVersion(tags[j].Version)
			return vi.GreaterThan(vj)
		}
		return tags[i].Tag < tags[j].Tag
	})

	return tags, nil
}

// Exists checks if an artifact exists in the catalog.
func (c *RemoteCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return false, err
	}

	if !ref.HasVersion() && !ref.HasDigest() {
		return false, fmt.Errorf("cannot check existence without version or digest for %q", ref.Name)
	}

	tag := c.tagForRef(ref)
	_, err = repo.Resolve(ctx, tag)
	if err != nil && isOCIAuthError(err) {
		c.logger.V(1).Info("existence check rejected by registry, retrying anonymously",
			"kind", ref.Kind, "name", ref.Name, "error", err.Error())
		c.switchToAnonymous()
		repo.Client = c.client
		_, err = repo.Resolve(ctx, tag)
	}
	//nolint:errcheck // Resolve error means artifact doesn't exist
	return err == nil, nil
}

// Delete removes an artifact from the catalog.
//
// The method first attempts a standard OCI delete by digest. If the registry
// rejects it because the manifest still has tags (e.g., GCP Artifact Registry
// returns 400 "dangling tag"), it retries by deleting via the tag reference.
func (c *RemoteCatalog) Delete(ctx context.Context, ref Reference) error {
	repo, err := c.getRepository(ref)
	if err != nil {
		return err
	}

	// Resolve to get the manifest descriptor
	tag := c.tagForRef(ref)
	desc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Delete the manifest (this may not be supported by all registries)
	if err := repo.Delete(ctx, desc); err != nil {
		errStr := err.Error()

		// Some registries require tag-based DELETE instead of digest-based:
		//   - GCP Artifact Registry rejects digest-based DELETE when the manifest
		//     still has a tag ("dangling tag" / "still referenced").
		//   - Quay-based registries reject the "delete" auth scope action in the
		//     Docker V2 token endpoint, returning a 400 ("Unable to decode").
		// In both cases, fall back to a direct HTTP DELETE using the tag reference,
		// which avoids the ORAS-injected "delete" scope.
		needsTagDelete := (strings.Contains(errStr, "dangling tag") || strings.Contains(errStr, "still referenced")) ||
			(strings.Contains(errStr, "400") && strings.Contains(errStr, "delete"))
		if needsTagDelete && ref.Version != nil {
			c.logger.V(1).Info("digest delete rejected, retrying with tag reference",
				"tag", tag, "digest", desc.Digest.String(), "reason", errStr)

			if deleteErr := c.deleteByTag(ctx, ref, tag); deleteErr != nil {
				return fmt.Errorf("failed to delete artifact by tag after digest rejection: %w", deleteErr)
			}

			c.logger.V(1).Info("deleted artifact via tag reference",
				"name", ref.Name,
				"tag", tag)

			return nil
		}

		return fmt.Errorf("failed to delete artifact: %w", err)
	}

	c.logger.V(1).Info("deleted artifact",
		"name", ref.Name,
		"version", ref.VersionOrDigest())

	return nil
}

// deleteByTag issues an HTTP DELETE using the tag reference instead of the
// digest. Some registries (GCP Artifact Registry) require this when the
// manifest is still tagged.
func (c *RemoteCatalog) deleteByTag(ctx context.Context, ref Reference, tag string) error {
	// Build the OCI repository name (without registry host).
	parts := []string{}
	if c.repository != "" {
		parts = append(parts, c.repository)
	}
	if ref.Kind != "" {
		parts = append(parts, ref.Kind.Plural())
	}
	parts = append(parts, ref.Name)
	repoName := strings.Join(parts, "/")

	// Try HTTPS first, fall back to HTTP if insecure.
	schemes := []string{"https"}
	if c.insecure {
		schemes = append(schemes, "http")
	}

	var lastErr error
	for _, scheme := range schemes {
		deleteURL := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, c.registry, repoName, tag)

		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create delete request: %w", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		status := resp.StatusCode
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if status == http.StatusOK || status == http.StatusAccepted || status == http.StatusNoContent {
			return nil
		}
		return fmt.Errorf("delete by tag returned status %d", status)
	}

	return fmt.Errorf("delete by tag request failed: %w", lastErr)
}

// Tag creates an alias tag for an existing remote artifact.
// Returns a non-empty string if the alias already existed pointing to a different digest.
func (c *RemoteCatalog) Tag(ctx context.Context, ref Reference, alias string) (string, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return "", err
	}

	// Resolve the source artifact
	tag := c.tagForRef(ref)
	desc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return "", &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Check if alias already exists and points to a different artifact
	var oldVersion string
	if oldDesc, resolveErr := repo.Resolve(ctx, alias); resolveErr == nil {
		if oldDesc.Digest != desc.Digest {
			oldVersion = oldDesc.Digest.String()
		}
	}

	// Tag with alias
	if err := repo.Tag(ctx, desc, alias); err != nil {
		return "", fmt.Errorf("failed to tag artifact: %w", err)
	}

	c.logger.V(1).Info("tagged artifact",
		"name", ref.Name,
		"source", tag,
		"alias", alias)

	return oldVersion, nil
}

// tagForRef returns the OCI tag string for a reference.
// The reference must have a version or digest — scafctl does not use
// arbitrary tags like "latest". Callers must resolve the version before
// calling this method (e.g. via resolveWithKind or listVersions).
func (c *RemoteCatalog) tagForRef(ref Reference) string {
	if ref.HasDigest() {
		return ref.Digest
	}
	if ref.HasVersion() {
		return ref.Version.String()
	}
	// This should never happen — callers must resolve the version first.
	// Panic in debug builds; return a sentinel that will fail OCI resolution
	// rather than silently creating a "latest" tag.
	c.logger.Error(nil, "BUG: tagForRef called without version or digest", "name", ref.Name, "kind", ref.Kind)
	return "__unresolved__"
}

// CopyOptions configures a copy operation between catalogs.
type CopyOptions struct {
	// TargetName overrides the artifact name in the target catalog
	TargetName string

	// Force overwrites existing artifacts
	Force bool

	// OnProgress reports copy progress
	OnProgress func(desc ocispec.Descriptor)
}

// CopyTo copies an artifact from this remote catalog to a local catalog.
func (c *RemoteCatalog) CopyTo(ctx context.Context, ref Reference, target *LocalCatalog, opts CopyOptions) (ArtifactInfo, error) {
	info, err := c.copyToInternal(ctx, ref, target, opts)
	if err != nil && isOCIAuthError(err) {
		c.logger.V(1).Info("copy rejected by registry, retrying anonymously",
			"kind", ref.Kind, "name", ref.Name, "error", err.Error())
		c.switchToAnonymous()
		info, retryErr := c.copyToInternal(ctx, ref, target, opts)
		if retryErr != nil {
			return ArtifactInfo{}, fmt.Errorf("anonymous retry failed (%w) after auth error: %w", retryErr, err)
		}
		return info, nil
	}
	return info, err
}

func (c *RemoteCatalog) copyToInternal(ctx context.Context, ref Reference, target *LocalCatalog, opts CopyOptions) (ArtifactInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	tag := c.tagForRef(ref)

	// Configure copy options
	copyOpts := oras.DefaultCopyOptions
	if opts.OnProgress != nil {
		copyOpts.OnCopySkipped = func(_ context.Context, desc ocispec.Descriptor) error {
			opts.OnProgress(desc)
			return nil
		}
		copyOpts.PostCopy = func(_ context.Context, desc ocispec.Descriptor) error {
			opts.OnProgress(desc)
			return nil
		}
	}

	// Copy from remote to local store
	desc, err := oras.Copy(ctx, repo, tag, target.store, tag, copyOpts)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to copy artifact: %w", err)
	}

	// Determine target reference
	targetRef := ref
	if opts.TargetName != "" {
		targetRef.Name = opts.TargetName
	}

	// Tag in local store with the canonical local tag format (kind/name:version).
	// Attach an origin annotation to the descriptor so the local catalog knows
	// where the artifact was pulled from. This lives only in the OCI index
	// (index.json) and does not modify the manifest blob or its digest.
	targetTag := target.tagForRef(targetRef)
	if desc.Annotations == nil {
		desc.Annotations = make(map[string]string)
	}
	origin := fmt.Sprintf("pulled from %s", c.name)
	if c.registry != "" {
		origin += fmt.Sprintf(" (%s/%s)", c.registry, c.repository)
	}
	desc.Annotations[AnnotationOrigin] = origin
	if err := target.store.Tag(ctx, desc, targetTag); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to tag artifact: %w", err)
	}

	// Remove the raw remote tag that oras.Copy created (e.g. "1.0.0") so the
	// artifact is only reachable via the canonical local tag. Without this the
	// local catalog would require two deletes for the same artifact.
	if tag != targetTag {
		_ = target.store.Untag(ctx, tag)
	}

	c.logger.V(1).Info("copied artifact from remote to local",
		"name", ref.Name,
		"version", ref.Version.String(),
		"digest", desc.Digest.String())

	return ArtifactInfo{
		Reference: targetRef,
		Digest:    desc.Digest.String(),
		Size:      desc.Size,
		Catalog:   LocalCatalogName,
	}, nil
}

// CopyFrom copies an artifact from a local catalog to this remote catalog.
func (c *RemoteCatalog) CopyFrom(ctx context.Context, source *LocalCatalog, ref Reference, opts CopyOptions) (ArtifactInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	// Resolve the artifact in the source catalog. This handles mismatched
	// tags (e.g., bare "1.0.0" vs canonical "solution/email-notifier:1.0.0")
	// by falling back to annotation-based lookup.
	info, err := source.Resolve(ctx, ref)
	if err != nil {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	// Use the resolved digest to locate the artifact in the OCI store,
	// which is tag-format independent.
	srcTag := info.Digest
	_, err = source.store.Resolve(ctx, srcTag)
	if err != nil {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	// Check if target already exists (unless force)
	if !opts.Force {
		exists, _ := c.Exists(ctx, ref)
		if exists {
			return ArtifactInfo{}, &ArtifactExistsError{Reference: ref, Catalog: c.name}
		}
	}

	// Configure copy options
	copyOpts := oras.DefaultCopyOptions
	if opts.OnProgress != nil {
		copyOpts.OnCopySkipped = func(_ context.Context, d ocispec.Descriptor) error {
			opts.OnProgress(d)
			return nil
		}
		copyOpts.PostCopy = func(_ context.Context, d ocispec.Descriptor) error {
			opts.OnProgress(d)
			return nil
		}
	}

	// Determine target tag
	targetRef := ref
	if opts.TargetName != "" {
		targetRef.Name = opts.TargetName
	}
	targetTag := c.tagForRef(targetRef)

	// Copy from local to remote
	desc, err := oras.Copy(ctx, source.store, srcTag, repo, targetTag, copyOpts)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to copy artifact: %w", err)
	}

	c.logger.V(1).Info("copied artifact from local to remote",
		"name", ref.Name,
		"version", ref.Version.String(),
		"digest", desc.Digest.String(),
		"targetCatalog", c.name)

	return ArtifactInfo{
		Reference: targetRef,
		Digest:    desc.Digest.String(),
		Size:      desc.Size,
		Catalog:   c.name,
	}, nil
}

// ReferrerInfo describes an artifact attached to a subject via OCI referrers.
type ReferrerInfo struct {
	// ArtifactType is the media type or artifactType of the referrer.
	ArtifactType string `json:"artifactType" yaml:"artifactType"`

	// Digest is the referrer manifest digest.
	Digest string `json:"digest" yaml:"digest"`

	// Size is the referrer manifest size.
	Size int64 `json:"size" yaml:"size"`

	// Annotations from the referrer manifest.
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

// Attach pushes an artifact that references (is attached to) a subject artifact
// via the OCI referrers mechanism. The subject is identified by ref; the
// attachment is described by artifactType and its raw bytes.
func (c *RemoteCatalog) Attach(ctx context.Context, ref Reference, artifactType string, data []byte, annotations map[string]string) (ocispec.Descriptor, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	// Resolve subject descriptor
	tag := c.tagForRef(ref)
	subjectDesc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return ocispec.Descriptor{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Push the attachment data as a layer
	layerDesc := ocispec.Descriptor{
		MediaType: artifactType,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
	if err := repo.Push(ctx, layerDesc, bytes.NewReader(data)); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to push attachment blob: %w", err)
	}

	// Pack manifest with Subject pointing to the resolved artifact
	manifestDesc, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, artifactType, oras.PackManifestOptions{
		Subject:             &subjectDesc,
		Layers:              []ocispec.Descriptor{layerDesc},
		ManifestAnnotations: annotations,
	})
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to pack attachment manifest: %w", err)
	}

	c.logger.V(1).Info("attached artifact",
		"subject", ref.String(),
		"artifactType", artifactType,
		"digest", manifestDesc.Digest.String())

	return manifestDesc, nil
}

// Referrers lists all artifacts that reference the given subject artifact.
// If artifactType is non-empty, only referrers matching that type are returned.
func (c *RemoteCatalog) Referrers(ctx context.Context, ref Reference, artifactType string) ([]ReferrerInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, err
	}

	// Resolve subject descriptor
	tag := c.tagForRef(ref)
	subjectDesc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	var infos []ReferrerInfo
	err = repo.Referrers(ctx, subjectDesc, artifactType, func(referrers []ocispec.Descriptor) error {
		for _, desc := range referrers {
			infos = append(infos, ReferrerInfo{
				ArtifactType: desc.ArtifactType,
				Digest:       desc.Digest.String(),
				Size:         desc.Size,
				Annotations:  desc.Annotations,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list referrers: %w", err)
	}

	return infos, nil
}

// Ensure RemoteCatalog implements Catalog interface.
var _ Catalog = (*RemoteCatalog)(nil)

// Ensure content.Storage is satisfied for oras.Copy (compile-time check).
var _ content.Storage = (*remote.Repository)(nil)
