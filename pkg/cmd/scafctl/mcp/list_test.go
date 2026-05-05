// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	mcpserver "github.com/oakwood-commons/scafctl/pkg/mcp"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandList(t *testing.T) {
	t.Run("creates command with correct flags", func(t *testing.T) {
		cliParams := &settings.Run{BinaryName: "scafctl"}
		ioStreams := &terminal.IOStreams{}
		cmd := CommandList(cliParams, ioStreams, "scafctl/mcp")

		assert.Equal(t, "list", cmd.Use)
		assert.Contains(t, cmd.Aliases, "ls")
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)
		assert.NotEmpty(t, cmd.Example)
		assert.True(t, cmd.SilenceUsage)

		// Verify kind flag
		kindFlag := cmd.Flags().Lookup("kind")
		require.NotNil(t, kindFlag)
		assert.Equal(t, "all", kindFlag.DefValue)

		// Verify output flag
		outputFlag := cmd.Flags().Lookup("output")
		require.NotNil(t, outputFlag)
	})

	t.Run("has RunE set", func(t *testing.T) {
		cliParams := &settings.Run{BinaryName: "scafctl"}
		ioStreams := &terminal.IOStreams{}
		cmd := CommandList(cliParams, ioStreams, "scafctl/mcp")

		assert.NotNil(t, cmd.RunE, "expected RunE to be set")
	})

	t.Run("non-default binary name in help text", func(t *testing.T) {
		cliParams := &settings.Run{BinaryName: "mycli"}
		ioStreams := &terminal.IOStreams{}
		cmd := CommandList(cliParams, ioStreams, "mycli/mcp")

		assert.Contains(t, cmd.Long, "mycli")
		assert.Contains(t, cmd.Example, "mycli")
		assert.NotContains(t, cmd.Long, "scafctl")
	})
}

func TestCommandMCPContainsList(t *testing.T) {
	cliParams := &settings.Run{BinaryName: "scafctl"}
	ioStreams := &terminal.IOStreams{}
	cmd := CommandMCP(cliParams, ioStreams, "scafctl")

	var listFound bool
	for _, sub := range cmd.Commands() {
		if sub.Name() == "list" {
			listFound = true
			break
		}
	}
	assert.True(t, listFound, "expected 'list' subcommand")
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	var stdout bytes.Buffer
	ioStreams := terminal.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}}
	cliParams := &settings.Run{BinaryName: "scafctl"}
	w := writer.New(&ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, logger.Get(-1))
	return ctx
}

func TestRunList_JSONOutput(t *testing.T) {
	ctx := testContext(t)

	var outBuf bytes.Buffer
	opts := &ListOptions{
		CliParams: &settings.Run{BinaryName: "scafctl"},
		IOStreams: &terminal.IOStreams{Out: &outBuf, ErrOut: &bytes.Buffer{}},
		Kind:      "all",
	}
	opts.Output = "json"
	opts.FormatExplicit = true

	err := runList(ctx, opts)
	require.NoError(t, err)

	var caps []mcpserver.Capability
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &caps))
	assert.NotEmpty(t, caps)

	// Verify both tools and prompts are present
	var hasTools, hasPrompts bool
	for _, c := range caps {
		if c.Kind == mcpserver.CapabilityTool {
			hasTools = true
		}
		if c.Kind == mcpserver.CapabilityPrompt {
			hasPrompts = true
		}
	}
	assert.True(t, hasTools, "should contain tools")
	assert.True(t, hasPrompts, "should contain prompts")
}

func TestRunList_KindFilter(t *testing.T) {
	ctx := testContext(t)

	var outBuf bytes.Buffer
	opts := &ListOptions{
		CliParams: &settings.Run{BinaryName: "scafctl"},
		IOStreams: &terminal.IOStreams{Out: &outBuf, ErrOut: &bytes.Buffer{}},
		Kind:      "prompt",
	}
	opts.Output = "json"
	opts.FormatExplicit = true

	err := runList(ctx, opts)
	require.NoError(t, err)

	var caps []mcpserver.Capability
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &caps))
	require.NotEmpty(t, caps)

	for _, c := range caps {
		assert.Equal(t, mcpserver.CapabilityPrompt, c.Kind,
			"all results should be prompts when --kind=prompt")
	}
}

func TestRunList_EmbedderBinaryName(t *testing.T) {
	var stdout bytes.Buffer
	ioStreams := terminal.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}}
	cliParams := &settings.Run{BinaryName: "mycli"}
	w := writer.New(&ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, logger.Get(-1))
	ctx = settings.IntoContext(ctx, cliParams)

	var outBuf bytes.Buffer
	opts := &ListOptions{
		CliParams: cliParams,
		IOStreams: &terminal.IOStreams{Out: &outBuf, ErrOut: &bytes.Buffer{}},
		Kind:      "all",
	}
	opts.Output = "json"
	opts.FormatExplicit = true

	err := runList(ctx, opts)
	require.NoError(t, err)

	var caps []mcpserver.Capability
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &caps))
	assert.NotEmpty(t, caps, "should list capabilities with custom binary name")
}

func TestCommandList_InvalidKind(t *testing.T) {
	cliParams := &settings.Run{BinaryName: "scafctl"}
	ioStreams := &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
	cmd := CommandList(cliParams, ioStreams, "scafctl/mcp")
	cmd.SetArgs([]string{"--kind", "invalid"})
	cmd.SilenceErrors = true

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --kind value")
}

func TestRunList_TableOutput(t *testing.T) {
	ctx := testContext(t)

	var outBuf bytes.Buffer
	opts := &ListOptions{
		CliParams: &settings.Run{BinaryName: "scafctl"},
		IOStreams: &terminal.IOStreams{Out: &outBuf, ErrOut: &bytes.Buffer{}},
		Kind:      "all",
	}

	err := runList(ctx, opts)
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, "kind")
	assert.Contains(t, output, "name")
	assert.Contains(t, output, "source")
}

func TestRunList_ToolKindFilter(t *testing.T) {
	ctx := testContext(t)

	var outBuf bytes.Buffer
	opts := &ListOptions{
		CliParams: &settings.Run{BinaryName: "scafctl"},
		IOStreams: &terminal.IOStreams{Out: &outBuf, ErrOut: &bytes.Buffer{}},
		Kind:      "tool",
	}
	opts.Output = "json"
	opts.FormatExplicit = true

	err := runList(ctx, opts)
	require.NoError(t, err)

	var caps []mcpserver.Capability
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &caps))
	require.NotEmpty(t, caps)

	for _, c := range caps {
		assert.Equal(t, mcpserver.CapabilityTool, c.Kind,
			"all results should be tools when --kind=tool")
	}
}

func TestRunList_VerboseOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	cliParams := &settings.Run{BinaryName: "scafctl", Verbose: true}
	w := writer.New(&ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, logger.Get(-1))

	var outBuf bytes.Buffer
	opts := &ListOptions{
		CliParams: cliParams,
		IOStreams: &terminal.IOStreams{Out: &outBuf, ErrOut: &bytes.Buffer{}},
		Kind:      "all",
	}
	opts.Output = "json"
	opts.FormatExplicit = true

	err := runList(ctx, opts)
	require.NoError(t, err)

	assert.Contains(t, stderr.String(), "Found")
	assert.Contains(t, stderr.String(), "capabilities")
}
