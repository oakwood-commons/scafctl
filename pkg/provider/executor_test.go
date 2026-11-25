// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockExecutableProvider is a provider that can be executed for testing.
type mockExecutableProvider struct {
	descriptor    *Descriptor
	executeFunc   func(ctx context.Context, input any) (*Output, error)
	executeCalled bool
}

func (m *mockExecutableProvider) Descriptor() *Descriptor {
	return m.descriptor
}

func (m *mockExecutableProvider) Execute(ctx context.Context, input any) (*Output, error) {
	m.executeCalled = true
	if m.executeFunc != nil {
		return m.executeFunc(ctx, input)
	}
	return &Output{Data: map[string]any{"result": "success"}}, nil
}

// newMockExecutableProvider creates a mock executable provider for testing.
func newMockExecutableProvider(name string, executeFunc func(ctx context.Context, input any) (*Output, error)) *mockExecutableProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         name,
			APIVersion:   "v1",
			Version:      version,
			Description:  "Mock provider for testing",
			Capabilities: []Capability{CapabilityFrom},
			Schema: schemahelper.ObjectSchema([]string{"input1"}, map[string]*jsonschema.Schema{
				"input1": schemahelper.StringProp(""),
				"input2": schemahelper.IntProp(""),
			}),
			OutputSchemas: map[Capability]*jsonschema.Schema{
				CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.StringProp(""),
				}),
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

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
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
	provider := newMockExecutableProvider("test-provider", func(ctx context.Context, _ any) (*Output, error) {
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

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	ctx = WithDryRun(ctx, true)
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

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
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

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	inputs := map[string]any{"input1": "test"}

	result, err := executor.Execute(ctx, provider, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "descriptor cannot be nil")
}

func TestExecutor_Execute_InputResolutionError(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
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

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
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
	provider := newMockExecutableProvider("test-provider", func(_ context.Context, _ any) (*Output, error) {
		return nil, fmt.Errorf("provider execution failed")
	})

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := executor.Execute(ctx, provider, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "provider execution failed")
}

// TestExecutor_Execute_OutputValidation tests that output validation would need
// to be handled at a higher level with the new per-capability OutputSchemas design.
// The executor no longer performs output validation directly since it doesn't know
// which capability context is being used.
func TestExecutor_Execute_OutputValidation_SkippedAtExecutorLevel(t *testing.T) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", func(_ context.Context, _ any) (*Output, error) {
		// Return output with mismatched type - executor accepts it since
		// per-capability output validation is done at resolver/higher level
		return &Output{Data: map[string]any{"result": 123}}, nil
	})

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := executor.Execute(ctx, provider, inputs)

	// No error - output validation is deferred to higher level with per-capability schemas
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 123, result.Output.Data.(map[string]any)["result"])
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
			Schema: schemahelper.ObjectSchema([]string{"input1"}, map[string]*jsonschema.Schema{
				"input1": schemahelper.StringProp(""),
			}),
			OutputSchemas: map[Capability]*jsonschema.Schema{
				CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.StringProp(""),
				}),
			},
		},
		executeFunc: func(_ context.Context, input any) (*Output, error) {
			inputs := input.(map[string]any)
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

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	ctx = WithResolverContext(ctx, resolverCtx)

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
	provider := newMockExecutableProvider("test-provider", func(_ context.Context, input any) (*Output, error) {
		inputs := input.(map[string]any)
		return &Output{Data: map[string]any{"result": inputs["input1"]}}, nil
	})

	// Set up resolver context
	resolverCtx := map[string]any{
		"value1": 10,
		"value2": 20,
	}

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	ctx = WithResolverContext(ctx, resolverCtx)

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
	provider := newMockExecutableProvider("test-provider", func(_ context.Context, input any) (*Output, error) {
		inputs := input.(map[string]any)
		return &Output{Data: map[string]any{"result": inputs["input1"]}}, nil
	})

	// Set up resolver context
	resolverCtx := map[string]any{
		"user": "john",
		"host": "localhost",
	}

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	ctx = WithResolverContext(ctx, resolverCtx)

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

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
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

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := executor.ExecuteByName(ctx, "non-existent-provider", inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not found")
}

func TestGlobalExecutor_Execute(t *testing.T) {
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
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

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	inputs := map[string]any{
		"input1": "test-value",
	}

	result, err := ExecuteByName(ctx, "test-provider", inputs)

	require.NoError(t, err)
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
	// Compare pointers to verify a new instance was created
	assert.True(t, oldExecutor != newExecutor, "ResetGlobalExecutor should create a new executor instance")
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

// TestExecutor_Execute_WithDecode tests that the Executor correctly calls
// the Decode function and passes the decoded typed input to Execute.
func TestExecutor_Execute_WithDecode(t *testing.T) {
	// Define a typed input struct for the provider
	type testInput struct {
		Name  string
		Count int
	}

	executor := NewExecutor()
	version, _ := semver.NewVersion("1.0.0")

	var receivedInput any

	provider := &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         "test-provider-with-decode",
			APIVersion:   "v1",
			Version:      version,
			Description:  "Test provider with decode function",
			Capabilities: []Capability{CapabilityFrom},
			Schema: schemahelper.ObjectSchema([]string{"name", "count"}, map[string]*jsonschema.Schema{
				"name":  schemahelper.StringProp(""),
				"count": schemahelper.IntProp(""),
			}),
			OutputSchemas: map[Capability]*jsonschema.Schema{
				CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.StringProp(""),
				}),
			},
			// Define Decode function to convert map to typed struct
			Decode: func(inputs map[string]any) (any, error) {
				name, _ := inputs["name"].(string)
				count := 0
				if c, ok := inputs["count"].(int); ok {
					count = c
				}
				return testInput{Name: name, Count: count}, nil
			},
		},
		executeFunc: func(_ context.Context, input any) (*Output, error) {
			receivedInput = input
			// Verify we received typed input, not map
			typed, ok := input.(testInput)
			if !ok {
				return nil, fmt.Errorf("expected testInput, got %T", input)
			}
			return &Output{Data: map[string]any{"result": fmt.Sprintf("%s-%d", typed.Name, typed.Count)}}, nil
		},
	}

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	inputs := map[string]any{
		"name":  "test",
		"count": 42,
	}

	result, err := executor.Execute(ctx, provider, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the typed input was received
	typed, ok := receivedInput.(testInput)
	require.True(t, ok, "expected typed input, got %T", receivedInput)
	assert.Equal(t, "test", typed.Name)
	assert.Equal(t, 42, typed.Count)

	// Verify the output
	assert.Equal(t, "test-42", result.Output.Data.(map[string]any)["result"])
}

