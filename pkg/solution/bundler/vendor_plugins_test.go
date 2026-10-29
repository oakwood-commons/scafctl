// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSourcedCatalog is a minimal content-digest catalog for unit-testing
// resolveContentDigests and VendorAllPlugins without a real OCI registry.
type fakeSourcedCatalog struct {
	platforms    []string
	platformsErr error
	digestByPlat map[string]string
	digestErr    map[string]error
}

func (f *fakeSourcedCatalog) ListPlatforms(_ context.Context, _ catalog.Reference) ([]string, error) {
	return f.platforms, f.platformsErr
}

func (f *fakeSourcedCatalog) ResolveContentDigest(_ context.Context, _ catalog.Reference, platform, _ string) (catalog.ContentDigestInfo, error) {
	if err := f.digestErr[platform]; err != nil {
		return catalog.ContentDigestInfo{}, err
	}
	return catalog.ContentDigestInfo{ContentDigest: f.digestByPlat[platform]}, nil
}

// newTestAliasResolver builds a catalog index mapping a single catalog alias to
// a registry origin (via its OCI URL).
func newTestAliasResolver(alias, url string) *catalogindex.Index { //nolint:unparam // test helper uses constant alias for clarity
	return catalogindex.FromConfig(&config.Config{
		Catalogs: []config.CatalogConfig{{Name: alias, URL: url}},
	})
}

func TestResolveContentDigests_MultiPlatform(t *testing.T) {
	cat := &fakeSourcedCatalog{
		platforms: []string{"linux/amd64", "darwin/arm64"},
		digestByPlat: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
		},
	}

	digests, primary, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"linux/amd64":  "sha256:aaa",
		"darwin/arm64": "sha256:bbb",
	}, digests)
	assert.Equal(t, "sha256:aaa", primary, "primary is the build-platform digest")
}

func TestResolveContentDigests_SinglePlatformNilMap(t *testing.T) {
	// ListPlatforms returns empty (single-platform artifact): the map is nil
	// (the invariant marker) and the sole digest is returned as primary.
	cat := &fakeSourcedCatalog{
		platforms:    nil,
		digestByPlat: map[string]string{"linux/amd64": "sha256:only"},
	}

	digests, primary, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.NoError(t, err)
	assert.Nil(t, digests, "single-platform must leave Digests nil")
	assert.Equal(t, "sha256:only", primary)
}

func TestResolveContentDigests_ListPlatformsError(t *testing.T) {
	cat := &fakeSourcedCatalog{platformsErr: errors.New("boom")}

	_, _, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing platforms")
}

func TestResolveContentDigests_ResolveError(t *testing.T) {
	cat := &fakeSourcedCatalog{
		platforms:    []string{"linux/amd64", "darwin/arm64"},
		digestByPlat: map[string]string{"linux/amd64": "sha256:aaa"},
		digestErr:    map[string]error{"darwin/arm64": errors.New("network")},
	}

	_, _, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "darwin/arm64")
}

func TestResolveContentDigests_EmptyDigest(t *testing.T) {
	cat := &fakeSourcedCatalog{
		platforms:    []string{"linux/amd64"},
		digestByPlat: map[string]string{"linux/amd64": ""},
	}

	_, _, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty digest")
}

// fakeArtifactResolver is a minimal ArtifactResolver for VendorAllPlugins
// tests. Find returns findInfo; content digests come from the embedded
// fakeSourcedCatalog.
type fakeArtifactResolver struct {
	fakeSourcedCatalog
	findInfo  catalog.ArtifactInfo
	findErr   error
	findCalls int
	findQuery catalog.ArtifactQuery
}

func (f *fakeArtifactResolver) Find(_ context.Context, q catalog.ArtifactQuery) (catalog.ArtifactInfo, error) {
	f.findCalls++
	f.findQuery = q
	if f.findErr != nil {
		return catalog.ArtifactInfo{}, f.findErr
	}
	return f.findInfo, nil
}

