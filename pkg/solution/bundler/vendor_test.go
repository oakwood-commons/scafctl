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

	"github.com/Masterminds/semver/v3"
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
	// listings maps artifact name → list of available versions.
	listings map[string][]catalog.ArtifactInfo
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

func (m *mockCatalogFetcher) ListSolutions(_ context.Context, name string) ([]catalog.ArtifactInfo, error) {
	if m.listings == nil {
		return nil, fmt.Errorf("no listings configured for %s", name)
	}
	infos, ok := m.listings[name]
	if !ok {
		return nil, fmt.Errorf("no listings for: %s", name)
	}
	return infos, nil
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

// --- Tests for splitRef ---

func TestSplitRef(t *testing.T) {
	tests := []struct {
		ref, name, version string
	}{
		{"deploy-to-k8s@2.0.0", "deploy-to-k8s", "2.0.0"},
		{"deploy-to-k8s@^1.5.0", "deploy-to-k8s", "^1.5.0"},
		{"deploy-to-k8s@>=2.0.0", "deploy-to-k8s", ">=2.0.0"},
		{"deploy-to-k8s", "deploy-to-k8s", ""},
		{"team/deploy@1.0.0", "team/deploy", "1.0.0"},
		{"has-at@@2.0.0", "has-at@", "2.0.0"}, // edge: multiple @ takes last
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			name, ver := splitRef(tt.ref)
			assert.Equal(t, tt.name, name)
			assert.Equal(t, tt.version, ver)
		})
	}
}

// --- Tests for parseAndResolveRef ---

func TestParseAndResolveRef_ExactVersion(t *testing.T) {
	ctx := testContext()
	r, err := parseAndResolveRef(ctx, "deploy-to-k8s@2.0.0", nil)
	require.NoError(t, err)
	assert.Equal(t, "deploy-to-k8s", r.name)
	assert.Equal(t, "2.0.0", r.version.String())
	assert.Empty(t, r.constraint)
	assert.Equal(t, "deploy-to-k8s@2.0.0", r.resolvedKey)
}

func TestParseAndResolveRef_BareName(t *testing.T) {
	ctx := testContext()
	r, err := parseAndResolveRef(ctx, "deploy-to-k8s", nil)
	require.NoError(t, err)
	assert.Equal(t, "deploy-to-k8s", r.name)
	assert.Nil(t, r.version)
	assert.Empty(t, r.constraint)
	assert.Equal(t, "deploy-to-k8s", r.resolvedKey)
}

func TestParseAndResolveRef_Constraint(t *testing.T) {
	ctx := testContext()
	v150 := semver.MustParse("1.5.0")
	v152 := semver.MustParse("1.5.2")
	v200 := semver.MustParse("2.0.0")
	fetcher := &mockCatalogFetcher{
		listings: map[string][]catalog.ArtifactInfo{
			"deploy-to-k8s": {
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v150}},
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v152}},
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v200}},
			},
		},
	}

	r, err := parseAndResolveRef(ctx, "deploy-to-k8s@^1.5.0", fetcher)
	require.NoError(t, err)
	assert.Equal(t, "deploy-to-k8s", r.name)
	assert.Equal(t, "1.5.2", r.version.String()) // highest matching ^1.5.0
	assert.Equal(t, "^1.5.0", r.constraint)
	assert.Equal(t, "deploy-to-k8s@1.5.2", r.resolvedKey)
}

func TestParseAndResolveRef_ConstraintNoMatch(t *testing.T) {
	ctx := testContext()
	v100 := semver.MustParse("1.0.0")
	fetcher := &mockCatalogFetcher{
		listings: map[string][]catalog.ArtifactInfo{
			"deploy-to-k8s": {
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v100}},
			},
		},
	}

	_, err := parseAndResolveRef(ctx, "deploy-to-k8s@^2.0.0", fetcher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no version of")
	assert.Contains(t, err.Error(), "satisfies constraint")
}

