// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	mcpserver "github.com/oakwood-commons/scafctl/pkg/mcp"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed list_schema.json
var listSchemaJSON []byte

// ListOptions holds the options for the list command.
type ListOptions struct {
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	Kind      string // "all", "tool", "prompt"
	rootCmd   *cobra.Command

	flags.KvxOutputFlags
}

// CommandList creates the `scafctl mcp list` command.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &ListOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List MCP tools and prompts",
		Long: fmt.Sprintf(`List all tools and prompts exposed by the %s MCP server.

Shows the capabilities that AI agents can discover and invoke when
connected to the MCP server. Each entry includes the name, kind
(tool or prompt), source, and description. Additional fields like
readOnly and destructive are available via -o json or -i.

Use --kind to filter by capability type:
  tool    Show only tools (callable operations)
  prompt  Show only prompts (interaction templates)`, cliParams.BinaryName),
		Example: fmt.Sprintf(`  # List all MCP capabilities
  %[1]s mcp list

  # List only tools
  %[1]s mcp list --kind tool

  # List only prompts
  %[1]s mcp list --kind prompt

  # Output as JSON
  %[1]s mcp list -o json

  # Output as YAML
  %[1]s mcp list -o yaml

  # Filter with CEL expression
  %[1]s mcp list -e '_.filter(c, c.kind == "tool")'`, cliParams.BinaryName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			switch opts.Kind {
			case "all", "tool", "prompt":
				return nil
			default:
				return fmt.Errorf("invalid --kind value %q: must be all, tool, or prompt", opts.Kind)
			}
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.rootCmd = cmd.Root()
			return runList(cmd.Context(), opts)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)
	cmd.Flags().StringVar(&opts.Kind, "kind", "all", "Filter by kind: all, tool, prompt")

	return cmd
}

func runList(ctx context.Context, opts *ListOptions) error {
	srv, err := buildListServer(ctx, opts)
	if err != nil {
		return err
	}

	caps := srv.ListCapabilities()

	// Apply kind filter
	if opts.Kind != "all" && opts.Kind != "" {
		kind := mcpserver.CapabilityKind(opts.Kind)
		filtered := make([]mcpserver.Capability, 0, len(caps))
		for _, c := range caps {
			if c.Kind == kind {
				filtered = append(filtered, c)
			}
		}
		caps = filtered
	}

	columnOrder := []string{"kind", "name", "source", "description"}
	columnHints := map[string]tui.ColumnHint{
		"kind":        {MaxWidth: 8, Priority: 10},
		"name":        {MaxWidth: 40, Priority: 9},
		"source":      {MaxWidth: 8, Priority: 7},
		"description": {Priority: 6},
		"title":       {Hidden: true},
		"readOnly":    {Hidden: true},
		"destructive": {Hidden: true},
	}

	if w := writer.FromContext(ctx); w != nil && w.VerboseEnabled() {
		w.Verbosef("Found %d capabilities (%s filter)", len(caps), opts.Kind)
	}

	kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(opts.CliParams.NoColor),
		kvx.WithOutputAppName(opts.CliParams.BinaryName+" mcp list"),
		kvx.WithIOStreams(opts.IOStreams),
		kvx.WithOutputDisplaySchemaJSON(listSchemaJSON),
		kvx.WithOutputColumnOrder(columnOrder),
		kvx.WithOutputColumnHints(columnHints),
	)

	return kvxOpts.Write(caps)
}

func buildListServer(ctx context.Context, opts *ListOptions) (*mcpserver.Server, error) {
	lgr := logger.FromContext(ctx)
	cfg := config.FromContext(ctx)
	authReg := auth.RegistryFromContext(ctx)

	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		return nil, fmt.Errorf("initializing provider registry: %w", err)
	}

	serverOpts := []mcpserver.ServerOption{
		mcpserver.WithServerLogger(*lgr),
		mcpserver.WithServerRegistry(reg),
		mcpserver.WithServerContext(ctx),
		mcpserver.WithServerVersion(settings.VersionInformation.BuildVersion),
	}
	if s, ok := settings.FromContext(ctx); ok && s.BinaryName != "" {
		serverOpts = append(serverOpts, mcpserver.WithServerName(s.BinaryName))
	}
	if cfg != nil {
		serverOpts = append(serverOpts, mcpserver.WithServerConfig(cfg))
	}
	if authReg != nil {
		serverOpts = append(serverOpts, mcpserver.WithServerAuthRegistry(authReg))
	}
	if opts.rootCmd != nil {
		serverOpts = append(serverOpts, mcpserver.WithRootCommand(opts.rootCmd))
	}

	return mcpserver.NewServer(serverOpts...)
}
