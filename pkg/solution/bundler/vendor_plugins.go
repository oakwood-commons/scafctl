// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"errors"
	"fmt"
	"maps"
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

// pluginDigestResolver is the minimal catalog surface resolveContentDigests
// needs. It resolves the content digest straight from the manifest via
// ResolveContentDigest without downloading the binary blob.
type pluginDigestResolver interface {
	ListPlatforms(ctx context.Context, name string, kind catalog.ArtifactKind, version string, opts ...catalog.Option) ([]string, error)
	ResolveContentDigest(ctx context.Context, name string, kind catalog.ArtifactKind, version, platform, mediaType string, opts ...catalog.Option) (catalog.ContentDigestInfo, error)
}

// PluginResolver is the catalog surface plugin vendoring needs: find a plugin
// across the configured source chain (or scoped to a single catalog via
// catalog.WithCatalog), then read its per-platform content digests without
// downloading the binary. *catalog.PluginFetcher satisfies it.
type PluginResolver interface {
	Find(ctx context.Context, query catalog.ArtifactQuery, opts ...catalog.Option) (catalog.ArtifactInfo, error)
	pluginDigestResolver
}

var _ PluginResolver = (*catalog.PluginFetcher)(nil)

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
// It reads the digest directly from the manifest (via ResolveContentDigest)
// instead of fetching the binary blob, so it does no wasted I/O. A per-platform
// failure or an empty digest is a hard error: a lock must never record a blank
// or partial digest, since runtime verification would then fail closed with a
// misleading message.
func resolveContentDigests(
	ctx context.Context,
	r pluginDigestResolver,
	name string,
	kind catalog.ArtifactKind,
	version, expectedTopDigest, fallbackPlatform, mediaType string,
	opts ...catalog.Option,
) (digests map[string]string, primary string, err error) {
	platforms, err := r.ListPlatforms(ctx, name, kind, version, opts...)
	if err != nil {
		return nil, "", fmt.Errorf("listing platforms: %w", err)
	}
	checkTopDigest := func(plat string, info catalog.ContentDigestInfo) error {
		if expectedTopDigest != "" && info.Digest != "" && info.Digest != expectedTopDigest {
			return fmt.Errorf("resolving digest for platform %s: tag %s moved from %s to %s during resolution", plat, version, expectedTopDigest, info.Digest)
		}
		if info.ContentDigest == "" {
			return fmt.Errorf("resolving digest for platform %s: catalog returned an empty digest", plat)
		}
		return nil
	}
	if len(platforms) == 0 {
		// Single-platform artifact: one binary for every os/arch. Return a nil
		// map (the single-platform marker) and the sole digest as primary.
		info, err := r.ResolveContentDigest(ctx, name, kind, version, fallbackPlatform, mediaType, opts...)
		if err != nil {
			return nil, "", fmt.Errorf("resolving digest for platform %s: %w", fallbackPlatform, err)
		}
		if err := checkTopDigest(fallbackPlatform, info); err != nil {
			return nil, "", err
		}
		return nil, info.ContentDigest, nil
	}

	digests = make(map[string]string, len(platforms))
	for _, plat := range platforms {
		info, err := r.ResolveContentDigest(ctx, name, kind, version, plat, mediaType, opts...)
		if err != nil {
			return nil, "", fmt.Errorf("resolving digest for platform %s: %w", plat, err)
		}
		if err := checkTopDigest(plat, info); err != nil {
			return nil, "", err
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
	// CatalogResolver maps a plugin's canonical OCI origin to a configured
	// catalog alias. An origin it does not recognize is rejected (or invalidates
	// short-name lock replay), not fetched.
	CatalogResolver RegistryAliasResolver

	// PluginFetcher resolves plugin artifacts and per-platform content digests
	// across the configured plugin catalog chain or scoped to a single catalog.
	PluginFetcher PluginResolver

	// Platform is the build platform (e.g. "linux/amd64"). It selects the
	// primary digest recorded in LockPlugin.Digest.
	Platform string

	// VerifySignature, if set, verifies a resolved plugin's signature at lock
	// time and returns the metadata to record in the lock entry. It receives
	// the plugin's OCI image reference (empty when the resolved copy has no
	// registry origin, such as a locally built plugin) and decides itself
	// whether a failure is fatal (enforce mode) or yields nil (warn mode).
	VerifySignature func(ctx context.Context, imageRef string) (*LockPluginSignature, error)
}

// VendorAllPlugins resolves plugin dependencies to lock entries. Plugins are
// not downloaded: only their versions and content digests are pinned, and the
// runtime fetches binaries as needed.
//
// Resolution follows three explicit paths after validating p.ValidateSource():
//   - Registry-sourced (p.HasRegistry()): validates p.Registry() against
//     CatalogResolver, replays a satisfying existingLock entry (reverifying the
//     pinned artifact under opts.VerifySignature when a signature policy is
//     active), and otherwise resolves scoped to that catalog alias.
//   - Built (p.HasBuilt()): never replays from existingLock (local builds can be
//     overwritten in place with --force); resolves scoped to local (SourceBuilt).
//   - Short-name (p.Source == nil): probes local (SourceBuilt) first so a local
//     build shadows both existingLock and remote catalogs (failing closed on
//     non-404 local errors); otherwise replays a satisfying remote lock entry
//     whose ResolvedCanonical is still a configured catalog (reverifying the
//     pinned artifact under opts.VerifySignature when a signature policy is
//     active); otherwise resolves across the PluginFetcher chain and reads
//     platforms/digests scoped to the winning catalog.
func VendorAllPlugins(ctx context.Context, plugins []solution.PluginDependency, existingLock *LockFile, opts VendorAllPluginsOptions) (*VendorPluginsResult, error) {
	if opts.Platform == "" {
		return nil, fmt.Errorf("plugin vendoring requires a build platform")
	}
	var result VendorPluginsResult
	for _, p := range plugins {
		if err := p.ValidateSource(); err != nil {
			return nil, err
		}
		switch {
		case p.HasRegistry():
			catalogAlias, err := resolveCatalog(opts.CatalogResolver, p.Registry())
			if err != nil {
				return nil, fmt.Errorf("plugin %q: resolving catalog for registry %q: %w", p.DisplayName(), p.Registry(), err)
			}
			locked, err := replayLockedRegistry(ctx, existingLock, p, catalogAlias, opts)
			if err != nil {
				return nil, err
			}
			if locked != nil {
				result.ResolvedPlugins = append(result.ResolvedPlugins, *locked)
				continue
			}
			if opts.PluginFetcher == nil {
				return nil, fmt.Errorf("plugin %q: no plugin fetcher configured", p.DisplayName())
			}
			resolved, err := resolveDependency(ctx, opts.PluginFetcher, p, opts.Platform, opts.VerifySignature, catalog.WithCatalog(catalogAlias))
			if err != nil {
				return nil, fmt.Errorf("plugin %q: resolving from catalog %q: %w", p.DisplayName(), catalogAlias, err)
			}
			result.ResolvedPlugins = append(result.ResolvedPlugins, resolved)

		case p.HasBuilt():
			if opts.PluginFetcher == nil {
				return nil, fmt.Errorf("plugin %q: no plugin fetcher configured", p.DisplayName())
			}
			resolved, err := resolveDependency(ctx, opts.PluginFetcher, p, opts.Platform, opts.VerifySignature, catalog.WithCatalog(catalog.LocalCatalogName))
			if err != nil {
				return nil, fmt.Errorf("plugin %q: resolving from local catalog: %w", p.DisplayName(), err)
			}
			result.ResolvedPlugins = append(result.ResolvedPlugins, resolved)

		default:
			if opts.PluginFetcher == nil {
				return nil, fmt.Errorf("plugin %q: no plugin fetcher configured", p.DisplayName())
			}
			resolved, ok, err := probeLocalBuiltShortName(ctx, opts.PluginFetcher, p, opts.Platform, opts.VerifySignature)
			if err != nil {
				return nil, fmt.Errorf("plugin %q: resolving from local catalog: %w", p.DisplayName(), err)
			}
			if ok {
				result.ResolvedPlugins = append(result.ResolvedPlugins, resolved)
				continue
			}
			locked, err := replayLockedShortName(ctx, existingLock, p, opts)
			if err != nil {
				return nil, err
			}
			if locked != nil {
				result.ResolvedPlugins = append(result.ResolvedPlugins, *locked)
				continue
			}
			resolved, err = resolveDependency(ctx, opts.PluginFetcher, p, opts.Platform, opts.VerifySignature)
			if err != nil {
				return nil, fmt.Errorf("plugin %q: resolving from catalog chain: %w", p.DisplayName(), err)
			}
			result.ResolvedPlugins = append(result.ResolvedPlugins, resolved)
		}
	}
	return &result, nil
}

// replayLockedRegistry replays a registry-sourced lock entry when its pinned
// version still satisfies p's constraint. When a signature policy is active,
// the pinned artifact is reverified against that policy before replay.
func replayLockedRegistry(ctx context.Context, existingLock *LockFile, p solution.PluginDependency, catalogAlias string, opts VendorAllPluginsOptions) (*LockPlugin, error) {
	locked := existingLock.FindPluginByDep(p)
	if locked == nil {
		return nil, nil
	}
	return replayCandidate(ctx, locked, p, catalogAlias, opts)
}

// replayLockedShortName replays a short-name lock entry only when it came from
// a remote catalog that is still configured and its version still satisfies p's
// constraint. When a signature policy is active, the pinned artifact is
// reverified against that policy before replay.
func replayLockedShortName(ctx context.Context, existingLock *LockFile, p solution.PluginDependency, opts VendorAllPluginsOptions) (*LockPlugin, error) {
	locked := existingLock.FindPluginByDep(p)
	if locked == nil || locked.IsBuilt() {
		return nil, nil
	}
	lgr := logger.FromContext(ctx)
	if opts.CatalogResolver == nil {
		lgr.V(1).Info("no catalog resolver configured for short-name lock replay, re-resolving",
			"name", p.DisplayName(),
			"kind", p.Kind)
		return nil, nil
	}
	catalogAlias, ok := opts.CatalogResolver.AliasForRegistry(locked.ResolvedCanonical)
	if !ok {
		lgr.V(1).Info("locked plugin catalog no longer configured, re-resolving",
			"name", p.DisplayName(),
			"kind", p.Kind,
			"resolvedCanonical", locked.ResolvedCanonical)
		return nil, nil
	}
	return replayCandidate(ctx, locked, p, catalogAlias, opts)
}

func lockedSatisfiesConstraint(constraint, lockedVersion string) bool {
	satisfies, err := CheckVersionConstraint(constraint, lockedVersion)
	return err == nil && satisfies
}

func replayCandidate(ctx context.Context, locked *LockPlugin, p solution.PluginDependency, catalogAlias string, opts VendorAllPluginsOptions) (*LockPlugin, error) {
	lgr := logger.FromContext(ctx)
	if !lockedSatisfiesConstraint(p.Version, locked.Version) {
		lgr.V(1).Info("lock entry stale, re-resolving",
			"name", p.DisplayName(),
			"kind", p.Kind,
			"constraint", p.Version,
			"lockedVersion", locked.Version)
		return nil, nil
	}
	entry := cloneLockPlugin(*locked)
	entry.Constraint = p.Version
	if opts.VerifySignature != nil {
		sig, err := reverifyLockedPlugin(ctx, opts.PluginFetcher, locked, p, catalogAlias, opts.Platform, opts.VerifySignature)
		if err != nil {
			return nil, err
		}
		entry.Signature = sig
	}
	lgr.V(1).Info("replaying plugin from lock file",
		"name", p.DisplayName(),
		"kind", p.Kind,
		"version", locked.Version,
		"digest", locked.Digest)
	return &entry, nil
}

// reverifyLockedPlugin resolves the pinned version in catalogAlias, verifies
// that its content digest(s) still match locked, and evaluates signatureFunc
// against the pinned artifact's ImageRef so recorded audit metadata never
// substitutes for verification under the active trust policy.
func reverifyLockedPlugin(
	ctx context.Context,
	r PluginResolver,
	locked *LockPlugin,
	p solution.PluginDependency,
	catalogAlias, buildPlatform string,
	signatureFunc func(ctx context.Context, imageRef string) (*LockPluginSignature, error),
) (*LockPluginSignature, error) {
	if r == nil {
		return nil, fmt.Errorf("plugin %q: no plugin fetcher configured", p.DisplayName())
	}
	kind := pluginKindToArtifactKind(p.Kind)
	scope := catalog.WithCatalog(catalogAlias)
	found, err := r.Find(ctx, catalog.ArtifactQuery{
		Kind:    kind,
		Name:    p.ArtifactName(),
		Version: locked.Version,
		Source:  selectorFor(p),
	}, scope)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: finding locked version %q in catalog %q: %w", p.DisplayName(), locked.Version, catalogAlias, err)
	}
	if !samePinnedVersion(found.Reference.Version, locked.Version) {
		got := "<nil>"
		if found.Reference.Version != nil {
			got = found.Reference.Version.String()
		}
		return nil, fmt.Errorf("plugin %q: catalog %q resolved version %q for pinned version %q", p.DisplayName(), catalogAlias, got, locked.Version)
	}
	if locked.ResolvedCanonical != "" && found.Canonical != "" && !strings.EqualFold(locked.ResolvedCanonical, found.Canonical) {
		return nil, fmt.Errorf("plugin %q: catalog %q canonical origin %q does not match locked origin %q", p.DisplayName(), catalogAlias, found.Canonical, locked.ResolvedCanonical)
	}
	artifactName := found.Reference.Name
	if artifactName == "" {
		artifactName = p.ArtifactName()
	}
	digests, primary, err := resolveContentDigests(ctx, r, artifactName, kind, locked.Version, found.Digest, buildPlatform, catalog.MediaTypeForKind(kind), scope)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: verifying locked digests for %q in catalog %q: %w", p.DisplayName(), locked.Version, catalogAlias, err)
	}
	if !lockedDigestsMatch(locked, digests, primary) {
		return nil, fmt.Errorf("plugin %q: locked version %q content digest in catalog %q does not match lock file", p.DisplayName(), locked.Version, catalogAlias)
	}
	sig, err := signatureFunc(ctx, found.ImageRef)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: signature verification failed during lock: %w", p.DisplayName(), err)
	}
	return sig, nil
}

