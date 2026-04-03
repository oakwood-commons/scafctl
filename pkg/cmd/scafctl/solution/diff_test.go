// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeIOStreams() (*bytes.Buffer, terminal.IOStreams) {
	out := &bytes.Buffer{}
	return out, terminal.IOStreams{
		In:     os.Stdin,
		Out:    out,
		ErrOut: &bytes.Buffer{},
	}
}

func writeSolution(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

const solutionV1 = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-app
  version: 1.0.0
  description: Version one
spec:
  resolvers:
    app_name:
      description: Application name
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "my-app"
`

const solutionV2 = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-app
  version: 2.0.0
  description: Version two
spec:
  resolvers:
    app_name:
      description: Application name
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "my-app"
    new_resolver:
      description: A new resolver
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "new"
`

func TestCommandDiff_TableOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fileA := writeSolution(t, dir, "v1.yaml", solutionV1)
	fileB := writeSolution(t, dir, "v2.yaml", solutionV2)

	out, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", fileA, "-f", fileB})

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "Solution Diff:")
	assert.Contains(t, output, "changed  metadata.version")
	assert.Contains(t, output, "changed  metadata.description")
	assert.Contains(t, output, "added    spec.resolvers.new_resolver")
	assert.Contains(t, output, "Summary:")
}

func TestCommandDiff_JSONOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fileA := writeSolution(t, dir, "v1.yaml", solutionV1)
	fileB := writeSolution(t, dir, "v2.yaml", solutionV2)

	out, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", fileA, "-f", fileB, "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	assert.Contains(t, result, "changes")
	assert.Contains(t, result, "summary")
	summary := result["summary"].(map[string]any)
	assert.Greater(t, summary["total"].(float64), float64(0))
}

func TestCommandDiff_YAMLOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fileA := writeSolution(t, dir, "v1.yaml", solutionV1)
	fileB := writeSolution(t, dir, "v2.yaml", solutionV2)

	out, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", fileA, "-f", fileB, "-o", "yaml"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "pathA:")
	assert.Contains(t, output, "changes:")
}

func TestCommandDiff_InvalidFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fileA := writeSolution(t, dir, "v1.yaml", solutionV1)
	fileB := writeSolution(t, dir, "v2.yaml", solutionV2)

	_, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", fileA, "-f", fileB, "-o", "invalid"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandDiff_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fileA := writeSolution(t, dir, "v1.yaml", solutionV1)

	_, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", fileA, "-f", "/nonexistent/file.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandDiff_IdenticalSolutions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fileA := writeSolution(t, dir, "v1.yaml", solutionV1)
	fileB := writeSolution(t, dir, "v1-copy.yaml", solutionV1)

	out, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", fileA, "-f", fileB})

	err := cmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, out.String(), "No structural differences found.")
}

func TestCommandDiff_WrongArgCount(t *testing.T) {
	t.Parallel()
	_, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", "only-one.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandSolution_HasDiffSubcommand(t *testing.T) {
	t.Parallel()
	_, ioStreams := makeIOStreams()
	cmd := CommandSolution(&settings.Run{}, ioStreams, "scafctl")

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "diff" {
			found = true
			break
		}
	}
	assert.True(t, found, "solution command should have diff subcommand")
}

// ── Benchmarks ──────────────────────────────────────────────────────

func BenchmarkCommandDiff_Table(b *testing.B) {
	dir := b.TempDir()
	fileA := filepath.Join(dir, "v1.yaml")
	fileB := filepath.Join(dir, "v2.yaml")
	require.NoError(b, os.WriteFile(fileA, []byte(solutionV1), 0o644))
	require.NoError(b, os.WriteFile(fileB, []byte(solutionV2), 0o644))

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		out := &bytes.Buffer{}
		ioStreams := terminal.IOStreams{In: os.Stdin, Out: out, ErrOut: out}
		cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
		cmd.SetArgs([]string{"-f", fileA, "-f", fileB})
		_ = cmd.Execute()
	}
}

