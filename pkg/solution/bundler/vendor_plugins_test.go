// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeVersionEntry struct {
	info         catalog.ArtifactInfo
	platforms    []string
	digestByPlat map[string]string
}

// fakeCatalogSource implements catalog.PluginSource for fast in-memory tests of
// VendorAllPlugins over a real *catalog.PluginFetcher.
type fakeCatalogSource struct {
	name         string
	findInfo     catalog.ArtifactInfo
	findErr      error
	platforms    []string
	platformsErr error
	digestByPlat map[string]string
	digestErr    map[string]error
	byVersion    map[string]fakeVersionEntry

	findCalls   int
	findQueries []catalog.ArtifactQuery
	platCalls   int
	digestCalls int
	digestPlats []string
	fetchCalls  int
}

func (f *fakeCatalogSource) Name() string { return f.name }

func (f *fakeCatalogSource) FetchByPlatform(_ context.Context, _ catalog.Reference, _ string) ([]byte, catalog.ArtifactInfo, error) {
	f.fetchCalls++
	return nil, catalog.ArtifactInfo{}, errors.New("unexpected binary fetch during vendoring")
}

func (f *fakeCatalogSource) ListPlatforms(_ context.Context, ref catalog.Reference) ([]string, error) {
	f.platCalls++
	if f.platformsErr != nil {
		return nil, f.platformsErr
	}
	if f.findErr != nil {
		return nil, f.findErr
	}
	if ref.Version != nil {
		if v, ok := f.byVersion[ref.Version.String()]; ok {
			return v.platforms, nil
		}
	}
	if f.findInfo.Reference.Name != "" && ref.Name != f.findInfo.Reference.Name {
		return nil, &catalog.ArtifactNotFoundError{Reference: ref, Catalog: f.name}
	}
	return f.platforms, nil
}

func (f *fakeCatalogSource) ResolveContentDigest(_ context.Context, ref catalog.Reference, platform, _ string) (catalog.ContentDigestInfo, error) {
	f.digestCalls++
	f.digestPlats = append(f.digestPlats, platform)
	if err := f.digestErr[platform]; err != nil {
		return catalog.ContentDigestInfo{}, err
	}
	if ref.Version != nil {
		if v, ok := f.byVersion[ref.Version.String()]; ok {
			return catalog.ContentDigestInfo{
				ArtifactInfo:  v.info,
				ContentDigest: v.digestByPlat[platform],
			}, nil
		}
	}
	return catalog.ContentDigestInfo{
		ArtifactInfo:  f.findInfo,
		ContentDigest: f.digestByPlat[platform],
	}, nil
}

func (f *fakeCatalogSource) Find(_ context.Context, q catalog.ArtifactQuery) (catalog.ArtifactInfo, error) {
	f.findCalls++
	f.findQueries = append(f.findQueries, q)
	if f.findErr != nil {
		return catalog.ArtifactInfo{}, f.findErr
	}
	if v, ok := f.byVersion[q.Version]; ok {
		return v.info, nil
	}
	return f.findInfo, nil
}

func mustPluginFetcher(t testing.TB, sources ...catalog.PluginSource) *catalog.PluginFetcher {
	t.Helper()
	pf, err := catalog.NewPluginFetcher(logr.Discard(), nil, sources...)
	require.NoError(t, err)
	return pf
}

// newTestAliasResolver builds a catalog index mapping catalog aliases to OCI URLs.
func newTestAliasResolver(alias, url string) *catalogindex.Index { //nolint:unparam // test helper uses constant alias for clarity
	return catalogindex.FromConfig(&config.Config{
		Catalogs: []config.CatalogConfig{{Name: alias, URL: url}},
	})
}

func newMultiAliasResolver(pairs ...string) *catalogindex.Index {
	var cats []config.CatalogConfig
	for i := 0; i+1 < len(pairs); i += 2 {
		cats = append(cats, config.CatalogConfig{Name: pairs[i], URL: pairs[i+1]})
	}
	return catalogindex.FromConfig(&config.Config{Catalogs: cats})
}

func newFakeRemoteSource(alias, canonical, name, version string) *fakeCatalogSource {
	imgRef := canonical + "/providers/" + name + "@sha256:manifest-" + alias
	return &fakeCatalogSource{
		name:      alias,
		platforms: []string{"linux/amd64", "darwin/arm64"},
		digestByPlat: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
		},
		findInfo: catalog.ArtifactInfo{
			Reference:      catalog.Reference{Kind: catalog.ArtifactKindProvider, Name: name, Version: semver.MustParse(version)},
			Catalog:        alias,
			Canonical:      canonical,
			ManifestDigest: "sha256:manifest-" + alias,
			ImageRef:       imgRef,
		},
	}
}

func newFakeRemote() *fakeCatalogSource {
	return newFakeRemoteSource("myorg", "ghcr.io/myorg", "exec", "1.2.3")
}

func newFakeLocal() *fakeCatalogSource {
	f := newFakeRemoteSource(catalog.LocalCatalogName, "", "exec", "1.2.3")
	f.findInfo.ManifestDigest = "sha256:local-manifest"
	f.findInfo.ImageRef = ""
	return f
}

func notFoundInCatalog(alias, name string) *fakeCatalogSource {
	return &fakeCatalogSource{
		name:    alias,
		findErr: &catalog.ArtifactNotFoundError{Reference: catalog.Reference{Kind: catalog.ArtifactKindProvider, Name: name}, Catalog: alias},
	}
}

func registryExecDep(version string) solution.PluginDependency {
	return solution.PluginDependency{
		Name:    "exec",
		Kind:    solution.PluginKindProvider,
		Version: version,
		Source:  &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"},
	}
}

func builtExecDep() solution.PluginDependency {
	return solution.PluginDependency{
		Name:    "exec",
		Kind:    solution.PluginKindProvider,
		Version: "^1.0.0",
		Source:  &solution.PluginSource{Built: true},
	}
}

func shortExecDep() solution.PluginDependency {
	return solution.PluginDependency{
		Name:    "exec",
		Kind:    solution.PluginKindProvider,
		Version: "^1.0.0",
	}
}

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

func TestResolveContentDigests_MultiPlatform(t *testing.T) {
	t.Parallel()
	src := newFakeRemote()
	pf := mustPluginFetcher(t, src)

	digests, primary, err := resolveContentDigests(t.Context(), pf, "exec", catalog.ArtifactKindProvider, "1.2.3", "", "linux/amd64", catalog.MediaTypeProviderBinary, catalog.WithCatalog("myorg"))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"linux/amd64":  "sha256:aaa",
		"darwin/arm64": "sha256:bbb",
	}, digests)
	assert.Equal(t, "sha256:aaa", primary)
	assert.Zero(t, src.fetchCalls, "must never download binary blobs")
}

