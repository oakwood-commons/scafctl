// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// singleHyphenSolution defines one hyphenated resolver with no references.
const singleHyphenSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: t
spec:
  resolvers:
    # a hyphenated resolver
    my-service-name:
      description: the service
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`

// multiRefHyphenSolution references the hyphenated resolver via dependsOn, a
// rslvr value ref, and a CEL bracket expression (dot notation is invalid CEL
// for hyphenated names, so bracket notation is the only in-expression form).
const multiRefHyphenSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: t
spec:
  resolvers:
    my-service-name:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
    consumer:
      dependsOn:
        - my-service-name
      resolve:
        with:
          - provider: static
            inputs:
              value:
                rslvr: my-service-name
    celuser:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                expr: '_["my-service-name"] + "x"'
`

// collisionSolution renames my-service -> myService, which already exists.
const collisionSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: t
spec:
  resolvers:
    my-service:
      resolve:
        with:
          - provider: static
            inputs:
              value: a
    myService:
      resolve:
        with:
          - provider: static
            inputs:
              value: b
`

// noFixableSolution has no hyphenated resolvers.
const noFixableSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: t
spec:
  resolvers:
    myServiceName:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`

func TestComputeFixPlan_SingleHyphenated(t *testing.T) {
	plan, err := ComputeFixPlan([]byte(singleHyphenSolution), "test.yaml", provider.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, plan)

	assert.True(t, plan.Changed)
	assert.Equal(t, 1, plan.AppliedCount())
	assert.Equal(t, 0, plan.SkippedCount())
	require.Len(t, plan.Outcomes, 1)
	assert.Equal(t, "hyphenated-name", plan.Outcomes[0].RuleName)
	assert.Equal(t, "resolvers.my-service-name", plan.Outcomes[0].Location)
	assert.True(t, plan.Outcomes[0].Applied)

	out := string(plan.NewContent)
	assert.Contains(t, out, "myServiceName:")
	assert.NotContains(t, out, "my-service-name:")
	// Comment must be preserved (byte-exact edits, no YAML round-trip).
	assert.Contains(t, out, "# a hyphenated resolver")
}

func TestComputeFixPlan_MultipleReferences(t *testing.T) {
	plan, err := ComputeFixPlan([]byte(multiRefHyphenSolution), "test.yaml", provider.NewRegistry())
	require.NoError(t, err)

	assert.True(t, plan.Changed)
	assert.Equal(t, 1, plan.AppliedCount())

	out := string(plan.NewContent)
	// Definition, dependsOn, rslvr, and CEL bracket must all be rewritten.
	assert.NotContains(t, out, "my-service-name")
	assert.Contains(t, out, "myServiceName:")
	assert.Contains(t, out, "- myServiceName")
	assert.Contains(t, out, "rslvr: myServiceName")
	assert.Contains(t, out, `_["myServiceName"]`)

	// Detail reports the reference count (definition + 3 refs == 4).
	assert.Contains(t, plan.Outcomes[0].Detail, "4 reference(s)")
}

func TestComputeFixPlan_CollisionSkipped(t *testing.T) {
	plan, err := ComputeFixPlan([]byte(collisionSolution), "test.yaml", provider.NewRegistry())
	require.NoError(t, err)

	assert.False(t, plan.Changed)
	assert.Equal(t, 0, plan.AppliedCount())
	require.Len(t, plan.Outcomes, 1)
	assert.False(t, plan.Outcomes[0].Applied)
	assert.Contains(t, plan.Outcomes[0].Detail, ErrNotFixable.Error())
	// The original bytes are returned unchanged.
	assert.Equal(t, collisionSolution, string(plan.NewContent))
}

func TestComputeFixPlan_NoFixableFindings(t *testing.T) {
	plan, err := ComputeFixPlan([]byte(noFixableSolution), "test.yaml", provider.NewRegistry())
	require.NoError(t, err)

	assert.False(t, plan.Changed)
	assert.Empty(t, plan.Outcomes)
	assert.Equal(t, noFixableSolution, string(plan.NewContent))
}

func TestComputeFixPlan_Idempotent(t *testing.T) {
	first, err := ComputeFixPlan([]byte(multiRefHyphenSolution), "test.yaml", provider.NewRegistry())
	require.NoError(t, err)
	require.True(t, first.Changed)

	second, err := ComputeFixPlan(first.NewContent, "test.yaml", provider.NewRegistry())
	require.NoError(t, err)
	assert.False(t, second.Changed, "re-running fix on already-fixed content must be a no-op")
	assert.Empty(t, second.Outcomes)
}

func TestComputeFixPlan_ParseError(t *testing.T) {
	_, err := ComputeFixPlan([]byte("this: [is: not: valid: yaml"), "test.yaml", provider.NewRegistry())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse solution")
}

func TestFixPlan_UnifiedDiff(t *testing.T) {
	plan, err := ComputeFixPlan([]byte(singleHyphenSolution), "test.yaml", provider.NewRegistry())
	require.NoError(t, err)
	require.True(t, plan.Changed)

	diff, err := plan.UnifiedDiff("test.yaml", []byte(singleHyphenSolution))
	require.NoError(t, err)
	assert.Contains(t, diff, "--- a/test.yaml")
	assert.Contains(t, diff, "+++ b/test.yaml")
	assert.Contains(t, diff, "@@")
	assert.Contains(t, diff, "-    my-service-name:")
	assert.Contains(t, diff, "+    myServiceName:")
}

func TestFixPlan_UnifiedDiff_NoChange(t *testing.T) {
	plan, err := ComputeFixPlan([]byte(noFixableSolution), "test.yaml", provider.NewRegistry())
	require.NoError(t, err)

	diff, err := plan.UnifiedDiff("test.yaml", []byte(noFixableSolution))
	require.NoError(t, err)
	assert.Empty(t, diff)
}

func TestFixableRegistry(t *testing.T) {
	assert.True(t, Fixable("hyphenated-name"))
	assert.False(t, Fixable("unused-resolver"))
	assert.False(t, Fixable("nonexistent-rule"))

	names := FixableRuleNames()
	assert.Contains(t, names, "hyphenated-name")
	// FixableRuleNames must be sorted.
	sorted := append([]string(nil), names...)
	require.True(t, isSorted(sorted))
}

func isSorted(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

func TestResolverNameFromLocation(t *testing.T) {
	tests := []struct {
		name     string
		location string
		wantName string
		wantOK   bool
	}{
		{name: "valid", location: "resolvers.my-svc", wantName: "my-svc", wantOK: true},
		{name: "no prefix", location: "actions.deploy", wantOK: false},
		{name: "empty name", location: "resolvers.", wantOK: false},
		{name: "nested path", location: "resolvers.my-svc.resolve", wantOK: false},
		{name: "empty string", location: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolverNameFromLocation(tt.location)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantName, got)
			}
		})
	}
}

func BenchmarkComputeFixPlan(b *testing.B) {
	raw := []byte(multiRefHyphenSolution)
	reg := provider.NewRegistry()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ComputeFixPlan(raw, "bench.yaml", reg); err != nil {
			b.Fatal(err)
		}
	}
}
