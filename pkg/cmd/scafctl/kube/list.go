// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	kubeapi "github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	skvx "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandList creates the 'kube list' subcommand.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags
	outputFlags.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List clusters known to the configured cluster resolver",
		Long: strings.ReplaceAll(heredoc.Doc(`
			List the clusters the configured cluster resolver can resolve by name.

			The list combines static cluster aliases with any dynamic inventory the
			resolver exposes. scafctl ships no cluster data of its own: when no
			resolver is configured the list is empty and an informative message is
			printed.

			Examples:
			  # List known clusters
			  scafctl kube list

			  # Machine-readable output
			  scafctl kube list -o json
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			resolver := kubeapi.ResolverFromContext(ctx)

			opts := flags.ToKvxOutputOptions(&outputFlags,
				skvx.WithIOStreams(ioStreams),
				skvx.WithOutputColumnOrder([]string{"name", "server", "authType", "console"}),
			)

			if resolver == nil {
				// No resolver: still emit a valid empty list for structured
				// formats so machine-readable mode is never broken. Human formats
				// get an informative note on stderr, keyed off the binary name.
				if skvx.IsStructuredFormat(opts.Format) {
					return opts.Write([]map[string]any{})
				}
				w.PlainStderrf("No cluster resolver configured; %s ships no cluster data.", cliParams.BinaryName)
				return nil
			}

			// List is best-effort: a partial result plus an error (for example a
			// reachable static-alias set with an unavailable dynamic inventory)
			// still renders the clusters that resolved, with a warning.
			clusters, err := resolver.List(ctx)
			if err != nil {
				w.WarnStderrf("cluster listing was incomplete: %v", err)
			}

			rows := make([]map[string]any, 0, len(clusters))
			for _, c := range clusters {
				rows = append(rows, map[string]any{
					"name":     c.Name,
					"server":   c.APIServerURL,
					"authType": string(c.AuthType),
					"console":  c.ConsoleURL,
				})
			}

			if skvx.IsStructuredFormat(opts.Format) {
				return opts.Write(rows)
			}

			if len(rows) == 0 {
				w.Infof("No clusters found.")
				return nil
			}
			return opts.Write(rows)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
	return cmd
}
