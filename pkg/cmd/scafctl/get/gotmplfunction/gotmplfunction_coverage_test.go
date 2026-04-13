// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmplfunction

import (
	"bytes"
	"context"
	"testing"

	cmdflags "github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

func newGotmplCtx(t *testing.T) (context.Context, *bytes.Buffer, *Options) {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &Options{
		IOStreams: ioStreams,
		CliParams: cliParams,
	}
	return ctx, &buf, opts
}

// mockFunctionList returns a small deterministic function list for tests.
func mockFunctionList() gotmpl.ExtFunctionList {
	return gotmpl.ExtFunctionList{
		{
			Name:        "testFunc",
			Description: "A test function for unit testing",
			Custom:      true,
		},
		{
			Name:        "sprigFunc",
			Description: "A sprig function for testing",
			Custom:      false,
		},
	}
}

// ── getFunctions tests ────────────────────────────────────────────────────────

func TestGetFunctions_All(t *testing.T) {
	t.Parallel()

	called := false
	opts := &Options{
		allFn: func() gotmpl.ExtFunctionList {
			called = true
			return mockFunctionList()
		},
	}

	result := opts.getFunctions()
	assert.True(t, called)
	assert.Len(t, result, 2)
}

func TestGetFunctions_Custom(t *testing.T) {
	t.Parallel()

	customList := gotmpl.ExtFunctionList{{Name: "custom1", Custom: true}}
	opts := &Options{
		Custom: true,
		customFn: func() gotmpl.ExtFunctionList {
			return customList
		},
		allFn: func() gotmpl.ExtFunctionList {
			return mockFunctionList()
		},
	}

	result := opts.getFunctions()
	assert.Len(t, result, 1)
	assert.Equal(t, "custom1", result[0].Name)
}

func TestGetFunctions_Sprig(t *testing.T) {
	t.Parallel()

	sprigList := gotmpl.ExtFunctionList{{Name: "sprig1", Custom: false}}
	opts := &Options{
		Sprig: true,
		sprigFn: func() gotmpl.ExtFunctionList {
			return sprigList
		},
		allFn: func() gotmpl.ExtFunctionList {
			return mockFunctionList()
		},
	}

	result := opts.getFunctions()
	assert.Len(t, result, 1)
	assert.Equal(t, "sprig1", result[0].Name)
}

func TestGetFunctions_DefaultsToAll_WhenNoInjected(t *testing.T) {
	t.Parallel()

	// When no injection functions are set, it should return a non-empty list
	// (from the real gotmpl extension registry)
	opts := &Options{}

	result := opts.getFunctions()
	assert.NotEmpty(t, result)
}

// ── RunListFunctions tests ────────────────────────────────────────────────────

func TestRunListFunctions_SimpleList(t *testing.T) {
	t.Parallel()

	ctx, buf, opts := newGotmplCtx(t)
	opts.allFn = func() gotmpl.ExtFunctionList {
		return mockFunctionList()
	}

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "testFunc")
	assert.Contains(t, output, "A test function for unit testing")
}

func TestRunListFunctions_QuietOutput(t *testing.T) {
	t.Parallel()

	ctx, buf, opts := newGotmplCtx(t)
	opts.KvxOutputFlags = cmdflags.KvxOutputFlags{Output: "quiet"}
	opts.allFn = func() gotmpl.ExtFunctionList {
		return mockFunctionList()
	}

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	// Quiet mode suppresses all output
	assert.Empty(t, buf.String())
}

func TestRunListFunctions_JSONOutput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &out, &out, false)
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &Options{
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: cmdflags.KvxOutputFlags{Output: "json"},
		allFn: func() gotmpl.ExtFunctionList {
			return mockFunctionList()
		},
	}

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "testFunc")
	assert.Contains(t, output, "sprigFunc")
}

func TestRunListFunctions_CustomFilter(t *testing.T) {
	t.Parallel()

	ctx, buf, opts := newGotmplCtx(t)
	opts.Custom = true
	opts.customFn = func() gotmpl.ExtFunctionList {
		return gotmpl.ExtFunctionList{
			{Name: "myCustom", Description: "My custom function", Custom: true},
		}
	}

	err := opts.RunListFunctions(ctx)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "myCustom")
}

// ── RunGetFunction tests ──────────────────────────────────────────────────────

func TestRunGetFunction_Found(t *testing.T) {
	t.Parallel()

	ctx, buf, opts := newGotmplCtx(t)
	opts.allFn = func() gotmpl.ExtFunctionList {
		return mockFunctionList()
	}

	err := opts.RunGetFunction(ctx, "testFunc")
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "testFunc")
}

func TestRunGetFunction_CaseInsensitive(t *testing.T) {
	t.Parallel()

	ctx, buf, opts := newGotmplCtx(t)
	opts.allFn = func() gotmpl.ExtFunctionList {
		return mockFunctionList()
	}

	err := opts.RunGetFunction(ctx, "TESTFUNC")
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "testFunc")
}

func TestRunGetFunction_NotFound(t *testing.T) {
	t.Parallel()

	ctx, _, opts := newGotmplCtx(t)
	opts.allFn = func() gotmpl.ExtFunctionList {
		return mockFunctionList()
	}

	err := opts.RunGetFunction(ctx, "nonExistentFunction")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunGetFunction_JSONOutput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &out, &out, false)
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &Options{
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: cmdflags.KvxOutputFlags{Output: "json"},
		allFn: func() gotmpl.ExtFunctionList {
			return mockFunctionList()
		},
	}

	err := opts.RunGetFunction(ctx, "testFunc")
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "testFunc")
}

// ── printFunctionDetail tests ─────────────────────────────────────────────────

func TestPrintFunctionDetail_NoColor(t *testing.T) {
	t.Parallel()

	ctx, buf, opts := newGotmplCtx(t)

	fn := &gotmpl.ExtFunction{
		Name:        "myFunc",
		Description: "Does something useful",
		Custom:      true,
		Examples: []gotmpl.Example{
			{Description: "Basic usage", Template: `{{ myFunc "arg" }}`},
		},
		Links: []string{"https://docs.example.com/myFunc"},
	}

	err := opts.printFunctionDetail(ctx, fn)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "myFunc")
	assert.Contains(t, output, "Does something useful")
	assert.Contains(t, output, "Basic usage")
	assert.Contains(t, output, `{{ myFunc "arg" }}`)
	assert.Contains(t, output, "https://docs.example.com/myFunc")
}

func TestPrintFunctionDetail_SprigFunction(t *testing.T) {
	t.Parallel()

	ctx, buf, opts := newGotmplCtx(t)

	fn := &gotmpl.ExtFunction{
		Name:        "sprigDate",
		Description: "A sprig date function",
		Custom:      false,
	}

	err := opts.printFunctionDetail(ctx, fn)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "sprigDate")
	assert.Contains(t, output, "sprig")
}

// ── writeQuietOutput tests ────────────────────────────────────────────────────

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkGetFunctions_All(b *testing.B) {
	opts := &Options{
		allFn: func() gotmpl.ExtFunctionList {
			return mockFunctionList()
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		opts.getFunctions()
	}
}

func BenchmarkRunGetFunction(b *testing.B) {
	var out bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &out, &out, false)
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &Options{
		IOStreams: ioStreams,
		CliParams: cliParams,
		allFn: func() gotmpl.ExtFunctionList {
			return mockFunctionList()
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		out.Reset()
		_ = opts.RunGetFunction(ctx, "testFunc")
	}
}
