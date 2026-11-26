// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRunCommand_WorkflowSolution(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {},
		},
	}

	info, err := BuildRunCommand(sol, "solutions/deploy.yaml", "scafctl")
	require.NoError(t, err)

	assert.Equal(t, "scafctl run solution", info.Subcommand)
	assert.Contains(t, info.Command, "scafctl run solution -f ./solutions/deploy.yaml")
	assert.True(t, info.HasWorkflow)
	assert.Empty(t, info.Parameters)
}

func TestBuildRunCommand_ResolverOnlySolution(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"env": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "parameter"}},
			},
			Description: "Environment name",
			Example:     "prod",
		},
		"region": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "parameter"}},
			},
		},
		"config": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "cel"}},
			},
		},
	}

	info, err := BuildRunCommand(sol, "/abs/path/solution.yaml", "scafctl")
	require.NoError(t, err)

	assert.Equal(t, "scafctl run resolver", info.Subcommand)
	assert.True(t, info.HasResolvers)
	assert.False(t, info.HasWorkflow)
	// Only parameter-type resolvers listed
	assert.Len(t, info.Parameters, 2)
	// Command includes positional parameter values (resolver-only uses positional syntax)
	assert.Contains(t, info.Command, "env=prod")
	assert.Contains(t, info.Command, "region=<value>")
	// Should NOT use -r flags for resolver-only solutions
	assert.NotContains(t, info.Command, "-r env=")
	assert.NotContains(t, info.Command, "-r region=")
	// Non-parameter resolver not included
	for _, p := range info.Parameters {
		assert.NotEqual(t, "config", p.Name)
	}
}

func TestBuildRunCommand_EmptySolution(t *testing.T) {
	sol := &solution.Solution{}

	info, err := BuildRunCommand(sol, "empty.yaml", "scafctl")

	assert.Nil(t, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "neither resolvers nor a workflow")
}

func TestBuildRunCommand_PathPrefixing(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		expectInCmd string
	}{
		{"relative bare", "solution.yaml", "-f ./solution.yaml"},
		{"relative dotslash", "./solution.yaml", "-f ./solution.yaml"},
		{"relative dotdot", "../solution.yaml", "-f ../solution.yaml"},
		{"absolute", "/home/user/solution.yaml", "-f /home/user/solution.yaml"},
		// URLs are passed positionally (run solution accepts a positional ref),
		// not as -f.
		{"url", "https://example.com/sol.yaml", "run resolver https://example.com/sol.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sol := &solution.Solution{}
			sol.Spec.Resolvers = map[string]*resolver.Resolver{
				"x": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "cel"}}}},
			}

			info, err := BuildRunCommand(sol, tt.path, "scafctl")
			require.NoError(t, err)
			assert.Contains(t, info.Command, tt.expectInCmd)
		})
	}
}

func BenchmarkBuildRunCommand(b *testing.B) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"env":    {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "parameter"}}}},
		"region": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "parameter"}}}},
		"config": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "cel"}}}},
	}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{"deploy": {}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		BuildRunCommand(sol, "solution.yaml", "scafctl") //nolint:errcheck
	}
}

