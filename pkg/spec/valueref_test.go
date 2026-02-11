// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestValueRef_UnmarshalYAML_Literal(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected any
	}{
		{
			name:     "string literal",
			yaml:     `"hello world"`,
			expected: "hello world",
		},
		{
			name:     "integer literal",
			yaml:     `42`,
			expected: 42,
		},
		{
			name:     "float literal",
			yaml:     `3.14`,
			expected: 3.14,
		},
		{
			name:     "boolean literal",
			yaml:     `true`,
			expected: true,
		},
		{
			name:     "array literal",
			yaml:     `[1, 2, 3]`,
			expected: []any{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vr ValueRef
			err := yaml.Unmarshal([]byte(tt.yaml), &vr)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, vr.Literal)
			assert.Nil(t, vr.Resolver)
			assert.Nil(t, vr.Expr)
			assert.Nil(t, vr.Tmpl)
		})
	}
}

func TestValueRef_UnmarshalYAML_Resolver(t *testing.T) {
	yamlData := `rslvr: environment`

	var vr ValueRef
	err := yaml.Unmarshal([]byte(yamlData), &vr)
	require.NoError(t, err)

	assert.Nil(t, vr.Literal)
	require.NotNil(t, vr.Resolver)
	assert.Equal(t, "environment", *vr.Resolver)
	assert.Nil(t, vr.Expr)
	assert.Nil(t, vr.Tmpl)
}

func TestValueRef_UnmarshalYAML_Expr(t *testing.T) {
	yamlData := `expr: _.env == 'prod'`

	var vr ValueRef
	err := yaml.Unmarshal([]byte(yamlData), &vr)
	require.NoError(t, err)

	assert.Nil(t, vr.Literal)
	assert.Nil(t, vr.Resolver)
	require.NotNil(t, vr.Expr)
	assert.Equal(t, "_.env == 'prod'", string(*vr.Expr))
	assert.Nil(t, vr.Tmpl)
}

func TestValueRef_UnmarshalYAML_Tmpl(t *testing.T) {
	yamlData := `tmpl: "{{ .name }}"`

	var vr ValueRef
	err := yaml.Unmarshal([]byte(yamlData), &vr)
	require.NoError(t, err)

	assert.Nil(t, vr.Literal)
	assert.Nil(t, vr.Resolver)
	assert.Nil(t, vr.Expr)
	require.NotNil(t, vr.Tmpl)
}

