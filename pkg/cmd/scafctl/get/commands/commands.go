// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	_ "embed"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/cmdinfo"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed commands_schema.json
var commandsSchemaJSON []byte

// CommandSummary is the table-friendly output for command listing.
type CommandSummary struct {
	Name      string `json:"name" yaml:"name" required:"true"`
	Short     string `json:"short" yaml:"short"`
	Group     string `json:"group" yaml:"group"`
	Aliases   string `json:"aliases" yaml:"aliases"`
	FlagCount int    `json:"flagCount" yaml:"flagCount"`
}

// Options holds options for the get commands command.
type Options struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	BinaryName string
	LeafOnly   bool

	// kvx output integration
	flags.KvxOutputFlags
}

// CommandCommands creates the 'get commands' subcommand.
func CommandCommands(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &Options{}

	cmd := &cobra.Command{
		Use:     "commands",
		Aliases: []string{"cmds"},
		Short:   "List all available commands with structured output",
		Long: heredoc.Docf(`
			List all available CLI commands with full kvx output support.

			By default, lists all commands (parent and leaf). Use --leaf to
			show only actionable commands (those without subcommands).

			Supports all kvx output formats including interactive TUI mode
			for searchable command discovery.
		`, binaryName),
		Example: heredoc.Docf(`
			# List all commands as a table
			$ %[1]s get commands

			# List only leaf (actionable) commands
			$ %[1]s get commands --leaf

			# Interactive command explorer
			$ %[1]s get commands -i

			# JSON output for scripting
			$ %[1]s get commands -o json

			# Filter commands by group
			$ %[1]s get commands -o json -w '_.group == "core"'

			# Tree view of command hierarchy
			$ %[1]s get commands -o tree
		`, binaryName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.BinaryName = binaryName

			ctx := cmd.Context()
			if writer.FromContext(ctx) == nil {
				w := writer.New(ioStreams, cliParams)
				ctx = writer.WithWriter(ctx, w)
			}

			// Walk the root command tree at execution time
			root := cmd.Root()
			commands := cmdinfo.CollectCommands(root, opts.LeafOnly)

			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
				kvx.WithOutputContext(ctx),
				kvx.WithOutputNoColor(cliParams.NoColor),
				kvx.WithOutputAppName(binaryName+" get commands"),
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputDisplaySchemaJSON(commandsSchemaJSON),
				kvx.WithOutputColumnOrder([]string{"name", "short"}),
				kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
					"name":      {MaxWidth: 35, Priority: 10},
					"short":     {DisplayName: "description"},
					"group":     {Hidden: true},
					"aliases":   {Hidden: true},
					"flagCount": {Hidden: true},
				}),
			)

			// Full data for json/yaml and interactive TUI, flat summary for table
			if opts.Output == "json" || opts.Output == "yaml" || opts.Interactive {
				return kvxOpts.Write(commands)
			}

			summaries := make([]CommandSummary, 0, len(commands))
			for _, c := range commands {
				summaries = append(summaries, CommandSummary{
					Name:      c.Name,
					Short:     c.Short,
					Group:     c.Group,
					Aliases:   strings.Join(c.Aliases, ", "),
					FlagCount: c.FlagCount,
				})
			}
			return kvxOpts.Write(summaries)
		},
		SilenceUsage: true,
	}

	cmd.Flags().BoolVar(&opts.LeafOnly, "leaf", false, "Show only leaf commands (actionable, no subcommands)")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}
