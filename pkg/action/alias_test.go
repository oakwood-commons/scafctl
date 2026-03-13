// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlias_Validation(t *testing.T) {
	t.Run("valid alias", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "config",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("alias with hyphen", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "my-config",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("alias with underscore", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "my_config",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("alias conflicts with action name", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "deploy",
				},
				"deploy": {
					Provider:  "shell",
					DependsOn: []string{"fetchConfiguration"},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "alias \"deploy\" conflicts with action name")
	})

	t.Run("alias conflicts with reserved name _", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "_",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved name")
	})

	t.Run("alias conflicts with reserved name __actions", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "__actions",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		// Should fail on both reserved prefix and reserved name
		assert.Contains(t, err.Error(), "reserved")
	})

	t.Run("alias conflicts with reserved name __item", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "__item",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved")
	})

	t.Run("alias with __ prefix is reserved", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "__myalias",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved prefix")
	})

	t.Run("duplicate alias across actions", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "config",
				},
				"loadConfiguration": {
					Provider: "api",
					Alias:    "config",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "alias \"config\" already used")
	})

	t.Run("duplicate alias across sections", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "config",
				},
			},
			Finally: map[string]*Action{
				"cleanup": {
					Provider: "shell",
					Alias:    "config",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "alias \"config\" already used")
	})

	t.Run("invalid alias pattern", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "123invalid",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must match pattern")
	})

	t.Run("empty alias is allowed (no alias)", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					// No alias set
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("unique aliases across actions are valid", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "config",
				},
				"buildApp": {
					Provider:  "shell",
					Alias:     "build",
					DependsOn: []string{"fetchConfiguration"},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})
}

func TestAlias_BuildGraph(t *testing.T) {
	ctx := context.Background()

	t.Run("alias map is populated", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "config",
				},
				"deploy": {
					Provider:  "shell",
					DependsOn: []string{"fetchConfiguration"},
				},
			},
		}

		graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
		require.NoError(t, err)
		require.NotNil(t, graph.AliasMap)
		assert.Equal(t, "fetchConfiguration", graph.AliasMap["config"])
		assert.Len(t, graph.AliasMap, 1)
	})

	t.Run("alias map from both sections", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "config",
				},
			},
			Finally: map[string]*Action{
				"cleanup": {
					Provider: "shell",
					Alias:    "clean",
				},
			},
		}

		graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
		require.NoError(t, err)
		assert.Equal(t, "fetchConfiguration", graph.AliasMap["config"])
		assert.Equal(t, "cleanup", graph.AliasMap["clean"])
	})

	t.Run("alias reference defers input", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "config",
					Inputs: map[string]*spec.ValueRef{
						"endpoint": {Literal: "https://api.example.com/config"},
					},
				},
				"deploy": {
					Provider:  "shell",
					DependsOn: []string{"fetchConfiguration"},
					Inputs: map[string]*spec.ValueRef{
						"endpoint": {Expr: celExpr("config.results.endpoint")},
						"name":     {Literal: "my-app"},
					},
				},
			},
		}

		resolverData := map[string]any{}
		graph, err := BuildGraph(ctx, w, resolverData, nil)
		require.NoError(t, err)

		deployAction := graph.Actions["deploy"]
		require.NotNil(t, deployAction)

		// The alias reference should be deferred
		assert.Contains(t, deployAction.DeferredInputs, "endpoint")
		assert.Equal(t, "config.results.endpoint", deployAction.DeferredInputs["endpoint"].OriginalExpr)

		// The literal should be materialized
		assert.Equal(t, "my-app", deployAction.MaterializedInputs["name"])
	})

	t.Run("__actions reference still works alongside alias", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"fetchConfiguration": {
					Provider: "api",
					Alias:    "config",
					Inputs: map[string]*spec.ValueRef{
						"endpoint": {Literal: "https://api.example.com/config"},
					},
				},
				"deploy": {
					Provider:  "shell",
					DependsOn: []string{"fetchConfiguration"},
					Inputs: map[string]*spec.ValueRef{
						"endpoint": {Expr: celExpr("__actions.fetchConfiguration.results.endpoint")},
					},
				},
			},
		}

		resolverData := map[string]any{}
		graph, err := BuildGraph(ctx, w, resolverData, nil)
		require.NoError(t, err)

		deployAction := graph.Actions["deploy"]
		require.NotNil(t, deployAction)

		// The __actions reference should still be deferred
		assert.Contains(t, deployAction.DeferredInputs, "endpoint")
	})
}

