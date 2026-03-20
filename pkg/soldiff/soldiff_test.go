// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soldiff

import (
	"context"
	"os"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
)

// helper to build a minimal solution.
func makeSolution(name, desc string, version *semver.Version) *solution.Solution {
	return &solution.Solution{
		Metadata: solution.Metadata{
			Name:        name,
			Description: desc,
			Version:     version,
		},
		Spec: solution.Spec{},
	}
}

// ── Compare – metadata ──────────────────────────────────────────────

func TestCompare_IdenticalSolutions(t *testing.T) {
	sol := makeSolution("app", "desc", semver.MustParse("1.0.0"))
	r := Compare(sol, sol)

	assert.Empty(t, r.Changes)
	assert.Equal(t, 0, r.Summary.Total)
}

func TestCompare_MetadataNameChanged(t *testing.T) {
	a := makeSolution("alpha", "desc", nil)
	b := makeSolution("beta", "desc", nil)

	r := Compare(a, b)

	require.Len(t, r.Changes, 1)
	assert.Equal(t, "metadata.name", r.Changes[0].Field)
	assert.Equal(t, "changed", r.Changes[0].Type)
	assert.Equal(t, "alpha", r.Changes[0].OldValue)
	assert.Equal(t, "beta", r.Changes[0].NewValue)
	assert.Equal(t, 1, r.Summary.Changed)
}

func TestCompare_MetadataDescriptionChanged(t *testing.T) {
	a := makeSolution("app", "old", nil)
	b := makeSolution("app", "new", nil)

	r := Compare(a, b)

	require.Len(t, r.Changes, 1)
	assert.Equal(t, "metadata.description", r.Changes[0].Field)
	assert.Equal(t, "changed", r.Changes[0].Type)
}

func TestCompare_MetadataVersionChanged(t *testing.T) {
	a := makeSolution("app", "", semver.MustParse("1.0.0"))
	b := makeSolution("app", "", semver.MustParse("2.0.0"))

	r := Compare(a, b)

	require.Len(t, r.Changes, 1)
	assert.Equal(t, "metadata.version", r.Changes[0].Field)
	assert.Equal(t, "1.0.0", r.Changes[0].OldValue)
	assert.Equal(t, "2.0.0", r.Changes[0].NewValue)
}

func TestCompare_MetadataVersionBothNil(t *testing.T) {
	a := makeSolution("app", "", nil)
	b := makeSolution("app", "", nil)

	r := Compare(a, b)
	assert.Empty(t, r.Changes)
}

func TestCompare_MetadataVersionOneNil(t *testing.T) {
	// When one version is nil, the code only compares when both are non-nil,
	// so no change should be reported.
	a := makeSolution("app", "", semver.MustParse("1.0.0"))
	b := makeSolution("app", "", nil)

	r := Compare(a, b)
	assert.Empty(t, r.Changes)
}

// ── Compare – resolvers ─────────────────────────────────────────────

func TestCompare_ResolverAdded(t *testing.T) {
	a := makeSolution("app", "", nil)
	b := makeSolution("app", "", nil)
	b.Spec.Resolvers = map[string]*resolver.Resolver{
		"newRes": {Description: "new"},
	}

	r := Compare(a, b)

	require.Len(t, r.Changes, 1)
	assert.Equal(t, "spec.resolvers.newRes", r.Changes[0].Field)
	assert.Equal(t, "added", r.Changes[0].Type)
	assert.Equal(t, 1, r.Summary.Added)
}

func TestCompare_ResolverRemoved(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Resolvers = map[string]*resolver.Resolver{
		"oldRes": {Description: "old"},
	}
	b := makeSolution("app", "", nil)

	r := Compare(a, b)

	require.Len(t, r.Changes, 1)
	assert.Equal(t, "spec.resolvers.oldRes", r.Changes[0].Field)
	assert.Equal(t, "removed", r.Changes[0].Type)
	assert.Equal(t, 1, r.Summary.Removed)
}

func TestCompare_ResolverTypeChanged(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Resolvers = map[string]*resolver.Resolver{
		"res": {Type: "string"},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Resolvers = map[string]*resolver.Resolver{
		"res": {Type: "int"},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.resolvers.res.type" {
			found = true
			assert.Equal(t, "changed", c.Type)
			assert.Equal(t, "string", c.OldValue)
			assert.Equal(t, "int", c.NewValue)
		}
	}
	assert.True(t, found, "expected type change")
}

