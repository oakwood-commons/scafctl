// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func makeMinimalDescriptor(name string) provider.Descriptor {
	return provider.Descriptor{
		Name:        name,
		DisplayName: name + "-display",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Description: "A test provider for unit tests.",
		Schema:      &jsonschema.Schema{},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: {},
		},
		Capabilities: []provider.Capability{provider.CapabilityFrom},
	}
}

func TestCapabilitiesToStrings_Empty(t *testing.T) {
	result := CapabilitiesToStrings(nil)
	assert.Empty(t, result)
}

func TestCapabilitiesToStrings_Multiple(t *testing.T) {
	caps := []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform, provider.CapabilityValidation}
	result := CapabilitiesToStrings(caps)
	assert.Equal(t, []string{"from", "transform", "validation"}, result)
}

func TestSchemaPlaceholder_Nil(t *testing.T) {
	result := SchemaPlaceholder("myField", nil)
	assert.Equal(t, "<value>", result)
}

func TestSchemaPlaceholder_WithExample(t *testing.T) {
	prop := &jsonschema.Schema{Examples: []any{"hello"}}
	result := SchemaPlaceholder("field", prop)
	assert.Contains(t, result, "hello")
}

func TestSchemaPlaceholder_WithEnum(t *testing.T) {
	prop := &jsonschema.Schema{Enum: []any{"option1"}}
	result := SchemaPlaceholder("field", prop)
	assert.Contains(t, result, "option1")
}

func TestSchemaPlaceholder_TypeString(t *testing.T) {
	prop := &jsonschema.Schema{Type: "string"}
	result := SchemaPlaceholder("myField", prop)
	assert.Equal(t, "<myField>", result)
}

func TestSchemaPlaceholder_TypeInteger(t *testing.T) {
	prop := &jsonschema.Schema{Type: "integer"}
	result := SchemaPlaceholder("count", prop)
	assert.Equal(t, "0", result)
}

func TestSchemaPlaceholder_TypeNumber(t *testing.T) {
	prop := &jsonschema.Schema{Type: "number"}
	result := SchemaPlaceholder("amount", prop)
	assert.Equal(t, "0", result)
}

func TestSchemaPlaceholder_TypeBoolean(t *testing.T) {
	prop := &jsonschema.Schema{Type: "boolean"}
	result := SchemaPlaceholder("enabled", prop)
	assert.Equal(t, "true", result)
}

func TestSchemaPlaceholder_TypeArray(t *testing.T) {
	prop := &jsonschema.Schema{Type: "array"}
	result := SchemaPlaceholder("items", prop)
	assert.Contains(t, result, "items")
}

func TestSchemaPlaceholder_TypeObject(t *testing.T) {
	prop := &jsonschema.Schema{Type: "object"}
	result := SchemaPlaceholder("config", prop)
	assert.Equal(t, "@config.yaml", result)
}

func TestSchemaPlaceholder_TypeDefault(t *testing.T) {
	prop := &jsonschema.Schema{Type: "unknown-type"}
	result := SchemaPlaceholder("val", prop)
	assert.Equal(t, "<val>", result)
}

func TestBuildSchemaOutput_Nil(t *testing.T) {
	result := BuildSchemaOutput(nil)
	assert.Nil(t, result)
}

func TestBuildSchemaOutput_EmptyProperties(t *testing.T) {
	result := BuildSchemaOutput(&jsonschema.Schema{})
	assert.Nil(t, result)
}

func TestBuildSchemaOutput_WithProperties(t *testing.T) {
	schema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"name": {Type: "string", Description: "The name field"},
		},
		Required: []string{"name"},
	}
	result := BuildSchemaOutput(schema)
	require.NotNil(t, result)
	props, ok := result["properties"].(map[string]any)
	require.True(t, ok)
	nameField, ok := props["name"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", nameField["type"])
	assert.Equal(t, "The name field", nameField["description"])
	assert.Equal(t, true, nameField["required"])
}

func TestGenerateCLIExamples_NilSchema(t *testing.T) {
	desc := makeMinimalDescriptor("test")
	desc.Schema = nil
	result := GenerateCLIExamples(&desc)
	assert.Nil(t, result)
}