func samePinnedVersion(resolved *semver.Version, lockedVersion string) bool {
	if resolved == nil {
		return false
	}
	if resolved.String() == lockedVersion {
		return true
	}
	lockedParsed, err := semver.NewVersion(lockedVersion)
	return err == nil && resolved.Equal(lockedParsed)
}

func lockedDigestsMatch(locked *LockPlugin, digests map[string]string, primary string) bool {
	if locked == nil || locked.Digest == "" {
		return false
	}
	if len(locked.Digests) == 0 {
		return len(digests) == 0 && primary == locked.Digest
	}
	if len(digests) == 0 || !maps.Equal(locked.Digests, digests) {
		return false
	}
	for _, d := range digests {
		if d == locked.Digest {
			return true
		}
	}
	return false
}

func cloneLockPlugin(lp LockPlugin) LockPlugin {
	out := lp
	if lp.Digests != nil {
		out.Digests = maps.Clone(lp.Digests)
	}
	if lp.Signature != nil {
		sig := *lp.Signature
		out.Signature = &sig
	}
	if lp.Source != nil {
		src := *lp.Source
		out.Source = &src
	}
	return out
}

// probeLocalBuiltShortName probes the local catalog for a locally built copy of
// a short-name plugin dependency. Returns ok=true when found, ok=false when
// absent or when the local catalog is not configured, and a non-nil error on
// any other failure so corrupt local builds fail closed.
func probeLocalBuiltShortName(
	ctx context.Context,
	r PluginResolver,
	p solution.PluginDependency,
	buildPlatform string,
	signatureFunc func(ctx context.Context, imageRef string) (*LockPluginSignature, error),
) (LockPlugin, bool, error) {
	kind := pluginKindToArtifactKind(p.Kind)
	query := catalog.ArtifactQuery{
		Kind:    kind,
		Name:    p.ArtifactName(),
		Version: p.Version,
		Source:  catalog.SourceSelector{Kind: catalog.SourceBuilt},
	}
	found, err := r.Find(ctx, query, catalog.WithCatalog(catalog.LocalCatalogName))
	if err != nil {
		if catalog.IsNotFound(err) || errors.Is(err, catalog.ErrCatalogNotConfigured) {
			return LockPlugin{}, false, nil
		}
		return LockPlugin{}, false, fmt.Errorf("plugin %q: finding %q: %w", p.DisplayName(), p.Version, err)
	}
	resolved, err := finishResolveDependency(ctx, r, p, kind, found, buildPlatform, signatureFunc)
	if err != nil {
		return LockPlugin{}, false, err
	}
	return resolved, true, nil
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

func resolveDependency(
	ctx context.Context,
	r PluginResolver,
	p solution.PluginDependency,
	buildPlatform string,
	signatureFunc func(ctx context.Context, imageRef string) (*LockPluginSignature, error),
	findOpts ...catalog.Option,
) (LockPlugin, error) {
	kind := pluginKindToArtifactKind(p.Kind)
	// NOTE: registry case sensitivity. AliasForRegistry matches the registry
	// case-insensitively, but RemoteCatalog.servesSource compares the selector's
	// Remote to its canonical ID exactly. A registry written in a different case
	// (e.g. "GHCR.io/MyOrg" vs configured "ghcr.io/myorg") therefore picks the
	// right catalog yet fails here as "not found". Users are expected to write
	// the registry as configured; if this confuses people, improve this error
	// (e.g. hint at the configured canonical) rather than normalizing case.
	found, err := r.Find(ctx, catalog.ArtifactQuery{
		Kind:    kind,
		Name:    p.ArtifactName(),
		Version: p.Version,
		Source:  selectorFor(p),
	}, findOpts...)
	if err != nil {
		return LockPlugin{}, fmt.Errorf("plugin %q: finding %q: %w", p.DisplayName(), p.Version, err)
	}
	return finishResolveDependency(ctx, r, p, kind, found, buildPlatform, signatureFunc)
}

func finishResolveDependency(
	ctx context.Context,
	r PluginResolver,
	p solution.PluginDependency,
	kind catalog.ArtifactKind,
	found catalog.ArtifactInfo,
	buildPlatform string,
	signatureFunc func(ctx context.Context, imageRef string) (*LockPluginSignature, error),
) (LockPlugin, error) {
	if found.Reference.Version == nil {
		return LockPlugin{}, fmt.Errorf("plugin %q: catalog returned no resolved version", p.DisplayName())
	}
	if found.Catalog == "" {
		return LockPlugin{}, fmt.Errorf("plugin %q: catalog returned no source catalog name", p.DisplayName())
	}
	version := found.Reference.Version.String()
	artifactName := found.Reference.Name
	if artifactName == "" {
		artifactName = p.ArtifactName()
	}
	// Scope platform enumeration and content-digest lookups to the winning
	// catalog so non-winning catalogs are not re-probed per platform and
	// digests cannot split across catalogs.
	scope := catalog.WithCatalog(found.Catalog)
	digests, primary, err := resolveContentDigests(ctx, r, artifactName, kind, version, found.Digest, buildPlatform, catalog.MediaTypeForKind(kind), scope)
	if err != nil {
		return LockPlugin{}, fmt.Errorf("plugin %q: %w", p.DisplayName(), err)
	}
	// A multi-platform artifact that does not publish the build platform has
	// no primary digest; a lock must never record a blank digest.
	if primary == "" {
		return LockPlugin{}, fmt.Errorf("plugin %q: no content digest resolved for build platform %q", p.DisplayName(), buildPlatform)
	}
	var sig *LockPluginSignature
	if signatureFunc != nil {
		s, err := signatureFunc(ctx, found.ImageRef)
		if err != nil {
			return LockPlugin{}, fmt.Errorf("plugin %q: signature verification failed during lock: %w", p.DisplayName(), err)
		}
		sig = s
	}
	return LockPlugin{
		Name: p.ArtifactName(), Kind: string(p.Kind),
		Version: version, Constraint: p.Version,
		Digest: primary, Digests: digests,
		ResolvedFrom: found.Catalog, ResolvedCanonical: found.Canonical,
		Source:    lockSourceFor(p),
		Signature: sig,
	}, nil
}
