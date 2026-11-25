// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

const (
	pipelineBundledDoc = sourceDoc + "bundle:\n  include:\n    - templates/**\n"
	pipelineTemplate   = "templates/a.txt"
)

// pipelineFixture is a solution directory, a local catalog, and a build
// cache directory shared by the Packagers it creates.
type pipelineFixture struct {
	dir      string
	cat      *catalog.LocalCatalog
	cacheDir string
}

func newPipelineFixture(t *testing.T, files map[string]string) *pipelineFixture {
	t.Helper()
	cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	f := &pipelineFixture{dir: t.TempDir(), cat: cat, cacheDir: t.TempDir()}
	for rel, content := range files {
		writeSourceFile(t, f.dir, rel, content)
	}
	return f
}

// packager returns a Packager for the fixture's solution.yaml, as demo@1.0.0.
func (f *pipelineFixture) packager(t *testing.T, opts ...SourceOption) *Packager {
	t.Helper()
	src, err := LoadSolutionSource(t.Context(), filepath.Join(f.dir, "solution.yaml"), "", nil, opts...)
	require.NoError(t, err)
	p, err := NewPackager(f.cat)
	require.NoError(t, err)
	return p.Solution(src).
		As(BuiltSolutionRef("demo", semver.MustParse("1.0.0"))).
		Cache(CacheOptions{Dir: f.cacheDir})
}

// stored returns the stored document and bundle of demo@1.0.0; found is
// false when nothing was stored.
func (f *pipelineFixture) stored(t *testing.T) (doc, bundle []byte, found bool) {
	t.Helper()
	info, err := ResolveBuiltSolution(t.Context(), f.cat, "demo", semver.MustParse("1.0.0"))
	if catalog.IsNotFound(err) {
		return nil, nil, false
	}
	require.NoError(t, err)
	doc, bundle, _, err = f.cat.FetchWithBundle(t.Context(), info.Reference)
	require.NoError(t, err)
	return doc, bundle, true
}

func TestPackager_Do_Stores(t *testing.T) {
	t.Parallel()

	bundled := map[string]string{"solution.yaml": pipelineBundledDoc, pipelineTemplate: "hello"}

	tests := []struct {
		name      string
		files     map[string]string
		opts      []SourceOption
		configure func(p *Packager)
		check     func(t *testing.T, res *PackageResult, doc, bundle []byte)
	}{
		{
			name:  "no bundle stores the document as written",
			files: map[string]string{"solution.yaml": sourceDoc},
			opts:  []SourceOption{WithNoBundle(true)},
			check: func(t *testing.T, res *PackageResult, doc, bundle []byte) {
				assert.Nil(t, res.Build)
				assert.Equal(t, sourceDoc, string(doc))
				assert.Empty(t, bundle)
				assert.NotNil(t, res.Verify)
				assert.False(t, res.Stored.CacheWritten)
			},
		},
		{
			name:  "bundled tar",
			files: bundled,
			check: func(t *testing.T, res *PackageResult, _, bundle []byte) {
				require.NotNil(t, res.Build)
				assert.NotEmpty(t, res.Build.TarData)
				assert.NotEmpty(t, bundle)
				assert.Equal(t, 1, res.Build.InputFileCount)
				assert.NotEmpty(t, res.Build.BuildFingerprint)
				assert.True(t, res.Stored.CacheWritten)
				assert.Contains(t, res.Messages[len(res.Messages)-1], "Bundled 1 file(s)")
				require.NotNil(t, res.Verify)
				assert.True(t, res.Verify.Passed())
			},
		},
		{
			name:      "bundled dedup",
			files:     bundled,
			configure: func(p *Packager) { p.Archive(ArchiveOptions{Limits: p.archive.Limits, Dedupe: true}) },
			check: func(t *testing.T, res *PackageResult, _, bundle []byte) {
				require.NotNil(t, res.Build)
				assert.NotNil(t, res.Build.Dedup)
				assert.Nil(t, res.Build.TarData)
				assert.NotEmpty(t, bundle)
				assert.Contains(t, res.Messages[len(res.Messages)-1], "deduplicated: 1 layer(s)")
			},
		},
		{
			name:  "composed solution is serialized",
			files: map[string]string{"solution.yaml": sourceComposeDoc, "part.yaml": sourcePartDoc},
			check: func(t *testing.T, res *PackageResult, doc, _ []byte) {
				assert.Contains(t, res.Messages, "Composed 2 file(s) into solution")
				assert.Contains(t, string(doc), "app:")
				assert.NotContains(t, string(doc), "part.yaml")
			},
		},
		{
			name:      "verify skipped writes no cache entry",
			files:     bundled,
			configure: func(p *Packager) { p.Verify(VerifyOptions{Skip: true}) },
			check: func(t *testing.T, res *PackageResult, _, _ []byte) {
				assert.Nil(t, res.Verify)
				assert.False(t, res.Stored.CacheWritten)
			},
		},
		{
			name:      "preflight passes",
			files:     map[string]string{"solution.yaml": sourceDoc},
			configure: func(p *Packager) { p.Preflight(PreflightOptions{SkipLint: true, SkipTests: true}) },
			check: func(t *testing.T, res *PackageResult, _, _ []byte) {
				require.NotNil(t, res.Preflight)
				assert.False(t, res.Preflight.Blocked)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPipelineFixture(t, tt.files)
			p := f.packager(t, tt.opts...)
			if tt.configure != nil {
				tt.configure(p)
			}

			res, err := p.Do(t.Context())
			require.NoError(t, err)
			require.NotNil(t, res.Stored)
			assert.True(t, res.Stored.Info.Reference.Built)
			assert.False(t, res.DryRun)
			assert.False(t, res.CacheHit())

			doc, bundle, found := f.stored(t)
			require.True(t, found)
			tt.check(t, res, doc, bundle)
		})
	}
}

