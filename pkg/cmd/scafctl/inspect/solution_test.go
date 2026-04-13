// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeIOStreams() (*bytes.Buffer, *terminal.IOStreams) {
	out := &bytes.Buffer{}
	return out, &terminal.IOStreams{
		In:     os.Stdin,
		Out:    out,
		ErrOut: &bytes.Buffer{},
	}
}

func writeSolution(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

const inspectSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: inspect-test
  version: 1.0.0
  description: A solution for inspect testing
  tags:
    - test
    - example
spec:
  resolvers:
    app_name:
      description: Application name
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              prompt: "Enter app name"
    region:
      description: Deployment region
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "us-east-1"
  workflow:
    actions:
      deploy:
        provider: shell
        inputs:
          command: "echo deploying"
`

func TestCommandInspectSolution_JSONOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solFile := writeSolution(t, dir, inspectSolution)

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspectSolution(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", solFile, "-o", "json"})
	cmd.SetOut(out)

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	err := cmd.Execute()
	require.NoError(t, err)

	var result Result
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))

	assert.Equal(t, "inspect-test", result.Name)
	assert.Equal(t, "1.0.0", result.Version)
	assert.Equal(t, "A solution for inspect testing", result.Description)
	assert.True(t, result.HasWorkflow)
	assert.True(t, result.HasResolvers)
	assert.Len(t, result.Resolvers, 2)
	assert.Len(t, result.Actions, 1)
	assert.NotEmpty(t, result.RunCommand)
	assert.Contains(t, result.RunCommand, "run solution")
	assert.Len(t, result.Parameters, 1)
	assert.Equal(t, "app_name", result.Parameters[0].Name)
	assert.Equal(t, []string{"test", "example"}, result.Tags)
}

func TestCommandInspectSolution_YAMLOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solFile := writeSolution(t, dir, inspectSolution)

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspectSolution(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", solFile, "-o", "yaml"})
	cmd.SetOut(out)

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "name: inspect-test")
	assert.Contains(t, output, "version: 1.0.0")
	assert.Contains(t, output, "hasWorkflow: true")
}

func TestCommandInspectSolution_QuietOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solFile := writeSolution(t, dir, inspectSolution)

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspectSolution(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", solFile, "-o", "quiet"})
	cmd.SetOut(out)

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Empty(t, out.String())
}

func TestCommandInspectSolution_InvalidFile(t *testing.T) {
	t.Parallel()

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspectSolution(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", "/nonexistent/solution.yaml"})
	cmd.SetOut(out)

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandInspectSolution_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solFile := writeSolution(t, dir, inspectSolution)

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspectSolution(cliParams, ioStreams, "mycli")
	cmd.SetArgs([]string{"-f", solFile, "-o", "json"})
	cmd.SetOut(out)

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	err := cmd.Execute()
	require.NoError(t, err)

	var result Result
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))

	assert.Contains(t, result.RunCommand, "mycli run solution")
}

func TestCommandInspectSolution_ResolverOnlySolution(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	resolverOnly := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-only
  version: 1.0.0
spec:
  resolvers:
    data:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "hello"
`
	solFile := writeSolution(t, dir, resolverOnly)

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspectSolution(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", solFile, "-o", "json"})
	cmd.SetOut(out)

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	err := cmd.Execute()
	require.NoError(t, err)

	var result Result
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))

	assert.False(t, result.HasWorkflow)
	assert.True(t, result.HasResolvers)
	assert.Contains(t, result.RunCommand, "run resolver")
}

func TestCommandInspectSolution_CatalogArg(t *testing.T) {
	t.Parallel()

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspectSolution(cliParams, ioStreams, "scafctl")
	// Using a file path as positional arg should fail validation
	cmd.SetArgs([]string{"./local-file.yaml"})
	cmd.SetOut(out)

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	err := cmd.Execute()
	require.Error(t, err, "local file paths must use -f/--file")
}

func TestBuildInspectResult(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(inspectSolution), 0o644))

	ctx := context.Background()
	sol, err := inspect.LoadSolution(ctx, solFile)
	require.NoError(t, err)

	exp := inspect.BuildSolutionExplanation(sol)
	result := buildInspectResult(exp, sol, solFile, "scafctl")

	assert.Equal(t, "inspect-test", result.Name)
	assert.Equal(t, "1.0.0", result.Version)
	assert.True(t, result.HasWorkflow)
	assert.True(t, result.HasResolvers)
	assert.NotEmpty(t, result.RunCommand)
	assert.Len(t, result.Parameters, 1)
}

func BenchmarkCommandInspectSolution_JSON(b *testing.B) {
	dir := b.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(b, os.WriteFile(solFile, []byte(inspectSolution), 0o644))

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		out := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			In:     os.Stdin,
			Out:    out,
			ErrOut: &bytes.Buffer{},
		}
		cliParams := settings.NewCliParams()
		cmd := CommandInspectSolution(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"-f", solFile, "-o", "json"})
		cmd.SetOut(out)

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		cmd.SetContext(ctx)

		_ = cmd.Execute()
	}
}