func TestAlias_DeferredValueEvaluate(t *testing.T) {
	ctx := context.Background()

	t.Run("evaluate with alias", func(t *testing.T) {
		dv := &DeferredValue{
			OriginalExpr: "config.results.endpoint",
			Deferred:     true,
		}

		additionalVars := map[string]any{
			"__actions": map[string]any{
				"fetchConfiguration": map[string]any{
					"results": map[string]any{
						"endpoint": "https://api.example.com",
					},
				},
			},
			"config": map[string]any{
				"results": map[string]any{
					"endpoint": "https://api.example.com",
				},
			},
		}

		result, err := dv.Evaluate(ctx, nil, additionalVars)
		require.NoError(t, err)
		assert.Equal(t, "https://api.example.com", result)
	})

	t.Run("evaluate with alias and resolver data", func(t *testing.T) {
		dv := &DeferredValue{
			OriginalExpr: `_.env + ":" + config.results.port`,
			Deferred:     true,
		}

		resolverData := map[string]any{"env": "production"}
		additionalVars := map[string]any{
			"__actions": map[string]any{
				"fetchConfiguration": map[string]any{
					"results": map[string]any{
						"port": "8080",
					},
				},
			},
			"config": map[string]any{
				"results": map[string]any{
					"port": "8080",
				},
			},
		}

		result, err := dv.Evaluate(ctx, resolverData, additionalVars)
		require.NoError(t, err)
		assert.Equal(t, "production:8080", result)
	})
}

func TestAlias_BuildAdditionalVars(t *testing.T) {
	t.Run("builds vars with aliases", func(t *testing.T) {
		executor := NewExecutor()

		// Set up action context with results
		executor.actionContext.MarkRunning("fetchConfiguration", map[string]any{"endpoint": "https://api.example.com"})
		executor.actionContext.MarkSucceeded("fetchConfiguration", map[string]any{
			"endpoint": "https://api.example.com",
			"version":  "1.2.3",
		})

		aliasMap := map[string]string{
			"config": "fetchConfiguration",
		}

		vars := executor.buildAdditionalVars(aliasMap)

		// Should have __actions namespace
		actionsNs, ok := vars["__actions"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, actionsNs, "fetchConfiguration")

		// Should have alias as top-level var pointing to the same data
		configAlias, ok := vars["config"]
		require.True(t, ok)
		assert.NotNil(t, configAlias)

		// Alias data should match __actions data
		configMap, ok := configAlias.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, actionsNs["fetchConfiguration"], configMap)
	})

	t.Run("alias for non-existent action omitted", func(t *testing.T) {
		executor := NewExecutor()

		aliasMap := map[string]string{
			"config": "nonExistent",
		}

		vars := executor.buildAdditionalVars(aliasMap)

		// __actions should be present
		assert.Contains(t, vars, "__actions")
		// Alias should not be present since action doesn't exist
		_, hasConfig := vars["config"]
		assert.False(t, hasConfig)
	})

	t.Run("empty alias map", func(t *testing.T) {
		executor := NewExecutor()

		vars := executor.buildAdditionalVars(nil)

		assert.Contains(t, vars, "__actions")
		assert.Contains(t, vars, "__cwd")
		assert.Len(t, vars, 2)
	})
}

