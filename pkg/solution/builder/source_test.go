// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
)

const sourceDoc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: demo
  version: 1.0.0
spec:
  resolvers: {}
`

const sourceComposeDoc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: demo
  version: 1.0.0
compose:
  - part.yaml
spec:
  resolvers: {}
`

const sourcePartDoc = `spec:
  resolvers:
    app:
      resolve:
        with:
          - provider: static
            inputs:
              value: x
`

const sourceLockDoc = `version: 1
plugins:
  - name: echo
    kind: provider
    version: 1.2.3
    digest: sha256:abc
    resolvedFrom: local
`

func writeSourceFile(t testing.TB, dir, rel, content string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadSolutionSource_File(t *testing.T) {
	t.Parallel()

	t.Run("absolute path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := writeSourceFile(t, dir, "my.yaml", sourceDoc)

		src, err := LoadSolutionSource(t.Context(), path, "", nil)
		require.NoError(t, err)
		assert.Equal(t, path, src.Path())
		assert.Equal(t, "demo", src.Solution.Metadata.Name)
		assert.Equal(t, dir, src.BundleRoot)
		assert.False(t, src.IsStdin())
		assert.Nil(t, src.Lock)
	})

	t.Run("relative path resolves against the context working directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := writeSourceFile(t, dir, filepath.Join("sub", "my.yaml"), sourceDoc)
		ctx := provider.WithWorkingDirectory(t.Context(), dir)

		src, err := LoadSolutionSource(ctx, filepath.Join("sub", "my.yaml"), "", nil)
		require.NoError(t, err)
		assert.Equal(t, path, src.Path())
		assert.Equal(t, filepath.Dir(path), src.BundleRoot)
	})

	t.Run("extension-less name is a local file, not a catalog ref", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := writeSourceFile(t, dir, "mysolution", sourceDoc)
		ctx := provider.WithWorkingDirectory(t.Context(), dir)

		src, err := LoadSolutionSource(ctx, "mysolution", "", nil)
		require.NoError(t, err)
		assert.Equal(t, path, src.Path())
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		_, err := LoadSolutionSource(t.Context(), filepath.Join(t.TempDir(), "nope.yaml"), "", nil)
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("compose and lock resolve next to the solution file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := writeSourceFile(t, dir, "solution.yaml", sourceComposeDoc)
		writeSourceFile(t, dir, "part.yaml", sourcePartDoc)
		writeSourceFile(t, dir, bundler.DefaultLockFileName, sourceLockDoc)

		src, err := LoadSolutionSource(t.Context(), path, "", nil)
		require.NoError(t, err)
		assert.Contains(t, src.Solution.Spec.Resolvers, "app")
		assert.Equal(t, path, src.Path())
		assertLockPlugin(t, src.Lock, "echo")
	})
}

func TestLoadSolutionSource_BaseDir(t *testing.T) {
	t.Parallel()

	t.Run("compose, lock, and bundle root use the base dir", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		path := writeSourceFile(t, cwd, filepath.Join("sol", "solution.yaml"), sourceComposeDoc)
		baseDir := filepath.Join(cwd, "base")
		writeSourceFile(t, baseDir, "part.yaml", sourcePartDoc)
		writeSourceFile(t, baseDir, bundler.DefaultLockFileName, sourceLockDoc)
		ctx := provider.WithWorkingDirectory(t.Context(), cwd)

		src, err := LoadSolutionSource(ctx, path, "base", nil)
		require.NoError(t, err)
		assert.Equal(t, baseDir, src.BundleRoot)
		assert.Equal(t, path, src.Path())
		assert.Contains(t, src.Solution.Spec.Resolvers, "app")
		assertLockPlugin(t, src.Lock, "echo")
	})

	t.Run("compose part next to the solution file is not used", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		path := writeSourceFile(t, cwd, filepath.Join("sol", "solution.yaml"), sourceComposeDoc)
		writeSourceFile(t, cwd, filepath.Join("sol", "part.yaml"), sourcePartDoc)

		_, err := LoadSolutionSource(t.Context(), path, filepath.Join(cwd, "base"), nil)
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("stdin uses the base dir as bundle root", func(t *testing.T) {
		t.Parallel()
		baseDir := t.TempDir()

		src, err := LoadSolutionSource(t.Context(), StdinPath, baseDir, strings.NewReader(sourceDoc))
		require.NoError(t, err)
		assert.Equal(t, baseDir, src.BundleRoot)
	})
}

