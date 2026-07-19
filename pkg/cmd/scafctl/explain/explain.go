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
// schema documentation for resource kinds (from Go struct tags). Instance
// views live under 'inspect' (e.g. 'inspect solution').
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
		Short:   "Show the schema of a resource kind (fields, types, validation)",
		Long: fmt.Sprintf(`Show the schema of a resource KIND -- the fields, types, and validation
rules available when writing that kind of YAML. The information is extracted
from Go struct tags.

Note: 'explain' describes a KIND (a type), not a specific file. To inspect an
actual solution file, use 'inspect solution'; for how to run it, use
'inspect solution --usage'.

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
  scafctl explain resolver

See also:
  inspect solution          Structure/metadata of a specific solution file
  inspect solution --usage  Parameters and how to run a solution
  get                       List resources or show one by name`, strings.Join(kindNames, ", ")),
		SilenceUsage: true,
	}

	// Add the schema browser as the default command behavior
	schemaCmd := CommandSchema(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use))

	// Copy the schema command's behavior onto the parent, but show help when
	// invoked with no arguments instead of erroring on the missing kind.
	schemaRunE := schemaCmd.RunE
	cCmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return c.Help()
		}
		return schemaRunE(c, args)
	}
	cCmd.Args = cobra.ArbitraryArgs
	cCmd.ValidArgsFunction = schemaCmd.ValidArgsFunction

	// Copy flags from schema command
	cCmd.Flags().AddFlagSet(schemaCmd.Flags())

	return cCmd
}
