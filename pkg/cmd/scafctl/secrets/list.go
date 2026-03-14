// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

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
	All bool
}

// CommandList creates the 'secrets list' command.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List secrets",
		Long:  "List the names of all stored secrets. By default, internal scafctl.* secrets are excluded. Use --all to include them.",
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

			names, err := store.List(ctx)
			if err != nil {
				err := fmt.Errorf("failed to list secrets: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Filter secrets based on --all flag
			filtered := secrets.FilterSecrets(names, opts.All)

			// Prepare output with kvx flags
			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))

			// Convert to slice of maps for table output
			data := make([]map[string]interface{}, len(filtered))
			for i, name := range filtered {
				data[i] = map[string]interface{}{
					"name": name,
					"type": secrets.SecretType(name),
				}
			}

			if err := kvxOpts.Write(data); err != nil {
				return err
			}

			if len(filtered) == 0 && !cliParams.IsQuiet {
				w.Warning("No secrets found")
			}

			return nil
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)
	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "Include internal secrets (e.g. auth tokens)")

	return cmd
}