func TestGenerateCLIExamples_EmptyProperties(t *testing.T) {
	desc := makeMinimalDescriptor("test")
	// makeMinimalDescriptor sets Schema with no properties, so GenerateCLIExamples returns nil
	result := GenerateCLIExamples(&desc)
	assert.Nil(t, result)
}

func TestGenerateCLIExamples_WithRequiredFields(t *testing.T) {
	desc := makeMinimalDescriptor("test")
	desc.Schema = &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"url": {Type: "string"},
		},
		Required: []string{"url"},
	}
	result := GenerateCLIExamples(&desc)
	require.NotEmpty(t, result)
	assert.Contains(t, result[0], "url=<url>")
	assert.Contains(t, result[0], "scafctl run provider test")
}

func TestGenerateCLIExamples_MultipleCapabilities(t *testing.T) {
	desc := makeMinimalDescriptor("multi")
	desc.Capabilities = []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform}
	desc.Schema = &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"field": {Type: "string"},
		},
	}
	result := GenerateCLIExamples(&desc)
	require.GreaterOrEqual(t, len(result), 2)
	// Should have an example with --capability transform
	hasCapExample := false
	for _, ex := range result {
		if contains(ex, "--capability transform") {
			hasCapExample = true
			break
		}
	}
	assert.True(t, hasCapExample, "expected an example with --capability transform")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && searchString(s, substr))
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestBuildProviderDetail_Minimal(t *testing.T) {
	desc := makeMinimalDescriptor("myprovider")
	result := BuildProviderDetail(desc)
	assert.Equal(t, "myprovider", result["name"])
	assert.Equal(t, "myprovider-display", result["displayName"])
	assert.Equal(t, "v1", result["apiVersion"])
	assert.Equal(t, "1.0.0", result["version"])
	assert.Equal(t, "A test provider for unit tests.", result["description"])
}

func TestBuildProviderDetail_WithAllFields(t *testing.T) {
	desc := makeMinimalDescriptor("full")
	desc.Category = "network"
	desc.Tags = []string{"http", "rest"}
	desc.Icon = "https://example.com/icon.svg"
	desc.IsDeprecated = true
	desc.Beta = true
	desc.Links = []provider.Link{{Name: "Docs", URL: "https://docs.example.com"}}
	desc.Examples = []provider.Example{{Name: "Basic", Description: "Example", YAML: "provider: full\n"}}
	desc.Maintainers = []provider.Contact{{Name: "Test User", Email: "test@example.com"}}
	desc.Schema = &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"field": {Type: "string"},
		},
	}
	desc.OutputSchemas = map[provider.Capability]*jsonschema.Schema{
		provider.CapabilityFrom: {
			Properties: map[string]*jsonschema.Schema{
				"result": {Type: "string"},
			},
		},
	}

	result := BuildProviderDetail(desc)
	assert.Equal(t, "network", result["category"])
	assert.Equal(t, true, result["deprecated"])
	assert.Equal(t, true, result["beta"])
	assert.NotNil(t, result["links"])
	assert.NotNil(t, result["examples"])
	assert.NotNil(t, result["maintainers"])
	assert.NotNil(t, result["schema"])
	assert.NotNil(t, result["outputSchemas"])
	assert.NotNil(t, result["cliUsage"])
	assert.NotContains(t, result, "operations")
	assert.NotContains(t, result, "writeOperations")
}

