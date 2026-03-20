// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
)

func TestMapKeys(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	result := MapKeys(m)
	assert.Equal(t, map[string]bool{"a": true, "b": true, "c": true}, result)
}

func TestMapKeys_Empty(t *testing.T) {
	m := map[string]int{}
	result := MapKeys(m)
	assert.Empty(t, result)
}

func TestComputeDiffSets(t *testing.T) {
	a := map[string]bool{"x": true, "y": true}
	b := map[string]bool{"y": true, "z": true}

	ds := ComputeDiffSets(a, b)
	assert.Equal(t, []string{"z"}, ds.Added)
	assert.Equal(t, []string{"x"}, ds.Removed)
	assert.Equal(t, []string{"y"}, ds.Modified)
}

func TestComputeDiffSetsFromBool_Empty(t *testing.T) {
	ds := ComputeDiffSetsFromBool(nil, nil)
	assert.Empty(t, ds.Added)
	assert.Empty(t, ds.Removed)
	assert.Empty(t, ds.Modified)
}

func TestSplitNameVersion(t *testing.T) {
	name, ver := SplitNameVersion("my-plugin@1.2.3")
	assert.Equal(t, "my-plugin", name)
	assert.Equal(t, "1.2.3", ver)
}

func TestSplitNameVersion_NoVersion(t *testing.T) {
	name, ver := SplitNameVersion("my-plugin")
	assert.Equal(t, "my-plugin", name)
	assert.Equal(t, "", ver)
}

func TestShouldIgnore(t *testing.T) {
	patterns := []string{"*.tmp", "vendor/**"}
	assert.True(t, ShouldIgnore("file.tmp", patterns))
	assert.False(t, ShouldIgnore("file.go", patterns))
}

func TestFormatSize(t *testing.T) {
	assert.Equal(t, "100 B", FormatSize(100))
	assert.Equal(t, "2.0 KB", FormatSize(2048))
	assert.Equal(t, "1.0 MB", FormatSize(1024*1024))
}

func TestMatchGlob(t *testing.T) {
	assert.True(t, MatchGlob("*.go", "main.go"))
	assert.False(t, MatchGlob("*.go", "main.txt"))
}

func TestCountChanges_NoChanges(t *testing.T) {
	result := &DiffResult{}
	assert.Equal(t, "no changes", CountChanges(result))
}

func TestCountChanges_WithChanges(t *testing.T) {
	result := &DiffResult{
		Solution: &SolutionDiff{
			Resolvers: DiffSets{Added: []string{"r1"}},
			Actions:   DiffSets{Added: []string{"a1"}, Removed: []string{"a2"}},
		},
		Files: &FilesDiff{
			Added: []FileDiffEntry{{Path: "file.yaml"}},
		},
		Vendored: &VendoredDiff{
			Added: []VendoredEntry{{Name: "dep@1.0"}},
		},
		Plugins: &PluginsDiff{
			Added: []PluginDiffEntry{{Name: "plugin"}},
		},
	}
	summary := CountChanges(result)
	assert.Contains(t, summary, "resolver")
	assert.Contains(t, summary, "action")
	assert.Contains(t, summary, "file")
	assert.Contains(t, summary, "dependency")
	assert.Contains(t, summary, "plugin")
}

func TestDiffSolutions_NoChanges(t *testing.T) {
	solA := &solution.Solution{}
	solB := &solution.Solution{}
	result := DiffSolutions(solA, solB)
	assert.Nil(t, result)
}

func TestDiffSolutions_ResolverAdded(t *testing.T) {
	solA := &solution.Solution{}
	solB := &solution.Solution{Spec: solution.Spec{Resolvers: map[string]*resolver.Resolver{"r1": {}}}}
	result := DiffSolutions(solA, solB)
	assert.NotNil(t, result)
	assert.Equal(t, []string{"r1"}, result.Resolvers.Added)
}

func TestDiffSolutions_WithWorkflow(t *testing.T) {
	solA := &solution.Solution{Spec: solution.Spec{
		Workflow: &action.Workflow{Actions: map[string]*action.Action{"a1": {}}},
	}}
	solB := &solution.Solution{Spec: solution.Spec{
		Workflow: &action.Workflow{Actions: map[string]*action.Action{"a2": {}}},
	}}
	result := DiffSolutions(solA, solB)
	assert.NotNil(t, result)
	assert.Equal(t, []string{"a2"}, result.Actions.Added)
	assert.Equal(t, []string{"a1"}, result.Actions.Removed)
}