// newFakeRemote returns a resolver for ghcr.io/myorg that resolves exec to
// 1.2.3, published for linux/amd64 and darwin/arm64.
func newFakeRemote() *fakeArtifactResolver {
	return &fakeArtifactResolver{
		fakeSourcedCatalog: fakeSourcedCatalog{
			platforms: []string{"linux/amd64", "darwin/arm64"},
			digestByPlat: map[string]string{
				"linux/amd64":  "sha256:aaa",
				"darwin/arm64": "sha256:bbb",
			},
		},
		findInfo: catalog.ArtifactInfo{
			Reference: catalog.Reference{Kind: catalog.ArtifactKindProvider, Name: "exec", Version: semver.MustParse("1.2.3")},
			Catalog:   "myorg",
			Canonical: "ghcr.io/myorg",
		},
	}
}

// registryExecDep is an exec provider dependency sourced from ghcr.io/myorg.
func registryExecDep(version string) solution.PluginDependency {
	return solution.PluginDependency{
		Name:    "exec",
		Kind:    solution.PluginKindProvider,
		Version: version,
		Source:  &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"},
	}
}

// lockedExec is a lock entry for the exec provider pinned at version.
func lockedExec(version string, source *LockPluginSource) LockPlugin {
	return LockPlugin{
		Name:              "exec",
		Kind:              "provider",
		Version:           version,
		Constraint:        "^1.0.0",
		Digest:            "sha256:locked",
		ResolvedFrom:      "myorg",
		ResolvedCanonical: "ghcr.io/myorg",
		Source:            source,
	}
}

func TestVendorAllPlugins_RegistryResolves(t *testing.T) {
	remote := newFakeRemote()

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, nil, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Remotes:         map[string]ArtifactResolver{"myorg": remote},
		Platform:        "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, catalog.ArtifactQuery{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "exec",
		Version: "^1.0.0",
		Source:  catalog.SourceSelector{Kind: catalog.SourceRemote, Remote: "ghcr.io/myorg"},
	}, remote.findQuery)

	assert.Equal(t, LockPlugin{
		Name:       "exec",
		Kind:       "provider",
		Version:    "1.2.3",
		Constraint: "^1.0.0",
		Digest:     "sha256:aaa",
		Digests: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
		},
		ResolvedFrom:      "myorg",
		ResolvedCanonical: "ghcr.io/myorg",
		Source:            &LockPluginSource{Registry: "ghcr.io/myorg"},
	}, result.ResolvedPlugins[0])
}

func TestVendorAllPlugins_VerifySignature(t *testing.T) {
	const imageRef = "ghcr.io/myorg/providers/exec:1.2.3"
	sigErr := errors.New("untrusted signer")

	tests := []struct {
		name     string
		imageRef string
		verify   func(context.Context, string) (*LockPluginSignature, error)
		wantSig  *LockPluginSignature
		wantErr  error
		wantRefs []string
	}{
		{
			name: "records signature", imageRef: imageRef,
			verify: func(context.Context, string) (*LockPluginSignature, error) {
				return &LockPluginSignature{Issuer: "https://issuer.example", Identity: "release"}, nil
			},
			wantSig:  &LockPluginSignature{Issuer: "https://issuer.example", Identity: "release"},
			wantRefs: []string{imageRef},
		},
		{
			name: "verification failure is fatal", imageRef: imageRef,
			verify:   func(context.Context, string) (*LockPluginSignature, error) { return nil, sigErr },
			wantErr:  sigErr,
			wantRefs: []string{imageRef},
		},
		{
			name:   "no image reference skips verification",
			verify: func(context.Context, string) (*LockPluginSignature, error) { return nil, sigErr },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote := newFakeRemote()
			remote.findInfo.ImageRef = tt.imageRef
			var gotRefs []string
			verify := func(ctx context.Context, ref string) (*LockPluginSignature, error) {
				gotRefs = append(gotRefs, ref)
				return tt.verify(ctx, ref)
			}

			result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, nil, VendorAllPluginsOptions{
				CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
				Remotes:         map[string]ArtifactResolver{"myorg": remote},
				Platform:        "linux/amd64",
				VerifySignature: verify,
			})

			assert.Equal(t, tt.wantRefs, gotRefs)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, result.ResolvedPlugins, 1)
			assert.Equal(t, tt.wantSig, result.ResolvedPlugins[0].Signature)
		})
	}
}

func TestVendorAllPlugins_RegistryMixedCaseAlias(t *testing.T) {
	// AliasForRegistry returns the alias as configured ("MyOrg"); Remotes is
	// keyed by the lowercased alias.
	remote := newFakeRemote()

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, nil, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("MyOrg", "oci://ghcr.io/myorg"),
		Remotes:         map[string]ArtifactResolver{"myorg": remote},
		Platform:        "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)
	assert.Equal(t, 1, remote.findCalls)
}

