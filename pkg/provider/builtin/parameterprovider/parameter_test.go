// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package parameterprovider

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockHTTPClient is a mock HTTP client for testing
type MockHTTPClient struct {
	Response *http.Response
	Err      error
}

func (m *MockHTTPClient) Get(_ context.Context, _ string) (*http.Response, error) {
	return m.Response, m.Err
}

// MockFileOps is a mock file operations for testing
type MockFileOps struct {
	Content []byte
	Err     error
}

func (m *MockFileOps) ReadFile(_ string) ([]byte, error) {
	return m.Content, m.Err
}

func TestNewParameterProvider(t *testing.T) {
	p := NewParameterProvider()

	assert.NotNil(t, p)
	assert.NotNil(t, p.Descriptor())
	assert.Equal(t, ProviderName, p.Descriptor().Name)
	assert.Equal(t, Version, p.Descriptor().Version.String())
}

func TestParameterProvider_Execute_StringParameter(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"env": "prod",
	})

	output, err := p.Execute(ctx, map[string]any{
		"key": "env",
	})

	require.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "prod", output.Data)
	assert.Equal(t, true, output.Metadata["exists"])
	assert.Equal(t, "string", output.Metadata["type"])
}

func TestParameterProvider_Execute_MissingParameter_NoDefault(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key": "env",
	})

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "not provided")
}

func TestParameterProvider_Execute_NoKey(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{})

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "at least one non-empty parameter name is required")
}

func TestParameterProvider_Execute_Keys_FirstMatchWins(t *testing.T) {
	tests := []struct {
		name     string
		inputs   map[string]any
		params   map[string]any
		expected any
		exists   bool
	}{
		{
			name:     "first alias provided wins",
			inputs:   map[string]any{"keys": []any{"environment", "e", "env"}},
			params:   map[string]any{"environment": "prod"},
			expected: "prod",
			exists:   true,
		},
		{
			name:     "earlier alias wins over later when both provided",
			inputs:   map[string]any{"keys": []any{"environment", "e", "env"}},
			params:   map[string]any{"e": "qa", "env": "dev"},
			expected: "qa",
			exists:   true,
		},
		{
			name:     "later alias used when earlier absent",
			inputs:   map[string]any{"keys": []any{"environment", "e", "env"}},
			params:   map[string]any{"env": "dev"},
			expected: "dev",
			exists:   true,
		},
		{
			name:     "keys accepts []string from coerced array inputs",
			inputs:   map[string]any{"keys": []string{"environment", "e", "env"}},
			params:   map[string]any{"e": "qa"},
			expected: "qa",
			exists:   true,
		},
		{
			name:     "key takes precedence over keys",
			inputs:   map[string]any{"key": "appName", "keys": []any{"app_name"}},
			params:   map[string]any{"appName": "alpha", "app_name": "beta"},
			expected: "alpha",
			exists:   true,
		},
		{
			name:     "falls through to key alias when primary absent",
			inputs:   map[string]any{"key": "appName", "keys": []any{"app_name"}},
			params:   map[string]any{"app_name": "beta"},
			expected: "beta",
			exists:   true,
		},
		{
			name:     "default used when no alias provided",
			inputs:   map[string]any{"keys": []any{"environment", "e", "env"}, "default": "development"},
			params:   map[string]any{},
			expected: "development",
			exists:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParameterProvider()
			ctx := provider.WithParameters(context.Background(), tt.params)

			output, err := p.Execute(ctx, tt.inputs)

			require.NoError(t, err)
			require.NotNil(t, output)
			assert.Equal(t, tt.expected, output.Data)
			assert.Equal(t, tt.exists, output.Metadata["exists"])
		})
	}
}

func TestParameterProvider_Execute_Keys_NoneProvided_NoDefault(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"keys": []any{"environment", "env"},
	})

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "not provided")
	// Error references the first candidate name.
	assert.Contains(t, err.Error(), "environment")
}

func TestParameterProvider_Execute_Keys_InvalidType(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	tests := []struct {
		name   string
		inputs map[string]any
	}{
		{"keys not a list", map[string]any{"keys": "environment"}},
		{"keys element not a string", map[string]any{"keys": []any{"env", 42}}},
		{"key not a string", map[string]any{"key": 42}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := p.Execute(ctx, tt.inputs)
			assert.Error(t, err)
			assert.Nil(t, output)
		})
	}
}

