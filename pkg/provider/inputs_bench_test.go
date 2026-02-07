package provider

import (
	"context"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

func BenchmarkInputResolver_Literal(b *testing.B) {
	ctx := context.Background()
	schema := schemahelper.ObjectSchema([]string{"name", "count", "enabled"}, map[string]*jsonschema.Schema{
		"name":    schemahelper.StringProp(""),
		"count":   schemahelper.IntProp(""),
		"enabled": schemahelper.BoolProp(""),
	})

	inputs := map[string]any{
		"name":    InputValue{Literal: "test-name"},
		"count":   InputValue{Literal: 42},
		"enabled": InputValue{Literal: true},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver := NewInputResolver(ctx, schema, nil)
		_, err := resolver.ResolveInputs(inputs)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInputResolver_Rslvr(b *testing.B) {
	resolverCtx := map[string]any{
		"environment": "prod",
		"region":      "us-east",
		"count":       100,
	}
	ctx := WithResolverContext(context.Background(), resolverCtx)

	schema := schemahelper.ObjectSchema([]string{"env", "region", "count"}, map[string]*jsonschema.Schema{
		"env":    schemahelper.StringProp(""),
		"region": schemahelper.StringProp(""),
		"count":  schemahelper.IntProp(""),
	})

	inputs := map[string]any{
		"env":    InputValue{Rslvr: "environment"},
		"region": InputValue{Rslvr: "region"},
		"count":  InputValue{Rslvr: "count"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver := NewInputResolver(ctx, schema, nil)
		_, err := resolver.ResolveInputs(inputs)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInputResolver_RslvrNested(b *testing.B) {
	resolverCtx := map[string]any{
		"config": map[string]any{
			"database": map[string]any{
				"host": "localhost",
				"port": 5432,
			},
		},
	}
	ctx := WithResolverContext(context.Background(), resolverCtx)

	schema := schemahelper.ObjectSchema([]string{"host", "port"}, map[string]*jsonschema.Schema{
		"host": schemahelper.StringProp(""),
		"port": schemahelper.IntProp(""),
	})

	inputs := map[string]any{
		"host": InputValue{Rslvr: "config.database.host"},
		"port": InputValue{Rslvr: "config.database.port"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver := NewInputResolver(ctx, schema, nil)
		_, err := resolver.ResolveInputs(inputs)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInputResolver_CEL(b *testing.B) {
	resolverCtx := map[string]any{
		"environment": "prod",
		"count":       10,
	}
	ctx := WithResolverContext(context.Background(), resolverCtx)

	schema := schemahelper.ObjectSchema([]string{"cluster", "scaled"}, map[string]*jsonschema.Schema{
		"cluster": schemahelper.StringProp(""),
		"scaled":  schemahelper.IntProp(""),
	})

	inputs := map[string]any{
		"cluster": InputValue{Expr: celexp.Expression(`environment + "-cluster"`)},
		"scaled":  InputValue{Expr: celexp.Expression("count * 2")},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver := NewInputResolver(ctx, schema, nil)
		_, err := resolver.ResolveInputs(inputs)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInputResolver_Template(b *testing.B) {
	resolverCtx := map[string]any{
		"environment": "prod",
		"region":      "us-east",
	}
	ctx := WithResolverContext(context.Background(), resolverCtx)

	schema := schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{
		"url": schemahelper.StringProp(""),
	})

	inputs := map[string]any{
		"url": InputValue{Tmpl: gotmpl.GoTemplatingContent("https://{{.environment}}.{{.region}}.example.com")},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver := NewInputResolver(ctx, schema, nil)
		_, err := resolver.ResolveInputs(inputs)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInputResolver_TypeCoercion(b *testing.B) {
	tests := []struct {
		name   string
		inputs map[string]any
		schema *jsonschema.Schema
	}{
		{
			name: "string to int",
			inputs: map[string]any{
				"count": InputValue{Literal: "42"},
			},
			schema: schemahelper.ObjectSchema([]string{"count"}, map[string]*jsonschema.Schema{
				"count": schemahelper.IntProp(""),
			}),
		},
		{
			name: "string to bool",
			inputs: map[string]any{
				"enabled": InputValue{Literal: "true"},
			},
			schema: schemahelper.ObjectSchema([]string{"enabled"}, map[string]*jsonschema.Schema{
				"enabled": schemahelper.BoolProp(""),
			}),
		},
		{
			name: "string to array",
			inputs: map[string]any{
				"tags": InputValue{Literal: "tag1,tag2,tag3"},
			},
			schema: schemahelper.ObjectSchema([]string{"tags"}, map[string]*jsonschema.Schema{
				"tags": schemahelper.ArrayProp(""),
			}),
		},
		{
			name: "int to string",
			inputs: map[string]any{
				"id": InputValue{Literal: 42},
			},
			schema: schemahelper.ObjectSchema([]string{"id"}, map[string]*jsonschema.Schema{
				"id": schemahelper.StringProp(""),
			}),
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			ctx := context.Background()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resolver := NewInputResolver(ctx, tt.schema, nil)
				_, err := resolver.ResolveInputs(tt.inputs)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkInputResolver_ComplexSchema(b *testing.B) {
	resolverCtx := map[string]any{
		"environment": "prod",
		"region":      "us-east",
		"config": map[string]any{
			"database": map[string]any{
				"host": "localhost",
				"port": 5432,
			},
		},
		"count": 10,
	}
	ctx := WithResolverContext(context.Background(), resolverCtx)

	schema := schemahelper.ObjectSchema([]string{"name", "env", "region", "dbHost", "dbPort", "url", "count", "scaledCount", "enabled", "tags"}, map[string]*jsonschema.Schema{
		"name":        schemahelper.StringProp(""),
		"env":         schemahelper.StringProp(""),
		"region":      schemahelper.StringProp(""),
		"dbHost":      schemahelper.StringProp(""),
		"dbPort":      schemahelper.IntProp(""),
		"url":         schemahelper.StringProp(""),
		"count":       schemahelper.IntProp(""),
		"scaledCount": schemahelper.IntProp(""),
		"enabled":     schemahelper.BoolProp(""),
		"tags":        schemahelper.ArrayProp(""),
	})

	inputs := map[string]any{
		"name":        InputValue{Literal: "my-app"},
		"env":         InputValue{Rslvr: "environment"},
		"region":      InputValue{Rslvr: "region"},
		"dbHost":      InputValue{Rslvr: "config.database.host"},
		"dbPort":      InputValue{Rslvr: "config.database.port"},
		"url":         InputValue{Tmpl: gotmpl.GoTemplatingContent("https://{{.environment}}.{{.region}}.example.com")},
		"count":       InputValue{Rslvr: "count"},
		"scaledCount": InputValue{Expr: celexp.Expression("count * 2")},
		"enabled":     InputValue{Literal: true},
		"tags":        InputValue{Literal: "web,api,production"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver := NewInputResolver(ctx, schema, nil)
		_, err := resolver.ResolveInputs(inputs)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInputResolver_SecretMasking(b *testing.B) {
	ctx := context.Background()
	schema := schemahelper.ObjectSchema([]string{"apiKey", "name"}, map[string]*jsonschema.Schema{
		"apiKey": schemahelper.StringProp(""),
		"name":   schemahelper.StringProp(""),
	})

	inputs := map[string]any{
		"apiKey": InputValue{Literal: "secret-api-key-12345"},
		"name":   InputValue{Literal: "my-app"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver := NewInputResolver(ctx, schema, []string{"apiKey"})
		_, err := resolver.ResolveInputs(inputs)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMaskValue(b *testing.B) {
	tests := []struct {
		name     string
		value    any
		isSecret bool
	}{
		{
			name:     "mask string secret",
			value:    "secret-password-123",
			isSecret: true,
		},
		{
			name:     "no mask public string",
			value:    "public-value",
			isSecret: false,
		},
		{
			name:     "mask int secret",
			value:    42,
			isSecret: true,
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = MaskValue(tt.value, tt.isSecret)
			}
		})
	}
}

func BenchmarkCoerceType(b *testing.B) {
	resolver := &InputResolver{}

	tests := []struct {
		name       string
		value      any
		targetType string
	}{
		{
			name:       "string to int",
			value:      "42",
			targetType: "integer",
		},
		{
			name:       "int to string",
			value:      42,
			targetType: "string",
		},
		{
			name:       "string to bool",
			value:      "true",
			targetType: "boolean",
		},
		{
			name:       "string to array",
			value:      "a,b,c,d,e",
			targetType: "array",
		},
		{
			name:       "no coercion needed",
			value:      map[string]any{"key": "value"},
			targetType: "",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := resolver.coerceType("test", tt.value, tt.targetType)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
