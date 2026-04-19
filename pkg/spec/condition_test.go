// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCondition_Evaluate_NilCondition(t *testing.T) {
	ctx := context.Background()
	var cond *Condition
	result, err := cond.Evaluate(ctx, nil)
	require.NoError(t, err)
	assert.True(t, result, "nil condition should evaluate to true")
}

func TestCondition_Evaluate_NilExpr(t *testing.T) {
	ctx := context.Background()
	cond := &Condition{Expr: nil}
	result, err := cond.Evaluate(ctx, nil)
	require.NoError(t, err)
	assert.True(t, result, "condition with nil expr should evaluate to true")
}

func TestCondition_Evaluate_Simple(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		expr         string
		resolverData map[string]any
		expected     bool
		expectError  bool
	}{
		{
			name:         "simple true",
			expr:         "true",
			resolverData: nil,
			expected:     true,
			expectError:  false,
		},
		{
			name:         "simple false",
			expr:         "false",
			resolverData: nil,
			expected:     false,
			expectError:  false,
		},
		{
			name:         "equality check true",
			expr:         "_.environment == 'prod'",
			resolverData: map[string]any{"environment": "prod"},
			expected:     true,
			expectError:  false,
		},
		{
			name:         "equality check false",
			expr:         "_.environment == 'prod'",
			resolverData: map[string]any{"environment": "dev"},
			expected:     false,
			expectError:  false,
		},
		{
			name:         "comparison",
			expr:         "_.count > 5",
			resolverData: map[string]any{"count": 10},
			expected:     true,
			expectError:  false,
		},
		{
			name:         "non-boolean result",
			expr:         "_.count + 1",
			resolverData: map[string]any{"count": 5},
			expected:     false,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := celexp.Expression(tt.expr)
			cond := &Condition{Expr: &expr}
			result, err := cond.Evaluate(ctx, tt.resolverData)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "boolean")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestCondition_EvaluateWithSelf(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		expr        string
		self        any
		expected    bool
		expectError bool
	}{
		{
			name:        "__self check string",
			expr:        "__self == 'test'",
			self:        "test",
			expected:    true,
			expectError: false,
		},
		{
			name:        "__self check integer",
			expr:        "__self > 5",
			self:        10,
			expected:    true,
			expectError: false,
		},
		{
			name:        "__self not null check",
			expr:        "__self != null",
			self:        "value",
			expected:    true,
			expectError: false,
		},
		{
			name:        "__self null check",
			expr:        "__self == null",
			self:        nil,
			expected:    true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := celexp.Expression(tt.expr)
			cond := &Condition{Expr: &expr}
			result, err := cond.EvaluateWithSelf(ctx, nil, tt.self)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestCondition_EvaluateWithAdditionalVars(t *testing.T) {
	ctx := context.Background()

	expr := celexp.Expression("customVar == 'hello'")
	cond := &Condition{Expr: &expr}

	additionalVars := map[string]any{
		"customVar": "hello",
	}

	result, err := cond.EvaluateWithAdditionalVars(ctx, nil, additionalVars)
	require.NoError(t, err)
	assert.True(t, result)
}

func TestCondition_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantExpr    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "boolean true",
			input:    "true",
			wantExpr: "true",
		},
		{
			name:     "boolean false",
			input:    "false",
			wantExpr: "false",
		},
		{
			name:     "string shorthand",
			input:    `"_.environment == 'prod'"`,
			wantExpr: "_.environment == 'prod'",
		},
		{
			name:     "explicit object form",
			input:    `expr: "_.count > 5"`,
			wantExpr: "_.count > 5",
		},
		{
			name:     "expression alias",
			input:    `expression: "_.count > 5"`,
			wantExpr: "_.count > 5",
		},
		{
			name:        "both expr and expression",
			input:       "expr: \"a\"\nexpression: \"b\"",
			wantErr:     true,
			errContains: "not both",
		},
		{
			name:        "empty string",
			input:       `""`,
			wantErr:     true,
			errContains: "empty string",
		},
		{
			name:     "null value unmarshals to zero condition",
			input:    "null",
			wantExpr: "",
		},
		{
			name:        "integer value",
			input:       "42",
			wantErr:     true,
			errContains: "unsupported type",
		},
		{
			name:        "sequence value",
			input:       "- foo\n- bar",
			wantErr:     true,
			errContains: "unsupported node kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c Condition
			err := yaml.Unmarshal([]byte(tt.input), &c)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			if tt.wantExpr == "" {
				assert.Nil(t, c.Expr)
			} else {
				require.NotNil(t, c.Expr)
				assert.Equal(t, tt.wantExpr, string(*c.Expr))
			}
		})
	}
}

