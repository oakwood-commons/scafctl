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
	assert.NotNil(t, desc.OutputSchema)
	assert.NotEmpty(t, desc.OutputSchema.Properties)
}

func TestCelProvider_Execute_StringTransform(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "hello world",
	})

	inputs := map[string]any{
		"expression": "input.upperAscii()",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "HELLO WORLD", data["result"])
	assert.Equal(t, "input.upperAscii()", data["expression"])
}

func TestCelProvider_Execute_Concatenation(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "original",
	})

	inputs := map[string]any{
		"expression": "input + ' - transformed'",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "original - transformed", data["result"])
}

func TestCelProvider_Execute_NumberTransform(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": 5,
	})

	inputs := map[string]any{
		"expression": "input * 2 + 10",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, int64(20), data["result"])
}

func TestCelProvider_Execute_BooleanLogic(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": 50,
	})

	inputs := map[string]any{
		"expression": "input > 10 && input < 100",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, true, data["result"])
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
		"expression": "input.name + ' is ' + string(input.age) + ' years old'",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "Alice is 30 years old", data["result"])
}

func TestCelProvider_Execute_ArrayTransform(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": []any{1, 2, 3, 4, 5},
	})

	inputs := map[string]any{
		"expression": "input.map(x, x * 2)",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	// CEL returns arrays that need to be asserted carefully
	assert.ElementsMatch(t, []any{int64(2), int64(4), int64(6), int64(8), int64(10)}, data["result"])
}

func TestCelProvider_Execute_ArrayFilter(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": []any{2, 4, 6, 8, 10},
	})

	inputs := map[string]any{
		"expression": "input.filter(x, x > 5)",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.ElementsMatch(t, []any{int64(6), int64(8), int64(10)}, data["result"])
}

func TestCelProvider_Execute_WithVariables(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "Smith",
	})

	inputs := map[string]any{
		"expression": "prefix + ' ' + input + ' ' + suffix",
		"variables": map[string]any{
			"prefix": "Mr.",
			"suffix": "Jr.",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "Mr. Smith Jr.", data["result"])
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
		"expression": "input.filter(x, x.active).map(x, x.name.upperAscii())",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.ElementsMatch(t, []any{"ALICE", "CHARLIE"}, data["result"])
}

func TestCelProvider_Execute_Conditional(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": 25,
	})

	inputs := map[string]any{
		"expression": "input > 18 ? 'adult' : 'minor'",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "adult", data["result"])
}

func TestCelProvider_Execute_StringManipulation(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "hello world from cel",
	})

	inputs := map[string]any{
		"expression": "input.split(' ').map(x, x.upperAscii()).join('-')",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "HELLO-WORLD-FROM-CEL", data["result"])
}

func TestCelProvider_Execute_TypeConversion(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{
		"input": "42",
	})

	inputs := map[string]any{
		"expression": "int(input)",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, int64(42), data["result"])
}

func TestCelProvider_Execute_DryRun(t *testing.T) {
	p := NewCelProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithResolverContext(ctx, map[string]any{
		"input": "test",
	})

	inputs := map[string]any{
		"expression": "input.upperAscii()",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Contains(t, data["result"], "DRY-RUN")
	assert.Equal(t, "input.upperAscii()", data["expression"])

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
		"expression": "input.upperAscii()",
	}

	// Should not error even without resolver data, just won't have 'input' variable
	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to compile expression")
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
		"expression": "input.nonExistentMethod()",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to compile expression")
}
