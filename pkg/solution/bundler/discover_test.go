// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// literalValueRef creates a ValueRef with a string literal.
func literalValueRef(s string) *spec.ValueRef {
	return &spec.ValueRef{Literal: s}
}

func TestDiscoverFiles_NilSolution(t *testing.T) {
	_, err := DiscoverFiles(nil, "/tmp/bundle")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "solution is nil")
}

func TestDiscoverFiles_FileProviderStaticAnalysis(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    myFile:
      resolve:
        with:
          - provider: file
            inputs:
              path: "templates/main.tmpl"
              operation: read
`))
	require.NoError(t, err)

	tmpDir := t.TempDir()
	err = os.MkdirAll(filepath.Join(tmpDir, "templates"), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "templates", "main.tmpl"), []byte("hello"), 0o644)
	require.NoError(t, err)

	result, err := DiscoverFiles(sol, tmpDir)
	require.NoError(t, err)
	assert.Len(t, result.LocalFiles, 1)
	assert.Equal(t, "templates/main.tmpl", result.LocalFiles[0].RelPath)
	assert.Equal(t, StaticAnalysis, result.LocalFiles[0].Source)
}

func TestDiscoverFiles_SolutionProviderLocal(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    nested:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child.yaml"
`))
	require.NoError(t, err)

	tmpDir := t.TempDir()
	err = os.MkdirAll(filepath.Join(tmpDir, "sub"), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "sub", "child.yaml"), []byte("content"), 0o644)
	require.NoError(t, err)

	result, err := DiscoverFiles(sol, tmpDir)
	require.NoError(t, err)
	assert.Len(t, result.LocalFiles, 1)
	assert.Equal(t, "sub/child.yaml", result.LocalFiles[0].RelPath)
}

func TestDiscoverFiles_SolutionProviderCatalog(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    nested:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "my-catalog/my-solution@1.0.0"
`))
	require.NoError(t, err)

	result, err := DiscoverFiles(sol, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, result.LocalFiles)
	assert.Len(t, result.CatalogRefs, 1)
	assert.Equal(t, "my-catalog/my-solution@1.0.0", result.CatalogRefs[0].Ref)
}

func TestDiscoverFiles_SkipsDynamicPaths(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    dynamic:
      resolve:
        with:
          - provider: file
            inputs:
              path:
                expr: "resolvers.fileName.value"
              operation: read
`))
	require.NoError(t, err)

	result, err := DiscoverFiles(sol, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, result.LocalFiles, "dynamic paths should be skipped")
}

func TestDiscoverFiles_ExplicitIncludes(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.MkdirAll(filepath.Join(tmpDir, "configs"), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "configs", "dev.yaml"), []byte("dev"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "configs", "prod.yaml"), []byte("prod"), 0o644)
	require.NoError(t, err)

	sol := &solution.Solution{}
	err = sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers: {}
`))
	require.NoError(t, err)
	sol.Bundle.Include = []string{"configs/*.yaml"}

	result, err := DiscoverFiles(sol, tmpDir)
	require.NoError(t, err)
	assert.Len(t, result.LocalFiles, 2)
	for _, f := range result.LocalFiles {
		assert.Equal(t, ExplicitInclude, f.Source)
	}
}

func TestDiscoverFiles_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "shared.yaml"), []byte("data"), 0o644)
	require.NoError(t, err)

	sol := &solution.Solution{}
	err = sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    myFile:
      resolve:
        with:
          - provider: file
            inputs:
              path: "shared.yaml"
              operation: read
`))
	require.NoError(t, err)
	sol.Bundle.Include = []string{"shared.yaml"}

	result, err := DiscoverFiles(sol, tmpDir)
	require.NoError(t, err)
	assert.Len(t, result.LocalFiles, 1, "duplicate files should be deduplicated")
}

func TestDiscoverFiles_SkipsAbsolutePaths(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    myFile:
      resolve:
        with:
          - provider: file
            inputs:
              path: "/etc/passwd"
              operation: read
`))
	require.NoError(t, err)

	result, err := DiscoverFiles(sol, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, result.LocalFiles, "absolute paths should be skipped by isLocalPath")
}

func TestDiscoverFiles_IgnoresURLs(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    remote:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "https://example.com/solution.yaml"
`))
	require.NoError(t, err)

	result, err := DiscoverFiles(sol, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, result.LocalFiles)
	assert.Empty(t, result.CatalogRefs)
}

