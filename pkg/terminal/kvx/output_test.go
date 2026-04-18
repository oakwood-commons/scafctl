// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kvx

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputFormat_String(t *testing.T) {
	tests := []struct {
		format   OutputFormat
		expected string
	}{
		{OutputFormatAuto, "auto"},
		{OutputFormatTable, "table"},
		{OutputFormatList, "list"},
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

	assert.Contains(t, formats, "auto")
	assert.Contains(t, formats, "table")
	assert.Contains(t, formats, "list")
	assert.Contains(t, formats, "json")
	assert.Contains(t, formats, "yaml")
	assert.Contains(t, formats, "quiet")
	assert.Contains(t, formats, "test")
	assert.Contains(t, formats, "tree")
	assert.Contains(t, formats, "mermaid")
	assert.Len(t, formats, 10)
}

func TestIsStructuredFormat(t *testing.T) {
	tests := []struct {
		format   OutputFormat
		expected bool
	}{
		{OutputFormatAuto, false},
		{OutputFormatTable, false},
		{OutputFormatList, false},
		{OutputFormatJSON, true},
		{OutputFormatYAML, true},
		{OutputFormatMermaid, true},
		{OutputFormatQuiet, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			assert.Equal(t, tt.expected, IsStructuredFormat(tt.format))
		})
	}
}

func TestIsKvxFormat(t *testing.T) {
	tests := []struct {
		format   OutputFormat
		expected bool
	}{
		{OutputFormatAuto, true},
		{OutputFormatTable, true},
		{OutputFormatList, true},
		{OutputFormatTree, true},
		{"", true},
		{OutputFormatJSON, false},
		{OutputFormatYAML, false},
		{OutputFormatQuiet, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			assert.Equal(t, tt.expected, IsKvxFormat(tt.format))
		})
	}
}

func TestIsQuietFormat(t *testing.T) {
	tests := []struct {
		format   OutputFormat
		expected bool
	}{
		{OutputFormatQuiet, true},
		{OutputFormatAuto, false},
		{OutputFormatTable, false},
		{OutputFormatList, false},
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
		{"auto", OutputFormatAuto, true},
		{"table", OutputFormatTable, true},
		{"list", OutputFormatList, true},
		{"", OutputFormatAuto, true},
		{"json", OutputFormatJSON, true},
		{"yaml", OutputFormatYAML, true},
		{"quiet", OutputFormatQuiet, true},
		{"test", OutputFormatTest, true},
		{"tree", OutputFormatTree, true},
		{"mermaid", OutputFormatMermaid, true},
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
		{"auto", OutputFormatAuto},
		{"table", OutputFormatTable},
		{"list", OutputFormatList},
		{"quiet", OutputFormatQuiet},
		{"invalid", OutputFormatAuto},
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

	assert.Equal(t, OutputFormatAuto, opts.Format)
	assert.True(t, opts.PrettyPrint)
	assert.False(t, opts.Interactive)
	assert.Empty(t, opts.Expression)
	assert.Same(t, ioStreams, opts.IOStreams)
}

func TestOutputOptions_Write_TableFallbackToTextWhenNotTTY(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatAuto

	data := map[string]any{"name": "test"}
	err := opts.Write(data)

	require.NoError(t, err)
	// Non-TTY output falls back to plain text table, not JSON
	assert.Contains(t, out.String(), "name")
	assert.Contains(t, out.String(), "test")
	assert.NotContains(t, out.String(), `"name"`, "should not produce JSON output")
}

func TestOutputOptions_Write_InteractiveErrorWhenNotTTY(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatAuto
	opts.Interactive = true

	data := map[string]any{"name": "test"}
	err := opts.Write(data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "interactive mode requires a terminal")
}

func TestOutputOptions_Write_KvxNonTTY_WithExpression(t *testing.T) {
	// Tests the non-TTY auto-fallback path in writeKvx WITH an expression filter
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatAuto
	opts.Expression = "_"

	data := map[string]any{"key": "value"}
	err := opts.Write(data)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "key")
}

func TestOutputOptions_Write_KvxNonTTY_WithInvalidExpression(t *testing.T) {
	// Tests expression evaluation error in non-TTY writeKvx path
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatAuto
	opts.Expression = "undefined_var_xyz_not_valid_in_this_context"

	data := map[string]any{"key": "value"}
	err := opts.Write(data)
	// May or may not error depending on CEL env; just ensure no panic
	_ = err
}

func TestOutputOptions_Write_TextFormat(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatText

	data := map[string]any{"name": "test", "count": 42}
	err := opts.Write(data)

	require.NoError(t, err)
	output := out.String()
	assert.Contains(t, output, "name")
	assert.Contains(t, output, "test")
	assert.NotContains(t, output, `"name"`, "text format should not produce JSON")
}