func TestPackager_Do_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newPipelineFixture(t, map[string]string{"solution.yaml": pipelineBundledDoc, pipelineTemplate: "hello"})

	res, err := f.packager(t).
		Preflight(PreflightOptions{}).
		DryRun(true).
		Do(t.Context())
	require.NoError(t, err)

	assert.True(t, res.DryRun)
	assert.Nil(t, res.Preflight, "preflight does not run on a dry run")
	assert.Nil(t, res.Stored)
	require.NotNil(t, res.Build)
	assert.Len(t, res.Build.Discovery.LocalFiles, 1)
	_, _, found := f.stored(t)
	assert.False(t, found)
	assert.NoFileExists(t, filepath.Join(f.dir, bundler.DefaultLockFileName))
}

func TestPackager_Do_ComposedOut(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		load func(sol *solution.Solution, data []byte) error
	}{
		{name: "yaml", file: "composed.yaml", load: (*solution.Solution).FromYAML},
		{name: "json", file: "composed.JSON", load: (*solution.Solution).FromJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPipelineFixture(t, map[string]string{"solution.yaml": sourceComposeDoc, "part.yaml": sourcePartDoc})
			out := filepath.Join(t.TempDir(), tt.file)

			res, err := f.packager(t).ComposedOut(out).DryRun(true).Do(t.Context())
			require.NoError(t, err)
			assert.Equal(t, out, res.ComposedOut)

			data, err := os.ReadFile(out)
			require.NoError(t, err)
			var sol solution.Solution
			require.NoError(t, tt.load(&sol, data))
			assert.Contains(t, sol.Spec.Resolvers, "app")
		})
	}
}

func TestPackager_Do_ComposedOutWriteFails(t *testing.T) {
	t.Parallel()
	f := newPipelineFixture(t, map[string]string{"solution.yaml": sourceDoc})
	out := filepath.Join(t.TempDir(), "missing-dir", "composed.yaml")

	res, err := f.packager(t).ComposedOut(out).Do(t.Context())
	require.ErrorContains(t, err, "writing composed solution")
	assert.Empty(t, res.ComposedOut)
	_, _, found := f.stored(t)
	assert.False(t, found)
}

