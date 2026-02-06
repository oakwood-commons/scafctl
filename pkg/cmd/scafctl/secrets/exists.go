package secrets

import (
	"fmt"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandExists creates the 'secrets exists' command.
func CommandExists(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exists <name>",
		Short: "Check if a secret exists",
		Long:  "Check if a secret exists. Exits with code 0 if it exists, 1 if not.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)
			name := args[0]

			// Validate name
			if err := ValidateUserSecretName(name); err != nil {
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
				fmt.Fprintln(ioStreams.Out, exists)
			}

			// Set exit code
			if !exists {
				os.Exit(1)
			}

			return nil
		},
	}

	return cmd
}