func TestVendorAllPlugins_RegistryErrors(t *testing.T) {
	tests := []struct {
		name         string
		resolver     RegistryAliasResolver
		remoteKey    string
		findErr      error
		existingLock *LockFile
		wantErr      string
		wantFind     int
	}{
		{
			name:     "nil catalog resolver",
			resolver: nil,
			wantErr:  "no catalog resolver configured",
		},
		{
			name:     "registry not configured",
			resolver: newTestAliasResolver("myorg", "oci://ghcr.io/other"),
			wantErr:  "does not match any configured catalog",
		},
		{
			name:      "no remote for alias",
			resolver:  newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			remoteKey: "different",
			wantErr:   "no catalog available for alias",
		},
		{
			name:     "find error propagates",
			resolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			findErr:  errors.New("registry unreachable"),
			wantErr:  "registry unreachable",
			wantFind: 1,
		},
		{
			// The configured-catalog check runs before replay, so a lock
			// cannot be used to pin a plugin from an unconfigured registry.
			name:     "registry not configured even when locked",
			resolver: newTestAliasResolver("myorg", "oci://ghcr.io/other"),
			existingLock: &LockFile{Plugins: []LockPlugin{
				lockedExec("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}),
			}},
			wantErr: "does not match any configured catalog",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote := newFakeRemote()
			remote.findErr = tt.findErr
			key := tt.remoteKey
			if key == "" {
				key = "myorg"
			}

			_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, tt.existingLock, VendorAllPluginsOptions{
				CatalogResolver: tt.resolver,
				Remotes:         map[string]ArtifactResolver{key: remote},
				Platform:        "linux/amd64",
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Equal(t, tt.wantFind, remote.findCalls)
		})
	}
}

func TestVendorAllPlugins_RegistryLockReplay(t *testing.T) {
	tests := []struct {
		name        string
		lock        *LockFile
		constraint  string
		wantVersion string
		wantDigest  string
		wantFind    int
	}{
		{
			name: "pinned version satisfies constraint is replayed",
			lock: &LockFile{Plugins: []LockPlugin{
				lockedExec("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}),
			}},
			constraint:  "^1.0",
			wantVersion: "1.1.0",
			wantDigest:  "sha256:locked",
			wantFind:    0,
		},
		{
			name: "stale pinned version is re-resolved",
			lock: &LockFile{Plugins: []LockPlugin{
				lockedExec("0.9.0", &LockPluginSource{Registry: "ghcr.io/myorg"}),
			}},
			constraint:  "^1.0.0",
			wantVersion: "1.2.3",
			wantDigest:  "sha256:aaa",
			wantFind:    1,
		},
		{
			name: "short-name entry with same name is not replayed",
			lock: &LockFile{Plugins: []LockPlugin{
				lockedExec("1.1.0", nil),
			}},
			constraint:  "^1.0.0",
			wantVersion: "1.2.3",
			wantDigest:  "sha256:aaa",
			wantFind:    1,
		},
		{
			name: "entry from a different registry is not replayed",
			lock: &LockFile{Plugins: []LockPlugin{
				lockedExec("1.1.0", &LockPluginSource{Registry: "ghcr.io/other"}),
			}},
			constraint:  "^1.0.0",
			wantVersion: "1.2.3",
			wantDigest:  "sha256:aaa",
			wantFind:    1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote := newFakeRemote()

			result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep(tt.constraint)}, tt.lock, VendorAllPluginsOptions{
				CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
				Remotes:         map[string]ArtifactResolver{"myorg": remote},
				Platform:        "linux/amd64",
			})
			require.NoError(t, err)
			require.Len(t, result.ResolvedPlugins, 1)

			lp := result.ResolvedPlugins[0]
			assert.Equal(t, tt.wantFind, remote.findCalls)
			assert.Equal(t, tt.wantVersion, lp.Version)
			assert.Equal(t, tt.wantDigest, lp.Digest)
			assert.Equal(t, tt.constraint, lp.Constraint, "constraint is refreshed to the current spec value")
			assert.Equal(t, &LockPluginSource{Registry: "ghcr.io/myorg"}, lp.Source)
		})
	}
}

