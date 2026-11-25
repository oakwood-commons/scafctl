// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSolYAML(name, version string, subRefs ...string) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: %s\n  version: %s\n", name, version)
	if len(subRefs) == 0 {
		sb.WriteString("spec: {}\n")
		return []byte(sb.String())
	}
	sb.WriteString("spec:\n  resolvers:\n")
	for i, r := range subRefs {
		fmt.Fprintf(&sb, "    sub%d:\n      resolve:\n        with:\n          - provider: solution\n            inputs:\n              source: %q\n", i, r)
	}
	return []byte(sb.String())
}

func digestOf(b []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

func solutionWithSubRefs(refs ...string) *solution.Solution {
	resolvers := make(map[string]*resolver.Resolver, len(refs))
	for i, r := range refs {
		resolvers[fmt.Sprintf("r%d", i)] = &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "solution", Inputs: map[string]*spec.ValueRef{"source": {Literal: r}}},
				},
			},
		}
	}
	return &solution.Solution{Spec: solution.Spec{Resolvers: resolvers}}
}

func TestVendorUpdateHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1.2.3", ExtractVersionFromRef("my-solution@1.2.3"))
	assert.Equal(t, "latest", ExtractVersionFromRef("my-solution"))
	assert.Equal(t, "2.0.0", ExtractVersionFromRef("registry.example.com/solutions/my-solution@2.0.0"))

	long := "sha256:abcdefghijklmnopqrstuvwxyz"
	assert.Equal(t, long[:19]+"...", TruncateDigest(long))
	assert.Equal(t, "short", TruncateDigest("short"))

	assert.Equal(t, "exec", (PluginUpdateEntry{Name: "exec"}).DisplayName())
	assert.Equal(t, "custom (exec)", (PluginUpdateEntry{Name: "exec", Alias: "custom"}).DisplayName())

	assert.Nil(t, CheckPluginUpdates(nil))
	msgs := CheckPluginUpdates(&LockFile{
		Plugins: []LockPlugin{{Name: "my-plugin", Kind: "provider", Version: "1.0.0", Digest: long}},
	})
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "my-plugin")

	_, err := matchUpdateFilters(nil, nil, &LockFile{Dependencies: []LockDependency{{Ref: "sol-a@1.0.0"}}}, []string{"missing", "also-missing"})
	require.ErrorIs(t, err, ErrInvalidUpdateTarget)
	assert.Contains(t, err.Error(), "missing, also-missing")
}

