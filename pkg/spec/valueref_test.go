// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"context"
	"encoding/json"
	"fmt"
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
			name:        "direct resolver access",
			tmpl:        "{{ .environment }}",
			expected:    "production",
			expectError: false,
		},
		{
			name:        "direct multiple variables",
			tmpl:        "{{ .environment }}-{{ .region }}",
			expected:    "production-us-west-2",
			expectError: false,
		},
		{
			name:        "underscore prefix is not supported",
			tmpl:        "{{ ._.environment }}",
			expectError: true,
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
	actionExpr := celexp.Expression("__actions.build.result")
	nestedExpr := celexp.Expression("__actions")
	noMatchExpr := celexp.Expression("_.env")
	actionTmpl := gotmpl.GoTemplatingContent("{{.__actions.build.result}}")
	noMatchTmpl := gotmpl.GoTemplatingContent("{{._.env}}")

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
		{
			name:     "expr references __actions top-level",
			vr:       ValueRef{Expr: &actionExpr},
			varName:  "__actions",
			expected: true,
		},
		{
			name:     "expr references __actions directly",
			vr:       ValueRef{Expr: &nestedExpr},
			varName:  "__actions",
			expected: true,
		},
		{
			name:     "expr does not reference __actions",
			vr:       ValueRef{Expr: &noMatchExpr},
			varName:  "__actions",
			expected: false,
		},
		{
			name:     "tmpl references __actions",
			vr:       ValueRef{Tmpl: &actionTmpl},
			varName:  "__actions",
			expected: true,
		},
		{
			name:     "tmpl does not reference __actions",
			vr:       ValueRef{Tmpl: &noMatchTmpl},
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

func TestValueRef_MarshalYAML_Literal(t *testing.T) {
	tests := []struct {
		name     string
		vr       ValueRef
		expected string
	}{
		{
			name:     "string literal",
			vr:       ValueRef{Literal: "hello world"},
			expected: "hello world\n",
		},
		{
			name:     "integer literal",
			vr:       ValueRef{Literal: 42},
			expected: "42\n",
		},
		{
			name:     "float literal",
			vr:       ValueRef{Literal: 3.14},
			expected: "3.14\n",
		},
		{
			name:     "boolean literal",
			vr:       ValueRef{Literal: true},
			expected: "true\n",
		},
		{
			name:     "array literal",
			vr:       ValueRef{Literal: []any{1, 2, 3}},
			expected: "- 1\n- 2\n- 3\n",
		},
		{
			name:     "map literal",
			vr:       ValueRef{Literal: map[string]any{"key": "value"}},
			expected: "key: value\n",
		},
		{
			name:     "file path literal",
			vr:       ValueRef{Literal: "./templates/main.tf.tmpl"},
			expected: "./templates/main.tf.tmpl\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(&tt.vr)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(data))
		})
	}
}

func TestValueRef_MarshalYAML_Resolver(t *testing.T) {
	resolver := "environment"
	vr := ValueRef{Resolver: &resolver}

	data, err := yaml.Marshal(&vr)
	require.NoError(t, err)
	assert.Equal(t, "rslvr: environment\n", string(data))
}

func TestValueRef_MarshalYAML_Expr(t *testing.T) {
	expr := celexp.Expression("_.env == 'prod'")
	vr := ValueRef{Expr: &expr}

	data, err := yaml.Marshal(&vr)
	require.NoError(t, err)
	assert.Equal(t, "expr: _.env == 'prod'\n", string(data))
}

func TestValueRef_MarshalYAML_Tmpl(t *testing.T) {
	tmpl := gotmpl.GoTemplatingContent("{{ .name }}")
	vr := ValueRef{Tmpl: &tmpl}

	data, err := yaml.Marshal(&vr)
	require.NoError(t, err)
	assert.Equal(t, "tmpl: '{{ .name }}'\n", string(data))
}

func TestValueRef_MarshalYAML_Nil(t *testing.T) {
	vr := ValueRef{}

	data, err := yaml.Marshal(&vr)
	require.NoError(t, err)
	assert.Equal(t, "null\n", string(data))
}

func TestValueRef_YAML_RoundTrip(t *testing.T) {
	resolver := "environment"
	expr := celexp.Expression("_.env == 'prod'")
	tmpl := gotmpl.GoTemplatingContent("{{ .name }}")

	tests := []struct {
		name string
		vr   ValueRef
	}{
		{
			name: "string literal",
			vr:   ValueRef{Literal: "hello world"},
		},
		{
			name: "integer literal",
			vr:   ValueRef{Literal: 42},
		},
		{
			name: "float literal",
			vr:   ValueRef{Literal: 3.14},
		},
		{
			name: "boolean literal",
			vr:   ValueRef{Literal: true},
		},
		{
			name: "array literal",
			vr:   ValueRef{Literal: []any{1, 2, 3}},
		},
		{
			name: "map literal",
			vr:   ValueRef{Literal: map[string]any{"key": "value"}},
		},
		{
			name: "file path literal",
			vr:   ValueRef{Literal: "./templates/main.tf.tmpl"},
		},
		{
			name: "resolver reference",
			vr:   ValueRef{Resolver: &resolver},
		},
		{
			name: "expression",
			vr:   ValueRef{Expr: &expr},
		},
		{
			name: "template",
			vr:   ValueRef{Tmpl: &tmpl},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := yaml.Marshal(&tt.vr)
			require.NoError(t, err)

			// Unmarshal
			var got ValueRef
			err = yaml.Unmarshal(data, &got)
			require.NoError(t, err)

			// Compare
			assert.Equal(t, tt.vr.Literal, got.Literal, "Literal mismatch")
			if tt.vr.Resolver != nil {
				require.NotNil(t, got.Resolver)
				assert.Equal(t, *tt.vr.Resolver, *got.Resolver, "Resolver mismatch")
			} else {
				assert.Nil(t, got.Resolver)
			}
			if tt.vr.Expr != nil {
				require.NotNil(t, got.Expr)
				assert.Equal(t, string(*tt.vr.Expr), string(*got.Expr), "Expr mismatch")
			} else {
				assert.Nil(t, got.Expr)
			}
			if tt.vr.Tmpl != nil {
				require.NotNil(t, got.Tmpl)
				assert.Equal(t, string(*tt.vr.Tmpl), string(*got.Tmpl), "Tmpl mismatch")
			} else {
				assert.Nil(t, got.Tmpl)
			}
		})
	}
}