func TestVendorAllPlugins_RegistryLockRoundTrip(t *testing.T) {
	// A lock written by one build must be replayed by the next.
	opts := func(r ArtifactResolver) VendorAllPluginsOptions {
		return VendorAllPluginsOptions{
			CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			Remotes:         map[string]ArtifactResolver{"myorg": r},
			Platform:        "linux/amd64",
		}
	}
	deps := []solution.PluginDependency{registryExecDep("^1.0.0")}

	first, err := VendorAllPlugins(testContext(), deps, nil, opts(newFakeRemote()))
	require.NoError(t, err)

	second := newFakeRemote()
	replayed, err := VendorAllPlugins(testContext(), deps, &LockFile{Plugins: first.ResolvedPlugins}, opts(second))
	require.NoError(t, err)

	assert.Equal(t, 0, second.findCalls, "second build must replay from the lock")
	assert.Equal(t, first.ResolvedPlugins, replayed.ResolvedPlugins)
}

// newFakeLocal returns a local-catalog resolver that resolves exec to 1.2.3.
// Local artifacts have no portable canonical identity.
func newFakeLocal() *fakeArtifactResolver {
	f := newFakeRemote()
	f.findInfo.Catalog = "local"
	f.findInfo.Canonical = ""
	return f
}

// builtExecDep is an exec provider dependency sourced from a local build.
func builtExecDep() solution.PluginDependency {
	return solution.PluginDependency{
		Name:    "exec",
		Kind:    solution.PluginKindProvider,
		Version: "^1.0.0",
		Source:  &solution.PluginSource{Built: true},
	}
}

func TestVendorAllPlugins_BuiltResolves(t *testing.T) {
	local := newFakeLocal()

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{builtExecDep()}, nil, VendorAllPluginsOptions{
		Local:    local,
		Platform: "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, catalog.ArtifactQuery{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "exec",
		Version: "^1.0.0",
		Source:  catalog.SourceSelector{Kind: catalog.SourceBuilt},
	}, local.findQuery)

	assert.Equal(t, LockPlugin{
		Name:       "exec",
		Kind:       "provider",
		Version:    "1.2.3",
		Constraint: "^1.0.0",
		Digest:     "sha256:aaa",
		Digests: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
		},
		ResolvedFrom: "local",
		Source:       &LockPluginSource{Built: true},
	}, result.ResolvedPlugins[0])
}

func TestVendorAllPlugins_BuiltErrors(t *testing.T) {
	tests := []struct {
		name     string
		local    func() *fakeArtifactResolver
		wantErr  string
		wantFind int
	}{
		{
			name:    "no local catalog configured",
			local:   func() *fakeArtifactResolver { return nil },
			wantErr: "no local catalog configured",
		},
		{
			name: "find error propagates",
			local: func() *fakeArtifactResolver {
				f := newFakeLocal()
				f.findErr = errors.New("artifact not found")
				return f
			},
			wantErr:  "artifact not found",
			wantFind: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local := tt.local()
			opts := VendorAllPluginsOptions{Platform: "linux/amd64"}
			if local != nil {
				// Avoid a typed-nil interface: only set Local when non-nil.
				opts.Local = local
			}

			_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{builtExecDep()}, nil, opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			if local != nil {
				assert.Equal(t, tt.wantFind, local.findCalls)
			}
		})
	}
}

