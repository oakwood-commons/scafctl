// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCelProvider(t *testing.T) {
	p := NewCelProvider()
	require.NotNil(t, p)

	desc := p.Descriptor()
	assert.Equal(t, "cel", desc.Name)
	assert.Equal(t, "1.0.0", desc.Version.String())
	assert.Equal(t, "data", desc.Category)
	assert.Contains(t, desc.Capabilities, provider.CapabilityTransform)
	assert.NotNil(t, desc.Schema)
	assert.NotEmpty(t, desc.Schema.Properties)
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityTransform])
	assert.NotEmpty(t, desc.OutputSchemas[provider.CapabilityTransform].Properties)
}

func TestCelProvider_Execute_StringTransform(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "hello world",
	})

	inputs := map[string]any{
		"expression": "_.input.upperAscii()",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "HELLO WORLD", output.Data)
}

func TestCelProvider_Execute_Concatenation(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "original",
	})

	inputs := map[string]any{
		"expression": "_.input + ' - transformed'",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "original - transformed", output.Data)
}

func TestCelProvider_Execute_NumberTransform(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": 5,
	})

	inputs := map[string]any{
		"expression": "_.input * 2 + 10",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, int64(20), output.Data)
}

func TestCelProvider_Execute_BooleanLogic(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": 50,
	})

	inputs := map[string]any{
		"expression": "_.input > 10 && _.input < 100",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, true, output.Data)
}

func TestCelProvider_Execute_MapAccess(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": map[string]any{
			"name": "Alice",
			"age":  30,
		},
	})

	inputs := map[string]any{
		"expression": "_.input.name + ' is ' + string(_.input.age) + ' years old'",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "Alice is 30 years old", output.Data)
}

func TestCelProvider_Execute_ArrayTransform(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": []any{1, 2, 3, 4, 5},
	})

	inputs := map[string]any{
		"expression": "_.input.map(x, x * 2)",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// CEL returns arrays that need to be asserted carefully
	assert.ElementsMatch(t, []any{int64(2), int64(4), int64(6), int64(8), int64(10)}, output.Data)
}

func TestCelProvider_Execute_ArrayFilter(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": []any{2, 4, 6, 8, 10},
	})

	inputs := map[string]any{
		"expression": "_.input.filter(x, x > 5)",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.ElementsMatch(t, []any{int64(6), int64(8), int64(10)}, output.Data)
}

func TestCelProvider_Execute_WithVariables(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "Smith",
	})

	inputs := map[string]any{
		"expression": "prefix + ' ' + _.input + ' ' + suffix",
		"variables": map[string]any{
			"prefix": "Mr.",
			"suffix": "Jr.",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "Mr. Smith Jr.", output.Data)
}

func TestCelProvider_Execute_ComplexExpression(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": []any{
			map[string]any{"name": "alice", "active": true},
			map[string]any{"name": "bob", "active": false},
			map[string]any{"name": "charlie", "active": true},
		},
	})

	inputs := map[string]any{
		"expression": "_.input.filter(x, x.active).map(x, x.name.upperAscii())",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.ElementsMatch(t, []any{"ALICE", "CHARLIE"}, output.Data)
}

func TestCelProvider_Execute_Conditional(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": 25,
	})

	inputs := map[string]any{
		"expression": "_.input > 18 ? 'adult' : 'minor'",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "adult", output.Data)
}

func TestCelProvider_Execute_StringManipulation(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "hello world from cel",
	})

	inputs := map[string]any{
		"expression": "_.input.split(' ').map(x, x.upperAscii()).join('-')",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "HELLO-WORLD-FROM-CEL", output.Data)
}

func TestCelProvider_Execute_TypeConversion(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "42",
	})

	inputs := map[string]any{
		"expression": "int(_.input)",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, int64(42), output.Data)
}

