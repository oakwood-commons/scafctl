// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmdinfo"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
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

func buildTestRoot(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	root := &cobra.Command{Use: binaryName, Short: "Test CLI"}

	run := &cobra.Command{Use: "run", Short: "Run things"}
	run.AddCommand(&cobra.Command{Use: "solution", Short: "Run a solution"})

	get := &cobra.Command{Use: "get", Short: "Get things"}
	get.AddCommand(&cobra.Command{Use: "provider", Short: "List providers"})
	get.AddCommand(CommandCommands(cliParams, ioStreams, binaryName))

	root.AddCommand(run, get)
	return root
}

func TestCommandCommands_JSONOutput(t *testing.T) {
	t.Parallel()

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	root := buildTestRoot(cliParams, ioStreams, "scafctl")

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	root.SetContext(ctx)
	root.SetOut(out)
	root.SetArgs([]string{"get", "commands", "-o", "json"})

	err := root.Execute()
	require.NoError(t, err)

	var commands []cmdinfo.CommandInfo
	require.NoError(t, json.Unmarshal(out.Bytes(), &commands))

	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.Name)
	}
	assert.Contains(t, names, "scafctl run solution")
	assert.Contains(t, names, "scafctl get provider")
}

func TestCommandCommands_LeafOnly(t *testing.T) {
	t.Parallel()

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	root := buildTestRoot(cliParams, ioStreams, "scafctl")

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	root.SetContext(ctx)
	root.SetOut(out)
	root.SetArgs([]string{"get", "commands", "--leaf", "-o", "json"})

	err := root.Execute()
	require.NoError(t, err)

	var commands []cmdinfo.CommandInfo
	require.NoError(t, json.Unmarshal(out.Bytes(), &commands))

	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.Name)
	}
	// Leaf commands should appear
	assert.Contains(t, names, "scafctl run solution")
	assert.Contains(t, names, "scafctl get provider")

	// Parent commands should not
	assert.NotContains(t, names, "scafctl run")
	assert.NotContains(t, names, "scafctl get")
}

func TestCommandCommands_QuietOutput(t *testing.T) {
	t.Parallel()

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	root := buildTestRoot(cliParams, ioStreams, "scafctl")

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	root.SetContext(ctx)
	root.SetOut(out)
	root.SetArgs([]string{"get", "commands", "-o", "quiet"})

	err := root.Execute()
	require.NoError(t, err)
	assert.Empty(t, out.String())
}

func TestCommandCommands_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	out, ioStreams := makeIOStreams()
	cliParams := settings.NewCliParams()

	root := buildTestRoot(cliParams, ioStreams, "mycli")

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	root.SetContext(ctx)
	root.SetOut(out)
	root.SetArgs([]string{"get", "commands", "-o", "json"})

	err := root.Execute()
	require.NoError(t, err)

	var commands []cmdinfo.CommandInfo
	require.NoError(t, json.Unmarshal(out.Bytes(), &commands))

	// All command names should use the custom binary name
	for _, c := range commands {
		assert.Contains(t, c.Name, "mycli", "command name should use custom binary name")
	}
}

func BenchmarkCommandCommands_JSON(b *testing.B) {
	cliParams := settings.NewCliParams()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		out := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			In:     os.Stdin,
			Out:    out,
			ErrOut: &bytes.Buffer{},
		}

		root := buildTestRoot(cliParams, ioStreams, "scafctl")

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		root.SetContext(ctx)
		root.SetOut(out)
		root.SetArgs([]string{"get", "commands", "-o", "json"})

		_ = root.Execute()
	}
}