// TestExecutor_Execute_WithDecodeError tests that decode errors are properly propagated.
func TestExecutor_Execute_WithDecodeError(t *testing.T) {
	executor := NewExecutor()
	version, _ := semver.NewVersion("1.0.0")

	provider := &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         "test-provider-decode-error",
			APIVersion:   "v1",
			Version:      version,
			Description:  "Test provider with failing decode",
			Capabilities: []Capability{CapabilityFrom},
			Schema: schemahelper.ObjectSchema([]string{"name"}, map[string]*jsonschema.Schema{
				"name": schemahelper.StringProp(""),
			}),
			// Decode function that always fails
			Decode: func(_ map[string]any) (any, error) {
				return nil, fmt.Errorf("decode error: invalid input format")
			},
		},
		executeFunc: func(_ context.Context, _ any) (*Output, error) {
			return &Output{Data: map[string]any{}}, nil
		},
	}

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	inputs := map[string]any{
		"name": "test",
	}

	result, err := executor.Execute(ctx, provider, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to decode inputs")
	assert.Contains(t, err.Error(), "invalid input format")
}

// TestExecutor_Execute_WithoutDecode tests that providers without Decode receive map[string]any.
func TestExecutor_Execute_WithoutDecode(t *testing.T) {
	executor := NewExecutor()
	version, _ := semver.NewVersion("1.0.0")

	var receivedInput any

	provider := &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         "test-provider-no-decode",
			APIVersion:   "v1",
			Version:      version,
			Description:  "Test provider without decode function",
			Capabilities: []Capability{CapabilityFrom},
			Schema: schemahelper.ObjectSchema([]string{"value"}, map[string]*jsonschema.Schema{
				"value": schemahelper.StringProp(""),
			}),
			OutputSchemas: map[Capability]*jsonschema.Schema{
				CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.StringProp(""),
				}),
			},
			// No Decode function - should receive map[string]any
		},
		executeFunc: func(_ context.Context, input any) (*Output, error) {
			receivedInput = input
			// Verify we received map, not typed struct
			inputs, ok := input.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("expected map[string]any, got %T", input)
			}
			return &Output{Data: map[string]any{"result": inputs["value"]}}, nil
		},
	}

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	inputs := map[string]any{
		"value": "hello",
	}

	result, err := executor.Execute(ctx, provider, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the map input was received
	receivedMap, ok := receivedInput.(map[string]any)
	require.True(t, ok, "expected map[string]any, got %T", receivedInput)
	assert.Equal(t, "hello", receivedMap["value"])

	// Verify the output
	assert.Equal(t, "hello", result.Output.Data.(map[string]any)["result"])
}

