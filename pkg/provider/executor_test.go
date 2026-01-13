package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockExecutableProvider is a provider that can be executed for testing.
type mockExecutableProvider struct {
	descriptor    *Descriptor
	executeFunc   func(ctx context.Context, inputs map[string]any) (*Output, error)
	executeCalled bool
}

func (m *mockExecutableProvider) Descriptor() *Descriptor {
	return m.descriptor
}

func (m *mockExecutableProvider) Execute(ctx context.Context, inputs map[string]any) (*Output, error) {
	m.executeCalled = true
	if m.executeFunc != nil {
		return m.executeFunc(ctx, inputs)
	}
	return &Output{Data: map[string]any{"result": "success"}}, nil
}

// newMockExecutableProvider creates a mock executable provider for testing.
func newMockExecutableProvider(name string, executeFunc func(ctx context.Context, inputs map[string]any) (*Output, error)) *mockExecutableProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         name,
			Version:      version,
			Capabilities: []Capability{CapabilityFrom},
			Schema: SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"input1": {Type: PropertyTypeString, Required: true},
					"input2": {Type: PropertyTypeInt, Required: false},
				},
			},
			OutputSchema: SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"result": {Type: PropertyTypeString},
				},
			},
		},
		executeFunc: executeFunc,
	}
}

func TestNewExecutor(t *testing.T) {
	tests := []struct {
		name string
		opts []ExecutorOption
	}{
		{
			name: "default executor",
			opts: nil,
		},
		{
			name: "with custom schema validator",
			opts: []ExecutorOption{WithSchemaValidator(NewSchemaValidator())},
		},
		{
			name: "with custom schema validator again",
			opts: []ExecutorOption{
				WithSchemaValidator(NewSchemaValidator()),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewExecutor(tt.opts...)
			assert.NotNil(t, executor)
			assert.NotNil(t, executor.schemaValidator)
		})
	}
}

func TestExecutor_Execute_Success(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
		"input2": 42,
	}

	result, err := executor.Execute(ctx, provider, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, provider, result.Provider)
	assert.False(t, result.DryRun)
	assert.Equal(t, "success", result.Output.Data.(map[string]any)["result"])
	assert.True(t, provider.executeCalled)
}

func TestExecutor_Execute_DryRun(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", func(ctx context.Context, _ map[string]any) (*Output, error) {
		// Provider should check dry-run mode from context
		if DryRunFromContext(ctx) {
			return &Output{
				Data: map[string]any{
					"_dryRun":  true,
					"_message": "Provider handled dry-run",
				},
			}, nil
		}
		return &Output{Data: map[string]any{"result": "success"}}, nil
	})

	ctx := WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := executor.Execute(ctx, provider, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.DryRun)
	assert.Equal(t, true, result.Output.Data.(map[string]any)["_dryRun"])
	assert.Contains(t, result.Output.Data.(map[string]any)["_message"], "Provider handled dry-run")
	assert.True(t, provider.executeCalled, "provider should be executed even in dry-run mode")
}

func TestExecutor_Execute_NilProvider(t *testing.T) {
	executor := NewExecutor()

	ctx := context.Background()
	inputs := map[string]any{"input1": "test"}

	result, err := executor.Execute(ctx, nil, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "provider cannot be nil")
}

func TestExecutor_Execute_NilDescriptor(t *testing.T) {
	executor := NewExecutor()

	// Create a provider with nil descriptor
	provider := &mockExecutableProvider{
		descriptor: nil,
	}

	ctx := context.Background()
	inputs := map[string]any{"input1": "test"}

	result, err := executor.Execute(ctx, provider, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "descriptor cannot be nil")
}

func TestExecutor_Execute_InputResolutionError(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := context.Background()
	// Missing required input
	inputs := map[string]any{
		"input2": 42,
	}

	result, err := executor.Execute(ctx, provider, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "required property")
}

func TestExecutor_Execute_InputValidationError(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := context.Background()
	// Wrong type for input2
	inputs := map[string]any{
		"input1": "test-value",
		"input2": "not-an-int",
	}

	result, err := executor.Execute(ctx, provider, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestExecutor_Execute_ProviderExecutionError(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", func(_ context.Context, _ map[string]any) (*Output, error) {
		return nil, fmt.Errorf("provider execution failed")
	})

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := executor.Execute(ctx, provider, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "provider execution failed")
}

func TestExecutor_Execute_OutputValidationError(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", func(_ context.Context, _ map[string]any) (*Output, error) {
		// Return output with wrong type
		return &Output{Data: map[string]any{"result": 123}}, nil
	})

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := executor.Execute(ctx, provider, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "output validation failed")
}

func TestExecutor_Execute_WithResolverContext(t *testing.T) {
	executor := NewExecutor()

	// Create provider that expects resolver binding
	version, _ := semver.NewVersion("1.0.0")
	provider := &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         "test-provider",
			Version:      version,
			Capabilities: []Capability{CapabilityFrom},
			Schema: SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"input1": {Type: PropertyTypeString, Required: true},
				},
			},
			OutputSchema: SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"result": {Type: PropertyTypeString},
				},
			},
		},
		executeFunc: func(_ context.Context, inputs map[string]any) (*Output, error) {
			return &Output{Data: map[string]any{"result": inputs["input1"]}}, nil
		},
	}

	// Set up resolver context
	resolverCtx := map[string]any{
		"config": map[string]any{
			"database": map[string]any{
				"host": "localhost",
			},
		},
	}

	ctx := WithResolverContext(context.Background(), resolverCtx)

	// Use resolver binding
	inputs := map[string]any{
		"input1": InputValue{
			Rslvr: "config.database.host",
		},
	}

	result, err := executor.Execute(ctx, provider, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "localhost", result.Output.Data.(map[string]any)["result"])
	assert.Equal(t, "localhost", result.ResolvedInputs["input1"])
}