func TestCelProvider_Execute_DryRun(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithResolverContext(ctx, map[string]any{
		"input": "test",
	})

	inputs := map[string]any{
		"expression": "_.input.upperAscii()",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Contains(t, output.Data, "DRY-RUN")

	assert.NotNil(t, output.Metadata)
	assert.Equal(t, true, output.Metadata["dryRun"])
}

func TestCelProvider_Execute_MissingExpression(t *testing.T) {
	p := NewCelProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"input": "test",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "expression is required")
}

func TestCelProvider_Execute_NoResolverData(t *testing.T) {
	p := NewCelProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"expression": "_.input.upperAscii()",
	}

	// Should error because 'input' key doesn't exist (caught at runtime with DynType)
	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such key: input")
}

func TestCelProvider_Execute_InvalidExpression(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "test",
	})

	inputs := map[string]any{
		"expression": "invalid..syntax!!",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to compile expression")
}

func TestCelProvider_Execute_RuntimeError(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "test",
	})

	inputs := map[string]any{
		"expression": "_.input.nonExistentMethod()",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to compile expression")
}

func TestCelProvider_Execute_ForEachWithCustomAliases(t *testing.T) {
	p := NewCelProvider()

	// Simulate the context set up by the executor during forEach iteration
	resolverData := map[string]any{
		"environment": "production",
		"__self":      []any{"a", "b", "c"},
		"__item":      map[string]any{"name": "api-service", "host": "localhost", "port": int64(8080)},
		"__index":     2,
		"svc":         map[string]any{"name": "api-service", "host": "localhost", "port": int64(8080)}, // custom item alias
		"idx":         2,                                                                               // custom index alias
	}
	ctx := provider.WithResolverContext(context.Background(), resolverData)

	// Add iteration context with alias information
	iterCtx := &provider.IterationContext{
		Item:       map[string]any{"name": "api-service", "host": "localhost", "port": int64(8080)},
		Index:      2,
		ItemAlias:  "svc",
		IndexAlias: "idx",
	}
	ctx = provider.WithIterationContext(ctx, iterCtx)

	// Expression uses custom aliases instead of __item and __index
	// Note: Use simple concatenation that works with CEL's type system
	inputs := map[string]any{
		"expression": `{
			'name': svc.name,
			'host': svc.host,
			'port': svc.port,
			'order': idx,
			'environment': _.environment
		}`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	result, ok := output.Data.(map[string]any)
	require.True(t, ok, "expected map[string]any, got %T", output.Data)

	assert.Equal(t, "api-service", result["name"])
	assert.Equal(t, "localhost", result["host"])
	assert.Equal(t, int64(8080), result["port"])
	assert.Equal(t, int64(2), result["order"])
	assert.Equal(t, "production", result["environment"])
}

func TestCelProvider_Execute_ForEachWithDefaultVariables(t *testing.T) {
	p := NewCelProvider()

	// Simulate the context set up by the executor during forEach iteration (no custom aliases)
	resolverData := map[string]any{
		"environment": "staging",
		"__self":      []any{"x", "y", "z"},
		"__item":      "current-item",
		"__index":     1,
	}
	ctx := provider.WithResolverContext(context.Background(), resolverData)

	// Expression uses default __item and __index variables
	inputs := map[string]any{
		"expression": `'Item: ' + __item + ', Index: ' + string(__index) + ', Env: ' + _.environment`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "Item: current-item, Index: 1, Env: staging", output.Data)
}

func TestCelProvider_Execute_SelfVariable(t *testing.T) {
	p := NewCelProvider()

	// Simulate context with __self for transform phase
	resolverData := map[string]any{
		"environment": "dev",
		"__self":      map[string]any{"value": 42, "label": "test"},
	}
	ctx := provider.WithResolverContext(context.Background(), resolverData)

	// Expression uses __self
	inputs := map[string]any{
		"expression": `__self.label + ': ' + string(__self.value)`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "test: 42", output.Data)
}
