// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── renderGraph tests ────────────────────────────────────────────────────────

// errGraphRenderer always returns an error for the specified format.
type errGraphRenderer struct {
	failFormat string
}

func (e *errGraphRenderer) RenderASCII(w io.Writer) error {
	if e.failFormat == "ascii" {
		return fmt.Errorf("ascii render failed")
	}
	_, err := w.Write([]byte("ascii output"))
	return err
}

func (e *errGraphRenderer) RenderDOT(w io.Writer) error {
	if e.failFormat == "dot" {
		return fmt.Errorf("dot render failed")
	}
	_, err := w.Write([]byte("digraph {}"))
	return err
}

func (e *errGraphRenderer) RenderMermaid(w io.Writer) error {
	if e.failFormat == "mermaid" {
		return fmt.Errorf("mermaid render failed")
	}
	_, err := w.Write([]byte("graph TD"))
	return err
}

func TestSolutionOptions_renderGraph(t *testing.T) {
	t.Parallel()

	newOpts := func(format string) (*SolutionOptions, *bytes.Buffer) {
		var buf bytes.Buffer
		return &SolutionOptions{
			IOStreams:   &terminal.IOStreams{Out: &buf, ErrOut: &buf},
			CliParams:   &settings.Run{},
			GraphFormat: format,
		}, &buf
	}

	t.Run("ascii_success", func(t *testing.T) {
		t.Parallel()
		opts, buf := newOpts("ascii")
		graph := &errGraphRenderer{}
		err := opts.renderGraph(graph, graph)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "ascii output")
	})

	t.Run("ascii_failure", func(t *testing.T) {
		t.Parallel()
		opts, _ := newOpts("ascii")
		graph := &errGraphRenderer{failFormat: "ascii"}
		err := opts.renderGraph(graph, graph)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ascii")
	})

	t.Run("dot_success", func(t *testing.T) {
		t.Parallel()
		opts, buf := newOpts("dot")
		graph := &errGraphRenderer{}
		err := opts.renderGraph(graph, graph)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "digraph")
	})

	t.Run("dot_failure", func(t *testing.T) {
		t.Parallel()
		opts, _ := newOpts("dot")
		graph := &errGraphRenderer{failFormat: "dot"}
		err := opts.renderGraph(graph, graph)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DOT")
	})

	t.Run("mermaid_success", func(t *testing.T) {
		t.Parallel()
		opts, buf := newOpts("mermaid")
		graph := &errGraphRenderer{}
		err := opts.renderGraph(graph, graph)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "graph TD")
	})

	t.Run("mermaid_failure", func(t *testing.T) {
		t.Parallel()
		opts, _ := newOpts("mermaid")
		graph := &errGraphRenderer{failFormat: "mermaid"}
		err := opts.renderGraph(graph, graph)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Mermaid")
	})

	t.Run("json_success", func(t *testing.T) {
		t.Parallel()
		opts, buf := newOpts("json")
		data := map[string]string{"key": "value"}
		graph := &errGraphRenderer{}
		err := opts.renderGraph(graph, data)
		require.NoError(t, err)

		var decoded map[string]string
		assert.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
		assert.Equal(t, "value", decoded["key"])
	})

	t.Run("unsupported_format", func(t *testing.T) {
		t.Parallel()
		opts, _ := newOpts("invalid-format")
		graph := &errGraphRenderer{}
		err := opts.renderGraph(graph, graph)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported graph format")
		assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))
	})
}

// ── getEffectiveResolverConfig tests ─────────────────────────────────────────

func TestSolutionOptions_getEffectiveResolverConfig(t *testing.T) {
	t.Parallel()

	opts := &SolutionOptions{
		ResolverTimeout: settings.DefaultResolverTimeout,
		PhaseTimeout:    settings.DefaultPhaseTimeout,
		flagsChanged:    map[string]bool{},
	}

	cfg := opts.getEffectiveResolverConfig(context.Background())
	assert.Equal(t, settings.DefaultResolverTimeout, cfg.Timeout)
	assert.Equal(t, settings.DefaultPhaseTimeout, cfg.PhaseTimeout)
}

