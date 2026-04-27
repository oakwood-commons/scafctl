// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/cmdinfo"
)

// registerCLITools registers CLI introspection tools.
func (s *Server) registerCLITools() {
	if s.rootCmd == nil {
		return
	}

	getCommandHelpTool := mcp.NewTool("get_command_help",
		mcp.WithDescription(fmt.Sprintf(
			"Get structured help for %s CLI commands. "+
				"Without a command parameter, returns the list of all available commands. "+
				"With a command path (e.g. \"run solution\"), returns full help including "+
				"description, usage, flags, examples, and subcommands.",
			s.name,
		)),
		mcp.WithTitleAnnotation("Get Command Help"),
		mcp.WithToolIcons(toolIcons["help"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("command",
			mcp.Description("Space-separated command path (e.g. \"run solution\", \"catalog list\"). Omit to list all commands."),
		),
	)
	s.mcpServer.AddTool(getCommandHelpTool, s.handleGetCommandHelp)
}

// handleGetCommandHelp returns structured CLI help.
func (s *Server) handleGetCommandHelp(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.rootCmd == nil {
		return mcp.NewToolResultError("CLI introspection not available: root command not configured"), nil
	}

	commandPath := req.GetString("command", "")

	// No command specified: return top-level command list.
	if commandPath == "" {
		commands := cmdinfo.CollectCommands(s.rootCmd, false)
		return mcp.NewToolResultJSON(commands)
	}

	// Find the specific command.
	cmd := cmdinfo.FindCommand(s.rootCmd, commandPath)
	if cmd == nil {
		return mcp.NewToolResultError(fmt.Sprintf("command not found: %q", commandPath)), nil
	}

	detail := cmdinfo.GetCommandDetail(cmd)
	return mcp.NewToolResultJSON(detail)
}