// TestBuildRunCommand_SourceArg verifies the generated source argument is
// correct for each load type (regression for PR #646: no bogus `-f ./` or
// `-f ./<catalog-ref>` commands).
func TestBuildRunCommand_SourceArg(t *testing.T) {
	newSol := func(resolvedPath string) *solution.Solution {
		s := &solution.Solution{}
		s.Spec.Resolvers = map[string]*resolver.Resolver{
			"x": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "cel"}}}},
		}
		if resolvedPath != "" {
			s.SetPath(resolvedPath)
		}
		return s
	}

	tests := []struct {
		name         string
		resolvedPath string // sol.GetPath()
		rawPath      string // the original -f/positional input
		wantContains string
		wantMissing  string
	}{
		{
			name:         "local file uses -f",
			resolvedPath: "/abs/solution.yaml",
			rawPath:      "./solution.yaml",
			wantContains: "run resolver -f ./solution.yaml",
		},
		{
			name:         "auto-discovery falls back to resolved path (no -f ./)",
			resolvedPath: "scafctl/solution.yaml",
			rawPath:      "",
			wantContains: "run resolver -f ./scafctl/solution.yaml",
			wantMissing:  "run resolver -f ./ ",
		},
		{
			name:         "catalog ref is positional, not -f",
			resolvedPath: "catalog:my-app@1.0.0",
			rawPath:      "my-app@1.0.0",
			wantContains: "run resolver my-app@1.0.0",
			wantMissing:  "-f",
		},
		{
			name:         "catalog ref with catalog name strips prefix",
			resolvedPath: "catalog:official:my-app",
			rawPath:      "",
			wantContains: "run resolver my-app",
			wantMissing:  "-f",
		},
		{
			name:         "remote ref is positional",
			resolvedPath: "remote:ghcr.io/org/app@1.0.0",
			rawPath:      "ghcr.io/org/app@1.0.0",
			wantContains: "run resolver ghcr.io/org/app@1.0.0",
			wantMissing:  "-f",
		},
		{
			name:         "stdin uses -f - without ./ prefix",
			resolvedPath: "-",
			rawPath:      "-",
			wantContains: "run resolver -f -",
			wantMissing:  "-f ./-",
		},
		{
			name:         "local path with spaces is shell-quoted",
			resolvedPath: "/abs/task files/solution.yaml",
			rawPath:      "task files/solution.yaml",
			wantContains: "run resolver -f './task files/solution.yaml'",
		},
		{
			name:         "local path with trailing whitespace preserves filename",
			resolvedPath: "/tmp/solution.yaml ",
			rawPath:      "/tmp/solution.yaml ",
			wantContains: "run resolver -f '/tmp/solution.yaml '",
		},
		{
			name:         "URL with shell metacharacters is shell-quoted",
			resolvedPath: "https://example.com/sol.yaml?a=1&b=2",
			rawPath:      "https://example.com/sol.yaml?a=1&b=2",
			wantContains: "run resolver 'https://example.com/sol.yaml?a=1&b=2'",
			wantMissing:  "-f",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := BuildRunCommand(newSol(tt.resolvedPath), tt.rawPath, "scafctl")
			require.NoError(t, err)
			assert.Contains(t, info.BaseCommand, tt.wantContains)
			if tt.wantMissing != "" {
				assert.NotContains(t, info.BaseCommand, tt.wantMissing)
			}
		})
	}
}

