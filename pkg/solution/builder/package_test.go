// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageSolution_DryRunBundleVerification(t *testing.T) {
	t.Parallel()

	t.Run("discovers static and glob files while preserving nil dry-run Verification", func(t *testing.T) {
		t.Parallel()
		solDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(solDir, "templates"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(solDir, "config.txt"), []byte("cfg"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(solDir, "templates", "app.tpl"), []byte("hello"), 0o600))

		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bundled-demo
  version: 1.2.0
  description: Bundled demo solution
bundle:
  include:
    - templates/**
spec:
  resolvers:
    cfg:
      description: Read local config
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./config.txt
  workflow:
    actions:
      emit:
        description: Emit message
        provider: message
        inputs:
          message:
            rslvr: cfg
`
		solPath := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)

		res, err := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:    solPath,
			DryRun:  true,
			Catalog: cat,
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.True(t, res.DryRun)
		assert.Equal(t, "bundled-demo", res.Name)
		assert.Equal(t, "1.2.0", res.Version)
		assert.Nil(t, res.Verification, "dry-run report must keep Verification nil per NewPackageReport contract")
		assert.Empty(t, res.Warnings)
		require.NotNil(t, res.Bundle)
		assert.Equal(t, 2, res.Bundle.FileCount)

		items, err := cat.List(context.Background(), catalog.ArtifactKindSolution, "")
		require.NoError(t, err)
		assert.Empty(t, items, "dry-run must not store artifacts in catalog")
	})

	t.Run("rejects parent-relative path escaping bundle root", func(t *testing.T) {
		t.Parallel()
		outerDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(outerDir, "outside.txt"), []byte("secret"), 0o600))
		subDir := filepath.Join(outerDir, "sol")
		require.NoError(t, os.MkdirAll(subDir, 0o700))

		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: escape-demo
  version: 1.0.0
spec:
  resolvers:
    leaked:
      description: Escapes bundle root
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ../outside.txt
`
		solPath := filepath.Join(subDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)

		_, err = PackageSolution(context.Background(), PackageSolutionOptions{
			Path:    solPath,
			DryRun:  true,
			Catalog: cat,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes bundle root")
	})

	t.Run("surfaces unmatched glob and dynamic path warnings and enforces strict mode", func(t *testing.T) {
		t.Parallel()
		solDir := t.TempDir()
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: warn-demo
  version: 1.0.0
bundle:
  include:
    - missing/*.tpl
spec:
  resolvers:
    targetPath:
      description: Dynamic path source
      resolve:
        with:
          - provider: static
            inputs:
              value: dynamic.txt
    dynamicFile:
      description: Reads dynamic path
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path:
                rslvr: targetPath
`
		solPath := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)

		res, err := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:    solPath,
			DryRun:  true,
			Catalog: cat,
		})
		require.NoError(t, err)
		require.Len(t, res.Warnings, 2)
		assert.Contains(t, res.Warnings[0], "matches no bundled files")
		assert.Contains(t, res.Warnings[1], "dynamic path")

		strictRes, strictErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:    solPath,
			DryRun:  true,
			Strict:  true,
			Catalog: cat,
		})
		require.ErrorIs(t, strictErr, ErrVerifyFailed)
		require.NotNil(t, strictRes)
		assert.Len(t, strictRes.Warnings, 2)
	})

	t.Run("blocks on lint errors unless SkipLint is set", func(t *testing.T) {
		t.Parallel()
		solDir := t.TempDir()
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bad-lint
  version: 1.0.0
spec:
  resolvers:
    broken:
      description: Invalid CEL syntax in builtin provider
      resolve:
        with:
          - provider: static
            inputs:
              value: "ok"
      transform:
        with:
          - provider: cel
            inputs:
              expression:
                expr: "_.broken"
`
		solPath := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)

		res, err := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:    solPath,
			DryRun:  true,
			Catalog: cat,
		})
		require.ErrorIs(t, err, ErrPreflightBlocked)
		require.NotNil(t, res)
		require.NotNil(t, res.LintResult)
		assert.Greater(t, res.LintResult.ErrorCount, 0)

		okRes, okErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:     solPath,
			DryRun:   true,
			SkipLint: true,
			Catalog:  cat,
		})
		require.NoError(t, okErr)
		assert.Nil(t, okRes.LintResult)
	})

	t.Run("strict dry-run succeeds when dynamic path is covered by bundle.include and warns on directory-scoped glob mismatch", func(t *testing.T) {
		t.Parallel()
		solDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(solDir, "other"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(solDir, "other", "app.tpl"), []byte("tpl"), 0o600))

		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: covered-dynamic
  version: 1.0.0
bundle:
  include:
    - other/**
spec:
  resolvers:
    relPath:
      description: Dynamic path resolver
      resolve:
        with:
          - provider: static
            inputs:
              value: other/app.tpl
    content:
      description: Reads dynamic file covered by bundle.include
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path:
                rslvr: relPath
`
		solPath := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)

		res, err := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:    solPath,
			DryRun:  true,
			Strict:  true,
			Catalog: cat,
		})
		require.NoError(t, err)
		require.Len(t, res.Warnings, 1)
		assert.Contains(t, res.Warnings[0], "dynamic path")

		// Directory-scoped missing/**/*.tpl must NOT match other/app.tpl.
		mismatchYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mismatch-glob
  version: 1.0.0
bundle:
  include:
    - missing/**/*.tpl
spec:
  resolvers:
    val:
      description: Static value
      resolve:
        with:
          - provider: static
            inputs:
              value: ok
`
		require.NoError(t, os.WriteFile(solPath, []byte(mismatchYAML), 0o600))
		_, mismatchErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:    solPath,
			DryRun:  true,
			Strict:  true,
			Catalog: cat,
		})
		require.ErrorIs(t, mismatchErr, ErrVerifyFailed)
	})

	t.Run("dry-run with NoBundle surfaces unbundled local file warning and fails under Strict", func(t *testing.T) {
		t.Parallel()
		solDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(solDir, "local.txt"), []byte("x"), 0o600))
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nobundle-warn
  version: 1.0.0
spec:
  resolvers:
    cfg:
      description: Reads local file
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./local.txt
`
		solPath := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)

		res, err := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:     solPath,
			DryRun:   true,
			NoBundle: true,
			Catalog:  cat,
		})
		require.NoError(t, err)
		require.NotEmpty(t, res.Warnings)
		assert.Contains(t, res.Warnings[0], "has no bundle")

		_, strictErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:     solPath,
			DryRun:   true,
			NoBundle: true,
			Strict:   true,
			Catalog:  cat,
		})
		require.ErrorIs(t, strictErr, ErrVerifyFailed)
	})

	t.Run("validates identity, missing version, name mismatch, and missing file", func(t *testing.T) {
		t.Parallel()
		solDir := t.TempDir()
		cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)

		_, invalidVerErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:    filepath.Join(solDir, "solution.yaml"),
			Version: "not-a-semver",
			Catalog: cat,
		})
		require.ErrorIs(t, invalidVerErr, ErrInvalidIdentity)

		_, missingErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:    filepath.Join(solDir, "missing.yaml"),
			Catalog: cat,
		})
		require.Error(t, missingErr)

		noVerPath := filepath.Join(solDir, "no-ver.yaml")
		require.NoError(t, os.WriteFile(noVerPath, []byte("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: no-ver\nspec: {}\n"), 0o600))
		_, devVerErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:     noVerPath,
			Version:  "0.0.0-dev",
			DryRun:   true,
			SkipLint: true,
			Catalog:  cat,
		})
		require.ErrorIs(t, devVerErr, ErrDevVersion)

		namedPath := filepath.Join(solDir, "named.yaml")
		require.NoError(t, os.WriteFile(namedPath, []byte("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: orig-name\n  version: 1.0.0\nspec: {}\n"), 0o600))
		_, mismatchErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:     namedPath,
			Name:     "different-name",
			DryRun:   true,
			SkipLint: true,
			Catalog:  cat,
		})
		require.ErrorIs(t, mismatchErr, ErrNameMismatch)
	})

	t.Run("detects unmatched bundle.include glob in recursively discovered child solution", func(t *testing.T) {
		t.Parallel()
		rootDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, "shared.txt"), []byte("shared"), 0o600))
		childDir := filepath.Join(rootDir, "child")
		require.NoError(t, os.MkdirAll(childDir, 0o700))

		childYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child-sol
  version: 1.0.0