// TestExecutor_Execute_ContextCancellation tests that execution respects context cancellation.
func TestExecutor_Execute_ContextCancellation(t *testing.T) {
	executor := NewExecutor()
	version, _ := semver.NewVersion("1.0.0")

	// Create a provider that blocks until context is cancelled
	executionStarted := make(chan struct{})

	provider := &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         "slow-provider",
			APIVersion:   "v1",
			Version:      version,
			Description:  "Test provider that blocks",
			Capabilities: []Capability{CapabilityFrom},
			Schema: schemahelper.ObjectSchema([]string{"input"}, map[string]*jsonschema.Schema{
				"input": schemahelper.StringProp(""),
			}),
		},
		executeFunc: func(ctx context.Context, _ any) (*Output, error) {
			close(executionStarted)
			// Wait for context cancellation
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = WithExecutionMode(ctx, CapabilityFrom)

	inputs := map[string]any{
		"input": "test",
	}

	// Run execution in goroutine
	errCh := make(chan error, 1)
	go func() {
		_, err := executor.Execute(ctx, provider, inputs)
		errCh <- err
	}()

	// Wait for execution to start, then cancel
	<-executionStarted
	cancel()

	// Wait for execution to complete
	err := <-errCh
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// ─── Write Operation Classification Tests ────────────────────────────────────

// newMockWriteClassifierProvider creates a mock with WriteOperations on the Descriptor.
func newMockWriteClassifierProvider(writeOps []string) *mockExecutableProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:        "test-provider",
			APIVersion:  "v1",
			Version:     version,
			Description: "Mock provider for testing",
			Capabilities: []Capability{
				CapabilityFrom,
				CapabilityAction,
				CapabilityTransform,
				CapabilityValidation,
				CapabilityState,
				CapabilityKubeconfig,
			},
			WriteOperations: writeOps,
			Schema: schemahelper.ObjectSchema([]string{"operation"}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform"),
			}),
			OutputSchemas: map[Capability]*jsonschema.Schema{
				CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.StringProp(""),
				}),
				CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success": schemahelper.BoolProp(""),
				}),
			},
		},
	}
}

