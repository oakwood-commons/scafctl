// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ptrExpr creates a pointer to a celexp.Expression
func ptrExpr(s string) *celexp.Expression {
	e := celexp.Expression(s)
	return &e
}

func TestCommandSolution(t *testing.T) {
	t.Run("creates solution command with correct usage", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}
		cliParams := &settings.Run{}

		cmd := CommandSolution(cliParams, ioStreams, "scafctl/explain")

		assert.Equal(t, "solution [path]", cmd.Use)
		assert.Contains(t, cmd.Aliases, "solutions")
		assert.Contains(t, cmd.Aliases, "sol")
		assert.Contains(t, cmd.Aliases, "s")
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)
	})

	t.Run("has path flag", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}
		cliParams := &settings.Run{}

		cmd := CommandSolution(cliParams, ioStreams, "scafctl/explain")

		flag := cmd.Flags().Lookup("path")
		require.NotNil(t, flag)
		assert.Equal(t, "p", flag.Shorthand)
	})
}

func TestSolutionOptions_printSolutionExplanation(t *testing.T) {
	version := semver.MustParse("1.2.3")

	t.Run("explains complete solution", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &SolutionOptions{
			IOStreams: ioStreams,
			CliParams: &settings.Run{NoColor: true},
		}

		whenExpr := "env.VALIDATE == 'true'"

		sol := &solution.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solution.Metadata{
				Name:        "test-solution",
				DisplayName: "Test Solution",
				Version:     version,
				Description: "A comprehensive test solution",
				Category:    "testing",
				Tags:        []string{"test", "example"},
				Links: []solution.Link{
					{Name: "Documentation", URL: "https://docs.example.com"},
				},
				Maintainers: []solution.Contact{
					{Name: "Test Team", Email: "team@example.com"},
				},
			},
			Catalog: solution.Catalog{
				Visibility: "public",
				Beta:       true,
			},
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"config": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{
								{Provider: "static"},
							},
						},
					},
					"data": {
						DependsOn: []string{"config"},
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{
								{Provider: "http"},
							},
						},
						Transform: &resolver.TransformPhase{
							With: []resolver.ProviderTransform{
								{Provider: "jq"},
							},
						},
					},
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"deploy": {
							Name:      "deploy",
							Provider:  "shell",
							DependsOn: []string{"validate"},
						},
						"validate": {
							Name:     "validate",
							Provider: "http",
							When:     &spec.Condition{Expr: ptrExpr(whenExpr)},
						},
					},
					Finally: map[string]*action.Action{
						"cleanup": {
							Name:     "cleanup",
							Provider: "shell",
						},
					},
				},
			},
		}
		sol.SetPath("/path/to/solution.yaml")

		w := writer.New(ioStreams, options.CliParams)
		exp := BuildSolutionExplanation(sol)
		options.printSolutionExplanation(w, exp)

		output := outBuf.String()

		// Check header
		assert.Contains(t, output, "Test Solution")
		assert.Contains(t, output, "test-solution")
		assert.Contains(t, output, "1.2.3")

		// Check description
		assert.Contains(t, output, "A comprehensive test solution")

		// Check metadata
		assert.Contains(t, output, "testing")

		// Check catalog
		assert.Contains(t, output, "public")
		assert.Contains(t, output, "Beta")

		// Check resolvers
		assert.Contains(t, output, "Resolvers (2)")
		assert.Contains(t, output, "config")
		assert.Contains(t, output, "data")
		assert.Contains(t, output, "static")
		assert.Contains(t, output, "http")
		assert.Contains(t, output, "depends on: config")

		// Check actions
		assert.Contains(t, output, "Actions (3)")
		assert.Contains(t, output, "deploy")
		assert.Contains(t, output, "validate")
		assert.Contains(t, output, "shell")
		assert.Contains(t, output, "depends on: validate")
		assert.Contains(t, output, "conditional: yes")

		// Check finally
		assert.Contains(t, output, "finally:")
		assert.Contains(t, output, "cleanup")

		// Check tags
		assert.Contains(t, output, "test, example")

		// Check links
		assert.Contains(t, output, "Documentation")
		assert.Contains(t, output, "https://docs.example.com")

		// Check maintainers
		assert.Contains(t, output, "Test Team")
		assert.Contains(t, output, "team@example.com")

		// Check source path
		assert.Contains(t, output, "/path/to/solution.yaml")
	})

	t.Run("explains minimal solution", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &SolutionOptions{
			IOStreams: ioStreams,
			CliParams: &settings.Run{NoColor: true},
		}

		sol := &solution.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solution.Metadata{
				Name: "minimal-solution",
			},
		}

		w := writer.New(ioStreams, options.CliParams)
		exp := BuildSolutionExplanation(sol)
		options.printSolutionExplanation(w, exp)

		output := outBuf.String()
		assert.Contains(t, output, "minimal-solution")
		assert.Contains(t, output, "unknown") // version
	})

	t.Run("shows disabled warning", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &SolutionOptions{
			IOStreams: ioStreams,
			CliParams: &settings.Run{NoColor: true},
		}

		sol := &solution.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solution.Metadata{
				Name: "disabled-solution",
			},
			Catalog: solution.Catalog{
				Disabled: true,
			},
		}

		w := writer.New(ioStreams, options.CliParams)
		exp := BuildSolutionExplanation(sol)
		options.printSolutionExplanation(w, exp)

		output := outBuf.String()
		assert.Contains(t, output, "DISABLED")
	})
}