func TestParseAndResolveRef_ConstraintNoFetcher(t *testing.T) {
	ctx := testContext()
	_, err := parseAndResolveRef(ctx, "deploy-to-k8s@^1.5.0", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no catalog fetcher configured")
}

func TestParseAndResolveRef_InvalidConstraint(t *testing.T) {
	ctx := testContext()
	_, err := parseAndResolveRef(ctx, "deploy-to-k8s@not_valid!!!", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version or constraint")
}

// --- Tests for version conflict detection ---

func TestVendorDependencies_VersionConflict(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	v100 := semver.MustParse("1.0.0")
	v200 := semver.MustParse("2.0.0")

	// Parent references the same solution at two different exact versions
	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &actionpkg.Workflow{
				Actions: map[string]*actionpkg.Action{
					"a1": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Literal: "deploy-to-k8s@1.0.0"},
						},
					},
					"a2": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Literal: "deploy-to-k8s@2.0.0"},
						},
					},
				},
			},
		},
	}

	fetcher := &mockCatalogFetcher{
		solutions: map[string]mockFetchResult{
			"deploy-to-k8s@1.0.0": {
				content: []byte("apiVersion: scafctl/v1\nkind: Solution\nmetadata:\n  name: deploy-to-k8s\n"),
				info:    catalog.ArtifactInfo{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v100}, Catalog: "default"},
			},
			"deploy-to-k8s@2.0.0": {
				content: []byte("apiVersion: scafctl/v1\nkind: Solution\nmetadata:\n  name: deploy-to-k8s\n"),
				info:    catalog.ArtifactInfo{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v200}, Catalog: "default"},
			},
		},
	}

	refs := []CatalogRefEntry{
		{Ref: "deploy-to-k8s@1.0.0"},
		{Ref: "deploy-to-k8s@2.0.0"},
	}

	_, err := VendorDependencies(ctx, sol, refs, VendorOptions{
		BundleRoot:     dir,
		VendorDir:      filepath.Join(dir, ".scafctl", "vendor"),
		CatalogFetcher: fetcher,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version conflict")
}

// --- Tests for constraint-based vendoring end-to-end ---

func TestVendorDependencies_ConstraintResolution(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	v150 := semver.MustParse("1.5.0")
	v152 := semver.MustParse("1.5.2")
	v200 := semver.MustParse("2.0.0")

	solutionContent := []byte("apiVersion: scafctl/v1\nkind: Solution\nmetadata:\n  name: deploy-to-k8s\n")

	fetcher := &mockCatalogFetcher{
		solutions: map[string]mockFetchResult{
			"deploy-to-k8s@1.5.2": {
				content: solutionContent,
				info:    catalog.ArtifactInfo{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v152}, Catalog: "default"},
			},
		},
		listings: map[string][]catalog.ArtifactInfo{
			"deploy-to-k8s": {
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v150}},
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v152}},
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v200}},
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
							"source": {Literal: "deploy-to-k8s@^1.5.0"},
						},
					},
				},
			},
		},
	}

	refs := []CatalogRefEntry{
		{Ref: "deploy-to-k8s@^1.5.0"},
	}

	lockPath := filepath.Join(dir, "solution.lock")
	result, err := VendorDependencies(ctx, sol, refs, VendorOptions{
		BundleRoot:     dir,
		VendorDir:      filepath.Join(dir, ".scafctl", "vendor"),
		LockPath:       lockPath,
		CatalogFetcher: fetcher,
	})
	require.NoError(t, err)
	require.Len(t, result.VendoredFiles, 1)

	// Verify lock file records both constraint and resolved version
	lf, err := LoadLockFile(lockPath)
	require.NoError(t, err)
	require.Len(t, lf.Dependencies, 1)
	assert.Equal(t, "deploy-to-k8s@^1.5.0", lf.Dependencies[0].Ref)
	assert.Equal(t, "1.5.2", lf.Dependencies[0].ResolvedVersion)
	assert.Equal(t, "^1.5.0", lf.Dependencies[0].Constraint)

	// Verify the constraint ref was rewritten to the vendored path
	src := sol.Spec.Workflow.Actions["deploy"].Inputs["source"]
	assert.Equal(t, result.VendoredFiles[0], src.Literal)
}

// --- Tests for lock replay with constraints ---

func TestVendorDependencies_LockReplayConstraintSatisfied(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	solutionContent := []byte("apiVersion: scafctl/v1\nkind: Solution\n")
	contentDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(solutionContent))

	// Pre-create vendored file
	vendorRelPath := ".scafctl/vendor/deploy-to-k8s@1.5.2.yaml"
	absVendorPath := filepath.Join(dir, vendorRelPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absVendorPath), 0o755))
	require.NoError(t, os.WriteFile(absVendorPath, solutionContent, 0o644))

	// Pre-create lock file with constraint info
	lockPath := filepath.Join(dir, "solution.lock")
	require.NoError(t, WriteLockFile(lockPath, &LockFile{
		Version: 1,
		Dependencies: []LockDependency{
			{
				Ref:             "deploy-to-k8s@^1.5.0",
				ResolvedVersion: "1.5.2",
				Constraint:      "^1.5.0",
				Digest:          contentDigest,
				ResolvedFrom:    "default",
				VendoredAt:      vendorRelPath,
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
							"source": {Literal: "deploy-to-k8s@^1.5.0"},
						},
					},
				},
			},
		},
	}

	refs := []CatalogRefEntry{
		{Ref: "deploy-to-k8s@^1.5.0"},
	}

	// No CatalogFetcher needed — lock replay should work since constraint is satisfied
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

