// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package effective

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSolution builds a solution with both resolvers and a workflow for
// projection tests. It uses UnmarshalFromBytes to avoid the provider-validation
// gate so the fixture stays self-contained.
func testSolution(t *testing.T) *solution.Solution {
	t.Helper()
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: effective-test
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
  workflow:
    actions:
      deploy:
        provider: shell
        inputs:
          command: "echo deploy"
    finally:
      cleanup:
        provider: shell
        inputs:
          command: "echo cleanup"
`))
	require.NoError(t, err)
	return sol
}

func TestRender_NilSolution(t *testing.T) {
	_, err := Render(nil, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "solution is nil")
}

func TestRender_SectionAll_DefaultsToYAML(t *testing.T) {
	sol := testSolution(t)

	out, err := Render(sol, Options{})
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "name: effective-test")
	assert.Contains(t, text, "workflow:")
	assert.Contains(t, text, "deploy:")
	assert.Contains(t, text, "env:")
}

func TestRender_SectionAll_JSON(t *testing.T) {
	sol := testSolution(t)

	out, err := Render(sol, Options{Format: FormatJSON})
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, `"name": "effective-test"`)
	// Pretty-printed JSON is indented.
	assert.Contains(t, text, "\n  ")
}

func TestRender_SectionAll_JSONCompact(t *testing.T) {
	sol := testSolution(t)

	out, err := Render(sol, Options{Format: FormatJSON, Compact: true})
	require.NoError(t, err)

	assert.NotContains(t, string(out), "\n  ", "compact JSON should not be indented")
}

func TestRender_SectionWorkflow(t *testing.T) {
	sol := testSolution(t)

	out, err := Render(sol, Options{Section: SectionWorkflow})
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "deploy:")
	assert.Contains(t, text, "cleanup:")
	// Workflow projection must not carry the resolvers section.
	assert.NotContains(t, text, "env:")
}

func TestRender_SectionWorkflow_NoWorkflow(t *testing.T) {
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-workflow
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
`)))

	_, err := Render(sol, Options{Section: SectionWorkflow})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not define a workflow")
}

func TestRender_SectionResolvers(t *testing.T) {
	sol := testSolution(t)

	out, err := Render(sol, Options{Section: SectionResolvers})
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "env:")
	// Resolver projection must not carry the workflow section.
	assert.NotContains(t, text, "deploy:")
}

func TestRender_SectionResolvers_NoResolvers(t *testing.T) {
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-resolvers
spec:
  workflow:
    actions:
      deploy:
        provider: shell
        inputs:
          command: "echo deploy"
`)))

	_, err := Render(sol, Options{Section: SectionResolvers})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not define any resolvers")
}

func TestRender_InvalidSection(t *testing.T) {
	sol := testSolution(t)

	_, err := Render(sol, Options{Section: Section("bogus")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid section")
}

func TestRender_UnsupportedFormat(t *testing.T) {
	sol := testSolution(t)

	_, err := Render(sol, Options{Format: Format("toml")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

// TestRender_Deterministic guards the core promise: identical input yields
// byte-identical output across runs, which is what makes the output safe as a
// golden file for fidelity diffing.
func TestRender_Deterministic(t *testing.T) {
	for _, section := range []Section{SectionAll, SectionWorkflow, SectionResolvers} {
		for _, format := range []Format{FormatYAML, FormatJSON} {
			sol := testSolution(t)
			first, err := Render(sol, Options{Section: section, Format: format})
			require.NoError(t, err)
			second, err := Render(sol, Options{Section: section, Format: format})
			require.NoError(t, err)
			assert.Equal(t, first, second, "section=%s format=%s should be deterministic", section, format)
		}
	}
}

func BenchmarkRender(b *testing.B) {
	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bench
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
  workflow:
    actions:
      deploy:
        provider: shell
        inputs:
          command: "echo deploy"
`)); err != nil {
		b.Fatalf("failed to build fixture: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(sol, Options{Format: FormatYAML}); err != nil {
			b.Fatal(err)
		}
	}
}
