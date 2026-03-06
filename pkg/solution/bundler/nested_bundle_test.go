// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Recursive Sub-Solution Discovery Tests ---

func TestDiscoverFiles_RecursiveSubSolution(t *testing.T) {
	// Parent solution references a sub-solution that itself references a template file
	tmpDir := t.TempDir()

	// Create parent solution content
	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child.yaml"
`

	// Create child solution that references its own template
	childYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child
  version: 1.0.0
spec:
  resolvers:
    template:
      resolve:
        with:
          - provider: file
            inputs:
              path: "templates/child.tmpl"
              operation: read
`

	// Set up directory structure
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub", "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "child.yaml"), []byte(childYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "templates", "child.tmpl"), []byte("child template"), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	result, err := DiscoverFiles(&sol, tmpDir)
	require.NoError(t, err)

	// Should discover both the child.yaml AND its template
	assert.GreaterOrEqual(t, len(result.LocalFiles), 2, "should discover child.yaml and its template")

	paths := make(map[string]bool)
	for _, f := range result.LocalFiles {
		paths[f.RelPath] = true
	}
	assert.True(t, paths["sub/child.yaml"], "should include sub/child.yaml")
	assert.True(t, paths[filepath.Join("sub", "templates", "child.tmpl")], "should include sub/templates/child.tmpl")
}

func TestDiscoverFiles_RecursiveSubSolution_CatalogRefs(t *testing.T) {
	// Sub-solution references a catalog dependency — should be discovered
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child.yaml"
`

	childYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child
  version: 1.0.0
spec:
  resolvers:
    dep:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "shared-lib@1.0.0"
`

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "child.yaml"), []byte(childYAML), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	result, err := DiscoverFiles(&sol, tmpDir)
	require.NoError(t, err)

	// Should discover the catalog ref from child
	require.Len(t, result.CatalogRefs, 1)
	assert.Equal(t, "shared-lib@1.0.0", result.CatalogRefs[0].Ref)
}

func TestDiscoverFiles_CircularReference(t *testing.T) {
	// Parent references child, child references parent → circular
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./child.yaml"
`

	childYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child
  version: 1.0.0
spec:
  resolvers:
    parent:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./parent.yaml"
`

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "parent.yaml"), []byte(parentYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child.yaml"), []byte(childYAML), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	// Write parent as a file so it can be discovered
	_, err := DiscoverFiles(&sol, tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular sub-solution reference detected")
}

func TestDiscoverFiles_DeepNesting(t *testing.T) {
	// Three levels: parent → child → grandchild
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./level1/child.yaml"
`

	childYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child
  version: 1.0.0
spec:
  resolvers:
    grandchild:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./level2/grandchild.yaml"
`

	grandchildYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: grandchild
  version: 1.0.0
spec:
  resolvers:
    data:
      resolve:
        with:
          - provider: file
            inputs:
              path: "data.json"
              operation: read
`

	// Create directory structure
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "level1", "level2"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "level1", "child.yaml"), []byte(childYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "level1", "level2", "grandchild.yaml"), []byte(grandchildYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "level1", "level2", "data.json"), []byte(`{"key":"value"}`), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	result, err := DiscoverFiles(&sol, tmpDir)
	require.NoError(t, err)

	paths := make(map[string]bool)
	for _, f := range result.LocalFiles {
		paths[f.RelPath] = true
	}

	assert.True(t, paths["level1/child.yaml"], "should include level1/child.yaml")
	assert.True(t, paths[filepath.Join("level1", "level2", "grandchild.yaml")], "should include grandchild")
	assert.True(t, paths[filepath.Join("level1", "level2", "data.json")], "should include grandchild's data.json")
}

func TestDiscoverFiles_SubSolutionWithBundleIncludes(t *testing.T) {
	// Sub-solution has its own bundle.include globs
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child.yaml"
`

	childYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child
  version: 1.0.0
bundle:
  include:
    - "configs/*.yaml"
spec:
  resolvers:
    config:
      resolve:
        with:
          - provider: file
            inputs:
              path:
                expr: "'configs/' + _.env + '.yaml'"
`

	// Create directory structure
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub", "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "child.yaml"), []byte(childYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "configs", "dev.yaml"), []byte("env: dev"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "configs", "prod.yaml"), []byte("env: prod"), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	result, err := DiscoverFiles(&sol, tmpDir)
	require.NoError(t, err)

	paths := make(map[string]bool)
	for _, f := range result.LocalFiles {
		paths[f.RelPath] = true
	}

	assert.True(t, paths["sub/child.yaml"], "should include sub/child.yaml")
	assert.True(t, paths[filepath.Join("sub", "configs", "dev.yaml")], "should include sub/configs/dev.yaml via bundle.include")
	assert.True(t, paths[filepath.Join("sub", "configs", "prod.yaml")], "should include sub/configs/prod.yaml via bundle.include")
}

func TestDiscoverFiles_SubSolutionNotParseable(t *testing.T) {
	// Sub-solution file is not valid YAML — should still be included as a file
	// but recursive analysis should be skipped gracefully
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/not-a-solution.yaml"
`

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "not-a-solution.yaml"), []byte("this is not valid solution yaml"), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	result, err := DiscoverFiles(&sol, tmpDir)
	require.NoError(t, err)

	// Should still include the file itself
	require.Len(t, result.LocalFiles, 1)
	assert.Equal(t, "sub/not-a-solution.yaml", result.LocalFiles[0].RelPath)
}

