// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// RegistryAliasResolver maps a plugin's canonical OCI origin (the catalog
// reference portion of its Source) to a configured catalog alias. It is the
// single lookup sourced-plugin vendoring needs from the catalog topology;
// *catalogindex.Index satisfies it and is nil-safe. An origin the resolver does
// not recognize names an unconfigured catalog and must be rejected rather than
// fetched.
type RegistryAliasResolver interface {
	AliasForRegistry(origin string) (string, bool)
}

// PluginResolver resolves plugin artifacts from the catalog.
type PluginResolver interface {
	// ResolvePlugin resolves a plugin by name and kind, returning its metadata.
	// If version constraint is non-empty, the resolver should pick the best
	// matching version. Returns the artifact info with the resolved version and digest.
	ResolvePlugin(ctx context.Context, name string, kind catalog.ArtifactKind, versionConstraint string) (catalog.ArtifactInfo, error)
}

// VendorPluginsOptions configures the plugin vendoring process.
type VendorPluginsOptions struct {
	// PluginResolver resolves plugins from the catalog.
	// If nil, plugin vendoring is skipped.
	PluginResolver PluginResolver

	// PlatformCatalog, if set, is used to resolve per-platform content
	// digests for multi-platform (OCI image index) plugins. Without this,
	// the lock file records only the OCI manifest/index digest.
	// catalog.PlatformAwareCatalog satisfies this interface.
	PlatformCatalog platformDigestCatalog

	// Platform is the target platform string (e.g., "darwin/arm64") used with
	// PlatformCatalog to resolve platform-specific content digests.
	Platform string

	// VerifySignature, if set, is called to verify plugin signatures at lock
	// time. It receives the OCI image reference and returns signature metadata
	// to record in the lock file. On error, the function should decide whether
	// to fail (enforce mode) or return nil (warn mode).
	VerifySignature func(ctx context.Context, imageRef string) (*LockPluginSignature, error)
}

// VendorPluginsResult describes the outcome of plugin vendoring.
type VendorPluginsResult struct {
	// ResolvedPlugins contains the lock entries for resolved plugins.
	ResolvedPlugins []LockPlugin
}