func TestPackager_Do_BuildCacheHit(t *testing.T) {
	t.Parallel()
	f := newPipelineFixture(t, map[string]string{"solution.yaml": pipelineBundledDoc, pipelineTemplate: "hello"})

	first, err := f.packager(t).Do(t.Context())
	require.NoError(t, err)
	require.True(t, first.Stored.CacheWritten)

	second, err := f.packager(t).Do(t.Context())
	require.NoError(t, err)
	assert.True(t, second.CacheHit())
	assert.Nil(t, second.Stored)
	assert.Nil(t, second.Verify)
	assert.Equal(t, first.Stored.Info.Digest, second.Build.CacheEntry.ArtifactDigest)

	writeSourceFile(t, f.dir, pipelineTemplate, "changed")
	third, err := f.packager(t).Store(true).Do(t.Context())
	require.NoError(t, err)
	assert.False(t, third.CacheHit(), "a changed input misses the cache")
	assert.NotNil(t, third.Stored)
}

func TestPackager_Do_BuildCacheMissesOnProvenanceOnlyChange(t *testing.T) {
	t.Parallel()
	f := newPipelineFixture(t, map[string]string{"solution.yaml": pipelineBundledDoc, pipelineTemplate: "hello"})
	id := &Identity{Ref: BuiltSolutionRef("demo", semver.MustParse("1.0.0"))}

	build := func(t *testing.T, git GitMetadata) *PackageResult {
		t.Helper()
		p := f.packager(t).Store(true)
		_, err := p.src.Stamp(id, git, StampOptions{})
		require.NoError(t, err)
		res, err := p.Do(t.Context())
		require.NoError(t, err)
		return res
	}
	storedAnnotations := func(t *testing.T) map[string]string {
		t.Helper()
		info, err := ResolveBuiltSolution(t.Context(), f.cat, "demo", semver.MustParse("1.0.0"))
		require.NoError(t, err)
		return info.Annotations
	}

	first := build(t, GitMetadata{Commit: "abc1234", Dirty: true})
	require.True(t, first.Stored.CacheWritten)
	assert.Equal(t, buildDirtyValue, storedAnnotations(t)[catalog.AnnotationBuildDirty])

	same := build(t, GitMetadata{Commit: "abc1234", Dirty: true})
	assert.True(t, same.CacheHit(), "identical provenance hits the cache")

	clean := build(t, GitMetadata{Commit: "abc1234"})
	assert.False(t, clean.CacheHit(), "a cleared dirty flag misses the cache")
	assert.NotContains(t, storedAnnotations(t), catalog.AnnotationBuildDirty)

	newCommit := build(t, GitMetadata{Commit: "def5678"})
	assert.False(t, newCommit.CacheHit(), "a new commit misses the cache")
	assert.Equal(t, "def5678", storedAnnotations(t)[catalog.AnnotationBuildCommit])

	noGit := build(t, GitMetadata{})
	assert.False(t, noGit.CacheHit(), "dropping git metadata misses the cache")
	assert.NotContains(t, storedAnnotations(t), catalog.AnnotationBuildCommit)
}

func TestPackager_CachedArtifactExists(t *testing.T) {
	t.Parallel()
	f := newPipelineFixture(t, map[string]string{"solution.yaml": sourceDoc})
	res, err := f.packager(t).Do(t.Context())
	require.NoError(t, err)
	digest := res.Stored.Info.Digest

	tests := []struct {
		name  string
		entry bundler.BuildCacheEntry
		want  bool
	}{
		{name: "built copy with the same digest", entry: bundler.BuildCacheEntry{ArtifactName: "demo", ArtifactVersion: "1.0.0", ArtifactDigest: digest}, want: true},
		{name: "rebuilt since", entry: bundler.BuildCacheEntry{ArtifactName: "demo", ArtifactVersion: "1.0.0", ArtifactDigest: "sha256:other"}},
		{name: "deleted since", entry: bundler.BuildCacheEntry{ArtifactName: "demo", ArtifactVersion: "2.0.0", ArtifactDigest: digest}},
		{name: "invalid version", entry: bundler.BuildCacheEntry{ArtifactName: "demo", ArtifactVersion: "not-a-version", ArtifactDigest: digest}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := f.packager(t)
			assert.Equal(t, tt.want, p.cachedArtifactExists(t.Context(), &tt.entry))
		})
	}
}

