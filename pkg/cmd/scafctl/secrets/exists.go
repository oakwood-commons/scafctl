// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandExists creates the 'secrets exists' command.
func CommandExists(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var allFlag bool

	cmd := &cobra.Command{
		Use:   "exists <name>",
		Short: "Check if a secret exists",
		Long:  "Check if a secret exists. Exits with code 0 if it exists, 1 if not.\nUse --all to check internal secrets (e.g. auth tokens).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}
			name := args[0]

			// Validate name
			if err := secrets.ValidateSecretName(name, allFlag); err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			store, err := secrets.New()
			if err != nil {
				err := fmt.Errorf("failed to initialize secrets store: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}

			exists, err := store.Exists(ctx, name)
			if err != nil {
				err := fmt.Errorf("failed to check if secret exists: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Print result
			if !cliParams.IsQuiet {
				w.Plainlnf("%v", exists)
			}

			// Set exit code
			if !exists {
				return exitcode.WithCode(fmt.Errorf("secret %q does not exist", name), exitcode.GeneralError)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Include internal secrets (e.g. auth tokens)")

	return cmd
}