bundle:
  include:
    - missing/*.tpl
spec:
  resolvers:
    shared:
      description: Reads shared parent-root file
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ../shared.txt
`
		require.NoError(t, os.WriteFile(filepath.Join(childDir, "solution.yaml"), []byte(childYAML), 0o600))

		parentYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-sol
  version: 1.0.0
spec:
  resolvers:
    sub:
      description: Invoke child solution
      resolve:
        with:
          - provider: solution
            inputs:
              source: ./child/solution.yaml
`
		parentPath := filepath.Join(rootDir, "solution.yaml")
		require.NoError(t, os.WriteFile(parentPath, []byte(parentYAML), 0o600))

		cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)

		res, err := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:     parentPath,
			DryRun:   true,
			SkipLint: true,
			Catalog:  cat,
		})
		require.NoError(t, err)
		require.Len(t, res.Warnings, 1)
		assert.Contains(t, res.Warnings[0], "missing/*.tpl")
		assert.Contains(t, res.Warnings[0], "child/solution.yaml")

		_, strictErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:     parentPath,
			DryRun:   true,
			Strict:   true,
			SkipLint: true,
			Catalog:  cat,
		})
		require.ErrorIs(t, strictErr, ErrVerifyFailed)

		// Full build with Strict: true also fails before publishing into the catalog.
		buildStrictRes, buildStrictErr := PackageSolution(context.Background(), PackageSolutionOptions{
			Path:     parentPath,
			DryRun:   false,
			Strict:   true,
			SkipLint: true,
			Catalog:  cat,
			CacheDir: t.TempDir(),
		})
		require.ErrorIs(t, buildStrictErr, ErrVerifyFailed)
		require.NotNil(t, buildStrictRes)
		assert.NotEmpty(t, buildStrictRes.Warnings)
		storedItems, listErr := cat.List(context.Background(), catalog.ArtifactKindSolution, "")
		require.NoError(t, listErr)
		assert.Empty(t, storedItems, "strict verification failure must not store incomplete artifact")
	})
}

func TestPackageSolution_StrictReverifiesCachedArtifact(t *testing.T) {
	t.Parallel()

	solDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(solDir, "present.txt"), []byte("ok"), 0o600))
	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cached-strict-demo
  version: 1.0.0
bundle:
  include:
    - missing/*.tpl
spec:
  resolvers:
    data:
      description: Read bundled file
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./present.txt
`
	solPath := filepath.Join(solDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

	cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	cacheDir := t.TempDir()

	// Non-strict build stores the artifact and writes a build-cache entry.
	firstRes, err := PackageSolution(context.Background(), PackageSolutionOptions{
		Path:     solPath,
		DryRun:   false,
		Strict:   false,
		SkipLint: true,
		Catalog:  cat,
		CacheDir: cacheDir,
	})
	require.NoError(t, err)
	assert.False(t, firstRes.CacheHit)
	assert.NotEmpty(t, firstRes.Warnings)

	// Subsequent Strict build hits the build cache and must still fail verification.
	strictRes, strictErr := PackageSolution(context.Background(), PackageSolutionOptions{
		Path:     solPath,
		DryRun:   false,
		Strict:   true,
		SkipLint: true,
		Catalog:  cat,
		CacheDir: cacheDir,
	})
	require.ErrorIs(t, strictErr, ErrVerifyFailed)
	require.NotNil(t, strictRes)
	assert.True(t, strictRes.CacheHit)
	assert.NotEmpty(t, strictRes.Warnings)

	// Also verify zero-file discovery (only unmatched bundle.include, no static files):
	// full Strict: true fails before publication, and cached non-strict -> Strict: true fails.
	emptyDiscDir := t.TempDir()
	emptyDiscYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: empty-disc-strict
  version: 1.0.0
bundle:
  include:
    - missing/*.tpl
spec:
  resolvers:
    val:
      description: Static value
      resolve:
        with:
          - provider: static
            inputs:
              value: ok
`
	emptyDiscPath := filepath.Join(emptyDiscDir, "solution.yaml")
	require.NoError(t, os.WriteFile(emptyDiscPath, []byte(emptyDiscYAML), 0o600))
	emptyCat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	emptyCache := t.TempDir()

	_, directStrictErr := PackageSolution(context.Background(), PackageSolutionOptions{
		Path:     emptyDiscPath,
		DryRun:   false,
		Strict:   true,
		SkipLint: true,
		Catalog:  emptyCat,
		CacheDir: emptyCache,
	})
	require.ErrorIs(t, directStrictErr, ErrVerifyFailed)
	items, listErr := emptyCat.List(context.Background(), catalog.ArtifactKindSolution, "")
	require.NoError(t, listErr)
	assert.Empty(t, items, "strict failure with zero discovered files must not publish artifact")

	nonStrictEmpty, err := PackageSolution(context.Background(), PackageSolutionOptions{
		Path:     emptyDiscPath,
		DryRun:   false,
		Strict:   false,
		SkipLint: true,
		Catalog:  emptyCat,
		CacheDir: emptyCache,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, nonStrictEmpty.Warnings)

	cachedStrictEmpty, cachedStrictErr := PackageSolution(context.Background(), PackageSolutionOptions{
		Path:     emptyDiscPath,
		DryRun:   false,
		Strict:   true,
		SkipLint: true,
		Catalog:  emptyCat,
		CacheDir: emptyCache,
	})
	require.ErrorIs(t, cachedStrictErr, ErrVerifyFailed)
	require.NotNil(t, cachedStrictEmpty)
	assert.True(t, cachedStrictEmpty.CacheHit)
	assert.NotEmpty(t, cachedStrictEmpty.Warnings)
}

func TestPackageSolution_StoreAndForceWithIsolatedXDG(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdgDir)
	t.Setenv("XDG_CACHE_HOME", xdgDir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })

	solDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(solDir, "note.txt"), []byte("v1"), 0o600))
	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: stored-demo
  version: 0.2.0
spec:
  resolvers:
    note:
      description: Read bundled note
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./note.txt
  workflow:
    actions:
      show:
        description: Show note
        provider: message
        inputs:
          message:
            rslvr: note
`
	solPath := filepath.Join(solDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

	cacheDir := t.TempDir()
	res, err := PackageSolution(context.Background(), PackageSolutionOptions{
		Path:     solPath,
		DryRun:   false,
		CacheDir: cacheDir,
	})
	require.NoError(t, err)
	assert.False(t, res.DryRun)
	assert.NotEmpty(t, res.Digest)
	assert.Contains(t, res.Catalog, xdgDir)
	require.NotNil(t, res.Verification)
	assert.True(t, res.Verification.Passed)

	// Modify content so build cache misses; without Force, duplicate version fails.
	require.NoError(t, os.WriteFile(filepath.Join(solDir, "note.txt"), []byte("v2"), 0o600))
	_, dupErr := PackageSolution(context.Background(), PackageSolutionOptions{
		Path:     solPath,
		DryRun:   false,
		CacheDir: cacheDir,
	})
	require.ErrorIs(t, dupErr, ErrStoreFailed)
	assert.True(t, catalog.IsExists(dupErr))

	// With Force: true, overwrite succeeds.
	forcedRes, forceErr := PackageSolution(context.Background(), PackageSolutionOptions{
		Path:     solPath,
		DryRun:   false,
		Force:    true,
		CacheDir: cacheDir,
	})
	require.NoError(t, forceErr)
	assert.NotEmpty(t, forcedRes.Digest)
}

func BenchmarkPackageSolution_DryRun(b *testing.B) {
	solDir := b.TempDir()
	if err := os.WriteFile(filepath.Join(solDir, "hello.txt"), []byte("hello"), 0o600); err != nil {
		b.Fatal(err)
	}
	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bench-pkg
  version: 1.0.0
spec:
  resolvers:
    msg:
      description: Read hello file
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./hello.txt
`
	solPath := filepath.Join(solDir, "solution.yaml")
	if err := os.WriteFile(solPath, []byte(solYAML), 0o600); err != nil {
		b.Fatal(err)
	}
	cat, err := catalog.NewLocalCatalogAt(b.TempDir(), logr.Discard())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = PackageSolution(ctx, PackageSolutionOptions{
			Path:     solPath,
			DryRun:   true,
			SkipLint: true,
			Catalog:  cat,
		})
	}
}