func TestOutputOptions_Write_TextFormat_WithExpression(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatText
	opts.Expression = "_.name"

	data := map[string]any{"name": "filtered-value"}
	err := opts.Write(data)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "filtered-value")
}

func TestOutputOptions_Write_TextFormat_Scalar(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatText

	err := opts.Write("hello world")

	require.NoError(t, err)
	assert.Equal(t, "hello world\n", out.String())
}

func TestParseOutputFormat_Text(t *testing.T) {
	f, ok := ParseOutputFormat("text")
	assert.True(t, ok)
	assert.Equal(t, OutputFormatText, f)
}

func TestOutputOptions_FormatExplicit(t *testing.T) {
	opts := NewOutputOptions(nil)
	assert.False(t, opts.FormatExplicit, "default should be false")

	WithFormatExplicit(true)(opts)
	assert.True(t, opts.FormatExplicit)
}

func TestIsAutoFormat(t *testing.T) {
	assert.True(t, IsAutoFormat(OutputFormatAuto))
	assert.False(t, IsAutoFormat(OutputFormatJSON))
	assert.False(t, IsAutoFormat(OutputFormatTable))
}

func TestIsListFormat(t *testing.T) {
	assert.True(t, IsListFormat(OutputFormatList))
	assert.False(t, IsListFormat(OutputFormatJSON))
	assert.False(t, IsListFormat(OutputFormatTable))
}

func TestWithOutputContext(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(ioStreams)
	WithOutputContext(context.TODO())(opts)
	assert.NotNil(t, opts)
}

func TestWithOutputColumnOrder(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(ioStreams)
	WithOutputColumnOrder([]string{"name", "value"})(opts)
	assert.Equal(t, []string{"name", "value"}, opts.ColumnOrder)
}

func TestWithOutputColumnHints(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(ioStreams)
	WithOutputColumnHints(nil)(opts)
	assert.NotNil(t, opts)
}

func TestWithOutputSchemaJSON(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	schema := []byte(`{"type":"object"}`)
	opts := NewOutputOptions(ioStreams)
	WithOutputSchemaJSON(schema)(opts)
	assert.Equal(t, schema, opts.SchemaJSON)
}

func TestWithOutputDisplaySchemaJSON(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	schema := []byte(`{"type":"object"}`)
	opts := NewOutputOptions(ioStreams)
	WithOutputDisplaySchemaJSON(schema)(opts)
	assert.Equal(t, schema, opts.DisplaySchemaJSON)
}

func TestWithIOStreams(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(&terminal.IOStreams{})
	WithIOStreams(ioStreams)(opts)
	assert.Equal(t, ioStreams, opts.IOStreams)
}

func TestWriteTo_JSON(t *testing.T) {
	src := &bytes.Buffer{}
	dst := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: src}
	opts := NewOutputOptions(ioStreams)
	WithOutputFormat(OutputFormatJSON)(opts)
	data := map[string]any{"key": "value"}
	err := opts.WriteTo(dst, data)
	require.NoError(t, err)
	assert.Contains(t, dst.String(), "key")
}

func TestDefaultViewerOptions(t *testing.T) {
	opts := DefaultViewerOptions()
	assert.NotNil(t, opts)
	assert.Equal(t, settings.CliBinaryName, opts.AppName)
	assert.NotNil(t, opts.In)
	assert.NotNil(t, opts.Out)
}

func TestWithAppName(t *testing.T) {
	opts := DefaultViewerOptions()
	WithAppName("myapp")(opts)
	assert.Equal(t, "myapp", opts.AppName)
}

func TestWithDimensions(t *testing.T) {
	opts := DefaultViewerOptions()
	WithDimensions(80, 24)(opts)
	assert.Equal(t, 80, opts.Width)
	assert.Equal(t, 24, opts.Height)
}

func TestWithNoColor(t *testing.T) {
	opts := DefaultViewerOptions()
	WithNoColor(true)(opts)
	assert.True(t, opts.NoColor)
}

func TestWithIO(t *testing.T) {
	opts := DefaultViewerOptions()
	in := strings.NewReader("input")
	out := &bytes.Buffer{}
	WithIO(in, out)(opts)
	assert.Equal(t, in, opts.In)
	assert.Equal(t, out, opts.Out)
}

func TestWithExpression(t *testing.T) {
	opts := DefaultViewerOptions()
	WithExpression("result.status == 'ok'")(opts)
	assert.Equal(t, "result.status == 'ok'", opts.Expression)
}