func TestBuildOperationDetail(t *testing.T) {
	t.Parallel()

	t.Run("minimal_read_operation_always_emits_name_and_isWrite", func(t *testing.T) {
		t.Parallel()
		op := provider.OperationDescriptor{
			Name:    "get_repo",
			IsWrite: false,
		}
		got := BuildOperationDetail(op)
		assert.Equal(t, "get_repo", got["name"])
		isWrite, hasIsWrite := got["isWrite"]
		require.True(t, hasIsWrite, "isWrite must always be present for CEL -w filters")
		assert.Equal(t, false, isWrite)
		assert.NotContains(t, got, "displayName")
		assert.NotContains(t, got, "description")
		assert.NotContains(t, got, "capabilities")
		assert.NotContains(t, got, "inputSchema")
		assert.NotContains(t, got, "outputSchema")
		assert.NotContains(t, got, "examples")
		assert.NotContains(t, got, "tags")
		assert.NotContains(t, got, "deprecated")
		assert.NotContains(t, got, "deprecationMessage")
	})

	t.Run("full_write_operation_includes_all_metadata_and_clones_tags", func(t *testing.T) {
		t.Parallel()
		tags := []string{"issues", "write"}
		op := provider.OperationDescriptor{
			Name:               "create_issue",
			DisplayName:        "Create Issue",
			Description:        "Create a new issue in the repository",
			Capabilities:       []provider.Capability{provider.CapabilityAction},
			IsWrite:            true,
			Tags:               tags,
			IsDeprecated:       true,
			DeprecationMessage: "Use create_issue_v2 instead",
			InputSchema: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"title"},
				Properties: map[string]*jsonschema.Schema{
					"title": {Type: "string", Description: "Issue title"},
				},
			},
			OutputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"number": {Type: "integer", Description: "Created issue number"},
				},
			},
			Examples: []provider.Example{
				{Name: "Create bug", Description: "Open a bug issue", YAML: "operation: create_issue\ntitle: Bug\n"},
			},
		}

		got := BuildOperationDetail(op)
		assert.Equal(t, "create_issue", got["name"])
		assert.Equal(t, true, got["isWrite"])
		assert.Equal(t, "Create Issue", got["displayName"])
		assert.Equal(t, "Create a new issue in the repository", got["description"])
		assert.Equal(t, []string{"action"}, got["capabilities"])
		assert.Equal(t, true, got["deprecated"])
		assert.Equal(t, "Use create_issue_v2 instead", got["deprecationMessage"])
		assert.NotNil(t, got["inputSchema"])
		assert.NotNil(t, got["outputSchema"])
		require.Len(t, got["examples"], 1)

		gotTags, ok := got["tags"].([]string)
		require.True(t, ok)
		gotTags[0] = "mutated"
		assert.Equal(t, "issues", tags[0], "mutating output tags must not mutate source descriptor")
	})

	t.Run("array_output_schema_preserves_type_and_items", func(t *testing.T) {
		t.Parallel()
		minItems := 1
		op := provider.OperationDescriptor{
			Name: "list_branches",
			OutputSchema: &jsonschema.Schema{
				Type:        "array",
				Description: "Branch names",
				MinItems:    &minItems,
				UniqueItems: true,
				Items: &jsonschema.Schema{
					Type:     "object",
					Required: []string{"name"},
					Properties: map[string]*jsonschema.Schema{
						"name":      {Type: "string"},
						"protected": {Type: "boolean"},
					},
				},
			},
		}
		got := BuildOperationDetail(op)
		outSchema, ok := got["outputSchema"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "array", outSchema["type"])
		assert.Equal(t, "Branch names", outSchema["description"])
		assert.Equal(t, int64(1), outSchema["minItems"])
		assert.Equal(t, true, outSchema["uniqueItems"])
		itemsMap, ok := outSchema["items"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "object", itemsMap["type"])
		assert.Equal(t, []any{"name"}, itemsMap["required"])
		itemProps, ok := itemsMap["properties"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, map[string]any{"type": "string"}, itemProps["name"])
		assert.Equal(t, map[string]any{"type": "boolean"}, itemProps["protected"])
	})

	t.Run("nested_object_and_array_input_schema_preserves_constraints_and_required_fields", func(t *testing.T) {
		t.Parallel()
		minLen := 1
		maxItems := 10
		minVal := 0.5
		maxVal := 5.0
		op := provider.OperationDescriptor{
			Name: "configure_webhook",
			InputSchema: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"config"},
				Properties: map[string]*jsonschema.Schema{
					"config": {
						Type:     "object",
						Required: []string{"endpoint"},
						Properties: map[string]*jsonschema.Schema{
							"endpoint": {
								Type:      "string",
								MinLength: &minLen,
								Pattern:   "^https://",
							},
							"retries": {
								Type:    "number",
								Minimum: &minVal,
								Maximum: &maxVal,
							},
						},
					},
					"events": {
						Type:     "array",
						MaxItems: &maxItems,
						Items: &jsonschema.Schema{
							Type: "string",
							Enum: []any{"push", "pull_request"},
						},
					},
				},
			},
		}

		got := BuildOperationDetail(op)
		inSchema, ok := got["inputSchema"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "object", inSchema["type"])
		assert.Equal(t, []any{"config"}, inSchema["required"])

		props, ok := inSchema["properties"].(map[string]any)
		require.True(t, ok)

		cfgMap, ok := props["config"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, []any{"endpoint"}, cfgMap["required"])
		cfgProps, ok := cfgMap["properties"].(map[string]any)
		require.True(t, ok)
		endpointMap, ok := cfgProps["endpoint"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, int64(1), endpointMap["minLength"])
		assert.Equal(t, "^https://", endpointMap["pattern"])

		retriesMap, ok := cfgProps["retries"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, float64(0.5), retriesMap["minimum"])
		assert.Equal(t, int64(5), retriesMap["maximum"])

		eventsMap, ok := props["events"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, int64(10), eventsMap["maxItems"])
		eventsItems, ok := eventsMap["items"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, []any{"push", "pull_request"}, eventsItems["enum"])

		// Mutating returned schema map must not mutate source descriptor.
		endpointMap["pattern"] = "mutated"
		assert.Equal(t, "^https://", op.InputSchema.Properties["config"].Properties["endpoint"].Pattern)
	})

	t.Run("dynamic_key_additional_properties_schema_is_preserved", func(t *testing.T) {
		t.Parallel()
		maxLen := 256
		op := provider.OperationDescriptor{
			Name: "set_labels",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				AdditionalProperties: &jsonschema.Schema{
					Type:      "string",
					MaxLength: &maxLen,
				},
			},
			OutputSchema: &jsonschema.Schema{
				AdditionalProperties: &jsonschema.Schema{
					Type: "integer",
				},
			},
		}

		got := BuildOperationDetail(op)
		inSchema, ok := got["inputSchema"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "object", inSchema["type"])
		addlIn, ok := inSchema["additionalProperties"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", addlIn["type"])
		assert.Equal(t, int64(256), addlIn["maxLength"])

		outSchema, ok := got["outputSchema"].(map[string]any)
		require.True(t, ok)
		addlOut, ok := outSchema["additionalProperties"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "integer", addlOut["type"])
	})

	t.Run("preserves_large_integers_above_2_pow_53_in_json_yaml_and_cel", func(t *testing.T) {
		t.Parallel()
		const aboveFloat53 int64 = 9007199254740993 // 2^53 + 1
		const maxUint uint64 = 18446744073709551615
		op := provider.OperationDescriptor{
			Name: "lookup_snowflake",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"snowflakeId": {
						Type:    "integer",
						Default: json.RawMessage(`9007199254740993`),
						Enum:    []any{aboveFloat53, maxUint},
					},
					"decimalInt": {
						Type:    "integer",
						Default: json.RawMessage(`9007199254740993.0`),
					},
					"exponentInt": {
						Type:    "integer",
						Default: json.RawMessage(`9.007199254740993e15`),
					},
					"negativeShiftInt": {
						Type:    "integer",
						Default: json.RawMessage(`-900719925474099300e-2`),
					},
					"exponentUint": {
						Type:    "integer",
						Default: json.RawMessage(`1.8446744073709551615e+19`),
					},
					"nonIntegral": {
						Type:    "number",
						Default: json.RawMessage(`12.5`),
					},
				},
			},
		}

		got := BuildOperationDetail(op)
		propsMap := got["inputSchema"].(map[string]any)["properties"].(map[string]any)
		propMap := propsMap["snowflakeId"].(map[string]any)
		assert.Equal(t, aboveFloat53, propMap["default"])
		assert.Equal(t, []any{aboveFloat53, maxUint}, propMap["enum"])
		assert.Equal(t, aboveFloat53, propsMap["decimalInt"].(map[string]any)["default"])
		assert.Equal(t, aboveFloat53, propsMap["exponentInt"].(map[string]any)["default"])
		assert.Equal(t, -aboveFloat53, propsMap["negativeShiftInt"].(map[string]any)["default"])
		assert.Equal(t, maxUint, propsMap["exponentUint"].(map[string]any)["default"])
		assert.Equal(t, float64(12.5), propsMap["nonIntegral"].(map[string]any)["default"])

		rawJSON, err := json.Marshal(got)
		require.NoError(t, err)
		assert.Contains(t, string(rawJSON), "9007199254740993")
		assert.Contains(t, string(rawJSON), "18446744073709551615")
		assert.NotContains(t, string(rawJSON), "9007199254740992")

		rawYAML, err := yaml.Marshal(got)
		require.NoError(t, err)
		var yamlNode yaml.Node
		require.NoError(t, yaml.Unmarshal(rawYAML, &yamlNode))
		assert.Contains(t, string(rawYAML), "default: 9007199254740993\n")
		assert.NotContains(t, string(rawYAML), `"9007199254740993"`)
		assert.NotContains(t, string(rawYAML), "9007199254740992")

		celRes, err := celexp.EvaluateExpression(
			context.Background(),
			"_.inputSchema.properties.snowflakeId.default == 9007199254740993 && "+
				"_.inputSchema.properties.decimalInt.default == 9007199254740993 && "+
				"_.inputSchema.properties.exponentInt.default == 9007199254740993 && "+
				"_.inputSchema.properties.snowflakeId.enum[0] == 9007199254740993",
			got,
			nil,
		)
		require.NoError(t, err)
		assert.Equal(t, true, celRes)
	})
}