// VendorPlugins resolves plugin dependencies against the catalog and records
// them in the lock file for reproducible builds. Unlike solution vendoring,
// plugins are not downloaded during build — only their versions and digests
// are pinned. The runtime fetches plugin binaries as needed.
func VendorPlugins(ctx context.Context, plugins []solution.PluginDependency, existingLock *LockFile, opts VendorPluginsOptions) (*VendorPluginsResult, error) {
	if opts.PluginResolver == nil {
		return &VendorPluginsResult{}, nil
	}

	lgr := logger.FromContext(ctx)
	result := &VendorPluginsResult{
		ResolvedPlugins: make([]LockPlugin, 0, len(plugins)),
	}

	for _, p := range plugins {
		kind := pluginKindToArtifactKind(p.Kind)

		// Check existing lock file for replay
		if existingLock != nil {
			if locked := existingLock.FindPluginByDep(p); locked != nil {
				// Verify the locked version still satisfies the constraint
				satisfies, err := CheckVersionConstraint(p.Version, locked.Version)
				if err == nil && satisfies {
					lgr.V(1).Info("replaying plugin from lock file",
						"name", p.DisplayName(),
						"kind", p.Kind,
						"version", locked.Version,
						"digest", locked.Digest)
					// Refresh the requested constraint to the current spec
					// value (the pinned Version is still replayed); this mirrors
					// how solution vendoring records the current constraint.
					entry := *locked
					entry.Constraint = p.Version
					result.ResolvedPlugins = append(result.ResolvedPlugins, entry)
					continue
				}
				lgr.V(1).Info("lock file plugin entry stale, re-resolving",
					"name", p.DisplayName(),
					"kind", p.Kind,
					"constraint", p.Version,
					"lockedVersion", locked.Version)
			}
		}

		// Resolve from catalog
		info, err := opts.PluginResolver.ResolvePlugin(ctx, p.ArtifactName(), kind, p.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve plugin %s (%s): %w", p.DisplayName(), p.Kind, err)
		}

		resolvedVersion := ""
		if info.Reference.Version != nil {
			resolvedVersion = info.Reference.Version.String()
		}

		// Verify the resolved version satisfies the constraint
		if resolvedVersion != "" {
			satisfies, err := CheckVersionConstraint(p.Version, resolvedVersion)
			if err != nil {
				return nil, fmt.Errorf("failed to check version constraint for plugin %s: %w", p.DisplayName(), err)
			}
			if !satisfies {
				return nil, fmt.Errorf("resolved version %s for plugin %s does not satisfy constraint %s", resolvedVersion, p.DisplayName(), p.Version)
			}
		}

		// Resolve per-platform content digests when a platform-aware catalog
		// is available. This pins every published platform in the lock so
		// runtime verification works regardless of where the solution runs.
		var digests map[string]string
		digest := info.Digest

		if opts.PlatformCatalog != nil && opts.Platform != "" {
			ref, refErr := catalog.ParseReference(kind, fmt.Sprintf("%s@%s", p.ArtifactName(), resolvedVersion))
			if refErr == nil {
				// platDigests is nil for a single-platform plugin (the invariant
				// marker); primary is the build-platform content digest.
				platDigests, primary, pdErr := resolvePlatformDigests(ctx, opts.PlatformCatalog, ref, opts.Platform)
				if pdErr == nil {
					digests = platDigests
					// Primary digest = build platform's content digest. Empty
					// when a multi-platform artifact omits the build platform;
					// keep the manifest digest in that case.
					if primary != "" {
						digest = primary
					}
					lgr.V(1).Info("resolved per-platform digests",
						"name", p.DisplayName(),
						"platforms", len(platDigests))
				} else {
					lgr.V(1).Info("platform digest resolution failed, using manifest digest",
						"name", p.DisplayName(),
						"error", pdErr)
				}
			}
		}

		if digest == "" {
			// Fall back to hashing the version string
			digest = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(resolvedVersion)))
		}

		// Verify signature if a verifier function is provided and imageRef is available.
		var sigMeta *LockPluginSignature
		if opts.VerifySignature != nil && info.ImageRef != "" {
			sig, sigErr := opts.VerifySignature(ctx, info.ImageRef)
			if sigErr != nil {
				return nil, fmt.Errorf("plugin %s: signature verification failed during lock: %w", p.DisplayName(), sigErr)
			}
			sigMeta = sig
		}

		lockEntry := LockPlugin{
			Name:              p.ArtifactName(),
			Kind:              string(p.Kind),
			Version:           resolvedVersion,
			Constraint:        p.Version,
			Digest:            digest,
			Digests:           digests,
			ResolvedFrom:      info.Catalog,
			ResolvedCanonical: info.Canonical,
			Signature:         sigMeta,
		}

		lgr.V(1).Info("resolved plugin",
			"name", p.DisplayName(),
			"kind", p.Kind,
			"version", resolvedVersion,
			"digest", digest,
			"catalog", info.Catalog)

		result.ResolvedPlugins = append(result.ResolvedPlugins, lockEntry)
	}

	return result, nil
}

// SourcedCatalog is the minimal remote-catalog surface VendorPluginsFQN needs
// to pin a sourced plugin: resolve its version and per-platform content digests.
// It is deliberately narrower than catalog.PlatformAwareCatalog so it is trivial
// to fake in unit tests; *catalog.RemoteCatalog satisfies it natively.
type SourcedCatalog interface {
	// Resolve returns the artifact metadata (including the resolved version)
	// for a reference. With no version on the reference this resolves to the
	// catalog's newest published version.
	Resolve(ctx context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error)

	// List returns one ArtifactInfo per published version of an artifact. It
	// backs range-constraint selection: the caller filters the returned
	// versions and picks the highest that satisfies the constraint.
	List(ctx context.Context, kind catalog.ArtifactKind, name string) ([]catalog.ArtifactInfo, error)

	// ListPlatforms returns the platforms an artifact publishes, or an empty
	// list for a single-platform artifact.
	ListPlatforms(ctx context.Context, ref catalog.Reference) ([]string, error)

	// ResolveContentDigest returns the content-layer digest and artifact
	// metadata for a specific platform without downloading the binary blob.
	// The mediaType selects which layer to read the digest from.
	ResolveContentDigest(ctx context.Context, ref catalog.Reference, platform, mediaType string) (catalog.ContentDigestInfo, error)
}

