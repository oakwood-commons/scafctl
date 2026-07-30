// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/effective"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// effectiveTestSolution builds a solution with resolvers and a workflow that a
// mockGetter can return (compose is assumed already applied on load).
func effectiveTestSolution(t *testing.T) *solution.Solution {
	t.Helper()
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: effective-cli
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
  workflow:
    actions:
      deploy:
        provider: shell
        inputs:
          command: "echo deploy"
`)))
	return sol
}

// newEffectiveOptions wires a SolutionOptions with a capturing writer context.
// output simulates an explicit -o value (flagsChanged["output"] is set so the
// effective-mode format selection mirrors a user-provided flag).
func newEffectiveOptions(sol *solution.Solution, err error, output, section string) (*SolutionOptions, *bytes.Buffer, context.Context) {
	opts, buf, ctx := newEffectiveOptionsRaw(sol, err, output, section)
	opts.flagsChanged = map[string]bool{"output": true}
	return opts, buf, ctx
}

// newEffectiveOptionsRaw is like newEffectiveOptions but leaves flagsChanged
// unset, simulating the bare default (no -o provided).
func newEffectiveOptionsRaw(sol *solution.Solution, err error, output, section string) (*SolutionOptions, *bytes.Buffer, context.Context) {
	buf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	opts := &SolutionOptions{
		Effective: true,
		Output:    output,
		Section:   section,
		IOStreams: ioStreams,
		CliParams: &settings.Run{},
		getter:    &mockGetter{sol: sol, err: err},
	}
	w := writer.New(ioStreams, opts.CliParams)
	ctx := writer.WithWriter(context.Background(), w)
	l := logr.Discard()
	ctx = logger.WithLogger(ctx, &l)
	return opts, buf, ctx
}

func TestSolutionOptions_Run_EffectiveRoute_YAML(t *testing.T) {
	t.Parallel()

	opts, buf, ctx := newEffectiveOptions(effectiveTestSolution(t), nil, "yaml", "all")
	require.NoError(t, opts.Run(ctx))

	out := buf.String()
	assert.Contains(t, out, "name: effective-cli")
	assert.Contains(t, out, "deploy:")
	assert.Contains(t, out, "env:")
	// Effective mode must NOT execute resolvers: the raw command is emitted
	// unresolved (no materialized ActionGraph envelope).
	assert.NotContains(t, out, "kind: ActionGraph")
}

// TestSolutionOptions_Run_EffectiveRoute_DefaultsToYAML verifies that with no
// explicit -o (flagsChanged unset), effective mode emits YAML even though the
// shared -o flag defaults to "json".
func TestSolutionOptions_Run_EffectiveRoute_DefaultsToYAML(t *testing.T) {
	t.Parallel()

	// Output is "json" (the shared flag default) but the user did not set -o.
	opts, buf, ctx := newEffectiveOptionsRaw(effectiveTestSolution(t), nil, "json", "all")
	require.NoError(t, opts.Run(ctx))

	out := buf.String()
	// YAML markers, not JSON.
	assert.Contains(t, out, "name: effective-cli")
	assert.NotContains(t, out, `"name": "effective-cli"`)
}

func TestSolutionOptions_Run_EffectiveRoute_JSON(t *testing.T) {
	t.Parallel()

	opts, buf, ctx := newEffectiveOptions(effectiveTestSolution(t), nil, "json", "all")
	require.NoError(t, opts.Run(ctx))

	assert.Contains(t, buf.String(), `"name": "effective-cli"`)
}

// TestSolutionOptions_Run_EffectiveRoute_VerbatimStdout asserts that effective
// mode emits bytes to stdout byte-identical to effective.Render (no extra
// trailing newline). This keeps stdout, --output-file, and effective.Render in
// agreement so golden-file diffing is exact.
func TestSolutionOptions_Run_EffectiveRoute_VerbatimStdout(t *testing.T) {
	t.Parallel()

	sol := effectiveTestSolution(t)
	opts, buf, ctx := newEffectiveOptions(sol, nil, "yaml", "all")
	require.NoError(t, opts.Run(ctx))

	want, err := effective.Render(sol, effective.Options{
		Section: effective.SectionAll,
		Format:  effective.FormatYAML,
	})
	require.NoError(t, err)

	assert.Equal(t, string(want), buf.String())
	assert.True(t, bytes.HasSuffix(buf.Bytes(), []byte("\n")))
	assert.False(t, bytes.HasSuffix(buf.Bytes(), []byte("\n\n")),
		"stdout must not carry a doubled trailing newline")
}

// TestSolutionOptions_Run_EffectiveRoute_OutputFileVerbatim asserts that
// --output-file receives the exact bytes of effective.Render (no extension
// munging, no trailing-newline drift) so a file written this way is identical
// to stdout.
func TestSolutionOptions_Run_EffectiveRoute_OutputFileVerbatim(t *testing.T) {
	t.Parallel()

	sol := effectiveTestSolution(t)
	opts, _, ctx := newEffectiveOptions(sol, nil, "yaml", "all")
	outFile := filepath.Join(t.TempDir(), "golden.effective.yaml")
	opts.OutputFile = outFile
	require.NoError(t, opts.Run(ctx))

	want, err := effective.Render(sol, effective.Options{
		Section: effective.SectionAll,
		Format:  effective.FormatYAML,
	})
	require.NoError(t, err)

	got, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestSolutionOptions_Run_EffectiveRoute_SectionWorkflow(t *testing.T) {
	t.Parallel()

	opts, buf, ctx := newEffectiveOptions(effectiveTestSolution(t), nil, "yaml", "workflow")
	require.NoError(t, opts.Run(ctx))

	out := buf.String()
	assert.Contains(t, out, "deploy:")
	assert.NotContains(t, out, "env:")
}

func TestSolutionOptions_Run_EffectiveRoute_LoadError(t *testing.T) {
	t.Parallel()

	opts, _, ctx := newEffectiveOptions(nil, fmt.Errorf("load error"), "yaml", "all")
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load error")
}

func TestSolutionOptions_Run_EffectiveRoute_NoWorkflow(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-workflow
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
`)))

	opts, _, ctx := newEffectiveOptions(sol, nil, "yaml", "workflow")
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not define a workflow")
}