func TestParameterProvider_Execute_Keys_DuplicatesDeduped(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"env": "prod",
	})

	output, err := p.Execute(ctx, map[string]any{
		"key":  "env",
		"keys": []any{"env", "environment"},
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "prod", output.Data)
	assert.Equal(t, true, output.Metadata["exists"])
}

func TestParameterProvider_Execute_ForceString(t *testing.T) {
	tests := []struct {
		name     string
		raw      any
		typeIn   any
		expected any
	}{
		{"integer kept as string", "52926", "string", "52926"},
		{"boolean kept as string", "false", "string", "false"},
		{"float kept as string", "3.14", "string", "3.14"},
		{"csv kept as string", "a,b,c", "string", "a,b,c"},
		{"json kept as string", `{"a":1}`, "string", `{"a":1}`},
		{"quoted value is unquoted", `"52926"`, "string", "52926"},
		{"non-string default coerced to string", int64(52926), "string", "52926"},
		{"coercion still applies without type", "52926", nil, int64(52926)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParameterProvider()
			ctx := provider.WithParameters(context.Background(), map[string]any{
				"billingId": tt.raw,
			})

			inputs := map[string]any{"key": "billingId"}
			if tt.typeIn != nil {
				inputs["type"] = tt.typeIn
			}

			output, err := p.Execute(ctx, inputs)
			require.NoError(t, err)
			require.NotNil(t, output)
			assert.Equal(t, tt.expected, output.Data)
			assert.Equal(t, true, output.Metadata["exists"])
		})
	}
}

func TestParameterProvider_Execute_ForceString_Default(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key":     "billingId",
		"type":    "string",
		"default": "52926",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "52926", output.Data)
	assert.Equal(t, false, output.Metadata["exists"])
	assert.Equal(t, "string", output.Metadata["type"])
}

func TestParameterProvider_Execute_ForceString_DryRun(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithDryRun(provider.WithParameters(context.Background(), map[string]any{
		"billingId": "52926",
	}), true)

	output, err := p.Execute(ctx, map[string]any{
		"key":  "billingId",
		"type": "string",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "string", output.Metadata["type"])
	assert.Equal(t, true, output.Metadata["dryRun"])
}

func TestParameterProvider_Execute_TypedCoercion(t *testing.T) {
	tests := []struct {
		name     string
		raw      any
		typeIn   string
		expected any
		typeMeta string
	}{
		{"int from string", "8080", TypeInt, int64(8080), "integer"},
		{"int from int default", int64(8080), TypeInt, int64(8080), "integer"},
		{"int from int32 default", int32(8080), TypeInt, int64(8080), "integer"},
		{"int from uint64 default", uint64(8080), TypeInt, int64(8080), "integer"},
		{"float from string", "3.14", TypeFloat, 3.14, "float"},
		{"float from int", 42, TypeFloat, float64(42), "float"},
		{"float from int32", int32(42), TypeFloat, float64(42), "float"},
		{"float from uint64", uint64(42), TypeFloat, float64(42), "float"},
		{"bool from string", "true", TypeBool, true, "boolean"},
		{"bool from bool default", false, TypeBool, false, "boolean"},
		{"csv from string", "a,b,c", TypeCSV, []string{"a", "b", "c"}, "array"},
		{"csv with spaces", "a, b , c", TypeCSV, []string{"a", "b", "c"}, "array"},
		{"json object", `{"a":1}`, TypeJSON, map[string]any{"a": float64(1)}, "object"},
		{"json with surrounding whitespace", "  {\"a\":1}  ", TypeJSON, map[string]any{"a": float64(1)}, "object"},
		{"json quote-wrapped payload", "\"{\"a\":1}\"", TypeJSON, map[string]any{"a": float64(1)}, "object"},
		{"json string literal", `"hello"`, TypeJSON, "hello", "string"},
		{"raw keeps numeric default", int64(42), TypeRaw, int64(42), "integer"},
		{"raw keeps verbatim string", "00042", TypeRaw, "00042", "string"},
		{"auto infers int", "12345", TypeAuto, int64(12345), "integer"},
		{"auto does not split csv", "a,b,c", TypeAuto, "a,b,c", "string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParameterProvider()
			ctx := provider.WithParameters(context.Background(), map[string]any{
				"val": tt.raw,
			})

			output, err := p.Execute(ctx, map[string]any{
				"key":  "val",
				"type": tt.typeIn,
			})
			require.NoError(t, err)
			require.NotNil(t, output)
			assert.Equal(t, tt.expected, output.Data)
			assert.Equal(t, tt.typeMeta, output.Metadata["type"])
		})
	}
}

