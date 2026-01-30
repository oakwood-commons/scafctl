package render

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandRender creates the 'render' command that renders artifacts for external execution.
func CommandRender(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:     "render",
		Aliases: []string{"r"},
		Short:   fmt.Sprintf("Renders %s artifacts for external execution", settings.CliBinaryName),
		Long: `Render produces executor-ready artifacts from solutions.

The render command outputs action graphs as JSON or YAML that can be consumed
by external execution engines. This enables decoupled architecture where
scafctl handles resolution and graph building while external systems handle
actual execution.

OUTPUT FORMATS:
  json    JSON output (default) - compact, machine-readable
  yaml    YAML output - human-readable

The rendered artifact includes:
  - Action definitions with materialized inputs
  - Deferred expressions (referencing __actions) preserved for runtime
  - Execution order phases for parallel execution
  - Dependency information for each action
  - Metadata including generation timestamp and statistics`,
		SilenceUsage: true,
	}

	cCmd.AddCommand(CommandWorkflow(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))

	return cCmd
}