func TestResolveContentDigests_SinglePlatformNilMap(t *testing.T) {
	t.Parallel()
	src := newFakeRemote()
	src.platforms = nil
	src.digestByPlat = map[string]string{"linux/amd64": "sha256:only"}
	pf := mustPluginFetcher(t, src)

	digests, primary, err := resolveContentDigests(t.Context(), pf, "exec", catalog.ArtifactKindProvider, "1.2.3", "", "linux/amd64", catalog.MediaTypeProviderBinary, catalog.WithCatalog("myorg"))
	require.NoError(t, err)
	assert.Nil(t, digests, "single-platform must leave Digests nil")
	assert.Equal(t, "sha256:only", primary)
}

func TestResolveContentDigests_Errors(t *testing.T) {
	t.Parallel()

	t.Run("list platforms error", func(t *testing.T) {
		t.Parallel()
		src := newFakeRemote()
		src.platformsErr = errors.New("boom")
		pf := mustPluginFetcher(t, src)

		_, _, err := resolveContentDigests(t.Context(), pf, "exec", catalog.ArtifactKindProvider, "1.2.3", "", "linux/amd64", catalog.MediaTypeProviderBinary, catalog.WithCatalog("myorg"))
		require.ErrorContains(t, err, "listing platforms")
	})

	t.Run("per-platform resolve error", func(t *testing.T) {
		t.Parallel()
		src := newFakeRemote()
		src.digestErr = map[string]error{"darwin/arm64": errors.New("network")}
		pf := mustPluginFetcher(t, src)

		_, _, err := resolveContentDigests(t.Context(), pf, "exec", catalog.ArtifactKindProvider, "1.2.3", "", "linux/amd64", catalog.MediaTypeProviderBinary, catalog.WithCatalog("myorg"))
		require.ErrorContains(t, err, "darwin/arm64")
	})

	t.Run("empty content digest", func(t *testing.T) {
		t.Parallel()
		src := newFakeRemote()
		src.digestByPlat = map[string]string{"linux/amd64": ""}
		pf := mustPluginFetcher(t, src)

		_, _, err := resolveContentDigests(t.Context(), pf, "exec", catalog.ArtifactKindProvider, "1.2.3", "", "linux/amd64", catalog.MediaTypeProviderBinary, catalog.WithCatalog("myorg"))
		require.ErrorContains(t, err, "empty digest")
	})

	t.Run("tag moved mid-resolution", func(t *testing.T) {
		t.Parallel()
		src := newFakeRemote()
		src.findInfo.Digest = "sha256:new-index-digest"
		pf := mustPluginFetcher(t, src)

		_, _, err := resolveContentDigests(t.Context(), pf, "exec", catalog.ArtifactKindProvider, "1.2.3", "sha256:original-index-digest", "linux/amd64", catalog.MediaTypeProviderBinary, catalog.WithCatalog("myorg"))
		require.ErrorContains(t, err, "moved from sha256:original-index-digest to sha256:new-index-digest")
	})
}

func TestVendorAllPlugins_InvalidSourceRejectedUpFront(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source *solution.PluginSource
	}{
		{
			name:   "both registry and built set",
			source: &solution.PluginSource{Registry: "ghcr.io/myorg", Built: true},
		},
		{
			name:   "empty source struct with neither registry nor built",
			source: &solution.PluginSource{Artifact: "exec"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			remote := newFakeRemote()
			dep := solution.PluginDependency{
				Name:    "exec",
				Kind:    solution.PluginKindProvider,
				Version: "^1.0.0",
				Source:  tt.source,
			}

			_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{dep}, nil, VendorAllPluginsOptions{
				CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
				PluginFetcher:   mustPluginFetcher(t, remote),
				Platform:        "linux/amd64",
			})
			require.ErrorIs(t, err, solution.ErrInvalidPluginSource)
			assert.Zero(t, remote.findCalls)
		})
	}
}

func TestVendorAllPlugins_RegistryResolves(t *testing.T) {
	t.Parallel()
	remote := newFakeRemote()

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, nil, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		PluginFetcher:   mustPluginFetcher(t, remote),
		Platform:        "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)
	require.Len(t, remote.findQueries, 1)

	assert.Equal(t, catalog.ArtifactQuery{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "exec",
		Version: "^1.0.0",
		Source:  catalog.SourceSelector{Kind: catalog.SourceRemote, Remote: "ghcr.io/myorg"},
	}, remote.findQueries[0])

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
	t.Parallel()
	const imageRef = "ghcr.io/myorg/providers/exec@sha256:manifest-myorg"
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
			name:     "records signature when verified",
			imageRef: imageRef,
			verify: func(context.Context, string) (*LockPluginSignature, error) {
				return &LockPluginSignature{Issuer: "https://issuer.example", Identity: "release"}, nil
			},
			wantSig:  &LockPluginSignature{Issuer: "https://issuer.example", Identity: "release"},
			wantRefs: []string{imageRef},
		},
		{
			name:     "verification failure is fatal",
			imageRef: imageRef,
			verify:   func(context.Context, string) (*LockPluginSignature, error) { return nil, sigErr },
			wantErr:  sigErr,
			wantRefs: []string{imageRef},
		},
		{
			name:     "empty image reference is still passed to verifier (enforce fails closed)",
			imageRef: "",
			verify:   func(context.Context, string) (*LockPluginSignature, error) { return nil, sigErr },
			wantErr:  sigErr,
			wantRefs: []string{""},
		},
		{
			name:     "empty image reference allowed when verifier returns nil (warn mode)",
			imageRef: "",
			verify:   func(context.Context, string) (*LockPluginSignature, error) { return nil, nil },
			wantSig:  nil,
			wantRefs: []string{""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			remote := newFakeRemote()
			remote.findInfo.ImageRef = tt.imageRef
			var gotRefs []string
			verify := func(ctx context.Context, ref string) (*LockPluginSignature, error) {
				gotRefs = append(gotRefs, ref)
				return tt.verify(ctx, ref)
			}

			result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, nil, VendorAllPluginsOptions{
				CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
				PluginFetcher:   mustPluginFetcher(t, remote),
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
	t.Parallel()
	remote := newFakeRemoteSource("MyOrg", "ghcr.io/myorg", "exec", "1.2.3")

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, nil, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("MyOrg", "oci://ghcr.io/myorg"),
		PluginFetcher:   mustPluginFetcher(t, remote),
		Platform:        "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)
	assert.Equal(t, 1, remote.findCalls)
}

func TestVendorAllPlugins_RegistryErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resolver     RegistryAliasResolver
		sourceAlias  string
		nilFetcher   bool
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
			name:       "nil plugin fetcher",
			resolver:   newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			nilFetcher: true,
			wantErr:    "no plugin fetcher configured",
		},
		{
			name:        "catalog alias missing from plugin fetcher",
			resolver:    newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			sourceAlias: "different",
			wantErr:     `catalog "myorg" not configured`,
		},
		{
			name:     "find error propagates",
			resolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			findErr:  errors.New("registry unreachable"),
			wantErr:  "registry unreachable",
			wantFind: 1,
		},
		{
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
			t.Parallel()
			alias := tt.sourceAlias
			if alias == "" {
				alias = "myorg"
			}
			remote := newFakeRemoteSource(alias, "ghcr.io/myorg", "exec", "1.2.3")
			remote.findErr = tt.findErr

			opts := VendorAllPluginsOptions{
				CatalogResolver: tt.resolver,
				Platform:        "linux/amd64",
			}
			if !tt.nilFetcher {
				opts.PluginFetcher = mustPluginFetcher(t, remote)
			}

			_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, tt.existingLock, opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Equal(t, tt.wantFind, remote.findCalls)
		})
	}
}

