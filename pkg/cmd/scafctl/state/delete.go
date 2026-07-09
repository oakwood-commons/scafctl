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

// CommandDelete creates the 'state delete' command.
func CommandDelete(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		path  string
		key   string
		force bool
	)

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a state key",
		Long:  "Remove a specific key from a state file (parameters or persisted resolvers).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			cwd, err := os.Getwd()
			if err != nil {
				err := fmt.Errorf("cannot determine working directory: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			sd, err := state.LoadFromFile(path, cwd)
			if err != nil {
				err := fmt.Errorf("failed to load state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Verify the file actually exists (LoadFromFile returns empty for non-existent).
			resolved, resolveErr := state.ResolveStatePath(path, cwd)
			if resolveErr != nil {
				w.Errorf("%v", resolveErr)
				return exitcode.WithCode(resolveErr, exitcode.InvalidInput)
			}
			if _, statErr := os.Stat(resolved); os.IsNotExist(statErr) {
				err := fmt.Errorf("state file not found: %s", resolved)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}

			// Check where the key exists
			_, inParams := sd.Parameters[key]
			resEntry, inResolvers := sd.Resolvers[key]
			isImmutable := inResolvers && resEntry.Immutable

			if !inParams && !inResolvers {
				err := fmt.Errorf("key %q not found in state", key)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// If the key is immutable, require --force
			if isImmutable && !force {
				err := fmt.Errorf("key %q is immutable; deleting it will cause the next run to generate a new value. Use --force to confirm", key)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Delete from both maps in one pass
			if inParams {
				delete(sd.Parameters, key)
			}
			if inResolvers {
				delete(sd.Resolvers, key)
			}

			if err := state.SaveToFile(path, cwd, sd); err != nil {
				err := fmt.Errorf("failed to save state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			switch {
			case inParams && isImmutable:
				w.Successf("Deleted parameter and immutable key %q\n", key)
			case inParams && inResolvers:
				w.Successf("Deleted parameter and persisted key %q\n", key)
			case isImmutable:
				w.Successf("Deleted immutable key %q\n", key)
			case inResolvers:
				w.Successf("Deleted persisted key %q\n", key)
			default:
				w.Successf("Deleted parameter %q\n", key)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "State file path (relative to working directory or absolute)")
	cmd.Flags().StringVar(&key, "key", "", "Key to delete")
	cmd.Flags().BoolVar(&force, "force", false, "Force deletion of immutable keys")
	_ = cmd.MarkFlagRequired("path")
	_ = cmd.MarkFlagRequired("key")

	return cmd
}
