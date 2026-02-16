// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandDelete creates the 'secrets delete' command.
func CommandDelete(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		forceFlag bool
		allFlag   bool
	)

	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a secret",
		Long:    "Delete a secret by name. Use --force to skip confirmation.\nUse --all to allow deleting internal secrets (e.g. auth tokens).",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)
			name := args[0]

			// Validate name
			if err := ValidateSecretName(name, allFlag); err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			store, err := secrets.New()
			if err != nil {
				err := fmt.Errorf("failed to initialize secrets store: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}

			// Check if secret exists
			exists, err := store.Exists(ctx, name)
			if err != nil {
				err := fmt.Errorf("failed to check if secret exists: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			if !exists {
				w.Warningf("Secret '%s' does not exist\n", name)
				return nil
			}

			// Warn about internal secret deletion
			if IsInternalSecret(name) {
				w.Warning("⚠️  WARNING: This is an internal secret managed by scafctl.")
				w.Warning("   Deleting it may break authentication or other functionality.")
			}

			// Confirm deletion (skip in force mode, quiet handled by SkipCondition)
			in := input.MustFromContext(ctx)
			skipConfirmation := forceFlag || cliParams.IsQuiet
			confirmed, err := in.Confirm(input.NewConfirmOptions().
				WithPrompt(fmt.Sprintf("Are you sure you want to delete secret '%s'?", name)).
				WithDefault(skipConfirmation). // Default to true when skipping
				WithSkipCondition(skipConfirmation))
			if err != nil {
				err := fmt.Errorf("failed to read confirmation: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
			if !confirmed {
				w.Info("Deletion cancelled")
				return nil
			}

			// Delete the secret
			if err := store.Delete(ctx, name); err != nil {
				err := fmt.Errorf("failed to delete secret '%s': %w", name, err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			w.Successf("Deleted secret '%s'\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Skip confirmation prompt")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Include internal secrets (e.g. auth tokens)")

	return cmd
}