func TestVendorAllPlugins_RegistryLockReplay(t *testing.T) {
	t.Parallel()
	const lockedImageRef = "ghcr.io/myorg/providers/exec@sha256:manifest-1.1.0"
	signerA := &LockPluginSignature{Issuer: "https://issuer.example", Identity: "signer-a"}
	signerB := &LockPluginSignature{Issuer: "https://issuer.example", Identity: "signer-b"}
	sigErr := errors.New("signer-a is not trusted by active policy")

	multiLocked := func(version string, source *LockPluginSource, sig *LockPluginSignature) LockPlugin {
		return LockPlugin{
			Name:       "exec",
			Kind:       "provider",
			Version:    version,
			Constraint: "^1.0.0",
			Digest:     "sha256:locked",
			Digests: map[string]string{
				"linux/amd64":  "sha256:locked",
				"darwin/arm64": "sha256:locked-darwin",
			},
			ResolvedFrom:      "myorg",
			ResolvedCanonical: "ghcr.io/myorg",
			Source:            source,
			Signature:         sig,
		}
	}

	tests := []struct {
		name         string
		lock         *LockFile
		constraint   string
		mutateRemote func(*fakeCatalogSource)
		verifySig    func(context.Context, string) (*LockPluginSignature, error)
		wantVersion  string
		wantDigest   string
		wantSig      *LockPluginSignature
		wantFind     int
		wantFindVer  string
		wantErr      string
	}{
		{
			name: "pinned version satisfies constraint is replayed without network when signatures off",
			lock: &LockFile{Plugins: []LockPlugin{
				multiLocked("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}, nil),
			}},
			constraint:  "^1.0",
			wantVersion: "1.1.0",
			wantDigest:  "sha256:locked",
			wantFind:    0,
		},
		{
			name: "unsigned lock entry is reverified at pinned version when signature verification is active",
			lock: &LockFile{Plugins: []LockPlugin{
				multiLocked("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}, nil),
			}},
			constraint: "^1.0",
			verifySig: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
				if imageRef != lockedImageRef {
					return nil, errors.New("unexpected imageRef: " + imageRef)
				}
				return signerB, nil
			},
			wantVersion: "1.1.0",
			wantDigest:  "sha256:locked",
			wantSig:     signerB,
			wantFind:    1,
			wantFindVer: "1.1.0",
		},
		{
			name: "signed lock entry is reverified against active policy and preserves pinned version",
			lock: &LockFile{Plugins: []LockPlugin{
				multiLocked("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}, signerA),
			}},
			constraint: "^1.0",
			verifySig: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
				if imageRef != lockedImageRef {
					return nil, errors.New("unexpected imageRef: " + imageRef)
				}
				return signerA, nil
			},
			wantVersion: "1.1.0",
			wantDigest:  "sha256:locked",
			wantSig:     signerA,
			wantFind:    1,
			wantFindVer: "1.1.0",
		},
		{
			name: "signed lock entry fails closed under tightened enforce policy",
			lock: &LockFile{Plugins: []LockPlugin{
				multiLocked("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}, signerA),
			}},
			constraint: "^1.0",
			verifySig: func(context.Context, string) (*LockPluginSignature, error) {
				return nil, sigErr
			},
			wantErr: "signature verification failed during lock: signer-a is not trusted by active policy",
		},
		{
			name: "signed lock entry clears stale signature when warn mode verification returns nil",
			lock: &LockFile{Plugins: []LockPlugin{
				multiLocked("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}, signerA),
			}},
			constraint: "^1.0",
			verifySig: func(context.Context, string) (*LockPluginSignature, error) {
				return nil, nil
			},
			wantVersion: "1.1.0",
			wantDigest:  "sha256:locked",
			wantSig:     nil,
			wantFind:    1,
			wantFindVer: "1.1.0",
		},
		{
			name: "reverification fails closed when locked tag content digest changed in catalog",
			lock: &LockFile{Plugins: []LockPlugin{
				multiLocked("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}, signerA),
			}},
			constraint: "^1.0",
			mutateRemote: func(remote *fakeCatalogSource) {
				entry := remote.byVersion["1.1.0"]
				entry.digestByPlat = map[string]string{
					"linux/amd64":  "sha256:tampered",
					"darwin/arm64": "sha256:locked-darwin",
				}
				remote.byVersion["1.1.0"] = entry
			},
			verifySig: func(context.Context, string) (*LockPluginSignature, error) {
				return signerA, nil
			},
			wantErr: "content digest in catalog \"myorg\" does not match lock file",
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
			wantFindVer: "^1.0.0",
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
			wantFindVer: "^1.0.0",
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
			wantFindVer: "^1.0.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			remote := newFakeRemote()
			remote.byVersion = map[string]fakeVersionEntry{
				"1.1.0": {
					info: catalog.ArtifactInfo{
						Reference:      catalog.Reference{Kind: catalog.ArtifactKindProvider, Name: "exec", Version: semver.MustParse("1.1.0")},
						Catalog:        "myorg",
						Canonical:      "ghcr.io/myorg",
						Digest:         "sha256:manifest-1.1.0",
						ManifestDigest: "sha256:manifest-1.1.0",
						ImageRef:       lockedImageRef,
					},
					platforms: []string{"linux/amd64", "darwin/arm64"},
					digestByPlat: map[string]string{
						"linux/amd64":  "sha256:locked",
						"darwin/arm64": "sha256:locked-darwin",
					},
				},
			}
			if tt.mutateRemote != nil {
				tt.mutateRemote(remote)
			}

			result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep(tt.constraint)}, tt.lock, VendorAllPluginsOptions{
				CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
				PluginFetcher:   mustPluginFetcher(t, remote),
				Platform:        "linux/amd64",
				VerifySignature: tt.verifySig,
			})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, result.ResolvedPlugins, 1)

			lp := result.ResolvedPlugins[0]
			assert.Equal(t, tt.wantFind, remote.findCalls)
			if tt.wantFindVer != "" && assert.NotEmpty(t, remote.findQueries) {
				assert.Equal(t, tt.wantFindVer, remote.findQueries[0].Version)
			}
			assert.Equal(t, tt.wantVersion, lp.Version)
			assert.Equal(t, tt.wantDigest, lp.Digest)
			assert.Equal(t, tt.wantSig, lp.Signature)
			assert.Equal(t, tt.constraint, lp.Constraint)
			assert.Equal(t, &LockPluginSource{Registry: "ghcr.io/myorg"}, lp.Source)
		})
	}
}

