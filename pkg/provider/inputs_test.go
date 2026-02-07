package provider

import (
	"context"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInputValue_Literal(t *testing.T) {
	tests := []struct {
		name     string
		input    InputValue
		wantErr  bool
		expected any
	}{
		{
			name:     "string literal",
			input:    InputValue{Literal: "test"},
			expected: "test",
		},
		{
			name:     "int literal",
			input:    InputValue{Literal: 42},
			expected: 42,
		},
		{
			name:     "bool literal",
			input:    InputValue{Literal: true},
			expected: true,
		},
		{
			name:     "map literal",
			input:    InputValue{Literal: map[string]any{"key": "value"}},
			expected: map[string]any{"key": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			schema := schemahelper.ObjectSchema([]string{"testProp"}, map[string]*jsonschema.Schema{
				"testProp": schemahelper.AnyProp(""),
			})

			resolver := NewInputResolver(ctx, schema, nil)
			inputs := map[string]any{"testProp": tt.input}

			resolved, err := resolver.ResolveInputs(inputs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, resolved["testProp"])
		})
	}
}

func TestInputValue_Rslvr(t *testing.T) {
	tests := []struct {
		name     string
		binding  string
		context  map[string]any
		expected any
		wantErr  bool
	}{
		{
			name:     "simple binding",
			binding:  "environment",
			context:  map[string]any{"environment": "prod"},
			expected: "prod",
		},
		{
			name:     "nested binding",
			binding:  "config.namespace",
			context:  map[string]any{"config": map[string]any{"namespace": "default"}},
			expected: "default",
		},
		{
			name:     "deep nested binding",
			binding:  "app.database.host",
			context:  map[string]any{"app": map[string]any{"database": map[string]any{"host": "localhost"}}},
			expected: "localhost",
		},
		{
			name:    "binding not found",
			binding: "missing",
			context: map[string]any{"environment": "prod"},
			wantErr: true,
		},
		{
			name:    "nested binding not found",
			binding: "config.missing",
			context: map[string]any{"config": map[string]any{"namespace": "default"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithResolverContext(context.Background(), tt.context)
			schema := schemahelper.ObjectSchema([]string{"testProp"}, map[string]*jsonschema.Schema{
				"testProp": schemahelper.AnyProp(""),
			})

			resolver := NewInputResolver(ctx, schema, nil)
			inputs := map[string]any{
				"testProp": InputValue{Rslvr: tt.binding},
			}

			resolved, err := resolver.ResolveInputs(inputs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, resolved["testProp"])
		})
	}
}

func TestInputValue_CEL(t *testing.T) {
	tests := []struct {
		name       string
		expression celexp.Expression
		context    map[string]any
		expected   any
		wantErr    bool
	}{
		{
			name:       "simple variable access",
			expression: "environment",
			context:    map[string]any{"environment": "prod"},
			expected:   "prod",
		},
		{
			name:       "string concatenation",
			expression: `environment + "-cluster"`,
			context:    map[string]any{"environment": "prod"},
			expected:   "prod-cluster",
		},
		{
			name:       "arithmetic expression",
			expression: "count * 2",
			context:    map[string]any{"count": 5},
			expected:   int64(10),
		},
		{
			name:       "conditional expression",
			expression: `environment == "prod" ? "production" : "development"`,
			context:    map[string]any{"environment": "prod"},
			expected:   "production",
		},
		{
			name:       "map access",
			expression: "config.namespace",
			context:    map[string]any{"config": map[string]any{"namespace": "default"}},
			expected:   "default",
		},
		{
			name:       "invalid expression",
			expression: "this is not valid CEL",
			context:    map[string]any{},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithResolverContext(context.Background(), tt.context)
			schema := schemahelper.ObjectSchema([]string{"testProp"}, map[string]*jsonschema.Schema{
				"testProp": schemahelper.AnyProp(""),
			})

			resolver := NewInputResolver(ctx, schema, nil)
			inputs := map[string]any{
				"testProp": InputValue{Expr: tt.expression},
			}

			resolved, err := resolver.ResolveInputs(inputs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, resolved["testProp"])
		})
	}
}

func TestInputValue_Template(t *testing.T) {
	tests := []struct {
		name     string
		template gotmpl.GoTemplatingContent
		context  map[string]any
		expected string
		wantErr  bool
	}{
		{
			name:     "simple variable",
			template: "{{.environment}}",
			context:  map[string]any{"environment": "prod"},
			expected: "prod",
		},
		{
			name:     "string concatenation",
			template: "Environment: {{.environment}}",
			context:  map[string]any{"environment": "prod"},
			expected: "Environment: prod",
		},
		{
			name:     "nested access",
			template: "{{.config.namespace}}",
			context:  map[string]any{"config": map[string]any{"namespace": "default"}},
			expected: "default",
		},
		{
			name:     "multiple variables",
			template: "{{.env}}-{{.region}}-{{.cluster}}",
			context:  map[string]any{"env": "prod", "region": "us-east", "cluster": "primary"},
			expected: "prod-us-east-primary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithResolverContext(context.Background(), tt.context)
			schema := schemahelper.ObjectSchema([]string{"testProp"}, map[string]*jsonschema.Schema{
				"testProp": schemahelper.StringProp(""),
			})

			resolver := NewInputResolver(ctx, schema, nil)
			inputs := map[string]any{
				"testProp": InputValue{Tmpl: tt.template},
			}

			resolved, err := resolver.ResolveInputs(inputs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, resolved["testProp"])
		})
	}
}