func TestDiscoverFiles_SubSolutionMissingFile(t *testing.T) {
	// Sub-solution referenced but file doesn't exist — should be added to static files
	// but recursive analysis should be skipped
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/missing.yaml"
`

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	// File doesn't exist so addFileEntry will fail
	_, err := DiscoverFiles(&sol, tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file not found")
}

// --- Nested Tar Extraction Tests ---

func TestExtractBundleTar_NestedBundle(t *testing.T) {
	// Create a nested bundle tar: inner tar inside the outer tar

	// Create the inner bundle tar manually
	var innerBuf bytes.Buffer
	innerTw := tar.NewWriter(&innerBuf)

	innerManifest := &BundleManifest{
		Version: 1,
		Root:    ".",
		Files: []BundleFileEntry{
			{Path: "inner-template.tmpl", Size: 13, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("inner content")))},
		},
	}
	manifestJSON, err := json.Marshal(innerManifest)
	require.NoError(t, err)
	require.NoError(t, writeToTar(innerTw, BundleManifestPath, manifestJSON))
	require.NoError(t, writeToTar(innerTw, "inner-template.tmpl", []byte("inner content")))
	require.NoError(t, innerTw.Close())

	// Create the outer bundle tar containing the inner tar as a .bundle.tar
	var outerBuf bytes.Buffer
	outerTw := tar.NewWriter(&outerBuf)

	outerManifest := &BundleManifest{
		Version: 1,
		Root:    ".",
		Files: []BundleFileEntry{
			{Path: "sub/child.yaml", Size: 11, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("child yaml")))},
			{Path: "sub/child.bundle.tar", Size: int64(innerBuf.Len()), Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(innerBuf.Bytes()))},
		},
	}
	outerManifestJSON, err := json.Marshal(outerManifest)
	require.NoError(t, err)
	require.NoError(t, writeToTar(outerTw, BundleManifestPath, outerManifestJSON))
	require.NoError(t, writeToTar(outerTw, "sub/child.yaml", []byte("child yaml")))
	require.NoError(t, writeToTar(outerTw, "sub/child.bundle.tar", innerBuf.Bytes()))
	require.NoError(t, outerTw.Close())

	// Extract and verify
	destDir := t.TempDir()
	manifest, err := ExtractBundleTar(outerBuf.Bytes(), destDir)
	require.NoError(t, err)
	require.NotNil(t, manifest)

	// The inner template should be extracted into sub/ directory
	innerTemplatePath := filepath.Join(destDir, "sub", "inner-template.tmpl")
	content, err := os.ReadFile(innerTemplatePath)
	require.NoError(t, err)
	assert.Equal(t, "inner content", string(content))
}

func TestExtractBundleTar_NoNestedTar(t *testing.T) {
	// Regular tar without nested bundles should still work
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	manifest := &BundleManifest{
		Version: 1,
		Root:    ".",
		Files: []BundleFileEntry{
			{Path: "template.tmpl", Size: 7, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("content")))},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, writeToTar(tw, BundleManifestPath, manifestJSON))
	require.NoError(t, writeToTar(tw, "template.tmpl", []byte("content")))
	require.NoError(t, tw.Close())

	destDir := t.TempDir()
	result, err := ExtractBundleTar(buf.Bytes(), destDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Files, 1)
	assert.Equal(t, "template.tmpl", result.Files[0].Path)
}

// --- Nested Vendoring Tests ---

func TestVendorDependencies_LocalSubSolutionCatalogRefs(t *testing.T) {
	// Parent references a local sub-solution, sub-solution has catalog deps.
	// DiscoverFiles should find catalog refs from sub-solution, which are then vendored.
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child.yaml"
`

	childYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child
  version: 1.0.0
spec:
  resolvers:
    dep:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "shared-lib@1.0.0"
`

	// Set up files
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "child.yaml"), []byte(childYAML), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	// Discover files to get catalog refs
	result, err := DiscoverFiles(&sol, tmpDir)
	require.NoError(t, err)
	require.Len(t, result.CatalogRefs, 1)
	assert.Equal(t, "shared-lib@1.0.0", result.CatalogRefs[0].Ref)

	// Vendor the discovered catalog refs
	sharedLibContent := []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: shared-lib
  version: 1.0.0
spec:
  resolvers:
    data:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hello'"
`)

	fetcher := &mockCatalogFetcher{
		solutions: map[string]mockFetchResult{
			"shared-lib@1.0.0": {
				content: sharedLibContent,
				info: catalog.ArtifactInfo{
					Catalog: "test-catalog",
				},
			},
		},
	}

	vendorResult, err := VendorDependencies(ctx, &sol, result.CatalogRefs, VendorOptions{
		BundleRoot:     tmpDir,
		VendorDir:      filepath.Join(tmpDir, ".scafctl", "vendor"),
		CatalogFetcher: fetcher,
	})
	require.NoError(t, err)
	assert.Len(t, vendorResult.VendoredFiles, 1)
}