// VendorPluginFQNOptions configures vendoring of sourced (fully-qualified)
// plugin dependencies -- those whose Source names a remote OCI origin.
type VendorPluginFQNOptions struct {
	// SourcedCatalogs holds one remote catalog per configured catalog alias,
	// keyed by the lowercased alias. A sourced plugin's origin is mapped to an
	// alias via CatalogAliasResolver, then to a catalog here. If nil, sourced
	// plugin vendoring is skipped.
	SourcedCatalogs map[string]SourcedCatalog

	// CatalogAliasResolver maps a plugin's canonical OCI origin (the catalog
	// reference portion of its Source) to a configured catalog alias. An origin
	// that names no configured catalog is rejected rather than fetched.
	CatalogAliasResolver RegistryAliasResolver

	// Platform is the build/target platform string (e.g. "darwin/arm64"). It
	// selects the primary digest recorded in LockPlugin.Digest and is the
	// fallback key for single-platform artifacts.
	Platform string

	// VerifySignature, if set, verifies a plugin signature at lock time given
	// its OCI image reference, returning metadata to record. On error the
	// function decides whether to fail (enforce) or return nil (warn).
	VerifySignature func(ctx context.Context, imageRef string) (*LockPluginSignature, error)
}

// VendorPluginsFQN resolves sourced plugin dependencies -- those declaring a
// fully-qualified OCI Source -- against their remote catalogs and records them
// in the lock for reproducible builds. Like VendorPlugins it pins versions and
// per-platform content digests without downloading binaries; the runtime fetches
// them on demand.
//
// A sourced plugin's lock identity is (canonical origin, leaf name, kind), where
// the canonical origin is PluginDependency.Source.Registry and the leaf name is
// PluginDependency.ArtifactName. The solution-local alias
// (PluginDependency.Name) is resolved to this identity here and is never stored
// in the lock, so two aliases pointing at the same artifact dedupe to one
// identity.
func VendorPluginsFQN(ctx context.Context, plugins []solution.PluginDependency, existingLock *LockFile, opts VendorPluginFQNOptions) (*VendorPluginsResult, error) {
	if opts.SourcedCatalogs == nil {
		return &VendorPluginsResult{}, nil
	}
	if opts.Platform == "" {
		return nil, fmt.Errorf("sourced plugin vendoring requires a target platform")
	}

	lgr := logger.FromContext(ctx)
	result := &VendorPluginsResult{
		ResolvedPlugins: make([]LockPlugin, 0, len(plugins)),
	}

	for _, p := range plugins {
		if !p.HasRegistry() {
			return nil, fmt.Errorf("plugin %q: sourced vendoring requires a source (registry with optional artifact name)", p.ArtifactName())
		}
		canonical := p.Registry()
		leaf := p.ArtifactName()
		if canonical == "" || leaf == "" {
			return nil, fmt.Errorf("plugin %q: sourced vendoring requires a source registry and artifact name", p.DisplayName())
		}
		kind := pluginKindToArtifactKind(p.Kind)

		// Map the plugin's canonical origin to a configured catalog. An origin
		// that names no configured catalog is rejected, not fetched.
		alias, ok := opts.CatalogAliasResolver.AliasForRegistry(canonical)
		if !ok {
			return nil, fmt.Errorf("plugin %q: source origin %q does not match any configured catalog", p.DisplayName(), canonical)
		}
		cat, ok := opts.SourcedCatalogs[strings.ToLower(alias)]
		if !ok {
			return nil, fmt.Errorf("plugin %q: no catalog available for alias %q (origin %q)", p.DisplayName(), alias, canonical)
		}

		// Replay from the lock when the pinned version still satisfies the
		// current constraint. Identity is (canonical, leaf, kind).
		if existingLock != nil {
			if locked := existingLock.FindPluginByDep(p); locked != nil {
				satisfies, err := CheckVersionConstraint(p.Version, locked.Version)
				if err == nil && satisfies {
					lgr.V(1).Info("replaying sourced plugin from lock file",
						"name", p.ArtifactName(),
						"leaf", leaf,
						"canonical", canonical,
						"kind", p.Kind,
						"version", locked.Version,
						"digest", locked.Digest)
					entry := *locked
					entry.Constraint = p.Version
					result.ResolvedPlugins = append(result.ResolvedPlugins, entry)
					continue
				}
				lgr.V(1).Info("sourced lock entry stale, re-resolving",
					"name", p.ArtifactName(),
					"leaf", leaf,
					"canonical", canonical,
					"kind", p.Kind,
					"constraint", p.Version,
					"lockedVersion", locked.Version)
			}
		}

		// Select the exact version to pin from the declared constraint.
		ref := catalog.Reference{Kind: kind, Name: leaf}
		v, err := ResolveVersionConstraint(ctx, cat, kind, leaf, p.Version)
		if err != nil {
			return nil, fmt.Errorf("plugin %q: %w", p.DisplayName(), err)
		}
		ref.Version = v

		info, err := cat.Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("plugin %q: resolving from %q: %w", p.DisplayName(), canonical, err)
		}
		if info.Reference.Version == nil {
			return nil, fmt.Errorf("plugin %q: catalog %q returned no resolved version", p.DisplayName(), canonical)
		}
		resolvedVersion := info.Reference.Version.String()

		// Post-condition guard: the selected version must satisfy the declared
		// constraint. With the selection above this should always hold; it
		// remains as a defensive check against a misbehaving catalog. Skipped
		// for an empty/"latest" constraint, which imposes no bound to check.
		trimmedVer := strings.TrimSpace(p.Version)
		if trimmedVer != "" && !strings.EqualFold(trimmedVer, "latest") {
			satisfies, err := CheckVersionConstraint(p.Version, resolvedVersion)
			if err != nil {
				return nil, fmt.Errorf("plugin %q: checking version constraint: %w", p.DisplayName(), err)
			}
			if !satisfies {
				return nil, fmt.Errorf("plugin %q: resolved version %s from %q does not satisfy constraint %s", p.DisplayName(), resolvedVersion, canonical, p.Version)
			}
		}

		// Pin per-platform content digests against the versioned reference so
		// they match the resolved version rather than a moving tag. digests is
		// nil for a single-platform plugin (the invariant marker); primary is
		// the build-platform digest recorded as the entry's primary Digest.
		versionedRef := catalog.Reference{Kind: kind, Name: leaf, Version: info.Reference.Version}
		mediaType := catalog.MediaTypeForKind(kind)
		digests, digest, err := resolveContentDigests(ctx, cat, versionedRef, opts.Platform, mediaType)
		if err != nil {
			return nil, fmt.Errorf("plugin %q: resolving content digests: %w", p.DisplayName(), err)
		}
		if digest == "" {
			return nil, fmt.Errorf("plugin %q: no content digest resolved for build platform %q", p.DisplayName(), opts.Platform)
		}

		// Verify signature when configured and an image reference is available.
		var sigMeta *LockPluginSignature
		if opts.VerifySignature != nil && info.ImageRef != "" {
			sig, sigErr := opts.VerifySignature(ctx, info.ImageRef)
			if sigErr != nil {
				return nil, fmt.Errorf("plugin %q: signature verification failed during lock: %w", p.DisplayName(), sigErr)
			}
			sigMeta = sig
		}

		lockEntry := LockPlugin{
			Name:              leaf,
			Kind:              string(p.Kind),
			Version:           resolvedVersion,
			Constraint:        p.Version,
			Digest:            digest,
			Digests:           digests,
			ResolvedFrom:      alias,
			ResolvedCanonical: canonical,
			Signature:         sigMeta,
			Source: &LockPluginSource{
				Registry: canonical,
			},
		}

		lgr.V(1).Info("resolved sourced plugin",
			"name", p.DisplayName(),
			"leaf", leaf,
			"canonical", canonical,
			"kind", p.Kind,
			"version", resolvedVersion,
			"digest", digest,
			"alias", alias)

		result.ResolvedPlugins = append(result.ResolvedPlugins, lockEntry)
	}

	return result, nil
}

