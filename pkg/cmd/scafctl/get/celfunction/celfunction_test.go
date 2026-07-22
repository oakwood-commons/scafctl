// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celfunction

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	celdetail "github.com/oakwood-commons/scafctl/pkg/celexp/detail"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testFuncs() celexp.ExtFunctionList {
	return celexp.ExtFunctionList{
		{
			Name:          "test.custom",
			Description:   "A test custom function",
			Custom:        true,
			FunctionNames: []string{"test_custom"},
			Examples: []celexp.Example{
				{Description: "Basic usage", Expression: "test.custom()"},
			},
			Links: []string{"https://example.com"},
		},
		{
			Name:          "test.builtin",
			Description:   "A test built-in function",
			Custom:        false,
			FunctionNames: []string{"test_builtin"},
		},
	}
}

func testCustomFuncs() celexp.ExtFunctionList {
	return celexp.ExtFunctionList{testFuncs()[0]}
}

func testBuiltInFuncs() celexp.ExtFunctionList {
	return celexp.ExtFunctionList{testFuncs()[1]}
}

func mkTestOpts(buf *bytes.Buffer) *Options {
	return &Options{
		IOStreams: &terminal.IOStreams{
			Out:    buf,
			ErrOut: buf,
		},
		CliParams: &settings.Run{
			NoColor: true,
		},
		allFn:     testFuncs,
		customFn:  testCustomFuncs,
		builtInFn: testBuiltInFuncs,
	}
}

func TestRunListFunctions_SimpleList(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test.custom")
	assert.Contains(t, output, "test.builtin")
	assert.Contains(t, output, "A test custom function")
	assert.Contains(t, output, "A test built-in function")
}

func TestRunListFunctions_CustomFilter(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	opts.Custom = true
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test.custom")
	assert.NotContains(t, output, "test.builtin")
}

func TestRunListFunctions_BuiltInFilter(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	opts.BuiltIn = true
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test.builtin")
	assert.NotContains(t, output, "test.custom")
}

func TestRunListFunctions_JSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	opts.Output = "json"
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "\"name\"")
	assert.Contains(t, output, "\"test.custom\"")
	assert.Contains(t, output, "\"custom\"")
}

func TestRunListFunctions_Quiet(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	opts.Output = "quiet"
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	// Quiet mode suppresses all output
	assert.Empty(t, buf.String())
}

func TestRunGetFunction_Found(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunGetFunction(ctx, "test.custom")
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test.custom")
	assert.Contains(t, output, "A test custom function")
	assert.Contains(t, output, "test_custom")
	assert.Contains(t, output, "Basic usage")
	assert.Contains(t, output, "https://example.com")
}

func TestRunGetFunction_NotFound(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunGetFunction(ctx, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunGetFunction_CaseInsensitive(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunGetFunction(ctx, "TEST.CUSTOM")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "test.custom")
}

func TestRunGetFunction_JSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	opts.Output = "json"
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunGetFunction(ctx, "test.custom")
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "\"name\"")
	assert.Contains(t, output, "\"test.custom\"")
}

func TestBuildFunctionDetailOutput(t *testing.T) {
	t.Parallel()
	fn := &celexp.ExtFunction{
		Name:          "test",
		Description:   "test desc",
		Custom:        true,
		FunctionNames: []string{"fn1", "fn2"},
		Links:         []string{"https://example.com"},
		Examples: []celexp.Example{
			{Description: "ex1", Expression: "test()"},
		},
	}

	result := celdetail.BuildFunctionDetail(fn)
	assert.Equal(t, "test", result["name"])
	assert.Equal(t, true, result["custom"])
	assert.Equal(t, "test desc", result["description"])
	assert.Equal(t, []string{"fn1", "fn2"}, result["functionNames"])
	assert.Equal(t, []string{"https://example.com"}, result["links"])

	examples, ok := result["examples"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, examples, 1)
	assert.Equal(t, "ex1", examples[0]["description"])
	assert.Equal(t, "test()", examples[0]["expression"])
}

func TestBuildFunctionListOutput(t *testing.T) {
	t.Parallel()
	funcs := testFuncs()
	result := celdetail.BuildFunctionList(funcs)
	assert.Len(t, result, 2)
	assert.Equal(t, "test.custom", result[0]["name"])
	assert.Equal(t, "test.builtin", result[1]["name"])
}

func TestCommandFunctions_Creation(t *testing.T) {
	t.Parallel()
	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}
	cmd := CommandFunctions(cliParams, ioStreams, "test/path")

	assert.Equal(t, "functions", cmd.Use)
	assert.Empty(t, cmd.Aliases)
	assert.False(t, cmd.Hidden)
	assert.Empty(t, cmd.Deprecated)
	assert.NotNil(t, cmd.RunE)
	assert.NotNil(t, cmd.Flags().Lookup("output"))
	assert.NotNil(t, cmd.Flags().Lookup("interactive"))
	assert.NotNil(t, cmd.Flags().Lookup("expression"))
	assert.NotNil(t, cmd.Flags().Lookup("custom"))
	assert.NotNil(t, cmd.Flags().Lookup("builtin"))
}

