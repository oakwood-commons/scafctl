// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deferredTestRegistry returns a registry with a "static" source provider and an
// "assert" validation provider that fails when its "ok" input is not true.
func deferredTestRegistry(t *testing.T) *mockRegistry {
	t.Helper()
	registry := newMockRegistry()
	require.NoError(t, registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	}))
	require.NoError(t, registry.Register(&mockProvider{
		name: "assert",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			if ok, _ := inputs["ok"].(bool); !ok {
				return nil, fmt.Errorf("assertion failed")
			}
			return &provider.Output{Data: true}, nil
		},
	}))
	return registry
}

func staticResolver(name, value string) *Resolver {
	return &Resolver{
		Name: name,
		Resolve: &ResolvePhase{
			With: []ProviderSource{{
				Provider: "static",
				Inputs:   map[string]*ValueRef{"value": {Literal: value}},
			}},
		},
	}
}

// crossChecker builds a resolver whose validate rule asserts env != region across
// two other resolvers -- a deferred (cross-resolver) validation.
func crossChecker() *Resolver {
	r := staticResolver("checker", "check")
	r.Validate = &ValidatePhase{
		With: []ProviderValidation{{
			Provider: "assert",
			Inputs:   map[string]*ValueRef{"ok": {Expr: celExpPtr("_.env != _.region")}},
		}},
	}
	return r
}

func TestExecute_DeferredValidation_Passes(t *testing.T) {
	registry := deferredTestRegistry(t)
	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		staticResolver("env", "prod"),
		staticResolver("region", "us-east1"),
		crossChecker(),
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	summary, ok := DeferredValidationResultFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, 1, summary.Evaluated)
	assert.Equal(t, 0, summary.Failed)
	assert.False(t, summary.HasFailures())
}

func TestExecute_DeferredValidation_Fails(t *testing.T) {
	registry := deferredTestRegistry(t)
	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		staticResolver("env", "prod"),
		staticResolver("region", "prod"), // equal -> assertion fails
		crossChecker(),
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.Error(t, err)

	var derr *AggregatedDeferredValidationError
	require.True(t, errors.As(err, &derr), "expected AggregatedDeferredValidationError, got %T", err)
	require.Len(t, derr.Failures, 1)
	assert.Equal(t, "checker", derr.Failures[0].ResolverName)

	summary, ok := DeferredValidationResultFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, 1, summary.Failed)
}

func TestExecute_DeferredValidation_MessageOnlyRefNoCycle(t *testing.T) {
	registry := deferredTestRegistry(t)
	executor := NewExecutor(registry)

	// checker's rule is self-only in its provider inputs, but its message
	// references another resolver. Historically this created a false dependency
	// cycle; it must now defer cleanly.
	checker := staticResolver("checker", "ok")
	checker.Validate = &ValidatePhase{
		With: []ProviderValidation{{
			Provider: "assert",
			Inputs:   map[string]*ValueRef{"ok": {Expr: celExpPtr("__self == 'ok'")}},
			Message:  &ValueRef{Tmpl: tmplPtr("checker conflicts with env {{ ._.env }}")},
		}},
	}
	// env references checker in ITS validate message too -- a mutual message-only
	// reference that would previously form a cycle.
	env := staticResolver("env", "prod")
	env.Validate = &ValidatePhase{
		With: []ProviderValidation{{
			Provider: "assert",
			Inputs:   map[string]*ValueRef{"ok": {Expr: celExpPtr("__self == 'prod'")}},
			Message:  &ValueRef{Tmpl: tmplPtr("env conflicts with checker {{ ._.checker }}")},
		}},
	}

	_, err := executor.Execute(context.Background(), []*Resolver{checker, env}, nil)
	require.NoError(t, err, "mutual message-only references must not form a cycle")
}

func TestExecute_DeferredValidation_NonFatalCollects(t *testing.T) {
	registry := deferredTestRegistry(t)
	executor := NewExecutor(registry, WithNonFatalValidation(true))

	resolvers := []*Resolver{
		staticResolver("env", "prod"),
		staticResolver("region", "prod"), // equal -> assertion fails
		crossChecker(),
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	// Non-fatal mode folds deferred failures into a collect-errors aggregated
	// error (values stay inspectable via the context) rather than a hard
	// deferred-validation error.
	require.Error(t, err)
	var aggErr *AggregatedExecutionError
	require.True(t, errors.As(err, &aggErr), "expected AggregatedExecutionError, got %T", err)

	// Deferred failures are reported as an extra phase after the last resolver
	// phase, satisfying FailedResolver.Phase's minimum:"1" contract (never 0).
	require.NotEmpty(t, aggErr.Errors)
	for _, fr := range aggErr.Errors {
		assert.GreaterOrEqual(t, fr.Phase, 1, "deferred validation phase must be >= 1")
	}

	summary, ok := DeferredValidationResultFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, 1, summary.Failed)
	assert.True(t, summary.HasFailures())
}

