// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	actionpkg "github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCatalogFetcher implements CatalogFetcher for testing.
type mockCatalogFetcher struct {
	solutions map[string]mockFetchResult
}

type mockFetchResult struct {
	content []byte
	info    catalog.ArtifactInfo
	err     error
}

func (m *mockCatalogFetcher) FetchSolution(_ context.Context, nameWithVersion string) ([]byte, catalog.ArtifactInfo, error) {
	r, ok := m.solutions[nameWithVersion]
	if !ok {
		return nil, catalog.ArtifactInfo{}, fmt.Errorf("not found: %s", nameWithVersion)
	}
	return r.content, r.info, r.err
}

func testContext() context.Context {
	return logger.WithLogger(context.Background(), logger.Get(-1))
}

func TestVendorDependencies_NilSolution(t *testing.T) {
	ctx := testContext()
	_, err := VendorDependencies(ctx, nil, nil, VendorOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solution is nil")
}

func TestVendorDependencies_NoRefs(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	sol := &solution.Solution{}
	result, err := VendorDependencies(ctx, sol, nil, VendorOptions{
		BundleRoot: dir,
		VendorDir:  filepath.Join(dir, ".scafctl", "vendor"),
	})
	require.NoError(t, err)
	assert.Empty(t, result.VendoredFiles)
	assert.Empty(t, result.Lock.Dependencies)
}

func TestVendorDependencies_NoCatalogFetcher(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	sol := &solution.Solution{}
	refs := []CatalogRefEntry{
		{Ref: "deploy-to-k8s@2.0.0"},
	}

	_, err := VendorDependencies(ctx, sol, refs, VendorOptions{
		BundleRoot: dir,
		VendorDir:  filepath.Join(dir, ".scafctl", "vendor"),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no catalog fetcher configured")
}

func TestVendorDependencies_FetchAndStore(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	solutionContent := []byte("apiVersion: scafctl/v1\nkind: Solution\nmetadata:\n  name: deploy-to-k8s\n")
	fetcher := &mockCatalogFetcher{
		solutions: map[string]mockFetchResult{
			"deploy-to-k8s@2.0.0": {
				content: solutionContent,
				info: catalog.ArtifactInfo{
					Reference: catalog.Reference{Name: "deploy-to-k8s"},
					Catalog:   "default",
				},
			},
		},
	}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &actionpkg.Workflow{
				Actions: map[string]*actionpkg.Action{
					"deploy": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Literal: "deploy-to-k8s@2.0.0"},
						},
					},
				},
			},
		},
	}

	refs := []CatalogRefEntry{
		{Ref: "deploy-to-k8s@2.0.0"},
	}

	lockPath := filepath.Join(dir, "solution.lock")
	vendorDir := filepath.Join(dir, ".scafctl", "vendor")

	result, err := VendorDependencies(ctx, sol, refs, VendorOptions{
		BundleRoot:     dir,
		VendorDir:      vendorDir,
		LockPath:       lockPath,
		CatalogFetcher: fetcher,
	})
	require.NoError(t, err)
	require.Len(t, result.VendoredFiles, 1)

	// Verify vendored file was written
	vendoredPath := filepath.Join(dir, result.VendoredFiles[0])
	data, err := os.ReadFile(vendoredPath)
	require.NoError(t, err)
	assert.Equal(t, solutionContent, data)

	// Verify lock file was written
	lf, err := LoadLockFile(lockPath)
	require.NoError(t, err)
	require.NotNil(t, lf)
	require.Len(t, lf.Dependencies, 1)
	assert.Equal(t, "deploy-to-k8s@2.0.0", lf.Dependencies[0].Ref)
	assert.Equal(t, "default", lf.Dependencies[0].ResolvedFrom)
	expectedDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(solutionContent))
	assert.Equal(t, expectedDigest, lf.Dependencies[0].Digest)

	// Verify source was rewritten in the solution
	src := sol.Spec.Workflow.Actions["deploy"].Inputs["source"]
	assert.Equal(t, result.VendoredFiles[0], src.Literal)
}

