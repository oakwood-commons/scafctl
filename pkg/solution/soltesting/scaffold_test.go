// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaffold_EmptyInput(t *testing.T) {
	result := Scaffold(&ScaffoldInput{})

	require.NotNil(t, result)
	assert.Len(t, result.Cases, 3, "should contain resolve-defaults, render-defaults, and lint")
	assert.Contains(t, result.Cases, "resolve-defaults")
	assert.Contains(t, result.Cases, "render-defaults")
	assert.Contains(t, result.Cases, "lint")
}

func TestScaffold_WithResolvers(t *testing.T) {
	input := &ScaffoldInput{
		Resolvers: map[string]*resolver.Resolver{
			"repo": {
				Description: "Repository name",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "static"},
					},
				},
			},
			"version": {
				Description: "Version to build",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "parameter"},
						{Provider: "static"},
					},
				},
				Validate: &resolver.ValidatePhase{
					With: []resolver.ProviderValidation{
						{
							Provider: "validation",
							Inputs: map[string]*spec.ValueRef{
								"match": {Literal: `^(dev|\d+\.\d+\.\d+.*)$`},
							},
						},
					},
				},
			},
		},
	}

	result := Scaffold(input)

	require.NotNil(t, result)

	// 3 base tests + 2 resolver tests + 1 validation failure test = 6
	assert.Len(t, result.Cases, 6)

	// Base tests
	assert.Contains(t, result.Cases, "resolve-defaults")
	assert.Contains(t, result.Cases, "render-defaults")
	assert.Contains(t, result.Cases, "lint")

	// Resolver tests
	assert.Contains(t, result.Cases, "resolver-repo")
	assert.Contains(t, result.Cases, "resolver-version")

	// Validation failure test for version
	assert.Contains(t, result.Cases, "resolver-version-invalid")
	assert.True(t, result.Cases["resolver-version-invalid"].ExpectFailure)
	assert.Contains(t, result.Cases["resolver-version-invalid"].Tags, "negative")
}

func TestScaffold_WithActions(t *testing.T) {
	input := &ScaffoldInput{
		Workflow: &action.Workflow{
			Actions: map[string]*action.Action{
				"build": {
					Description: "Build binary",
					Provider:    "exec",
				},
				"test": {
					Description: "Run tests",
					Provider:    "exec",
				},
			},
		},
	}

	result := Scaffold(input)

	require.NotNil(t, result)

	// 3 base tests + 2 action tests = 5
	assert.Len(t, result.Cases, 5)
	assert.Contains(t, result.Cases, "action-build")
	assert.Contains(t, result.Cases, "action-test")

	// Action tests should include provider tag
	assert.Contains(t, result.Cases["action-build"].Tags, "exec")
	assert.Contains(t, result.Cases["action-build"].Tags, "actions")

	// Action name should be a positional arg, not a flag
	assert.Equal(t, []string{"build"}, result.Cases["action-build"].Args)
	assert.Equal(t, []string{"test"}, result.Cases["action-test"].Args)
}

func TestScaffold_ConditionalAction(t *testing.T) {
	input := &ScaffoldInput{
		Workflow: &action.Workflow{
			Actions: map[string]*action.Action{
				"release": {
					Description: "Create release",
					Provider:    "api",
					When: &spec.Condition{
						Expr: exprPtr(`_.version != "dev"`),
					},
				},
			},
		},
	}

	result := Scaffold(input)

	require.NotNil(t, result)
	assert.Contains(t, result.Cases, "action-release")
	assert.Contains(t, result.Cases["action-release"].Tags, "conditional")
}

func TestScaffold_ResolverWithValidationExpression(t *testing.T) {
	input := &ScaffoldInput{
		Resolvers: map[string]*resolver.Resolver{
			"goos": {
				Description: "Target OS",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "static"},
					},
				},
				Validate: &resolver.ValidatePhase{
					With: []resolver.ProviderValidation{
						{
							Provider: "validation",
							Inputs: map[string]*spec.ValueRef{
								"expression": {Literal: `__self in ["linux", "darwin", "windows"]`},
							},
						},
					},
				},
			},
		},
	}

	result := Scaffold(input)

	assert.Contains(t, result.Cases, "resolver-goos-invalid")
	tc := result.Cases["resolver-goos-invalid"]
	assert.True(t, tc.ExpectFailure)
	assert.Contains(t, tc.Description, "expression")
}

func TestScaffold_DeterministicOrder(t *testing.T) {
	input := &ScaffoldInput{
		Resolvers: map[string]*resolver.Resolver{
			"zulu":  {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}},
			"alpha": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}},
			"mike":  {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}},
		},
	}

	result1 := Scaffold(input)
	result2 := Scaffold(input)

	yaml1, err1 := ScaffoldToYAML(result1)
	require.NoError(t, err1)
	yaml2, err2 := ScaffoldToYAML(result2)
	require.NoError(t, err2)

	assert.Equal(t, string(yaml1), string(yaml2), "scaffold output should be deterministic")
}

func TestScaffoldToYAML_ContainsExpectedContent(t *testing.T) {
	input := &ScaffoldInput{
		Resolvers: map[string]*resolver.Resolver{
			"repo": {
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "static"},
					},
				},
			},
		},
	}

	result := Scaffold(input)
	out, err := ScaffoldToYAML(result)

	require.NoError(t, err)
	assert.Contains(t, string(out), "testing:")
	assert.Contains(t, string(out), "cases:")
	assert.Contains(t, string(out), "resolve-defaults")
	assert.Contains(t, string(out), "resolver-repo")
}

func exprPtr(s string) *celexp.Expression {
	e := celexp.Expression(s)
	return &e
}

func TestScaffold_FileDependenciesUseTemplate(t *testing.T) {
	input := &ScaffoldInput{
		Resolvers: map[string]*resolver.Resolver{
			"region": {
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "static"},
					},
				},
			},
		},
		FileDependencies: []string{"templates/main.yaml", "data/config.json"},
	}

	result := Scaffold(input)

	// Multiple cases => _files-base template should be generated
	require.Contains(t, result.Cases, "_files-base")
	tmpl := result.Cases["_files-base"]
	assert.Equal(t, []string{"data/config.json", "templates/main.yaml"}, tmpl.Files)

	// All non-template cases should extend _files-base and have no inline files
	for name, tc := range result.Cases {
		if name == "_files-base" {
			continue
		}
		assert.Equal(t, []string{"_files-base"}, tc.Extends,
			"test case %q should extend _files-base", name)
		assert.Nil(t, tc.Files,
			"test case %q should not have inline files", name)
	}
}

func TestScaffold_NormalizeBackslashPaths(t *testing.T) {
	input := &ScaffoldInput{
		FileDependencies: []string{
			"templates\\.github\\copilot.md.tpl",
			"templates\\.github\\instructions\\terraform.md.tpl",
		},
	}

	result := Scaffold(input)

	// Single case (3 builtins) => template used; check paths are normalized
	tmpl := result.Cases["_files-base"]
	require.NotNil(t, tmpl)
	for _, f := range tmpl.Files {
		assert.NotContains(t, f, "\\", "paths should use forward slashes: %s", f)
	}
}

func TestScaffold_EmptyFileDependenciesOmitted(t *testing.T) {
	result := Scaffold(&ScaffoldInput{})

	for name, tc := range result.Cases {
		assert.Nil(t, tc.Files,
			"test case %q should have nil Files when no dependencies discovered", name)
	}
}
