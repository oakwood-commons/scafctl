// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hcl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToHcl_SimpleMap(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    string
		wantErr string
	}{
		{
			name:  "simple string attribute",
			input: map[string]any{"key": "value"},
			want:  "key = \"value\"\n",
		},
		{
			name:  "multiple attributes sorted",
			input: map[string]any{"name": "myapp", "region": "us-east-1"},
			want:  "name = \"myapp\"\nregion = \"us-east-1\"\n",
		},
		{
			name:  "integer attribute",
			input: map[string]any{"port": float64(8080)},
			want:  "port = 8080\n",
		},
		{
			name:  "boolean attribute",
			input: map[string]any{"enabled": true},
			want:  "enabled = true\n",
		},
		{
			name:  "float attribute",
			input: map[string]any{"ratio": 3.14},
			want:  "ratio = 3.14\n",
		},
		{
			name:  "null attribute",
			input: map[string]any{"value": nil},
			want:  "value = null\n",
		},
		{
			name:  "nil input",
			input: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ToHcl(tt.input)
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestToHcl_NestedMap(t *testing.T) {
	input := map[string]any{
		"server": map[string]any{
			"host": "localhost",
			"port": float64(443),
		},
	}

	result, err := ToHcl(input)
	require.NoError(t, err)
	expected := "server {\n  host = \"localhost\"\n  port = 443\n}\n"
	assert.Equal(t, expected, result)
}

func TestToHcl_DeeplyNested(t *testing.T) {
	input := map[string]any{
		"database": map[string]any{
			"primary": map[string]any{
				"host": "db.example.com",
				"port": float64(5432),
			},
		},
	}

	result, err := ToHcl(input)
	require.NoError(t, err)
	expected := "database {\n  primary {\n    host = \"db.example.com\"\n    port = 5432\n  }\n}\n"
	assert.Equal(t, expected, result)
}

func TestToHcl_ListOfPrimitives(t *testing.T) {
	input := map[string]any{
		"tags": []any{"web", "production", "v2"},
	}

	result, err := ToHcl(input)
	require.NoError(t, err)
	expected := "tags = [\"web\", \"production\", \"v2\"]\n"
	assert.Equal(t, expected, result)
}

func TestToHcl_ListOfNumbers(t *testing.T) {
	input := map[string]any{
		"ports": []any{float64(80), float64(443), float64(8080)},
	}

	result, err := ToHcl(input)
	require.NoError(t, err)
	expected := "ports = [80, 443, 8080]\n"
	assert.Equal(t, expected, result)
}

func TestToHcl_ListOfMaps(t *testing.T) {
	input := map[string]any{
		"ingress": []any{
			map[string]any{"from_port": float64(80), "protocol": "tcp"},
			map[string]any{"from_port": float64(443), "protocol": "tcp"},
		},
	}

	result, err := ToHcl(input)
	require.NoError(t, err)
	assert.Contains(t, result, "ingress {\n")
	assert.Contains(t, result, "from_port = 80")
	assert.Contains(t, result, "from_port = 443")
	assert.Contains(t, result, "protocol = \"tcp\"")
}

func TestToHcl_Struct(t *testing.T) {
	type Config struct {
		Name    string `json:"name"`
		Port    int    `json:"port"`
		Enabled bool   `json:"enabled"`
	}

	input := Config{
		Name:    "myservice",
		Port:    8080,
		Enabled: true,
	}

	result, err := ToHcl(input)
	require.NoError(t, err)
	assert.Contains(t, result, "name = \"myservice\"")
	assert.Contains(t, result, "port = 8080")
	assert.Contains(t, result, "enabled = true")
}

func TestToHcl_NestedStruct(t *testing.T) {
	type Server struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	type Config struct {
		Server Server `json:"server"`
	}

	input := Config{
		Server: Server{
			Host: "localhost",
			Port: 443,
		},
	}

	result, err := ToHcl(input)
	require.NoError(t, err)
	assert.Contains(t, result, "server {")
	assert.Contains(t, result, "host = \"localhost\"")
	assert.Contains(t, result, "port = 443")
}

func TestToHcl_EmptyMap(t *testing.T) {
	input := map[string]any{}
	result, err := ToHcl(input)
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestToHcl_TopLevelPrimitive(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"string", "hello", `"hello"`},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"integer", float64(42), "42"},
		{"float", 3.14, "3.14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ToHcl(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToHcl_ComplexExample(t *testing.T) {
	input := map[string]any{
		"ami":           "abc-123",
		"instance_type": "t2.micro",
		"network_interface": map[string]any{
			"device_index":         float64(0),
			"network_interface_id": "eni-12345",
		},
		"tags": []any{"web", "prod"},
	}

	result, err := ToHcl(input)
	require.NoError(t, err)
	assert.Contains(t, result, "ami = \"abc-123\"")
	assert.Contains(t, result, "instance_type = \"t2.micro\"")
	assert.Contains(t, result, "network_interface {")
	assert.Contains(t, result, "device_index = 0")
	assert.Contains(t, result, "network_interface_id = \"eni-12345\"")
	assert.Contains(t, result, "tags = [\"web\", \"prod\"]")
}

func TestToHclFunc_Metadata(t *testing.T) {
	fn := ToHclFunc()
	assert.Equal(t, "toHcl", fn.Name)
	assert.True(t, fn.Custom)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.NotEmpty(t, fn.Links)
	assert.Contains(t, fn.Func, "toHcl")
}

func TestToHcl_StringEscaping(t *testing.T) {
	input := map[string]any{
		"message": "hello \"world\"",
	}

	result, err := ToHcl(input)
	require.NoError(t, err)
	assert.Contains(t, result, "message = \"hello \\\"world\\\"\"")
}

func TestFormatHclValue_AllKinds(t *testing.T) {
	tests := []struct {
		value    any
		expected string
	}{
		{nil, "null"},
		{"hello", `"hello"`},
		{true, "true"},
		{false, "false"},
		{float64(3.14), "3.14"},
		{float64(5.0), "5"},
		{int(42), "42"},
		{int64(99), "99"},
		{uint(7), "7"},
	}
	for _, tt := range tests {
		result := formatHclValue(tt.value)
		assert.Equal(t, tt.expected, result)
	}
}

func TestFormatHclValue_DefaultKind(t *testing.T) {
	// Struct is a complex kind that falls through to the default
	type myStruct struct{ X int }
	result := formatHclValue(myStruct{X: 42})
	// Should be a quoted string representation
	assert.NotEmpty(t, result)
}

func TestIsListOfMaps_Empty(t *testing.T) {
	assert.False(t, isListOfMaps(nil))
	assert.False(t, isListOfMaps([]any{}))
}

func TestIsListOfMaps_AllMaps(t *testing.T) {
	list := []any{map[string]any{"a": 1}, map[string]any{"b": 2}}
	assert.True(t, isListOfMaps(list))
}

func TestIsListOfMaps_Mixed(t *testing.T) {
	list := []any{map[string]any{"a": 1}, "not-a-map"}
	assert.False(t, isListOfMaps(list))
}
