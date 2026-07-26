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

func TestExplain_StateRefStateDependent(t *testing.T) {
	exp := Explain(`state.enabled references state-dependent resolver(s) [saved]: those resolvers read state (or depend on one that does), so they cannot run before state is loaded (circular dependency)`)
	assert.Equal(t, "state", exp.Category)
	assert.Contains(t, exp.Summary, "state.enabled")
	assert.Contains(t, exp.RootCause, "saved")
	assert.NotEmpty(t, exp.Suggestions)
}

func TestExplain_StateRefUnknown(t *testing.T) {
	exp := Explain(`state.backend.inputs.path references unknown resolver(s) [typo]: no such resolver is defined in the solution`)
	assert.Equal(t, "state", exp.Category)
	assert.Contains(t, exp.Summary, "state.backend.inputs.path")
	assert.Contains(t, exp.RootCause, "typo")
	assert.NotEmpty(t, exp.Suggestions)
}

func TestExplain_StateSchemaVersionUnsupported(t *testing.T) {
	exp := Explain(`state load: unsupported state schema version: file version 4 is newer than supported version 3; upgrade scafctl`)
	assert.Equal(t, "state", exp.Category)
	assert.Contains(t, exp.Summary, "4")
	assert.Contains(t, exp.Summary, "3")
	assert.Contains(t, exp.RootCause, "newer version of scafctl")
	assert.NotEmpty(t, exp.Suggestions)
	assert.Contains(t, exp.Suggestions[0], "Upgrade scafctl")
}

func TestExplain_StateSchemaVersionIncompatible(t *testing.T) {
	exp := Explain(`state load: incompatible state schema version: file version 1 is older than the minimum supported version 2; delete the state file and recreate it`)
	assert.Equal(t, "state", exp.Category)
	assert.Contains(t, exp.Summary, "1")
	assert.Contains(t, exp.Summary, "2")
	assert.Contains(t, exp.RootCause, "breaking change")
	assert.NotEmpty(t, exp.Suggestions)
	assert.Contains(t, exp.Suggestions[0], "Delete the state file")
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

func TestExplain_DirectoryNotFound(t *testing.T) {
	exp := Explain(`directory "/tmp/static" does not exist: stat /tmp/static: no such file or directory`)
	assert.Equal(t, "path_resolution", exp.Category)
	assert.Contains(t, exp.Summary, "/tmp/static")
	assert.Contains(t, exp.Suggestions[0], "catalog pull")
}

func TestExplain_DirectoryNotFound_CatalogContext(t *testing.T) {
	exp := Explain(`catalog: directory "static" does not exist: stat static: no such file or directory`)
	assert.Equal(t, "path_resolution", exp.Category)
	assert.Contains(t, exp.RootCause, "loaded from a catalog without a bundle")
}

func TestExplain_StateMissingParams(t *testing.T) {
	exp := Explain(`state configuration requires parameters [project, env] that were not supplied: state: resolve backend inputs: failed to execute template`)
	assert.Equal(t, "state_configuration", exp.Category)
	assert.Contains(t, exp.Summary, "project, env")
	assert.Contains(t, exp.Suggestions[0], "-r project=<value>")
	assert.Contains(t, exp.Suggestions[0], "-r env=<value>")
	assert.Contains(t, exp.RootCause, "__params")
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
