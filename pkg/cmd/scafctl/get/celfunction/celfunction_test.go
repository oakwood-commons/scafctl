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

	output := buf.String()
	assert.Contains(t, output, "test.custom")
	assert.Contains(t, output, "test.builtin")
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

func TestCommandCelFunction_Creation(t *testing.T) {
	t.Parallel()
	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}
	cmd := CommandCelFunction(cliParams, ioStreams, "test/path")

	assert.Equal(t, "cel-functions", cmd.Use)
	assert.Contains(t, cmd.Aliases, "cel")
	assert.Contains(t, cmd.Aliases, "cf")
	assert.NotNil(t, cmd.RunE)
	assert.NotNil(t, cmd.Flags().Lookup("output"))
	assert.NotNil(t, cmd.Flags().Lookup("interactive"))
	assert.NotNil(t, cmd.Flags().Lookup("expression"))
	assert.NotNil(t, cmd.Flags().Lookup("custom"))
	assert.NotNil(t, cmd.Flags().Lookup("builtin"))
}