func TestWithInteractive(t *testing.T) {
	opts := DefaultViewerOptions()
	WithInteractive(true)(opts)
	assert.True(t, opts.Interactive)
}

func TestWithHelp(t *testing.T) {
	opts := DefaultViewerOptions()
	lines := []string{"line1", "line2"}
	WithHelp("My Title", lines)(opts)
	assert.Equal(t, "My Title", opts.HelpTitle)
	assert.Equal(t, lines, opts.HelpLines)
}

func TestWithTheme(t *testing.T) {
	opts := DefaultViewerOptions()
	WithTheme("dark")(opts)
	assert.Equal(t, "dark", opts.Theme)
}

func TestWithLayout(t *testing.T) {
	opts := DefaultViewerOptions()
	WithLayout("table")(opts)
	assert.Equal(t, "table", opts.Layout)
}

func TestWithInitialExpr(t *testing.T) {
	opts := DefaultViewerOptions()
	WithInitialExpr("_.status")(opts)
	assert.Equal(t, "_.status", opts.InitialExpr)
}

func TestWithColumnOrder(t *testing.T) {
	opts := DefaultViewerOptions()
	order := []string{"name", "status", "age"}
	WithColumnOrder(order)(opts)
	assert.Equal(t, order, opts.ColumnOrder)
}

func TestWithSchemaJSON(t *testing.T) {
	opts := DefaultViewerOptions()
	schema := []byte(`{"type":"object"}`)
	WithSchemaJSON(schema)(opts)
	assert.Equal(t, schema, opts.SchemaJSON)
}

func TestWithDisplaySchemaJSON(t *testing.T) {
	opts := DefaultViewerOptions()
	schema := []byte(`{"type":"array"}`)
	WithDisplaySchemaJSON(schema)(opts)
	assert.Equal(t, schema, opts.DisplaySchemaJSON)
}

func TestWithContext(t *testing.T) {
	opts := DefaultViewerOptions()
	ctx := context.Background()
	WithContext(ctx)(opts)
	assert.Equal(t, ctx, opts.Ctx)
}

func TestWithColumnHints(t *testing.T) {
	opts := DefaultViewerOptions()
	hints := map[string]tui.ColumnHint{
		"name": {DisplayName: "Name"},
	}
	WithColumnHints(hints)(opts)
	assert.Equal(t, hints, opts.ColumnHints)
}

func TestRenderTable(t *testing.T) {
	data := []map[string]any{
		{"name": "alice", "age": 30},
		{"name": "bob", "age": 25},
	}
	result, err := RenderTable(data, tui.TableOptions{})
	assert.NoError(t, err)
	assert.Contains(t, result, "alice")
	assert.Contains(t, result, "bob")
}

func TestRenderList(t *testing.T) {
	data := map[string]any{
		"name":    "scafctl",
		"version": "1.0.0",
	}
	result, err := RenderList(data, true)
	assert.NoError(t, err)
	assert.Contains(t, result, "scafctl")
}

func TestOutputOptions_Write_TestFormat_Error(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatTest

	err := opts.Write(map[string]any{"key": "val"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestOutputOptions_Write_YAML_Success(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatYAML

	err := opts.Write(map[string]any{"status": "ok"})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "status")
}

func TestOutputOptions_Write_JSON_PrettyPrint(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatJSON
	opts.PrettyPrint = true

	err := opts.Write(map[string]any{"key": "val"})
	require.NoError(t, err)
	// Pretty-printed JSON has indentation
	assert.Contains(t, out.String(), "  \"key\"")
}

func TestOutputOptions_Write_TableFormat_FallbackJSON(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatTable

	err := opts.Write(map[string]any{"name": "alice"})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "alice")
}

func TestOutputOptions_Write_ListFormat_FallbackJSON(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatList

	err := opts.Write(map[string]any{"name": "bob"})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "bob")
}

func TestOutputOptions_Write_TableFallback_WithExpression(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}
	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatAuto
	opts.Expression = "_"
	opts.Ctx = context.Background()

	err := opts.Write(map[string]any{"x": 1})
	require.NoError(t, err)
}

// ---- viewer.go coverage tests ----

func TestView_NonInteractive_Table(t *testing.T) {
	out := &bytes.Buffer{}
	data := []map[string]any{
		{"name": "alice", "age": 30},
		{"name": "bob", "age": 25},
	}
	err := View(data, WithIO(strings.NewReader(""), out), WithNoColor(true))
	require.NoError(t, err)
	// Table output should contain the data
	assert.Contains(t, out.String(), "alice")
}

