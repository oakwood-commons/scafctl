// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/cmdinfo"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTestCommandTree() *cobra.Command {
	root := &cobra.Command{Use: "mycli", Short: "Test CLI"}

	run := &cobra.Command{Use: "run", Short: "Run things"}
	sol := &cobra.Command{Use: "solution", Short: "Run a solution", Aliases: []string{"sol"}}
	sol.Flags().StringP("file", "f", "", "Solution file path")
	run.AddCommand(sol)

	catalog := &cobra.Command{Use: "catalog", Short: "Catalog commands"}
	list := &cobra.Command{Use: "list", Short: "List artifacts"}
	catalog.AddCommand(list)

	root.AddCommand(run, catalog)
	return root
}

func TestHandleGetCommandHelp_ListAll(t *testing.T) {
	t.Parallel()

	root := buildTestCommandTree()
	srv, err := NewServer(
		WithServerVersion("test"),
		WithRootCommand(root),
	)
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "get_command_help"
	request.Params.Arguments = map[string]any{}

	result, err := srv.handleGetCommandHelp(context.Background(), request)
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := result.Content[0].(mcp.TextContent).Text
	var commands []cmdinfo.CommandInfo
	require.NoError(t, json.Unmarshal([]byte(text), &commands))
	assert.NotEmpty(t, commands)

	// Should contain top-level commands
	names := make(map[string]bool)
	for _, c := range commands {
		names[c.Name] = true
	}
	assert.True(t, names["mycli run"])
	assert.True(t, names["mycli catalog"])
	assert.True(t, names["mycli run solution"])
}

func TestHandleGetCommandHelp_SpecificCommand(t *testing.T) {
	t.Parallel()

	root := buildTestCommandTree()
	srv, err := NewServer(
		WithServerVersion("test"),
		WithRootCommand(root),
	)
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "get_command_help"
	request.Params.Arguments = map[string]any{
		"command": "run solution",
	}

	result, err := srv.handleGetCommandHelp(context.Background(), request)
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := result.Content[0].(mcp.TextContent).Text
	var detail cmdinfo.CommandDetail
	require.NoError(t, json.Unmarshal([]byte(text), &detail))
	assert.Equal(t, "mycli run solution", detail.Name)
	assert.Equal(t, "Run a solution", detail.Short)
	assert.Equal(t, []string{"sol"}, detail.Aliases)
	assert.NotEmpty(t, detail.Flags)
}

func TestHandleGetCommandHelp_NotFound(t *testing.T) {
	t.Parallel()

	root := buildTestCommandTree()
	srv, err := NewServer(
		WithServerVersion("test"),
		WithRootCommand(root),
	)
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "get_command_help"
	request.Params.Arguments = map[string]any{
		"command": "nonexistent",
	}

	result, err := srv.handleGetCommandHelp(context.Background(), request)
	require.NoError(t, err)
	require.True(t, result.IsError)
}

func TestHandleGetCommandHelp_NoRootCommand(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "get_command_help"
	request.Params.Arguments = map[string]any{}

	result, err := srv.handleGetCommandHelp(context.Background(), request)
	require.NoError(t, err)
	require.True(t, result.IsError)
}

func TestRegisterCLITools_SkipsWhenNoRootCmd(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)
	// registerCLITools is called during NewServer — just verify no panic
	assert.Nil(t, srv.rootCmd)
}