func TestValueRef_YAML_RoundTrip_InMap(t *testing.T) {
	// Simulates how ValueRef is used in practice: as values in a map (e.g., action inputs)
	resolver := "environment"
	expr := celexp.Expression("_.count * 2")

	inputs := map[string]*ValueRef{
		"message": {Literal: "hello world"},
		"count":   {Literal: 42},
		"path":    {Literal: "./templates/main.tf.tmpl"},
		"env":     {Resolver: &resolver},
		"doubled": {Expr: &expr},
	}

	data, err := yaml.Marshal(inputs)
	require.NoError(t, err)

	var got map[string]*ValueRef
	err = yaml.Unmarshal(data, &got)
	require.NoError(t, err)

	assert.Equal(t, "hello world", got["message"].Literal)
	assert.Equal(t, 42, got["count"].Literal)
	assert.Equal(t, "./templates/main.tf.tmpl", got["path"].Literal)
	require.NotNil(t, got["env"].Resolver)
	assert.Equal(t, "environment", *got["env"].Resolver)
	require.NotNil(t, got["doubled"].Expr)
	assert.Equal(t, "_.count * 2", string(*got["doubled"].Expr))
}

func TestValueRef_MarshalJSON_Literal(t *testing.T) {
	tests := []struct {
		name     string
		vr       ValueRef
		expected string
	}{
		{
			name:     "string literal",
			vr:       ValueRef{Literal: "hello"},
			expected: `"hello"`,
		},
		{
			name:     "integer literal",
			vr:       ValueRef{Literal: 42},
			expected: `42`,
		},
		{
			name:     "boolean literal",
			vr:       ValueRef{Literal: true},
			expected: `true`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.vr)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(data))
		})
	}
}

func TestValueRef_MarshalJSON_Resolver(t *testing.T) {
	resolver := "environment"
	vr := ValueRef{Resolver: &resolver}

	data, err := json.Marshal(&vr)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rslvr":"environment"}`, string(data))
}

func TestValueRef_MarshalJSON_Nil(t *testing.T) {
	vr := ValueRef{}

	data, err := json.Marshal(&vr)
	require.NoError(t, err)
	assert.Equal(t, "null", string(data))
}

