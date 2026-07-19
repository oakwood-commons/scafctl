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

const usageInspectSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: usage-test
  version: 2.0.0
  description: Fallback description
  usage:
    synopsis: Do useful things with this solution
    examples:
      - description: Refresh
        command: scafctl run solution -r action=refresh
spec:
  resolvers:
    action:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: action
              default: show
  workflow:
    actions:
      show:
        description: Show summary
        when: '_.action == "show"'
        provider: shell
        inputs:
          command: "echo show"
      refresh:
        description: Refresh data
        when: '_.action == "refresh"'
        provider: shell
        inputs:
          command: "echo refresh"
`

func TestCommandInspectSolution_UsageJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solFile := writeSolution(t, dir, usageInspectSolution)

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspectSolution(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", solFile, "--usage", "-o", "json"})
	cmd.SetOut(out)

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	require.NoError(t, cmd.Execute())

	var usage inspect.UsageInfo
	require.NoError(t, json.Unmarshal(out.Bytes(), &usage))

	assert.Equal(t, "usage-test", usage.Name)
	assert.Equal(t, "2.0.0", usage.Version)
	assert.Equal(t, "Do useful things with this solution", usage.Synopsis)
	assert.Equal(t, "scafctl run solution", usage.Run)
	require.Len(t, usage.Params, 1)
	assert.Equal(t, "action", usage.Params[0].Name)
	assert.Equal(t, "show", usage.Params[0].Default)
	assert.ElementsMatch(t, []any{"refresh", "show"}, usage.Params[0].AllowedValues)
	assert.Len(t, usage.Actions, 2)
	assert.Len(t, usage.Examples, 1)
}

func TestCommandInspectSolution_UsageText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solFile := writeSolution(t, dir, usageInspectSolution)

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspectSolution(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"-f", solFile, "--usage"})
	cmd.SetOut(out)

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	require.NoError(t, cmd.Execute())

	got := out.String()
	assert.Contains(t, got, "usage-test (2.0.0)")
	assert.Contains(t, got, "Do useful things with this solution")
	assert.Contains(t, got, "PARAMETERS")
	assert.Contains(t, got, "values: refresh, show")
	assert.Contains(t, got, "ACTIONS")
	assert.Contains(t, got, "scafctl run solution -r action=refresh")
	assert.Contains(t, got, "EXAMPLES")
}

func TestCommandInspect(t *testing.T) {
	t.Parallel()
	_, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandInspect(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "inspect", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)
	assert.True(t, cmd.SilenceUsage)

	// The group wires up the solution subcommand.
	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "solution")

	// Embedder-safe: a non-default binary name appears in the examples.
	cmd2 := CommandInspect(cliParams, ioStreams, "mycli")
	assert.Contains(t, cmd2.Example, "mycli inspect solution")
}