func TestDiscoverFiles_WithIgnoreChecker(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.MkdirAll(filepath.Join(tmpDir, "templates"), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "templates", "keep.tmpl"), []byte("keep"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "templates", "ignore.tmpl"), []byte("ignored"), 0o644)
	require.NoError(t, err)

	sol := &solution.Solution{}
	err = sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers: {}
`))
	require.NoError(t, err)
	sol.Bundle.Include = []string{"templates/*.tmpl"}

	ignoreChecker := ParseIgnorePatterns([]string{"templates/ignore.tmpl"})
	result, err := DiscoverFiles(sol, tmpDir, WithIgnoreChecker(ignoreChecker))
	require.NoError(t, err)
	assert.Len(t, result.LocalFiles, 1)
	assert.Equal(t, "templates/keep.tmpl", result.LocalFiles[0].RelPath)
}

// Helper function tests

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"relative file", "templates/main.tmpl", true},
		{"dot relative", "./templates/main.tmpl", true},
		{"parent relative", "../templates/main.tmpl", true},
		{"http URL", "https://example.com/file.yaml", false},
		{"http URL", "http://example.com/file.yaml", false},
		{"catalog ref with version", "my-catalog/solution@1.0.0", false},
		// ambiguous: treated as local path without @
		{"catalog ref no version", "my-catalog/solution", true},
		{"simple filename", "solution.yaml", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isLocalPath(tt.path))
		})
	}
}

func TestIsCatalogRef(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		expected bool
	}{
		{"catalog ref with version", "my-catalog/solution@1.0.0", true},
		{"catalog ref no version", "my-catalog/solution", false}, // needs @ or no slashes to be catalog ref
		{"local path", "templates/main.tmpl", false},
		{"dot path", "./solution.yaml", false},
		{"URL", "https://example.com/sol.yaml", false},
		{"simple file", "solution.yaml", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isCatalogRef(tt.ref))
		})
	}
}

func TestExtractLiteralString(t *testing.T) {
	tests := []struct {
		name     string
		inputs   map[string]*spec.ValueRef
		key      string
		expected string
	}{
		{"nil inputs", nil, "path", ""},
		{"missing key", map[string]*spec.ValueRef{"other": literalValueRef("x")}, "path", ""},
		{"literal string", map[string]*spec.ValueRef{"path": literalValueRef("hello")}, "path", "hello"},
		{"nil valueref", map[string]*spec.ValueRef{"path": nil}, "path", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLiteralString(tt.inputs, tt.key)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDiscoverySource_String(t *testing.T) {
	assert.Equal(t, "static-analysis", fmt.Sprint(StaticAnalysis))
	assert.Equal(t, "explicit-include", fmt.Sprint(ExplicitInclude))
}

func TestWithStatFunc(t *testing.T) {
	called := false
	opt := WithStatFunc(func(path string) (os.FileInfo, error) {
		called = true
		return nil, nil
	})
	cfg := &discoverConfig{}
	opt(cfg)
	assert.NotNil(t, cfg.statFunc)
	cfg.statFunc("test")
	assert.True(t, called)
}

func TestWithDiscoverReadFileFunc(t *testing.T) {
	called := false
	opt := WithDiscoverReadFileFunc(func(path string) ([]byte, error) {
		called = true
		return []byte("data"), nil
	})
	cfg := &discoverConfig{}
	opt(cfg)
	assert.NotNil(t, cfg.readFile)
	data, _ := cfg.readFile("test")
	assert.True(t, called)
	assert.Equal(t, []byte("data"), data)
}

func TestWithWalkDirFunc(t *testing.T) {
	called := false
	opt := WithWalkDirFunc(func(root string, fn filepath.WalkFunc) error {
		called = true
		return nil
	})
	cfg := &discoverConfig{}
	opt(cfg)
	assert.NotNil(t, cfg.walkDir)
	cfg.walkDir(".", nil)
	assert.True(t, called)
}