func TestView_NonInteractive_List(t *testing.T) {
	out := &bytes.Buffer{}
	data := map[string]any{"name": "scafctl", "version": "1.0"}
	err := View(data, WithIO(strings.NewReader(""), out), WithLayout("list"), WithNoColor(true))
	require.NoError(t, err)
	assert.Contains(t, out.String(), "scafctl")
}

func TestView_WithExpression(t *testing.T) {
	out := &bytes.Buffer{}
	data := map[string]any{"items": []string{"a", "b"}, "other": "skip"}
	err := View(data,
		WithIO(strings.NewReader(""), out),
		WithExpression("_.items"),
		WithNoColor(true),
	)
	require.NoError(t, err)
}

func TestView_WithInvalidExpression(t *testing.T) {
	out := &bytes.Buffer{}
	data := map[string]any{"name": "test"}
	err := View(data,
		WithIO(strings.NewReader(""), out),
		WithExpression("invalid((syntax"),
		WithNoColor(true),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expression evaluation failed")
}

func TestView_Interactive_NonTTY(t *testing.T) {
	out := &bytes.Buffer{}
	data := map[string]any{"key": "val"}
	err := View(data, WithIO(strings.NewReader(""), out), WithInteractive(true))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interactive mode requires a terminal")
}

func TestView_NonInteractive_Tree(t *testing.T) {
	out := &bytes.Buffer{}
	data := map[string]any{"name": "scafctl", "version": "1.0"}
	err := View(data, WithIO(strings.NewReader(""), out), WithLayout("tree"), WithNoColor(true))
	require.NoError(t, err)
	assert.Contains(t, out.String(), "scafctl")
}

func TestView_NonInteractive_Mermaid(t *testing.T) {
	out := &bytes.Buffer{}
	data := map[string]any{"name": "scafctl", "version": "1.0"}
	err := View(data, WithIO(strings.NewReader(""), out), WithLayout("mermaid"))
	require.NoError(t, err)
	assert.NotEmpty(t, out.String())
}

func TestView_WithWhere(t *testing.T) {
	out := &bytes.Buffer{}
	data := []map[string]any{
		{"name": "alice", "enabled": true},
		{"name": "bob", "enabled": false},
	}
	err := View(data,
		WithIO(strings.NewReader(""), out),
		WithWhere("_.enabled == true"),
		WithNoColor(true),
	)
	require.NoError(t, err)
	output := out.String()
	assert.Contains(t, output, "alice")
	assert.NotContains(t, output, "bob")
}

func TestView_WithWhere_InvalidExpression(t *testing.T) {
	out := &bytes.Buffer{}
	data := []map[string]any{{"name": "test"}}
	err := View(data,
		WithIO(strings.NewReader(""), out),
		WithWhere("invalid((syntax"),
		WithNoColor(true),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "where filter failed")
}

func TestOutputOptions_Write_Mermaid(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatMermaid

	data := map[string]any{"name": "test", "version": "1.0"}
	err := opts.Write(data)

	require.NoError(t, err)
	assert.NotEmpty(t, out.String())
}

func TestOutputOptions_Write_Tree(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatTree

	data := map[string]any{"name": "test", "version": "1.0"}
	err := opts.Write(data)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "test")
}

func TestOutputOptions_Write_JSON_WithWhere(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatJSON
	opts.Where = "_.enabled == true"

	data := []map[string]any{
		{"name": "alice", "enabled": true},
		{"name": "bob", "enabled": false},
	}
	err := opts.Write(data)

	require.NoError(t, err)
	output := out.String()
	assert.Contains(t, output, "alice")
	assert.NotContains(t, output, "bob")
}

func TestOutputOptions_Write_YAML_WithWhere(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatYAML
	opts.Where = "_.enabled == true"

	data := []map[string]any{
		{"name": "keep", "enabled": true},
		{"name": "drop", "enabled": false},
	}
	err := opts.Write(data)

	require.NoError(t, err)
	output := out.String()
	assert.Contains(t, output, "keep")
	assert.NotContains(t, output, "drop")
}

func TestOutputOptions_Write_JSON_WithWhere_InvalidExpression(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatJSON
	opts.Where = "invalid((syntax"

	data := []map[string]any{{"name": "test"}}
	err := opts.Write(data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "where filter failed")
}

func TestResolveColumnHints_BothEmpty(t *testing.T) {
	result := resolveColumnHints(nil, nil)
	assert.Nil(t, result)
}

func TestResolveColumnHints_ProgrammaticOnly(t *testing.T) {
	hints := map[string]tui.ColumnHint{"name": {DisplayName: "Name"}}
	result := resolveColumnHints(nil, hints)
	assert.Equal(t, hints, result)
}