func TestBuildOperationsOutput_And_ProviderDetailOperations(t *testing.T) {
	t.Parallel()

	assert.Nil(t, BuildOperationsOutput(nil))
	assert.Nil(t, BuildOperationsOutput([]provider.OperationDescriptor{}))

	t.Run("operations_only_derives_effective_write_operations", func(t *testing.T) {
		t.Parallel()
		desc := makeMinimalDescriptor("github")
		desc.Capabilities = []provider.Capability{provider.CapabilityFrom, provider.CapabilityAction}
		desc.Operations = []provider.OperationDescriptor{
			{Name: "get_repo", Description: "Get repo", Capabilities: []provider.Capability{provider.CapabilityFrom}, IsWrite: false},
			{Name: "create_issue", Description: "Create issue", Capabilities: []provider.Capability{provider.CapabilityAction}, IsWrite: true},
		}

		detail := BuildProviderDetail(desc)
		assert.Equal(t, []string{"create_issue"}, detail["writeOperations"])
		ops, ok := detail["operations"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, ops, 2)
		assert.Equal(t, "get_repo", ops[0]["name"])
		assert.Equal(t, false, ops[0]["isWrite"])
		assert.Equal(t, "create_issue", ops[1]["name"])
		assert.Equal(t, true, ops[1]["isWrite"])
	})

	t.Run("legacy_write_operations_only", func(t *testing.T) {
		t.Parallel()
		desc := makeMinimalDescriptor("legacy")
		desc.WriteOperations = []string{"write_item"}

		detail := BuildProviderDetail(desc)
		assert.Equal(t, []string{"write_item"}, detail["writeOperations"])
		assert.NotContains(t, detail, "operations")
	})
}