func BenchmarkCommandDiff_JSON(b *testing.B) {
	dir := b.TempDir()
	fileA := filepath.Join(dir, "v1.yaml")
	fileB := filepath.Join(dir, "v2.yaml")
	require.NoError(b, os.WriteFile(fileA, []byte(solutionV1), 0o644))
	require.NoError(b, os.WriteFile(fileB, []byte(solutionV2), 0o644))

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		out := &bytes.Buffer{}
		ioStreams := terminal.IOStreams{In: os.Stdin, Out: out, ErrOut: out}
		cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
		cmd.SetArgs([]string{"-f", fileA, "-f", fileB, "-o", "json"})
		_ = cmd.Execute()
	}
}

// ── resolveDiffSlotOrder tests ──────────────────────────────────────

// newTestDiffFlags builds a FlagSet that mirrors the diff command's flags.
func newTestDiffFlags() *pflag.FlagSet {
	fs := pflag.NewFlagSet("diff", pflag.ContinueOnError)
	fs.StringArrayP("file", "f", nil, "")
	fs.StringP("output", "o", "table", "")
	return fs
}

func TestResolveDiffSlotOrder(t *testing.T) {
	fs := newTestDiffFlags()

	tests := []struct {
		name    string
		osArgs  []string
		files   []string
		posArgs []string
		wantA   string
		wantB   string
	}{
		{
			name:   "two -f flags",
			osArgs: []string{"scafctl", "solution", "diff", "-f", "old.yaml", "-f", "new.yaml"},
			files:  []string{"old.yaml", "new.yaml"},
			wantA:  "old.yaml",
			wantB:  "new.yaml",
		},
		{
			name:    "two positional catalog refs",
			osArgs:  []string{"scafctl", "solution", "diff", "my-app@1.0.0", "my-app@2.0.0"},
			posArgs: []string{"my-app@1.0.0", "my-app@2.0.0"},
			wantA:   "my-app@1.0.0",
			wantB:   "my-app@2.0.0",
		},
		{
			name:    "file first then catalog",
			osArgs:  []string{"scafctl", "solution", "diff", "-f", "modified.yaml", "my-app@1.0.0"},
			files:   []string{"modified.yaml"},
			posArgs: []string{"my-app@1.0.0"},
			wantA:   "modified.yaml",
			wantB:   "my-app@1.0.0",
		},
		{
			name:    "catalog first then file",
			osArgs:  []string{"scafctl", "solution", "diff", "my-app@1.0.0", "-f", "modified.yaml"},
			files:   []string{"modified.yaml"},
			posArgs: []string{"my-app@1.0.0"},
			wantA:   "my-app@1.0.0",
			wantB:   "modified.yaml",
		},
		{
			name:   "with output flag",
			osArgs: []string{"scafctl", "solution", "diff", "-f", "a.yaml", "-f", "b.yaml", "-o", "json"},
			files:  []string{"a.yaml", "b.yaml"},
			wantA:  "a.yaml",
			wantB:  "b.yaml",
		},
		{
			// Regression: shorthand -o was not looked up via ShorthandLookup,
			// so its value token could be misread as a positional source,
			// inverting the A/B order when the catalog ref shared the flag value.
			name:    "shorthand output flag before mixed file and catalog",
			osArgs:  []string{"scafctl", "solution", "diff", "-o", "json", "-f", "a.yaml", "json"},
			files:   []string{"a.yaml"},
			posArgs: []string{"json"},
			wantA:   "a.yaml",
			wantB:   "json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sources := resolveDiffSlotOrder(tt.osArgs, fs, tt.files, tt.posArgs)
			require.Len(t, sources, 2)
			assert.Equal(t, tt.wantA, sources[0].Value, "slot A")
			assert.Equal(t, tt.wantB, sources[1].Value, "slot B")
		})
	}
}

func TestCommandDiff_RejectsPositionalFilePaths(t *testing.T) {
	t.Parallel()
	_, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"./solution.yaml", "my-app@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local file paths must use -f/--file flag")
}