func TestVendorAllPlugins_BuiltNeverReplayed(t *testing.T) {
	// The local catalog can overwrite a version in place, so a satisfying
	// built lock entry must still be re-resolved to pick up the current digest.
	lock := &LockFile{Plugins: []LockPlugin{
		lockedExec("1.2.3", &LockPluginSource{Built: true}),
	}}
	local := newFakeLocal()

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{builtExecDep()}, lock, VendorAllPluginsOptions{
		Local:    local,
		Platform: "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, 1, local.findCalls)
	assert.Equal(t, "sha256:aaa", result.ResolvedPlugins[0].Digest, "digest comes from the local catalog, not the lock")
}

func TestVendorAllPlugins_BuiltAndRegistrySameNameRoundTrip(t *testing.T) {
	// A built and a registry plugin sharing name+kind get distinct lock
	// identities: on the next build the registry entry replays and the built
	// entry re-resolves, neither picking up the other's entry.
	deps := []solution.PluginDependency{registryExecDep("^1.0.0"), builtExecDep()}
	opts := func(remote, local ArtifactResolver) VendorAllPluginsOptions {
		return VendorAllPluginsOptions{
			CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			Remotes:         map[string]ArtifactResolver{"myorg": remote},
			Local:           local,
			Platform:        "linux/amd64",
		}
	}

	first, err := VendorAllPlugins(testContext(), deps, nil, opts(newFakeRemote(), newFakeLocal()))
	require.NoError(t, err)
	require.Len(t, first.ResolvedPlugins, 2)
	assert.Equal(t, &LockPluginSource{Registry: "ghcr.io/myorg"}, first.ResolvedPlugins[0].Source)
	assert.Equal(t, &LockPluginSource{Built: true}, first.ResolvedPlugins[1].Source)

	remote, local := newFakeRemote(), newFakeLocal()
	second, err := VendorAllPlugins(testContext(), deps, &LockFile{Plugins: first.ResolvedPlugins}, opts(remote, local))
	require.NoError(t, err)

	assert.Equal(t, 0, remote.findCalls, "registry entry replays")
	assert.Equal(t, 1, local.findCalls, "built entry re-resolves")
	assert.Equal(t, first.ResolvedPlugins, second.ResolvedPlugins, "unchanged artifacts yield an identical lock")
}

// shortExecDep is an exec provider dependency declared by short name only.
func shortExecDep() solution.PluginDependency {
	return solution.PluginDependency{
		Name:    "exec",
		Kind:    solution.PluginKindProvider,
		Version: "^1.0.0",
	}
}

func TestVendorAllPlugins_ShortNameResolves(t *testing.T) {
	// A short name searches the local catalog across all sources. The lock
	// entry keeps a nil Source (its lock identity) even though the resolved
	// copy came from a remote origin, which is recorded in ResolvedCanonical.
	local := newFakeLocal()
	local.findInfo.Canonical = "ghcr.io/myorg"

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, nil, VendorAllPluginsOptions{
		Local:    local,
		Platform: "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, catalog.ArtifactQuery{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "exec",
		Version: "^1.0.0",
		Source:  catalog.SourceSelector{Kind: catalog.SourceAny},
	}, local.findQuery)

	assert.Equal(t, LockPlugin{
		Name:       "exec",
		Kind:       "provider",
		Version:    "1.2.3",
		Constraint: "^1.0.0",
		Digest:     "sha256:aaa",
		Digests: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
		},
		ResolvedFrom:      "local",
		ResolvedCanonical: "ghcr.io/myorg",
	}, result.ResolvedPlugins[0])
}

func TestVendorAllPlugins_ShortNameErrors(t *testing.T) {
	ambiguous := &catalog.AmbiguousArtifactError{
		Query: catalog.ArtifactQuery{Kind: catalog.ArtifactKindProvider, Name: "exec"},
	}
	tests := []struct {
		name          string
		local         func() *fakeArtifactResolver
		wantErr       string
		wantAmbiguous bool
		wantFind      int
	}{
		{
			name:    "no local catalog configured",
			local:   func() *fakeArtifactResolver { return nil },
			wantErr: "no local catalog configured",
		},
		{
			name: "find error propagates",
			local: func() *fakeArtifactResolver {
				f := newFakeLocal()
				f.findErr = errors.New("artifact not found")
				return f
			},
			wantErr:  "artifact not found",
			wantFind: 1,
		},
		{
			name: "ambiguous match is surfaced",
			local: func() *fakeArtifactResolver {
				f := newFakeLocal()
				f.findErr = ambiguous
				return f
			},
			wantAmbiguous: true,
			wantFind:      1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local := tt.local()
			opts := VendorAllPluginsOptions{Platform: "linux/amd64"}
			if local != nil {
				// Avoid a typed-nil interface: only set Local when non-nil.
				opts.Local = local
			}

			_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, nil, opts)
			require.Error(t, err)
			if tt.wantErr != "" {
				assert.Contains(t, err.Error(), tt.wantErr)
			}
			if tt.wantAmbiguous {
				var ambErr *catalog.AmbiguousArtifactError
				assert.ErrorAs(t, err, &ambErr)
				assert.ErrorIs(t, err, catalog.ErrAmbiguousArtifact)
			}
			if local != nil {
				assert.Equal(t, tt.wantFind, local.findCalls)
			}
		})
	}
}

