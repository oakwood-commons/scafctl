package secrets

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandDelete creates the 'secrets delete' command.
func CommandDelete(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var forceFlag bool

	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a secret",
		Long:    "Delete a secret by name. Use --force to skip confirmation.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)
			name := args[0]

			// Validate name
			if err := ValidateUserSecretName(name); err != nil {
				return err
			}

			store, err := secrets.New()
			if err != nil {
				return fmt.Errorf("failed to initialize secrets store: %w", err)
			}

			// Check if secret exists
			exists, err := store.Exists(ctx, name)
			if err != nil {
				return fmt.Errorf("failed to check if secret exists: %w", err)
			}

			if !exists {
				w.Warningf("Secret '%s' does not exist\n", name)
				return nil
			}

			// Confirm deletion (skip in force mode, quiet handled by SkipCondition)
			in := input.MustFromContext(ctx)
			skipConfirmation := forceFlag || cliParams.IsQuiet
			confirmed, err := in.Confirm(input.NewConfirmOptions().
				WithPrompt(fmt.Sprintf("Are you sure you want to delete secret '%s'?", name)).
				WithDefault(skipConfirmation). // Default to true when skipping
				WithSkipCondition(skipConfirmation))
			if err != nil {
				return fmt.Errorf("failed to read confirmation: %w", err)
			}
			if !confirmed {
				w.Info("Deletion cancelled")
				return nil
			}

			// Delete the secret
			if err := store.Delete(ctx, name); err != nil {
				return fmt.Errorf("failed to delete secret '%s': %w", name, err)
			}

			w.Successf("Deleted secret '%s'\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Skip confirmation prompt")

	return cmd
}