func TestBuildRunCommand_FallbackDefaultsAndRequiredParams(t *testing.T) {
	t.Parallel()

	condExpr := celexp.Expression("_.useCustom == true")
	dynDefaultExpr := celexp.Expression("'dynamic-val'")
	falseExpr := celexp.Expression("false")
	trueExpr := celexp.Expression("true")

	sol := &solution.Solution{}
	sol.Metadata.Name = "my-service"
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"requiredParam": {
			Description: "Must be supplied",
			Example:     "acme",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "project"},
						},
					},
				},
			},
		},
		"inputDefaultLiteral": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key":     {Literal: "env"},
							"default": {Literal: "dev"},
						},
					},
				},
			},
		},
		"inputDefaultDynamic": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key":     {Literal: "tag"},
							"default": {Expr: &dynDefaultExpr},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "unreachable-static"},
						},
					},
				},
			},
		},
		"dynamicStaticStopsBeforeLaterLiteralStatic": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "endpoint"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Expr: &dynDefaultExpr},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "unreachable-endpoint"},
						},
					},
				},
			},
		},
		"continueOnErrorFalseBlocksFallback": {
			Example: "strict-val",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider:        "parameter",
						ContinueOnError: &resolver.Condition{Expr: &falseExpr},
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "strictKey"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "unreachable"},
						},
					},
				},
			},
		},
		"onErrorFailBlocksFallback": {
			Example: "fail-val",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						OnError:  resolver.ErrorBehaviorFail,
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "failKey"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "unreachable"},
						},
					},
				},
			},
		},
		"continueOnErrorTrueOverridesOnErrorFail": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider:        "parameter",
						OnError:         resolver.ErrorBehaviorFail,
						ContinueOnError: &resolver.Condition{Expr: &trueExpr},
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "overrideKey"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "recovered-default"},
						},
					},
				},
			},
		},
		"staticallyDisabledStepWithOnErrorFailStillFallsThrough": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "disabledMidKey"},
						},
					},
					{
						Provider:        "parameter",
						When:            &resolver.Condition{Expr: &falseExpr},
						OnError:         resolver.ErrorBehaviorFail,
						ContinueOnError: &resolver.Condition{Expr: &falseExpr},
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "neverRuns"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "after-disabled"},
						},
					},
				},
			},
		},
		"leadingDisabledParameterSkippedBeforeStatic": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						When:     &resolver.Condition{Expr: &falseExpr},
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "deadLeadingParam"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "fixed"},
						},
					},
				},
			},
		},
		"leadingDisabledStepBeforeActiveDefaultedParameter": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						When:     &resolver.Condition{Expr: &falseExpr},
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "deadOldKey"},
						},
					},
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key":     {Literal: "activeKey"},
							"default": {Literal: "active-default"},
						},
					},
				},
			},
		},
		"staticFallback": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "region"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "us-east-1"},
						},
					},
				},
			},
		},
		"conditionalThenStaticFallback": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "tier"},
						},
					},
					{
						Provider: "static",
						When:     &resolver.Condition{Expr: &condExpr},
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "premium"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "standard"},
						},
					},
				},
			},
		},
		"celFallback": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "replicas"},
						},
					},
					{
						Provider: "cel",
						Inputs: map[string]*resolver.ValueRef{
							"expression": {Literal: "3"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "unreachable-after-cel"},
						},
					},
				},
			},
		},
		"conditionalOnlyNoGuaranteedFallback": {
			Example: "custom-only",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "mode"},
						},
					},
					{
						Provider: "static",
						When:     &resolver.Condition{Expr: &condExpr},
						Inputs: map[string]*resolver.ValueRef{
							"value": {Literal: "conditional"},
						},
					},
				},
			},
		},
		"parameterAliasWithoutDefault": {
			Example: "v1",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "apiVersion"},
						},
					},
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: "legacyApiVersion"},
						},
					},
				},
			},
		},
		"mapModeKeys": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"keys": {Literal: []any{"labelA", "labelB"}},
							"as":   {Literal: "map"},
						},
					},
				},
			},
		},
		"allParamsMap": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*resolver.ValueRef{
							"all": {Literal: true},
						},
					},
				},
			},
		},
	}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Description: "Default deploy"},
		},
	}

	info, err := BuildRunCommand(sol, "solution.yaml", "scafctl")
	require.NoError(t, err)

	byResolver := make(map[string]ParamInfo, len(info.Parameters))
	for _, p := range info.Parameters {
		byResolver[p.ResolverName] = p
	}

	assert.True(t, byResolver["requiredParam"].Required)
	assert.Nil(t, byResolver["requiredParam"].Default)

	assert.False(t, byResolver["inputDefaultLiteral"].Required)
	assert.Equal(t, "dev", byResolver["inputDefaultLiteral"].Default)

	assert.False(t, byResolver["inputDefaultDynamic"].Required)
	assert.Nil(t, byResolver["inputDefaultDynamic"].Default)

	assert.False(t, byResolver["dynamicStaticStopsBeforeLaterLiteralStatic"].Required)
	assert.Nil(t, byResolver["dynamicStaticStopsBeforeLaterLiteralStatic"].Default)

	assert.True(t, byResolver["continueOnErrorFalseBlocksFallback"].Required)
	assert.Nil(t, byResolver["continueOnErrorFalseBlocksFallback"].Default)

	assert.True(t, byResolver["onErrorFailBlocksFallback"].Required)
	assert.Nil(t, byResolver["onErrorFailBlocksFallback"].Default)

	assert.False(t, byResolver["continueOnErrorTrueOverridesOnErrorFail"].Required)
	assert.Equal(t, "recovered-default", byResolver["continueOnErrorTrueOverridesOnErrorFail"].Default)

	assert.False(t, byResolver["staticallyDisabledStepWithOnErrorFailStillFallsThrough"].Required)
	assert.Equal(t, "after-disabled", byResolver["staticallyDisabledStepWithOnErrorFailStillFallsThrough"].Default)

	_, hasDeadLeading := byResolver["leadingDisabledParameterSkippedBeforeStatic"]
	assert.False(t, hasDeadLeading, "disabled leading parameter before static is not an active parameter")

	assert.Equal(t, "activeKey", byResolver["leadingDisabledStepBeforeActiveDefaultedParameter"].Name)
	assert.False(t, byResolver["leadingDisabledStepBeforeActiveDefaultedParameter"].Required)
	assert.Equal(t, "active-default", byResolver["leadingDisabledStepBeforeActiveDefaultedParameter"].Default)

	assert.False(t, byResolver["staticFallback"].Required)
	assert.Equal(t, "us-east-1", byResolver["staticFallback"].Default)

	assert.False(t, byResolver["conditionalThenStaticFallback"].Required)
	assert.Equal(t, "standard", byResolver["conditionalThenStaticFallback"].Default)

	assert.False(t, byResolver["celFallback"].Required)
	assert.Nil(t, byResolver["celFallback"].Default)

	assert.True(t, byResolver["conditionalOnlyNoGuaranteedFallback"].Required)
	assert.Nil(t, byResolver["conditionalOnlyNoGuaranteedFallback"].Default)

	assert.True(t, byResolver["parameterAliasWithoutDefault"].Required)
	assert.Nil(t, byResolver["parameterAliasWithoutDefault"].Default)

	assert.False(t, byResolver["mapModeKeys"].Required)
	_, hasAllParams := byResolver["allParamsMap"]
	assert.False(t, hasAllParams, "all: true parameter resolver does not have a named CLI key")

	// Only required parameters are appended to Command and CatalogCommand (sorted by resolver name).
	assert.Equal(t, "scafctl run solution -f ./solution.yaml -r mode=custom-only -r strictKey=strict-val -r failKey=fail-val -r apiVersion=v1 -r project=acme", info.Command)
	assert.Equal(t, "scafctl run solution my-service -r mode=custom-only -r strictKey=strict-val -r failKey=fail-val -r apiVersion=v1 -r project=acme", info.CatalogCommand)

	// Resolver-only solutions also append positional key=value only for required parameters.
	resOnly := &solution.Solution{}
	resOnly.Spec.Resolvers = sol.Spec.Resolvers
	resInfo, err := BuildRunCommand(resOnly, "resolvers.yaml", "scafctl")
	require.NoError(t, err)
	assert.Equal(t, "scafctl run resolver -f ./resolvers.yaml mode=custom-only strictKey=strict-val failKey=fail-val apiVersion=v1 project=acme", resInfo.Command)
	assert.Empty(t, resInfo.CatalogCommand)
}