func TestCompare_ResolverDescriptionChanged(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Resolvers = map[string]*resolver.Resolver{
		"res": {Description: "old desc"},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Resolvers = map[string]*resolver.Resolver{
		"res": {Description: "new desc"},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.resolvers.res.description" {
			found = true
			assert.Equal(t, "changed", c.Type)
		}
	}
	assert.True(t, found, "expected description change")
}

func TestCompare_ResolverProviderChanged(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Resolvers = map[string]*resolver.Resolver{
		"res": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "parameter"}},
			},
		},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Resolvers = map[string]*resolver.Resolver{
		"res": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
		},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.resolvers.res.provider" {
			found = true
			assert.Equal(t, "changed", c.Type)
			assert.Equal(t, "parameter", c.OldValue)
			assert.Equal(t, "http", c.NewValue)
		}
	}
	assert.True(t, found, "expected provider change")
}

func TestCompare_ResolverProviderNilResolve(t *testing.T) {
	// Both resolvers have nil Resolve — provider comparison should show both as empty
	a := makeSolution("app", "", nil)
	a.Spec.Resolvers = map[string]*resolver.Resolver{
		"res": {},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Resolvers = map[string]*resolver.Resolver{
		"res": {},
	}

	r := Compare(a, b)
	// No provider change when both are empty
	for _, c := range r.Changes {
		assert.NotContains(t, c.Field, "provider")
	}
}

// ── Compare – actions ───────────────────────────────────────────────

func TestCompare_ActionAdded(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell"},
		},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.workflow.actions.deploy" && c.Type == "added" {
			found = true
		}
	}
	assert.True(t, found, "expected deploy action added")
}

func TestCompare_ActionRemoved(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"cleanup": {Provider: "shell"},
		},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.workflow.actions.cleanup" && c.Type == "removed" {
			found = true
		}
	}
	assert.True(t, found, "expected cleanup action removed")
}

func TestCompare_ActionProviderChanged(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell", Description: "d"},
		},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "http", Description: "d"},
		},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.workflow.actions.deploy.provider" {
			found = true
			assert.Equal(t, "changed", c.Type)
			assert.Equal(t, "shell", c.OldValue)
			assert.Equal(t, "http", c.NewValue)
		}
	}
	assert.True(t, found, "expected provider change")
}

func TestCompare_ActionDescriptionChanged(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell", Description: "old"},
		},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell", Description: "new"},
		},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.workflow.actions.deploy.description" {
			found = true
			assert.Equal(t, "changed", c.Type)
		}
	}
	assert.True(t, found, "expected description change")
}

// ── Compare – finally actions ───────────────────────────────────────

func TestCompare_FinallyAdded(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Workflow = &action.Workflow{
		Finally: map[string]*action.Action{},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Workflow = &action.Workflow{
		Finally: map[string]*action.Action{
			"cleanup": {Provider: "shell"},
		},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.workflow.finally.cleanup" && c.Type == "added" {
			found = true
		}
	}
	assert.True(t, found, "expected finally action added")
}

func TestCompare_FinallyRemoved(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Workflow = &action.Workflow{
		Finally: map[string]*action.Action{
			"notify": {Provider: "shell"},
		},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Workflow = &action.Workflow{
		Finally: map[string]*action.Action{},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.workflow.finally.notify" && c.Type == "removed" {
			found = true
		}
	}
	assert.True(t, found, "expected finally action removed")
}

// ── Compare – workflow added / removed ──────────────────────────────

func TestCompare_WorkflowAdded(t *testing.T) {
	a := makeSolution("app", "", nil)
	b := makeSolution("app", "", nil)
	b.Spec.Workflow = &action.Workflow{}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.workflow" && c.Type == "added" {
			found = true
		}
	}
	assert.True(t, found, "expected workflow added")
}

func TestCompare_WorkflowRemoved(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Workflow = &action.Workflow{}
	b := makeSolution("app", "", nil)

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.workflow" && c.Type == "removed" {
			found = true
		}
	}
	assert.True(t, found, "expected workflow removed")
}

// ── Compare – testing section ───────────────────────────────────────