func TestVendorDependencies_LockFileReplay(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	solutionContent := []byte("apiVersion: scafctl/v1\nkind: Solution\n")
	contentDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(solutionContent))

	// Pre-create vendored file
	vendorRelPath := ".scafctl/vendor/deploy-to-k8s@2.0.0.yaml"
	absVendorPath := filepath.Join(dir, vendorRelPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absVendorPath), 0o755))
	require.NoError(t, os.WriteFile(absVendorPath, solutionContent, 0o644))

	// Pre-create lock file
	lockPath := filepath.Join(dir, "solution.lock")
	require.NoError(t, WriteLockFile(lockPath, &LockFile{
		Version: 1,
		Dependencies: []LockDependency{
			{
				Ref:          "deploy-to-k8s@2.0.0",
				Digest:       contentDigest,
				ResolvedFrom: "default",
				VendoredAt:   vendorRelPath,
			},
		},
	}))

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &actionpkg.Workflow{
				Actions: map[string]*actionpkg.Action{
					"deploy": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Literal: "deploy-to-k8s@2.0.0"},
						},
					},
				},
			},
		},
	}

	refs := []CatalogRefEntry{
		{Ref: "deploy-to-k8s@2.0.0"},
	}

	// No CatalogFetcher needed — should replay from lock
	result, err := VendorDependencies(ctx, sol, refs, VendorOptions{
		BundleRoot: dir,
		VendorDir:  filepath.Join(dir, ".scafctl", "vendor"),
		LockPath:   lockPath,
	})
	require.NoError(t, err)
	require.Len(t, result.VendoredFiles, 1)
	assert.Equal(t, vendorRelPath, result.VendoredFiles[0])

	// Verify source was rewritten
	src := sol.Spec.Workflow.Actions["deploy"].Inputs["source"]
	assert.Equal(t, vendorRelPath, src.Literal)
}

func TestVendorDependencies_CircularDetection(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	// The fetched solution references itself
	selfRefContent := `apiVersion: scafctl/v1
kind: Solution
metadata:
  name: circular
spec:
  workflow:
    actions:
      self-ref:
        provider: solution
        inputs:
          source: "circular@1.0.0"
`

	fetcher := &mockCatalogFetcher{
		solutions: map[string]mockFetchResult{
			"circular@1.0.0": {
				content: []byte(selfRefContent),
				info: catalog.ArtifactInfo{
					Reference: catalog.Reference{Name: "circular"},
					Catalog:   "default",
				},
			},
		},
	}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &actionpkg.Workflow{
				Actions: map[string]*actionpkg.Action{
					"deploy": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Literal: "circular@1.0.0"},
						},
					},
				},
			},
		},
	}

	refs := []CatalogRefEntry{{Ref: "circular@1.0.0"}}

	result, err := VendorDependencies(ctx, sol, refs, VendorOptions{
		BundleRoot:     dir,
		VendorDir:      filepath.Join(dir, ".scafctl", "vendor"),
		CatalogFetcher: fetcher,
	})

	// Should succeed — circular ref is detected and skipped (visited[ref] = true)
	require.NoError(t, err)
	// Only the first ref should be vendored, not the recursive self-reference
	require.Len(t, result.VendoredFiles, 1)
}

func TestRewriteSolutionSources_Resolvers(t *testing.T) {
	sourceRef := "my-resolver@1.0.0"
	vendorPath := ".scafctl/vendor/my-resolver@1.0.0.yaml"

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"env": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "solution",
								Inputs: map[string]*spec.ValueRef{
									"source": {Literal: sourceRef},
								},
							},
							{
								Provider: "parameter",
								Inputs: map[string]*spec.ValueRef{
									"value": {Literal: "default-val"},
								},
							},
						},
					},
				},
			},
		},
	}

	rewriteSolutionSources(sol, sourceRef, vendorPath)

	// The solution provider input should be rewritten
	src := sol.Spec.Resolvers["env"].Resolve.With[0].Inputs["source"]
	assert.Equal(t, vendorPath, src.Literal)

	// The parameter provider input should be unchanged
	val := sol.Spec.Resolvers["env"].Resolve.With[1].Inputs["value"]
	assert.Equal(t, "default-val", val.Literal)
}