func TestBuildRunCommand_CatalogAndExplicitActions_Embedder(t *testing.T) {
	t.Parallel()

	t.Run("mixed default and explicit actions with custom binary name", func(t *testing.T) {
		t.Parallel()
		sol := &solution.Solution{}
		sol.Metadata.Name = "starter-kit"
		sol.Spec.Resolvers = map[string]*resolver.Resolver{
			"appName": {
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "parameter"}},
				},
			},
		}
		sol.Spec.Workflow = &action.Workflow{
			Actions: map[string]*action.Action{
				"scaffold": {
					Description: "Generate starter files",
				},
				"verify": {
					Description: "Run verification suite",
					Explicit:    true,
					Alias:       "v",
				},
				"clean": {
					Description: "Remove generated files",
					Explicit:    true,
				},
			},
		}

		info, err := BuildRunCommand(sol, "solution.yaml", "mycli")
		require.NoError(t, err)

		assert.Equal(t, "mycli run solution", info.Subcommand)
		assert.Equal(t, "mycli run solution -f ./solution.yaml -r appName=<value>", info.Command)
		assert.Equal(t, "mycli run solution starter-kit -r appName=<value>", info.CatalogCommand)

		require.Len(t, info.ExplicitActions, 2)
		assert.Equal(t, ExplicitActionInfo{
			Name:        "clean",
			Description: "Remove generated files",
			Command:     "mycli run solution -f ./solution.yaml --action clean -r appName=<value>",
		}, info.ExplicitActions[0])
		assert.Equal(t, ExplicitActionInfo{
			Name:        "verify",
			Alias:       "v",
			Description: "Run verification suite",
			Command:     "mycli run solution -f ./solution.yaml --action verify -r appName=<value>",
		}, info.ExplicitActions[1])
	})

	t.Run("all actions explicit omits catalogCommand and keeps Subcommand unchanged", func(t *testing.T) {
		t.Parallel()
		sol := &solution.Solution{}
		sol.Metadata.Name = "repo-tasks"
		sol.Spec.Workflow = &action.Workflow{
			Actions: map[string]*action.Action{
				"test": {Explicit: true, Description: "Run tests"},
			},
		}

		info, err := BuildRunCommand(sol, "actions.yaml", "")
		require.NoError(t, err)

		assert.Equal(t, "scafctl run solution", info.Subcommand)
		assert.Empty(t, info.CatalogCommand)
		require.Len(t, info.ExplicitActions, 1)
		assert.Equal(t, "scafctl run solution -f ./actions.yaml --action test", info.ExplicitActions[0].Command)
	})

	t.Run("invalid metadata name omits catalogCommand", func(t *testing.T) {
		t.Parallel()
		sol := &solution.Solution{}
		sol.Metadata.Name = "Invalid Name!"
		sol.Spec.Workflow = &action.Workflow{
			Actions: map[string]*action.Action{
				"deploy": {},
			},
		}

		info, err := BuildRunCommand(sol, "solution.yaml", "scafctl")
		require.NoError(t, err)
		assert.Empty(t, info.CatalogCommand)
	})
}

func TestRunCommandInfo_SchemaGenerationDoesNotPanic(t *testing.T) {
	t.Parallel()
	info, err := schema.IntrospectType(RunCommandInfo{})
	require.NoError(t, err)
	require.NotNil(t, info)
}

func BenchmarkBuildRunCommand_WithExplicitAndFallback(b *testing.B) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "bench-solution"
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"project": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "parameter"}},
			},
		},
		"region": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "parameter"},
					{Provider: "static", Inputs: map[string]*resolver.ValueRef{"value": {Literal: "us-east-1"}}},
				},
			},
		},
	}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy":   {Description: "Deploy"},
			"teardown": {Description: "Teardown", Explicit: true, Alias: "td"},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = BuildRunCommand(sol, "solution.yaml", "mycli")
	}
}