func TestValueRef_UnmarshalJSON_Literal(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected any
	}{
		{
			name:     "string literal",
			json:     `"hello world"`,
			expected: "hello world",
		},
		{
			name:     "integer literal",
			json:     `42`,
			expected: float64(42), // JSON numbers decode as float64
		},
		{
			name:     "float literal",
			json:     `3.14`,
			expected: 3.14,
		},
		{
			name:     "boolean literal",
			json:     `true`,
			expected: true,
		},
		{
			name:     "array literal",
			json:     `[1, 2, 3]`,
			expected: []any{float64(1), float64(2), float64(3)},
		},
		{
			name:     "null literal",
			json:     `null`,
			expected: nil,
		},
		{
			name:     "map literal without known keys",
			json:     `{"key": "value"}`,
			expected: map[string]any{"key": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vr ValueRef
			err := json.Unmarshal([]byte(tt.json), &vr)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, vr.Literal)
			assert.Nil(t, vr.Resolver)
			assert.Nil(t, vr.Expr)
			assert.Nil(t, vr.Tmpl)
		})
	}
}

func TestValueRef_UnmarshalJSON_Resolver(t *testing.T) {
	var vr ValueRef
	err := json.Unmarshal([]byte(`{"rslvr":"environment"}`), &vr)
	require.NoError(t, err)

	assert.Nil(t, vr.Literal)
	require.NotNil(t, vr.Resolver)
	assert.Equal(t, "environment", *vr.Resolver)
	assert.Nil(t, vr.Expr)
	assert.Nil(t, vr.Tmpl)
}

func TestValueRef_UnmarshalJSON_Expr(t *testing.T) {
	var vr ValueRef
	err := json.Unmarshal([]byte(`{"expr":"_.env == 'prod'"}`), &vr)
	require.NoError(t, err)

	assert.Nil(t, vr.Literal)
	assert.Nil(t, vr.Resolver)
	require.NotNil(t, vr.Expr)
	assert.Equal(t, "_.env == 'prod'", string(*vr.Expr))
	assert.Nil(t, vr.Tmpl)
}

func TestValueRef_UnmarshalJSON_MultipleFields_Error(t *testing.T) {
	var vr ValueRef
	err := json.Unmarshal([]byte(`{"rslvr":"environment","expr":"_.env"}`), &vr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected exactly one of rslvr, expr, or tmpl, but found")
}

func TestValueRef_JSON_RoundTrip(t *testing.T) {
	resolver := "environment"
	expr := celexp.Expression("_.env == 'prod'")
	tmpl := gotmpl.GoTemplatingContent("{{ .name }}")

	tests := []struct {
		name string
		vr   ValueRef
	}{
		{name: "string literal", vr: ValueRef{Literal: "hello world"}},
		{name: "number literal", vr: ValueRef{Literal: float64(42)}},
		{name: "boolean literal", vr: ValueRef{Literal: true}},
		{name: "resolver", vr: ValueRef{Resolver: &resolver}},
		{name: "expression", vr: ValueRef{Expr: &expr}},
		{name: "template", vr: ValueRef{Tmpl: &tmpl}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.vr)
			require.NoError(t, err)

			var got ValueRef
			err = json.Unmarshal(data, &got)
			require.NoError(t, err)

			assert.Equal(t, tt.vr.Literal, got.Literal, "Literal mismatch")
			if tt.vr.Resolver != nil {
				require.NotNil(t, got.Resolver)
				assert.Equal(t, *tt.vr.Resolver, *got.Resolver)
			}
			if tt.vr.Expr != nil {
				require.NotNil(t, got.Expr)
				assert.Equal(t, string(*tt.vr.Expr), string(*got.Expr))
			}
			if tt.vr.Tmpl != nil {
				require.NotNil(t, got.Tmpl)
				assert.Equal(t, string(*tt.vr.Tmpl), string(*got.Tmpl))
			}
		})
	}
}

