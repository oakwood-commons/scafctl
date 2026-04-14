// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validationprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewValidationProvider(t *testing.T) {
	p := NewValidationProvider()

	assert.NotNil(t, p)
	assert.NotNil(t, p.descriptor)
	assert.Equal(t, "validation", p.descriptor.Name)
	assert.Equal(t, "Validation Provider", p.descriptor.DisplayName)
	assert.Equal(t, "v1", p.descriptor.APIVersion)
	assert.Equal(t, "validation", p.descriptor.Category)
	assert.Contains(t, p.descriptor.Capabilities, provider.CapabilityValidation)
}

func TestValidationProvider_Descriptor(t *testing.T) {
	p := NewValidationProvider()
	desc := p.Descriptor()

	assert.NotNil(t, desc)
	assert.Equal(t, "validation", desc.Name)
	assert.NotNil(t, desc.Schema.Properties)
	assert.Contains(t, desc.Schema.Properties, "value")
	assert.Contains(t, desc.Schema.Properties, "match")
	assert.Contains(t, desc.Schema.Properties, "notMatch")
	assert.Contains(t, desc.Schema.Properties, "expression")
	assert.Contains(t, desc.Schema.Properties, "failWhen")
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityValidation].Properties)
}

func TestValidationProvider_Execute_Match_Success(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value": "my-valid-value",
		"match": "^[a-z-]+$",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
	assert.Equal(t, "all validations passed", data["details"])
}

func TestValidationProvider_Execute_Match_Failure(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value": "my-invalid-value-123",
		"match": "^[a-z-]+$",
	}

	result, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "does not match pattern")
}

func TestValidationProvider_Execute_NotMatch_Success(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":    "my-value",
		"notMatch": "^test-",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
	assert.Equal(t, "all validations passed", data["details"])
}

func TestValidationProvider_Execute_NotMatch_Failure(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":    "test-value",
		"notMatch": "^test-",
	}

	result, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "matches forbidden pattern")
}

func TestValidationProvider_Execute_MatchAndNotMatch_Success(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":    "my-value",
		"match":    "^[a-z-]+$",
		"notMatch": "^test-",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
}

func TestValidationProvider_Execute_Expression_Success(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":      "dev",
		"expression": "__self in [\"dev\", \"staging\", \"prod\"]",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
}

func TestValidationProvider_Execute_Expression_Failure(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":      "invalid",
		"expression": "__self in [\"dev\", \"staging\", \"prod\"]",
	}

	result, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "expression evaluated to false")
}

func TestValidationProvider_Execute_WithSelf(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"__self": "test-value",
		"match":  "^[a-z-]+$",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
}

func TestValidationProvider_Execute_MissingValue(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"match": "^[a-z-]+$",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "either 'value' or '__self' is required")
}

func TestValidationProvider_Execute_MissingCriteria(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value": "test",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "at least one of")
}

func TestValidationProvider_Execute_InvalidMatchPattern(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value": "test",
		"match": "[invalid",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid match pattern")
}

func TestValidationProvider_Execute_InvalidExpression(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":      "test",
		"expression": "invalid syntax {{",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "expression")
}

func TestValidationProvider_Execute_ExpressionReturnsNonBoolean(t *testing.T) {
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":      "test",
		"expression": "__self + \"-suffix\"",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "must return boolean")
}

// ── failWhen tests ──

func TestValidationProvider_Execute_FailWhen_ConditionTrue(t *testing.T) {
	t.Parallel()
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":    map[string]any{"statusCode": 401},
		"failWhen": "__self.statusCode == 401",
		"message":  "Authentication failed (HTTP 401)",
	}

	result, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "Authentication failed (HTTP 401)")
}

func TestValidationProvider_Execute_FailWhen_ConditionFalse(t *testing.T) {
	t.Parallel()
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":    map[string]any{"statusCode": 200},
		"failWhen": "__self.statusCode == 401",
		"message":  "Authentication failed (HTTP 401)",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
}

func TestValidationProvider_Execute_FailWhen_DefaultMessage(t *testing.T) {
	t.Parallel()
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":    "bad",
		"failWhen": "__self == 'bad'",
	}

	result, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "error condition met")
	assert.Contains(t, err.Error(), "__self == 'bad'")
}

func TestValidationProvider_Execute_FailWhen_WithRegex(t *testing.T) {
	t.Parallel()
	p := NewValidationProvider()

	// failWhen can be combined with match/notMatch (regex checks first, then failWhen)
	ctx := context.Background()
	inputs := map[string]any{
		"value":    "test-service",
		"match":    "^[a-z-]+$",
		"failWhen": "__self == 'test-service'",
		"message":  "test-service is reserved",
	}

	result, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "test-service is reserved")
}

func TestValidationProvider_Execute_FailWhen_MutuallyExclusiveWithExpression(t *testing.T) {
	t.Parallel()
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":      "test",
		"expression": "__self != ''",
		"failWhen":   "__self == ''",
	}

	result, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestValidationProvider_Execute_FailWhen_NonBooleanResult(t *testing.T) {
	t.Parallel()
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value":    "test",
		"failWhen": "__self + '-suffix'",
	}

	result, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "must return boolean")
}

func TestValidationProvider_Execute_NoCriteria_IncludesFailWhen(t *testing.T) {
	t.Parallel()
	p := NewValidationProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"value": "test",
	}

	_, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failWhen")
}