// ── writeOutput tests (extensions) ───────────────────────────────────────────

func TestSolutionOptions_writeOutput_ToFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := &SolutionOptions{
		OutputFile: dir + "/out",
		Output:     "json",
		IOStreams:  &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		CliParams:  &settings.Run{},
	}

	err := opts.writeOutput(context.Background(), []byte(`{"ok":true}`))
	require.NoError(t, err)
	assert.Equal(t, dir+"/out.json", opts.OutputFile)
}

func TestSolutionOptions_writeOutput_NoWriter(t *testing.T) {
	t.Parallel()

	opts := &SolutionOptions{
		IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		CliParams: &settings.Run{},
	}

	// Context without writer — should not panic
	err := opts.writeOutput(context.Background(), []byte("data"))
	require.NoError(t, err)
}

// ── Run routing tests ────────────────────────────────────────────────────────

func TestSolutionOptions_Run_ActionGraphRoute(t *testing.T) {
	t.Parallel()

	opts := &SolutionOptions{
		ActionGraph: true,
		IOStreams:   &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		CliParams:   &settings.Run{},
		getter:      &mockGetter{err: fmt.Errorf("load error")},
	}

	ctx := setupWriterContext()
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load error")
}

func TestSolutionOptions_Run_SnapshotRoute(t *testing.T) {
	t.Parallel()

	opts := &SolutionOptions{
		Snapshot:     true,
		SnapshotFile: "/tmp/test-snap.json",
		IOStreams:    &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		CliParams:    &settings.Run{},
		getter:       &mockGetter{err: fmt.Errorf("load error")},
	}

	ctx := setupWriterContext()
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load error")
}

func TestSolutionOptions_Run_DefaultRoute(t *testing.T) {
	t.Parallel()

	opts := &SolutionOptions{
		IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		CliParams: &settings.Run{},
		getter:    &mockGetter{err: fmt.Errorf("load error")},
	}

	ctx := setupWriterContext()
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load error")
}

// ── runActionGraph validation tests ──────────────────────────────────────────

func TestSolutionOptions_runActionGraph_NoWorkflow(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "no-wf"},
	}

	opts := &SolutionOptions{
		IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		CliParams: &settings.Run{},
		getter:    &mockGetter{sol: sol},
	}

	ctx := setupWriterContext()
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not define a workflow")
}

func TestSolutionOptions_runActionGraphVisualization_NoWorkflow(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "no-wf"},
	}

	opts := &SolutionOptions{
		ActionGraph: true,
		IOStreams:   &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		CliParams:   &settings.Run{},
		getter:      &mockGetter{sol: sol},
	}

	ctx := setupWriterContext()
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not define a workflow")
}

func TestSolutionOptions_runSnapshot_NoResolvers(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "no-resolvers"},
	}

	opts := &SolutionOptions{
		Snapshot:     true,
		SnapshotFile: "/tmp/snap.json",
		IOStreams:    &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		CliParams:    &settings.Run{},
		getter:       &mockGetter{sol: sol},
	}

	ctx := setupWriterContext()
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not define any resolvers")
}

// ── exitWithCode code extraction tests ───────────────────────────────────────

func TestSolutionOptions_exitWithCode_Codes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code int
	}{
		{"file-not-found", exitcode.FileNotFound},
		{"validation-failed", exitcode.ValidationFailed},
		{"render-failed", exitcode.RenderFailed},
		{"invalid-input", exitcode.InvalidInput},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := &SolutionOptions{
				IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
				CliParams: &settings.Run{},
			}
			err := opts.exitWithCode(fmt.Errorf("err"), tc.code)
			assert.Equal(t, tc.code, exitcode.GetCode(err))
			assert.Equal(t, "err", err.Error())
		})
	}
}

// ── Command flags extended tests ─────────────────────────────────────────────