func TestVendorAllPlugins_BuiltResolvesAndNeverReplays(t *testing.T) {
	t.Parallel()
	local := newFakeLocal()
	remote := newFakeRemote()
	lock := &LockFile{Plugins: []LockPlugin{
		lockedExec("1.2.3", &LockPluginSource{Built: true}),
	}}

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{builtExecDep()}, lock, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		PluginFetcher:   mustPluginFetcher(t, local, remote),
		Platform:        "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, 1, local.findCalls, "built entry must always re-resolve from local")
	assert.Zero(t, remote.findCalls, "built entry must not query remote catalogs")
	assert.Equal(t, catalog.ArtifactQuery{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "exec",
		Version: "^1.0.0",
		Source:  catalog.SourceSelector{Kind: catalog.SourceBuilt},
	}, local.findQueries[0])
	assert.Equal(t, "sha256:aaa", result.ResolvedPlugins[0].Digest)
	assert.Equal(t, catalog.LocalCatalogName, result.ResolvedPlugins[0].ResolvedFrom)
	assert.Empty(t, result.ResolvedPlugins[0].ResolvedCanonical)
	assert.Equal(t, &LockPluginSource{Built: true}, result.ResolvedPlugins[0].Source)
}

func TestVendorAllPlugins_BuiltErrors(t *testing.T) {
	t.Parallel()

	t.Run("nil plugin fetcher", func(t *testing.T) {
		t.Parallel()
		_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{builtExecDep()}, nil, VendorAllPluginsOptions{
			Platform: "linux/amd64",
		})
		require.ErrorContains(t, err, "no plugin fetcher configured")
	})

	t.Run("local catalog not configured", func(t *testing.T) {
		t.Parallel()
		remote := newFakeRemote()
		_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{builtExecDep()}, nil, VendorAllPluginsOptions{
			PluginFetcher: mustPluginFetcher(t, remote),
			Platform:      "linux/amd64",
		})
		require.ErrorContains(t, err, `catalog "local" not configured`)
	})
}

func TestVendorAllPlugins_ShortNameChainResolutionAndScoping(t *testing.T) {
	t.Parallel()

	// Chain order: local (miss) -> corp (miss) -> official (multi-platform hit) -> mirror (also has exec, must not be touched).
	local := notFoundInCatalog(catalog.LocalCatalogName, "directory")
	corp := notFoundInCatalog("corp", "directory")
	official := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "directory", "0.3.0")
	mirror := newFakeRemoteSource("mirror", "ghcr.io/mirror", "directory", "0.3.0")
	mirror.digestByPlat = map[string]string{
		"linux/amd64":  "sha256:wrong-mirror-linux",
		"darwin/arm64": "sha256:wrong-mirror-darwin",
	}

	var verifiedRefs []string
	dep := solution.PluginDependency{
		Name:    "directory",
		Kind:    solution.PluginKindProvider,
		Version: "^0.1.0",
	}

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{dep}, nil, VendorAllPluginsOptions{
		CatalogResolver: newMultiAliasResolver(
			"corp", "oci://ghcr.io/corp",
			"official", "oci://ghcr.io/oakwood-commons",
			"mirror", "oci://ghcr.io/mirror",
		),
		PluginFetcher: mustPluginFetcher(t, local, corp, official, mirror),
		Platform:      "linux/amd64",
		VerifySignature: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
			verifiedRefs = append(verifiedRefs, imageRef)
			return &LockPluginSignature{Issuer: "https://token.actions.githubusercontent.com", Identity: "official-ci"}, nil
		},
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	// Verify ListPlatforms and ResolveContentDigest were scoped to "official" via WithCatalog(found.Catalog):
	// local and corp must NOT be re-probed for ListPlatforms or ResolveContentDigest, and mirror is never touched.
	assert.Zero(t, local.platCalls, "non-winning local catalog must not be probed for ListPlatforms")
	assert.Zero(t, local.digestCalls, "non-winning local catalog must not be probed for ResolveContentDigest")
	assert.Zero(t, corp.platCalls, "non-winning corp catalog must not be probed for ListPlatforms")
	assert.Zero(t, corp.digestCalls, "non-winning corp catalog must not be probed for ResolveContentDigest")
	assert.Equal(t, 1, official.platCalls, "winning catalog serves ListPlatforms once")
	assert.Equal(t, 2, official.digestCalls, "winning catalog serves ResolveContentDigest per platform")
	assert.Zero(t, mirror.findCalls)
	assert.Zero(t, mirror.platCalls)
	assert.Zero(t, mirror.digestCalls)

	assert.Equal(t, []string{"ghcr.io/oakwood-commons/providers/directory@sha256:manifest-official"}, verifiedRefs)
	assert.Equal(t, LockPlugin{
		Name:       "directory",
		Kind:       "provider",
		Version:    "0.3.0",
		Constraint: "^0.1.0",
		Digest:     "sha256:aaa",
		Digests: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
		},
		ResolvedFrom:      "official",
		ResolvedCanonical: "ghcr.io/oakwood-commons",
		Source:            nil,
		Signature:         &LockPluginSignature{Issuer: "https://token.actions.githubusercontent.com", Identity: "official-ci"},
	}, result.ResolvedPlugins[0])
}

func TestVendorAllPlugins_ShortNameLocalBuildShadowsLockAndRemotes(t *testing.T) {
	t.Parallel()

	local := newFakeLocal()
	local.digestByPlat["linux/amd64"] = "sha256:local-built-digest"
	remote := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "exec", "1.9.0")

	// Even with a remote lock entry that satisfies ^1.0.0, a local build in
	// "local" shadows both the lock and remote catalogs.
	existingLock := &LockFile{Plugins: []LockPlugin{
		{
			Name:              "exec",
			Kind:              "provider",
			Version:           "1.5.0",
			Constraint:        "^1.0.0",
			Digest:            "sha256:locked-remote",
			ResolvedFrom:      "official",
			ResolvedCanonical: "ghcr.io/oakwood-commons",
		},
	}}

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, existingLock, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
		PluginFetcher:   mustPluginFetcher(t, local, remote),
		Platform:        "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, 1, local.findCalls)
	assert.Zero(t, remote.findCalls, "local build must shadow remote catalogs")
	assert.Equal(t, "sha256:local-built-digest", result.ResolvedPlugins[0].Digest)
	assert.Equal(t, catalog.LocalCatalogName, result.ResolvedPlugins[0].ResolvedFrom)
	assert.Empty(t, result.ResolvedPlugins[0].ResolvedCanonical)
	assert.Nil(t, result.ResolvedPlugins[0].Source)
}

func TestVendorAllPlugins_ShortNameLocalCorruptFailsClosed(t *testing.T) {
	t.Parallel()

	local := &fakeCatalogSource{
		name:    catalog.LocalCatalogName,
		findErr: errors.New("corrupt local OCI index"),
	}
	remote := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "exec", "1.2.3")
	existingLock := &LockFile{Plugins: []LockPlugin{
		{
			Name:              "exec",
			Kind:              "provider",
			Version:           "1.2.3",
			Constraint:        "^1.0.0",
			Digest:            "sha256:locked",
			ResolvedFrom:      "official",
			ResolvedCanonical: "ghcr.io/oakwood-commons",
		},
	}}

	_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, existingLock, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
		PluginFetcher:   mustPluginFetcher(t, local, remote),
		Platform:        "linux/amd64",
	})
	require.ErrorContains(t, err, "corrupt local OCI index")
	assert.Zero(t, remote.findCalls, "non-404 local error must fail closed without falling through to lock or remote")
}