func TestInputResolver_Exclusivity(t *testing.T) {
	tests := []struct {
		name    string
		input   InputValue
		wantErr bool
		errMsg  string
	}{
		{
			name:    "no form specified",
			input:   InputValue{},
			wantErr: true,
			errMsg:  "no input form specified",
		},
		{
			name:    "literal and rslvr",
			input:   InputValue{Literal: "value", Rslvr: "binding"},
			wantErr: true,
			errMsg:  "multiple input forms specified",
		},
		{
			name:    "literal and expr",
			input:   InputValue{Literal: "value", Expr: "expression"},
			wantErr: true,
			errMsg:  "multiple input forms specified",
		},
		{
			name:    "rslvr and tmpl",
			input:   InputValue{Rslvr: "binding", Tmpl: "template"},
			wantErr: true,
			errMsg:  "multiple input forms specified",
		},
		{
			name:    "all forms",
			input:   InputValue{Literal: "value", Rslvr: "binding", Expr: "expr", Tmpl: "tmpl"},
			wantErr: true,
			errMsg:  "multiple input forms specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			schema := schemahelper.ObjectSchema([]string{"testProp"}, map[string]*jsonschema.Schema{
				"testProp": schemahelper.AnyProp(""),
			})

			resolver := NewInputResolver(ctx, schema, nil)
			inputs := map[string]any{"testProp": tt.input}

			_, err := resolver.ResolveInputs(inputs)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInputResolver_TypeCoercion(t *testing.T) {
	tests := []struct {
		name       string
		value      any
		targetType string
		expected   any
		wantErr    bool
	}{
		// String coercion
		{
			name:       "string to string",
			value:      "test",
			targetType: "string",
			expected:   "test",
		},
		{
			name:       "int to string",
			value:      42,
			targetType: "string",
			expected:   "42",
		},
		{
			name:       "bool to string",
			value:      true,
			targetType: "string",
			expected:   "true",
		},

		// Int coercion
		{
			name:       "int to int",
			value:      42,
			targetType: "integer",
			expected:   42,
		},
		{
			name:       "string to int",
			value:      "42",
			targetType: "integer",
			expected:   42,
		},
		{
			name:       "float to int",
			value:      42.5,
			targetType: "integer",
			expected:   42,
		},
		{
			name:       "invalid string to int",
			value:      "not-a-number",
			targetType: "integer",
			wantErr:    true,
		},

		// Float coercion
		{
			name:       "float to float",
			value:      42.5,
			targetType: "number",
			expected:   42.5,
		},
		{
			name:       "string to float",
			value:      "42.5",
			targetType: "number",
			expected:   42.5,
		},
		{
			name:       "int to float",
			value:      42,
			targetType: "number",
			expected:   42.0,
		},
		{
			name:       "invalid string to float",
			value:      "not-a-number",
			targetType: "number",
			wantErr:    true,
		},

		// Bool coercion
		{
			name:       "bool to bool",
			value:      true,
			targetType: "boolean",
			expected:   true,
		},
		{
			name:       "string 'true' to bool",
			value:      "true",
			targetType: "boolean",
			expected:   true,
		},
		{
			name:       "string 'false' to bool",
			value:      "false",
			targetType: "boolean",
			expected:   false,
		},
		{
			name:       "string '1' to bool",
			value:      "1",
			targetType: "boolean",
			expected:   true,
		},
		{
			name:       "invalid string to bool",
			value:      "not-a-bool",
			targetType: "boolean",
			wantErr:    true,
		},

		// Array coercion
		{
			name:       "slice to array",
			value:      []string{"a", "b", "c"},
			targetType: "array",
			expected:   []string{"a", "b", "c"},
		},
		{
			name:       "comma-separated string to array",
			value:      "a,b,c",
			targetType: "array",
			expected:   []string{"a", "b", "c"},
		},
		{
			name:       "empty string to array",
			value:      "",
			targetType: "array",
			expected:   []string{},
		},
		{
			name:       "string with spaces to array",
			value:      "a, b, c",
			targetType: "array",
			expected:   []string{"a", "b", "c"},
		},

		// Any type (no coercion)
		{
			name:       "any type preserves value",
			value:      map[string]any{"key": "value"},
			targetType: "",
			expected:   map[string]any{"key": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &InputResolver{}
			result, err := resolver.coerceType("testProp", tt.value, tt.targetType)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInputResolver_RequiredProperties(t *testing.T) {
	tests := []struct {
		name    string
		schema  *jsonschema.Schema
		inputs  map[string]any
		wantErr bool
		errMsg  string
	}{
		{
			name: "required property provided",
			schema: schemahelper.ObjectSchema([]string{"name"}, map[string]*jsonschema.Schema{
				"name": schemahelper.StringProp(""),
			}),
			inputs:  map[string]any{"name": InputValue{Literal: "test"}},
			wantErr: false,
		},
		{
			name: "required property missing",
			schema: schemahelper.ObjectSchema([]string{"name"}, map[string]*jsonschema.Schema{
				"name": schemahelper.StringProp(""),
			}),
			inputs:  map[string]any{},
			wantErr: true,
			errMsg:  "required property \"name\" is missing",
		},
		{
			name: "required property with default",
			schema: schemahelper.ObjectSchema([]string{"name"}, map[string]*jsonschema.Schema{
				"name": schemahelper.StringProp("", schemahelper.WithDefault("default-name")),
			}),
			inputs:  map[string]any{},
			wantErr: false,
		},
		{
			name: "optional property missing",
			schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"name": schemahelper.StringProp(""),
			}),
			inputs:  map[string]any{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			resolver := NewInputResolver(ctx, tt.schema, nil)

			_, err := resolver.ResolveInputs(tt.inputs)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInputResolver_SecretRedaction(t *testing.T) {
	tests := []struct {
		name            string
		schema          *jsonschema.Schema
		sensitiveFields []string
		input           InputValue
		wantMask        bool
	}{
		{
			name: "secret property with invalid rslvr",
			schema: schemahelper.ObjectSchema([]string{"testProp"}, map[string]*jsonschema.Schema{
				"testProp": schemahelper.StringProp(""),
			}),
			sensitiveFields: []string{"testProp"},
			input:           InputValue{Rslvr: "nonexistent"},
			wantMask:        true,
		},
		{
			name: "non-secret property with invalid rslvr",
			schema: schemahelper.ObjectSchema([]string{"testProp"}, map[string]*jsonschema.Schema{
				"testProp": schemahelper.StringProp(""),
			}),
			sensitiveFields: nil,
			input:           InputValue{Rslvr: "nonexistent"},
			wantMask:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			resolver := NewInputResolver(ctx, tt.schema, tt.sensitiveFields)
			inputs := map[string]any{"testProp": tt.input}

			_, err := resolver.ResolveInputs(inputs)
			require.Error(t, err)

			if tt.wantMask {
				// Should contain masked version
				assert.Contains(t, err.Error(), SecretMask)
				// Should NOT contain the actual binding name
				assert.NotContains(t, err.Error(), "nonexistent")
			} else {
				// Should contain the actual binding name
				assert.Contains(t, err.Error(), "nonexistent")
			}
		})
	}
}

func TestMaskValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		isSecret bool
		expected string
	}{
		{
			name:     "secret string",
			value:    "password123",
			isSecret: true,
			expected: SecretMask,
		},
		{
			name:     "non-secret string",
			value:    "public-value",
			isSecret: false,
			expected: "public-value",
		},
		{
			name:     "secret int",
			value:    42,
			isSecret: true,
			expected: SecretMask,
		},
		{
			name:     "non-secret int",
			value:    42,
			isSecret: false,
			expected: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskValue(tt.value, tt.isSecret)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInputResolver_NormalizeInputMap(t *testing.T) {
	tests := []struct {
		name      string
		rawInputs any
		expected  map[string]InputValue
		wantErr   bool
	}{
		{
			name:      "nil inputs",
			rawInputs: nil,
			expected:  map[string]InputValue{},
		},
		{
			name: "direct InputValue",
			rawInputs: map[string]any{
				"prop1": InputValue{Literal: "value1"},
			},
			expected: map[string]InputValue{
				"prop1": {Literal: "value1"},
			},
		},
		{
			name: "map with literal key",
			rawInputs: map[string]any{
				"prop1": map[string]any{"literal": "value1"},
			},
			expected: map[string]InputValue{
				"prop1": {Literal: "value1"},
			},
		},
		{
			name: "map with rslvr key",
			rawInputs: map[string]any{
				"prop1": map[string]any{"rslvr": "binding"},
			},
			expected: map[string]InputValue{
				"prop1": {Rslvr: "binding"},
			},
		},
		{
			name: "map with expr key",
			rawInputs: map[string]any{
				"prop1": map[string]any{"expr": "expression"},
			},
			expected: map[string]InputValue{
				"prop1": {Expr: "expression"},
			},
		},
		{
			name: "map with tmpl key",
			rawInputs: map[string]any{
				"prop1": map[string]any{"tmpl": "template"},
			},
			expected: map[string]InputValue{
				"prop1": {Tmpl: "template"},
			},
		},
		{
			name: "plain value as literal",
			rawInputs: map[string]any{
				"prop1": "value1",
			},
			expected: map[string]InputValue{
				"prop1": {Literal: "value1"},
			},
		},
		{
			name:      "invalid input type",
			rawInputs: "not a map",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &InputResolver{}
			result, err := resolver.normalizeInputMap(tt.rawInputs)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
