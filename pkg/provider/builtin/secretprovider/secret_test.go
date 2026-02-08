// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secretprovider

import (
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSecretOps is a test implementation of SecretOps
type mockSecretOps struct {
	secrets map[string][]byte
	getErr  error
	listErr error
}

func (m *mockSecretOps) Get(_ context.Context, name string) ([]byte, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	val, ok := m.secrets[name]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	return val, nil
}

func (m *mockSecretOps) List(_ context.Context) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	names := make([]string, 0, len(m.secrets))
	for name := range m.secrets {
		names = append(names, name)
	}
	return names, nil
}

func (m *mockSecretOps) Exists(_ context.Context, name string) (bool, error) {
	_, ok := m.secrets[name]
	return ok, nil
}

func TestNewSecretProvider(t *testing.T) {
	p := NewSecretProvider()

	assert.NotNil(t, p)
	assert.NotNil(t, p.descriptor)
	assert.Equal(t, ProviderName, p.descriptor.Name)
	assert.Equal(t, ProviderDisplayName, p.descriptor.DisplayName)
	assert.Equal(t, ProviderAPIVersion, p.descriptor.APIVersion)
	assert.NotNil(t, p.descriptor.Schema)
	assert.NotNil(t, p.descriptor.OutputSchemas)
	assert.NotEmpty(t, p.descriptor.Examples)
}

func TestSecretProvider_Descriptor(t *testing.T) {
	p := NewSecretProvider()
	desc := p.Descriptor()

	assert.NotNil(t, desc)
	assert.Equal(t, ProviderName, desc.Name)

	// Check schema has required fields
	schema := desc.Schema
	assert.Contains(t, schema.Properties, FieldOperation)
	assert.Contains(t, schema.Properties, FieldName)
	assert.Contains(t, schema.Properties, FieldPattern)
	assert.Contains(t, schema.Properties, FieldRequired)
	assert.Contains(t, schema.Properties, FieldFallback)

	// Check operation field has enum
	opField := schema.Properties[FieldOperation]
	assert.Equal(t, []any{OpGet, OpList}, opField.Enum)
}

func TestSecretProvider_Execute_NoStore(t *testing.T) {
	p := NewSecretProvider()
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldName:      "test",
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret store not configured")
}

func TestSecretProvider_Execute_DryRun(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{
			"test": []byte("value"),
		},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldName:      "test",
		"__dry_run__":  true,
	}

	result, err := p.Execute(ctx, input)
	require.NoError(t, err)

	resultMap, ok := result.Data.(map[string]any)
	require.True(t, ok)
	assert.True(t, resultMap["_dry_run"].(bool))
	assert.Contains(t, resultMap["message"], "dry-run")
}

func TestSecretProvider_Execute_MissingOperation(t *testing.T) {
	ops := &mockSecretOps{secrets: make(map[string][]byte)}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldName: "test",
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "operation field is required")
}

func TestSecretProvider_Execute_UnsupportedOperation(t *testing.T) {
	ops := &mockSecretOps{secrets: make(map[string][]byte)}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: "invalid",
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported operation")
}

func TestSecretProvider_ExecuteGet_Success(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{
			"api-token": []byte("secret-value"),
		},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldName:      "api-token",
		FieldRequired:  true,
	}

	result, err := p.Execute(ctx, input)
	require.NoError(t, err)

	value, ok := result.Data.(string)
	require.True(t, ok)
	assert.Equal(t, "secret-value", value)
}

func TestSecretProvider_ExecuteGet_NotFoundRequired(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldName:      "missing",
		FieldRequired:  true,
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSecretProvider_ExecuteGet_NotFoundWithFallback(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldName:      "missing",
		FieldRequired:  false,
		FieldFallback:  "default-value",
	}

	result, err := p.Execute(ctx, input)
	require.NoError(t, err)

	value, ok := result.Data.(string)
	require.True(t, ok)
	assert.Equal(t, "default-value", value)
}

func TestSecretProvider_ExecuteGet_PatternMatch(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{
			"dev-token":  []byte("dev-value"),
			"prod-token": []byte("prod-value"),
		},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldPattern:   "^prod-.+$",
		FieldRequired:  true,
	}

	result, err := p.Execute(ctx, input)
	require.NoError(t, err)

	value, ok := result.Data.(string)
	require.True(t, ok)
	assert.Equal(t, "prod-value", value)
}

func TestSecretProvider_ExecuteGet_PatternNoMatch(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{
			"dev-token": []byte("dev-value"),
		},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldPattern:   "^prod-.+$",
		FieldRequired:  true,
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no secret found matching pattern")
}

func TestSecretProvider_ExecuteGet_PatternInvalidRegex(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{
			"test": []byte("value"),
		},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldPattern:   "[invalid((",
		FieldRequired:  true,
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex pattern")
}

func TestSecretProvider_ExecuteGet_MissingNameAndPattern(t *testing.T) {
	ops := &mockSecretOps{secrets: make(map[string][]byte)}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldRequired:  true,
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "either 'name' or 'pattern' field is required")
}

func TestSecretProvider_ExecuteGet_StoreError(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{},
		getErr:  errors.New("store error"),
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpGet,
		FieldName:      "test",
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get secret")
}

func TestSecretProvider_ExecuteList_Success(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{
			"secret1": []byte("value1"),
			"secret2": []byte("value2"),
		},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpList,
	}

	result, err := p.Execute(ctx, input)
	require.NoError(t, err)

	secrets, ok := result.Data.([]string)
	require.True(t, ok)
	assert.Len(t, secrets, 2)
	assert.Contains(t, secrets, "secret1")
	assert.Contains(t, secrets, "secret2")
}

func TestSecretProvider_ExecuteList_Empty(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpList,
	}

	result, err := p.Execute(ctx, input)
	require.NoError(t, err)

	secrets, ok := result.Data.([]string)
	require.True(t, ok)
	assert.Empty(t, secrets)
}

func TestSecretProvider_ExecuteList_StoreError(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{},
		listErr: errors.New("store error"),
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	input := map[string]any{
		FieldOperation: OpList,
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list secrets")
}

func TestSecretProvider_RequiredDefaultTrue(t *testing.T) {
	ops := &mockSecretOps{
		secrets: map[string][]byte{},
	}
	p := NewSecretProvider(WithSecretOps(ops))
	ctx := context.Background()

	// Don't specify required field - should default to true
	input := map[string]any{
		FieldOperation: OpGet,
		FieldName:      "missing",
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
