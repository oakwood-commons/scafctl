// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"fmt"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandOptions returns a cobra.Command that prints global (persistent) flags
// applicable to all commands, similar to kubectl options.
func CommandOptions(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	return &cobra.Command{
		Use:          "options",
		Short:        "List global command-line options (applies to all commands)",
		Long:         "List global command-line options that are applicable to all commands.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
			ctx := settings.IntoContext(cmd.Context(), cliParams)

			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)
			cmd.SetContext(ctx)

			root := cmd.Root()
			if root == nil {
				return fmt.Errorf("unable to determine root command")
			}

			flags := root.PersistentFlags()
			w.Plainf("The following options can be passed to any command:\n\n")
			w.Plainf("%s", flags.FlagUsages())
			return nil
		},
		Annotations: map[string]string{
			"commandType": "main",
		},
	}
}
