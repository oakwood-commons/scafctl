// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package yaml

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToYaml_SimpleValues(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    string
		wantErr string
	}{
		{
			name:  "simple map",
			input: map[string]any{"name": "myapp"},
			want:  "name: myapp",
		},
		{
			name:  "boolean value",
			input: map[string]any{"enabled": true},
			want:  "enabled: true",
		},
		{
			name:  "nil input",
			input: nil,
			want:  "",
		},
		{
			name:  "string value",
			input: map[string]any{"msg": "hello world"},
			want:  "msg: hello world",
		},
		{
			name:  "integer value",
			input: map[string]any{"count": 42},
			want:  "count: 42",
		},
		{
			name:  "float value",
			input: map[string]any{"ratio": 3.14},
			want:  "ratio: 3.14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ToYaml(tt.input)
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

func TestToYaml_NestedMap(t *testing.T) {
	input := map[string]any{
		"server": map[string]any{
			"host": "localhost",
			"port": 443,
		},
	}

	result, err := ToYaml(input)
	require.NoError(t, err)
	assert.Contains(t, result, "server:")
	assert.Contains(t, result, "host: localhost")
	assert.Contains(t, result, "port: 443")
}

func TestToYaml_List(t *testing.T) {
	input := map[string]any{
		"tags": []string{"web", "production"},
	}

	result, err := ToYaml(input)
	require.NoError(t, err)
	assert.Contains(t, result, "tags:")
	assert.Contains(t, result, "- web")
	assert.Contains(t, result, "- production")
}

func TestToYaml_EmptyMap(t *testing.T) {
	input := map[string]any{}
	result, err := ToYaml(input)
	require.NoError(t, err)
	assert.Equal(t, "{}", result)
}

func TestToYaml_Struct(t *testing.T) {
	type Config struct {
		Name string `yaml:"name"`
		Port int    `yaml:"port"`
	}

	input := Config{Name: "svc", Port: 8080}
	result, err := ToYaml(input)
	require.NoError(t, err)
	assert.Contains(t, result, "name: svc")
	assert.Contains(t, result, "port: 8080")
}

func TestToYaml_NoTrailingNewline(t *testing.T) {
	input := map[string]any{"key": "value"}
	result, err := ToYaml(input)
	require.NoError(t, err)
	assert.False(t, len(result) > 0 && result[len(result)-1] == '\n',
		"result should not end with a newline")
}

func TestFromYaml_SimpleMap(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantKey string
		wantVal any
		wantErr string
	}{
		{
			name:    "simple key-value",
			input:   "name: myapp",
			wantKey: "name",
			wantVal: "myapp",
		},
		{
			name:    "integer value",
			input:   "port: 8080",
			wantKey: "port",
			wantVal: 8080,
		},
		{
			name:    "boolean value",
			input:   "enabled: true",
			wantKey: "enabled",
			wantVal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FromYaml(tt.input)
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantVal, result[tt.wantKey])
		})
	}
}

func TestFromYaml_NestedMap(t *testing.T) {
	yamlInput := "server:\n  host: localhost\n  port: 443"
	result, err := FromYaml(yamlInput)
	require.NoError(t, err)

	server, ok := result["server"].(map[string]any)
	require.True(t, ok, "server should be a map")
	assert.Equal(t, "localhost", server["host"])
	assert.Equal(t, 443, server["port"])
}

func TestFromYaml_List(t *testing.T) {
	yamlInput := "tags:\n  - web\n  - production"
	result, err := FromYaml(yamlInput)
	require.NoError(t, err)

	tags, ok := result["tags"].([]any)
	require.True(t, ok, "tags should be a list")
	assert.Equal(t, []any{"web", "production"}, tags)
}

func TestFromYaml_EmptyInput(t *testing.T) {
	result, err := FromYaml("")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)

	result2, err2 := FromYaml("   \n  ")
	require.NoError(t, err2)
	assert.Equal(t, map[string]any{}, result2)
}

func TestFromYaml_InvalidYaml(t *testing.T) {
	_, err := FromYaml("invalid: [yaml: {{")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fromYaml")
}

func TestRoundTrip(t *testing.T) {
	original := map[string]any{
		"name": "myapp",
		"port": 8080,
		"tags": []any{"web", "prod"},
	}

	yamlStr, err := ToYaml(original)
	require.NoError(t, err)

	decoded, err := FromYaml(yamlStr)
	require.NoError(t, err)

	assert.Equal(t, "myapp", decoded["name"])
	assert.Equal(t, 8080, decoded["port"])
}

func TestToYamlFunc_Metadata(t *testing.T) {
	fn := ToYamlFunc()
	assert.Equal(t, "toYaml", fn.Name)
	assert.True(t, fn.Custom)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.NotEmpty(t, fn.Links)
	assert.Contains(t, fn.Func, "toYaml")
}

func TestFromYamlFunc_Metadata(t *testing.T) {
	fn := FromYamlFunc()
	assert.Equal(t, "fromYaml", fn.Name)
	assert.True(t, fn.Custom)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.NotEmpty(t, fn.Links)
	assert.Contains(t, fn.Func, "fromYaml")
}

func TestMustToYamlFunc_Metadata(t *testing.T) {
	fn := MustToYamlFunc()
	assert.Equal(t, "mustToYaml", fn.Name)
	assert.True(t, fn.Custom)
	assert.NotEmpty(t, fn.Description)
	assert.Contains(t, fn.Func, "mustToYaml")
}

func TestMustFromYamlFunc_Metadata(t *testing.T) {
	fn := MustFromYamlFunc()
	assert.Equal(t, "mustFromYaml", fn.Name)
	assert.True(t, fn.Custom)
	assert.NotEmpty(t, fn.Description)
	assert.Contains(t, fn.Func, "mustFromYaml")
}

func TestMustToYaml_SameBehavior(t *testing.T) {
	input := map[string]any{"key": "value"}

	toResult, toErr := ToYaml(input)
	require.NoError(t, toErr)

	mustFn := MustToYamlFunc()
	mustFunc := mustFn.Func["mustToYaml"]
	mustResult, mustErr := mustFunc.(func(any) (string, error))(input)
	require.NoError(t, mustErr)

	assert.Equal(t, toResult, mustResult)
}

func TestMustFromYaml_SameBehavior(t *testing.T) {
	fromResult, fromErr := FromYaml("name: myapp")
	require.NoError(t, fromErr)

	mustFn := MustFromYamlFunc()
	mustFunc := mustFn.Func["mustFromYaml"]
	mustResult, mustErr := mustFunc.(func(string) (map[string]any, error))("name: myapp")
	require.NoError(t, mustErr)

	assert.Equal(t, fromResult, mustResult)
}