func TestParameterProvider_Execute_TypedCoercion_Errors(t *testing.T) {
	tests := []struct {
		name    string
		raw     any
		typeIn  string
		wantMsg string
	}{
		{"int rejects non-numeric", "abc", TypeInt, "cannot parse"},
		{"int rejects fractional float", 3.5, TypeInt, "without loss"},
		{"int rejects out-of-range float", 1e300, TypeInt, "without loss"},
		{"int rejects overflow uint64", uint64(math.MaxUint64), TypeInt, "without loss"},
		{"float rejects non-numeric", "abc", TypeFloat, "cannot parse"},
		{"bool rejects other", "yes", TypeBool, "expected true or false"},
		{"json rejects invalid", "{not json", TypeJSON, "cannot parse value as JSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParameterProvider()
			ctx := provider.WithParameters(context.Background(), map[string]any{
				"val": tt.raw,
			})

			output, err := p.Execute(ctx, map[string]any{
				"key":  "val",
				"type": tt.typeIn,
			})
			require.Error(t, err)
			assert.Nil(t, output)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestCoerceInt_NumericWidths(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"int", int(7)},
		{"int8", int8(7)},
		{"int16", int16(7)},
		{"int32", int32(7)},
		{"int64", int64(7)},
		{"uint", uint(7)},
		{"uint8", uint8(7)},
		{"uint16", uint16(7)},
		{"uint32", uint32(7)},
		{"uint64", uint64(7)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coerceInt(tt.value)
			require.NoError(t, err)
			assert.Equal(t, int64(7), got)
		})
	}
}

func TestCoerceFloat_NumericWidths(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"float32", float32(7)},
		{"float64", float64(7)},
		{"int", int(7)},
		{"int8", int8(7)},
		{"int16", int16(7)},
		{"int32", int32(7)},
		{"int64", int64(7)},
		{"uint", uint(7)},
		{"uint8", uint8(7)},
		{"uint16", uint16(7)},
		{"uint32", uint32(7)},
		{"uint64", uint64(7)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coerceFloat(tt.value)
			require.NoError(t, err)
			assert.Equal(t, float64(7), got)
		})
	}
}

func TestParameterProvider_Execute_UnsupportedType(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{"val": "x"})

	output, err := p.Execute(ctx, map[string]any{
		"key":  "val",
		"type": "number",
	})
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "unsupported type")
}

func TestParameterProvider_Execute_TypeNotString(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{"val": "x"})

	output, err := p.Execute(ctx, map[string]any{
		"key":  "val",
		"type": 123,
	})
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "type must be a string")
}

func TestParameterProvider_Execute_TypedDefault(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key":     "port",
		"type":    TypeInt,
		"default": "9090",
	})
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, int64(9090), output.Data)
	assert.Equal(t, false, output.Metadata["exists"])
}

func TestParameterProvider_Execute_TypedDryRun_ReportsType(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithDryRun(provider.WithParameters(context.Background(), map[string]any{}), true)

	output, err := p.Execute(ctx, map[string]any{
		"key":  "port",
		"type": TypeInt,
	})
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, TypeInt, output.Metadata["type"])

	// No type provided -> reports auto.
	output, err = p.Execute(ctx, map[string]any{"key": "port"})
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, TypeAuto, output.Metadata["type"])
}