func TestVendorAllPlugins_ShortNameWithoutLocalConfigured(t *testing.T) {
	t.Parallel()

	// PluginFetcher configured with remotes only (no "local" source, e.g. WithoutLocalBuilds).
	official := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "exec", "1.2.3")

	result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, nil, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
		PluginFetcher:   mustPluginFetcher(t, official),
		Platform:        "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)
	assert.Equal(t, "official", result.ResolvedPlugins[0].ResolvedFrom)
}

func TestVendorAllPlugins_ShortNameErrorsAndEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("nil plugin fetcher", func(t *testing.T) {
		t.Parallel()
		_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, nil, VendorAllPluginsOptions{
			Platform: "linux/amd64",
		})
		require.ErrorContains(t, err, "no plugin fetcher configured")
	})

	t.Run("not found in any catalog", func(t *testing.T) {
		t.Parallel()
		local := notFoundInCatalog(catalog.LocalCatalogName, "exec")
		official := notFoundInCatalog("official", "exec")
		_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, nil, VendorAllPluginsOptions{
			CatalogResolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
			PluginFetcher:   mustPluginFetcher(t, local, official),
			Platform:        "linux/amd64",
		})
		require.ErrorIs(t, err, catalog.ErrArtifactNotFound)
	})

	t.Run("ambiguous artifact error surfaces", func(t *testing.T) {
		t.Parallel()
		ambiguous := &catalog.AmbiguousArtifactError{
			Query: catalog.ArtifactQuery{Kind: catalog.ArtifactKindProvider, Name: "exec"},
		}
		local := notFoundInCatalog(catalog.LocalCatalogName, "exec")
		official := &fakeCatalogSource{name: "official", findErr: ambiguous}
		_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, nil, VendorAllPluginsOptions{
			CatalogResolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
			PluginFetcher:   mustPluginFetcher(t, local, official),
			Platform:        "linux/amd64",
		})
		require.ErrorIs(t, err, catalog.ErrAmbiguousArtifact)
	})

	t.Run("nil catalog resolver skips short-name lock replay and re-resolves", func(t *testing.T) {
		t.Parallel()
		local := notFoundInCatalog(catalog.LocalCatalogName, "exec")
		official := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "exec", "1.2.3")
		lock := &LockFile{Plugins: []LockPlugin{{
			Name:              "exec",
			Kind:              "provider",
			Version:           "1.1.0",
			Constraint:        "^1.0.0",
			Digest:            "sha256:locked",
			ResolvedFrom:      "official",
			ResolvedCanonical: "ghcr.io/oakwood-commons",
		}}}
		res, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, lock, VendorAllPluginsOptions{
			CatalogResolver: nil,
			PluginFetcher:   mustPluginFetcher(t, local, official),
			Platform:        "linux/amd64",
		})
		require.NoError(t, err)
		assert.Equal(t, 1, official.findCalls)
		assert.Equal(t, "1.2.3", res.ResolvedPlugins[0].Version)
	})

	t.Run("missing resolved version or catalog name in finishResolveDependency", func(t *testing.T) {
		t.Parallel()
		noVer := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "exec", "1.2.3")
		noVer.findInfo.Reference.Version = nil
		_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, nil, VendorAllPluginsOptions{
			PluginFetcher: mustPluginFetcher(t, noVer),
			Platform:      "linux/amd64",
		})
		require.ErrorContains(t, err, "catalog returned no resolved version")

		noCat := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "exec", "1.2.3")
		noCat.findInfo.Catalog = ""
		_, err = VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, nil, VendorAllPluginsOptions{
			PluginFetcher: mustPluginFetcher(t, noCat),
			Platform:      "linux/amd64",
		})
		require.ErrorContains(t, err, "catalog returned no source catalog name")
	})
}

