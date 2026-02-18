// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

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
			assert.Contains(t, err.Error(), "expected exactly one of rslvr, expr, or tmpl")
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
		{
			name:        "invalid expression",
			expr:        "_.nonexistent.field",
			expected:    nil,
			expectError: true,
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmplContent := gotmpl.GoTemplatingContent(tt.tmpl)
			vr := ValueRef{Tmpl: &tmplContent}
			result, err := vr.Resolve(context.Background(), resolverData, nil)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestValueRef_Resolve_Empty_Error(t *testing.T) {
	ctx := context.Background()
	vr := ValueRef{} // All fields nil

	result, err := vr.Resolve(ctx, map[string]any{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty value reference")
	assert.Nil(t, result)
}

func TestValueRef_ComplexYAML_Integration(t *testing.T) {
	yamlData := `
resolvers:
  - name: env
    resolve:
      with:
        - provider: parameter
          inputs:
            key: environment
        - provider: static
          inputs:
            value: dev
  - name: region
    resolve:
      with:
        - provider: parameter
          inputs:
            key: region
        - provider: static
          inputs:
            value:
              rslvr: env
  - name: full-name
    resolve:
      with:
        - provider: static
          inputs:
            value:
              expr: _.env + '-' + _.region
`

	type ResolverInputs struct {
		Resolvers []Resolver `yaml:"resolvers"`
	}

	var input ResolverInputs
	err := yaml.Unmarshal([]byte(yamlData), &input)
	require.NoError(t, err)

	assert.Len(t, input.Resolvers, 3)

	// Verify first resolver with parameter + static fallback
	r1 := input.Resolvers[0]
	assert.Equal(t, "env", r1.Name)
	require.NotNil(t, r1.Resolve)
	assert.Len(t, r1.Resolve.With, 2)
	assert.Equal(t, "parameter", r1.Resolve.With[0].Provider)
	assert.Equal(t, "environment", r1.Resolve.With[0].Inputs["key"].Literal)
	assert.Equal(t, "static", r1.Resolve.With[1].Provider)
	assert.Equal(t, "dev", r1.Resolve.With[1].Inputs["value"].Literal)

	// Verify second resolver with rslvr reference in static fallback
	r2 := input.Resolvers[1]
	assert.Equal(t, "region", r2.Name)
	require.NotNil(t, r2.Resolve)
	assert.Len(t, r2.Resolve.With, 2)
	fallbackVal := r2.Resolve.With[1].Inputs["value"]
	require.NotNil(t, fallbackVal)
	require.NotNil(t, fallbackVal.Resolver)
	assert.Equal(t, "env", *fallbackVal.Resolver)

	// Verify third resolver with expr
	r3 := input.Resolvers[2]
	assert.Equal(t, "full-name", r3.Name)
	require.NotNil(t, r3.Resolve)
	valueVal := r3.Resolve.With[0].Inputs["value"]
	require.NotNil(t, valueVal)
	require.NotNil(t, valueVal.Expr)
	assert.Equal(t, "_.env + '-' + _.region", string(*valueVal.Expr))
}

func TestValueRef_UnmarshalYAML_InvalidYAMLNode(t *testing.T) {
	// Test with a YAML node that contains unexpected structure
	yamlData := `!!binary |
  R0lGODlhAQABAAAAACw=`

	var vr ValueRef
	err := yaml.Unmarshal([]byte(yamlData), &vr)
	// This should unmarshal as a literal string since it doesn't match the object structure
	require.NoError(t, err)
	assert.NotNil(t, vr.Literal)
}

func TestValueRef_Resolve_WithNilContext(t *testing.T) {
	// Test that Resolve works with nil values in data map
	resolverData := map[string]any{
		"nullable": nil,
	}

	vr := ValueRef{Literal: "test"}
	result, err := vr.Resolve(context.Background(), resolverData, nil)
	require.NoError(t, err)
	assert.Equal(t, "test", result)
}

func TestValueRef_Resolve_Tmpl_InvalidTemplate(t *testing.T) {
	resolverData := map[string]any{
		"environment": "production",
	}

	// Invalid template syntax
	tmplContent := gotmpl.GoTemplatingContent("{{ ._.environment")
	vr := ValueRef{Tmpl: &tmplContent}

	result, err := vr.Resolve(context.Background(), resolverData, nil)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestValueRef_Resolve_Expr_InvalidSyntax(t *testing.T) {
	resolverData := map[string]any{
		"count": 5,
	}

	// Invalid CEL expression syntax
	expr := celexp.Expression("_.count +++ 2")
	vr := ValueRef{Expr: &expr}

	result, err := vr.Resolve(context.Background(), resolverData, nil)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestValueRef_UnmarshalYAML_WithUnknownFields(t *testing.T) {
	// YAML with known and unknown fields
	yamlData := `
rslvr: environment
unknownField: someValue`

	var vr ValueRef
	err := yaml.Unmarshal([]byte(yamlData), &vr)
	require.NoError(t, err)
	require.NotNil(t, vr.Resolver)
	assert.Equal(t, "environment", *vr.Resolver)
}

func TestValueRef_Resolve_WithEmptyResolverData(t *testing.T) {
	resolverRef := "nonexistent"
	vr := ValueRef{Resolver: &resolverRef}

	result, err := vr.Resolve(context.Background(), map[string]any{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Nil(t, result)
}

func TestValueRef_Resolve_Literal_ComplexTypes(t *testing.T) {
	tests := []struct {
		name     string
		literal  any
		expected any
	}{
		{
			name:     "map literal",
			literal:  map[string]any{"key1": "value1", "key2": 42},
			expected: map[string]any{"key1": "value1", "key2": 42},
		},
		{
			name:     "nested array",
			literal:  []any{[]any{1, 2}, []any{3, 4}},
			expected: []any{[]any{1, 2}, []any{3, 4}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValueRef{Literal: tt.literal}
			result, err := vr.Resolve(context.Background(), map[string]any{}, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValueRef_Resolve_Literal_ZeroValue(t *testing.T) {
	// Test that zero values (0, false, "") are treated as valid literals
	tests := []struct {
		name     string
		literal  any
		expected any
	}{
		{
			name:     "zero int",
			literal:  0,
			expected: 0,
		},
		{
			name:     "false bool",
			literal:  false,
			expected: false,
		},
		{
			name:     "empty string",
			literal:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValueRef{Literal: tt.literal}
			result, err := vr.Resolve(context.Background(), map[string]any{}, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