func TestValueRef_UnmarshalYAML_MultipleFields_Error(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "rslvr and expr",
			yaml: `
rslvr: environment
expr: _.env == 'prod'`,
		},
		{
			name: "rslvr and tmpl",
			yaml: `
rslvr: environment
tmpl: "{{ .name }}"`,
		},
		{
			name: "expr and tmpl",
			yaml: `
expr: _.env == 'prod'
tmpl: "{{ .name }}"`,
		},
		{
			name: "all three fields",
			yaml: `
rslvr: environment
expr: _.env == 'prod'
tmpl: "{{ .name }}"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vr ValueRef
			err := yaml.Unmarshal([]byte(tt.yaml), &vr)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "expected exactly one of rslvr, expr, or tmpl, but found")
		})
	}
}

func TestValueRef_UnmarshalYAML_EmptyMap_Literal(t *testing.T) {
	yamlData := `{}`

	var vr ValueRef
	err := yaml.Unmarshal([]byte(yamlData), &vr)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, vr.Literal)
}

func TestValueRef_Resolve_Literal(t *testing.T) {
	tests := []struct {
		name     string
		vr       ValueRef
		expected any
	}{
		{
			name:     "string literal",
			vr:       ValueRef{Literal: "hello"},
			expected: "hello",
		},
		{
			name:     "integer literal",
			vr:       ValueRef{Literal: 42},
			expected: 42,
		},
		{
			name:     "boolean literal",
			vr:       ValueRef{Literal: true},
			expected: true,
		},
	}

	ctx := context.Background()
	resolverData := map[string]any{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.vr.Resolve(ctx, resolverData, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValueRef_Resolve_Resolver(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"environment": "production",
		"region":      "us-west-2",
	}

	tests := []struct {
		name        string
		resolverRef string
		expected    any
		expectError bool
	}{
		{
			name:        "existing resolver",
			resolverRef: "environment",
			expected:    "production",
			expectError: false,
		},
		{
			name:        "another existing resolver",
			resolverRef: "region",
			expected:    "us-west-2",
			expectError: false,
		},
		{
			name:        "non-existent resolver",
			resolverRef: "nonexistent",
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValueRef{Resolver: &tt.resolverRef}
			result, err := vr.Resolve(ctx, resolverData, nil)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not found")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestValueRef_Resolve_Expr(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"environment": "production",
		"count":       5,
	}

	tests := []struct {
		name        string
		expr        string
		expected    any
		expectError bool
	}{
		{
			name:        "simple equality",
			expr:        "_.environment == 'production'",
			expected:    true,
			expectError: false,
		},
		{
			name:        "arithmetic",
			expr:        "_.count * 2",
			expected:    int64(10),
			expectError: false,
		},
		{
			name:        "string concatenation",
			expr:        "_.environment + '-app'",
			expected:    "production-app",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := celexp.Expression(tt.expr)
			vr := ValueRef{Expr: &expr}
			result, err := vr.Resolve(ctx, resolverData, nil)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestValueRef_Resolve_Tmpl(t *testing.T) {
	resolverData := map[string]any{
		"environment": "production",
		"region":      "us-west-2",
	}

	tests := []struct {
		name        string
		tmpl        string
		expected    string
		expectError bool
	}{
		{
			name:        "simple variable",
			tmpl:        "{{ ._.environment }}",
			expected:    "production",
			expectError: false,
		},
		{
			name:        "multiple variables",
			tmpl:        "{{ ._.environment }}-{{ ._.region }}",
			expected:    "production-us-west-2",
			expectError: false,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := gotmpl.GoTemplatingContent(tt.tmpl)
			vr := ValueRef{Tmpl: &tmpl}
			result, err := vr.Resolve(ctx, resolverData, nil)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestValueRef_Resolve_NilValueRef(t *testing.T) {
	ctx := context.Background()
	var vr *ValueRef
	result, err := vr.Resolve(ctx, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestValueRef_Resolve_EmptyValueRef(t *testing.T) {
	ctx := context.Background()
	vr := &ValueRef{}
	_, err := vr.Resolve(ctx, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty value reference")
}

func TestValueRef_ResolveWithIterationContext(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"prefix": "item",
	}

	iterCtx := &IterationContext{
		Item:       "element",
		Index:      5,
		ItemAlias:  "el",
		IndexAlias: "i",
	}

	tests := []struct {
		name     string
		expr     string
		expected any
	}{
		{
			name:     "__item access",
			expr:     "__item",
			expected: "element",
		},
		{
			name:     "__index access",
			expr:     "__index",
			expected: int64(5),
		},
		{
			name:     "item alias",
			expr:     "el",
			expected: "element",
		},
		{
			name:     "index alias",
			expr:     "i",
			expected: int64(5),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := celexp.Expression(tt.expr)
			vr := ValueRef{Expr: &expr}
			result, err := vr.ResolveWithIterationContext(ctx, resolverData, nil, iterCtx)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValueRef_ReferencesVariable(t *testing.T) {
	tests := []struct {
		name     string
		vr       ValueRef
		varName  string
		expected bool
	}{
		{
			name:     "nil valueref",
			vr:       ValueRef{},
			varName:  "__actions",
			expected: false,
		},
		{
			name:     "literal does not reference",
			vr:       ValueRef{Literal: "test"},
			varName:  "__actions",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.vr.ReferencesVariable(tt.varName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIterationContext(t *testing.T) {
	ctx := &IterationContext{
		Item:       "test-item",
		Index:      3,
		ItemAlias:  "myItem",
		IndexAlias: "myIndex",
	}

	assert.Equal(t, "test-item", ctx.Item)
	assert.Equal(t, 3, ctx.Index)
	assert.Equal(t, "myItem", ctx.ItemAlias)
	assert.Equal(t, "myIndex", ctx.IndexAlias)
}