func TestPackager_Do_ExistingVersion(t *testing.T) {
	t.Parallel()
	f := newPipelineFixture(t, map[string]string{"solution.yaml": sourceDoc})
	noCache := CacheOptions{Skip: true}

	_, err := f.packager(t).Cache(noCache).Do(t.Context())
	require.NoError(t, err)

	_, err = f.packager(t).Cache(noCache).Do(t.Context())
	require.ErrorIs(t, err, ErrStoreFailed)
	assert.True(t, catalog.IsExists(err), "got %v", err)

	res, err := f.packager(t).Cache(noCache).Store(true).Do(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, res.Stored)
}

func TestPackager_Do_VerifyFailureStoresNothing(t *testing.T) {
	t.Parallel()
	doc := pipelineBundledDoc + "    - unmatched/**\n"
	f := newPipelineFixture(t, map[string]string{"solution.yaml": doc, pipelineTemplate: "hello"})

	res, err := f.packager(t).Verify(VerifyOptions{Strict: true}).Do(t.Context())
	require.ErrorIs(t, err, ErrVerifyFailed)
	assert.EqualError(t, err, "built bundle has 1 warning(s) (strict)")
	require.NotNil(t, res.Verify)
	assert.Nil(t, res.Stored)

	_, _, found := f.stored(t)
	assert.False(t, found, "a bundle that fails verification is never stored")
	_, hit := bundler.CheckBuildCache(f.cacheDir, res.Build.BuildFingerprint)
	assert.False(t, hit)
}

func TestPackager_Do_PreflightBlocked(t *testing.T) {
	t.Parallel()
	f := newPipelineFixture(t, map[string]string{"solution.yaml": sourceDoc})
	p := f.packager(t).Preflight(PreflightOptions{SkipTests: true})
	p.src.Solution.Metadata = solution.Metadata{}

	res, err := p.Do(t.Context())
	require.ErrorIs(t, err, ErrPreflightBlocked)
	require.NotNil(t, res.Preflight)
	assert.True(t, res.Preflight.Blocked)
	assert.Nil(t, res.Stored)
}