func TestVendorAllPlugins_ShortNameLockReplay(t *testing.T) {
	t.Parallel()

	const lockedImageRef = "ghcr.io/oakwood-commons/providers/exec@sha256:manifest-official-1.1.0"
	signerA := &LockPluginSignature{Issuer: "https://token.actions.githubusercontent.com", Identity: "signer-a"}
	signerB := &LockPluginSignature{Issuer: "https://token.actions.githubusercontent.com", Identity: "signer-b"}
	sigErr := errors.New("signer-a is not trusted by active policy")

	remoteLockEntry := func(version, canonical string, s *LockPluginSignature) LockPlugin {
		return LockPlugin{
			Name:       "exec",
			Kind:       "provider",
			Version:    version,
			Constraint: "^1.0.0",
			Digest:     "sha256:locked-remote",
			Digests: map[string]string{
				"linux/amd64":  "sha256:locked-remote",
				"darwin/arm64": "sha256:locked-remote-darwin",
			},
			ResolvedFrom:      "corp",
			ResolvedCanonical: canonical,
			Signature:         s,
		}
	}

	tests := []struct {
		name         string
		existingLock *LockFile
		resolver     RegistryAliasResolver
		verifySig    func(context.Context, string) (*LockPluginSignature, error)
		wantVersion  string
		wantDigest   string
		wantFrom     string
		wantSig      *LockPluginSignature
		wantFind     int
		wantFindVer  string
		wantErr      string
	}{
		{
			name: "replays remote lock entry without network when catalog configured and signatures off",
			existingLock: &LockFile{Plugins: []LockPlugin{
				remoteLockEntry("1.1.0", "ghcr.io/corp", nil),
			}},
			resolver:    newMultiAliasResolver("corp", "oci://ghcr.io/corp", "official", "oci://ghcr.io/oakwood-commons"),
			wantVersion: "1.1.0",
			wantDigest:  "sha256:locked-remote",
			wantFrom:    "corp",
			wantFind:    0,
		},
		{
			name: "invalidates replay when locked catalog is no longer configured",
			existingLock: &LockFile{Plugins: []LockPlugin{
				remoteLockEntry("1.1.0", "ghcr.io/removed-corp", nil),
			}},
			resolver:    newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
			wantVersion: "1.2.3",
			wantDigest:  "sha256:aaa",
			wantFrom:    "official",
			wantFind:    1,
			wantFindVer: "^1.0.0",
		},
		{
			name: "never replays local built lock entry",
			existingLock: &LockFile{Plugins: []LockPlugin{
				{
					Name:         "exec",
					Kind:         "provider",
					Version:      "1.1.0",
					Constraint:   "^1.0.0",
					Digest:       "sha256:stale-built",
					ResolvedFrom: catalog.LocalCatalogName,
				},
			}},
			resolver:    newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
			wantVersion: "1.2.3",
			wantDigest:  "sha256:aaa",
			wantFrom:    "official",
			wantFind:    1,
			wantFindVer: "^1.0.0",
		},
		{
			name: "reverifies unsigned short-name lock entry at pinned version when signature policy is active",
			existingLock: &LockFile{Plugins: []LockPlugin{
				remoteLockEntry("1.1.0", "ghcr.io/oakwood-commons", nil),
			}},
			resolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
			verifySig: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
				if imageRef != lockedImageRef {
					return nil, errors.New("unexpected imageRef: " + imageRef)
				}
				return signerB, nil
			},
			wantVersion: "1.1.0",
			wantDigest:  "sha256:locked-remote",
			wantFrom:    "corp",
			wantSig:     signerB,
			wantFind:    1,
			wantFindVer: "1.1.0",
		},
		{
			name: "reverifies signed short-name lock entry against active policy and preserves pinned metadata",
			existingLock: &LockFile{Plugins: []LockPlugin{
				remoteLockEntry("1.1.0", "ghcr.io/oakwood-commons", signerA),
			}},
			resolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
			verifySig: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
				if imageRef != lockedImageRef {
					return nil, errors.New("unexpected imageRef: " + imageRef)
				}
				return signerA, nil
			},
			wantVersion: "1.1.0",
			wantDigest:  "sha256:locked-remote",
			wantFrom:    "corp",
			wantSig:     signerA,
			wantFind:    1,
			wantFindVer: "1.1.0",
		},
		{
			name: "signed short-name lock entry fails closed when tightened enforce policy rejects recorded signer",
			existingLock: &LockFile{Plugins: []LockPlugin{
				remoteLockEntry("1.1.0", "ghcr.io/oakwood-commons", signerA),
			}},
			resolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
			verifySig: func(context.Context, string) (*LockPluginSignature, error) {
				return nil, sigErr
			},
			wantErr: "signature verification failed during lock: signer-a is not trusted by active policy",
		},
		{
			name: "signed short-name lock entry clears signature when warn mode returns nil",
			existingLock: &LockFile{Plugins: []LockPlugin{
				remoteLockEntry("1.1.0", "ghcr.io/oakwood-commons", signerA),
			}},
			resolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
			verifySig: func(context.Context, string) (*LockPluginSignature, error) {
				return nil, nil
			},
			wantVersion: "1.1.0",
			wantDigest:  "sha256:locked-remote",
			wantFrom:    "corp",
			wantSig:     nil,
			wantFind:    1,
			wantFindVer: "1.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			local := notFoundInCatalog(catalog.LocalCatalogName, "exec")
			official := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "exec", "1.2.3")
			official.byVersion = map[string]fakeVersionEntry{
				"1.1.0": {
					info: catalog.ArtifactInfo{
						Reference:      catalog.Reference{Kind: catalog.ArtifactKindProvider, Name: "exec", Version: semver.MustParse("1.1.0")},
						Catalog:        "official",
						Canonical:      "ghcr.io/oakwood-commons",
						Digest:         "sha256:manifest-official-1.1.0",
						ManifestDigest: "sha256:manifest-official-1.1.0",
						ImageRef:       lockedImageRef,
					},
					platforms: []string{"linux/amd64", "darwin/arm64"},
					digestByPlat: map[string]string{
						"linux/amd64":  "sha256:locked-remote",
						"darwin/arm64": "sha256:locked-remote-darwin",
					},
				},
			}

			result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{shortExecDep()}, tt.existingLock, VendorAllPluginsOptions{
				CatalogResolver: tt.resolver,
				PluginFetcher:   mustPluginFetcher(t, local, official),
				Platform:        "linux/amd64",
				VerifySignature: tt.verifySig,
			})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, result.ResolvedPlugins, 1)

			lp := result.ResolvedPlugins[0]
			assert.Equal(t, tt.wantFind, official.findCalls)
			if tt.wantFindVer != "" && assert.NotEmpty(t, official.findQueries) {
				assert.Equal(t, tt.wantFindVer, official.findQueries[0].Version)
			}
			assert.Equal(t, tt.wantVersion, lp.Version)
			assert.Equal(t, tt.wantDigest, lp.Digest)
			assert.Equal(t, tt.wantFrom, lp.ResolvedFrom)
			assert.Equal(t, tt.wantSig, lp.Signature)
			assert.Nil(t, lp.Source)
		})
	}
}

// minimalOCIRegistry serves a multi-platform provider over OCI Distribution v2
// so VendorAllPlugins can be tested against real RemoteCatalog + LocalCatalog +
// CachedRemoteCatalog instances (verifying live and cached ImageRef population).
type minimalOCIRegistry struct {
	mu        sync.RWMutex
	blobs     map[string][]byte
	manifests map[string]ociEntry
	tags      map[string]map[string]string // repo -> tag -> digest
}

type ociEntry struct {
	mediaType string
	data      []byte
}

func newMinimalOCIRegistry() *minimalOCIRegistry {
	return &minimalOCIRegistry{
		blobs:     make(map[string][]byte),
		manifests: make(map[string]ociEntry),
		tags:      make(map[string]map[string]string),
	}
}

func (r *minimalOCIRegistry) seedMultiPlatformProvider(t *testing.T, repo, name, version string, bins map[string][]byte) string {
	t.Helper()
	fullRepo := repo + "/providers/" + name
	cfgData := []byte(`{}`)
	cfgDigest := digest.FromBytes(cfgData).String()
	r.blobs[cfgDigest] = cfgData

	platDescs := make([]ocispec.Descriptor, 0, len(bins))
	for plat, content := range bins {
		ociPlat, err := catalog.PlatformToOCI(plat)
		require.NoError(t, err)
		layerDigest := digest.FromBytes(content)
		r.blobs[layerDigest.String()] = content

		m := ocispec.Manifest{
			MediaType: ocispec.MediaTypeImageManifest,
			Config: ocispec.Descriptor{
				MediaType: "application/vnd.scafctl.provider.config.v1+json",
				Digest:    digest.Digest(cfgDigest),
				Size:      int64(len(cfgData)),
			},
			Layers: []ocispec.Descriptor{{
				MediaType: catalog.MediaTypeProviderBinary,
				Digest:    layerDigest,
				Size:      int64(len(content)),
			}},
			Annotations: map[string]string{
				catalog.AnnotationArtifactName: name,
				catalog.AnnotationArtifactType: catalog.ArtifactKindProvider.String(),
				catalog.AnnotationVersion:      version,
				catalog.AnnotationPlatform:     plat,
			},
		}
		m.SchemaVersion = 2
		mBytes, err := json.Marshal(m)
		require.NoError(t, err)
		mDigest := digest.FromBytes(mBytes)
		r.manifests[mDigest.String()] = ociEntry{mediaType: ocispec.MediaTypeImageManifest, data: mBytes}
		r.blobs[mDigest.String()] = mBytes
		platDescs = append(platDescs, ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    mDigest,
			Size:      int64(len(mBytes)),
			Platform:  ociPlat,
		})
	}

	idx := ocispec.Index{
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: platDescs,
		Annotations: map[string]string{
			catalog.AnnotationArtifactName: name,
			catalog.AnnotationArtifactType: catalog.ArtifactKindProvider.String(),
			catalog.AnnotationVersion:      version,
		},
	}
	idx.SchemaVersion = 2
	idxBytes, err := json.Marshal(idx)
	require.NoError(t, err)
	idxDigest := digest.FromBytes(idxBytes).String()
	r.manifests[idxDigest] = ociEntry{mediaType: ocispec.MediaTypeImageIndex, data: idxBytes}
	r.blobs[idxDigest] = idxBytes
	if r.tags[fullRepo] == nil {
		r.tags[fullRepo] = make(map[string]string)
	}
	r.tags[fullRepo][version] = idxDigest
	return idxDigest
}

