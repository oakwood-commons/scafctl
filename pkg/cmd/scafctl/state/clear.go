// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandClear creates the 'state clear' command.
func CommandClear(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear all state values",
		Long:  "Remove all stored parameters, immutables, and fingerprints from a state file, preserving metadata.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			sd, err := state.LoadFromFile(path)
			if err != nil {
				err := fmt.Errorf("failed to load state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Verify the file actually exists (LoadFromFile returns empty for non-existent).
			resolved, resolveErr := state.ResolveStatePath(path)
			if resolveErr != nil {
				w.Errorf("%v", resolveErr)
				return exitcode.WithCode(resolveErr, exitcode.InvalidInput)
			}
			if _, statErr := os.Stat(resolved); os.IsNotExist(statErr) {
				err := fmt.Errorf("state file not found: %s", resolved)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}

			count := len(sd.Parameters) + len(sd.Immutables) + len(sd.Fingerprints)
			sd.Parameters = make(map[string]any)
			sd.Immutables = make(map[string]*state.ImmutableEntry)
			sd.Fingerprints = make(map[string]*state.FingerprintEntry)

			if err := state.SaveToFile(path, sd); err != nil {
				err := fmt.Errorf("failed to save state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			w.Successf("Cleared %d entries\n", count)
			return nil
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "State file path (relative to state directory)")
	_ = cmd.MarkFlagRequired("path")

	return cmd
}
