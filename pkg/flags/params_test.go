// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
				"key2": float64(123),
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

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, tt.filename)
			err := os.WriteFile(filePath, []byte(tt.content), 0o600)
			require.NoError(t, err)

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

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "params.yaml")
	content := "fileKey: fileValue\n"
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err)

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

func TestLoadParameterReader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected map[string]any
		wantErr  string
	}{
		{
			name:  "valid yaml",
			input: "key1: value1\nkey2: 123\n",
			expected: map[string]any{
				"key1": "value1",
				"key2": 123,
			},
		},
		{
			name:  "valid json",
			input: `{"key1": "value1", "key2": 123}`,
			expected: map[string]any{
				"key1": "value1",
				"key2": 123, // YAML parser is tried first and parses ints as int
			},
		},
		{
			name:  "nested yaml",
			input: "nested:\n  key: value\n",
			expected: map[string]any{
				"nested": map[string]any{
					"key": "value",
				},
			},
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: "no data received from stdin",
		},
		{
			name:    "whitespace-only input",
			input:   "\n  \n\t\n",
			wantErr: "no data received from stdin",
		},
		{
			name:    "invalid data",
			input:   "{{{{invalid",
			wantErr: "failed to parse stdin as YAML or JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := strings.NewReader(tt.input)
			result, err := LoadParameterReader(r, "stdin")

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseResolverFlagsWithStdin(t *testing.T) {
	t.Parallel()

	t.Run("reads parameters from stdin via @-", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("greeting: hello\nname: world\n")
		result, err := ParseResolverFlagsWithStdin([]string{"@-"}, stdin)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"greeting": "hello",
			"name":     "world",
		}, result)
	})

	t.Run("stdin JSON", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader(`{"env": "prod", "count": 42}`)
		result, err := ParseResolverFlagsWithStdin([]string{"@-"}, stdin)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"env":   "prod",
			"count": 42, // YAML parser is tried first
		}, result)
	})

	t.Run("mixes @- with key=value", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("fromStdin: yes\n")
		result, err := ParseResolverFlagsWithStdin([]string{"cli=value", "@-"}, stdin)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"cli":       "value",
			"fromStdin": "yes",
		}, result)
	})

	t.Run("mixes @- with @file", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "params.yaml")
		err := os.WriteFile(filePath, []byte("fileKey: fileValue\n"), 0o600)
		require.NoError(t, err)

		stdin := strings.NewReader("stdinKey: stdinValue\n")
		result, err := ParseResolverFlagsWithStdin([]string{"@" + filePath, "@-"}, stdin)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"fileKey":  "fileValue",
			"stdinKey": "stdinValue",
		}, result)
	})

	t.Run("errors on duplicate @-", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("key: value\n")
		_, err := ParseResolverFlagsWithStdin([]string{"@-", "@-"}, stdin)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "@- can only be specified once")
	})

	t.Run("errors when stdin is nil", func(t *testing.T) {
		t.Parallel()

		_, err := ParseResolverFlagsWithStdin([]string{"@-"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "@- requires stdin but no stdin is available")
	})

	t.Run("no @- passes through to ParseResolverFlags behavior", func(t *testing.T) {
		t.Parallel()

		result, err := ParseResolverFlagsWithStdin([]string{"key=value"}, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"key": "value"}, result)
	})

	t.Run("stdin merges with overlapping keys from CLI", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("key: fromStdin\n")
		result, err := ParseResolverFlagsWithStdin([]string{"key=fromCLI", "@-"}, stdin)
		require.NoError(t, err)
		// Both values merged into array since key appears twice
		assert.Equal(t, map[string]any{
			"key": []any{"fromCLI", "fromStdin"},
		}, result)
	})

	t.Run("key=@- reads raw stdin as value", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("hello world\n")
		result, err := ParseResolverFlagsWithStdin([]string{"message=@-"}, stdin)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"message": "hello world",
		}, result)
	})

	t.Run("key=@- trims single trailing newline", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("line1\nline2\n")
		result, err := ParseResolverFlagsWithStdin([]string{"content=@-"}, stdin)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"content": "line1\nline2",
		}, result)
	})

	t.Run("key=@- with other key=value params", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("piped data\n")
		result, err := ParseResolverFlagsWithStdin([]string{"name=test", "body=@-"}, stdin)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"name": "test",
			"body": "piped data",
		}, result)
	})

	t.Run("key=@- errors on nil stdin", func(t *testing.T) {
		t.Parallel()

		_, err := ParseResolverFlagsWithStdin([]string{"msg=@-"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "msg=@- requires stdin but no stdin is available")
	})

	t.Run("key=@- errors on empty stdin", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("")
		_, err := ParseResolverFlagsWithStdin([]string{"msg=@-"}, stdin)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no data received from stdin")
	})

	t.Run("key=@- conflicts with standalone @-", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("data\n")
		_, err := ParseResolverFlagsWithStdin([]string{"msg=@-", "@-"}, stdin)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stdin can only be read once")
	})

	t.Run("standalone @- then key=@- conflicts", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("key: value\n")
		_, err := ParseResolverFlagsWithStdin([]string{"@-", "msg=@-"}, stdin)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stdin has already been consumed")
	})

	t.Run("key=@file reads raw file content", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "content.txt")
		err := os.WriteFile(filePath, []byte("file content here\n"), 0o600)
		require.NoError(t, err)

		result, err := ParseResolverFlagsWithStdin([]string{"data=@" + filePath}, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"data": "file content here",
		}, result)
	})

	t.Run("key=@file errors on missing file", func(t *testing.T) {
		t.Parallel()

		_, err := ParseResolverFlagsWithStdin([]string{"data=@/nonexistent/file.txt"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read file")
	})

	t.Run("key=@file with key=@- together", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "content.txt")
		err := os.WriteFile(filePath, []byte("from file\n"), 0o600)
		require.NoError(t, err)

		stdin := strings.NewReader("from stdin\n")
		result, err := ParseResolverFlagsWithStdin([]string{"file=@" + filePath, "pipe=@-"}, stdin)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"file": "from file",
			"pipe": "from stdin",
		}, result)
	})

	t.Run("key=@- whitespace-only stdin errors", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("   \n  \n")
		_, err := ParseResolverFlagsWithStdin([]string{"msg=@-"}, stdin)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no data received from stdin")
	})

	t.Run("key=@- trims \\r\\n line ending", func(t *testing.T) {
		t.Parallel()

		stdin := strings.NewReader("windows line\r\n")
		result, err := ParseResolverFlagsWithStdin([]string{"msg=@-"}, stdin)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"msg": "windows line",
		}, result)
	})

	t.Run("key=@file errors on empty file", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "empty.txt")
		err := os.WriteFile(filePath, []byte(""), 0o600)
		require.NoError(t, err)

		_, err = ParseResolverFlagsWithStdin([]string{"data=@" + filePath}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no data received from file")
	})

	t.Run("key=@file trims \\r\\n line ending", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "crlf.txt")
		err := os.WriteFile(filePath, []byte("windows content\r\n"), 0o600)
		require.NoError(t, err)

		result, err := ParseResolverFlagsWithStdin([]string{"data=@" + filePath}, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"data": "windows content",
		}, result)
	})

	t.Run("key=@file with standalone @params.yaml mixed", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		rawFile := filepath.Join(tmpDir, "raw.txt")
		err := os.WriteFile(rawFile, []byte("raw content\n"), 0o600)
		require.NoError(t, err)

		paramsFile := filepath.Join(tmpDir, "params.yaml")
		err = os.WriteFile(paramsFile, []byte("env: prod\n"), 0o600)
		require.NoError(t, err)

		result, err := ParseResolverFlagsWithStdin([]string{"body=@" + rawFile, "@" + paramsFile}, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"body": "raw content",
			"env":  "prod",
		}, result)
	})

	t.Run("key=@- exceeds max raw read size", func(t *testing.T) {
		t.Parallel()

		// Create a reader just over the 1 MiB limit
		bigData := make([]byte, maxRawReadSize+1)
		for i := range bigData {
			bigData[i] = 'x'
		}
		stdin := bytes.NewReader(bigData)
		_, err := ParseResolverFlagsWithStdin([]string{"data=@-"}, stdin)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum raw read size")
	})

	t.Run("key=@file exceeds max raw read size", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		bigFile := filepath.Join(tmpDir, "big.txt")
		bigData := make([]byte, maxRawReadSize+1)
		for i := range bigData {
			bigData[i] = 'y'
		}
		err := os.WriteFile(bigFile, bigData, 0o600)
		require.NoError(t, err)

		_, err = ParseResolverFlagsWithStdin([]string{"data=@" + bigFile}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum raw read size")
	})
}

func TestParseValueRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input  string
		key    string
		ref    string
		wantOK bool
	}{
		{"key=@-", "key", "-", true},
		{"msg=@/path/to/file.txt", "msg", "/path/to/file.txt", true},
		{"a==@-", "a", "=@-", false},  // value is =@- which starts with = not @
		{"key=value", "", "", false},  // no @ in value
		{"key=@", "", "", false},      // @ alone — too short (need at least @X)
		{"@-", "", "", false},         // standalone, no =
		{"@file.yaml", "", "", false}, // standalone file ref
		{"noequals", "", "", false},   // no = sign
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			key, ref, ok := parseValueRef(tt.input)
			assert.Equal(t, tt.wantOK, ok, "ok mismatch")
			if ok {
				assert.Equal(t, tt.key, key)
				assert.Equal(t, tt.ref, ref)
			}
		})
	}
}

func TestContainsStdinRef(t *testing.T) {
	t.Parallel()

	assert.True(t, ContainsStdinRef([]string{"key=value", "@-"}))
	assert.True(t, ContainsStdinRef([]string{"@-"}))
	assert.True(t, ContainsStdinRef([]string{"msg=@-"}))
	assert.True(t, ContainsStdinRef([]string{"key=value", "body=@-"}))
	assert.False(t, ContainsStdinRef([]string{"key=value"}))
	assert.False(t, ContainsStdinRef([]string{"@file.yaml"}))
	assert.False(t, ContainsStdinRef([]string{"key=@file.txt"}))
	assert.False(t, ContainsStdinRef([]string{}))
	assert.False(t, ContainsStdinRef(nil))
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

			result := MergeValue(tt.existing, tt.newVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkLoadParameterFile(b *testing.B) {
	dir := b.TempDir()
	p := filepath.Join(dir, "bench.yaml")
	_ = os.WriteFile(p, []byte("key1: value1\nkey2: 123\nnested:\n  a: b\n"), 0o600)

	b.ResetTimer()
	for b.Loop() {
		_, _ = LoadParameterFile(p)
	}
}

func BenchmarkParseResolverFlags(b *testing.B) {
	dir := b.TempDir()
	p := filepath.Join(dir, "params.yaml")
	_ = os.WriteFile(p, []byte("fileKey: fileValue\n"), 0o600)
	values := []string{"key1=value1", "key2=value2", "@" + p}

	b.ResetTimer()
	for b.Loop() {
		_, _ = ParseResolverFlags(values)
	}
}

func BenchmarkMergeValue(b *testing.B) {
	existing := []any{"value1", "value2"}
	newVal := []any{"value3", "value4"}

	for b.Loop() {
		_ = MergeValue(existing, newVal)
	}
}

func TestParseDynamicInputArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		expected []string
		wantErr  string
	}{
		{
			name:     "empty args",
			args:     []string{},
			expected: []string{},
		},
		{
			name:     "double-dash key=value stripped",
			args:     []string{"--url=https://example.com", "--method=GET"},
			expected: []string{"url=https://example.com", "method=GET"},
		},
		{
			name:     "positional key=value passed through",
			args:     []string{"url=https://example.com", "method=GET"},
			expected: []string{"url=https://example.com", "method=GET"},
		},
		{
			name:     "file reference passed through",
			args:     []string{"@inputs.yaml"},
			expected: []string{"@inputs.yaml"},
		},
		{
			name:     "mixed forms",
			args:     []string{"--url=https://example.com", "method=GET", "@extra.yaml"},
			expected: []string{"url=https://example.com", "method=GET", "@extra.yaml"},
		},
		{
			name:    "bare double-dash flag rejected",
			args:    []string{"--verbose"},
			wantErr: "must use --key=value syntax",
		},
		{
			name:    "single-dash flag rejected",
			args:    []string{"-k=v"},
			wantErr: "single-dash flag",
		},
		{
			name:    "single-dash bare flag rejected",
			args:    []string{"-v"},
			wantErr: "single-dash flag",
		},
		{
			name:    "bare word rejected",
			args:    []string{"something"},
			wantErr: "unexpected argument",
		},
		{
			name:     "value with equals sign preserved",
			args:     []string{"--expr=a==b"},
			expected: []string{"expr=a==b"},
		},
		{
			name:     "key=@- passed through",
			args:     []string{"msg=@-"},
			expected: []string{"msg=@-"},
		},
		{
			name:     "double-dash key=@file stripped and passed through",
			args:     []string{"--body=@/tmp/data.txt"},
			expected: []string{"body=@/tmp/data.txt"},
		},
		{
			name:     "@- stdin ref passed through",
			args:     []string{"@-"},
			expected: []string{"@-"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ParseDynamicInputArgs(tt.args)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkParseDynamicInputArgs(b *testing.B) {
	args := []string{"--url=https://example.com", "--method=GET", "timeout=30", "@extra.yaml"}

	b.ResetTimer()
	for b.Loop() {
		_, _ = ParseDynamicInputArgs(args)
	}
}

func BenchmarkLoadParameterReader(b *testing.B) {
	data := "key1: value1\nkey2: 123\nnested:\n  a: b\n"
	for b.Loop() {
		r := strings.NewReader(data)
		_, _ = LoadParameterReader(r, "stdin")
	}
}

func BenchmarkParseResolverFlagsWithStdin(b *testing.B) {
	data := []byte("stdinKey: stdinValue\nother: 42\n")
	for b.Loop() {
		r := bytes.NewReader(data)
		_, _ = ParseResolverFlagsWithStdin([]string{"key=value", "@-"}, r)
	}
}

func BenchmarkParseResolverFlagsWithStdin_ValueRef(b *testing.B) {
	data := []byte("hello world\n")
	for b.Loop() {
		r := bytes.NewReader(data)
		_, _ = ParseResolverFlagsWithStdin([]string{"key=value", "msg=@-"}, r)
	}
}

func BenchmarkParseValueRef(b *testing.B) {
	for b.Loop() {
		parseValueRef("message=@-")
	}
}

func BenchmarkContainsStdinRef(b *testing.B) {
	values := []string{"key1=value1", "key2=value2", "@file.yaml", "@-"}
	for b.Loop() {
		_ = ContainsStdinRef(values)
	}
}

func BenchmarkContainsStdinRef_ValueRef(b *testing.B) {
	values := []string{"key1=value1", "key2=value2", "msg=@-"}
	for b.Loop() {
		_ = ContainsStdinRef(values)
	}
}
