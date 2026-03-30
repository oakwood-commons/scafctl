// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFuncTestCtx creates the context and writer needed for runFunctional tests.
func newFuncTestCtx(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	return ctx, &buf
}

// ── runFunctional error path tests ────────────────────────────────────────────

// TestRunFunctional_NoPathProvided verifies that runFunctional returns an error
// when no file, no tests-path, and no auto-discoverable solution exist.
func TestRunFunctional_NoPathProvided(t *testing.T) {
	ctx, _ := newFuncTestCtx(t)

	// Use a temp dir as CWD so auto-discovery finds nothing
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	opts := &FunctionalOptions{
		IOStreams: terminal.NewIOStreams(nil, nil, nil, false),
		CliParams: settings.NewCliParams(),
		// File, TestsPath are both empty — simulates "no solution path provided"
		// Auto-discovery will also return empty since temp dir is empty
	}

	err := runFunctional(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no solution path provided")
}

// TestRunFunctional_EmptyDirectory verifies that runFunctional returns no error
// when the directory exists but has no solution files.
func TestRunFunctional_EmptyDirectory(t *testing.T) {
	ctx, buf := newFuncTestCtx(t)

	// Create a solutions directory with no YAML files
	tmpDir := t.TempDir()

	opts := &FunctionalOptions{
		IOStreams: terminal.NewIOStreams(nil, buf, buf, false),
		CliParams: settings.NewCliParams(),
		TestsPath: tmpDir, // exists but empty
	}

	err := runFunctional(ctx, opts)
	require.NoError(t, err)
	// Should print "No solutions with tests found."
	assert.Contains(t, buf.String(), "No solutions")
}

// TestRunFunctional_TestsPathSet verifies that an explicit --tests-path takes
// priority over --file when both are set.
func TestRunFunctional_TestsPathTakesPrecedence(t *testing.T) {
	ctx, _ := newFuncTestCtx(t)

	tmpDir := t.TempDir()

	opts := &FunctionalOptions{
		IOStreams: terminal.NewIOStreams(nil, nil, nil, false),
		CliParams: settings.NewCliParams(),
		File:      "/nonexistent/solution.yaml",
		TestsPath: tmpDir, // should be used instead of File
	}

	// The tests-path (tmpDir) exists but is empty → no solutions, returns nil
	err := runFunctional(ctx, opts)
	require.NoError(t, err)
}

// TestRunFunctional_InvalidFile verifies that passing a nonexistent file returns an error.
func TestRunFunctional_InvalidFile(t *testing.T) {
	ctx, _ := newFuncTestCtx(t)

	opts := &FunctionalOptions{
		IOStreams: terminal.NewIOStreams(nil, nil, nil, false),
		CliParams: settings.NewCliParams(),
		File:      "/nonexistent/completely-made-up-path/solution.yaml",
	}

	err := runFunctional(ctx, opts)
	// Should return an error — either path not found or discovery failed
	require.Error(t, err)
}

// TestRunFunctional_NilWriter verifies runFunctional works even when the writer
// is not injected in context (uses fallback path).
func TestRunFunctional_NilWriter(t *testing.T) {
	// No writer in context — runFunctional should create one as fallback
	ctx := context.Background()

	tmpDir := t.TempDir()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)

	opts := &FunctionalOptions{
		IOStreams: ioStreams,
		CliParams: settings.NewCliParams(),
		TestsPath: tmpDir,
	}

	// Should not panic — fallback writer is created
	err := runFunctional(ctx, opts)
	require.NoError(t, err, "should succeed finding 0 solutions in empty dir")
}

// ── CommandFunctional additional tests ────────────────────────────────────────

// TestCommandFunctional_SilenceUsage verifies that SilenceUsage is set.
func TestCommandFunctional_SilenceUsage(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandFunctional(cliParams, ioStreams, "scafctl/test")

	assert.True(t, cmd.SilenceUsage)
}

// TestCommandFunctional_SequentialFlag verifies that sequential and concurrency flags exist.
func TestCommandFunctional_ConcurrencyDefault(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandFunctional(cliParams, ioStreams, "scafctl/test")

	concurrency, err := cmd.Flags().GetInt("concurrency")
	require.NoError(t, err)
	assert.Equal(t, 1, concurrency)
}

// TestCommandFunctional_WatchFlag verifies the watch flag exists with proper shorthand.
func TestCommandFunctional_WatchFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandFunctional(cliParams, ioStreams, "scafctl/test")

	flag := cmd.Flags().Lookup("watch")
	require.NotNil(t, flag)
	assert.Equal(t, "w", flag.Shorthand)
}

// BenchmarkRunFunctional_EmptyDirectory measures time for runFunctional with empty dir.
func BenchmarkRunFunctional_EmptyDirectory(b *testing.B) {
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	tmpDir := b.TempDir()
	opts := &FunctionalOptions{
		IOStreams: terminal.NewIOStreams(nil, nil, nil, false),
		CliParams: settings.NewCliParams(),
		TestsPath: tmpDir,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = runFunctional(ctx, opts)
	}
}