func BenchmarkBuildProviderDetail_WithOperations(b *testing.B) {
	desc := makeMinimalDescriptor("github")
	desc.Capabilities = []provider.Capability{provider.CapabilityFrom, provider.CapabilityAction}
	desc.Operations = []provider.OperationDescriptor{
		{Name: "get_repo", Description: "Get repository metadata", Capabilities: []provider.Capability{provider.CapabilityFrom}, IsWrite: false},
		{Name: "create_issue", Description: "Create an issue", Capabilities: []provider.Capability{provider.CapabilityAction}, IsWrite: true},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = BuildProviderDetail(desc)
	}
}

func TestBuildSchemaOutput_NilPropertyAndSliceCloning(t *testing.T) {
	t.Parallel()

	enums := []any{"open", "closed"}
	schema := &jsonschema.Schema{
		Required: []string{"nullableReq"},
		Properties: map[string]*jsonschema.Schema{
			"nullableReq": nil,
			"nullableOpt": nil,
			"state":       {Type: "string", Enum: enums},
		},
	}

	got := BuildSchemaOutput(schema)
	require.NotNil(t, got)
	props := got["properties"].(map[string]any)
	assert.Equal(t, map[string]any{"type": "any", "required": true}, props["nullableReq"])
	assert.Equal(t, map[string]any{"type": "any"}, props["nullableOpt"])

	stateProp := props["state"].(map[string]any)
	gotEnum := stateProp["enum"].([]any)
	gotEnum[0] = "mutated"
	assert.Equal(t, "open", enums[0], "mutating output enum must not mutate source schema")

	// Also verify BuildProviderDetail clones desc.Tags.
	tags := []string{"scm", "git"}
	desc := makeMinimalDescriptor("cloned")
	desc.Tags = tags
	detail := BuildProviderDetail(desc)
	detail["tags"].([]string)[0] = "changed"
	assert.Equal(t, "scm", tags[0])
}

