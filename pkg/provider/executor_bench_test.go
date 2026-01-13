package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
)

func BenchmarkExecutor_Execute_Literal(b *testing.B) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
		"input2": 42,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Execute(ctx, provider, inputs)
	}
}

func BenchmarkExecutor_Execute_DryRun(b *testing.B) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", func(ctx context.Context, _ map[string]any) (*Output, error) {
		if DryRunFromContext(ctx) {
			return &Output{
				Data: map[string]any{
					"_dryRun":  true,
					"_message": "Dry-run execution",
				},
			}, nil
		}
		return &Output{Data: map[string]any{"result": "success"}}, nil
	})

	ctx := WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"input1": "test-value",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Execute(ctx, provider, inputs)
	}
}

func BenchmarkExecutor_Execute_Rslvr(b *testing.B) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	resolverCtx := map[string]any{
		"config": map[string]any{
			"database": map[string]any{
				"host": "localhost",
			},
		},
	}

	ctx := WithResolverContext(context.Background(), resolverCtx)
	inputs := map[string]any{
		"input1": InputValue{
			Rslvr: "config.database.host",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Execute(ctx, provider, inputs)
	}
}

func BenchmarkExecutor_Execute_CEL(b *testing.B) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	resolverCtx := map[string]any{
		"value1": 10,
		"value2": 20,
	}

	ctx := WithResolverContext(context.Background(), resolverCtx)
	inputs := map[string]any{
		"input1": InputValue{
			Expr: "string(value1 + value2)",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Execute(ctx, provider, inputs)
	}
}

func BenchmarkExecutor_Execute_Template(b *testing.B) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	resolverCtx := map[string]any{
		"user": "john",
		"host": "localhost",
	}

	ctx := WithResolverContext(context.Background(), resolverCtx)
	inputs := map[string]any{
		"input1": InputValue{
			Tmpl: "{{.user}}@{{.host}}",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Execute(ctx, provider, inputs)
	}
}

func BenchmarkExecutor_Execute_MixedInputs(b *testing.B) {
	executor := NewExecutor()

	version, _ := semver.NewVersion("1.0.0")
	provider := &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         "test-provider",
			Version:      version,
			Capabilities: []Capability{CapabilityFrom},
			Schema: SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"literal":  {Type: PropertyTypeString, Required: true},
					"rslvr":    {Type: PropertyTypeString, Required: true},
					"expr":     {Type: PropertyTypeInt, Required: true},
					"template": {Type: PropertyTypeString, Required: true},
				},
			},
			OutputSchema: SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"result": {Type: PropertyTypeString},
				},
			},
		},
		executeFunc: func(_ context.Context, _ map[string]any) (*Output, error) {
			return &Output{Data: map[string]any{"result": "success"}}, nil
		},
	}

	resolverCtx := map[string]any{
		"config": map[string]any{
			"host": "localhost",
		},
		"port":     8080,
		"username": "admin",
	}

	ctx := WithResolverContext(context.Background(), resolverCtx)
	inputs := map[string]any{
		"literal": "static-value",
		"rslvr": InputValue{
			Rslvr: "config.host",
		},
		"expr": InputValue{
			Expr: "port + 10",
		},
		"template": InputValue{
			Tmpl: "{{.username}}@{{.config.host}}",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Execute(ctx, provider, inputs)
	}
}

func BenchmarkExecutor_ExecuteByName(b *testing.B) {
	// Clean up global registry
	ResetGlobalRegistry()
	defer ResetGlobalRegistry()

	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	// Register provider
	_ = Register(provider)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.ExecuteByName(ctx, "test-provider", inputs)
	}
}

func BenchmarkGlobalExecutor_Execute(b *testing.B) {
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Execute(ctx, provider, inputs)
	}
}

func BenchmarkGlobalExecutor_ExecuteByName(b *testing.B) {
	// Clean up global registry
	ResetGlobalRegistry()
	defer ResetGlobalRegistry()

	provider := newMockExecutableProvider("test-provider", nil)

	// Register provider
	_ = Register(provider)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExecuteByName(ctx, "test-provider", inputs)
	}
}