func (r *minimalOCIRegistry) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/v2/")
		if req.URL.Path == "/v2/" || req.URL.Path == "/v2" {
			w.WriteHeader(http.StatusOK)
			return
		}
		r.mu.RLock()
		defer r.mu.RUnlock()

		if strings.HasSuffix(path, "/tags/list") {
			repo := strings.TrimSuffix(path, "/tags/list")
			tagMap := r.tags[repo]
			if len(tagMap) == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			tags := make([]string, 0, len(tagMap))
			for k := range tagMap {
				tags = append(tags, k)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": repo, "tags": tags})
			return
		}
		if idx := strings.LastIndex(path, "/manifests/"); idx > 0 {
			repo := path[:idx]
			ref := path[idx+len("/manifests/"):]
			dgst := ref
			if !strings.HasPrefix(ref, "sha256:") {
				dgst = r.tags[repo][ref]
			}
			entry, ok := r.manifests[dgst]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", entry.mediaType)
			w.Header().Set("Docker-Content-Digest", dgst)
			w.Header().Set("Content-Length", strconv.Itoa(len(entry.data)))
			if req.Method == http.MethodGet {
				_, _ = w.Write(entry.data)
			}
			return
		}
		if idx := strings.LastIndex(path, "/blobs/"); idx > 0 {
			dgst := path[idx+len("/blobs/"):]
			data, ok := r.blobs[dgst]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Docker-Content-Digest", dgst)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			if req.Method == http.MethodGet {
				_, _ = w.Write(data)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
}

func TestVendorAllPlugins_LiveAndCachedRemoteImageRef(t *testing.T) {
	t.Parallel()

	reg := newMinimalOCIRegistry()
	idxDigest := reg.seedMultiPlatformProvider(t, "scafctl", "directory", "0.2.0", map[string][]byte{
		"linux/amd64":  []byte("dir-linux"),
		"darwin/arm64": []byte("dir-darwin"),
	})
	srv := httptest.NewTLSServer(reg.handler())
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "https://")
	local, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: config.CatalogNameOfficial, Type: config.CatalogTypeOCI, URL: "oci://" + host + "/scafctl", Insecure: true},
		},
	}

	pf, err := catalog.BuildPluginFetcher(cfg, nil, logr.Discard(), catalog.WithLocalCatalog(local))
	require.NoError(t, err)

	dep := solution.PluginDependency{Name: "directory", Kind: solution.PluginKindProvider, Version: "0.2.0"}
	wantImageRef := host + "/scafctl/providers/directory@" + idxDigest

	// 1. Live remote lookup: must pass non-empty ImageRef to VerifySignature.
	var liveRef string
	liveRes, err := VendorAllPlugins(testContext(), []solution.PluginDependency{dep}, nil, VendorAllPluginsOptions{
		CatalogResolver: catalogindex.FromCatalogs(pf.Catalogs()),
		PluginFetcher:   pf,
		Platform:        "linux/amd64",
		VerifySignature: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
			liveRef = imageRef
			return &LockPluginSignature{Issuer: "https://issuer.example", Identity: "live"}, nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, wantImageRef, liveRef)
	require.Len(t, liveRes.ResolvedPlugins, 1)
	assert.Equal(t, digest.FromString("dir-linux").String(), liveRes.ResolvedPlugins[0].Digests["linux/amd64"])
	assert.Equal(t, digest.FromString("dir-darwin").String(), liveRes.ResolvedPlugins[0].Digests["darwin/arm64"])

	// 2. Pull the artifact into LocalCatalog, shut down the remote server, and
	//    verify that CachedRemoteCatalog serves the cached copy (not mistaking
	//    it for a local build) with the exact same ImageRef!
	remoteCat, err := catalog.BuildRemoteCatalogFromConfig(cfg.Catalogs[0], nil, nil, logr.Discard())
	require.NoError(t, err)
	_, err = remoteCat.CopyToLocal(t.Context(), catalog.Reference{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "directory",
		Version: semver.MustParse("0.2.0"),
	}, local, catalog.CopyOptions{})
	require.NoError(t, err)
	srv.Close()

	var cachedRef string
	cachedRes, err := VendorAllPlugins(testContext(), []solution.PluginDependency{dep}, nil, VendorAllPluginsOptions{
		CatalogResolver: catalogindex.FromCatalogs(pf.Catalogs()),
		PluginFetcher:   pf,
		Platform:        "linux/amd64",
		VerifySignature: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
			cachedRef = imageRef
			return &LockPluginSignature{Issuer: "https://issuer.example", Identity: "cached"}, nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, wantImageRef, cachedRef, "cached hit must preserve remote ImageRef for cosign verification")
	require.Len(t, cachedRes.ResolvedPlugins, 1)
	assert.Equal(t, config.CatalogNameOfficial, cachedRes.ResolvedPlugins[0].ResolvedFrom)
	assert.Equal(t, host+"/scafctl", cachedRes.ResolvedPlugins[0].ResolvedCanonical)
	assert.Nil(t, cachedRes.ResolvedPlugins[0].Source)

	// 3. Replaying that signed lock entry under a tightened policy must reverify
	//    against the cached OCI ImageRef and fail closed when the signer is rejected.
	existingLock := &LockFile{Plugins: cachedRes.ResolvedPlugins}
	constraintDep := solution.PluginDependency{Name: "directory", Kind: solution.PluginKindProvider, Version: "^0.2.0"}
	var replayRef string
	_, err = VendorAllPlugins(testContext(), []solution.PluginDependency{constraintDep}, existingLock, VendorAllPluginsOptions{
		CatalogResolver: catalogindex.FromCatalogs(pf.Catalogs()),
		PluginFetcher:   pf,
		Platform:        "linux/amd64",
		VerifySignature: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
			replayRef = imageRef
			return nil, errors.New("signer rejected by tightened policy")
		},
	})
	require.ErrorContains(t, err, "signer rejected by tightened policy")
	assert.Equal(t, wantImageRef, replayRef)
}

