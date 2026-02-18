// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	mcpserver "github.com/oakwood-commons/scafctl/pkg/mcp"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// ServeOptions holds the options for the serve command.
type ServeOptions struct {
	Transport string
	LogFile   string
	Info      bool
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandServe creates the `scafctl mcp serve` command.
func CommandServe(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &ServeOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server",
		Long: heredoc.Doc(`
			Start the Model Context Protocol (MCP) server for AI agent integration.

			The MCP server exposes scafctl capabilities as tools that AI agents can discover
			and invoke programmatically. It communicates over stdio (JSON-RPC 2.0) by default.

			Example VS Code configuration (.vscode/mcp.json):

			  {
			    "servers": {
			      "scafctl": {
			        "type": "stdio",
			        "command": "scafctl",
			        "args": ["mcp", "serve"]
			      }
			    }
			  }

			Use --info to print the server's capabilities and exit (useful for debugging):

			  scafctl mcp serve --info
		`),
		Example: heredoc.Doc(`
			# Start the MCP server (stdio transport)
			scafctl mcp serve

			# Print server capabilities as JSON and exit
			scafctl mcp serve --info

			# Start with file-based logging for debugging
			scafctl mcp serve --log-file /tmp/scafctl-mcp.log
		`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.Transport, "transport", "stdio", "Transport protocol (stdio)")
	cmd.Flags().StringVar(&opts.LogFile, "log-file", "", "Write server logs to file (default: stderr)")
	cmd.Flags().BoolVar(&opts.Info, "info", false, "Print server capabilities as JSON and exit")

	return cmd
}

func runServe(ctx context.Context, opts *ServeOptions) error {
	// Get dependencies from context (injected by PersistentPreRun)
	lgr := logger.FromContext(ctx)

	// Config may be nil if no config file exists
	cfg := config.FromContext(ctx)

	// Auth registry from context
	authReg := auth.RegistryFromContext(ctx)

	// Load the built-in provider registry (http, cel, file, exec, etc.)
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		return fmt.Errorf("initializing provider registry: %w", err)
	}

	// Build server options
	serverOpts := []mcpserver.ServerOption{
		mcpserver.WithServerLogger(*lgr),
		mcpserver.WithServerRegistry(reg),
		mcpserver.WithServerContext(ctx),
		mcpserver.WithServerVersion(settings.VersionInformation.BuildVersion),
	}
	if cfg != nil {
		serverOpts = append(serverOpts, mcpserver.WithServerConfig(cfg))
	}
	if authReg != nil {
		serverOpts = append(serverOpts, mcpserver.WithServerAuthRegistry(authReg))
	}

	// Create server
	srv, err := mcpserver.NewServer(serverOpts...)
	if err != nil {
		return fmt.Errorf("creating MCP server: %w", err)
	}

	// --info: print capabilities and exit
	if opts.Info {
		info, err := srv.Info()
		if err != nil {
			return fmt.Errorf("getting server info: %w", err)
		}
		fmt.Fprintln(os.Stdout, string(info))
		return nil
	}

	// Start serving
	lgr.Info("starting MCP server", "transport", opts.Transport)
	return srv.Serve()
}