// --- Integration: Full Bundle Round-Trip with Nested Sub-Solutions ---

func TestNestedBundle_FullRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child.yaml"
    template:
      resolve:
        with:
          - provider: file
            inputs:
              path: "parent-template.tmpl"
              operation: read
`

	childYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child
  version: 1.0.0
spec:
  resolvers:
    childTemplate:
      resolve:
        with:
          - provider: file
            inputs:
              path: "child-template.tmpl"
              operation: read
`

	// Set up directory structure
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "child.yaml"), []byte(childYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "child-template.tmpl"), []byte("child tmpl"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "parent-template.tmpl"), []byte("parent tmpl"), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	// Step 1: Discover files (recursively)
	discovery, err := DiscoverFiles(&sol, tmpDir)
	require.NoError(t, err)

	paths := make(map[string]bool)
	for _, f := range discovery.LocalFiles {
		paths[f.RelPath] = true
	}

	assert.True(t, paths["parent-template.tmpl"])
	assert.True(t, paths["sub/child.yaml"])
	assert.True(t, paths[filepath.Join("sub", "child-template.tmpl")])

	// Step 2: Create bundle tar
	tarData, manifest, err := CreateBundleTar(tmpDir, discovery.LocalFiles, nil)
	require.NoError(t, err)
	require.NotNil(t, manifest)
	assert.Len(t, manifest.Files, 3) // parent-template, child.yaml, child-template

	// Step 3: Extract and verify all files are present
	extractDir := t.TempDir()
	_, err = ExtractBundleTar(tarData, extractDir)
	require.NoError(t, err)

	// Verify all files were extracted
	for _, f := range discovery.LocalFiles {
		extractedPath := filepath.Join(extractDir, f.RelPath)
		_, err := os.Stat(extractedPath)
		assert.NoError(t, err, "expected file to be extracted: %s", f.RelPath)
	}
}