func TestLoadSolutionSource_Stdin(t *testing.T) {
	t.Parallel()

	t.Run("reads the document", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		ctx := provider.WithWorkingDirectory(t.Context(), cwd)

		src, err := LoadSolutionSource(ctx, StdinPath, "", strings.NewReader(sourceDoc))
		require.NoError(t, err)
		assert.True(t, src.IsStdin())
		assert.Empty(t, src.Path())
		assert.Equal(t, "demo", src.Solution.Metadata.Name)
		assert.Equal(t, cwd, src.BundleRoot)
	})

	t.Run("nil reader", func(t *testing.T) {
		t.Parallel()
		_, err := LoadSolutionSource(t.Context(), StdinPath, "", nil)
		require.ErrorIs(t, err, load.ErrSourceNotEnabled)
	})

	t.Run("read failure", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		_, err := LoadSolutionSource(t.Context(), StdinPath, "", iotest.ErrReader(boom))
		require.ErrorIs(t, err, boom)
	})
}

func TestLoadSolutionSource_Discovery(t *testing.T) {
	t.Parallel()

	t.Run("single match", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := writeSourceFile(t, dir, "solution.yaml", sourceDoc)
		ctx := provider.WithWorkingDirectory(t.Context(), dir)

		src, err := LoadSolutionSource(ctx, "", "", nil)
		require.NoError(t, err)
		assert.Equal(t, path, src.Path())
		assert.Equal(t, dir, src.BundleRoot)
		require.NotNil(t, src.Discovery)
		assert.Equal(t, path, src.Discovery.Path)
	})

	t.Run("no match", func(t *testing.T) {
		t.Parallel()
		ctx := provider.WithWorkingDirectory(t.Context(), t.TempDir())

		_, err := LoadSolutionSource(ctx, "", "", nil)
		require.ErrorIs(t, err, load.ErrNoSolutionFound)
	})

	t.Run("ambiguous", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeSourceFile(t, dir, "solution.yaml", sourceDoc)
		writeSourceFile(t, dir, "scafctl.yaml", sourceDoc)
		ctx := provider.WithWorkingDirectory(t.Context(), dir)

		_, err := LoadSolutionSource(ctx, "", "", nil)
		require.ErrorIs(t, err, load.ErrAmbiguousSolution)
	})
}

func TestSolutionSource_ShouldBundle(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeSourceFile(t, dir, "solution.yaml", sourceDoc)

	tests := []struct {
		name            string
		path            string
		noBundle        bool
		wantBundle      bool
		wantStdinForced bool
	}{
		{name: "file bundles by default", path: path, wantBundle: true},
		{name: "file with --no-bundle", path: path, noBundle: true},
		{name: "stdin forces no bundle", path: StdinPath, wantStdinForced: true},
		{name: "stdin with --no-bundle is not reported as forced", path: StdinPath, noBundle: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src, err := LoadSolutionSource(t.Context(), tt.path, dir, strings.NewReader(sourceDoc), WithNoBundle(tt.noBundle))
			require.NoError(t, err)
			bundle, stdinForced := src.ShouldBundle()
			assert.Equal(t, tt.wantBundle, bundle)
			assert.Equal(t, tt.wantStdinForced, stdinForced)
		})
	}
}