func TestPackager_Do_VendorsCatalogDependency(t *testing.T) {
	t.Parallel()

	const doc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: demo
  version: 1.0.0
spec:
  workflow:
    actions:
      deploy:
        provider: solution
        inputs:
          source: dep@1.0.0
`
	const depDoc = "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: dep\n  version: 1.0.0\nspec:\n  resolvers: {}\n"

	tests := []struct {
		name   string
		dryRun bool
	}{
		{name: "build writes vendored files and lock"},
		{name: "dry run writes nothing", dryRun: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPipelineFixture(t, map[string]string{"solution.yaml": doc})
			depRef := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "dep", Version: semver.MustParse("1.0.0")}
			_, err := f.cat.Store(t.Context(), depRef, []byte(depDoc), nil, nil, false)
			require.NoError(t, err)

			res, err := f.packager(t).DryRun(tt.dryRun).Do(t.Context())
			require.NoError(t, err)
			assert.Contains(t, res.Messages, "Vendored 1 catalog dependency(ies)")
			require.NotNil(t, res.Build)
			require.NotNil(t, res.Build.LockData)

			lockPath := filepath.Join(f.dir, bundler.DefaultLockFileName)
			if tt.dryRun {
				assert.NoFileExists(t, lockPath)
				assert.NoDirExists(t, filepath.Join(f.dir, bundler.VendorDirName))
				return
			}
			lock, err := bundler.LoadLockFile(lockPath)
			require.NoError(t, err)
			require.Len(t, lock.Dependencies, 1)
			assert.Equal(t, "dep@1.0.0", lock.Dependencies[0].Ref)
			assert.DirExists(t, filepath.Join(f.dir, bundler.VendorDirName))
			require.NotNil(t, res.Verify)
			assert.True(t, res.Verify.Passed(), "%+v", res.Verify)
		})
	}
}

func TestPackager_Do_PluginErrors(t *testing.T) {
	t.Parallel()

	const missingProvider = "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: demo\n  version: 1.0.0\nspec:\n  resolvers:\n    x:\n      resolve:\n        with:\n          - provider: not-a-provider\n"

	tests := []struct {
		name    string
		doc     string
		wantErr string
	}{
		{
			name:    "undeclared provider",
			doc:     missingProvider,
			wantErr: "not-a-provider",
		},
		{
			name:    "unresolvable plugin",
			doc:     sourceDoc + "bundle:\n  plugins:\n    - name: not-in-catalog\n      kind: provider\n      version: 1.0.0\n",
			wantErr: "failed to vendor plugin dependencies",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPipelineFixture(t, map[string]string{"solution.yaml": tt.doc})

			_, err := f.packager(t).Do(t.Context())
			require.ErrorContains(t, err, tt.wantErr)
			_, _, found := f.stored(t)
			assert.False(t, found)
		})
	}
}

func TestPackager_Do_BuiltPluginSignatureModes(t *testing.T) {
	t.Parallel()

	const doc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: demo
  version: 1.0.0
bundle:
  plugins:
    - name: local-prov
      kind: provider
      version: ^1.0.0
spec:
  resolvers:
    x:
      resolve:
        with:
          - provider: local-prov
`
	f := newPipelineFixture(t, map[string]string{"solution.yaml": doc})
	_, err := f.cat.StoreMultiPlatform(t.Context(), catalog.Reference{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "local-prov",
		Version: semver.MustParse("1.0.0"),
		Built:   true,
	}, []catalog.PlatformBinary{
		{Platform: runtime.GOOS + "/" + runtime.GOARCH, Data: []byte("local-bin")},
	}, nil, false)
	require.NoError(t, err)

	// Warn mode logs and succeeds on empty ImageRef (local build).
	warnCtx := plugin.WithSignaturePolicy(t.Context(), &plugin.SignaturePolicy{
		Mode:              plugin.SignatureModeWarn,
		TrustedIssuers:    []string{"https://issuer.example.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	})
	res, err := f.packager(t).DryRun(true).Do(warnCtx)
	require.NoError(t, err)
	assert.Contains(t, res.Messages, "Pinned 1 plugin(s) in lock file")

	// Enforce mode fails closed with ErrSignatureUnverifiable on empty ImageRef.
	enforceCtx := plugin.WithSignaturePolicy(t.Context(), &plugin.SignaturePolicy{
		Mode:              plugin.SignatureModeEnforce,
		TrustedIssuers:    []string{"https://issuer.example.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	})
	_, err = f.packager(t).DryRun(true).Do(enforceCtx)
	require.ErrorIs(t, err, plugin.ErrSignatureUnverifiable)
}

func TestPackager_Do_BuiltPluginPreflightWithoutArtifact(t *testing.T) {
	t.Parallel()

	const doc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: built-demo
  version: 1.0.0
  description: Built plugin without source.artifact passes schema lint
bundle:
  plugins:
    - name: my-local-provider
      kind: provider
      version: "^0.1.0"
      source:
        built: true
spec:
  resolvers:
    out:
      description: test output
      resolve:
        with:
          - provider: my-local-provider
`
	f := newPipelineFixture(t, map[string]string{"solution.yaml": doc})
	_, err := f.cat.StoreMultiPlatform(t.Context(), catalog.Reference{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "my-local-provider",
		Version: semver.MustParse("0.1.0"),
		Built:   true,
	}, []catalog.PlatformBinary{
		{Platform: runtime.GOOS + "/" + runtime.GOARCH, Data: []byte("local-bin")},
	}, nil, false)
	require.NoError(t, err)

	res, err := f.packager(t).
		As(BuiltSolutionRef("built-demo", semver.MustParse("1.0.0"))).
		Preflight(PreflightOptions{SkipTests: true, Registry: provider.NewRegistry()}).
		Do(t.Context())
	require.NoError(t, err)
	require.NotNil(t, res.Preflight)
	assert.False(t, res.Preflight.Blocked)
	assert.Contains(t, res.Messages, "Pinned 1 plugin(s) in lock file")
}

func TestVerifyOptions_Check(t *testing.T) {
	t.Parallel()

	failing := &bundler.VerifyResult{Errors: []bundler.VerifyError{{Path: "a", Reason: "not found in bundle"}}}
	warning := &bundler.VerifyResult{Warnings: []string{"w"}}

	tests := []struct {
		name    string
		opts    VerifyOptions
		vr      *bundler.VerifyResult
		wantErr string
	}{
		{name: "passes", vr: &bundler.VerifyResult{}},
		{name: "warnings pass", vr: warning},
		{name: "errors fail", vr: failing, wantErr: "built bundle is incomplete: 1 error(s)"},
		{name: "strict fails on warnings", opts: VerifyOptions{Strict: true}, vr: warning, wantErr: "built bundle has 1 warning(s) (strict)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.opts.check(tt.vr)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrVerifyFailed)
			assert.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestNewLock(t *testing.T) {
	t.Parallel()

	plugins := []bundler.LockPlugin{{Name: "echo"}}
	depLock := func() *bundler.VendorResult {
		return &bundler.VendorResult{Lock: &bundler.LockFile{Version: bundler.LockFileVersion, Dependencies: []bundler.LockDependency{{Ref: "dep@1.0.0"}}}}
	}

	tests := []struct {
		name        string
		deps        *bundler.VendorResult
		plugins     []bundler.LockPlugin
		wantNil     bool
		wantDeps    int
		wantPlugins int
	}{
		{name: "nothing pinned", wantNil: true},
		{name: "plugins only", plugins: plugins, wantPlugins: 1},
		{name: "dependencies only", deps: depLock(), wantDeps: 1},
		{name: "dependencies and plugins", deps: depLock(), plugins: plugins, wantDeps: 1, wantPlugins: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lock := newLock(tt.deps, tt.plugins)
			if tt.wantNil {
				assert.Nil(t, lock)
				return
			}
			require.NotNil(t, lock)
			assert.Equal(t, bundler.LockFileVersion, lock.Version)
			assert.Len(t, lock.Dependencies, tt.wantDeps)
			assert.Len(t, lock.Plugins, tt.wantPlugins)
		})
	}
}

func TestPackager_WriteLock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		root    func(t *testing.T) string
		wantErr bool
	}{
		{name: "writes next to the solution", root: func(t *testing.T) string { return t.TempDir() }},
		{name: "unwritable bundle root", root: func(t *testing.T) string {
			file := filepath.Join(t.TempDir(), "file")
			require.NoError(t, os.WriteFile(file, nil, 0o600))
			return file
		}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := tt.root(t)
			p := &Packager{src: &SolutionSource{BundleRoot: root}}
			st := &pipelineState{lock: &bundler.LockFile{Version: bundler.LockFileVersion, Plugins: []bundler.LockPlugin{{Name: "echo"}}}}

			_, err := p.writeLock(t.Context(), st)
			if tt.wantErr {
				require.ErrorContains(t, err, "writing lock file")
				return
			}
			require.NoError(t, err)
			lock, err := bundler.LoadLockFile(filepath.Join(root, bundler.DefaultLockFileName))
			require.NoError(t, err)
			assert.Len(t, lock.Plugins, 1)
		})
	}
}

func TestPackager_ReadFiles(t *testing.T) {
	t.Parallel()

	const vendored = ".scafctl/vendor/dep@1.0.0.yaml"

	tests := []struct {
		name      string
		files     []bundler.FileEntry
		deps      *bundler.VendorResult
		maxSize   int64
		wantPaths []string
		wantErr   string
	}{
		{name: "reads bundled files", files: []bundler.FileEntry{{RelPath: pipelineTemplate}}, maxSize: bundler.DefaultMaxBundleSize, wantPaths: []string{pipelineTemplate}},
		{
			name:      "vendored files come from memory",
			files:     []bundler.FileEntry{{RelPath: pipelineTemplate}, {RelPath: vendored}},
			deps:      &bundler.VendorResult{Fetched: map[string][]byte{vendored: []byte("dep")}},
			maxSize:   bundler.DefaultMaxBundleSize,
			wantPaths: []string{pipelineTemplate, vendored},
		},
		{name: "missing file", files: []bundler.FileEntry{{RelPath: "missing.txt"}}, maxSize: bundler.DefaultMaxBundleSize, wantErr: "failed to read bundle files: failed to read missing.txt"},
		{name: "exceeds max size", files: []bundler.FileEntry{{RelPath: pipelineTemplate}}, maxSize: 1, wantErr: "bundle exceeds maximum size limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeSourceFile(t, root, pipelineTemplate, "hello")
			p := &Packager{src: &SolutionSource{BundleRoot: root}, archive: ArchiveOptions{Limits: BundleLimits{MaxSize: tt.maxSize}}}
			st := &pipelineState{discovery: &bundler.DiscoveryResult{LocalFiles: tt.files}, deps: tt.deps}

			_, err := p.readFiles(t.Context(), st)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			paths := make([]string, 0, len(st.files.Files))
			for _, f := range st.files.Files {
				paths = append(paths, f.RelPath)
			}
			assert.Equal(t, tt.wantPaths, paths)
		})
	}
}

func TestLockDigest(t *testing.T) {
	t.Parallel()
	assert.Empty(t, lockDigest(nil))
	assert.Equal(t, "sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae", lockDigest([]byte("foo")))
}

func TestPackager_ResolvePlugins_FallbackWarningAndStrictMode(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	unreachSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(unreachSrv.Close)
	host := strings.TrimPrefix(unreachSrv.URL, "http://")

	appCfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{Name: "broken-corp", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/corp", Insecure: true},
			{Name: "second", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/second", Insecure: true},
		},
	}

	solWithPlugin := sourceDoc + "bundle:\n  plugins:\n    - name: exec\n      kind: provider\n      version: ^1.0.0\n"
	f := newPipelineFixture(t, map[string]string{"solution.yaml": solWithPlugin})

	var stdout, stderr bytes.Buffer
	ios := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	w := writer.New(ios, settings.NewCliParams())
	ctx := config.WithConfig(writer.WithWriter(t.Context(), w), appCfg)

	// Default (best-effort) emits fallback warning to stderr.
	p := f.packager(t)
	_, err := p.resolvePlugins(ctx, &pipelineState{})
	require.Error(t, err)
	assert.Empty(t, stdout.String(), "fallback warning must not write to stdout")
	assert.Contains(t, stderr.String(), `catalog "broken-corp" unavailable, trying next catalog...`)

	// Strict verify mode halts immediately without fallback warning.
	stderr.Reset()
	strictP := f.packager(t).Verify(VerifyOptions{Strict: true})
	_, err = strictP.resolvePlugins(ctx, &pipelineState{})
	require.Error(t, err)
	assert.Empty(t, stderr.String(), "strict verify mode must halt without fallback warnings")
}

func BenchmarkPackager_Do_DryRun(b *testing.B) {
	dir := b.TempDir()
	writeSourceFile(b, dir, "solution.yaml", pipelineBundledDoc)
	writeSourceFile(b, dir, pipelineTemplate, "hello")
	cat, err := catalog.NewLocalCatalogAt(b.TempDir(), logr.Discard())
	require.NoError(b, err)
	src, err := LoadSolutionSource(b.Context(), filepath.Join(dir, "solution.yaml"), "", nil)
	require.NoError(b, err)
	p, err := NewPackager(cat)
	require.NoError(b, err)
	p.Solution(src).As(BuiltSolutionRef("demo", semver.MustParse("1.0.0"))).DryRun(true)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := p.Do(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPackager_Do_MaterializesLockOnlyRemoteDependency(t *testing.T) {
	t.Parallel()

	childV110 := []byte("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: remote-dep\n  version: 1.1.0\nspec:\n  resolvers: {}\n")
	sum := sha256.Sum256(childV110)
	childDigest := "sha256:" + hex.EncodeToString(sum[:])

	cfgBytes := []byte(`{}`)
	cfgSum := sha256.Sum256(cfgBytes)
	cfgDigest := "sha256:" + hex.EncodeToString(cfgSum[:])

	manifestDoc := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.scafctl.solution.config.v1+json","digest":%q,"size":%d},"layers":[{"mediaType":"application/vnd.scafctl.solution.v1+yaml","digest":%q,"size":%d}],"annotations":{"dev.scafctl.artifact.name":"remote-dep","dev.scafctl.artifact.type":"solution","org.opencontainers.image.version":"1.1.0"}}`,
		cfgDigest, len(cfgBytes), childDigest, len(childV110))
	manifestBytes := []byte(manifestDoc)
	manifestSum := sha256.Sum256(manifestBytes)
	manifestDigest := "sha256:" + hex.EncodeToString(manifestSum[:])

	ociSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/", "/v2":
			w.WriteHeader(http.StatusOK)
		case "/v2/acme/solutions/remote-dep/tags/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"acme/solutions/remote-dep","tags":["1.1.0","1.2.0"]}`))
		case "/v2/acme/solutions/remote-dep/manifests/1.1.0", "/v2/acme/solutions/remote-dep/manifests/" + manifestDigest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.Header().Set("Docker-Content-Digest", manifestDigest)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(manifestBytes)))
			if r.Method == http.MethodGet {
				_, _ = w.Write(manifestBytes)
			}
		case "/v2/acme/solutions/remote-dep/blobs/" + cfgDigest:
			w.Header().Set("Docker-Content-Digest", cfgDigest)
			_, _ = w.Write(cfgBytes)
		case "/v2/acme/solutions/remote-dep/blobs/" + childDigest:
			w.Header().Set("Docker-Content-Digest", childDigest)
			_, _ = w.Write(childV110)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ociSrv.Close)
	host := strings.TrimPrefix(ociSrv.URL, "https://")

	const parentDoc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: demo
  version: 1.0.0
spec:
  resolvers:
    sub:
      resolve:
        with:
          - provider: solution
            inputs:
              source: remote-dep@^1.0.0
`
	f := newPipelineFixture(t, map[string]string{"solution.yaml": parentDoc})
	require.NoError(t, bundler.WriteLockFile(filepath.Join(f.dir, bundler.DefaultLockFileName), &bundler.LockFile{
		Version: bundler.LockFileVersion,
		Dependencies: []bundler.LockDependency{
			{
				Ref:             "remote-dep@^1.0.0",
				ResolvedVersion: "1.1.0",
				Constraint:      "^1.0.0",
				Digest:          childDigest,
				ResolvedFrom:    "solutions-remote",
				VendoredAt:      ".scafctl/vendor/remote-dep@1.1.0.yaml",
			},
		},
	}))

	appCfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{Name: "solutions-remote", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/acme", Insecure: true},
		},
	}
	ctx := config.WithConfig(t.Context(), appCfg)

	// DryRun fetches from remote to verify bundle completeness without writing to disk or caching locally.
	dryRes, err := f.packager(t).DryRun(true).Do(ctx)
	require.NoError(t, err)
	assert.Contains(t, dryRes.Messages, "Vendored 1 catalog dependency(ies)")
	assert.NoDirExists(t, filepath.Join(f.dir, bundler.VendorDirName))
	_, err = f.cat.Resolve(ctx, catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "remote-dep", Version: semver.MustParse("1.1.0")})
	assert.True(t, catalog.IsNotFound(err), "dry-run must not auto-cache remote dependencies into local catalog")

	// Full package materializes pinned 1.1.0 (not 1.2.0), writes .scafctl/vendor/remote-dep@1.1.0.yaml, and caches locally.
	res, err := f.packager(t).Verify(VerifyOptions{Skip: true}).Do(ctx)
	require.NoError(t, err)
	assert.Contains(t, res.Messages, "Vendored 1 catalog dependency(ies)")
	assert.FileExists(t, filepath.Join(f.dir, ".scafctl", "vendor", "remote-dep@1.1.0.yaml"))

	cachedInfo, err := f.cat.Resolve(ctx, catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "remote-dep", Version: semver.MustParse("1.1.0")})
	require.NoError(t, err)
	assert.Equal(t, "auto-cached", cachedInfo.Annotations[catalog.AnnotationSource])
	assert.Equal(t, host+"/acme", cachedInfo.Annotations[catalog.AnnotationSourceCanonical])
}