func TestParameterProvider_ParseValue_Boolean(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"lowercase true", "true", true},
		{"uppercase TRUE", "TRUE", true},
		{"lowercase false", "false", false},
		{"uppercase FALSE", "FALSE", false},
		{"mixed case True", "True", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.parseValue(ctx, tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_ParseValue_Integer(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"positive", "42", 42},
		{"negative", "-10", -10},
		{"zero", "0", 0},
		{"large", "9999999", 9999999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.parseValue(ctx, tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_ParseValue_Float(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected float64
	}{
		{"decimal", "3.14", 3.14},
		{"negative", "-2.5", -2.5},
		{"scientific", "1e10", 1e10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.parseValue(ctx, tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_ParseValue_CSV_NotAutoSplit(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	// Comma-separated values are NOT auto-split; they stay literal strings.
	tests := []string{"a,b,c", "a, b, c", "us-east1,us-west1,eu-west1"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			result, err := p.parseValue(ctx, input)
			require.NoError(t, err)
			assert.Equal(t, input, result)
		})
	}
}

func TestParameterProvider_ParseValue_QuotedString(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	// Quoted strings are unquoted and returned as literal strings.
	result, err := p.parseValue(ctx, `"a,b,c"`)
	require.NoError(t, err)
	assert.Equal(t, "a,b,c", result)
}

func TestParameterProvider_ParseValue_JSON_Object(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	result, err := p.parseValue(ctx, `{"key":"value","num":42}`)
	require.NoError(t, err)

	obj, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", obj["key"])
	assert.Equal(t, float64(42), obj["num"]) // JSON numbers are float64
}

func TestParameterProvider_ParseValue_JSON_Array(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	result, err := p.parseValue(ctx, `["a","b","c"]`)
	require.NoError(t, err)

	arr, ok := result.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 3)
	assert.Equal(t, "a", arr[0])
}

func TestParameterProvider_ParseValue_FileProtocol(t *testing.T) {
	mockFile := &MockFileOps{
		Content: []byte("file content"),
		Err:     nil,
	}

	p := NewParameterProvider(WithFileOps(mockFile))
	ctx := context.Background()

	result, err := p.parseValue(ctx, "file:///path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "file content", result)
}

func TestParameterProvider_ParseValue_FileProtocol_Error(t *testing.T) {
	mockFile := &MockFileOps{
		Content: nil,
		Err:     errors.New("file not found"),
	}

	p := NewParameterProvider(WithFileOps(mockFile))
	ctx := context.Background()

	result, err := p.parseValue(ctx, "file:///path/to/missing.txt")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestParameterProvider_ParseValue_LiteralString(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "hello", "hello"},
		{"with spaces", "hello world", "hello world"},
		{"url-like but quoted", `"https://example.com"`, "https://example.com"},
		{"number-like string", "042x", "042x"}, // Not parseable as number
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.parseValue(ctx, tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_ParseValue_Stdin_ShouldError(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	result, err := p.parseValue(ctx, "-")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "stdin value")
}

func TestParameterProvider_Execute_DryRun(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithParameters(ctx, map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key": "env",
	})

	require.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "[DRY-RUN] Not retrieved", output.Data)
	assert.True(t, output.Metadata["dryRun"].(bool))
}

func TestParameterProvider_Execute_AlreadyParsedValue(t *testing.T) {
	p := NewParameterProvider()

	// Simulate value already parsed (e.g., from merging multiple -r flags)
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"items": []string{"a", "b", "c"},
	})

	output, err := p.Execute(ctx, map[string]any{
		"key": "items",
	})

	require.NoError(t, err)
	assert.NotNil(t, output)
	result := output.Data
	arr, ok := result.([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, arr)
}

func TestDetectType(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"nil", nil, "null"},
		{"bool", true, "boolean"},
		{"int", 42, "integer"},
		{"int64", int64(42), "integer"},
		{"float64", 3.14, "float"},
		{"string", "hello", "string"},
		{"array", []string{"a", "b"}, "array"},
		{"map", map[string]any{"key": "value"}, "object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsQuoted(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"quoted", `"hello"`, true},
		{"not quoted", "hello", false},
		{"single char", `"a"`, true},
		{"empty quotes", `""`, true},
		{"one quote", `"hello`, false},
		{"trailing quote", `hello"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isQuoted(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_Descriptor(t *testing.T) {
	p := NewParameterProvider()
	desc := p.Descriptor()

	assert.Equal(t, ProviderName, desc.Name)
	assert.Equal(t, "CLI Parameters", desc.DisplayName)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)

	// Check schema: neither key nor keys is required at the schema level
	// (Execute enforces that at least one is provided), so a parameter can be
	// declared with a single key or an alias list.
	assert.Contains(t, desc.Schema.Properties, "key")
	assert.NotContains(t, desc.Schema.Required, "key")
	assert.Contains(t, desc.Schema.Properties, "keys")
	assert.Contains(t, desc.Schema.Properties, "default")
	assert.NotContains(t, desc.Schema.Required, "default")
}

func TestParameterProvider_Execute_Default_UsedWhenKeyMissing(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key":     "env",
		"default": "development",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "development", output.Data)
	assert.Equal(t, false, output.Metadata["exists"])
	assert.Equal(t, "string", output.Metadata["type"])
}

func TestParameterProvider_Execute_Default_NotUsedWhenKeyPresent(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"env": "production",
	})

	output, err := p.Execute(ctx, map[string]any{
		"key":     "env",
		"default": "development",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "production", output.Data)
	assert.Equal(t, true, output.Metadata["exists"])
}

func TestParameterProvider_Execute_Default_TypedValues(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	tests := []struct {
		name         string
		defaultVal   any
		expectedData any
		expectedType string
	}{
		{"string literal", "fallback", "fallback", "string"},
		{"bool string true", "true", true, "boolean"},
		{"integer string", "42", int64(42), "integer"},
		{"csv string not auto-split", "a,b,c", "a,b,c", "string"},
		{"already-bool", true, true, "boolean"},
		{"already-int", int64(99), int64(99), "integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			output, err := p.Execute(ctx, map[string]any{
				"key":     "missing",
				"default": tt.defaultVal,
			})
			require.NoError(t, err)
			require.NotNil(t, output)
			assert.Equal(t, tt.expectedData, output.Data)
			assert.Equal(t, false, output.Metadata["exists"])
			assert.Equal(t, tt.expectedType, output.Metadata["type"])
		})
	}
}

func TestParameterProvider_Execute_NoDefault_StillErrors(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key": "env",
	})

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "not provided")
}

func TestWithHTTPClient_ExercisesOption(t *testing.T) {
	t.Parallel()
	allow := true
	cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow}}
	ctx := config.WithConfig(context.Background(), cfg)

	mock := &MockHTTPClient{Err: errors.New("mock-error")}
	p := NewParameterProvider(WithHTTPClient(mock))
	_, err := p.resolveValue(ctx, "http://127.0.0.1/data", TypeFetch)
	assert.ErrorContains(t, err, "mock-error")
}

func TestParameterProvider_Fetch_Success(t *testing.T) {
	t.Parallel()
	allow := true
	cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow}}
	ctx := config.WithConfig(context.Background(), cfg)

	mock := &MockHTTPClient{
		Response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("remote-value")),
		},
	}
	p := NewParameterProvider(WithHTTPClient(mock))
	result, err := p.resolveValue(ctx, "http://127.0.0.1/data", TypeFetch)
	require.NoError(t, err)
	assert.Equal(t, "remote-value", result)
}

func TestParameterProvider_Fetch_NonOKStatus(t *testing.T) {
	t.Parallel()
	allow := true
	cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow}}
	ctx := config.WithConfig(context.Background(), cfg)

	mock := &MockHTTPClient{
		Response: &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
		},
	}
	p := NewParameterProvider(WithHTTPClient(mock))
	_, err := p.resolveValue(ctx, "http://127.0.0.1/data", TypeFetch)
	assert.ErrorContains(t, err, "404")
}

// TestParameterProvider_Fetch_SSRFBlocked verifies that fetch blocks a
// private/loopback address when private IPs are not permitted, without issuing
// the request (the mock fails if called).
func TestParameterProvider_Fetch_SSRFBlocked(t *testing.T) {
	t.Parallel()
	deny := false
	cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &deny}}
	ctx := config.WithConfig(context.Background(), cfg)
	mock := &MockHTTPClient{Err: errors.New("network should not be called")}
	p := NewParameterProvider(WithHTTPClient(mock))

	_, err := p.resolveValue(ctx, "http://127.0.0.1/data", TypeFetch)
	require.Error(t, err)
	assert.ErrorContains(t, err, "resolver parameter URL blocked")
}

// TestParameterProvider_Auto_DoesNotFetchURL is the core regression guard for
// the breaking change: under auto, an http(s):// value is stored as a literal
// string and no network I/O occurs. The mock client is configured to fail if
// called, so a fetch would surface as an error.
func TestParameterProvider_Auto_DoesNotFetchURL(t *testing.T) {
	t.Parallel()
	mock := &MockHTTPClient{Err: errors.New("network should not be called")}
	p := NewParameterProvider(WithHTTPClient(mock))
	ctx := context.Background()

	for _, url := range []string{"http://example.com/data", "https://example.com/data"} {
		result, err := p.parseValue(ctx, url)
		require.NoError(t, err)
		assert.Equal(t, url, result)
	}
}

// TestParameterProvider_Fetch_RequiresURL verifies type: fetch rejects a
// non-URL value with a descriptive error instead of silently falling through.
func TestParameterProvider_Fetch_RequiresURL(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name  string
		value any
	}{
		{"plain string", "not-a-url"},
		{"ftp scheme", "ftp://example.com"},
		{"non-string", 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := p.resolveValue(ctx, tt.value, TypeFetch)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "requires an http:// or https:// value")
		})
	}
}

// TestParameterProvider_Execute_Fetch fetches remote content through the full
// Execute path when type: fetch is requested for a URL parameter.
func TestParameterProvider_Execute_Fetch(t *testing.T) {
	t.Parallel()
	allow := true
	cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow}}
	ctx := config.WithConfig(context.Background(), cfg)
	ctx = provider.WithParameters(ctx, map[string]any{
		"configUrl": "http://127.0.0.1/config",
	})

	mock := &MockHTTPClient{
		Response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("remote-body")),
		},
	}
	p := NewParameterProvider(WithHTTPClient(mock))
	output, err := p.Execute(ctx, map[string]any{
		"key":  "configUrl",
		"type": TypeFetch,
	})
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "remote-body", output.Data)
	assert.Equal(t, true, output.Metadata["exists"])
}

// TestParameterProvider_Execute_Fetch_DryRunNoNetwork ensures dry-run never
// performs the fetch: the mock is configured to fail if called.
func TestParameterProvider_Execute_Fetch_DryRunNoNetwork(t *testing.T) {
	t.Parallel()
	mock := &MockHTTPClient{Err: errors.New("network should not be called")}
	p := NewParameterProvider(WithHTTPClient(mock))
	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithParameters(ctx, map[string]any{
		"configUrl": "http://127.0.0.1/config",
	})

	output, err := p.Execute(ctx, map[string]any{
		"key":  "configUrl",
		"type": TypeFetch,
	})
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "[DRY-RUN] Not retrieved", output.Data)
}

func TestDefaultFileOps_ReadFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("file-content"), 0o600))

	ops := &DefaultFileOps{}
	content, err := ops.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "file-content", string(content))
}

func TestDefaultFileOps_ReadFile_Missing(t *testing.T) {
	t.Parallel()
	ops := &DefaultFileOps{}
	_, err := ops.ReadFile(filepath.Join(t.TempDir(), "nonexistent.txt"))
	assert.Error(t, err)
}

func TestDefaultHTTPClient_Get(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &DefaultHTTPClient{client: httpc.NewClient(&httpc.ClientConfig{
		Timeout:      settings.DefaultHTTPTimeout,
		RetryMax:     0,
		RetryWaitMin: settings.DefaultHTTPRetryWaitMinimum,
		RetryWaitMax: settings.DefaultHTTPRetryWaitMaximum,
	})}

	resp, err := c.Get(context.Background(), srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestParameterProvider_Execute_Default_ParseError(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	// "-" triggers a parse error in parseValue (stdin should have been resolved)
	output, err := p.Execute(ctx, map[string]any{
		"key":     "env",
		"default": "-",
	})

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to parse default")
	assert.Contains(t, err.Error(), "stdin value")
}

func TestParameterProvider_Execute_DryRun_WithDefault_IgnoresDefault(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	// dry-run is active, key is absent, default is provided
	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithParameters(ctx, map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key":     "env",
		"default": "should-not-appear",
	})

	// dry-run fires before default check — always returns the dry-run sentinel
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "[DRY-RUN] Not retrieved", output.Data)
	assert.True(t, output.Metadata["dryRun"].(bool))
}

// --- Map mode ("keys" + "as: map", or "all: true") -------------------------

func TestParameterProvider_Execute_MapMode_Keys(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"keyA": "alpha",
		"keyC": "gamma",
		"keyD": "delta", // present but not requested -> excluded
	})

	output, err := p.Execute(ctx, map[string]any{
		"keys": []any{"keyA", "keyB", "keyC"},
		"as":   "map",
	})

	require.NoError(t, err)
	require.NotNil(t, output)

	result, ok := output.Data.(map[string]any)
	require.True(t, ok, "map mode returns a bare map")
	// Present keys are returned; absent keyB is omitted (not defaulted).
	assert.Equal(t, map[string]any{"keyA": "alpha", "keyC": "gamma"}, result)
	_, hasB := result["keyB"]
	assert.False(t, hasB, "absent keys must be omitted so has() stays faithful")
	// keyD is present in params but was not requested -> excluded.
	_, hasD := result["keyD"]
	assert.False(t, hasD, "unrequested params must not leak into the map")

	assert.Equal(t, "map", output.Metadata["mode"])
	assert.Equal(t, []string{"keyA", "keyC"}, output.Metadata["keys"])
	assert.Equal(t, []string{"keyB"}, output.Metadata["missing"])
}

func TestParameterProvider_Execute_MapMode_Keys_StringSlice(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{"a": "1"})

	output, err := p.Execute(ctx, map[string]any{
		"keys": []string{"a", "b"},
		"as":   "map",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, int64(1), result["a"], "values use the provider's auto inference")
	assert.Equal(t, []string{"b"}, output.Metadata["missing"])
}

func TestParameterProvider_Execute_MapMode_Keys_AutoInference(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"flag":  "true",
		"count": "3",
		"name":  "svc",
	})

	output, err := p.Execute(ctx, map[string]any{
		"keys": []any{"flag", "count", "name"},
		"as":   "map",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["flag"])
	assert.Equal(t, int64(3), result["count"])
	assert.Equal(t, "svc", result["name"])
}

func TestParameterProvider_Execute_MapMode_Keys_EmptyList(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{"a": "1"})

	output, err := p.Execute(ctx, map[string]any{
		"keys": []any{},
		"as":   "map",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Empty(t, result)
	assert.Equal(t, []string{}, output.Metadata["keys"])
	assert.Equal(t, []string{}, output.Metadata["missing"])
}

func TestParameterProvider_Execute_MapMode_Keys_Duplicates(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{"a": "1"})

	output, err := p.Execute(ctx, map[string]any{
		// "a" (present) and "b" (absent) are each repeated; the distinct-set
		// semantics must collapse them in both the map and the metadata.
		"keys": []any{"a", "b", "a", "b"},
		"as":   "map",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	result, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"a": int64(1)}, result)
	// Duplicates are removed (first-seen order preserved) from metadata too.
	assert.Equal(t, []string{"a"}, output.Metadata["keys"])
	assert.Equal(t, []string{"b"}, output.Metadata["missing"])
}

func TestParameterProvider_Execute_MapMode_All(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"flag":  "true",
		"count": "3",
		"name":  "svc",
	})

	output, err := p.Execute(ctx, map[string]any{"all": true})

	require.NoError(t, err)
	require.NotNil(t, output)
	result, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"flag": true, "count": int64(3), "name": "svc"}, result)
	assert.Equal(t, "map", output.Metadata["mode"])
	// present keys are sorted; no "missing" key in all mode.
	assert.Equal(t, []string{"count", "flag", "name"}, output.Metadata["keys"])
	_, hasMissing := output.Metadata["missing"]
	assert.False(t, hasMissing, "all mode has no notion of missing keys")
}

func TestParameterProvider_Execute_MapMode_All_Empty(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{"all": true})

	require.NoError(t, err)
	require.NotNil(t, output)
	result, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Empty(t, result)
	assert.Equal(t, []string{}, output.Metadata["keys"])
}

func TestParameterProvider_Execute_MapMode_AllFalseIsInert(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{"env": "prod"})

	// all: false must not select map mode; the scalar key read wins.
	output, err := p.Execute(ctx, map[string]any{"key": "env", "all": false})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "prod", output.Data)
	assert.Equal(t, true, output.Metadata["exists"])
}

func TestParameterProvider_Execute_MapMode_DryRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		inputs map[string]any
	}{
		{"keys as map", map[string]any{"keys": []any{"a", "b"}, "as": "map"}},
		{"all", map[string]any{"all": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewParameterProvider()
			ctx := provider.WithDryRun(provider.WithParameters(context.Background(), map[string]any{
				"a": "1",
				"b": "2",
			}), true)

			output, err := p.Execute(ctx, tt.inputs)

			require.NoError(t, err)
			require.NotNil(t, output)
			// Dry-run returns an empty placeholder map (absent keys are omitted).
			assert.Equal(t, map[string]any{}, output.Data)
			assert.Equal(t, true, output.Metadata["dryRun"])
			assert.Equal(t, "map", output.Metadata["mode"])
		})
	}
}

func TestParameterProvider_Execute_MapMode_MutualExclusionAndValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		inputs  map[string]any
		wantErr string
	}{
		{
			name:    "as map without keys",
			inputs:  map[string]any{"as": "map"},
			wantErr: `"as: map" requires "keys"`,
		},
		{
			name:    "all with key",
			inputs:  map[string]any{"all": true, "key": "env"},
			wantErr: `"all" is mutually exclusive with "key" and "keys"`,
		},
		{
			name:    "all with keys",
			inputs:  map[string]any{"all": true, "keys": []any{"a"}},
			wantErr: `"all" is mutually exclusive with "key" and "keys"`,
		},
		{
			name:    "all with as map",
			inputs:  map[string]any{"all": true, "as": "map"},
			wantErr: `"all" is mutually exclusive with "as"`,
		},
		{
			name:    "as map with key",
			inputs:  map[string]any{"key": "env", "keys": []any{"a"}, "as": "map"},
			wantErr: `"as: map" is mutually exclusive with "key"`,
		},
		{
			name:    "unsupported as value",
			inputs:  map[string]any{"keys": []any{"a"}, "as": "list"},
			wantErr: `unsupported as "list"`,
		},
		{
			name:    "as not a string",
			inputs:  map[string]any{"keys": []any{"a"}, "as": 42},
			wantErr: "as must be a string",
		},
		{
			name:    "all not a bool",
			inputs:  map[string]any{"all": "yes"},
			wantErr: "all must be a boolean",
		},
		{
			name:    "default rejected in keys map mode",
			inputs:  map[string]any{"keys": []any{"a"}, "as": "map", "default": "x"},
			wantErr: `"default" is only valid with a single "key"/alias "keys"`,
		},
		{
			name:    "default rejected in all mode",
			inputs:  map[string]any{"all": true, "default": "x"},
			wantErr: `"default" is only valid with a single "key"/alias "keys"`,
		},
		{
			name:    "type rejected in keys map mode",
			inputs:  map[string]any{"keys": []any{"a"}, "as": "map", "type": "string"},
			wantErr: `"type" is only valid with a single "key"/alias "keys"`,
		},
		{
			name:    "type rejected in all mode",
			inputs:  map[string]any{"all": true, "type": "string"},
			wantErr: `"type" is only valid with a single "key"/alias "keys"`,
		},
		{
			name:    "invalid key pattern in map mode",
			inputs:  map[string]any{"keys": []any{"ok", "_.bad expr"}, "as": "map"},
			wantErr: "invalid key",
		},
		{
			name:    "non-string key element in map mode",
			inputs:  map[string]any{"keys": []any{"ok", 42}, "as": "map"},
			wantErr: "keys[1] must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewParameterProvider()
			ctx := provider.WithParameters(context.Background(), map[string]any{"a": "1"})
			output, err := p.Execute(ctx, tt.inputs)
			require.Error(t, err)
			assert.Nil(t, output)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestToStringKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     any
		want    []string
		wantErr string
	}{
		{"string slice", []string{"a", "b"}, []string{"a", "b"}, ""},
		{"any slice of strings", []any{"a", "b"}, []string{"a", "b"}, ""},
		{"empty any slice", []any{}, []string{}, ""},
		{"nil", nil, nil, `"keys" must be an array of strings`},
		{"wrong type", "a", nil, `"keys" must be an array of strings, got string`},
		{"non-string element", []any{"a", 1}, nil, "keys[1] must be a string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := toStringKeys(tt.raw)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