func TestCompare_TestCaseAdded(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{
			"basic": {},
		},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.testing.cases.basic" && c.Type == "added" {
			found = true
		}
	}
	assert.True(t, found, "expected test case added")
}

func TestCompare_TestCaseRemoved(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{
			"old": {},
		},
	}
	b := makeSolution("app", "", nil)
	b.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{},
	}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.testing.cases.old" && c.Type == "removed" {
			found = true
		}
	}
	assert.True(t, found, "expected test case removed")
}

func TestCompare_TestingAdded(t *testing.T) {
	a := makeSolution("app", "", nil)
	b := makeSolution("app", "", nil)
	b.Spec.Testing = &soltesting.TestSuite{}

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.testing" && c.Type == "added" {
			found = true
		}
	}
	assert.True(t, found, "expected testing added")
}

func TestCompare_TestingRemoved(t *testing.T) {
	a := makeSolution("app", "", nil)
	a.Spec.Testing = &soltesting.TestSuite{}
	b := makeSolution("app", "", nil)

	r := Compare(a, b)

	found := false
	for _, c := range r.Changes {
		if c.Field == "spec.testing" && c.Type == "removed" {
			found = true
		}
	}
	assert.True(t, found, "expected testing removed")
}

func TestCompare_BothTestingNil(t *testing.T) {
	a := makeSolution("app", "", nil)
	b := makeSolution("app", "", nil)

	r := Compare(a, b)
	assert.Empty(t, r.Changes)
}

// ── Compare – sorting & summary ─────────────────────────────────────

func TestCompare_ChangesSortedByField(t *testing.T) {
	a := makeSolution("app", "old desc", nil)
	a.Spec.Resolvers = map[string]*resolver.Resolver{
		"z_res": {},
	}
	b := makeSolution("app", "new desc", nil)
	b.Spec.Resolvers = map[string]*resolver.Resolver{
		"a_res": {},
	}

	r := Compare(a, b)

	require.True(t, len(r.Changes) >= 2, "expected at least 2 changes")
	for i := 1; i < len(r.Changes); i++ {
		assert.True(t, r.Changes[i-1].Field <= r.Changes[i].Field,
			"changes not sorted: %s > %s", r.Changes[i-1].Field, r.Changes[i].Field)
	}
}

func TestCompare_SummaryCountsCorrect(t *testing.T) {
	a := makeSolution("alpha", "old", nil)
	a.Spec.Resolvers = map[string]*resolver.Resolver{
		"removed": {},
		"common":  {Description: "old"},
	}
	b := makeSolution("beta", "new", nil)
	b.Spec.Resolvers = map[string]*resolver.Resolver{
		"added":  {},
		"common": {Description: "new"},
	}

	r := Compare(a, b)

	// added: spec.resolvers.added
	// removed: spec.resolvers.removed
	// changed: metadata.name, metadata.description, spec.resolvers.common.description
	assert.Equal(t, 1, r.Summary.Added)
	assert.Equal(t, 1, r.Summary.Removed)
	assert.Equal(t, 3, r.Summary.Changed) // name + description + resolver desc
	assert.Equal(t, 5, r.Summary.Total)
}

// ── Compare – complex scenario ──────────────────────────────────────

func TestCompare_ComplexDiff(t *testing.T) {
	a := makeSolution("app", "v1", semver.MustParse("1.0.0"))
	a.Spec.Resolvers = map[string]*resolver.Resolver{
		"name":   {Type: "string", Description: "The name"},
		"region": {Type: "string"},
	}
	a.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell", Description: "deploy v1"},
			"test":   {Provider: "shell"},
		},
		Finally: map[string]*action.Action{
			"cleanup": {Provider: "shell"},
		},
	}
	a.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{
			"smoke": {},
		},
	}

	b := makeSolution("app", "v2", semver.MustParse("2.0.0"))
	b.Spec.Resolvers = map[string]*resolver.Resolver{
		"name": {Type: "string", Description: "Updated name"},
		"env":  {Type: "string"}, // added
		// region removed
	}
	b.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "http", Description: "deploy v2"}, // provider+desc changed
			// test removed
			"validate": {Provider: "shell"}, // added
		},
		Finally: map[string]*action.Action{
			"cleanup": {Provider: "shell"},
			"notify":  {Provider: "http"}, // added
		},
	}
	b.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{
			"smoke":      {},
			"regression": {}, // added
		},
	}

	r := Compare(a, b)

	assert.True(t, r.Summary.Total > 0, "expected changes")
	assert.True(t, r.Summary.Added > 0, "expected additions")
	assert.True(t, r.Summary.Removed > 0, "expected removals")
	assert.True(t, r.Summary.Changed > 0, "expected modifications")

	// Verify sorted
	for i := 1; i < len(r.Changes); i++ {
		assert.True(t, r.Changes[i-1].Field <= r.Changes[i].Field,
			"not sorted: %s > %s", r.Changes[i-1].Field, r.Changes[i].Field)
	}
}