func TestDiffFiles_Added(t *testing.T) {
	a := &BundleManifest{Files: []BundleFileEntry{{Path: "old.yaml", Digest: "abc"}}}
	b := &BundleManifest{Files: []BundleFileEntry{{Path: "old.yaml", Digest: "abc"}, {Path: "new.yaml", Digest: "def"}}}
	result := DiffFiles(a, b, nil)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1)
	assert.Equal(t, "new.yaml", result.Added[0].Path)
}

func TestDiffFiles_Removed(t *testing.T) {
	a := &BundleManifest{Files: []BundleFileEntry{{Path: "gone.yaml", Digest: "abc"}, {Path: "keep.yaml", Digest: "xyz"}}}
	b := &BundleManifest{Files: []BundleFileEntry{{Path: "keep.yaml", Digest: "xyz"}}}
	result := DiffFiles(a, b, nil)
	assert.NotNil(t, result)
	assert.Len(t, result.Removed, 1)
	assert.Equal(t, "gone.yaml", result.Removed[0].Path)
}

func TestDiffFiles_Modified(t *testing.T) {
	a := &BundleManifest{Files: []BundleFileEntry{{Path: "same.yaml", Digest: "old"}}}
	b := &BundleManifest{Files: []BundleFileEntry{{Path: "same.yaml", Digest: "new"}}}
	result := DiffFiles(a, b, nil)
	assert.NotNil(t, result)
	assert.Len(t, result.Modified, 1)
}

func TestDiffFiles_NoChanges(t *testing.T) {
	a := &BundleManifest{Files: []BundleFileEntry{{Path: "same.yaml", Digest: "abc"}}}
	result := DiffFiles(a, a, nil)
	assert.Nil(t, result)
}

func TestDiffVendored_Added(t *testing.T) {
	a := &BundleManifest{}
	b := &BundleManifest{Files: []BundleFileEntry{{Path: ".scafctl/vendor/dep@1.0.yaml", Digest: "abc"}}}
	result := DiffVendored(a, b)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1)
	assert.Equal(t, "dep", result.Added[0].Name)
}

func TestDiffVendored_Removed(t *testing.T) {
	a := &BundleManifest{Files: []BundleFileEntry{{Path: ".scafctl/vendor/dep@1.0.yaml", Digest: "abc"}}}
	b := &BundleManifest{}
	result := DiffVendored(a, b)
	assert.NotNil(t, result)
	assert.Len(t, result.Removed, 1)
}

func TestDiffVendored_NoChanges(t *testing.T) {
	entry := BundleFileEntry{Path: ".scafctl/vendor/dep@1.0.yaml", Digest: "abc"}
	m := &BundleManifest{Files: []BundleFileEntry{entry}}
	result := DiffVendored(m, m)
	assert.Nil(t, result)
}

func TestDiffPlugins_Added(t *testing.T) {
	a := &BundleManifest{}
	b := &BundleManifest{Plugins: []BundlePluginEntry{{Name: "my-plugin", Version: "1.0"}}}
	result := DiffPlugins(a, b)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1)
	assert.Equal(t, "my-plugin", result.Added[0].Name)
}

func TestDiffPlugins_Removed(t *testing.T) {
	a := &BundleManifest{Plugins: []BundlePluginEntry{{Name: "old-plugin", Version: "1.0"}}}
	b := &BundleManifest{}
	result := DiffPlugins(a, b)
	assert.NotNil(t, result)
	assert.Len(t, result.Removed, 1)
}

func TestDiffPlugins_Modified(t *testing.T) {
	a := &BundleManifest{Plugins: []BundlePluginEntry{{Name: "plug", Version: "1.0", Kind: "provider"}}}
	b := &BundleManifest{Plugins: []BundlePluginEntry{{Name: "plug", Version: "2.0", Kind: "provider"}}}
	result := DiffPlugins(a, b)
	assert.NotNil(t, result)
	assert.Len(t, result.Modified, 1)
	assert.Equal(t, "1.0", result.Modified[0].VersionFrom)
	assert.Equal(t, "2.0", result.Modified[0].VersionTo)
}

func TestDiffPlugins_NoChanges(t *testing.T) {
	m := &BundleManifest{Plugins: []BundlePluginEntry{{Name: "plug", Version: "1.0", Kind: "provider"}}}
	result := DiffPlugins(m, m)
	assert.Nil(t, result)
}