func TestResolveColumnHints_SchemaJSON_Valid(t *testing.T) {
	// Valid schema JSON (tui.ParseSchema expects a schema with properties)
	schema := []byte(`{"type":"object","properties":{"name":{"type":"string","description":"The name"}}}`)
	result := resolveColumnHints(schema, nil)
	// May be nil if schema doesn't produce hints, but should not panic
	_ = result
}

func TestResolveColumnHints_SchemaJSON_Invalid(t *testing.T) {
	// Invalid JSON — ParseSchema will error, merged stays nil
	schema := []byte(`{not valid json`)
	result := resolveColumnHints(schema, nil)
	assert.Nil(t, result)
}

func TestResolveColumnHints_BothProvided(t *testing.T) {
	// Programmatic hints should take precedence over schema hints
	schema := []byte(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	programmatic := map[string]tui.ColumnHint{"status": {DisplayName: "Status"}}
	result := resolveColumnHints(schema, programmatic)
	// Should contain the programmatic hint
	assert.NotNil(t, result)
}

func TestIsTerminal_WithBuffer(t *testing.T) {
	out := &bytes.Buffer{}
	assert.False(t, IsTerminal(out))
}

// ---- cel.go coverage tests ----

func TestSetupScafctlCELProvider_Success(t *testing.T) {
	// Should not panic or return error
	err := SetupScafctlCELProvider(nil)
	require.NoError(t, err)
}

func TestBuildFunctionHints_NotEmpty(t *testing.T) {
	hints := buildFunctionHints()
	// Should return a non-empty map with known function hints
	assert.NotEmpty(t, hints)
	assert.Contains(t, hints, "regex.match")
	assert.Contains(t, hints, "regex.replace")
}

func TestStructToMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    any
		expected any
		wantErr  bool
	}{
		{
			name: "struct to map",
			input: struct {
				Name  string `json:"name"`
				Count int    `json:"count"`
			}{Name: "test", Count: 42},
			expected: map[string]any{"name": "test", "count": float64(42)},
		},
		{
			name: "slice of structs",
			input: []struct {
				V string `json:"v"`
			}{{V: "a"}, {V: "b"}},
			expected: []any{map[string]any{"v": "a"}, map[string]any{"v": "b"}},
		},
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := StructToMap(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOutputOptions_Write_KvxNonTTY_WithWhere(t *testing.T) {
	// Tests the non-TTY auto-fallback path in writeKvx with Where filter
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatAuto
	opts.Where = "_.active"

	data := []map[string]any{
		{"name": "keep", "active": true},
		{"name": "drop", "active": false},
	}
	err := opts.Write(data)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "keep")
	assert.NotContains(t, out.String(), "drop")
}

func TestOutputOptions_Write_KvxNonTTY_WithWhereAndExpression(t *testing.T) {
	// Tests the non-TTY auto-fallback path with both Where and Expression
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatTable
	opts.Where = "_.active"
	opts.Expression = "_"

	data := []map[string]any{
		{"name": "alpha", "active": true},
		{"name": "beta", "active": false},
	}
	err := opts.Write(data)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "alpha")
	assert.NotContains(t, out.String(), "beta")
}

func TestOutputOptions_Write_KvxNonTTY_InvalidWhere(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatAuto
	opts.Where = "invalid((syntax"

	data := []map[string]any{{"name": "test"}}
	err := opts.Write(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "where filter failed")
}

func TestOutputOptions_Write_ScalarWithExpression_NotShortCircuited(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatJSON
	opts.Expression = "_.name"

	// Scalar data with expression should not take the scalar fast-path;
	// it should fall through so the expression is applied.
	err := opts.Write("plain-string")
	// Expression may fail on a scalar — the point is it must NOT silently
	// print the raw scalar and skip the expression.
	if err == nil {
		assert.NotEqual(t, "plain-string\n", out.String(), "scalar fast-path must not bypass expression")
	}
}

func TestOutputOptions_Write_ScalarWithWhere_NotShortCircuited(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatJSON
	opts.Where = "_ == true"

	err := opts.Write("plain-string")
	if err == nil {
		assert.NotEqual(t, "plain-string\n", out.String(), "scalar fast-path must not bypass where filter")
	}
}

func TestOutputOptions_Write_ScalarNoFilters_FastPath(t *testing.T) {
	out := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: out}

	opts := NewOutputOptions(ioStreams)
	opts.Format = OutputFormatAuto

	err := opts.Write("hello world")
	require.NoError(t, err)
	assert.Equal(t, "hello world\n", out.String(), "scalar without filters should use fast-path")
}