func TestValueRef_UnmarshalJSON_MalformedTypedRef_Error(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "expr with wrong type (object)",
			json: `{"expr": {}}`,
		},
		{
			name: "rslvr with wrong type (array)",
			json: `{"rslvr": []}`,
		},
		{
			name: "tmpl with wrong type (number)",
			json: `{"tmpl": 42}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vr ValueRef
			err := json.Unmarshal([]byte(tt.json), &vr)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid value ref")
		})
	}
}

func TestValueRef_UnmarshalJSON_ClearsStaleState(t *testing.T) {
	// Start with a ValueRef that has Literal set
	vr := ValueRef{Literal: "stale"}

	// Unmarshal a resolver ref into the same instance
	err := json.Unmarshal([]byte(`{"rslvr":"env"}`), &vr)
	require.NoError(t, err)

	assert.Nil(t, vr.Literal, "stale Literal should be cleared")
	require.NotNil(t, vr.Resolver)
	assert.Equal(t, "env", *vr.Resolver)

	// Now unmarshal a literal into the same instance
	err = json.Unmarshal([]byte(`"hello"`), &vr)
	require.NoError(t, err)

	assert.Equal(t, "hello", vr.Literal)
	assert.Nil(t, vr.Resolver, "stale Resolver should be cleared")
	assert.Nil(t, vr.Expr, "stale Expr should be cleared")
	assert.Nil(t, vr.Tmpl, "stale Tmpl should be cleared")
}

func TestValueRef_UnmarshalJSON_NullKnownKey_Error(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "null rslvr",
			json: `{"rslvr": null}`,
		},
		{
			name: "null expr",
			json: `{"expr": null}`,
		},
		{
			name: "null tmpl",
			json: `{"tmpl": null}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vr ValueRef
			err := json.Unmarshal([]byte(tt.json), &vr)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "null value")
		})
	}
}

func BenchmarkValueRef_MarshalYAML(b *testing.B) {
	expr := celexp.Expression("_.env == 'prod'")
	cases := []struct {
		name string
		vr   ValueRef
	}{
		{"literal_string", ValueRef{Literal: "hello"}},
		{"literal_int", ValueRef{Literal: 42}},
		{"expr", ValueRef{Expr: &expr}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var err error
			for b.Loop() {
				_, err = yaml.Marshal(&tc.vr)
			}
			if err != nil {
				b.Fatal(err)
			}
		})
	}
}

func BenchmarkValueRef_UnmarshalYAML(b *testing.B) {
	cases := []struct {
		name string
		data []byte
	}{
		{"literal_string", []byte(`"hello"`)},
		{"literal_int", []byte(`42`)},
		{"expr", []byte(`expr: _.env == 'prod'`)},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var err error
			for b.Loop() {
				var vr ValueRef
				err = yaml.Unmarshal(tc.data, &vr)
			}
			if err != nil {
				b.Fatal(err)
			}
		})
	}
}

func BenchmarkValueRef_MarshalJSON(b *testing.B) {
	expr := celexp.Expression("_.env == 'prod'")
	cases := []struct {
		name string
		vr   ValueRef
	}{
		{"literal_string", ValueRef{Literal: "hello"}},
		{"literal_int", ValueRef{Literal: 42}},
		{"expr", ValueRef{Expr: &expr}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var err error
			for b.Loop() {
				_, err = json.Marshal(&tc.vr)
			}
			if err != nil {
				b.Fatal(err)
			}
		})
	}
}

func BenchmarkValueRef_UnmarshalJSON(b *testing.B) {
	cases := []struct {
		name string
		data []byte
	}{
		{"literal_string", []byte(`"hello"`)},
		{"literal_int", []byte(`42`)},
		{"expr", []byte(`{"expr":"_.env == 'prod'"}`)},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var err error
			for b.Loop() {
				var vr ValueRef
				err = json.Unmarshal(tc.data, &vr)
			}
			if err != nil {
				b.Fatal(err)
			}
		})
	}
}