func TestDiscoverFiles_ActionSubSolution(t *testing.T) {
	// Sub-solution referenced from an action
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    data:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hello'"
  workflow:
    actions:
      deploy:
        provider: solution
        inputs:
          source: "./actions/deploy.yaml"
`

	deployYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: deploy
  version: 1.0.0
spec:
  resolvers:
    script:
      resolve:
        with:
          - provider: file
            inputs:
              path: "deploy.sh"
              operation: read
`

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "actions"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "actions", "deploy.yaml"), []byte(deployYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "actions", "deploy.sh"), []byte("#!/bin/bash\necho deploy"), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	result, err := DiscoverFiles(&sol, tmpDir)
	require.NoError(t, err)

	paths := make(map[string]bool)
	for _, f := range result.LocalFiles {
		paths[f.RelPath] = true
	}

	assert.True(t, paths["actions/deploy.yaml"], "should include actions/deploy.yaml")
	assert.True(t, paths[filepath.Join("actions", "deploy.sh")], "should include actions/deploy.sh from sub-solution")
}

func TestIdentifySubSolutionFiles(t *testing.T) {
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    child1:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child1.yaml"
    child2:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child2.yaml"
    catalog:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "remote-dep@1.0.0"
    file:
      resolve:
        with:
          - provider: file
            inputs:
              path: "template.tmpl"
              operation: read
`)))

	subFiles := identifySubSolutionFiles(sol)
	assert.Len(t, subFiles, 2, "should identify 2 local sub-solution files")

	subSet := make(map[string]bool)
	for _, s := range subFiles {
		subSet[s] = true
	}
	assert.True(t, subSet["./sub/child1.yaml"])
	assert.True(t, subSet["./sub/child2.yaml"])
}

func TestIsNestedBundleTar(t *testing.T) {
	// Create a valid nested bundle tar
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	manifest := &BundleManifest{Version: 1, Root: ".", Files: nil}
	manifestJSON, _ := json.Marshal(manifest)
	require.NoError(t, writeToTar(tw, BundleManifestPath, manifestJSON))
	require.NoError(t, tw.Close())

	assert.True(t, isNestedBundleTar("sub/child.bundle.tar", buf.Bytes()), "should detect .bundle.tar extension")
	assert.True(t, isNestedBundleTar("sub/child.tar", buf.Bytes()), "should detect tar with bundle manifest content")
	assert.False(t, isNestedBundleTar("template.tmpl", []byte("plain text")), "should not detect plain text")
	assert.False(t, isNestedBundleTar("small.tar", []byte("too small")), "should not detect too-small content")
}

func TestDiscoverFiles_DuplicateSubSolutionReferences(t *testing.T) {
	// Same sub-solution referenced from multiple resolvers — should not duplicate
	tmpDir := t.TempDir()

	parentYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent
  version: 1.0.0
spec:
  resolvers:
    ref1:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child.yaml"
    ref2:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child.yaml"
`

	childYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child
  version: 1.0.0
spec:
  resolvers:
    template:
      resolve:
        with:
          - provider: file
            inputs:
              path: "data.json"
              operation: read
`

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "child.yaml"), []byte(childYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "data.json"), []byte(`{}`), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(parentYAML)))

	result, err := DiscoverFiles(&sol, tmpDir)
	require.NoError(t, err)

	// Should have exactly 2 files: child.yaml and data.json (deduplicated)
	assert.Len(t, result.LocalFiles, 2)

	paths := make(map[string]bool)
	for _, f := range result.LocalFiles {
		paths[f.RelPath] = true
	}
	assert.True(t, paths["sub/child.yaml"])
	assert.True(t, paths[filepath.Join("sub", "data.json")])
}

func TestDiscoverFiles_SelfReferentialCircular(t *testing.T) {
	// Solution references itself — should detect circular reference
	tmpDir := t.TempDir()

	selfRefYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: self-ref
  version: 1.0.0
spec:
  resolvers:
    self:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./self.yaml"
`

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "self.yaml"), []byte(selfRefYAML), 0o644))

	var sol solution.Solution
	require.NoError(t, sol.UnmarshalFromBytes([]byte(selfRefYAML)))

	_, err := DiscoverFiles(&sol, tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular sub-solution reference detected")
}

// --- Unused import suppressor ---
var (
	_ = spec.ValueRef{}
)