func TestSanitizeAndFormatAvailableOperations(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", FormatAvailableOperations(nil))
	assert.Equal(t, "a, b", FormatAvailableOperations([]string{"a", "b"}))
	assert.Equal(t, "clean_op, escaped op injected, <unnamed>", FormatAvailableOperations([]string{
		"clean_op",
		"\x1b[31mescaped\x1b[0m\t\x1b]0;title\x07op\r\ninjected",
		"\x1b[31m\r\n\t ",
	}))
	many := make([]string, 20)
	for i := range many {
		many[i] = "op\x1b[32m\nline"
	}
	formattedMany := FormatAvailableOperations(many)
	assert.Contains(t, formattedMany, "... (20 total)")
	assert.NotContains(t, formattedMany, "\x1b")
	assert.NotContains(t, formattedMany, "\n")

	assert.Equal(t, "Hello world test", SanitizeInlineText(" \x1b[1;31mHello\x1b[0m\r\n\tworld\x1b]0;title\x07 \x00test "))
	assert.Equal(t, "line one\nline two", SanitizeMultilineText(" \x1b[32mline one\x1b[0m\r\nline two\x07 \n"))
}

func TestParseIntegralJSONNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		want   any
		wantOK bool
	}{
		{name: "plain_above_2_pow_53", input: "9007199254740993", want: int64(9007199254740993), wantOK: true},
		{name: "decimal_above_2_pow_53", input: "9007199254740993.0", want: int64(9007199254740993), wantOK: true},
		{name: "exponent_above_2_pow_53", input: "9.007199254740993e15", want: int64(9007199254740993), wantOK: true},
		{name: "min_int64_decimal", input: "-9223372036854775808.0", want: int64(-9223372036854775808), wantOK: true},
		{name: "min_int64_exponent", input: "-9.223372036854775808e18", want: int64(-9223372036854775808), wantOK: true},
		{name: "below_min_int64", input: "-9223372036854775809.0", want: nil, wantOK: false},
		{name: "max_int64", input: "9223372036854775807.0", want: int64(9223372036854775807), wantOK: true},
		{name: "max_int64_plus_one_uint64", input: "9223372036854775808.0", want: uint64(9223372036854775808), wantOK: true},
		{name: "max_uint64_extra_zeros", input: "18446744073709551615.00", want: uint64(18446744073709551615), wantOK: true},
		{name: "above_max_uint64", input: "18446744073709551616.0", want: nil, wantOK: false},
		{name: "negative_zero_decimal", input: "-0.0", want: int64(0), wantOK: true},
		{name: "zero_huge_exponent", input: "0.0e9999999999999999999999999", want: int64(0), wantOK: true},
		{name: "leading_frac_zeros_integral", input: "0.0005e4", want: int64(5), wantOK: true},
		{name: "leading_frac_zeros_non_integral", input: "0.050e1", want: nil, wantOK: false},
		{name: "negative_exponent_non_integral", input: "1230e-2", want: nil, wantOK: false},
		{name: "extreme_max_int_exponent", input: "1e9223372036854775807", want: nil, wantOK: false},
		{name: "extreme_min_int_exponent_no_frac", input: "1e-9223372036854775808", want: nil, wantOK: false},
		{name: "extreme_min_int_exponent_with_frac", input: "1.0e-9223372036854775808", want: nil, wantOK: false},
		{name: "overflow_atoi_exponent", input: "1e9999999999999999999999999", want: nil, wantOK: false},
		{name: "empty_string", input: "", want: nil, wantOK: false},
		{name: "trailing_dot_malformed", input: "1.", want: nil, wantOK: false},
		{name: "leading_dot_malformed", input: ".5", want: nil, wantOK: false},
		{name: "incomplete_exponent_malformed", input: "1e+", want: nil, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseIntegralJSONNumber(tc.input)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