func TestVendorAllPlugins_LockReplayReverifyEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("single-platform lock entry reverifies matching sole digest", func(t *testing.T) {
		t.Parallel()
		remote := newFakeRemote()
		remote.platforms = nil
		remote.digestByPlat = map[string]string{"linux/amd64": "sha256:single"}
		remote.findInfo.Reference.Version = semver.MustParse("1.1.0")
		lock := &LockFile{Plugins: []LockPlugin{{
			Name:              "exec",
			Kind:              "provider",
			Version:           "1.1.0",
			Constraint:        "^1.0.0",
			Digest:            "sha256:single",
			ResolvedFrom:      "myorg",
			ResolvedCanonical: "ghcr.io/myorg",
			Source:            &LockPluginSource{Registry: "ghcr.io/myorg"},
			Signature:         &LockPluginSignature{Issuer: "https://issuer.example", Identity: "old"},
		}}}
		wantSig := &LockPluginSignature{Issuer: "https://issuer.example", Identity: "verified"}
		res, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, lock, VendorAllPluginsOptions{
			CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			PluginFetcher:   mustPluginFetcher(t, remote),
			Platform:        "linux/amd64",
			VerifySignature: func(context.Context, string) (*LockPluginSignature, error) {
				return wantSig, nil
			},
		})
		require.NoError(t, err)
		require.Len(t, res.ResolvedPlugins, 1)
		assert.Nil(t, res.ResolvedPlugins[0].Digests)
		assert.Equal(t, "sha256:single", res.ResolvedPlugins[0].Digest)
		assert.Equal(t, wantSig, res.ResolvedPlugins[0].Signature)
	})

	t.Run("nil plugin fetcher fails closed when reverifying registry lock entry", func(t *testing.T) {
		t.Parallel()
		lock := &LockFile{Plugins: []LockPlugin{
			lockedExec("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}),
		}}
		_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, lock, VendorAllPluginsOptions{
			CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			Platform:        "linux/amd64",
			VerifySignature: func(context.Context, string) (*LockPluginSignature, error) {
				return nil, nil
			},
		})
		require.ErrorContains(t, err, "no plugin fetcher configured")
	})

	t.Run("missing locked version in catalog fails closed during reverification", func(t *testing.T) {
		t.Parallel()
		remote := notFoundInCatalog("myorg", "exec")
		lock := &LockFile{Plugins: []LockPlugin{
			lockedExec("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}),
		}}
		_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, lock, VendorAllPluginsOptions{
			CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			PluginFetcher:   mustPluginFetcher(t, remote),
			Platform:        "linux/amd64",
			VerifySignature: func(context.Context, string) (*LockPluginSignature, error) {
				return nil, nil
			},
		})
		require.ErrorIs(t, err, catalog.ErrArtifactNotFound)
	})

	t.Run("mismatched resolved version fails closed during reverification", func(t *testing.T) {
		t.Parallel()
		remote := newFakeRemote() // returns 1.2.3 even when 1.1.0 is queried
		lock := &LockFile{Plugins: []LockPlugin{
			lockedExec("1.1.0", &LockPluginSource{Registry: "ghcr.io/myorg"}),
		}}
		_, err := VendorAllPlugins(testContext(), []solution.PluginDependency{registryExecDep("^1.0.0")}, lock, VendorAllPluginsOptions{
			CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			PluginFetcher:   mustPluginFetcher(t, remote),
			Platform:        "linux/amd64",
			VerifySignature: func(context.Context, string) (*LockPluginSignature, error) {
				return nil, nil
			},
		})
		require.ErrorContains(t, err, `resolved version "1.2.3" for pinned version "1.1.0"`)
	})
}

func TestVendorAllPlugins_AllFormsSameNameRoundTrip(t *testing.T) {
	t.Parallel()
	deps := []solution.PluginDependency{registryExecDep("^1.0.0"), builtExecDep(), shortExecDep()}
	local := newFakeLocal()
	remote := newFakeRemote()

	opts := VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		PluginFetcher:   mustPluginFetcher(t, local, remote),
		Platform:        "linux/amd64",
	}

	result, err := VendorAllPlugins(testContext(), deps, nil, opts)
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 3)

	lock := &LockFile{Plugins: result.ResolvedPlugins}
	for i, dep := range deps {
		found := lock.FindPluginByDep(dep)
		require.NotNil(t, found, "dep %d must find its own entry", i)
		assert.Same(t, &lock.Plugins[i], found)
	}
}

func TestVendorAllPlugins_EmptyPlatform(t *testing.T) {
	t.Parallel()
	remote, local := newFakeRemote(), newFakeLocal()
	deps := []solution.PluginDependency{registryExecDep("^1.0.0"), builtExecDep(), shortExecDep()}

	_, err := VendorAllPlugins(testContext(), deps, nil, VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		PluginFetcher:   mustPluginFetcher(t, local, remote),
	})
	require.ErrorContains(t, err, "requires a build platform")
	assert.Zero(t, remote.findCalls)
	assert.Zero(t, local.findCalls)
}

func TestVendorAllPlugins_BuildPlatformNotPublished(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result, err := VendorAllPlugins(testContext(), []solution.PluginDependency{tt.dep}, nil, VendorAllPluginsOptions{
				CatalogResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
				PluginFetcher:   mustPluginFetcher(t, newFakeLocal(), newFakeRemote()),
				Platform:        "windows/amd64",
			})
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), `no content digest resolved for build platform "windows/amd64"`)
		})
	}
}

func TestLockSourceFor(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			assert.Equal(t, tt.want, lockSourceFor(tt.dep))
		})
	}
}

func TestSelectorFor(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			assert.Equal(t, tt.want, selectorFor(tt.dep))
		})
	}
}

func BenchmarkVendorAllPlugins_ShortNameRemote(b *testing.B) {
	local := notFoundInCatalog(catalog.LocalCatalogName, "exec")
	official := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "exec", "1.2.3")
	pf := mustPluginFetcher(b, local, official)
	opts := VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
		PluginFetcher:   pf,
		Platform:        "linux/amd64",
	}
	deps := []solution.PluginDependency{shortExecDep()}
	ctx := b.Context()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := VendorAllPlugins(ctx, deps, nil, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVendorAllPlugins_ShortNameLockReplay(b *testing.B) {
	local := notFoundInCatalog(catalog.LocalCatalogName, "exec")
	official := newFakeRemoteSource("official", "ghcr.io/oakwood-commons", "exec", "1.2.3")
	pf := mustPluginFetcher(b, local, official)
	opts := VendorAllPluginsOptions{
		CatalogResolver: newTestAliasResolver("official", "oci://ghcr.io/oakwood-commons"),
		PluginFetcher:   pf,
		Platform:        "linux/amd64",
	}
	deps := []solution.PluginDependency{shortExecDep()}
	lock := &LockFile{Plugins: []LockPlugin{{
		Name:              "exec",
		Kind:              "provider",
		Version:           "1.2.3",
		Constraint:        "^1.0.0",
		Digest:            "sha256:aaa",
		ResolvedFrom:      "official",
		ResolvedCanonical: "ghcr.io/oakwood-commons",
	}}}
	ctx := b.Context()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := VendorAllPlugins(ctx, deps, lock, opts); err != nil {
			b.Fatal(err)
		}
	}
}
