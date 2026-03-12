// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractResolverName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path     string
		expected string
	}{
		{"._.config.host", "config"},
		{"._.port", "port"},
		{"_.config", "config"},
		{".config.host", "config"},
		{"config", "config"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			result := ExtractResolverName(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractFromTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		expected []string
	}{
		{
			name:     "simple template",
			template: "{{ ._.config.host }}",
			expected: []string{"config"},
		},
		{
			name:     "multiple references",
			template: "{{ ._.config.host }}:{{ ._.port.number }}",
			expected: []string{"config", "port"},
		},
		{
			name:     "duplicate references",
			template: "{{ ._.config.host }} and {{ ._.config.port }}",
			expected: []string{"config"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			refs, err := ExtractFromTemplate(tt.template, "{{", "}}")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, refs)
		})
	}
}

func TestExtractFromCEL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expr     string
		expected []string
	}{
		{
			name:     "simple expression",
			expr:     "_.config.host",
			expected: []string{"config"},
		},
		{
			name:     "multiple references",
			expr:     `_.config.host + ":" + string(_.port.value)`,
			expected: []string{"config", "port"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			refs, err := ExtractFromCEL(context.Background(), tt.expr)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, refs)
		})
	}
}

func TestExtractFromTemplateFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	content := "Hello {{ ._.name.first }} {{ ._.config.value }}"
	templatePath := filepath.Join(tempDir, "test.tmpl")
	err := os.WriteFile(templatePath, []byte(content), 0o644)
	require.NoError(t, err)

	refs, err := ExtractFromTemplateFile(templatePath, "{{", "}}")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"name", "config"}, refs)
}

func TestExtractFromTemplateFile_NotFound(t *testing.T) {
	t.Parallel()

	_, err := ExtractFromTemplateFile("/nonexistent/file.tmpl", "{{", "}}")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read template file")
}

func TestReadStdin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple content",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "content with trailing newline",
			input:    "hello world\n",
			expected: "hello world",
		},
		{
			name:     "multiline content",
			input:    "line1\nline2",
			expected: "line1\nline2",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader := bytes.NewBufferString(tt.input)
			result, err := ReadStdin(reader)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadStdin_NilReader(t *testing.T) {
	t.Parallel()

	_, err := ReadStdin(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin is not available")
}
