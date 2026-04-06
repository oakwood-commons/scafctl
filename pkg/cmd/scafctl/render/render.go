// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandRender creates the 'render' command that renders artifacts for external execution.
func CommandRender(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:     "render",
		Aliases: []string{"rn"},
		Short:   fmt.Sprintf("Renders %s artifacts for external execution", path),
		Long: strings.ReplaceAll(`Render produces executor-ready artifacts from solutions.

The render command outputs action graphs as JSON or YAML that can be consumed
by external execution engines. This enables decoupled architecture where
scafctl handles resolution and graph building while external systems handle
actual execution.

SUBCOMMANDS:
  solution  Render a solution's action graph, dependency graph, or snapshot

The rendered artifact includes:
  - Action definitions with materialized inputs
  - Deferred expressions (referencing __actions) preserved for runtime
  - Execution order phases for parallel execution
  - Dependency information for each action
  - Metadata including generation timestamp and statistics`, settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
	}

	cCmd.AddCommand(CommandSolution(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))

	return cCmd
}
