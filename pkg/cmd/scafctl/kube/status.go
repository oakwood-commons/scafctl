// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	kubelogin "github.com/oakwood-commons/scafctl/pkg/kube/login"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	skvx "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandStatus creates the 'kube status' subcommand.
func CommandStatus(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var (
		kubeconfigPath string
		outputFlags    flags.KvxOutputFlags
	)
	outputFlags.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the current kubeconfig context and cluster",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Show the current kubeconfig context: its cluster, server, namespace, and
			whether scafctl manages the entry (i.e. it was written by "kube login").

			For a scafctl-managed context, the command also runs a best-effort whoami
			to report the authenticated user, using the handler baked into the entry
			and the kubeconfig provider. That step degrades gracefully: when the
			provider or a valid token is unavailable, the static context data is still
			shown. The static view never contacts the cluster and works offline.

			Examples:
			  # Show the current context
			  scafctl kube status

			  # Machine-readable output
			  scafctl kube status -o json
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			mgr := kubeconfig.NewManager(cliParams.BinaryName)
			defer func() { _ = mgr.Close() }()
			deps := kubelogin.Deps{
				Kubeconfig: mgr,
				BinaryName: cliParams.BinaryName,
				HandlerLookup: func(ctx context.Context, name string) (kubelogin.Authenticator, error) {
					return auth.GetHandler(ctx, name)
				},
			}
			st, err := kubelogin.Status(ctx, deps, kubeconfigPath)
			if err != nil {
				w.Errorf("%v", err)
				return err
			}

			opts := flags.ToKvxOutputOptions(&outputFlags,
				skvx.WithIOStreams(ioStreams),
				skvx.WithOutputColumnOrder([]string{"context", "cluster", "server", "namespace", "userEntry", "managed", "identity", "groups"}),
			)

			if st.Context == "" {
				if skvx.IsStructuredFormat(opts.Format) {
					return opts.Write(map[string]any{})
				}
				w.Infof("No current kubeconfig context is set.")
				return nil
			}

			// "userEntry" is the kubeconfig user entry name; "identity" is the
			// authenticated subject from the best-effort whoami. They are distinct.
			// A single object renders as an aligned key/value view in the default
			// format and as a structured object under -o json/yaml. Optional
			// fields are omitted when empty so the view stays clean.
			row := map[string]any{
				"context": st.Context,
				"managed": st.Managed,
			}
			addIfSet := func(key, val string) {
				if val != "" {
					row[key] = val
				}
			}
			addIfSet("cluster", st.Cluster)
			addIfSet("server", st.Server)
			addIfSet("namespace", st.Namespace)
			addIfSet("userEntry", st.User)
			addIfSet("identity", st.Username)
			if len(st.Groups) > 0 {
				row["groups"] = st.Groups
			}
			return opts.Write(row)
		},
	}

	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file (defaults to KUBECONFIG or ~/.kube/config)")
	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
	return cmd
}
