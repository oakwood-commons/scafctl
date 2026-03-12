// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"errors"
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
)

func TestDiagnoseExecutionError_AggregatedExecution(t *testing.T) {
	aggErr := &resolver.AggregatedExecutionError{
		SucceededCount: 2,
		SkippedCount:   1,
		SkippedNames:   []string{"skippedResolver"},
	}
	aggErr.Add("failedResolver", 1, fmt.Errorf("connection refused"))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{}

	diag := DiagnoseExecutionError(aggErr, sol)

	assert.Contains(t, diag.Details, "1 resolver(s) failed")
	assert.Contains(t, diag.Details, "2 succeeded")
	assert.Contains(t, diag.Details, "1 skipped")
	assert.NotEmpty(t, diag.Suggestions)
}

func TestDiagnoseExecutionError_ExecutionError(t *testing.T) {
	execErr := resolver.NewExecutionError("myResolver", "resolve", "http", 0, fmt.Errorf("connection refused"))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{}

	diag := DiagnoseExecutionError(execErr, sol)

	assert.Contains(t, diag.Details, `Resolver "myResolver"`)
	assert.Contains(t, diag.Details, "resolve phase")
	assert.Contains(t, diag.Details, "provider: http")
	// HTTP provider hint
	assert.Contains(t, diag.Suggestions[len(diag.Suggestions)-1], "HTTP provider")
}

func TestDiagnoseExecutionError_TypeCoercion(t *testing.T) {
	coercionErr := &resolver.TypeCoercionError{
		ResolverName: "age",
		Phase:        "resolve",
		SourceType:   "string",
		TargetType:   "int",
		Cause:        fmt.Errorf("cannot parse"),
	}

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{}

	diag := DiagnoseExecutionError(coercionErr, sol)

	assert.Contains(t, diag.Details, "cannot coerce string → int")
	assert.Len(t, diag.Suggestions, 2)
}

func TestDiagnoseExecutionError_CircularDependency(t *testing.T) {
	circErr := resolver.NewCircularDependencyError([]string{"a", "b", "a"})

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{}

	diag := DiagnoseExecutionError(circErr, sol)

	assert.Contains(t, diag.Details, "Circular dependency")
	assert.Contains(t, diag.Details, "a → b → a")
}

func TestDiagnoseExecutionError_ValidationError(t *testing.T) {
	valErr := &resolver.AggregatedValidationError{
		ResolverName: "email",
		Failures: []resolver.ValidationFailure{
			{Rule: 0, Message: "must be a valid email"},
		},
	}

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{}

	diag := DiagnoseExecutionError(valErr, sol)

	assert.Contains(t, diag.Details, `Resolver "email"`)
	assert.Contains(t, diag.Details, "must be a valid email")
}

func TestDiagnoseExecutionError_Unknown(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{}

	diag := DiagnoseExecutionError(errors.New("something unexpected"), sol)

	assert.Equal(t, "something unexpected", diag.Details)
	assert.Contains(t, diag.Suggestions, "Check resolver configuration and dependencies")
}

func TestAppendResolverHints_HTTPNoSuchKey(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"api": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
		},
	}

	var suggestions []string
	AppendResolverHints(&suggestions, fmt.Errorf("no such key: statusCode"), "api", sol)

	assert.Len(t, suggestions, 1)
	assert.Contains(t, suggestions[0], "http provider")
}

func TestAppendResolverHints_CELUndeclaredRef(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{}

	var suggestions []string
	AppendResolverHints(&suggestions, fmt.Errorf("undeclared reference to 'foo'"), "test", sol)

	assert.Len(t, suggestions, 1)
	assert.Contains(t, suggestions[0], "CEL functions")
}

func TestAppendResolverHints_NilCause(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{}

	var suggestions []string
	AppendResolverHints(&suggestions, nil, "test", sol)

	assert.Empty(t, suggestions)
}

func BenchmarkDiagnoseExecutionError(b *testing.B) {
	aggErr := &resolver.AggregatedExecutionError{
		SucceededCount: 5,
		SkippedCount:   2,
		SkippedNames:   []string{"skipped1", "skipped2"},
	}
	aggErr.Add("resolver1", 1, fmt.Errorf("connection refused"))
	aggErr.Add("resolver2", 2, fmt.Errorf("timeout"))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"resolver1": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "http"}}}},
		"resolver2": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "cel"}}}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DiagnoseExecutionError(aggErr, sol)
	}
}
