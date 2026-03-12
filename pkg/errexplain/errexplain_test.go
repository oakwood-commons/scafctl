// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package errexplain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExplain_ExecutionError(t *testing.T) {
	exp := Explain(`resolver "myApi" failed in resolve phase (step 0, provider http): connection refused`)
	assert.Equal(t, "resolver_execution", exp.Category)
	assert.Contains(t, exp.Summary, "myApi")
	assert.Contains(t, exp.Summary, "resolve")
	assert.Contains(t, exp.RootCause, "connection refused")
	// HTTP-specific suggestions
	assert.True(t, len(exp.Suggestions) > 3)
}

func TestExplain_TypeCoercion(t *testing.T) {
	exp := Explain(`resolver "age": type coercion from string to int failed after resolve phase: cannot parse "abc"`)
	assert.Equal(t, "type_coercion", exp.Category)
	assert.Contains(t, exp.Summary, "string")
	assert.Contains(t, exp.Summary, "int")
}

func TestExplain_ValidationFailed(t *testing.T) {
	exp := Explain(`resolver "email" validation failed: must be a valid email`)
	assert.Equal(t, "validation", exp.Category)
	assert.Contains(t, exp.RootCause, "must be a valid email")
}

func TestExplain_CircularDependency(t *testing.T) {
	exp := Explain(`circular dependency detected: a → b → a`)
	assert.Equal(t, "dependency", exp.Category)
	assert.Contains(t, exp.RootCause, "a → b → a")
}

func TestExplain_CELUndeclaredRef(t *testing.T) {
	exp := Explain(`undeclared reference to 'foo'`)
	assert.Equal(t, "cel_expression", exp.Category)
	assert.Contains(t, exp.Summary, "foo")
}

func TestExplain_CELNoOverload(t *testing.T) {
	exp := Explain(`found no matching overload for 'size'`)
	assert.Equal(t, "cel_expression", exp.Category)
	assert.Contains(t, exp.Summary, "size")
}

func TestExplain_NoSuchKey(t *testing.T) {
	exp := Explain(`no such key: statusCode`)
	assert.Equal(t, "data_access", exp.Category)
	assert.Contains(t, exp.Summary, "statusCode")
}

func TestExplain_NoSuchKey_HTTPHint(t *testing.T) {
	exp := Explain(`http provider: no such key: data`)
	assert.Equal(t, "data_access", exp.Category)
	// Should include HTTP-specific suggestion
	found := false
	for _, s := range exp.Suggestions {
		if s == "HTTP provider returns {statusCode, body, headers} - access response fields via body.<field>" {
			found = true
		}
	}
	assert.True(t, found, "should include HTTP-specific suggestion")
}

func TestExplain_PhaseTimeout(t *testing.T) {
	exp := Explain(`phase 2 timed out with 3 resolvers still waiting`)
	assert.Equal(t, "timeout", exp.Category)
	assert.Contains(t, exp.Summary, "Phase 2")
}

func TestExplain_ValueSize(t *testing.T) {
	exp := Explain(`resolver "bigData" value size 10485760 bytes exceeds maximum 1048576 bytes`)
	assert.Equal(t, "value_size", exp.Category)
	assert.Contains(t, exp.Summary, "bigData")
}

func TestExplain_ForEachType(t *testing.T) {
	exp := Explain(`resolver "items" transform step 1: forEach requires array input, got string`)
	assert.Equal(t, "type_mismatch", exp.Category)
	assert.Contains(t, exp.Summary, "items")
}

func TestExplain_AggregatedExecution(t *testing.T) {
	exp := Explain(`3 resolver(s) failed`)
	assert.Equal(t, "multiple_failures", exp.Category)
	assert.Contains(t, exp.Summary, "3")
}

func TestExplain_Unknown(t *testing.T) {
	exp := Explain(`some completely unknown error message`)
	assert.Equal(t, "unknown", exp.Category)
	assert.Equal(t, "some completely unknown error message", exp.RootCause)
	assert.NotEmpty(t, exp.Suggestions)
}

func TestExplain_CELProvider(t *testing.T) {
	exp := Explain(`resolver "calc" failed in transform phase (step 1, provider cel): type mismatch`)
	assert.Equal(t, "resolver_execution", exp.Category)
	// CEL-specific suggestions
	found := false
	for _, s := range exp.Suggestions {
		if s == "Use evaluate_cel to test the expression independently" {
			found = true
		}
	}
	assert.True(t, found, "should include CEL-specific suggestion")
}

func BenchmarkExplain(b *testing.B) {
	errors := []string{
		`resolver "api" failed in resolve phase (step 0, provider http): connection refused`,
		`circular dependency detected: a → b → a`,
		`undeclared reference to 'foo'`,
		`some unknown error`,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Explain(errors[i%len(errors)])
	}
}