func TestRewriteSolutionSources_Actions(t *testing.T) {
	sourceRef := "deploy-to-k8s@2.0.0"
	vendorPath := ".scafctl/vendor/deploy-to-k8s@2.0.0.yaml"

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &actionpkg.Workflow{
				Actions: map[string]*actionpkg.Action{
					"deploy": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Literal: sourceRef},
						},
					},
					"notify": {
						Provider: "shell",
						Inputs: map[string]*spec.ValueRef{
							"command": {Literal: "echo done"},
						},
					},
				},
				Finally: map[string]*actionpkg.Action{
					"cleanup": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Literal: sourceRef},
						},
					},
				},
			},
		},
	}

	rewriteSolutionSources(sol, sourceRef, vendorPath)

	// deploy action source should be rewritten
	assert.Equal(t, vendorPath, sol.Spec.Workflow.Actions["deploy"].Inputs["source"].Literal)
	// notify action should be unchanged
	assert.Equal(t, "echo done", sol.Spec.Workflow.Actions["notify"].Inputs["command"].Literal)
	// finally cleanup action source should be rewritten
	assert.Equal(t, vendorPath, sol.Spec.Workflow.Finally["cleanup"].Inputs["source"].Literal)
}

func TestRewriteSourceInput_NonLiteralSkipped(t *testing.T) {
	exprVal := celexp.Expression("some_expression")
	inputs := map[string]*spec.ValueRef{
		"source": {Literal: "some-ref@1.0.0", Expr: &exprVal},
	}
	rewriteSourceInput(inputs, "some-ref@1.0.0", "vendor/path")
	// Should NOT be rewritten because Expr is set
	assert.Equal(t, "some-ref@1.0.0", inputs["source"].Literal)
}

func TestRewriteSourceInput_DifferentRefSkipped(t *testing.T) {
	inputs := map[string]*spec.ValueRef{
		"source": {Literal: "other-ref@1.0.0"},
	}
	rewriteSourceInput(inputs, "target-ref@2.0.0", "vendor/path")
	// Should NOT be rewritten because ref doesn't match
	assert.Equal(t, "other-ref@1.0.0", inputs["source"].Literal)
}

func TestRewriteSourceInput_NilInputs(t *testing.T) {
	// Should not panic
	rewriteSourceInput(nil, "ref", "path")
}

func TestRewriteSourceInput_NoSourceKey(t *testing.T) {
	inputs := map[string]*spec.ValueRef{
		"other": {Literal: "value"},
	}
	rewriteSourceInput(inputs, "ref", "path")
	assert.Equal(t, "value", inputs["other"].Literal)
}

func TestVendorFileName(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		info     catalog.ArtifactInfo
		expected string
	}{
		{
			name:     "simple ref with version",
			ref:      "deploy-to-k8s@2.0.0",
			info:     catalog.ArtifactInfo{},
			expected: "deploy-to-k8s@2.0.0.yaml",
		},
		{
			name:     "ref with slash",
			ref:      "team/deploy-to-k8s@1.0.0",
			info:     catalog.ArtifactInfo{},
			expected: "team_deploy-to-k8s@1.0.0.yaml",
		},
		{
			name:     "ref already has yaml extension",
			ref:      "deploy@1.0.0.yaml",
			info:     catalog.ArtifactInfo{},
			expected: "deploy@1.0.0.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := vendorFileName(tt.ref, tt.info)
			assert.Equal(t, tt.expected, result)
		})
	}
}