func TestVendorAllPlugins_ShortNameNeverReplayed(t *testing.T) {
	// A short name may resolve to a built copy whose digest can change in
	// place, so a satisfying short-name lock entry is still re-resolved.
	lock := &LockFile{Plugins: []LockPlugin{lockedExec("1.2.3", nil)}}
	local := newFakeLocal()

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, lock, VendorAllPluginsOptions{
		Local:    local,
		Platform: "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, 1, local.findCalls)
	assert.Equal(t, "sha256:aaa", result.ResolvedPlugins[0].Digest, "digest comes from the local catalog, not the lock")
	assert.Nil(t, result.ResolvedPlugins[0].Source)
}

func TestVendorAllPlugins_AllFormsSameNameRoundTrip(t *testing.T) {
	// Registry, built, and short-name plugins sharing name+kind get distinct
	// lock identities, and each is found again by its own declaration.
	deps := []solution.PluginDependency{registryExecDep("^1.0.0"), builtExecDep(), shortExecDep()}
	opts := VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Remotes:         map[string]ArtifactResolver{"myorg": newFakeRemote()},
		Local:           newFakeLocal(),
		Platform:        "linux/amd64",
	}

	result, err := VendorAllPlugins(testContext(), deps, nil, opts)
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 3)

	lock := &LockFile{Plugins: result.ResolvedPlugins}
	for i, dep := range deps {
		found := lock.FindPluginByDep(dep)
		require.NotNil(t, found, "dep %d must find its own entry", i)
		assert.Same(t, &lock.Plugins[i], found, "dep %d must find its own entry, not another form's", i)
	}
}

func TestVendorAllPlugins_EmptyPlatform(t *testing.T) {
	remote, local := newFakeRemote(), newFakeLocal()
	deps := []solution.PluginDependency{registryExecDep("^1.0.0"), builtExecDep(), shortExecDep()}

	_, err := VendorAllPlugins(testContext(), deps, nil, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Remotes:         map[string]ArtifactResolver{"myorg": remote},
		Local:           local,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a build platform")
	assert.Zero(t, remote.findCalls)
	assert.Zero(t, local.findCalls)
}

func TestVendorAllPlugins_BuildPlatformNotPublished(t *testing.T) {
	// The fakes publish linux/amd64 and darwin/arm64 only, so a windows build
	// has no primary digest and must fail rather than lock a blank digest.
	tests := []struct {
		name string
		dep  solution.PluginDependency
	}{
		{name: "registry", dep: registryExecDep("^1.0.0")},
		{name: "built", dep: builtExecDep()},
		{name: "short name", dep: shortExecDep()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{tt.dep}, nil, VendorAllPluginsOptions{
				CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
				Remotes:         map[string]ArtifactResolver{"myorg": newFakeRemote()},
				Local:           newFakeLocal(),
				Platform:        "windows/amd64",
			})
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), `no content digest resolved for build platform "windows/amd64"`)
		})
	}
}

func TestLockSourceFor(t *testing.T) {
	tests := []struct {
		name string
		dep  solution.PluginDependency
		want *LockPluginSource
	}{
		{name: "registry", dep: registryExecDep("^1.0.0"), want: &LockPluginSource{Registry: "ghcr.io/myorg"}},
		{name: "built", dep: builtExecDep(), want: &LockPluginSource{Built: true}},
		{name: "short name", dep: shortExecDep(), want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lockSourceFor(tt.dep))
		})
	}
}

func TestSelectorFor(t *testing.T) {
	tests := []struct {
		name string
		dep  solution.PluginDependency
		want catalog.SourceSelector
	}{
		{name: "registry", dep: registryExecDep("^1.0.0"), want: catalog.SourceSelector{Kind: catalog.SourceRemote, Remote: "ghcr.io/myorg"}},
		{name: "built", dep: builtExecDep(), want: catalog.SourceSelector{Kind: catalog.SourceBuilt}},
		{name: "short name", dep: shortExecDep(), want: catalog.SourceSelector{Kind: catalog.SourceAny}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, selectorFor(tt.dep))
		})
	}
}
