// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandRotate creates the 'secrets rotate' command.
func CommandRotate(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate master encryption key",
		Long: `Rotate the master encryption key used to encrypt all secrets.

This operation:
  1. Decrypts all secrets with the current master key
  2. Generates a new master key
  3. Re-encrypts all secrets with the new key
  4. Updates the keyring with the new master key

All secrets (including internal scafctl.* secrets) are rotated.

If any step fails, the operation is rolled back and the original
master key remains in use.

This is useful for:
  - Periodic key rotation for security compliance
  - After suspected key compromise
  - Before sharing a device or system`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			store, err := newStoreFromContext(ctx)
			if err != nil {
				err := fmt.Errorf("failed to initialize secrets store: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}

			// List secrets first to show what will be rotated
			names, err := store.List(ctx)
			if err != nil {
				err := fmt.Errorf("failed to list secrets: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Confirm rotation
			if len(names) == 0 {
				w.Info("No secrets to rotate, but the master key will be rotated.")
			} else {
				w.Infof("This will rotate the master encryption key and re-encrypt %d secret(s):\n", len(names))
				for _, name := range names {
					w.Infof("  - %s\n", name)
				}
			}

			in := input.FromContext(ctx)
			if in == nil {
				return fmt.Errorf("input not initialized in context")
			}
			confirmed, err := in.Confirm(input.NewConfirmOptions().
				WithPrompt("Continue?").
				WithDefault(false).
				WithSkipCondition(cliParams.IsQuiet))
			if err != nil {
				err := fmt.Errorf("failed to read confirmation: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
			if !confirmed {
				w.Info("Rotation cancelled")
				return nil
			}

			w.Info("Rotating master key...")

			// Perform rotation
			if err := store.Rotate(ctx); err != nil {
				err := fmt.Errorf("rotation failed: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			w.Successf("Master key rotated successfully (%d secret(s) re-encrypted)\n", len(names))

			return nil
		},
	}

	return cmd
}
