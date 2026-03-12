// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandExplain creates the 'explain' command which provides detailed
// documentation about resource schemas and solution/provider instances.
func CommandExplain(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	// Get available kinds for documentation
	reg, err := schema.GetGlobalRegistry()
	var kindNames []string
	if err == nil {
		kindNames = reg.Names()
	}
	sort.Strings(kindNames)

	cCmd := &cobra.Command{
		Use:     "explain <kind>[.field.path]",
		Aliases: []string{"exp"},
		Short:   "Show schema documentation for resource kinds",
		Long: fmt.Sprintf(`Show detailed schema documentation for resource kinds.

This command displays the struct definition, field types, validation rules,
and documentation extracted from Go struct tags. Use it to understand what
fields are available and their constraints when writing YAML configurations.

AVAILABLE KINDS:
  %s

Examples:
  # Show the Provider Descriptor schema
  scafctl explain provider

  # Drill into the schema field
  scafctl explain provider.schema

  # Show all fields in Action schema
  scafctl explain action --recursive

  # Show Resolver schema
  scafctl explain resolver`, strings.Join(kindNames, ", ")),
		SilenceUsage: true,
	}

	// Add the schema browser as the default command behavior
	schemaCmd := CommandSchema(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use))

	// Copy schema command's RunE to the parent command
	cCmd.RunE = schemaCmd.RunE
	cCmd.Args = schemaCmd.Args
	cCmd.ValidArgsFunction = schemaCmd.ValidArgsFunction

	// Copy flags from schema command
	cCmd.Flags().AddFlagSet(schemaCmd.Flags())

	// Add subcommands for specific resource types
	// Note: We don't add CommandProvider here because it would shadow the "provider" schema lookup
	cCmd.AddCommand(CommandSolution(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))

	return cCmd
}
