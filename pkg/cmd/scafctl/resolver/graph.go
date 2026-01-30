package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// GraphOptions holds options for the graph command
type GraphOptions struct {
	ConfigFile   string
	Format       string
	ShowStats    bool
	ShowMetadata bool
}

// CommandGraph creates the resolver graph command
func CommandGraph(_ *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &GraphOptions{}

	cmd := &cobra.Command{
		Use:   "graph [config-file]",
		Short: "Visualize resolver dependency graph",
		Long: heredoc.Doc(`
			Visualize the dependency graph of resolvers in a configuration file.
			
			The graph shows how resolvers depend on each other, execution phases,
			and parallelization opportunities. This is useful for understanding
			resolver execution order and identifying circular dependencies.
			
			Supported output formats:
			  - ascii: Human-readable ASCII art (default)
			  - dot: Graphviz DOT format (pipe to 'dot' command)
			  - mermaid: Mermaid diagram syntax
			  - json: Machine-readable JSON format
		`),
		Example: heredoc.Docf(`
			# Show ASCII dependency graph
			$ %s resolver graph config.yaml
			
			# Generate PNG image using Graphviz
			$ %s resolver graph config.yaml --format dot | dot -Tpng > graph.png
			
			# Generate SVG using Graphviz
			$ %s resolver graph config.yaml --format dot | dot -Tsvg > graph.svg
			
			# Show Mermaid diagram (copy to mermaid.live)
			$ %s resolver graph config.yaml --format mermaid
			
			# Export as JSON for automation
			$ %s resolver graph config.yaml --format json | jq '.stats'
			
			# Include detailed statistics
			$ %s resolver graph config.yaml --stats
		`, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ConfigFile = args[0]
			return runGraph(cmd.Context(), opts, ioStreams)
		},
	}

	cmd.Flags().StringVarP(&opts.Format, "format", "f", "ascii", "Output format: ascii, dot, mermaid, json")
	cmd.Flags().BoolVar(&opts.ShowStats, "stats", false, "Show detailed statistics")
	cmd.Flags().BoolVar(&opts.ShowMetadata, "metadata", false, "Include resolver metadata in output")

	return cmd
}

func runGraph(ctx context.Context, opts *GraphOptions, ioStreams terminal.IOStreams) error {
	lgr := logger.FromContext(ctx)

	// Read and parse config file
	lgr.V(-1).Info("reading config file", "file", opts.ConfigFile)
	data, err := os.ReadFile(opts.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse resolver configuration
	var config struct {
		Resolvers []*resolver.Resolver `yaml:"resolvers" json:"resolvers"`
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	if len(config.Resolvers) == 0 {
		return fmt.Errorf("no resolvers found in config file")
	}

	lgr.V(-1).Info("building dependency graph", "resolvers", len(config.Resolvers))

	// Build dependency graph (nil lookup since we don't have registry access for visualization)
	graph, err := resolver.BuildGraph(config.Resolvers, nil)
	if err != nil {
		return fmt.Errorf("failed to build dependency graph: %w", err)
	}

	lgr.V(-1).Info("graph built successfully",
		"nodes", len(graph.Nodes),
		"phases", len(graph.Phases),
		"maxParallelism", graph.Stats.MaxParallelism)

	// Render based on format
	switch opts.Format {
	case "ascii":
		if err := graph.RenderASCII(ioStreams.Out); err != nil {
			return fmt.Errorf("failed to render ASCII graph: %w", err)
		}

	case "dot":
		if err := graph.RenderDOT(ioStreams.Out); err != nil {
			return fmt.Errorf("failed to render DOT graph: %w", err)
		}

	case "mermaid":
		if err := graph.RenderMermaid(ioStreams.Out); err != nil {
			return fmt.Errorf("failed to render Mermaid graph: %w", err)
		}

	case "json":
		encoder := json.NewEncoder(ioStreams.Out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(graph); err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}

	default:
		return fmt.Errorf("unsupported format: %s (supported: ascii, dot, mermaid, json)", opts.Format)
	}

	// Show stats if requested (for non-ASCII formats)
	if opts.ShowStats && opts.Format != "ascii" {
		fmt.Fprintf(ioStreams.Out, "\nStatistics:\n")
		fmt.Fprintf(ioStreams.Out, "  Total Resolvers: %d\n", graph.Stats.TotalResolvers)
		fmt.Fprintf(ioStreams.Out, "  Total Phases: %d\n", graph.Stats.TotalPhases)
		fmt.Fprintf(ioStreams.Out, "  Max Parallelism: %d\n", graph.Stats.MaxParallelism)
		fmt.Fprintf(ioStreams.Out, "  Avg Dependencies: %.2f\n", graph.Stats.AvgDependencies)
	}

	return nil
}