func TestCommandSolution_AllFlags(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}
	cmd := CommandSolution(cliParams, ioStreams, "render")

	expectedFlags := []string{
		"file", "output", "output-file", "resolver", "compact",
		"no-timestamp", "no-cache", "action-graph", "graph-format",
		"snapshot", "snapshot-file", "redact", "test-name",
		"resolver-timeout", "phase-timeout",
	}

	for _, name := range expectedFlags {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			flag := cmd.Flags().Lookup(name)
			assert.NotNil(t, flag, "flag %q should exist", name)
		})
	}
}

func TestCommandSolution_FlagDefaults(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}
	cmd := CommandSolution(cliParams, ioStreams, "render")

	tests := []struct {
		flag     string
		defValue string
	}{
		{"output", "json"},
		{"graph-format", "ascii"},
		{"compact", "false"},
		{"no-timestamp", "false"},
		{"no-cache", "false"},
		{"action-graph", "false"},
		{"snapshot", "false"},
		{"redact", "false"},
	}

	for _, tc := range tests {
		t.Run(tc.flag, func(t *testing.T) {
			t.Parallel()
			flag := cmd.Flags().Lookup(tc.flag)
			require.NotNil(t, flag)
			assert.Equal(t, tc.defValue, flag.DefValue)
		})
	}
}

// ── writeToFile extended tests ───────────────────────────────────────────────

func TestSolutionOptions_writeToFile_WritesContents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := &SolutionOptions{
		OutputFile: dir + "/result.json",
		Output:     "json",
	}

	data := []byte(`{"rendered": true}`)
	err := opts.writeToFile(data)
	require.NoError(t, err)

	content, err := os.ReadFile(opts.OutputFile)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rendered": true}`, string(content))
}

// ── writeTestOutput tests ────────────────────────────────────────────────────

func TestSolutionOptions_writeTestOutput_ValidJSON(t *testing.T) {
	t.Parallel()

	ctx := setupWriterContext()

	dir := t.TempDir()
	var buf bytes.Buffer
	opts := &SolutionOptions{
		IOStreams:      &terminal.IOStreams{Out: &buf, ErrOut: &buf},
		CliParams:      &settings.Run{},
		File:           dir + "/solution.yaml",
		TestName:       "my_test",
		ResolverParams: []string{"env=dev"},
	}

	rendered := []byte(`{"resolvers":{"env":{"data":{"value":"dev"}}}}`)
	err := opts.writeTestOutput(ctx, rendered)
	require.NoError(t, err)
}

func TestSolutionOptions_writeTestOutput_InvalidJSON(t *testing.T) {
	t.Parallel()

	ctx := setupWriterContext()

	var buf bytes.Buffer
	opts := &SolutionOptions{
		IOStreams: &terminal.IOStreams{Out: &buf, ErrOut: &buf},
		CliParams: &settings.Run{ExitOnError: false},
		File:      "/tmp/sol.yaml",
	}

	err := opts.writeTestOutput(ctx, []byte("not valid json {{{"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse rendered output")
}

// ── autoResolveOfficialProviders tests ───────────────────────────────────────

func TestSolutionOptions_autoResolveOfficialProviders_NilSolution(t *testing.T) {
	t.Parallel()

	ctx := setupWriterContext()

	var buf bytes.Buffer
	opts := &SolutionOptions{
		IOStreams: &terminal.IOStreams{Out: &buf, ErrOut: &buf},
		CliParams: &settings.Run{},
	}

	// nil solution should not panic and should return nil
	clients := opts.autoResolveOfficialProviders(ctx, nil, nil)
	assert.Nil(t, clients)
}

func TestSolutionOptions_autoResolveOfficialProviders_EmptySolution(t *testing.T) {
	t.Parallel()

	ctx := setupWriterContext()

	var buf bytes.Buffer
	opts := &SolutionOptions{
		IOStreams: &terminal.IOStreams{Out: &buf, ErrOut: &buf},
		CliParams: &settings.Run{},
	}

	sol := &solution.Solution{}
	reg := provider.NewRegistry()

	clients := opts.autoResolveOfficialProviders(ctx, sol, reg)
	assert.Nil(t, clients)
}

// ── helper ───────────────────────────────────────────────────────────────────

func setupWriterContext() context.Context {
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)
	l := logr.Discard()
	ctx = logger.WithLogger(ctx, &l)
	return ctx
}