func TestUpdateVendoredDependencies_SubSolutionsAndPruning(t *testing.T) {
	t.Parallel()

	childV100 := validSolYAML("child", "1.0.0")
	childV110 := validSolYAML("child", "1.1.0", "leaf@1.0.0")
	leafV100 := validSolYAML("leaf", "1.0.0")
	staleOld := validSolYAML("stale-dep", "0.9.0")

	fetcher := &mockCatalogFetcher{
		listings: map[string][]catalog.ArtifactInfo{
			"child": {
				{Reference: catalog.Reference{Name: "child", Version: semver.MustParse("1.0.0")}, Catalog: "acme"},
				{Reference: catalog.Reference{Name: "child", Version: semver.MustParse("1.1.0")}, Catalog: "acme"},
			},
		},
		solutions: map[string]mockFetchResult{
			"child@1.1.0": {content: childV110, info: catalog.ArtifactInfo{Reference: catalog.Reference{Name: "child", Version: semver.MustParse("1.1.0")}, Catalog: "acme"}},
			"leaf@1.0.0":  {content: leafV100, info: catalog.ArtifactInfo{Reference: catalog.Reference{Name: "leaf", Version: semver.MustParse("1.0.0")}, Catalog: "acme"}},
		},
	}

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".scafctl", "vendor"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".scafctl", "vendor", "child@1.0.0.yaml"), childV100, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".scafctl", "vendor", "stale-dep@0.9.0.yaml"), staleOld, 0o600))

	lockPath := filepath.Join(dir, DefaultLockFileName)
	existingLock := &LockFile{
		Version: LockFileVersion,
		Dependencies: []LockDependency{
			{Ref: "child@^1.0.0", ResolvedVersion: "1.0.0", Constraint: "^1.0.0", Digest: digestOf(childV100), ResolvedFrom: "acme", VendoredAt: ".scafctl/vendor/child@1.0.0.yaml"},
			{Ref: "stale-dep@0.9.0", ResolvedVersion: "0.9.0", Digest: digestOf(staleOld), ResolvedFrom: "acme", VendoredAt: ".scafctl/vendor/stale-dep@0.9.0.yaml"},
		},
	}
	require.NoError(t, WriteLockFile(lockPath, existingLock))

	sol := solutionWithSubRefs("child@^1.0.0")

	// 1. DryRun reports updated + added + pruned without mutating disk.
	dryRes, err := UpdateVendoredDependencies(t.Context(), sol, existingLock, VendorUpdateRunOptions{
		BundleRoot:      dir,
		LockPath:        lockPath,
		DryRun:          true,
		SolutionFetcher: fetcher,
	})
	require.NoError(t, err)
	assert.True(t, dryRes.DryRun)
	assert.Equal(t, 3, dryRes.UpdateCount)
	assert.NoFileExists(t, filepath.Join(dir, ".scafctl", "vendor", "child@1.1.0.yaml"))
	assert.FileExists(t, filepath.Join(dir, ".scafctl", "vendor", "stale-dep@0.9.0.yaml"))

	// 2. Full update writes new vendor files, deletes superseded/pruned vendor files, and updates solution.lock.
	res, err := UpdateVendoredDependencies(t.Context(), sol, existingLock, VendorUpdateRunOptions{
		BundleRoot:      dir,
		LockPath:        lockPath,
		SolutionFetcher: fetcher,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, res.UpdateCount)
	assert.FileExists(t, filepath.Join(dir, ".scafctl", "vendor", "child@1.1.0.yaml"))
	assert.FileExists(t, filepath.Join(dir, ".scafctl", "vendor", "leaf@1.0.0.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, ".scafctl", "vendor", "child@1.0.0.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, ".scafctl", "vendor", "stale-dep@0.9.0.yaml"))

	updatedLock, err := LoadLockFile(lockPath)
	require.NoError(t, err)
	require.Len(t, updatedLock.Dependencies, 2)
	assert.Equal(t, "1.1.0", updatedLock.Dependencies[0].ResolvedVersion)
	assert.Equal(t, "1.0.0", updatedLock.Dependencies[1].ResolvedVersion)

	// 3. Re-running when up to date reports UpdateCount == 0 and leaves solution.lock untouched.
	noOpRes, err := UpdateVendoredDependencies(t.Context(), sol, updatedLock, VendorUpdateRunOptions{
		BundleRoot:      dir,
		LockPath:        lockPath,
		SolutionFetcher: fetcher,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, noOpRes.UpdateCount)
}

func TestUpdateVendoredDependencies_SelectiveTransitiveAndAtomicFailure(t *testing.T) {
	t.Parallel()

	// Transitive selective update: parent@^1.0.0 is untargeted (replayed at 1.0.0),
	// while its transitive child sub@^1.0.0 IS targeted and upgrades 1.0.0 -> 1.1.0.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".scafctl", "vendor"), 0o700))

	parentV100 := validSolYAML("parent", "1.0.0", "sub@^1.0.0")
	subV100 := validSolYAML("sub", "1.0.0")
	subV110 := validSolYAML("sub", "1.1.0")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".scafctl", "vendor", "parent@1.0.0.yaml"), parentV100, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".scafctl", "vendor", "sub@1.0.0.yaml"), subV100, 0o600))

	lockPath := filepath.Join(dir, DefaultLockFileName)
	existingLock := &LockFile{
		Version: LockFileVersion,
		Dependencies: []LockDependency{
			{Ref: "parent@^1.0.0", ResolvedVersion: "1.0.0", Constraint: "^1.0.0", Digest: digestOf(parentV100), ResolvedFrom: "acme", VendoredAt: ".scafctl/vendor/parent@1.0.0.yaml"},
			{Ref: "sub@^1.0.0", ResolvedVersion: "1.0.0", Constraint: "^1.0.0", Digest: digestOf(subV100), ResolvedFrom: "acme", VendoredAt: ".scafctl/vendor/sub@1.0.0.yaml"},
		},
	}
	require.NoError(t, WriteLockFile(lockPath, existingLock))

	fetcher := &mockCatalogFetcher{
		listings: map[string][]catalog.ArtifactInfo{
			"parent": {
				{Reference: catalog.Reference{Name: "parent", Version: semver.MustParse("1.0.0")}, Catalog: "acme"},
				{Reference: catalog.Reference{Name: "parent", Version: semver.MustParse("1.2.0")}, Catalog: "acme"},
			},
			"sub": {
				{Reference: catalog.Reference{Name: "sub", Version: semver.MustParse("1.0.0")}, Catalog: "acme"},
				{Reference: catalog.Reference{Name: "sub", Version: semver.MustParse("1.1.0")}, Catalog: "acme"},
			},
		},
		solutions: map[string]mockFetchResult{
			"sub@1.1.0": {content: subV110, info: catalog.ArtifactInfo{Reference: catalog.Reference{Name: "sub", Version: semver.MustParse("1.1.0")}, Catalog: "acme"}},
		},
	}

	sol := solutionWithSubRefs("parent@^1.0.0")
	res, err := UpdateVendoredDependencies(t.Context(), sol, existingLock, VendorUpdateRunOptions{
		BundleRoot:      dir,
		LockPath:        lockPath,
		Dependencies:    []string{"sub"},
		SolutionFetcher: fetcher,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.UpdateCount)
	assert.FileExists(t, filepath.Join(dir, ".scafctl", "vendor", "parent@1.0.0.yaml"))
	assert.FileExists(t, filepath.Join(dir, ".scafctl", "vendor", "sub@1.1.0.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, ".scafctl", "vendor", "sub@1.0.0.yaml"))

	afterSelective, err := LoadLockFile(lockPath)
	require.NoError(t, err)
	require.Len(t, afterSelective.Dependencies, 2)
	assert.Equal(t, "1.0.0", afterSelective.Dependencies[0].ResolvedVersion, "untargeted parent stays at 1.0.0")
	assert.Equal(t, "1.1.0", afterSelective.Dependencies[1].ResolvedVersion, "targeted transitive sub upgrades to 1.1.0")

	// In-memory atomic failure on version conflict leaves existing vendor files and solution.lock untouched!
	conflictSol := solutionWithSubRefs("sub@1.0.0", "sub@2.0.0")
	_, err = UpdateVendoredDependencies(t.Context(), conflictSol, afterSelective, VendorUpdateRunOptions{
		BundleRoot: dir,
		LockPath:   lockPath,
		SolutionFetcher: &mockCatalogFetcher{
			solutions: map[string]mockFetchResult{
				"sub@1.0.0": {content: subV100, info: catalog.ArtifactInfo{Reference: catalog.Reference{Name: "sub", Version: semver.MustParse("1.0.0")}, Catalog: "acme"}},
				"sub@2.0.0": {content: validSolYAML("sub", "2.0.0"), info: catalog.ArtifactInfo{Reference: catalog.Reference{Name: "sub", Version: semver.MustParse("2.0.0")}, Catalog: "acme"}},
			},
		},
	})
	require.ErrorContains(t, err, "version conflict")
	unchangedLock, err := LoadLockFile(lockPath)
	require.NoError(t, err)
	assert.Equal(t, afterSelective, unchangedLock)
	assert.FileExists(t, filepath.Join(dir, ".scafctl", "vendor", "sub@1.1.0.yaml"))
}

func TestUpdateVendoredDependencies_PreReleaseAndLockOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, DefaultLockFileName)
	childRC := validSolYAML("child", "1.2.0-rc.1")
	fetcher := &mockCatalogFetcher{
		listings: map[string][]catalog.ArtifactInfo{
			"child": {
				{Reference: catalog.Reference{Name: "child", Version: semver.MustParse("1.0.0")}, Catalog: "acme"},
				{Reference: catalog.Reference{Name: "child", Version: semver.MustParse("1.2.0-rc.1")}, Catalog: "acme"},
			},
		},
		solutions: map[string]mockFetchResult{
			"child@1.2.0-rc.1": {content: childRC, info: catalog.ArtifactInfo{Reference: catalog.Reference{Name: "child", Version: semver.MustParse("1.2.0-rc.1")}, Catalog: "acme"}},
		},
	}

	sol := solutionWithSubRefs("child@^1.0.0")
	preCtx := catalog.WithIncludePreRelease(t.Context())
	res, err := UpdateVendoredDependencies(preCtx, sol, &LockFile{Version: LockFileVersion}, VendorUpdateRunOptions{
		BundleRoot:      dir,
		LockPath:        lockPath,
		LockOnly:        true,
		SolutionFetcher: fetcher,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.UpdateCount)
	assert.NoDirExists(t, filepath.Join(dir, ".scafctl", "vendor"), "--lock-only must not write vendor files")

	lockedPre, err := LoadLockFile(lockPath)
	require.NoError(t, err)
	require.Len(t, lockedPre.Dependencies, 1)
	assert.Equal(t, "1.2.0-rc.1", lockedPre.Dependencies[0].ResolvedVersion)

	// Subsequent ResolveDependencies without --pre-release materializes the pinned pre-release!
	replayRes, err := ResolveDependencies(t.Context(), solutionWithSubRefs("child@^1.0.0"), []CatalogRefEntry{{Ref: "child@^1.0.0"}}, VendorOptions{
		BundleRoot:     dir,
		ExistingLock:   lockedPre,
		CatalogFetcher: fetcher,
	})
	require.NoError(t, err)
	assert.Equal(t, childRC, replayRes.Fetched[".scafctl/vendor/child@1.2.0-rc.1.yaml"])
}

func TestUpdateVendoredDependencies_PluginsAndAdapters(t *testing.T) {
	t.Parallel()

	t.Run("resolves targeted plugins, preserves cross-OS digest and signature when unchanged, and prunes on full update", func(t *testing.T) {
		t.Parallel()
		src := newFakeRemoteSource("myorg", "ghcr.io/myorg", "exec", "1.2.3")
		pf, err := catalog.NewPluginFetcher(logr.Discard(), nil, src)
		require.NoError(t, err)
		aliasResolver := newTestAliasResolver("myorg", "oci://ghcr.io/myorg")

		linuxDigest := src.digestByPlat["linux/amd64"]
		darwinDigest := src.digestByPlat["darwin/arm64"]

		existingLock := &LockFile{
			Version: LockFileVersion,
			Plugins: []LockPlugin{
				{
					Name:              "exec",
					Kind:              "provider",
					Version:           "1.2.3",
					Constraint:        "^1.0.0",
					Digest:            linuxDigest,
					Digests:           map[string]string{"linux/amd64": linuxDigest, "darwin/arm64": darwinDigest},
					ResolvedFrom:      "myorg",
					ResolvedCanonical: "ghcr.io/myorg",
					Signature:         &LockPluginSignature{Issuer: "https://issuer.example", Identity: "ci", SignedAt: "2026-01-01T00:00:00Z"},
				},
				{
					Name:         "stale-plugin",
					Kind:         "provider",
					Version:      "0.1.0",
					Digest:       "sha256:stale",
					ResolvedFrom: "myorg",
				},
			},
		}

		sol := &solution.Solution{Bundle: solution.Bundle{Plugins: []solution.PluginDependency{shortExecDep()}}}
		dir := t.TempDir()
		lockPath := filepath.Join(dir, DefaultLockFileName)

		// Running on darwin/arm64 when exec@1.2.3 is unchanged preserves linuxDigest + Signature, while pruning stale-plugin!
		res, err := UpdateVendoredDependencies(t.Context(), sol, existingLock, VendorUpdateRunOptions{
			BundleRoot: dir,
			LockPath:   lockPath,
			PluginOptions: &VendorAllPluginsOptions{
				CatalogResolver: aliasResolver,
				PluginFetcher:   pf,
				Platform:        "darwin/arm64",
			},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, res.UpdateCount, "only stale-plugin should be pruned; exec stays unchanged across OS/arch")
		require.Len(t, res.PluginEntries, 2)
		assert.Equal(t, UpdateStatusUnchanged, res.PluginEntries[0].Status)
		assert.Equal(t, UpdateStatusPruned, res.PluginEntries[1].Status)

		savedLock, err := LoadLockFile(lockPath)
		require.NoError(t, err)
		require.Len(t, savedLock.Plugins, 1)
		assert.Equal(t, linuxDigest, savedLock.Plugins[0].Digest)
		require.NotNil(t, savedLock.Plugins[0].Signature)
		assert.NotEmpty(t, res.PluginMessages, "UpdateVendoredDependencies populates PluginMessages directly")

		// Selective --dependency stale-plugin prunes targeted orphan plugin even when updateAll is false.
		selOrphan, err := UpdateVendoredDependencies(t.Context(), sol, existingLock, VendorUpdateRunOptions{
			BundleRoot:   dir,
			LockPath:     lockPath,
			Dependencies: []string{"stale-plugin"},
			PluginOptions: &VendorAllPluginsOptions{
				CatalogResolver: aliasResolver,
				PluginFetcher:   pf,
				Platform:        "darwin/arm64",
			},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, selOrphan.UpdateCount)
		assert.Equal(t, UpdateStatusPruned, selOrphan.PluginEntries[1].Status)

		// Selective --dependency with aliased duplicate plugins + edited incompatible constraint on untargeted plugin:
		v130Entry := fakeVersionEntry{
			info:         catalog.ArtifactInfo{Reference: catalog.Reference{Kind: catalog.ArtifactKindProvider, Name: "exec", Version: semver.MustParse("1.3.0")}, Catalog: "myorg", Canonical: "ghcr.io/myorg"},
			platforms:    []string{"linux/amd64"},
			digestByPlat: map[string]string{"linux/amd64": "sha256:v130"},
		}
		v210Entry := fakeVersionEntry{
			info:         catalog.ArtifactInfo{Reference: catalog.Reference{Kind: catalog.ArtifactKindProvider, Name: "exec", Version: semver.MustParse("2.1.0")}, Catalog: "myorg", Canonical: "ghcr.io/myorg"},
			platforms:    []string{"linux/amd64"},
			digestByPlat: map[string]string{"linux/amd64": "sha256:v210"},
		}
		multiSrc := newFakeRemoteSource("myorg", "ghcr.io/myorg", "exec", "2.1.0")
		multiSrc.byVersion = map[string]fakeVersionEntry{
			"^1.0.0": v130Entry,
			"1.3.0":  v130Entry,
			"^2.0.0": v210Entry,
			"2.1.0":  v210Entry,
		}
		multiPF, err := catalog.NewPluginFetcher(logr.Discard(), nil, multiSrc)
		require.NoError(t, err)
		alias1 := solution.PluginDependency{Name: "exec-a", Kind: solution.PluginKindProvider, Version: "^1.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}}
		alias2 := solution.PluginDependency{Name: "exec-b", Kind: solution.PluginKindProvider, Version: "^2.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}}
		alias3 := solution.PluginDependency{Name: "exec-c", Kind: solution.PluginKindProvider, Version: "^3.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}}
		multiLock := &LockFile{
			Version: LockFileVersion,
			Plugins: []LockPlugin{
				{Name: "exec", Kind: "provider", Version: "1.2.3", Constraint: "^1.0.0", Digest: "sha256:v123", Digests: map[string]string{"linux/amd64": "sha256:v123"}, ResolvedFrom: "myorg", ResolvedCanonical: "ghcr.io/myorg", Source: &LockPluginSource{Registry: "ghcr.io/myorg"}},
				{Name: "exec", Kind: "provider", Version: "1.5.0", Constraint: "^1.5.0", Digest: "sha256:v150", Digests: map[string]string{"linux/amd64": "sha256:v150"}, ResolvedFrom: "myorg", ResolvedCanonical: "ghcr.io/myorg", Source: &LockPluginSource{Registry: "ghcr.io/myorg"}},
				{Name: "exec", Kind: "provider", Version: "3.0.1", Constraint: "^3.0.0", Digest: "sha256:v301", Digests: map[string]string{"linux/amd64": "sha256:v301"}, ResolvedFrom: "myorg", ResolvedCanonical: "ghcr.io/myorg", Source: &LockPluginSource{Registry: "ghcr.io/myorg"}},
			},
		}
		multiRes, err := UpdateVendoredDependencies(t.Context(), &solution.Solution{Bundle: solution.Bundle{Plugins: []solution.PluginDependency{alias1, alias2, alias3}}}, multiLock, VendorUpdateRunOptions{
			BundleRoot:   dir,
			Dependencies: []string{"exec-a"},
			PluginOptions: &VendorAllPluginsOptions{
				CatalogResolver: aliasResolver,
				PluginFetcher:   multiPF,
				Platform:        "linux/amd64",
			},
		})
		require.NoError(t, err)
		require.Len(t, multiRes.PluginEntries, 3)
		assert.Equal(t, "1.3.0", multiRes.PluginEntries[0].LatestVersion)
		assert.Equal(t, "2.1.0", multiRes.PluginEntries[1].LatestVersion, "untargeted plugin whose locked 1.5.0 violates ^2.0.0 must re-resolve to 2.1.0")
		assert.Equal(t, "3.0.1", multiRes.PluginEntries[2].LatestVersion, "untargeted plugin whose locked 3.0.1 satisfies ^3.0.0 replays unchanged")
		assert.Equal(t, UpdateStatusUnchanged, multiRes.PluginEntries[2].Status)
	})

	t.Run("LoadComposedSolution handles # compose: comments and rejects malicious vendoredAt", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		commentSol := "# compose: note in comment\napiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: plain\n  version: 1.0.0\nbundle:\n  plugins:\n    - name: exec\n      kind: provider\n      version: ^1.0.0\nspec: {}\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "plain.yaml"), []byte(commentSol), 0o600))
		loaded, _, err := LoadComposedSolution(filepath.Join(dir, "plain.yaml"))
		require.NoError(t, err)
		assert.Len(t, loaded.Bundle.Plugins, 1)

		// Malicious vendoredAt outside .scafctl/vendor is rejected in both replay and materialization.
		badLock := &LockFile{
			Version: LockFileVersion,
			Dependencies: []LockDependency{
				{Ref: "child@^1.0.0", ResolvedVersion: "1.0.0", Constraint: "^1.0.0", Digest: "sha256:x", VendoredAt: "solution.yaml"},
			},
		}
		_, err = ResolveDependencies(t.Context(), solutionWithSubRefs("child@^1.0.0"), []CatalogRefEntry{{Ref: "child@^1.0.0"}}, VendorOptions{
			BundleRoot:     dir,
			ExistingLock:   badLock,
			CatalogFetcher: &mockCatalogFetcher{},
		})
		require.ErrorContains(t, err, "invalid vendoredAt path")
	})

	t.Run("LoadComposedSolution and CatalogChainAdapter auto-cache", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		rootYAML := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: comp\n  version: 1.0.0\ncompose:\n  - part.yaml\nspec: {}\n"
		partYAML := "spec:\n  resolvers:\n    sub:\n      resolve:\n        with:\n          - provider: solution\n            inputs:\n              source: child@1.0.0\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "solution.yaml"), []byte(rootYAML), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "part.yaml"), []byte(partYAML), 0o600))

		composed, bundleRoot, err := LoadComposedSolution(filepath.Join(dir, "solution.yaml"))
		require.NoError(t, err)
		assert.Equal(t, dir, bundleRoot)
		assert.Contains(t, composed.Spec.Resolvers, "sub")

		localCat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)
		ref := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "shared", Version: semver.MustParse("1.0.0")}
		_, err = localCat.Store(t.Context(), ref, validSolYAML("shared", "1.0.0"), nil, nil, false)
		require.NoError(t, err)

		chainAdapter := &CatalogChainAdapter{Catalog: localCat}
		gotBytes, _, err := chainAdapter.FetchSolution(t.Context(), "shared@1.0.0")
		require.NoError(t, err)
		assert.Contains(t, string(gotBytes), "version: 1.0.0")
		listed, err := chainAdapter.ListSolutions(t.Context(), "shared")
		require.NoError(t, err)
		assert.Len(t, listed, 1)

		var nilChain *CatalogChainAdapter
		_, _, err = nilChain.FetchSolution(t.Context(), "shared@1.0.0")
		require.Error(t, err)
		_, err = nilChain.ListSolutions(t.Context(), "shared")
		require.Error(t, err)
	})
}
