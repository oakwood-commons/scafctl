// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandSet creates the 'state set' command.
func CommandSet(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		path      string
		key       string
		value     string
		valueType string
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set a state value",
		Long:  "Set or update a value in a state file.",
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

			// Check if entry is immutable
			if existing, ok := sd.Values[key]; ok && existing.Immutable {
				err := fmt.Errorf("key %q is immutable and cannot be modified", key)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			sd.Values[key] = &state.Entry{
				Value:     value,
				Type:      valueType,
				UpdatedAt: time.Now().UTC(),
			}

			if err := state.SaveToFile(path, sd); err != nil {
				err := fmt.Errorf("failed to save state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			w.Successf("Set key %q\n", key)
			return nil
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "State file path (relative to state directory)")
	cmd.Flags().StringVar(&key, "key", "", "Key to set")
	cmd.Flags().StringVar(&value, "value", "", "Value to store")
	cmd.Flags().StringVar(&valueType, "type", "string", "Value type (string, int, bool, etc.)")
	_ = cmd.MarkFlagRequired("path")
	_ = cmd.MarkFlagRequired("key")
	_ = cmd.MarkFlagRequired("value")

	return cmd
}