// isExactVersion reports whether s parses as a single concrete semver version
// (e.g. "1.2.3", "2.0.0-rc1") rather than a range constraint (">=1.2", "~1.4",
// "^1"). semver.NewConstraint accepts a bare version too, so a successful
// NewVersion parse is what distinguishes "pin this tag" from "select the
// highest match within a range".
func isExactVersion(s string) bool {
	_, err := semver.NewVersion(s)
	return err == nil
}

// VersionLister lists all published versions of an artifact. catalog.Catalog
// satisfies this interface via its List method.
type VersionLister interface {
	List(ctx context.Context, kind catalog.ArtifactKind, name string) ([]catalog.ArtifactInfo, error)
}

// ResolveVersionConstraint picks the concrete semver version to use for a given
// version string. It handles three cases:
//   - "" / "latest": returns nil (caller should leave Reference.Version nil so
//     the catalog resolves to the newest published version).
//   - exact version ("1.2.3"): returns the parsed *semver.Version directly.
//   - range constraint (">=1.0 <2", "~1.4", "^1"): lists all published
//     versions via the lister, filters by the constraint, and returns the
//     highest satisfying version.
//
// The returned *semver.Version can be set directly on a catalog.Reference
// before calling Resolve.
func ResolveVersionConstraint(ctx context.Context, lister VersionLister, kind catalog.ArtifactKind, name, versionStr string) (*semver.Version, error) {
	verStr := strings.TrimSpace(versionStr)
	switch {
	case verStr == "" || strings.EqualFold(verStr, "latest"):
		return nil, nil
	case isExactVersion(verStr):
		v, err := semver.NewVersion(verStr)
		if err != nil {
			return nil, fmt.Errorf("invalid version %q: %w", verStr, err)
		}
		return v, nil
	default:
		infos, err := lister.List(ctx, kind, name)
		if err != nil {
			return nil, fmt.Errorf("listing versions for %s: %w", name, err)
		}
		matches, err := catalog.FilterByVersionConstraint(infos, verStr)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no published version of %s satisfies constraint %q", name, verStr)
		}
		return matches[0].Reference.Version, nil
	}
}