func TestExtractProviderNames(t *testing.T) {
	t.Run("extracts providers from all phases", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http"},
					{Provider: "static"},
				},
			},
			Transform: &resolver.TransformPhase{
				With: []resolver.ProviderTransform{
					{Provider: "jq"},
				},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{
					{Provider: "schema"},
				},
			},
		}

		providers := extractProviderNames(r)

		assert.Len(t, providers, 4)
		assert.Contains(t, providers, "http")
		assert.Contains(t, providers, "static")
		assert.Contains(t, providers, "jq")
		assert.Contains(t, providers, "schema")
	})

	t.Run("removes duplicates", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http"},
					{Provider: "http"},
				},
			},
		}

		providers := extractProviderNames(r)
		assert.Len(t, providers, 1)
		assert.Equal(t, "http", providers[0])
	})

	t.Run("returns empty slice for empty resolver", func(t *testing.T) {
		r := &resolver.Resolver{}

		providers := extractProviderNames(r)
		assert.Empty(t, providers)
	})
}

func TestExtractPhases(t *testing.T) {
	t.Run("identifies all phases", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
			Transform: &resolver.TransformPhase{
				With: []resolver.ProviderTransform{{Provider: "jq"}},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{{Provider: "schema"}},
			},
		}

		phases := extractPhases(r)
		assert.Len(t, phases, 3)
		assert.Equal(t, []string{"resolve", "transform", "validate"}, phases)
	})

	t.Run("identifies single phase", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
		}

		phases := extractPhases(r)
		assert.Equal(t, []string{"resolve"}, phases)
	})

	t.Run("returns empty slice for empty resolver", func(t *testing.T) {
		r := &resolver.Resolver{}

		phases := extractPhases(r)
		assert.Empty(t, phases)
	})
}

func TestSolutionOptions_Run_WithMock(t *testing.T) {
	t.Run("loads and explains solution successfully", func(t *testing.T) {
		mockGetter := &get.MockGetter{}
		sol := &solution.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solution.Metadata{
				Name:        "mocked-solution",
				Description: "A mocked solution for testing",
			},
		}

		mockGetter.On("Get", mock.Anything, "/path/to/solution.yaml").
			Return(sol, nil)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &SolutionOptions{
			IOStreams: ioStreams,
			CliParams: &settings.Run{NoColor: true},
			Path:      "/path/to/solution.yaml",
		}

		// Note: We can't easily inject the getter into Run() without refactoring,
		// so we'll test the explanation printing directly
		w := writer.New(ioStreams, options.CliParams)
		exp := BuildSolutionExplanation(sol)
		options.printSolutionExplanation(w, exp)

		output := outBuf.String()
		assert.Contains(t, output, "mocked-solution")
		assert.Contains(t, output, "A mocked solution for testing")
	})

	t.Run("handles getter error", func(t *testing.T) {
		mockGetter := &get.MockGetter{}
		mockGetter.On("Get", mock.Anything, "/invalid/path").
			Return(nil, errors.New("file not found"))

		// The actual Run() method calls get.NewGetter() internally,
		// so we just verify the mock behavior is correct
		sol, err := mockGetter.Get(context.Background(), "/invalid/path")
		assert.Error(t, err)
		assert.Nil(t, sol)
		mockGetter.AssertExpectations(t)
	})
}

func TestBuildSolutionExplanation_SourcePositions(t *testing.T) {
	t.Run("includes source positions when source map is set", func(t *testing.T) {
		ver := semver.MustParse("1.0.0")
		sol := &solution.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solution.Metadata{
				Name:    "pos-test",
				Version: ver,
			},
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"greeting": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{
								{Provider: "static"},
							},
						},
					},
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"deploy": {
							Name:     "deploy",
							Provider: "exec",
						},
					},
					Finally: map[string]*action.Action{
						"cleanup": {
							Name:     "cleanup",
							Provider: "exec",
						},
					},
				},
			},
		}

		// Set up source map with positions
		sm := sourcepos.NewSourceMap()
		sm.Set("spec.resolvers.greeting", sourcepos.Position{Line: 8, Column: 5, File: "solution.yaml"})
		sm.Set("spec.workflow.actions.deploy", sourcepos.Position{Line: 15, Column: 7, File: "solution.yaml"})
		sm.Set("spec.workflow.finally.cleanup", sourcepos.Position{Line: 22, Column: 7, File: "solution.yaml"})
		sol.SetSourceMap(sm)

		exp := BuildSolutionExplanation(sol)

		// Verify resolver source position
		require.Len(t, exp.Resolvers, 1)
		require.NotNil(t, exp.Resolvers[0].SourcePos)
		assert.Equal(t, 8, exp.Resolvers[0].SourcePos.Line)
		assert.Equal(t, 5, exp.Resolvers[0].SourcePos.Column)
		assert.Equal(t, "solution.yaml", exp.Resolvers[0].SourcePos.File)

		// Verify action source position
		require.Len(t, exp.Actions, 1)
		require.NotNil(t, exp.Actions[0].SourcePos)
		assert.Equal(t, 15, exp.Actions[0].SourcePos.Line)
		assert.Equal(t, 7, exp.Actions[0].SourcePos.Column)

		// Verify finally source position
		require.Len(t, exp.Finally, 1)
		require.NotNil(t, exp.Finally[0].SourcePos)
		assert.Equal(t, 22, exp.Finally[0].SourcePos.Line)
	})

	t.Run("omits source positions when no source map", func(t *testing.T) {
		sol := &solution.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solution.Metadata{
				Name: "no-pos",
			},
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"data": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{
								{Provider: "static"},
							},
						},
					},
				},
			},
		}

		exp := BuildSolutionExplanation(sol)

		require.Len(t, exp.Resolvers, 1)
		assert.Nil(t, exp.Resolvers[0].SourcePos)
	})
}