func TestAlias_BuildAliasNames(t *testing.T) {
	t.Run("collects aliases from actions", func(t *testing.T) {
		actions := map[string]*Action{
			"fetchConfig": {Alias: "config"},
			"buildApp":    {Alias: "build"},
			"deploy":      {},
		}

		names := buildAliasNames(actions)
		assert.Len(t, names, 2)
		assert.Contains(t, names, "config")
		assert.Contains(t, names, "build")
	})

	t.Run("collects aliases from multiple sections", func(t *testing.T) {
		actions := map[string]*Action{
			"fetchConfig": {Alias: "config"},
		}
		finally := map[string]*Action{
			"cleanup": {Alias: "clean"},
		}

		names := buildAliasNames(actions, finally)
		assert.Len(t, names, 2)
		assert.Contains(t, names, "config")
		assert.Contains(t, names, "clean")
	})

	t.Run("handles empty sections", func(t *testing.T) {
		names := buildAliasNames(nil, nil)
		assert.Len(t, names, 0)
	})

	t.Run("handles nil actions", func(t *testing.T) {
		actions := map[string]*Action{
			"fetchConfig": nil,
			"buildApp":    {Alias: "build"},
		}

		names := buildAliasNames(actions)
		assert.Len(t, names, 1)
		assert.Contains(t, names, "build")
	})
}

func TestAlias_ReferencesActionsOrAlias(t *testing.T) {
	t.Run("detects __actions reference", func(t *testing.T) {
		v := &spec.ValueRef{Expr: celExpr("__actions.build.results.exitCode")}
		assert.True(t, referencesActionsOrAlias(v, nil))
	})

	t.Run("detects alias reference", func(t *testing.T) {
		v := &spec.ValueRef{Expr: celExpr("config.results.endpoint")}
		assert.True(t, referencesActionsOrAlias(v, []string{"config"}))
	})

	t.Run("no match without alias", func(t *testing.T) {
		v := &spec.ValueRef{Expr: celExpr("config.results.endpoint")}
		assert.False(t, referencesActionsOrAlias(v, nil))
	})

	t.Run("no match for resolver reference", func(t *testing.T) {
		v := &spec.ValueRef{Expr: celExpr("_.env")}
		assert.False(t, referencesActionsOrAlias(v, []string{"config"}))
	})

	t.Run("literal value", func(t *testing.T) {
		v := &spec.ValueRef{Literal: "hello"}
		assert.False(t, referencesActionsOrAlias(v, []string{"config"}))
	})
}

// Benchmarks

func BenchmarkBuildAliasNames(b *testing.B) {
	actions := make(map[string]*Action, 100)
	for i := 0; i < 100; i++ {
		actions[fmt.Sprintf("action%d", i)] = &Action{
			Alias: fmt.Sprintf("alias%d", i),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildAliasNames(actions)
	}
}

func BenchmarkReferencesActionsOrAlias(b *testing.B) {
	v := &spec.ValueRef{Expr: celExpr("config.results.endpoint")}
	aliases := []string{"config", "build", "deploy", "test", "validate"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		referencesActionsOrAlias(v, aliases)
	}
}

func BenchmarkBuildAdditionalVars(b *testing.B) {
	executor := NewExecutor()
	executor.actionContext.MarkRunning("fetchConfiguration", map[string]any{"endpoint": "https://api.example.com"})
	executor.actionContext.MarkSucceeded("fetchConfiguration", map[string]any{
		"endpoint": "https://api.example.com",
		"version":  "1.2.3",
	})

	aliasMap := map[string]string{
		"config": "fetchConfiguration",
		"build":  "buildApp",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executor.buildAdditionalVars(aliasMap)
	}
}

func BenchmarkValidateAlias(b *testing.B) {
	w := &Workflow{
		Actions: map[string]*Action{
			"fetchConfiguration": {
				Provider: "api",
				Alias:    "config",
			},
			"buildApp": {
				Provider:  "shell",
				Alias:     "build",
				DependsOn: []string{"fetchConfiguration"},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateWorkflow(w, nil)
	}
}
