// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// paramResolver builds a parameter-provider resolver with an optional literal default.
func paramResolver(key string, def any) *resolver.Resolver {
	inputs := map[string]*resolver.ValueRef{
		"key": {Literal: key},
	}
	if def != nil {
		inputs["default"] = &resolver.ValueRef{Literal: def}
	}
	return &resolver.Resolver{
		Type: "string",
		Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{
				{Provider: "parameter", Inputs: inputs},
			},
		},
	}
}

func whenCondition(expr string) *spec.Condition {
	e := celexp.Expression(expr)
	return &spec.Condition{Expr: &e}
}

func TestBuildUsage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	ver := semver.MustParse("1.2.3")
	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:        "tf-registry",
			Version:     ver,
			Description: "Discovers Terraform modules and providers",
			Source:      "./solution.yaml",
			Usage: &spec.Usage{
				Synopsis: "Registry index for Terraform modules",
				Examples: []spec.UsageExample{
					{Description: "Refresh", Command: "scafctl run solution -r action=refresh"},
				},
			},
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"action": paramResolver("action", "show"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"show": {
						Description: "Display registry summary",
						When:        whenCondition(`_.action == "show"`),
					},
					"refresh": {
						Description: "Fetch fresh data",
						When:        whenCondition(`_.action == "refresh"`),
					},
					"cleanup": {
						Description: "Explicit cleanup",
						Explicit:    true,
					},
				},
			},
		},
	}

	usage, err := BuildUsage(ctx, sol, "solution.yaml", "scafctl")
	require.NoError(t, err)

	// Synopsis prefers metadata.usage.synopsis.
	assert.Equal(t, "Registry index for Terraform modules", usage.Synopsis)
	assert.Equal(t, "tf-registry", usage.Name)
	assert.Equal(t, "1.2.3", usage.Version)
	assert.Equal(t, "scafctl run solution", usage.Run)
	require.Len(t, usage.Examples, 1)

	// Parameter 'action' has default "show" and discovered allowed values.
	require.Len(t, usage.Params, 1)
	p := usage.Params[0]
	assert.Equal(t, "action", p.Name)
	assert.Equal(t, "show", p.Default)
	assert.False(t, p.Required)
	assert.Equal(t, []any{"refresh", "show"}, p.AllowedValues)

	// Actions: sorted, with commands. cleanup is explicit -> run action; when-gated
	// actions get -r commands; none are "default" here (all gated or explicit).
	byName := map[string]ActionUsage{}
	for _, a := range usage.Actions {
		byName[a.Name] = a
	}
	assert.Equal(t, "scafctl run solution -f ./solution.yaml -r action=refresh", byName["refresh"].Command)
	assert.Equal(t, "scafctl run solution -f ./solution.yaml -r action=show", byName["show"].Command)
	assert.Equal(t, "scafctl run action cleanup -f ./solution.yaml", byName["cleanup"].Command)
	assert.False(t, byName["show"].Default)
	assert.False(t, byName["cleanup"].Default)
}

func TestBuildUsage_SynopsisFallsBackToDescription(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:        "app",
			Description: "A plain description",
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"x": paramResolver("x", nil),
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	assert.Equal(t, "A plain description", usage.Synopsis)
	// No default -> required.
	require.Len(t, usage.Params, 1)
	assert.True(t, usage.Params[0].Required)
}

func TestBuildUsage_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"build": {Description: "Build it"},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "mycli")
	require.NoError(t, err)
	assert.Equal(t, "mycli run solution", usage.Run)
	require.Len(t, usage.Actions, 1)
	// Default action (not explicit, no when) -> base run command (with source)
	// and the embedder binary name.
	assert.True(t, usage.Actions[0].Default)
	assert.Equal(t, "mycli run solution -f ./solution.yaml", usage.Actions[0].Command)
}

func TestBuildUsage_NoRunnableErrors(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{Metadata: solution.Metadata{Name: "empty"}}
	_, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.Error(t, err)
}

// Finally actions also contribute discovered allowed values from their when-clauses.
func TestBuildUsage_FinallyActionsDiscovered(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"mode": paramResolver("mode", "normal"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"build": {Description: "Build"},
				},
				Finally: map[string]*action.Action{
					"cleanup": {
						Description: "Cleanup on teardown",
						When:        whenCondition(`_.mode == "teardown"`),
					},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	require.Len(t, usage.Params, 1)
	// "teardown" is discovered from the finally action's when-clause.
	assert.Contains(t, usage.Params[0].AllowedValues, "teardown")
}

// A resolver-only solution (no workflow) yields no actions and no discovered values.
func TestBuildUsage_ResolverOnly(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app", Description: "desc"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"x": paramResolver("x", "default-x"),
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	assert.Equal(t, "scafctl run resolver", usage.Run)
	assert.Empty(t, usage.Actions)
	require.Len(t, usage.Params, 1)
	assert.Empty(t, usage.Params[0].AllowedValues)
}

func BenchmarkBuildUsage(b *testing.B) {
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"action": paramResolver("action", "show"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"show":    {Description: "Show", When: whenCondition(`_.action == "show"`)},
					"refresh": {Description: "Refresh", When: whenCondition(`_.action == "refresh"`)},
				},
			},
		},
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = BuildUsage(ctx, sol, "solution.yaml", "scafctl")
	}
}

// C: when the parameter provider's inputs.key differs from the resolver name,
// allowed values (discovered by resolver name via when-clauses) must attach to
// the CLI key, and the -r command must use the CLI key.
func TestBuildUsage_ParamKeyDiffersFromResolverName(t *testing.T) {
	t.Parallel()
	// Resolver named "environment" but CLI key is "env".
	envResolver := &resolver.Resolver{
		Type: "string",
		Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{
				{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{
					"key":     {Literal: "env"},
					"default": {Literal: "dev"},
				}},
			},
		},
	}
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{"environment": envResolver},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"deploy": {
						Description: "Deploy",
						// when references the RESOLVER name, not the CLI key.
						When: whenCondition(`_.environment == "prod"`),
					},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)

	// Parameter is displayed by its CLI key.
	require.Len(t, usage.Params, 1)
	assert.Equal(t, "env", usage.Params[0].Name)
	// Allowed value discovered from the resolver-name when-clause attaches to it.
	assert.Contains(t, usage.Params[0].AllowedValues, "prod")

	// The action command uses the CLI key (-r env=prod), not the resolver name.
	require.Len(t, usage.Actions, 1)
	assert.Equal(t, "scafctl run solution -f ./solution.yaml -r env=prod", usage.Actions[0].Command)
}

// D: a compound when-clause that is not fully reducible must NOT produce a
// misleading -r command (it would not actually satisfy the whole gate).
func TestBuildUsage_CompoundWhenNoMisleadingCommand(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"mode": paramResolver("mode", "normal"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"go": {
						Description: "Go",
						When:        whenCondition(`_.mode == "prod" && _.enabled`),
					},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	require.Len(t, usage.Actions, 1)
	// Falls back to the base command (no misleading -r mode=prod).
	assert.Equal(t, "scafctl run solution -f ./solution.yaml", usage.Actions[0].Command)
}

// E: generated -r values with spaces/metacharacters are shell-quoted.
func TestBuildUsage_ShellQuotesValues(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"mode": paramResolver("mode", "normal"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"run": {
						Description: "Run",
						When:        whenCondition(`_.mode == "dry run"`),
					},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	require.Len(t, usage.Actions, 1)
	assert.Equal(t, "scafctl run solution -f ./solution.yaml -r mode='dry run'", usage.Actions[0].Command)
}
