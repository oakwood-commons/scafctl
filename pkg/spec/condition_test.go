// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
