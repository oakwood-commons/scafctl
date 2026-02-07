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
		Use:          "resolver",
		Short:        "Resolver operations (deprecated - use render solution)",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			NOTE: Most resolver commands have been moved to the render command:
			
			  scafctl render solution --graph           # View dependency graph
			  scafctl render solution --snapshot        # Save execution snapshot
			
			This command will be removed in a future version.
		`),
		Example: heredoc.Docf(`
			# View dependency graph (new way)
			$ %s render solution -f config.yaml --graph
			
			# Generate Graphviz DOT format (new way)  
			$ %s render solution -f config.yaml --graph --graph-format=dot | dot -Tpng > graph.png
		`, binaryName, binaryName),
		Deprecated: "use 'render solution --graph' instead",
	}

	// Keep the graph subcommand for backward compatibility but mark as deprecated
	graphCmd := CommandGraph(cliParams, ioStreams, binaryName)
	graphCmd.Deprecated = "use 'render solution --graph' instead"
	cmd.AddCommand(graphCmd)

	return cmd
}