func TestLoadSolutionSource_NoBundle(t *testing.T) {
	t.Parallel()

	t.Run("loads the document as written without compose or lock", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := writeSourceFile(t, dir, "solution.yaml", sourceComposeDoc)
		writeSourceFile(t, dir, "part.yaml", sourcePartDoc)
		writeSourceFile(t, dir, bundler.DefaultLockFileName, sourceLockDoc)

		src, err := LoadSolutionSource(t.Context(), path, "", nil, WithNoBundle(true))
		require.NoError(t, err)
		assert.Equal(t, []string{"part.yaml"}, src.Solution.Compose)
		assert.NotContains(t, src.Solution.Spec.Resolvers, "app")
		assert.Nil(t, src.Lock)
		assert.Equal(t, dir, src.BundleRoot)
	})

	t.Run("missing compose part and malformed lock do not fail", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := writeSourceFile(t, dir, "solution.yaml", sourceComposeDoc)
		writeSourceFile(t, dir, bundler.DefaultLockFileName, "not: [valid")

		src, err := LoadSolutionSource(t.Context(), path, "", nil, WithNoBundle(true))
		require.NoError(t, err)
		assert.Nil(t, src.Lock)
	})

	t.Run("applies to auto-discovery", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeSourceFile(t, dir, "solution.yaml", sourceComposeDoc)
		ctx := provider.WithWorkingDirectory(t.Context(), dir)

		src, err := LoadSolutionSource(ctx, "", "", nil, WithNoBundle(true))
		require.NoError(t, err)
		assert.Equal(t, []string{"part.yaml"}, src.Solution.Compose)
	})
}

func TestSolutionSource_ComposeFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		doc      string
		noBundle bool
		want     []string
	}{
		{name: "composed", doc: sourceComposeDoc, want: []string{"part.yaml"}},
		{name: "not composed", doc: sourceDoc},
		{name: "--no-bundle does not compose", doc: sourceComposeDoc, noBundle: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := writeSourceFile(t, dir, "solution.yaml", tt.doc)
			writeSourceFile(t, dir, "part.yaml", sourcePartDoc)

			src, err := LoadSolutionSource(t.Context(), path, "", nil, WithNoBundle(tt.noBundle))
			require.NoError(t, err)
			assert.Equal(t, tt.want, src.ComposeFiles())
		})
	}
}

func TestSolutionSource_Document(t *testing.T) {
	t.Parallel()

	const includeDoc = sourceDoc + "bundle:\n  include:\n    - templates/**\n"

	tests := []struct {
		name     string
		doc      string
		noBundle bool
		version  string
		wantRaw  bool
	}{
		{name: "as written", doc: sourceDoc, wantRaw: true},
		{name: "composed", doc: sourceComposeDoc},
		{name: "declares bundle.include", doc: includeDoc},
		{name: "bundle.include under --no-bundle", doc: includeDoc, noBundle: true, wantRaw: true},
		{name: "identity stamped", doc: sourceDoc, version: "2.0.0"},
		{name: "stamp without identity change", doc: sourceDoc, version: "1.0.0", wantRaw: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := writeSourceFile(t, dir, "solution.yaml", tt.doc)
			writeSourceFile(t, dir, "part.yaml", sourcePartDoc)
			src, err := LoadSolutionSource(t.Context(), path, "", nil, WithNoBundle(tt.noBundle))
			require.NoError(t, err)

			if tt.version != "" {
				id := &Identity{Ref: BuiltSolutionRef("demo", semver.MustParse(tt.version))}
				_, err := src.Stamp(id, GitMetadata{}, StampOptions{})
				require.NoError(t, err)
			}

			got, err := src.Document()
			require.NoError(t, err)
			if tt.wantRaw {
				assert.Equal(t, tt.doc, string(got))
				return
			}
			want, err := src.Solution.ToYAML()
			require.NoError(t, err)
			assert.Equal(t, string(want), string(got))
		})
	}
}

func TestSolutionSource_Stamp_NameMismatch(t *testing.T) {
	t.Parallel()
	src := identitySource(t, "  version: 1.0.0\n")

	id := &Identity{Ref: BuiltSolutionRef("other", semver.MustParse("1.0.0"))}
	_, err := src.Stamp(id, GitMetadata{}, StampOptions{})
	require.ErrorIs(t, err, ErrNameMismatch)

	got, err := src.Document()
	require.NoError(t, err)
	assert.Equal(t, string(src.Solution.RawContent()), string(got), "a rejected stamp leaves the document as written")
}

func assertLockPlugin(t *testing.T, lock []byte, name string) {
	t.Helper()
	require.NotNil(t, lock)
	lf, err := bundler.ParseLockJSON(lock)
	require.NoError(t, err)
	require.Len(t, lf.Plugins, 1)
	assert.Equal(t, name, lf.Plugins[0].Name)
}