// pluginKindToArtifactKind converts a solution.PluginKind to a catalog.ArtifactKind.
func pluginKindToArtifactKind(kind solution.PluginKind) catalog.ArtifactKind {
	switch kind {
	case solution.PluginKindProvider:
		return catalog.ArtifactKindProvider
	case solution.PluginKindAuthHandler:
		return catalog.ArtifactKindAuthHandler
	default:
		return catalog.ArtifactKind(string(kind))
	}
}

// platformDigestCatalog is the minimal catalog surface resolvePlatformDigests
// needs. Defining it here rather than depending on the full
// catalog.PlatformAwareCatalog (which embeds catalog.Catalog and its many
// methods) keeps the helper trivial to fake in unit tests.
// catalog.PlatformAwareCatalog satisfies this interface.
type platformDigestCatalog interface {
	// ListPlatforms returns the platforms an artifact publishes, or an empty
	// list for a single-platform artifact.
	ListPlatforms(ctx context.Context, ref catalog.Reference) ([]string, error)

	// FetchByPlatform returns the binary and metadata (including the
	// content-layer Digest) for a specific platform.
	FetchByPlatform(ctx context.Context, ref catalog.Reference, platform string) ([]byte, catalog.ArtifactInfo, error)
}

// resolvePlatformDigests resolves content-layer digests for an artifact and
// returns them alongside the primary digest for fallbackPlatform.
//
// It upholds the LockPlugin.Digests invariant that the map is populated ONLY
// for genuine multi-platform (image index) artifacts:
//   - Single-platform (ListPlatforms returns empty): returns a nil map and the
//     sole content digest as primary. The nil map is what marks the lock entry
//     single-platform, so the fetcher verifies it against the primary Digest on
//     any os/arch.
//   - Multi-platform: returns a map keyed by "os/arch" (e.g. "linux/amd64")
//     with one entry per published platform, and primary = digests[fallbackPlatform]
//     ("" when the artifact does not publish the build platform, which the
//     caller decides how to treat).
//
// It performs no lock mutation or other side effects, so it can be unit tested
// in isolation against a fake catalog. A per-platform fetch failure or an empty
// digest is treated as a hard error: a lock must never record a blank or
// partial digest, since runtime verification would then fail closed with a
// misleading message.
func resolvePlatformDigests(ctx context.Context, cat platformDigestCatalog, ref catalog.Reference, fallbackPlatform string) (digests map[string]string, primary string, err error) {
	// NOTE: This is the local-catalog path (VendorPlugins wires PlatformCatalog to
	// the on-disk LocalCatalog), so FetchByPlatform is a local blob read, not a
	// network download. Reading the blob also re-verifies its digest at pin time,
	// which a manifest-only ResolveContentDigest would skip. The remote path
	// (VendorPluginsFQN) uses resolveContentDigests to avoid a real download; do
	// not "unify" these without preserving that local verification.
	platforms, err := cat.ListPlatforms(ctx, ref)
	if err != nil {
		return nil, "", fmt.Errorf("listing platforms: %w", err)
	}
	if len(platforms) == 0 {
		// Single-platform artifact: one binary for every os/arch. Return a nil
		// map (the single-platform marker) and the sole digest as primary.
		_, info, err := cat.FetchByPlatform(ctx, ref, fallbackPlatform)
		if err != nil {
			return nil, "", fmt.Errorf("resolving digest for platform %s: %w", fallbackPlatform, err)
		}
		if info.Digest == "" {
			return nil, "", fmt.Errorf("resolving digest for platform %s: catalog returned an empty digest", fallbackPlatform)
		}
		return nil, info.Digest, nil
	}

	digests = make(map[string]string, len(platforms))
	for _, plat := range platforms {
		_, info, err := cat.FetchByPlatform(ctx, ref, plat)
		if err != nil {
			return nil, "", fmt.Errorf("resolving digest for platform %s: %w", plat, err)
		}
		if info.Digest == "" {
			return nil, "", fmt.Errorf("resolving digest for platform %s: catalog returned an empty digest", plat)
		}
		digests[plat] = info.Digest
	}
	return digests, digests[fallbackPlatform], nil
}

