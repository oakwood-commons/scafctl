// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	refslib "github.com/oakwood-commons/scafctl/pkg/resolver/refs"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandRefs_Template(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		expectedRefs   []string
		expectedOutput string
	}{
		{
			name:         "simple template",
			template:     "{{ ._.config.host }}",
			expectedRefs: []string{"config"},
		},
		{
			name:         "multiple references",
			template:     "{{ ._.config.host }}:{{ ._.port.number }}",
			expectedRefs: []string{"config", "port"},
		},
		{
			name:         "duplicate references",
			template:     "{{ ._.config.host }} and {{ ._.config.port }}",
			expectedRefs: []string{"config"},
		},
		{
			name:         "no references",
			template:     "static text",
			expectedRefs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			errOut := &bytes.Buffer{}
			ioStreams := &terminal.IOStreams{
				Out:    out,
				ErrOut: errOut,
			}

			cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
			cmd.SetArgs([]string{"--template", tt.template, "-o", "json"})

			err := cmd.Execute()
			require.NoError(t, err)

			var result refslib.Output
			err = json.Unmarshal(out.Bytes(), &result)
			require.NoError(t, err)

			assert.Equal(t, "template", result.SourceType)
			assert.Equal(t, tt.template, result.Source)
			assert.Equal(t, tt.expectedRefs, result.References)
			assert.Equal(t, len(tt.expectedRefs), result.Count)
		})
	}
}

func TestCommandRefs_CEL(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		expectedRefs []string
	}{
		{
			name:         "simple cel expression",
			expr:         "_.config.host",
			expectedRefs: []string{"config"},
		},
		{
			name:         "multiple references",
			expr:         `_.config.host + ":" + string(_.port.value)`,
			expectedRefs: []string{"config", "port"},
		},
		{
			name:         "no references",
			expr:         `"static" + "text"`,
			expectedRefs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			errOut := &bytes.Buffer{}
			ioStreams := &terminal.IOStreams{
				Out:    out,
				ErrOut: errOut,
			}

			cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
			cmd.SetArgs([]string{"--expr", tt.expr, "-o", "json"})

			err := cmd.Execute()
			require.NoError(t, err)

			var result refslib.Output
			err = json.Unmarshal(out.Bytes(), &result)
			require.NoError(t, err)

			assert.Equal(t, "cel-expression", result.SourceType)
			assert.Equal(t, tt.expr, result.Source)
			assert.Equal(t, tt.expectedRefs, result.References)
		})
	}
}

func TestCommandRefs_TemplateFile(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	// Create test template file
	templateContent := "Hello {{ ._.name.first }} {{ ._.name.last }}, your config is {{ ._.config.value }}"
	templatePath := filepath.Join(tempDir, "test.tmpl")
	err := os.WriteFile(templatePath, []byte(templateContent), 0o644)
	require.NoError(t, err)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		Out:    out,
		ErrOut: errOut,
	}

	cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"--template-file", templatePath, "-o", "json"})

	err = cmd.Execute()
	require.NoError(t, err)

	var result refslib.Output
	err = json.Unmarshal(out.Bytes(), &result)
	require.NoError(t, err)

	assert.Equal(t, "template-file", result.SourceType)
	assert.Equal(t, templatePath, result.Source)
	assert.ElementsMatch(t, []string{"config", "name"}, result.References)
	assert.Equal(t, 2, result.Count)
}

func TestCommandRefs_CustomDelimiters(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		Out:    out,
		ErrOut: errOut,
	}

	template := "<% ._.config.host %> and <% ._.port.value %>"
	cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{
		"--template", template,
		"--left-delim", "<%",
		"--right-delim", "%>",
		"-o", "json",
	})

	err := cmd.Execute()
	require.NoError(t, err)

	var result refslib.Output
	err = json.Unmarshal(out.Bytes(), &result)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"config", "port"}, result.References)
}

func TestCommandRefs_Validation(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectedErr string
	}{
		{
			name:        "no input provided",
			args:        []string{},
			expectedErr: "one of --template-file, --template, or --expr is required",
		},
		{
			name:        "multiple inputs provided",
			args:        []string{"--template", "foo", "--expr", "bar"},
			expectedErr: "only one of --template-file, --template, or --expr can be specified",
		},
		{
			name:        "file not found",
			args:        []string{"--template-file", "/nonexistent/file.tmpl"},
			expectedErr: "failed to read template file",
		},
		{
			name:        "invalid output format",
			args:        []string{"--template", "{{ ._.foo }}", "-o", "xml"},
			expectedErr: "unknown output format: xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			errOut := &bytes.Buffer{}
			ioStreams := &terminal.IOStreams{
				Out:    out,
				ErrOut: errOut,
			}

			cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestCommandRefs_TextOutput(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		Out:    out,
		ErrOut: errOut,
	}

	cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"--template", "{{ ._.config.host }}:{{ ._.port.value }}"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "Resolver references found in template:")
	assert.Contains(t, output, "- config")
	assert.Contains(t, output, "- port")
	assert.Contains(t, output, "Total: 2 reference(s)")
}

func TestCommandRefs_NoReferencesTextOutput(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		Out:    out,
		ErrOut: errOut,
	}

	cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"--template", "static content"})

	err := cmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, out.String(), "No resolver references found.")
}

func TestExtractResolverName(t *testing.T) {
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
			result := refslib.ExtractResolverName(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCommandRefs_TemplateStdin(t *testing.T) {
	stdinContent := "{{ ._.config.host }}:{{ ._.port.value }}"
	stdin := io.NopCloser(bytes.NewBufferString(stdinContent))
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		In:     stdin,
		Out:    out,
		ErrOut: errOut,
	}

	cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"--template", "-", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var result refslib.Output
	err = json.Unmarshal(out.Bytes(), &result)
	require.NoError(t, err)

	assert.Equal(t, "template-stdin", result.SourceType)
	assert.Equal(t, stdinContent, result.Source)
	assert.ElementsMatch(t, []string{"config", "port"}, result.References)
}

func TestCommandRefs_CELStdin(t *testing.T) {
	stdinContent := `_.config.host + ":" + string(_.port)`
	stdin := io.NopCloser(bytes.NewBufferString(stdinContent))
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		In:     stdin,
		Out:    out,
		ErrOut: errOut,
	}

	cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"--expr", "-", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var result refslib.Output
	err = json.Unmarshal(out.Bytes(), &result)
	require.NoError(t, err)

	assert.Equal(t, "cel-expression-stdin", result.SourceType)
	assert.Equal(t, stdinContent, result.Source)
	assert.ElementsMatch(t, []string{"config", "port"}, result.References)
}

func TestCommandRefs_StdinWithNewline(t *testing.T) {
	// Test that trailing newlines are trimmed (common when piping)
	stdinContent := "{{ ._.config.host }}\n"
	stdin := io.NopCloser(bytes.NewBufferString(stdinContent))
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		In:     stdin,
		Out:    out,
		ErrOut: errOut,
	}

	cmd := CommandRefs(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"--template", "-", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var result refslib.Output
	err = json.Unmarshal(out.Bytes(), &result)
	require.NoError(t, err)

	// Source should have trailing newline trimmed
	assert.Equal(t, "{{ ._.config.host }}", result.Source)
	assert.Equal(t, []string{"config"}, result.References)
}

func TestReadStdin(t *testing.T) {
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
			reader := bytes.NewBufferString(tt.input)
			result, err := refslib.ReadStdin(reader)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadStdin_NilReader(t *testing.T) {
	_, err := refslib.ReadStdin(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin is not available")
}
