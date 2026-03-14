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
	cmd.SetArgs([]string{fileA, fileB})

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
	cmd.SetArgs([]string{fileA, fileB, "-o", "json"})

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
	cmd.SetArgs([]string{fileA, fileB, "-o", "yaml"})

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
	cmd.SetArgs([]string{fileA, fileB, "-o", "invalid"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandDiff_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fileA := writeSolution(t, dir, "v1.yaml", solutionV1)

	_, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{fileA, "/nonexistent/file.yaml"})

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
	cmd.SetArgs([]string{fileA, fileB})

	err := cmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, out.String(), "No structural differences found.")
}

func TestCommandDiff_WrongArgCount(t *testing.T) {
	t.Parallel()
	_, ioStreams := makeIOStreams()
	cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
	cmd.SetArgs([]string{"only-one.yaml"})

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

	for b.Loop() {
		out := &bytes.Buffer{}
		ioStreams := terminal.IOStreams{In: os.Stdin, Out: out, ErrOut: out}
		cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
		cmd.SetArgs([]string{fileA, fileB})
		_ = cmd.Execute()
	}
}

func BenchmarkCommandDiff_JSON(b *testing.B) {
	dir := b.TempDir()
	fileA := filepath.Join(dir, "v1.yaml")
	fileB := filepath.Join(dir, "v2.yaml")
	require.NoError(b, os.WriteFile(fileA, []byte(solutionV1), 0o644))
	require.NoError(b, os.WriteFile(fileB, []byte(solutionV2), 0o644))

	for b.Loop() {
		out := &bytes.Buffer{}
		ioStreams := terminal.IOStreams{In: os.Stdin, Out: out, ErrOut: out}
		cmd := CommandDiff(&settings.Run{}, ioStreams, "scafctl")
		cmd.SetArgs([]string{fileA, fileB, "-o", "json"})
		_ = cmd.Execute()
	}
}
