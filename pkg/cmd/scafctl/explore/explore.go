// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package explore wires the cobra-explorer interactive TUI into scafctl as the
// "explore" subcommand.
package explore

import (
	explorer "github.com/oakwood-commons/cobra-explorer/explore"
	"github.com/spf13/cobra"
)

// CommandExplore returns the "explore" subcommand: an interactive TUI for
// browsing the command tree, inspecting flags, and building commands visually.
//
// It wraps github.com/oakwood-commons/cobra-explorer, wiring the resolved binary
// name so embedders see their own CLI name in the TUI, and enabling in-process
// execution so a command assembled in the builder can be run directly.
//
// root must be the fully-assembled root command; the explorer introspects it
// read-only at run time, so it reflects every subcommand registered on root.
func CommandExplore(root *cobra.Command, binaryName string) *cobra.Command {
	return explorer.NewCommand(root,
		explorer.WithBinaryName(binaryName),
		explorer.WithExecution(true),
	)
}
