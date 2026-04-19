// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"

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
		Long:  "Remove all stored values from a state file, preserving metadata.",
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

			count := len(sd.Values)
			sd.Values = make(map[string]*state.Entry)

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