func TestExecute_DeferredValidation_FailClosedOnSkippedRef(t *testing.T) {
	registry := deferredTestRegistry(t)
	executor := NewExecutor(registry)

	// "maybe" is skipped via a false when condition, so it produces no value.
	maybe := staticResolver("maybe", "x")
	maybe.When = &Condition{Expr: celExpPtr("false")}

	checker := staticResolver("checker", "check")
	checker.Validate = &ValidatePhase{
		With: []ProviderValidation{{
			Provider: "assert",
			Inputs:   map[string]*ValueRef{"ok": {Expr: celExpPtr("_.env != _.maybe")}},
		}},
	}

	resolvers := []*Resolver{
		staticResolver("env", "prod"),
		maybe,
		checker,
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.Error(t, err)

	var derr *AggregatedDeferredValidationError
	require.True(t, errors.As(err, &derr))
	require.Len(t, derr.Failures, 1)
	require.Len(t, derr.Failures[0].Failures, 1)
	assert.Contains(t, derr.Failures[0].Failures[0].Message, "did not produce a value")

	summary, ok := DeferredValidationResultFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, 1, summary.Failed)
	// No provider is invoked when a referenced resolver has no value, so the
	// fail-closed rule must not be counted as evaluated.
	assert.Equal(t, 0, summary.Evaluated)
}

func TestExecute_DeferredValidation_RuleWhenFalseCountsSkipped(t *testing.T) {
	registry := deferredTestRegistry(t)
	executor := NewExecutor(registry)

	// checker's cross-resolver rule is gated by a rule-level when that evaluates
	// false, so no provider runs: it must be counted as skipped, not evaluated.
	checker := staticResolver("checker", "check")
	checker.Validate = &ValidatePhase{
		With: []ProviderValidation{{
			Provider: "assert",
			When:     &Condition{Expr: celExpPtr("_.env == 'never'")},
			Inputs:   map[string]*ValueRef{"ok": {Expr: celExpPtr("_.env != _.region")}},
		}},
	}

	resolvers := []*Resolver{
		staticResolver("env", "prod"),
		staticResolver("region", "us-east1"),
		checker,
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	summary, ok := DeferredValidationResultFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, 0, summary.Evaluated)
	assert.Equal(t, 1, summary.Skipped)
	assert.Equal(t, 0, summary.Failed)
}

func TestExecute_DeferredValidation_SkippedWhenValidationDisabled(t *testing.T) {
	registry := deferredTestRegistry(t)
	executor := NewExecutor(registry, WithSkipValidation(true))

	resolvers := []*Resolver{
		staticResolver("env", "prod"),
		staticResolver("region", "prod"), // would fail if validated
		crossChecker(),
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	_, ok := DeferredValidationResultFromContext(ctx)
	assert.False(t, ok, "deferred validation must not run when validation is skipped")
}

func TestExecute_DeferredValidation_DisabledViaOption(t *testing.T) {
	registry := deferredTestRegistry(t)
	executor := NewExecutor(registry, WithDeferredValidation(false))

	resolvers := []*Resolver{
		staticResolver("env", "prod"),
		staticResolver("region", "prod"), // would fail if deferred validation ran
		crossChecker(),
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	_, ok := DeferredValidationResultFromContext(ctx)
	assert.False(t, ok)
}

// TestExecute_DeferredValidation_WhenEvalError_CounterConsistency verifies that
// when a deferred validate-phase when: condition fails to evaluate, the summary
// counters stay consistent with the detailed Results: one failure is recorded
// per deferred rule (not a single failure with an inflated Failed count).
func TestExecute_DeferredValidation_WhenEvalError_CounterConsistency(t *testing.T) {
	registry := deferredTestRegistry(t)
	executor := NewExecutor(registry)

	// checker's validate phase references a foreign resolver (env) so it is
	// classified as deferred, and its when: condition evaluates to a non-boolean
	// value, forcing an evaluation error that fails every rule in the block.
	checker := staticResolver("checker", "check")
	checker.Validate = &ValidatePhase{
		When: &Condition{Expr: celExpPtr("_.env")}, // string, not bool -> eval error
		With: []ProviderValidation{
			{Provider: "assert", Inputs: map[string]*ValueRef{"ok": {Literal: "true"}}},
			{Provider: "assert", Inputs: map[string]*ValueRef{"ok": {Literal: "true"}}},
		},
	}

	resolvers := []*Resolver{
		staticResolver("env", "prod"),
		checker,
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.Error(t, err)

	summary, ok := DeferredValidationResultFromContext(ctx)
	require.True(t, ok)

	// Two rules in the block -> two recorded failures, and the Failed counter
	// must equal the number of detailed failures.
	assert.Equal(t, 2, summary.Failed)
	totalFailures := 0
	for _, rf := range summary.Results {
		totalFailures += len(rf.Failures)
	}
	assert.Equal(t, summary.Failed, totalFailures,
		"Failed counter must match the number of detailed failures")

	// Each failure must be attributable to a specific rule/provider.
	require.Len(t, summary.Results, 1)
	require.Len(t, summary.Results[0].Failures, 2)
	for _, f := range summary.Results[0].Failures {
		assert.Equal(t, "assert", f.Provider)
		assert.Contains(t, f.Message, "when condition")
	}
}
