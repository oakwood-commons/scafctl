// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package envprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEnvProvider(t *testing.T) {
	p := NewEnvProvider()
	require.NotNil(t, p)

	desc := p.Descriptor()
	assert.Equal(t, "env", desc.Name)
	assert.Equal(t, "1.0.0", desc.Version.String())
	assert.Equal(t, "system", desc.Category)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)
	assert.NotNil(t, desc.Schema)
	assert.NotEmpty(t, desc.Schema.Properties)
	assert.NotNil(t, desc.OutputSchemas)
	// From returns AnyProp (string by default, object with expand: true)
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityFrom])
}

func TestEnvProvider_Execute_Get(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	// Set a test environment variable in mock
	testKey := "TEST_ENV_VAR_GET"
	testValue := "test-value-123"
	mockOps.Set(testKey, testValue)

	inputs := map[string]any{
		"operation": "get",
		"name":      testKey,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "get", data["operation"])
	assert.Equal(t, testKey, data["name"])
	assert.Equal(t, testValue, data["value"])
	assert.Equal(t, true, data["exists"])
}

func TestEnvProvider_Execute_Get_NotExists(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	testKey := "TEST_ENV_VAR_NOT_EXISTS"

	inputs := map[string]any{
		"operation": "get",
		"name":      testKey,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "get", data["operation"])
	assert.Equal(t, testKey, data["name"])
	assert.Equal(t, "", data["value"])
	assert.Equal(t, false, data["exists"])
}

func TestEnvProvider_Execute_Get_WithDefault(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	testKey := "TEST_ENV_VAR_DEFAULT"
	defaultValue := "default-value"

	inputs := map[string]any{
		"operation": "get",
		"name":      testKey,
		"default":   defaultValue,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "get", data["operation"])
	assert.Equal(t, testKey, data["name"])
	assert.Equal(t, defaultValue, data["value"])
	assert.Equal(t, false, data["exists"])
}

