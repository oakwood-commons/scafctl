package secrets

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// listOptions holds the options for the list command.
type listOptions struct {
	flags.KvxOutputFlags
}

// CommandList creates the 'secrets list' command.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all user secrets",
		Long:  "List the names of all stored secrets (excluding internal scafctl.* secrets).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)

			store, err := secrets.New()
			if err != nil {
				err := fmt.Errorf("failed to initialize secrets store: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}

			names, err := store.List(ctx)
			if err != nil {
				err := fmt.Errorf("failed to list secrets: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Filter out internal secrets
			userSecrets := FilterUserSecrets(names)

			// Prepare output with kvx flags
			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))

			// Convert to slice of maps for table output
			data := make([]map[string]interface{}, len(userSecrets))
			for i, name := range userSecrets {
				data[i] = map[string]interface{}{
					"name": name,
				}
			}

			if err := kvxOpts.Write(data); err != nil {
				return err
			}

			if len(userSecrets) == 0 && !cliParams.IsQuiet {
				w.Warning("No secrets found")
			}

			return nil
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}