// ── CompareFiles ────────────────────────────────────────────────────

func TestCompareFiles_BadPathA(t *testing.T) {
	_, err := CompareFiles(context.Background(), "/nonexistent/a.yaml", "/nonexistent/b.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading solution A")
}

// ── Benchmarks ──────────────────────────────────────────────────────

func BenchmarkCompare_MinimalSolutions(b *testing.B) {
	a := makeSolution("app", "desc", semver.MustParse("1.0.0"))
	sol := makeSolution("app", "desc", semver.MustParse("1.0.0"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Compare(a, sol)
	}
}

func BenchmarkCompare_ManyResolvers(b *testing.B) {
	a := makeSolution("app", "", nil)
	a.Spec.Resolvers = make(map[string]*resolver.Resolver, 50)
	bSol := makeSolution("app", "", nil)
	bSol.Spec.Resolvers = make(map[string]*resolver.Resolver, 50)

	for i := 0; i < 50; i++ {
		name := "resolver_" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		a.Spec.Resolvers[name] = &resolver.Resolver{
			Type:        "string",
			Description: "desc " + name,
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "parameter"}},
			},
		}
		bSol.Spec.Resolvers[name] = &resolver.Resolver{
			Type:        "int",
			Description: "updated " + name,
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Compare(a, bSol)
	}
}

func BenchmarkCompare_ManyActions(b *testing.B) {
	a := makeSolution("app", "", nil)
	a.Spec.Workflow = &action.Workflow{
		Actions: make(map[string]*action.Action, 30),
		Finally: make(map[string]*action.Action, 10),
	}
	bSol := makeSolution("app", "", nil)
	bSol.Spec.Workflow = &action.Workflow{
		Actions: make(map[string]*action.Action, 30),
		Finally: make(map[string]*action.Action, 10),
	}

	for i := 0; i < 30; i++ {
		name := "action_" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		a.Spec.Workflow.Actions[name] = &action.Action{Provider: "shell", Description: "old"}
		bSol.Spec.Workflow.Actions[name] = &action.Action{Provider: "http", Description: "new"}
	}
	for i := 0; i < 10; i++ {
		name := "finally_" + string(rune('A'+i%26))
		a.Spec.Workflow.Finally[name] = &action.Action{Provider: "shell"}
		bSol.Spec.Workflow.Finally[name] = &action.Action{Provider: "http"}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Compare(a, bSol)
	}
}

func TestCompareFiles_InvalidPathA(t *testing.T) {
	ctx := context.Background()
	_, err := CompareFiles(ctx, "/nonexistent/pathA.yaml", "/nonexistent/pathB.yaml")
	require.Error(t, err)
}

func TestCompareFiles_InvalidPathB(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	pathA := tmpDir + "/solA.yaml"
	content := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sol-a
  version: "1.0.0"
`)
	require.NoError(t, os.WriteFile(pathA, content, 0o600))
	_, err := CompareFiles(ctx, pathA, "/nonexistent/pathB.yaml")
	require.Error(t, err)
}

func TestCompareFiles_Valid(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	solContent := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-sol
  version: "1.0.0"
`)
	pathA := tmpDir + "/solA.yaml"
	pathB := tmpDir + "/solB.yaml"
	require.NoError(t, os.WriteFile(pathA, solContent, 0o600))
	require.NoError(t, os.WriteFile(pathB, solContent, 0o600))

	result, err := CompareFiles(ctx, pathA, pathB)
	require.NoError(t, err)
	assert.Equal(t, pathA, result.PathA)
	assert.Equal(t, pathB, result.PathB)
}