func TestValidateWriteOperation_BlocksWriteInResolver(t *testing.T) {
	t.Parallel()

	p := newMockWriteClassifierProvider([]string{"create_label", "delete_label"})

	executor := NewExecutor()
	ctx := WithExecutionMode(context.Background(), CapabilityFrom)

	_, err := executor.Execute(ctx, p, map[string]any{
		"operation": "create_label",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write operation")
	assert.Contains(t, err.Error(), "create_label")
	assert.Contains(t, err.Error(), "workflow action")
}

func TestValidateWriteOperation_AllowsReadInResolver(t *testing.T) {
	t.Parallel()

	p := newMockWriteClassifierProvider([]string{"create_label", "delete_label"})

	executor := NewExecutor()
	ctx := WithExecutionMode(context.Background(), CapabilityFrom)

	result, err := executor.Execute(ctx, p, map[string]any{
		"operation": "list_labels",
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestValidateWriteOperation_AllowsWriteInAction(t *testing.T) {
	t.Parallel()

	p := newMockWriteClassifierProvider([]string{"create_label"})

	executor := NewExecutor()
	ctx := WithExecutionMode(context.Background(), CapabilityAction)

	result, err := executor.Execute(ctx, p, map[string]any{
		"operation": "create_label",
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestValidateWriteOperation_BlocksWriteInTransform(t *testing.T) {
	t.Parallel()

	p := newMockWriteClassifierProvider([]string{"create_label", "delete_label"})

	executor := NewExecutor()
	ctx := WithExecutionMode(context.Background(), CapabilityTransform)

	_, err := executor.Execute(ctx, p, map[string]any{
		"operation": "create_label",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write operation")
	assert.Contains(t, err.Error(), "create_label")
	assert.Contains(t, err.Error(), "workflow action")
}

func TestValidateWriteOperation_SkipsNonClassifier(t *testing.T) {
	t.Parallel()

	// Provider without WriteOperations (nil) — no enforcement
	p := newMockExecutableProvider("test-provider", nil)

	executor := NewExecutor()
	ctx := WithExecutionMode(context.Background(), CapabilityFrom)

	result, err := executor.Execute(ctx, p, map[string]any{
		"input1":    "value",
		"operation": "create_label",
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestValidateWriteOperation_BlocksWriteInValidation(t *testing.T) {
	t.Parallel()

	p := newMockWriteClassifierProvider([]string{"create_label", "delete_label"})

	executor := NewExecutor()
	ctx := WithExecutionMode(context.Background(), CapabilityValidation)

	_, err := executor.Execute(ctx, p, map[string]any{
		"operation": "create_label",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write operation")
	assert.Contains(t, err.Error(), "create_label")
	assert.Contains(t, err.Error(), "workflow action")
}

func TestValidateWriteOperation_AllowsWriteInKubeconfig(t *testing.T) {
	t.Parallel()

	// Regression test for #579: kubeconfig writes run under CapabilityKubeconfig
	// and must not be rejected by the read-only resolver write guard.
	p := newMockWriteClassifierProvider([]string{"kubeconfig_write", "kubeconfig_remove"})

	executor := NewExecutor()
	ctx := WithExecutionMode(context.Background(), CapabilityKubeconfig)

	result, err := executor.Execute(ctx, p, map[string]any{
		"operation": "kubeconfig_write",
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestValidateWriteOperation_AllowsWriteInState(t *testing.T) {
	t.Parallel()

	// Regression test for #579: state backends run save/delete under
	// CapabilityState and must not be rejected by the write guard.
	p := newMockWriteClassifierProvider([]string{"state_save", "state_delete"})

	executor := NewExecutor()
	ctx := WithExecutionMode(context.Background(), CapabilityState)

	result, err := executor.Execute(ctx, p, map[string]any{
		"operation": "state_save",
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
}

// newMockOperationsProvider creates a provider that classifies operations exclusively
// via Descriptor.Operations (leaving legacy WriteOperations nil).
func newMockOperationsProvider(ops []OperationDescriptor) *mockExecutableProvider {
	version := semver.MustParse("1.0.0")
	return &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:        "ops-provider",
			APIVersion:  "v1",
			Version:     version,
			Description: "Mock provider with OperationDescriptor entries",
			Capabilities: []Capability{
				CapabilityFrom,
				CapabilityTransform,
				CapabilityValidation,
				CapabilityAction,
			},
			Operations: ops,
			Schema: schemahelper.ObjectSchema([]string{"operation"}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform"),
			}),
			OutputSchemas: map[Capability]*jsonschema.Schema{
				CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.StringProp(""),
				}),
				CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.StringProp(""),
				}),
				CapabilityValidation: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"valid":  schemahelper.BoolProp(""),
					"errors": schemahelper.ArrayProp(""),
				}),
				CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success": schemahelper.BoolProp(""),
				}),
			},
		},
	}
}

func TestValidateWriteOperation_OperationsDescriptor(t *testing.T) {
	t.Parallel()

	richOps := []OperationDescriptor{
		{
			Name:         "get_repo",
			Description:  "Get repository metadata and settings",
			Capabilities: []Capability{CapabilityFrom},
			IsWrite:      false,
		},
		{
			Name:         "create_issue",
			Description:  "  Create a new issue in the repository  ",
			Capabilities: []Capability{CapabilityAction},
			IsWrite:      true,
		},
		{
			Name:         "delete_issue",
			Description:  "   ",
			Capabilities: []Capability{CapabilityAction},
			IsWrite:      true,
		},
	}

	tests := []struct {
		name        string
		ops         []OperationDescriptor
		mode        Capability
		operation   string
		wantErr     bool
		wantErrText string
	}{
		{
			name:        "blocks_write_in_from_with_trimmed_description",
			ops:         richOps,
			mode:        CapabilityFrom,
			operation:   "create_issue",
			wantErr:     true,
			wantErrText: `operation "create_issue" (Create a new issue in the repository) is a write operation and cannot be used in read-only execution mode "from"; use a workflow action instead`,
		},
		{
			name:        "blocks_write_in_transform_with_description",
			ops:         richOps,
			mode:        CapabilityTransform,
			operation:   "create_issue",
			wantErr:     true,
			wantErrText: `operation "create_issue" (Create a new issue in the repository) is a write operation and cannot be used in read-only execution mode "transform"; use a workflow action instead`,
		},
		{
			name:        "blocks_write_in_validation_with_description",
			ops:         richOps,
			mode:        CapabilityValidation,
			operation:   "create_issue",
			wantErr:     true,
			wantErrText: `operation "create_issue" (Create a new issue in the repository) is a write operation and cannot be used in read-only execution mode "validation"; use a workflow action instead`,
		},
		{
			name:        "blocks_write_without_empty_parentheses_when_description_blank",
			ops:         richOps,
			mode:        CapabilityFrom,
			operation:   "delete_issue",
			wantErr:     true,
			wantErrText: `operation "delete_issue" is a write operation and cannot be used in read-only execution mode "from"; use a workflow action instead`,
		},
		{
			name:      "allows_read_operation_in_from",
			ops:       richOps,
			mode:      CapabilityFrom,
			operation: "get_repo",
			wantErr:   false,
		},
		{
			name:      "allows_write_operation_in_action",
			ops:       richOps,
			mode:      CapabilityAction,
			operation: "create_issue",
			wantErr:   false,
		},
		{
			name: "allows_all_operations_when_operations_are_all_reads",
			ops: []OperationDescriptor{
				{Name: "get_repo", IsWrite: false},
				{Name: "list_branches", IsWrite: false},
			},
			mode:      CapabilityFrom,
			operation: "get_repo",
			wantErr:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newMockOperationsProvider(tc.ops)
			require.Nil(t, p.Descriptor().WriteOperations)

			executor := NewExecutor()
			ctx := WithExecutionMode(context.Background(), tc.mode)
			res, err := executor.Execute(ctx, p, map[string]any{"operation": tc.operation})
			if tc.wantErr {
				require.Error(t, err)
				assert.Equal(t, tc.wantErrText, err.Error())
				assert.False(t, p.executeCalled, "blocked write operation must not invoke Execute")
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)
				assert.True(t, p.executeCalled)
			}
		})
	}
}

func TestValidateWriteOperation_GuardsAndZeroAllocFastPaths(t *testing.T) {
	ctxFrom := WithExecutionMode(context.Background(), CapabilityFrom)
	ctxAction := WithExecutionMode(context.Background(), CapabilityAction)

	// Nil provider and nil descriptor guards.
	require.NoError(t, ValidateWriteOperation(ctxFrom, nil, map[string]any{"operation": "write"}))
	require.NoError(t, ValidateWriteOperation(ctxFrom, &mockExecutableProvider{descriptor: nil}, map[string]any{"operation": "write"}))

	// Missing execution mode in context.
	p := newMockOperationsProvider([]OperationDescriptor{{Name: "create_issue", IsWrite: true}})
	require.NoError(t, ValidateWriteOperation(context.Background(), p, map[string]any{"operation": "create_issue"}))

	// Empty or missing operation input.
	require.NoError(t, ValidateWriteOperation(ctxFrom, p, nil))
	require.NoError(t, ValidateWriteOperation(ctxFrom, p, map[string]any{}))
	require.NoError(t, ValidateWriteOperation(ctxFrom, p, map[string]any{"operation": ""}))

	// Action mode and non-operation providers must not allocate EffectiveWriteOperations slices.
	actionInputs := map[string]any{"operation": "create_issue"}
	allocsAction := testing.AllocsPerRun(100, func() {
		_ = ValidateWriteOperation(ctxAction, p, actionInputs)
	})
	assert.Zero(t, allocsAction, "expected zero allocations in action mode")

	nonOpInputs := map[string]any{"url": "https://example.com"}
	allocsNonOp := testing.AllocsPerRun(100, func() {
		_ = ValidateWriteOperation(ctxFrom, p, nonOpInputs)
	})
	assert.Zero(t, allocsNonOp, "expected zero allocations when operation input is absent")

	// Control characters, ANSI CSI/OSC sequences, and newlines in plugin descriptions
	// are stripped/collapsed, and tracked secrets (including whitespace-bearing and
	// ANSI-split credentials) are redacted before and after normalization.
	tracker := redact.NewTracker()
	tracker.Track("tok_secret_value_123")
	tracker.Track("cred\twith   spaces")
	tracker.Track("ansi_split_secret")
	ctxRedacted := redact.WithTracker(ctxFrom, tracker)
	untrustedProv := newMockOperationsProvider([]OperationDescriptor{
		{
			Name:        "write_tok_secret_value_123",
			Description: "\x1b[31mMutate\x1b[0m\x1b]0;title\x07\r\n::add-mask::injected\tstate using cred\twith   spaces and ansi_\x1b[0msplit_secret",
			IsWrite:     true,
		},
		{
			Name:        "op_cred\twith   spaces",
			Description: "Tab secret in operation name",
			IsWrite:     true,
		},
	})
	err := ValidateWriteOperation(ctxRedacted, untrustedProv, map[string]any{"operation": "write_tok_secret_value_123"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "\x1b")
	assert.NotContains(t, err.Error(), "\n")
	assert.NotContains(t, err.Error(), "tok_secret_value_123")
	assert.NotContains(t, err.Error(), "cred\twith   spaces")
	assert.NotContains(t, err.Error(), "cred with spaces")
	assert.NotContains(t, err.Error(), "ansi_split_secret")
	assert.Contains(t, err.Error(), "(Mutate ::add-mask::injected state using [REDACTED] and [REDACTED])")

	errTabOp := ValidateWriteOperation(ctxRedacted, untrustedProv, map[string]any{"operation": "op_cred\twith   spaces"})
	require.Error(t, errTabOp)
	assert.NotContains(t, errTabOp.Error(), "cred")
	assert.Contains(t, errTabOp.Error(), `operation "op_[REDACTED]"`)
}

func TestExecutor_Execute_PreservesNonNilOutputOnError(t *testing.T) {
	t.Parallel()

	executor := NewExecutor()
	p := newMockExecutableProvider("exec", func(_ context.Context, _ any) (*Output, error) {
		return &Output{
			Data: map[string]any{
				"stdout":   "--- FAIL: TestWidget secret-tok\n",
				"exitCode": 1,
			},
			Warnings: []string{"warn secret-tok"},
			Metadata: map[string]any{"key": "secret-tok"},
		}, fmt.Errorf("exec: command exited with code 1")
	})

	tracker := redact.NewTracker()
	tracker.Track("secret-tok")
	ctx := redact.WithTracker(WithExecutionMode(context.Background(), CapabilityFrom), tracker)

	result, err := executor.Execute(ctx, p, map[string]any{"input1": "secret-tok"})
	require.Error(t, err)
	assert.Equal(t, "provider execution failed: exec: command exited with code 1", err.Error())
	require.NotNil(t, result, "non-nil provider output must be preserved on error")
	assert.Equal(t, "--- FAIL: TestWidget [REDACTED]\n", result.Output.Data.(map[string]any)["stdout"])
	assert.Equal(t, []string{"warn [REDACTED]"}, result.Output.Warnings)
	assert.Equal(t, "[REDACTED]", result.Output.Metadata["key"])
	assert.Equal(t, "[REDACTED]", result.ResolvedInputs["input1"])
}

func TestCapturedStreamSink_AndMergeIntoData(t *testing.T) {
	t.Parallel()

	var nilSink *CapturedStreamSink
	nilSink.Set("ignored", "ignored")
	assert.True(t, nilSink.Logs().Empty())

	_, ok := CapturedStreamSinkFromContext(context.Background())
	assert.False(t, ok)

	sink := &CapturedStreamSink{}
	ctx := WithCapturedStreamSink(context.Background(), sink)
	gotSink, ok := CapturedStreamSinkFromContext(ctx)
	require.True(t, ok)
	assert.Same(t, sink, gotSink)

	sink.Set("out\n", "err\n")
	logs := sink.Logs()
	assert.False(t, logs.Empty())
	assert.Equal(t, map[string]any{"stdout": "out\n", "stderr": "err\n"}, logs.MergeIntoData(nil))

	orig := map[string]any{"id": 1}
	merged := logs.MergeIntoData(orig).(map[string]any)
	assert.Equal(t, map[string]any{"id": 1}, orig, "MergeIntoData must not mutate caller map")
	assert.Equal(t, map[string]any{"id": 1, "stdout": "out\n", "stderr": "err\n"}, merged)

	existing := map[string]any{"stdout": []string{"structured"}, "stderr": "kept\n"}
	assert.Equal(t, existing, logs.MergeIntoData(existing))
	assert.Equal(t, "scalar", logs.MergeIntoData("scalar"))
}