func TestCondition_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantExpr    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "boolean true",
			input:    "true",
			wantExpr: "true",
		},
		{
			name:     "boolean false",
			input:    "false",
			wantExpr: "false",
		},
		{
			name:     "string shorthand",
			input:    `"_.environment == 'prod'"`,
			wantExpr: "_.environment == 'prod'",
		},
		{
			name:     "explicit object form",
			input:    `{"expr": "_.count > 5"}`,
			wantExpr: "_.count > 5",
		},
		{
			name:     "expression alias",
			input:    `{"expression": "_.count > 5"}`,
			wantExpr: "_.count > 5",
		},
		{
			name:        "both expr and expression",
			input:       `{"expr": "a", "expression": "b"}`,
			wantErr:     true,
			errContains: "not both",
		},
		{
			name:        "empty string",
			input:       `""`,
			wantErr:     true,
			errContains: "empty string",
		},
		{
			name:  "null value",
			input: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c Condition
			err := json.Unmarshal([]byte(tt.input), &c)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			if tt.wantExpr == "" {
				assert.Nil(t, c.Expr)
			} else {
				require.NotNil(t, c.Expr)
				assert.Equal(t, tt.wantExpr, string(*c.Expr))
			}
		})
	}
}

func TestCondition_MarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		{name: "true", expr: "true", expected: "true\n"},
		{name: "false", expr: "false", expected: "false\n"},
		{name: "expression", expr: "_.env == 'prod'", expected: "_.env == 'prod'\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := celexp.Expression(tt.expr)
			c := Condition{Expr: &expr}
			data, err := yaml.Marshal(c)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(data))
		})
	}
}

func TestCondition_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		{name: "true", expr: "true", expected: "true"},
		{name: "false", expr: "false", expected: "false"},
		{name: "expression", expr: "_.env == 'prod'", expected: `"_.env == 'prod'"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := celexp.Expression(tt.expr)
			c := Condition{Expr: &expr}
			data, err := json.Marshal(c)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(data))
		})
	}
}

func TestCondition_MarshalJSON_Nil(t *testing.T) {
	c := Condition{Expr: nil}
	data, err := json.Marshal(c)
	require.NoError(t, err)
	assert.Equal(t, "null", string(data))
}

func TestCondition_YAMLRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "bool true", input: "true\n"},
		{name: "bool false", input: "false\n"},
		{name: "string expr", input: "_.env == 'prod'\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c Condition
			err := yaml.Unmarshal([]byte(tt.input), &c)
			require.NoError(t, err)

			data, err := yaml.Marshal(c)
			require.NoError(t, err)
			assert.Equal(t, tt.input, string(data))
		})
	}
}

func BenchmarkCondition_UnmarshalYAML_Bool(b *testing.B) {
	data := []byte("true")
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var c Condition
		_ = yaml.Unmarshal(data, &c)
	}
}

func BenchmarkCondition_UnmarshalYAML_String(b *testing.B) {
	data := []byte(`"_.environment == 'prod'"`)
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var c Condition
		_ = yaml.Unmarshal(data, &c)
	}
}

func BenchmarkCondition_UnmarshalYAML_Object(b *testing.B) {
	data := []byte(`expr: "_.environment == 'prod'"`)
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var c Condition
		_ = yaml.Unmarshal(data, &c)
	}
}

func BenchmarkCondition_UnmarshalJSON(b *testing.B) {
	data := []byte(`"_.environment == 'prod'"`)
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var c Condition
		_ = json.Unmarshal(data, &c)
	}
}
