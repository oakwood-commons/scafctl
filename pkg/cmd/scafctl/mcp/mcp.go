// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandMCP creates the `scafctl mcp` parent command.
func CommandMCP(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "mcp",
		Short:        "MCP (Model Context Protocol) server for AI agent integration",
		Long:         `Manage the MCP server that exposes scafctl capabilities to AI agents like GitHub Copilot, Claude, and Cursor.`,
		SilenceUsage: true,
	}
	cmd.AddCommand(CommandServe(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cmd.Use)))
	return cmd
}
