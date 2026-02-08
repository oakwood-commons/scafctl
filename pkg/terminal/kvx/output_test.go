// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kvx

import (
	"bytes"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputFormat_String(t *testing.T) {
	tests := []struct {
		format   OutputFormat
		expected string
	}{
		{OutputFormatTable, "table"},
		{OutputFormatJSON, "json"},
		{OutputFormatYAML, "yaml"},
		{OutputFormatQuiet, "quiet"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.format.String())
		})
	}
}

func TestBaseOutputFormats(t *testing.T) {
	formats := BaseOutputFormats()

	assert.Contains(t, formats, "table")
	assert.Contains(t, formats, "json")
	assert.Contains(t, formats, "yaml")
	assert.Contains(t, formats, "quiet")
	assert.Len(t, formats, 4)
}

func TestIsStructuredFormat(t *testing.T) {
	tests := []struct {
		format   OutputFormat
		expected bool
	}{
		{OutputFormatTable, false},
		{OutputFormatJSON, true},
		{OutputFormatYAML, true},
		{OutputFormatQuiet, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			assert.Equal(t, tt.expected, IsStructuredFormat(tt.format))
		})
	}
}

func TestIsTableFormat(t *testing.T) {
	tests := []struct {
		format   OutputFormat
		expected bool
	}{
		{OutputFormatTable, true},
		{"", true},
		{OutputFormatJSON, false},
		{OutputFormatYAML, false},
		{OutputFormatQuiet, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			assert.Equal(t, tt.expected, IsTableFormat(tt.format))
		})
	}
}

func TestIsQuietFormat(t *testing.T) {
	tests := []struct {
		format   OutputFormat
		expected bool
	}{
		{OutputFormatQuiet, true},
		{OutputFormatTable, false},
		{OutputFormatJSON, false},
		{OutputFormatYAML, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			assert.Equal(t, tt.expected, IsQuietFormat(tt.format))
		})
	}
}

func TestParseOutputFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected OutputFormat
		ok       bool
	}{
		{"table", OutputFormatTable, true},
		{"", OutputFormatTable, true},
		{"json", OutputFormatJSON, true},
		{"yaml", OutputFormatYAML, true},
		{"quiet", OutputFormatQuiet, true},
		{"invalid", "", false},
		{"JSON", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			format, ok := ParseOutputFormat(tt.input)
			assert.Equal(t, tt.expected, format)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

func TestOutputOptions_Write_QuietMode(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatQuiet

	err := opts.Write(map[string]any{"key": "value"})

	require.NoError(t, err)
	assert.Empty(t, out.String())
}

func TestOutputOptions_Write_JSON(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatJSON

	data := map[string]any{"name": "test", "count": 42}
	err := opts.Write(data)

	require.NoError(t, err)
	assert.Contains(t, out.String(), `"name": "test"`)
	assert.Contains(t, out.String(), `"count": 42`)
}

func TestOutputOptions_Write_YAML(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatYAML

	data := map[string]any{"name": "test", "count": 42}
	err := opts.Write(data)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "name: test")
	assert.Contains(t, out.String(), "count: 42")
}

func TestOutputOptions_Write_JSONWithExpression(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatJSON
	opts.Expression = "_.name"

	data := map[string]any{"name": "test", "count": 42}
	err := opts.Write(data)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "test")
	assert.NotContains(t, out.String(), "count")
}

func TestOutputOptions_Write_YAMLWithExpression(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatYAML
	opts.Expression = "_.items"

	data := map[string]any{
		"items": []string{"a", "b", "c"},
		"other": "ignored",
	}
	err := opts.Write(data)

	require.NoError(t, err)
	outputStr := out.String()
	assert.Contains(t, outputStr, "- a")
	assert.Contains(t, outputStr, "- b")
	assert.Contains(t, outputStr, "- c")
	assert.NotContains(t, outputStr, "ignored")
}

func TestOutputOptions_Write_InvalidExpression(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatJSON
	opts.Expression = "invalid((syntax"

	data := map[string]any{"name": "test"}
	err := opts.Write(data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expression evaluation failed")
}

func TestOutputOptions_Write_JSONNoPrettyPrint(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatJSON
	opts.PrettyPrint = false

	data := map[string]any{"name": "test", "count": 42}
	err := opts.Write(data)

	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	assert.Len(t, lines, 1)
}

func TestOutputOptions_Functional_Options(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)

	WithOutputFormat(OutputFormatJSON)(opts)
	WithOutputInteractive(true)(opts)
	WithOutputExpression("_.name")(opts)
	WithOutputNoColor(true)(opts)
	WithOutputAppName("test-app")(opts)
	WithOutputHelp("Test Help", []string{"Line 1", "Line 2"})(opts)
	WithOutputTheme("dark")(opts)
	WithOutputPrettyPrint(false)(opts)

	assert.Equal(t, OutputFormatJSON, opts.Format)
	assert.True(t, opts.Interactive)
	assert.Equal(t, "_.name", opts.Expression)
	assert.True(t, opts.NoColor)
	assert.Equal(t, "test-app", opts.AppName)
	assert.Equal(t, "Test Help", opts.HelpTitle)
	assert.Equal(t, []string{"Line 1", "Line 2"}, opts.HelpLines)
	assert.Equal(t, "dark", opts.Theme)
	assert.False(t, opts.PrettyPrint)
}

func TestOutputOptions_WithOutputFormatString(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	tests := []struct {
		input    string
		expected OutputFormat
	}{
		{"json", OutputFormatJSON},
		{"yaml", OutputFormatYAML},
		{"table", OutputFormatTable},
		{"quiet", OutputFormatQuiet},
		{"invalid", OutputFormatTable},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			opts := NewOutputOptions(ioStreams)
			WithOutputFormatString(tt.input)(opts)
			assert.Equal(t, tt.expected, opts.Format)
		})
	}
}

func TestNewOutputOptions_Defaults(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)

	assert.Equal(t, OutputFormatTable, opts.Format)
	assert.True(t, opts.PrettyPrint)
	assert.False(t, opts.Interactive)
	assert.Empty(t, opts.Expression)
	assert.Same(t, ioStreams, opts.IOStreams)
}

func TestOutputOptions_Write_TableFallbackToJSONWhenNotTTY(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatTable

	data := map[string]any{"name": "test"}
	err := opts.Write(data)

	require.NoError(t, err)
	assert.Contains(t, out.String(), `"name": "test"`)
}

func TestOutputOptions_Write_InteractiveErrorWhenNotTTY(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatTable
	opts.Interactive = true

	data := map[string]any{"name": "test"}
	err := opts.Write(data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "interactive mode requires a terminal")
}
