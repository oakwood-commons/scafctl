package resolver

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandResolver creates the resolver command
func CommandResolver(cliParams *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolver",
		Short: "Resolver operations",
		Long: heredoc.Doc(`
			Manage and visualize resolver configurations.
			
			Resolvers are the core abstraction for discovering and transforming configuration values.
			This command provides tools for visualizing dependencies, analyzing execution plans,
			and validating resolver definitions.
		`),
		Example: heredoc.Docf(`
			# Visualize resolver dependencies as ASCII art
			$ %s resolver graph config.yaml --format ascii
			
			# Generate DOT format for Graphviz
			$ %s resolver graph config.yaml --format dot | dot -Tpng > graph.png
			
			# Generate Mermaid diagram
			$ %s resolver graph config.yaml --format mermaid
			
			# Export as JSON for programmatic analysis
			$ %s resolver graph config.yaml --format json
		`, binaryName, binaryName, binaryName, binaryName),
	}

	cmd.AddCommand(CommandGraph(cliParams, ioStreams, binaryName))

	return cmd
}