func TestCommandCelFunctionDeprecated_Creation(t *testing.T) {
	t.Parallel()
	cliParams := &settings.Run{BinaryName: "scafctl"}
	ioStreams := &terminal.IOStreams{}
	cmd := CommandCelFunctionDeprecated(cliParams, ioStreams, "test/path")

	assert.Equal(t, "cel-functions", cmd.Use)
	assert.Contains(t, cmd.Aliases, "cel-funcs")
	assert.Contains(t, cmd.Aliases, "cf")
	assert.NotContains(t, cmd.Aliases, "cel", "bare 'cel' alias is now claimed by the group")
	assert.True(t, cmd.Hidden)
	assert.NotEmpty(t, cmd.Deprecated)
	assert.Contains(t, cmd.Deprecated, "get cel functions")
	assert.NotNil(t, cmd.RunE)
	assert.NotNil(t, cmd.Flags().Lookup("custom"))
	assert.NotNil(t, cmd.Flags().Lookup("builtin"))
}

// TestCanonicalAndDeprecated_ShareRunE verifies the canonical child and the
// deprecated leaf produce identical output for the same args, since both share
// the newCommand builder / RunE.
func TestCanonicalAndDeprecated_ShareRunE(t *testing.T) {
	t.Parallel()
	cliParams := &settings.Run{BinaryName: "scafctl"}

	var outC, errC, outD, errD bytes.Buffer
	ioC := terminal.NewIOStreams(nil, &outC, &errC, false)
	ioD := terminal.NewIOStreams(nil, &outD, &errD, false)

	canonical := CommandFunctions(cliParams, ioC, "scafctl/get/cel")
	deprecated := CommandCelFunctionDeprecated(cliParams, ioD, "scafctl/get")

	canonical.SetOut(&errC)
	canonical.SetErr(&errC)
	canonical.SetArgs([]string{"-o", "json"})
	require.NoError(t, canonical.Execute())

	deprecated.SetOut(&errD)
	deprecated.SetErr(&errD)
	deprecated.SetArgs([]string{"-o", "json"})
	require.NoError(t, deprecated.Execute())

	// The rendered function list is written through IOStreams.Out (outC/outD),
	// which is independent of cobra's own writer. Cobra emits its deprecation
	// notice through the command's writer (pointed at errC/errD here), so the
	// two streams stay cleanly separated: the deprecated leaf's stdout must be
	// byte-identical to the canonical child's stdout (shared RunE), with no
	// stripping required.
	assert.Equal(t, outC.String(), outD.String())
	assert.Contains(t, outC.String(), "\"name\"")
	// The deprecation notice must be emitted on the deprecated leaf's cobra
	// writer (errD), not mixed into the function-list output on stdout.
	assert.Contains(t, errD.String(), `is deprecated`)
	assert.NotContains(t, outD.String(), `is deprecated`)
}

func TestRunListFunctions_SearchByFunctionName(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	opts.Search = "test_custom"
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test.custom")
	assert.NotContains(t, output, "test.builtin")
}

func TestRunListFunctions_SearchByDescription(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	opts.Search = "built-in"
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test.builtin")
	assert.NotContains(t, output, "test.custom")
}

func TestRunListFunctions_SearchNoMatch(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	opts.Search = "nonexistent_xyz"
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunListFunctions(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no CEL functions matching")
}

func TestRunListFunctions_FunctionsColumnInTable(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf)
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test_custom")
	assert.Contains(t, output, "test_builtin")
}

func TestFilterBySearch(t *testing.T) {
	t.Parallel()
	funcs := celexp.ExtFunctionList{
		{Name: "encoders", Description: "Encoding functions", FunctionNames: []string{"base64.decode", "base64.encode"}},
		{Name: "strings", Description: "String functions", FunctionNames: []string{"charAt", "join"}},
		{Name: "math", Description: "Math functions", FunctionNames: []string{"math.abs"}},
	}

	tests := []struct {
		name    string
		query   string
		wantLen int
		wantHas string
	}{
		{"match by function name", "base64", 1, "encoders"},
		{"match by group name", "strings", 1, "strings"},
		{"match by description", "Math", 1, "math"},
		{"case insensitive", "BASE64", 1, "encoders"},
		{"no match", "nonexistent", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := filterBySearch(funcs, tt.query)
			assert.Len(t, result, tt.wantLen)
			if tt.wantLen > 0 {
				assert.Equal(t, tt.wantHas, result[0].Name)
			}
		})
	}
}

func TestCommandCelFunction_SearchFlag(t *testing.T) {
	t.Parallel()
	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}
	cmd := CommandFunctions(cliParams, ioStreams, "test/path")

	f := cmd.Flags().Lookup("search")
	assert.NotNil(t, f)
	assert.Equal(t, "s", f.Shorthand)
}

// TestWriteOutput_EmptyBinaryNameFallback verifies writeOutput does not blank
// the kvx app name when BinaryName is unset (embedder didn't set it): the
// local fallback to settings.CliBinaryName keeps the name non-blank.
func TestWriteOutput_EmptyBinaryNameFallback(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := mkTestOpts(&buf) // BinaryName intentionally empty
	require.Empty(t, opts.BinaryName)
	ctx := settings.IntoContext(context.Background(), opts.CliParams)
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))

	err := opts.writeOutput(ctx, []FunctionSummary{{Name: "x"}})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "x")
}