// contentDigestCatalog is the minimal catalog surface resolveContentDigests
// needs. Unlike platformDigestCatalog it resolves the content digest straight
// from the (already-fetched) manifest via ResolveContentDigest, so it never
// downloads the binary blob. SourcedCatalog satisfies this interface.
type contentDigestCatalog interface {
	// ListPlatforms returns the platforms an artifact publishes, or an empty
	// list for a single-platform artifact.
	ListPlatforms(ctx context.Context, ref catalog.Reference) ([]string, error)

	// ResolveContentDigest returns the content-layer digest and artifact
	// metadata for a specific platform without downloading the binary blob.
	// The mediaType selects which layer to read the digest from.
	ResolveContentDigest(ctx context.Context, ref catalog.Reference, platform, mediaType string) (catalog.ContentDigestInfo, error)
}

// resolveContentDigests resolves content-layer digests for an artifact and
// returns them alongside the primary digest for fallbackPlatform.
//
// It upholds the LockPlugin.Digests invariant that the map is populated ONLY
// for genuine multi-platform (image index) artifacts:
//   - Single-platform (ListPlatforms returns empty): returns a nil map and the
//     sole content digest as primary. The nil map is what marks the lock entry
//     single-platform, so the fetcher verifies it against the primary Digest on
//     any os/arch.
//   - Multi-platform: returns a map keyed by "os/arch" (e.g. "linux/amd64")
//     with one entry per published platform, and primary = digests[fallbackPlatform]
//     ("" when the artifact does not publish the build platform, which the
//     caller decides how to treat).
//
// It mirrors resolvePlatformDigests but reads the digest directly from the
// manifest (via ResolveContentDigest) instead of fetching the binary blob, so
// it does no wasted I/O. A per-platform failure or an empty digest is a hard
// error: a lock must never record a blank or partial digest, since runtime
// verification would then fail closed with a misleading message.
func resolveContentDigests(ctx context.Context, cat contentDigestCatalog, ref catalog.Reference, fallbackPlatform, mediaType string) (digests map[string]string, primary string, err error) {
	platforms, err := cat.ListPlatforms(ctx, ref)
	if err != nil {
		return nil, "", fmt.Errorf("listing platforms: %w", err)
	}
	if len(platforms) == 0 {
		// Single-platform artifact: one binary for every os/arch. Return a nil
		// map (the single-platform marker) and the sole digest as primary.
		info, err := cat.ResolveContentDigest(ctx, ref, fallbackPlatform, mediaType)
		if err != nil {
			return nil, "", fmt.Errorf("resolving digest for platform %s: %w", fallbackPlatform, err)
		}
		if info.ContentDigest == "" {
			return nil, "", fmt.Errorf("resolving digest for platform %s: catalog returned an empty digest", fallbackPlatform)
		}
		return nil, info.ContentDigest, nil
	}

	digests = make(map[string]string, len(platforms))
	for _, plat := range platforms {
		info, err := cat.ResolveContentDigest(ctx, ref, plat, mediaType)
		if err != nil {
			return nil, "", fmt.Errorf("resolving digest for platform %s: %w", plat, err)
		}
		if info.ContentDigest == "" {
			return nil, "", fmt.Errorf("resolving digest for platform %s: catalog returned an empty digest", plat)
		}
		digests[plat] = info.ContentDigest
	}
	return digests, digests[fallbackPlatform], nil
}
