// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolverType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    Type
		expected string
	}{
		{"string type", TypeString, "string"},
		{"int type", TypeInt, "int"},
		{"float type", TypeFloat, "float"},
		{"bool type", TypeBool, "bool"},
		{"array type", TypeArray, "array"},
		{"any type", TypeAny, "any"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.value))
		})
	}
}

func TestErrorBehavior_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    ErrorBehavior
		expected string
	}{
		{"fail behavior", ErrorBehaviorFail, "fail"},
		{"continue behavior", ErrorBehaviorContinue, "continue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.value))
		})
	}
}

func TestResolver_Structure(t *testing.T) {
	timeout := 30 * time.Second
	expr := celexp.Expression("_.env == 'prod'")

	resolver := Resolver{
		Name:        "test-resolver",
		Description: "A test resolver",
		DisplayName: "Test Resolver",
		Sensitive:   true,
		Example:     "example-value",
		Type:        TypeString,
		When: &Condition{
			Expr: &expr,
		},
		Timeout: &timeout,
		Resolve: &ResolvePhase{
			With: []ProviderSource{
				{
					Provider: "parameter",
					Inputs: map[string]*ValueRef{
						"key": {Literal: "test-key"},
					},
				},
			},
		},
		Transform: &TransformPhase{
			With: []ProviderTransform{
				{
					Provider: "cel",
					Inputs: map[string]*ValueRef{
						"expr": {Literal: "value.toUpper()"},
					},
				},
			},
		},
		Validate: &ValidatePhase{
			With: []ProviderValidation{
				{
					Provider: "validation",
					Inputs: map[string]*ValueRef{
						"rule": {Literal: "len(value) > 0"},
					},
				},
			},
		},
	}

	assert.Equal(t, "test-resolver", resolver.Name)
	assert.Equal(t, "A test resolver", resolver.Description)
	assert.Equal(t, "Test Resolver", resolver.DisplayName)
	assert.True(t, resolver.Sensitive)
	assert.Equal(t, "example-value", resolver.Example)
	assert.Equal(t, TypeString, resolver.Type)
	require.NotNil(t, resolver.When)
	require.NotNil(t, resolver.When.Expr)
	assert.Equal(t, "_.env == 'prod'", string(*resolver.When.Expr))
	require.NotNil(t, resolver.Timeout)
	assert.Equal(t, 30*time.Second, *resolver.Timeout)
	require.NotNil(t, resolver.Resolve)
	assert.Len(t, resolver.Resolve.With, 1)
	require.NotNil(t, resolver.Transform)
	assert.Len(t, resolver.Transform.With, 1)
	require.NotNil(t, resolver.Validate)
	assert.Len(t, resolver.Validate.With, 1)
}

func TestResolverConfig_Defaults(t *testing.T) {
	config := Config{
		MaxValueSizeBytes:  10 * 1024 * 1024, // 10MB
		WarnValueSizeBytes: 1 * 1024 * 1024,  // 1MB
		MaxConcurrency:     10,
		PhaseTimeout:       5 * time.Minute,
	}

	assert.Equal(t, int64(10*1024*1024), config.MaxValueSizeBytes)
	assert.Equal(t, int64(1*1024*1024), config.WarnValueSizeBytes)
	assert.Equal(t, 10, config.MaxConcurrency)
	assert.Equal(t, 5*time.Minute, config.PhaseTimeout)
}

func TestProviderSource_OnError(t *testing.T) {
	tests := []struct {
		name            string
		source          ProviderSource
		expectedOnError ErrorBehavior
	}{
		{
			name: "default error behavior",
			source: ProviderSource{
				Provider: "parameter",
				Inputs:   map[string]*ValueRef{},
			},
			expectedOnError: "", // Empty string for default
		},
		{
			name: "explicit fail behavior",
			source: ProviderSource{
				Provider: "parameter",
				Inputs:   map[string]*ValueRef{},
				OnError:  ErrorBehaviorFail,
			},
			expectedOnError: ErrorBehaviorFail,
		},
		{
			name: "continue behavior",
			source: ProviderSource{
				Provider: "parameter",
				Inputs:   map[string]*ValueRef{},
				OnError:  ErrorBehaviorContinue,
			},
			expectedOnError: ErrorBehaviorContinue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedOnError, tt.source.OnError)
		})
	}
}

func TestCondition_Structure(t *testing.T) {
	expr := celexp.Expression("_.env == 'prod'")
	condition := Condition{
		Expr: &expr,
	}

	require.NotNil(t, condition.Expr)
	assert.Equal(t, "_.env == 'prod'", string(*condition.Expr))
}

func TestResolvePhase_Structure(t *testing.T) {
	expr := celexp.Expression("value != null")
	phase := ResolvePhase{
		With: []ProviderSource{
			{
				Provider: "parameter",
				Inputs: map[string]*ValueRef{
					"key": {Literal: "test-key"},
				},
			},
		},
		Until: &Condition{
			Expr: &expr,
		},
	}

	assert.Len(t, phase.With, 1)
	assert.Equal(t, "parameter", phase.With[0].Provider)
	require.NotNil(t, phase.Until)
	require.NotNil(t, phase.Until.Expr)
	assert.Equal(t, "value != null", string(*phase.Until.Expr))
}

func TestTransformPhase_Structure(t *testing.T) {
	phase := TransformPhase{
		With: []ProviderTransform{
			{
				Provider: "cel",
				Inputs: map[string]*ValueRef{
					"expr": {Literal: "value.toUpper()"},
				},
			},
		},
	}

	assert.Len(t, phase.With, 1)
	assert.Equal(t, "cel", phase.With[0].Provider)
}

func TestValidatePhase_Structure(t *testing.T) {
	phase := ValidatePhase{
		With: []ProviderValidation{
			{
				Provider: "validation",
				Inputs: map[string]*ValueRef{
					"rule": {Literal: "len(value) > 0"},
				},
				Message: &ValueRef{
					Literal: "Value must not be empty",
				},
			},
		},
	}

	assert.Len(t, phase.With, 1)
	assert.Equal(t, "validation", phase.With[0].Provider)
	require.NotNil(t, phase.With[0].Message)
	assert.Equal(t, "Value must not be empty", phase.With[0].Message.Literal)
}

func TestProviderValidation_WithMessage(t *testing.T) {
	validation := ProviderValidation{
		Provider: "validation",
		Inputs: map[string]*ValueRef{
			"rule": {Literal: "len(value) > 5"},
		},
		Message: &ValueRef{
			Literal: "Value must be at least 6 characters",
		},
	}

	assert.Equal(t, "validation", validation.Provider)
	require.NotNil(t, validation.Message)
	assert.Equal(t, "Value must be at least 6 characters", validation.Message.Literal)
}