func TestVendorDependencies_LockReplayConstraintStale(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	solutionContent := []byte("apiVersion: scafctl/v1\nkind: Solution\n")
	contentDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(solutionContent))

	// Pre-create vendored file
	vendorRelPath := ".scafctl/vendor/deploy-to-k8s@1.5.2.yaml"
	absVendorPath := filepath.Join(dir, vendorRelPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absVendorPath), 0o755))
	require.NoError(t, os.WriteFile(absVendorPath, solutionContent, 0o644))

	// Pre-create lock file with a version that does NOT satisfy the new constraint
	lockPath := filepath.Join(dir, "solution.lock")
	require.NoError(t, WriteLockFile(lockPath, &LockFile{
		Version: 1,
		Dependencies: []LockDependency{
			{
				Ref:             "deploy-to-k8s@^1.5.0",
				ResolvedVersion: "1.5.2",
				Constraint:      "^1.5.0",
				Digest:          contentDigest,
				ResolvedFrom:    "default",
				VendoredAt:      vendorRelPath,
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
							"source": {Literal: "deploy-to-k8s@^2.0.0"},
						},
					},
				},
			},
		},
	}

	refs := []CatalogRefEntry{
		{Ref: "deploy-to-k8s@^2.0.0"}, // constraint changed — locked 1.5.2 does NOT satisfy ^2.0.0
	}

	v200 := semver.MustParse("2.0.0")
	newContent := []byte("apiVersion: scafctl/v1\nkind: Solution\nmetadata:\n  name: deploy-to-k8s\n")

	fetcher := &mockCatalogFetcher{
		solutions: map[string]mockFetchResult{
			"deploy-to-k8s@2.0.0": {
				content: newContent,
				info:    catalog.ArtifactInfo{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v200}, Catalog: "default"},
			},
		},
		listings: map[string][]catalog.ArtifactInfo{
			"deploy-to-k8s": {
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v200}},
			},
		},
	}

	result, err := VendorDependencies(ctx, sol, refs, VendorOptions{
		BundleRoot:     dir,
		VendorDir:      filepath.Join(dir, ".scafctl", "vendor"),
		LockPath:       lockPath,
		CatalogFetcher: fetcher,
	})
	require.NoError(t, err)
	require.Len(t, result.VendoredFiles, 1)

	// The lock file should now have the new version
	lf, err := LoadLockFile(lockPath)
	require.NoError(t, err)
	require.Len(t, lf.Dependencies, 1)
	assert.Equal(t, "2.0.0", lf.Dependencies[0].ResolvedVersion)
	assert.Equal(t, "^2.0.0", lf.Dependencies[0].Constraint)
}

// --- Tests for dedup with different constraint strings resolving to same version ---

func TestVendorDependencies_DedupSameResolvedVersion(t *testing.T) {
	ctx := testContext()
	dir := t.TempDir()

	v152 := semver.MustParse("1.5.2")
	solutionContent := []byte("apiVersion: scafctl/v1\nkind: Solution\nmetadata:\n  name: deploy-to-k8s\n")

	fetcher := &mockCatalogFetcher{
		solutions: map[string]mockFetchResult{
			"deploy-to-k8s@1.5.2": {
				content: solutionContent,
				info:    catalog.ArtifactInfo{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v152}, Catalog: "default"},
			},
		},
		listings: map[string][]catalog.ArtifactInfo{
			"deploy-to-k8s": {
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: v152}},
			},
		},
	}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &actionpkg.Workflow{
				Actions: map[string]*actionpkg.Action{
					"a1": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Literal: "deploy-to-k8s@1.5.2"},
						},
					},
					"a2": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Literal: "deploy-to-k8s@^1.5.0"},
						},
					},
				},
			},
		},
	}

	refs := []CatalogRefEntry{
		{Ref: "deploy-to-k8s@1.5.2"},
		{Ref: "deploy-to-k8s@^1.5.0"}, // resolves to same 1.5.2 — should dedup
	}

	result, err := VendorDependencies(ctx, sol, refs, VendorOptions{
		BundleRoot:     dir,
		VendorDir:      filepath.Join(dir, ".scafctl", "vendor"),
		CatalogFetcher: fetcher,
	})
	require.NoError(t, err)
	// Only one vendored file, not two
	require.Len(t, result.VendoredFiles, 1)
}

// --- Tests for formatAvailableVersions ---

func TestFormatAvailableVersions(t *testing.T) {
	v100 := semver.MustParse("1.0.0")
	v200 := semver.MustParse("2.0.0")

	assert.Equal(t, "none", formatAvailableVersions(nil))
	assert.Equal(t, "none", formatAvailableVersions([]catalog.ArtifactInfo{}))
	assert.Equal(t, "none", formatAvailableVersions([]catalog.ArtifactInfo{
		{Reference: catalog.Reference{Name: "x"}}, // no version
	}))
	assert.Equal(t, "1.0.0, 2.0.0", formatAvailableVersions([]catalog.ArtifactInfo{
		{Reference: catalog.Reference{Name: "x", Version: v100}},
		{Reference: catalog.Reference{Name: "x", Version: v200}},
	}))
}
