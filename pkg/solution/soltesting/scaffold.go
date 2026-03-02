// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"gopkg.in/yaml.v3"
)

// ScaffoldResult holds the generated test scaffold for a solution.
type ScaffoldResult struct {
	// Cases is a map of generated test cases keyed by name.
	Cases map[string]*TestCase `json:"cases" yaml:"cases"`
}

// ScaffoldInput provides the solution data needed for scaffold generation,
// avoiding a direct dependency on the solution package (which imports soltesting).
type ScaffoldInput struct {
	// Resolvers is the map of resolver definitions from the solution spec.
	Resolvers map[string]*resolver.Resolver

	// Workflow is the action workflow from the solution spec (may be nil).
	Workflow *action.Workflow

	// FileDependencies is a list of file paths (relative to the solution dir)
	// discovered through static analysis of provider inputs.
	// These are automatically populated onto generated test cases.
	FileDependencies []string
}

// Scaffold generates a skeleton test suite from the provided solution data.
// It performs structural analysis only — no commands are executed.
func Scaffold(input *ScaffoldInput) *ScaffoldResult {
	result := &ScaffoldResult{
		Cases: make(map[string]*TestCase),
	}

	// Always generate builtin-style tests
	addResolveDefaultsTest(result)
	addRenderDefaultsTest(result)
	addLintTest(result)

	// Generate resolver-specific tests
	if input.Resolvers != nil {
		addResolverTests(result, input.Resolvers)
	}

	// Generate action-specific tests
	if input.Workflow != nil && input.Workflow.Actions != nil {
		addActionTests(result, input.Workflow)
	}

	// Auto-populate discovered file dependencies on all generated test cases
	if len(input.FileDependencies) > 0 {
		sorted := make([]string, len(input.FileDependencies))
		copy(sorted, input.FileDependencies)
		sort.Strings(sorted)

		for _, tc := range result.Cases {
			tc.Files = sorted
		}
	}

	return result
}

// addResolveDefaultsTest adds a test that verifies all resolvers resolve with defaults.
func addResolveDefaultsTest(result *ScaffoldResult) {
	exitCodeZero := 0
	result.Cases["resolve-defaults"] = &TestCase{
		Description: "Verify all resolvers resolve with default values",
		Command:     []string{"run", "resolver"},
		Args:        []string{"-o", "json"},
		Tags:        []string{"smoke", "resolvers"},
		ExitCode:    &exitCodeZero,
	}
}

// addRenderDefaultsTest adds a test that verifies the solution renders with defaults.
func addRenderDefaultsTest(result *ScaffoldResult) {
	exitCodeZero := 0
	result.Cases["render-defaults"] = &TestCase{
		Description: "Verify solution renders with default values",
		Command:     []string{"render", "solution"},
		Tags:        []string{"smoke", "render"},
		ExitCode:    &exitCodeZero,
	}
}

// addLintTest adds a lint test.
func addLintTest(result *ScaffoldResult) {
	exitCodeZero := 0
	result.Cases["lint"] = &TestCase{
		Description: "Verify solution has no lint errors",
		Command:     []string{"lint"},
		Tags:        []string{"smoke", "lint"},
		ExitCode:    &exitCodeZero,
	}
}

// addResolverTests generates per-resolver tests, including validation failure tests.
func addResolverTests(result *ScaffoldResult, resolvers map[string]*resolver.Resolver) {
	// Sort resolver names for deterministic output
	names := make([]string, 0, len(resolvers))
	for name := range resolvers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		r := resolvers[name]
		if r == nil {
			continue
		}

		// Generate a basic resolver output test
		exitCodeZero := 0
		testName := fmt.Sprintf("resolver-%s", name)
		result.Cases[testName] = &TestCase{
			Description: fmt.Sprintf("Verify resolver %q produces expected output", name),
			Command:     []string{"run", "resolver"},
			Args:        []string{"--resolver", name, "-o", "json"},
			Tags:        []string{"resolvers"},
			ExitCode:    &exitCodeZero,
			Assertions: []Assertion{
				{
					Expression: celexp.Expression(fmt.Sprintf(`__output.%s != null`, name)),
					Message:    fmt.Sprintf("Resolver %q should produce a non-null value", name),
				},
			},
		}

		// If the resolver has validation rules, generate an expectFailure test
		if r.Validate != nil && len(r.Validate.With) > 0 {
			failTestName := fmt.Sprintf("resolver-%s-invalid", name)
			tc := &TestCase{
				Description:   fmt.Sprintf("Verify resolver %q rejects invalid input", name),
				Command:       []string{"run", "resolver"},
				Args:          []string{"--resolver", name},
				Tags:          []string{"resolvers", "validation", "negative"},
				ExpectFailure: true,
			}

			// Try to generate a meaningful invalid input based on validation provider inputs
			for _, pv := range r.Validate.With {
				if pv.Inputs != nil {
					if matchRef, ok := pv.Inputs["match"]; ok && matchRef != nil {
						tc.Description = fmt.Sprintf("Verify resolver %q rejects values not matching pattern", name)
					}
					if exprRef, ok := pv.Inputs["expression"]; ok && exprRef != nil {
						tc.Description = fmt.Sprintf("Verify resolver %q rejects values failing validation expression", name)
					}
				}
			}

			// Add parameter override with obviously invalid value
			tc.Args = append(tc.Args, "--param", fmt.Sprintf("%s=___invalid___", name))
			result.Cases[failTestName] = tc
		}
	}
}

// addActionTests generates skeleton tests for workflow actions.
func addActionTests(result *ScaffoldResult, wf *action.Workflow) {
	// Sort action names for deterministic output
	names := make([]string, 0, len(wf.Actions))
	for name := range wf.Actions {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		act := wf.Actions[name]
		if act == nil {
			continue
		}

		testName := fmt.Sprintf("action-%s", name)
		tc := &TestCase{
			Description: fmt.Sprintf("Verify action %q executes successfully", name),
			Command:     []string{"run", "action"},
			Args:        []string{"--action", name},
			Tags:        []string{"actions"},
		}

		if act.Provider != "" {
			tc.Tags = append(tc.Tags, act.Provider)
		}

		// For actions with conditions, add a note in the description
		if act.When != nil {
			tc.Description = fmt.Sprintf("Verify action %q executes when condition is met (provider: %s)", name, act.Provider)
			tc.Tags = append(tc.Tags, "conditional")
		}

		exitCodeZero := 0
		tc.ExitCode = &exitCodeZero
		result.Cases[testName] = tc
	}
}

// ScaffoldToYAML marshals the scaffold result to YAML suitable for embedding
// in a solution's spec.testing.cases section.
func ScaffoldToYAML(result *ScaffoldResult) ([]byte, error) {
	// Build a wrapper that produces spec-level YAML: testing: { cases: { ... } }
	wrapper := map[string]any{
		"testing": map[string]any{
			"cases": result.Cases,
		},
	}
	return yaml.Marshal(wrapper)
}
