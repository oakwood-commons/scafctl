// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandList creates the 'auth list' command.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered auth handlers",
		Long: heredoc.Doc(`
			List all registered authentication handlers and their metadata.

			Displays handler name, display name, supported authentication flows,
			and capabilities for each registered handler. This is useful for
			discovering which handlers are available and what features they support.

			Examples:
			  # List all auth handlers
			  scafctl auth list

			  # Output as JSON
			  scafctl auth list -o json

			  # Output as YAML
			  scafctl auth list -o yaml
		`),
		Aliases:      []string{"ls"},
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)

			handlerNames := listHandlers(ctx)
			if len(handlerNames) == 0 {
				err := fmt.Errorf("no auth handlers registered")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			results := make([]map[string]any, 0, len(handlerNames))

			for _, name := range handlerNames {
				handler, err := getHandler(ctx, name)
				if err != nil {
					w.Warningf("Failed to initialize %s: %v", name, err)
					continue
				}

				flows := handler.SupportedFlows()
				flowStrs := make([]string, len(flows))
				for i, f := range flows {
					flowStrs[i] = string(f)
				}

				capabilities := handler.Capabilities()
				capStrs := make([]string, len(capabilities))
				for i, c := range capabilities {
					capStrs[i] = string(c)
				}

				result := map[string]any{
					"name":         handler.Name(),
					"displayName":  handler.DisplayName(),
					"flows":        strings.Join(flowStrs, ", "),
					"capabilities": strings.Join(capStrs, ", "),
				}

				results = append(results, result)
			}

			if len(results) == 0 {
				err := fmt.Errorf("no auth handlers could be loaded")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			outputOpts := flags.NewKvxOutputOptionsFromFlags(
				outputFlags.Output,
				outputFlags.Interactive,
				outputFlags.Expression,
				kvx.WithOutputContext(ctx),
				kvx.WithOutputNoColor(cliParams.NoColor),
				kvx.WithOutputAppName("scafctl auth list"),
			)
			outputOpts.IOStreams = ioStreams

			return outputOpts.Write(results)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
	return cmd
}

// flowsToStrings converts a slice of Flow to a slice of strings.
func flowsToStrings(flows []auth.Flow) []string {
	strs := make([]string, len(flows))
	for i, f := range flows {
		strs[i] = string(f)
	}
	return strs
}

// capabilitiesToStrings converts a slice of Capability to a slice of strings.
func capabilitiesToStrings(caps []auth.Capability) []string {
	strs := make([]string, len(caps))
	for i, c := range caps {
		strs[i] = string(c)
	}
	return strs
}