func TestValueRef_Resolve_NestedValueRefs(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"app-name":    "my-service",
		"environment": "production",
		"port":        int64(8080),
	}

	tests := []struct {
		name        string
		literal     any
		expected    any
		expectError bool
		errContains string
	}{
		{
			name: "nested rslvr in map",
			literal: map[string]any{
				"APP_NAME": map[string]any{"rslvr": "app-name"},
				"STATIC":   "literal-value",
			},
			expected: map[string]any{
				"APP_NAME": "my-service",
				"STATIC":   "literal-value",
			},
		},
		{
			name: "nested expr in map",
			literal: map[string]any{
				"GREETING": map[string]any{"expr": "'Hello ' + _['app-name']"},
			},
			expected: map[string]any{
				"GREETING": "Hello my-service",
			},
		},
		{
			name: "nested tmpl in map",
			literal: map[string]any{
				"URL": map[string]any{"tmpl": "https://{{ .environment }}.example.com"},
			},
			expected: map[string]any{
				"URL": "https://production.example.com",
			},
		},
		{
			name: "nested rslvr in array",
			literal: []any{
				map[string]any{"rslvr": "app-name"},
				"static-value",
				map[string]any{"expr": "string(_.port)"},
			},
			expected: []any{
				"my-service",
				"static-value",
				"8080",
			},
		},
		{
			name: "deeply nested value refs",
			literal: map[string]any{
				"outer": map[string]any{
					"inner": map[string]any{"rslvr": "environment"},
				},
			},
			expected: map[string]any{
				"outer": map[string]any{
					"inner": "production",
				},
			},
		},
		{
			name: "plain map without value ref keys passes through",
			literal: map[string]any{
				"host": "localhost",
				"port": 3000,
			},
			expected: map[string]any{
				"host": "localhost",
				"port": 3000,
			},
		},
		{
			name:     "scalar literal passes through",
			literal:  "hello",
			expected: "hello",
		},
		{
			name: "rslvr with non-string value errors",
			literal: map[string]any{
				"BAD": map[string]any{"rslvr": 123},
			},
			expectError: true,
			errContains: "rslvr value must be a string",
		},
		{
			name: "rslvr referencing non-existent resolver errors",
			literal: map[string]any{
				"MISSING": map[string]any{"rslvr": "nonexistent"},
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "map with rslvr and extra keys is not a value ref",
			literal: map[string]any{
				"rslvr": "app-name",
				"extra": "value",
			},
			expected: map[string]any{
				"rslvr": "app-name",
				"extra": "value",
			},
		},
		{
			name: "nesting at exactly max depth succeeds",
			literal: func() any {
				// Build a structure exactly maxNestedValueRefDepth levels deep.
				// Each level is a plain map (not a ValueRef), so depth increments
				// once per level. The leaf is a ValueRef at depth == maxNestedValueRefDepth.
				var cur any = map[string]any{"rslvr": "app-name"}
				for i := range maxNestedValueRefDepth {
					cur = map[string]any{fmt.Sprintf("l%d", i): cur}
				}
				return cur
			}(),
			expected: func() any {
				var cur any = "my-service"
				for i := range maxNestedValueRefDepth {
					cur = map[string]any{fmt.Sprintf("l%d", i): cur}
				}
				return cur
			}(),
		},
		{
			name: "nesting beyond max depth returns error",
			literal: func() any {
				// One level deeper than maxNestedValueRefDepth triggers the guard.
				var cur any = map[string]any{"rslvr": "app-name"}
				for i := range maxNestedValueRefDepth + 1 {
					cur = map[string]any{fmt.Sprintf("l%d", i): cur}
				}
				return cur
			}(),
			expectError: true,
			errContains: fmt.Sprintf("nested value ref exceeds maximum depth of %d", maxNestedValueRefDepth),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValueRef{Literal: tt.literal}
			result, err := vr.Resolve(ctx, resolverData, nil)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestValueRef_ReferencedVariables_NestedValueRefs(t *testing.T) {
	tests := []struct {
		name     string
		literal  any
		expected map[string]struct{}
	}{
		{
			name: "nested expr references _",
			literal: map[string]any{
				"APP": map[string]any{"expr": "_['app-name']"},
			},
			expected: map[string]struct{}{"_": {}, "app-name": {}},
		},
		{
			name: "nested expr references __actions",
			literal: map[string]any{
				"STATUS": map[string]any{"expr": "__actions.build.status"},
			},
			expected: map[string]struct{}{"__actions": {}},
		},
		{
			name: "nested tmpl references",
			literal: map[string]any{
				"URL": map[string]any{"tmpl": "{{ .environment }}"},
			},
			expected: map[string]struct{}{"environment": {}},
		},
		{
			name: "nested rslvr has no variable references",
			literal: map[string]any{
				"VAL": map[string]any{"rslvr": "env"},
			},
			expected: map[string]struct{}{},
		},
		{
			name: "plain literal has no references",
			literal: map[string]any{
				"host": "localhost",
			},
			expected: map[string]struct{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValueRef{Literal: tt.literal}
			vars := vr.ReferencedVariables()
			assert.Equal(t, tt.expected, vars)
		})
	}
}

func BenchmarkValueRef_Resolve_NestedValueRefs(b *testing.B) {
	ctx := context.Background()
	resolverData := map[string]any{
		"app-name":    "my-service",
		"environment": "production",
	}

	vr := ValueRef{
		Literal: map[string]any{
			"APP_NAME": map[string]any{"rslvr": "app-name"},
			"ENV":      map[string]any{"expr": "_.environment"},
			"STATIC":   "literal",
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := vr.Resolve(ctx, resolverData, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}
