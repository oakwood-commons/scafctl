// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeferredValue_IsDeferred(t *testing.T) {
	tests := []struct {
		name     string
		value    *DeferredValue
		expected bool
	}{
		{"nil value", nil, false},
		{"not deferred", &DeferredValue{Deferred: false}, false},
		{"deferred expr", &DeferredValue{OriginalExpr: "__actions.build.results", Deferred: true}, true},
		{"deferred tmpl", &DeferredValue{OriginalTmpl: "{{ .__actions.build.results }}", Deferred: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.value.IsDeferred())
		})
	}
}

func TestDeferredValue_Evaluate(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		value        *DeferredValue
		resolverData map[string]any
		actionsData  map[string]any
		expected     any
		expectError  bool
	}{
		{
			name:        "nil value",
			value:       nil,
			expectError: true,
		},
		{
			name:        "not deferred",
			value:       &DeferredValue{Deferred: false},
			expectError: true,
		},
		{
			name: "evaluate expr with actions data",
			value: &DeferredValue{
				OriginalExpr: "__actions.build.results.exitCode",
				Deferred:     true,
			},
			resolverData: map[string]any{"env": "prod"},
			actionsData: map[string]any{
				"build": map[string]any{
					"results": map[string]any{
						"exitCode": int64(0),
					},
				},
			},
			expected: int64(0),
		},
		{
			name: "evaluate expr combining resolver and actions data",
			value: &DeferredValue{
				OriginalExpr: `_.env + "-" + __actions.build.results.output`,
				Deferred:     true,
			},
			resolverData: map[string]any{"env": "prod"},
			actionsData: map[string]any{
				"build": map[string]any{
					"results": map[string]any{
						"output": "success",
					},
				},
			},
			expected: "prod-success",
		},
		{
			name: "evaluate tmpl with actions data",
			value: &DeferredValue{
				OriginalTmpl: "Exit: {{ .__actions.build.results.exitCode }}",
				Deferred:     true,
			},
			actionsData: map[string]any{
				"build": map[string]any{
					"results": map[string]any{
						"exitCode": 0,
					},
				},
			},
			expected: "Exit: 0",
		},
		{
			name: "empty deferred value",
			value: &DeferredValue{
				Deferred: true,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.value.Evaluate(ctx, tt.resolverData, tt.actionsData)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaterialize(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		valueRef     *spec.ValueRef
		resolverData map[string]any
		checkResult  func(t *testing.T, result any)
		expectError  bool
	}{
		{
			name:     "nil valueref",
			valueRef: nil,
			checkResult: func(t *testing.T, result any) {
				assert.Nil(t, result)
			},
		},
		{
			name:     "literal value",
			valueRef: &spec.ValueRef{Literal: "hello"},
			checkResult: func(t *testing.T, result any) {
				assert.Equal(t, "hello", result)
			},
		},
		{
			name:         "resolver reference",
			valueRef:     &spec.ValueRef{Resolver: ptr("env")},
			resolverData: map[string]any{"env": "production"},
			checkResult: func(t *testing.T, result any) {
				assert.Equal(t, "production", result)
			},
		},
		{
			name:         "expr without __actions - evaluates immediately",
			valueRef:     &spec.ValueRef{Expr: ptr(celexp.Expression(`_.env + "-app"`))},
			resolverData: map[string]any{"env": "prod"},
			checkResult: func(t *testing.T, result any) {
				assert.Equal(t, "prod-app", result)
			},
		},
		{
			name:         "expr with __actions - returns deferred",
			valueRef:     &spec.ValueRef{Expr: ptr(celexp.Expression(`__actions.build.results.exitCode`))},
			resolverData: map[string]any{"env": "prod"},
			checkResult: func(t *testing.T, result any) {
				dv, ok := result.(*DeferredValue)
				require.True(t, ok, "expected DeferredValue")
				assert.True(t, dv.Deferred)
				assert.Equal(t, "__actions.build.results.exitCode", dv.OriginalExpr)
			},
		},
		{
			name:         "tmpl without __actions - evaluates immediately",
			valueRef:     &spec.ValueRef{Tmpl: ptr(gotmpl.GoTemplatingContent(`Env: {{ ._.env }}`))},
			resolverData: map[string]any{"env": "staging"},
			checkResult: func(t *testing.T, result any) {
				assert.Equal(t, "Env: staging", result)
			},
		},
		{
			name:         "tmpl with __actions - returns deferred",
			valueRef:     &spec.ValueRef{Tmpl: ptr(gotmpl.GoTemplatingContent(`{{ .__actions.build.results }}`))},
			resolverData: map[string]any{"env": "prod"},
			checkResult: func(t *testing.T, result any) {
				dv, ok := result.(*DeferredValue)
				require.True(t, ok, "expected DeferredValue")
				assert.True(t, dv.Deferred)
				assert.Equal(t, "{{ .__actions.build.results }}", dv.OriginalTmpl)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Materialize(ctx, tt.valueRef, tt.resolverData)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.checkResult != nil {
				tt.checkResult(t, result)
			}
		})
	}
}

func TestMaterializeInputs(t *testing.T) {
	ctx := context.Background()

	resolverData := map[string]any{
		"env":    "prod",
		"region": "us-east-1",
	}

	inputs := map[string]*spec.ValueRef{
		"literal":  {Literal: "hello"},
		"resolved": {Resolver: ptr("env")},
		"expr":     {Expr: ptr(celexp.Expression(`_.region`))},
		"deferred": {Expr: ptr(celexp.Expression(`__actions.build.results`))},
		"nil":      nil,
	}

	result, err := MaterializeInputs(ctx, inputs, resolverData)
	require.NoError(t, err)

	assert.Equal(t, "hello", result["literal"])
	assert.Equal(t, "prod", result["resolved"])
	assert.Equal(t, "us-east-1", result["expr"])
	assert.Nil(t, result["nil"])

	// Check deferred value
	dv, ok := result["deferred"].(*DeferredValue)
	require.True(t, ok)
	assert.True(t, dv.Deferred)
}

func TestMaterializeInputs_NilInputs(t *testing.T) {
	ctx := context.Background()
	result, err := MaterializeInputs(ctx, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHasDeferredValues(t *testing.T) {
	tests := []struct {
		name     string
		values   map[string]any
		expected bool
	}{
		{"nil map", nil, false},
		{"empty map", map[string]any{}, false},
		{"no deferred values", map[string]any{"a": "hello", "b": 123}, false},
		{
			"has deferred value",
			map[string]any{
				"a": "hello",
				"b": &DeferredValue{Deferred: true},
			},
			true,
		},
		{
			"deferred but not flagged",
			map[string]any{
				"a": &DeferredValue{Deferred: false},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, HasDeferredValues(tt.values))
		})
	}
}

func TestResolveDeferredValues(t *testing.T) {
	ctx := context.Background()

	resolverData := map[string]any{"env": "prod"}
	actionsData := map[string]any{
		"build": map[string]any{
			"results": map[string]any{
				"exitCode": int64(0),
				"output":   "success",
			},
		},
	}

	values := map[string]any{
		"literal": "hello",
		"number":  42,
		"deferred": &DeferredValue{
			OriginalExpr: "__actions.build.results.exitCode",
			Deferred:     true,
		},
	}

	result, err := ResolveDeferredValues(ctx, values, resolverData, actionsData)
	require.NoError(t, err)

	assert.Equal(t, "hello", result["literal"])
	assert.Equal(t, 42, result["number"])
	assert.Equal(t, int64(0), result["deferred"])
}

func TestResolveDeferredValues_NilValues(t *testing.T) {
	ctx := context.Background()
	result, err := ResolveDeferredValues(ctx, nil, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetDeferredInputNames(t *testing.T) {
	values := map[string]any{
		"literal":     "hello",
		"deferred1":   &DeferredValue{Deferred: true},
		"number":      42,
		"deferred2":   &DeferredValue{Deferred: true},
		"notDeferred": &DeferredValue{Deferred: false},
	}

	names := GetDeferredInputNames(values)

	assert.Len(t, names, 2)
	assert.Contains(t, names, "deferred1")
	assert.Contains(t, names, "deferred2")
}

// ptr is a helper to create pointers to values
func ptr[T any](v T) *T {
	return &v
}