func TestEnvProvider_Execute_Set(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	testKey := "TEST_ENV_VAR_SET"
	testValue := "new-value-456"

	inputs := map[string]any{
		"operation": "set",
		"name":      testKey,
		"value":     testValue,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify the environment variable was set in mock
	actualValue, exists := mockOps.Get(testKey)
	assert.True(t, exists)
	assert.Equal(t, testValue, actualValue)

	data := output.Data.(map[string]any)
	assert.Equal(t, "set", data["operation"])
	assert.Equal(t, testKey, data["name"])
	assert.Equal(t, testValue, data["value"])

	// Check for warning metadata
	assert.NotNil(t, output.Metadata)
	assert.Contains(t, output.Metadata, "warning")
}

func TestEnvProvider_Execute_List(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	// Populate mock with test data
	mockOps.Set("PATH", "/usr/bin")
	mockOps.Set("HOME", "/home/user")
	mockOps.Set("USER", "testuser")

	// list without a prefix must be rejected
	inputs := map[string]any{
		"operation": "list",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "prefix is required")
}

func TestEnvProvider_Execute_List_WithPrefix(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	// Set some test environment variables with a specific prefix
	prefix := "TEST_PREFIX_"
	testVars := map[string]string{
		"TEST_PREFIX_VAR1": "value1",
		"TEST_PREFIX_VAR2": "value2",
		"TEST_PREFIX_VAR3": "value3",
		"OTHER_VAR":        "should-not-appear",
	}

	for key, value := range testVars {
		mockOps.Set(key, value)
	}

	inputs := map[string]any{
		"operation": "list",
		"prefix":    prefix,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	variables := data["variables"].(map[string]string)
	count := data["count"].(int)

	// Should only have the prefixed variables
	assert.Equal(t, 3, count)
	assert.Equal(t, "value1", variables["TEST_PREFIX_VAR1"])
	assert.Equal(t, "value2", variables["TEST_PREFIX_VAR2"])
	assert.Equal(t, "value3", variables["TEST_PREFIX_VAR3"])
	assert.NotContains(t, variables, "OTHER_VAR")
}

func TestEnvProvider_Execute_Unset(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	testKey := "TEST_ENV_VAR_UNSET"
	testValue := "to-be-removed"

	// Set the variable in mock
	mockOps.Set(testKey, testValue)

	// Verify it exists
	_, exists := mockOps.Get(testKey)
	assert.True(t, exists)

	inputs := map[string]any{
		"operation": "unset",
		"name":      testKey,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify the environment variable was removed
	_, exists = mockOps.Get(testKey)
	assert.False(t, exists)

	data := output.Data.(map[string]any)
	assert.Equal(t, "unset", data["operation"])
	assert.Equal(t, testKey, data["name"])

	// Check for warning metadata
	assert.NotNil(t, output.Metadata)
	assert.Contains(t, output.Metadata, "warning")
}

func TestEnvProvider_Execute_DryRun_Get(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"operation": "get",
		"name":      "TEST_VAR",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "get", data["operation"])
	assert.Equal(t, "TEST_VAR", data["name"])
	assert.Contains(t, data["value"], "DRY-RUN")
	assert.Equal(t, false, data["exists"])

	// Verify dry-run metadata
	assert.NotNil(t, output.Metadata)
	assert.Equal(t, true, output.Metadata["dryRun"])
}

func TestEnvProvider_Execute_DryRun_Set(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := provider.WithDryRun(context.Background(), true)

	testKey := "TEST_VAR_SET_DRYRUN"
	testValue := "should-not-be-set"

	inputs := map[string]any{
		"operation": "set",
		"name":      testKey,
		"value":     testValue,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify the variable was NOT actually set in mock
	_, exists := mockOps.Get(testKey)
	assert.False(t, exists, "Variable should not be set in dry-run mode")

	data := output.Data.(map[string]any)
	assert.Equal(t, "set", data["operation"])
	assert.Equal(t, testKey, data["name"])
	assert.Equal(t, testValue, data["value"])

	// Verify dry-run metadata
	assert.NotNil(t, output.Metadata)
	assert.Equal(t, true, output.Metadata["dryRun"])
}

func TestEnvProvider_Execute_DryRun_List(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"operation": "list",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "list", data["operation"])

	variables := data["variables"].(map[string]string)
	count := data["count"].(int)

	assert.Empty(t, variables)
	assert.Equal(t, 0, count)

	// Verify dry-run metadata
	assert.NotNil(t, output.Metadata)
	assert.Equal(t, true, output.Metadata["dryRun"])
}

func TestEnvProvider_Execute_DryRun_Unset(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := provider.WithDryRun(context.Background(), true)

	testKey := "TEST_VAR_UNSET_DRYRUN"
	testValue := "should-remain"

	// Set the variable in mock
	mockOps.Set(testKey, testValue)

	inputs := map[string]any{
		"operation": "unset",
		"name":      testKey,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify the variable was NOT actually unset
	value, exists := mockOps.Get(testKey)
	assert.True(t, exists, "Variable should still exist in dry-run mode")
	assert.Equal(t, testValue, value)

	data := output.Data.(map[string]any)
	assert.Equal(t, "unset", data["operation"])
	assert.Equal(t, testKey, data["name"])

	// Verify dry-run metadata
	assert.NotNil(t, output.Metadata)
	assert.Equal(t, true, output.Metadata["dryRun"])
}

func TestEnvProvider_Execute_InvalidOperation(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "invalid",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "unsupported operation")
}

func TestEnvProvider_Execute_MissingOperationField(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	inputs := map[string]any{
		"name": "TEST_VAR",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "operation is required")
}

func TestEnvProvider_Execute_Get_MissingName(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "get",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "name is required")
}

func TestEnvProvider_Execute_Set_MissingValue(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "set",
		"name":      "TEST_VAR",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "value is required")
}

func TestEnvProvider_Execute_Unset_MissingName(t *testing.T) {
	mockOps := NewMockEnvOps()
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "unset",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "name is required")
}

// Test error injection scenarios
func TestEnvProvider_Execute_Set_Error(t *testing.T) {
	mockOps := NewMockEnvOps()
	mockOps.SetenvErr = true // Inject error
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "set",
		"name":      "TEST_VAR",
		"value":     "test",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to set environment variable")
}

func TestEnvProvider_Execute_Unset_Error(t *testing.T) {
	mockOps := NewMockEnvOps()
	mockOps.UnsetenvErr = true // Inject error
	p := NewEnvProvider(WithEnvOps(mockOps))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "unset",
		"name":      "TEST_VAR",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to unset environment variable")
}

func TestDefaultEnvOps_LookupEnv(t *testing.T) {
	d := &DefaultEnvOps{}
	t.Setenv("TEST_LOOKUP_KEY", "test-value")
	val, ok := d.LookupEnv("TEST_LOOKUP_KEY")
	assert.True(t, ok)
	assert.Equal(t, "test-value", val)

	_, ok = d.LookupEnv("NON_EXISTENT_KEY_XYZ123")
	assert.False(t, ok)
}

func TestDefaultEnvOps_Setenv(t *testing.T) {
	d := &DefaultEnvOps{}
	err := d.Setenv("TEST_SET_KEY", "set-value")
	assert.NoError(t, err)
}

func TestDefaultEnvOps_Unsetenv(t *testing.T) {
	d := &DefaultEnvOps{}
	t.Setenv("TEST_UNSET_KEY", "unset-value")
	err := d.Unsetenv("TEST_UNSET_KEY")
	assert.NoError(t, err)
}
