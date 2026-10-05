// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
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

// VendorPluginsResult describes the outcome of plugin vendoring.
type VendorPluginsResult struct {
	// ResolvedPlugins contains the lock entries for resolved plugins.
	ResolvedPlugins []LockPlugin
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

// contentDigestCatalog is the minimal catalog surface resolveContentDigests
// needs. Unlike platformDigestCatalog it resolves the content digest straight
// from the (already-fetched) manifest via ResolveContentDigest, so it never
// downloads the binary blob.
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

func resolveCatalog(catalogResolver RegistryAliasResolver, registry string) (string, error) {
	if catalogResolver == nil {
		return "", fmt.Errorf("no catalog resolver configured")
	}
	if catalogAlias, ok := catalogResolver.AliasForRegistry(registry); ok {
		return catalogAlias, nil
	}
	return "", fmt.Errorf("registry %q does not match any configured catalog", registry)
}

// VendorAllPluginsOptions configures VendorAllPlugins.
type VendorAllPluginsOptions struct {
	// CatalogResolver maps a plugin's source registry to a configured catalog
	// alias. A registry it does not recognize is rejected, not fetched.
	CatalogResolver RegistryAliasResolver

	// Remotes maps a lowercased catalog alias to the resolver for that remote
	// catalog (same keying as builder.buildSourcedCatalogs).
	Remotes map[string]ArtifactResolver

	// Local resolves built and short-name plugins from the local catalog.
	Local ArtifactResolver

	// Platform is the build platform (e.g. "linux/amd64"). It selects the
	// primary digest recorded in LockPlugin.Digest.
	Platform string

	// VerifySignature, if set, verifies a resolved plugin's signature at lock
	// time and returns the metadata to record in the lock entry. It receives
	// the plugin's OCI image reference and decides itself whether a failure
	// is fatal (enforce mode) or yields nil (warn mode). Plugins resolved
	// without an image reference are not verified.
	VerifySignature func(ctx context.Context, imageRef string) (*LockPluginSignature, error)
}

// VendorAllPlugins resolves plugin dependencies to lock entries. Plugins are
// not downloaded: only their versions and content digests are pinned, and the
// runtime fetches binaries as needed. A registry
// entry in existingLock whose pinned version still satisfies the dependency's
// constraint is replayed instead of re-resolved. Built and short-name
// dependencies are always re-resolved from the local catalog, since local
// content can be overwritten in place (--force) and a replayed digest could
// be stale.
func VendorAllPlugins(ctx context.Context, plugins []solution.PluginDependency, existingLock *LockFile, opts VendorAllPluginsOptions) (*VendorPluginsResult, error) {
	if opts.Platform == "" {
		return nil, fmt.Errorf("plugin vendoring requires a build platform")
	}
	var result VendorPluginsResult
	for _, p := range plugins {
		switch {
		case p.HasRegistry():
			catalogAlias, err := resolveCatalog(opts.CatalogResolver, p.Registry())
			if err != nil {
				return nil, fmt.Errorf("plugin %q: resolving catalog for registry %q: %w", p.DisplayName(), p.Registry(), err)
			}
			remote, ok := opts.Remotes[strings.ToLower(catalogAlias)]
			if !ok {
				return nil, fmt.Errorf("plugin %q: no catalog available for alias %q (registry %q)", p.DisplayName(), catalogAlias, p.Registry())
			}
			if locked := replayLocked(ctx, existingLock, p); locked != nil {
				result.ResolvedPlugins = append(result.ResolvedPlugins, *locked)
				continue
			}
			resolved, err := resolveDependency(ctx, remote, p, opts.Platform, opts.VerifySignature)
			if err != nil {
				return nil, fmt.Errorf("plugin %q: resolving from catalog %q: %w", p.DisplayName(), catalogAlias, err)
			}
			result.ResolvedPlugins = append(result.ResolvedPlugins, resolved)
		default:
			// Built and short-name plugins both resolve from the local catalog
			// and are never replayed. They differ only in selector and lock
			// Source, which resolveDependency derives from p.
			//
			// TODO: source.built plugins are locked and published here, but the
			// runtime rejects them (plugin.ErrBuiltPluginUnsupported in
			// pkg/plugin/fetcher.go), we will address that in future work. For now, we vendor them so the lock file is complete
			if opts.Local == nil {
				return nil, fmt.Errorf("plugin %q: no local catalog configured", p.DisplayName())
			}
			resolved, err := resolveDependency(ctx, opts.Local, p, opts.Platform, opts.VerifySignature)
			if err != nil {
				return nil, fmt.Errorf("plugin %q: resolving from local catalog: %w", p.DisplayName(), err)
			}
			result.ResolvedPlugins = append(result.ResolvedPlugins, resolved)
		}
	}
	return &result, nil
}

// replayLocked returns a copy of p's lock entry when its pinned version still
// satisfies p's constraint, with Constraint refreshed to the current spec
// value. It returns nil when there is no entry or the entry is stale.
func replayLocked(ctx context.Context, existingLock *LockFile, p solution.PluginDependency) *LockPlugin {
	locked := existingLock.FindPluginByDep(p)
	if locked == nil {
		return nil
	}
	lgr := logger.FromContext(ctx)
	satisfies, err := CheckVersionConstraint(p.Version, locked.Version)
	if err != nil || !satisfies {
		lgr.V(1).Info("lock entry stale, re-resolving",
			"name", p.DisplayName(),
			"kind", p.Kind,
			"constraint", p.Version,
			"lockedVersion", locked.Version)
		return nil
	}
	lgr.V(1).Info("replaying plugin from lock file",
		"name", p.DisplayName(),
		"kind", p.Kind,
		"version", locked.Version,
		"digest", locked.Digest)
	entry := *locked
	entry.Constraint = p.Version
	return &entry
}

// ArtifactResolver is the catalog surface plugin vendoring needs: find a
// plugin by identity, then read its per-platform content digests without
// downloading the binary. *catalog.LocalCatalog and *catalog.RemoteCatalog
// satisfy it.
type ArtifactResolver interface {
	Find(ctx context.Context, query catalog.ArtifactQuery) (catalog.ArtifactInfo, error)
	contentDigestCatalog // ListPlatforms + ResolveContentDigest
}

// selectorFor maps a dependency's declared form to a catalog source selector.
// A short name selects SourceAny: Find returns the single match, or an
// AmbiguousArtifactError when copies from more than one origin match.
func selectorFor(p solution.PluginDependency) catalog.SourceSelector {
	switch {
	case p.HasRegistry():
		return catalog.SourceSelector{Kind: catalog.SourceRemote, Remote: p.Registry()}
	case p.HasBuilt():
		return catalog.SourceSelector{Kind: catalog.SourceBuilt}
	default:
		// TODO(short-name-ambiguity): decide whether ambiguity should be
		// name-level. catalog.checkAmbiguity runs over the matches for the
		// requested version/range, so origins are only compared within that
		// set: an exact version or a range can resolve without error when
		// another origin publishes the name only at versions outside it, and
		// an open/latest query ignores an origin that only has pre-releases.
		// Plan 4.4 wants an error whenever the name exists in more than one
		// origin across all versions; that change belongs in pkg/catalog
		// (Find), not here.
		return catalog.SourceSelector{Kind: catalog.SourceAny}
	}
}

// lockSourceFor returns the lock identity for p's declared form. It mirrors
// LockFile.findPluginByDep: registry -> Source.Registry, built -> Source.Built,
// short name -> nil. It is derived from the declaration, never from the
// resolved artifact, so the entry is found again on the next build.
func lockSourceFor(p solution.PluginDependency) *LockPluginSource {
	switch {
	case p.HasRegistry():
		return &LockPluginSource{Registry: p.Registry()}
	case p.HasBuilt():
		return &LockPluginSource{Built: true}
	default:
		return nil
	}
}

func resolveDependency(ctx context.Context, r ArtifactResolver, p solution.PluginDependency, buildPlatform string, signatureFunc func(ctx context.Context, imageRef string) (*LockPluginSignature, error)) (LockPlugin, error) {
	kind := pluginKindToArtifactKind(p.Kind)
	// NOTE: registry case sensitivity. AliasForRegistry matches the registry
	// case-insensitively, but RemoteCatalog.servesSource compares the selector's
	// Remote to its canonical ID exactly. A registry written in a different case
	// (e.g. "GHCR.io/MyOrg" vs configured "ghcr.io/myorg") therefore picks the
	// right catalog yet fails here as "not found". Users are expected to write
	// the registry as configured; if this confuses people, improve this error
	// (e.g. hint at the configured canonical) rather than normalizing case.
	found, err := r.Find(ctx, catalog.ArtifactQuery{Kind: kind, Name: p.ArtifactName(), Version: p.Version, Source: selectorFor(p)})
	if err != nil {
		return LockPlugin{}, fmt.Errorf("plugin %q: finding %q: %w", p.DisplayName(), p.Version, err)
	}
	if found.Reference.Version == nil {
		return LockPlugin{}, fmt.Errorf("plugin %q: catalog returned no resolved version", p.DisplayName())
	}
	// found.Reference keeps its origin (Built/Remote) -- do not rebuild it.
	digests, primary, err := resolveContentDigests(ctx, r, found.Reference, buildPlatform, catalog.MediaTypeForKind(kind))
	if err != nil {
		return LockPlugin{}, fmt.Errorf("plugin %q: %w", p.DisplayName(), err)
	}
	// A multi-platform artifact that does not publish the build platform has
	// no primary digest; a lock must never record a blank digest.
	if primary == "" {
		return LockPlugin{}, fmt.Errorf("plugin %q: no content digest resolved for build platform %q", p.DisplayName(), buildPlatform)
	}
	// TODO(local-copy-signatures): LocalCatalog never sets ImageRef, so
	// built plugins and short names that resolve to a copy pulled from a
	// registry are never verified at lock time, while a registry
	// declaration of the same plugin is. For a pulled copy (non-empty
	// found.Canonical) we could rebuild the origin image reference from the
	// canonical, name and version and verify against that. Built plugins
	// have no origin to verify against.
	var sig *LockPluginSignature
	if signatureFunc != nil && found.ImageRef != "" {
		s, err := signatureFunc(ctx, found.ImageRef)
		if err != nil {
			return LockPlugin{}, fmt.Errorf("plugin %q: signature verification failed during lock: %w", p.DisplayName(), err)
		}
		sig = s
	}
	return LockPlugin{
		Name: p.ArtifactName(), Kind: string(p.Kind),
		Version: found.Reference.Version.String(), Constraint: p.Version,
		Digest: primary, Digests: digests,
		ResolvedFrom: found.Catalog, ResolvedCanonical: found.Canonical,
		Source:    lockSourceFor(p),
		Signature: sig,
	}, nil
}