func TestExecutor_Execute_WithCELExpression(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", func(_ context.Context, inputs map[string]any) (*Output, error) {
		return &Output{Data: map[string]any{"result": inputs["input1"]}}, nil
	})

	// Set up resolver context
	resolverCtx := map[string]any{
		"value1": 10,
		"value2": 20,
	}

	ctx := WithResolverContext(context.Background(), resolverCtx)

	// Use CEL expression
	inputs := map[string]any{
		"input1": InputValue{
			Expr: "string(value1 + value2)",
		},
	}

	result, err := executor.Execute(ctx, provider, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "30", result.Output.Data.(map[string]any)["result"])
}

func TestExecutor_Execute_WithTemplate(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", func(_ context.Context, inputs map[string]any) (*Output, error) {
		return &Output{Data: map[string]any{"result": inputs["input1"]}}, nil
	})

	// Set up resolver context
	resolverCtx := map[string]any{
		"user": "john",
		"host": "localhost",
	}

	ctx := WithResolverContext(context.Background(), resolverCtx)

	// Use template
	inputs := map[string]any{
		"input1": InputValue{
			Tmpl: "{{.user}}@{{.host}}",
		},
	}

	result, err := executor.Execute(ctx, provider, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "john@localhost", result.Output.Data.(map[string]any)["result"])
}

func TestExecutor_ExecuteByName_Success(t *testing.T) {
	// Clean up global registry
	ResetGlobalRegistry()
	defer ResetGlobalRegistry()

	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	// Register provider
	err := Register(provider)
	require.NoError(t, err)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := executor.ExecuteByName(ctx, "test-provider", inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "success", result.Output.Data.(map[string]any)["result"])
}

func TestExecutor_ExecuteByName_ProviderNotFound(t *testing.T) {
	// Clean up global registry
	ResetGlobalRegistry()
	defer ResetGlobalRegistry()

	executor := NewExecutor()

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := executor.ExecuteByName(ctx, "non-existent-provider", inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not found")
}

func TestExecutor_MustExecuteByName_Success(t *testing.T) {
	// Clean up global registry
	ResetGlobalRegistry()
	defer ResetGlobalRegistry()

	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	// Register provider
	err := Register(provider)
	require.NoError(t, err)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	result := executor.MustExecuteByName(ctx, "test-provider", inputs)

	require.NotNil(t, result)
	assert.Equal(t, "success", result.Output.Data.(map[string]any)["result"])
}

func TestExecutor_MustExecuteByName_Panic(t *testing.T) {
	// Clean up global registry
	ResetGlobalRegistry()
	defer ResetGlobalRegistry()

	executor := NewExecutor()

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	assert.Panics(t, func() {
		executor.MustExecuteByName(ctx, "non-existent-provider", inputs)
	})
}

func TestGlobalExecutor_Execute(t *testing.T) {
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := Execute(ctx, provider, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "success", result.Output.Data.(map[string]any)["result"])
}

func TestGlobalExecutor_ExecuteByName(t *testing.T) {
	// Clean up global registry
	ResetGlobalRegistry()
	defer ResetGlobalRegistry()

	provider := newMockExecutableProvider("test-provider", nil)

	// Register provider
	err := Register(provider)
	require.NoError(t, err)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := ExecuteByName(ctx, "test-provider", inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "success", result.Output.Data.(map[string]any)["result"])
}

func TestGlobalExecutor_MustExecuteByName(t *testing.T) {
	// Clean up global registry
	ResetGlobalRegistry()
	defer ResetGlobalRegistry()

	provider := newMockExecutableProvider("test-provider", nil)

	// Register provider
	err := Register(provider)
	require.NoError(t, err)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	result := MustExecuteByName(ctx, "test-provider", inputs)

	require.NotNil(t, result)
	assert.Equal(t, "success", result.Output.Data.(map[string]any)["result"])
}

func TestGetGlobalExecutor(t *testing.T) {
	executor := GetGlobalExecutor()
	assert.NotNil(t, executor)
	assert.NotNil(t, executor.schemaValidator)
}

func TestResetGlobalExecutor(t *testing.T) {
	oldExecutor := GetGlobalExecutor()
	ResetGlobalExecutor()
	newExecutor := GetGlobalExecutor()

	assert.NotNil(t, newExecutor)
	assert.NotEqual(t, oldExecutor, newExecutor)
}

func TestExecutionResult_Structure(t *testing.T) {
	version, _ := semver.NewVersion("1.0.0")
	provider := &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:    "test",
			Version: version,
		},
	}

	result := &ExecutionResult{
		Provider: provider,
		Output: Output{
			Data: map[string]any{"key": "value"},
		},
		DryRun: true,
		ResolvedInputs: map[string]any{
			"input1": "resolved-value",
		},
	}

	assert.Equal(t, provider, result.Provider)
	assert.Equal(t, "value", result.Output.Data.(map[string]any)["key"])
	assert.True(t, result.DryRun)
	assert.Equal(t, "resolved-value", result.ResolvedInputs["input1"])
}