func BenchmarkExecutor_Execute_ComplexSchema(b *testing.B) {
	executor := NewExecutor()

	version, _ := semver.NewVersion("1.0.0")
	provider := &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         "complex-provider",
			Version:      version,
			Capabilities: []Capability{CapabilityFrom, CapabilityTransform},
			Schema: SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"string1":  {Type: PropertyTypeString, Required: true},
					"string2":  {Type: PropertyTypeString, Required: true},
					"int1":     {Type: PropertyTypeInt, Required: true},
					"int2":     {Type: PropertyTypeInt, Required: false},
					"bool1":    {Type: PropertyTypeBool, Required: true},
					"bool2":    {Type: PropertyTypeBool, Required: false},
					"float1":   {Type: PropertyTypeFloat, Required: true},
					"array1":   {Type: PropertyTypeArray, Required: false},
					"optional": {Type: PropertyTypeString, Required: false},
				},
			},
			OutputSchema: SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"result":  {Type: PropertyTypeString},
					"count":   {Type: PropertyTypeInt},
					"success": {Type: PropertyTypeBool},
				},
			},
		},
		executeFunc: func(_ context.Context, _ map[string]any) (*Output, error) {
			return &Output{Data: map[string]any{
				"result":  "success",
				"count":   42,
				"success": true,
			}}, nil
		},
	}

	ctx := context.Background()
	inputs := map[string]any{
		"string1": "value1",
		"string2": "value2",
		"int1":    100,
		"int2":    200,
		"bool1":   true,
		"bool2":   false,
		"float1":  3.14,
		"array1":  []any{"a", "b", "c"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Execute(ctx, provider, inputs)
	}
}

func BenchmarkExecutor_Execute_ValidationError(b *testing.B) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := context.Background()
	// Wrong type for input2
	inputs := map[string]any{
		"input1": "test-value",
		"input2": "not-an-int",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Execute(ctx, provider, inputs)
	}
}

func BenchmarkExecutor_NewExecutor(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewExecutor()
	}
}

func BenchmarkExecutor_NewExecutor_WithOptions(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewExecutor(
			WithSchemaValidator(NewSchemaValidator()),
		)
	}
}

// Helper function for benchmarks
func BenchmarkProviderExecution(b *testing.B) {
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = provider.Execute(ctx, inputs)
	}
}

func BenchmarkExecutionResult_Creation(b *testing.B) {
	version, _ := semver.NewVersion("1.0.0")
	provider := &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:    "test",
			Version: version,
		},
	}

	output := Output{
		Data: map[string]any{"key": "value"},
	}

	resolvedInputs := map[string]any{
		"input1": "resolved-value",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &ExecutionResult{
			Provider:       provider,
			Output:         output,
			DryRun:         false,
			ResolvedInputs: resolvedInputs,
		}
	}
}

func BenchmarkExecutor_Execute_Parallel(b *testing.B) {
	executor := NewExecutor()
	provider := newMockExecutableProvider("test-provider", nil)

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = executor.Execute(ctx, provider, inputs)
		}
	})
}

func BenchmarkExecutor_ExecuteByName_Parallel(b *testing.B) {
	// Clean up global registry
	ResetGlobalRegistry()
	defer ResetGlobalRegistry()

	executor := NewExecutor()
	providers := make([]*mockExecutableProvider, 10)
	for i := 0; i < 10; i++ {
		providers[i] = newMockExecutableProvider(fmt.Sprintf("provider-%d", i), nil)
		_ = Register(providers[i])
	}

	ctx := context.Background()
	inputs := map[string]any{
		"input1": "test-value",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			providerName := fmt.Sprintf("provider-%d", i%10)
			_, _ = executor.ExecuteByName(ctx, providerName, inputs)
			i++
		}
	})
}
