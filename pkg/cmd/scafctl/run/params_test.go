// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadParameterFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
		content  string
		expected map[string]any
		wantErr  bool
	}{
		{
			name:     "yaml file with .yaml extension",
			filename: "params.yaml",
			content:  "key1: value1\nkey2: 123\n",
			expected: map[string]any{
				"key1": "value1",
				"key2": 123,
			},
		},
		{
			name:     "yaml file with .yml extension",
			filename: "params.yml",
			content:  "nested:\n  key: value\n",
			expected: map[string]any{
				"nested": map[string]any{
					"key": "value",
				},
			},
		},
		{
			name:     "json file",
			filename: "params.json",
			content:  `{"key1": "value1", "key2": 123}`,
			expected: map[string]any{
				"key1": "value1",
				"key2": float64(123), // JSON unmarshals numbers as float64
			},
		},
		{
			name:     "unknown extension with valid yaml",
			filename: "params.txt",
			content:  "key: value\n",
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:     "unknown extension with valid json",
			filename: "params.txt",
			content:  `{"key": "value"}`,
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:     "invalid yaml file",
			filename: "invalid.yaml",
			content:  "key: [invalid",
			wantErr:  true,
		},
		{
			name:     "invalid json file",
			filename: "invalid.json",
			content:  `{"key": invalid}`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create temp file
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, tt.filename)
			err := os.WriteFile(filePath, []byte(tt.content), 0o600)
			require.NoError(t, err)

			// Load file
			result, err := LoadParameterFile(filePath)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoadParameterFile_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadParameterFile("/nonexistent/path/to/file.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read parameter file")
}

func TestParseResolverFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		values   []string
		expected map[string]any
		wantErr  bool
	}{
		{
			name:   "simple key=value",
			values: []string{"key=value"},
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:   "multiple key=value pairs",
			values: []string{"key1=value1", "key2=value2"},
			expected: map[string]any{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name:   "key with multiple values",
			values: []string{"key=value1", "key=value2"},
			expected: map[string]any{
				"key": []any{"value1", "value2"},
			},
		},
		{
			name:     "empty values slice",
			values:   []string{},
			expected: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ParseResolverFlags(tt.values)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseResolverFlags_WithFileRef(t *testing.T) {
	t.Parallel()

	// Create temp file with params
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "params.yaml")
	content := "fileKey: fileValue\n"
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err)

	// Parse with file reference
	result, err := ParseResolverFlags([]string{"@" + filePath, "cliKey=cliValue"})
	require.NoError(t, err)

	expected := map[string]any{
		"fileKey": "fileValue",
		"cliKey":  "cliValue",
	}
	assert.Equal(t, expected, result)
}

func TestParseResolverFlags_FileRefError(t *testing.T) {
	t.Parallel()

	_, err := ParseResolverFlags([]string{"@/nonexistent/file.yaml"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read parameter file")
}

func TestMergeValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing any
		newVal   any
		expected any
	}{
		{
			name:     "nil existing",
			existing: nil,
			newVal:   "value",
			expected: "value",
		},
		{
			name:     "scalar to scalar creates array",
			existing: "value1",
			newVal:   "value2",
			expected: []any{"value1", "value2"},
		},
		{
			name:     "slice appends scalar",
			existing: []any{"value1"},
			newVal:   "value2",
			expected: []any{"value1", "value2"},
		},
		{
			name:     "slice appends slice",
			existing: []any{"value1"},
			newVal:   []any{"value2", "value3"},
			expected: []any{"value1", "value2", "value3"},
		},
		{
			name:     "string slice converts and appends",
			existing: []string{"value1"},
			newVal:   "value2",
			expected: []any{"value1", "value2"},
		},
		{
			name:     "string slice appends string slice",
			existing: []string{"value1"},
			newVal:   []string{"value2", "value3"},
			expected: []any{"value1", "value2", "value3"},
		},
		{
			name:     "string slice appends any slice",
			existing: []string{"value1"},
			newVal:   []any{"value2", 123},
			expected: []any{"value1", "value2", 123},
		},
		{
			name:     "scalar appends to any slice",
			existing: "value1",
			newVal:   []any{"value2", "value3"},
			expected: []any{"value1", "value2", "value3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := mergeValue(tt.existing, tt.newVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}
