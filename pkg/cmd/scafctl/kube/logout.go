// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	kubeapi "github.com/oakwood-commons/scafctl/pkg/kube"
	kubelogin "github.com/oakwood-commons/scafctl/pkg/kube/login"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	skvx "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandLogout creates the top-level 'logout' command.
func CommandLogout(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var (
		handlerName     string
		clusterName     string
		contextName     string
		userName        string
		kubeconfigPath  string
		keepCredentials bool
		outputFlags     flags.KvxOutputFlags
	)
	outputFlags.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:   "logout [cluster]",
		Short: "Remove a cluster's kubeconfig entry and clear credentials",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Remove a cluster's kubeconfig entry and clear the cached credentials.

			When --handler is provided, that handler's cached tokens are revoked
			unless --keep-credentials is set. Without --handler, the cluster's
			default handler (supplied by the resolver) is revoked instead, so
			logout clears the same credentials that login obtained. Use
			--keep-credentials to remove only the kubeconfig entry (for example to
			log out of the cluster while staying authenticated for other clusters).

			Examples:
			  # Log out of a cluster and revoke its credentials
			  scafctl kube logout prod

			  # Revoke a specific handler's credentials
			  scafctl kube logout prod --handler oidc

			  # Remove the kubeconfig entry but keep cached credentials
			  scafctl kube logout prod --keep-credentials
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage:      true,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: clusterArgCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			var cluster string
			if len(args) == 1 {
				cluster = args[0]
			}

			deps := kubelogin.Deps{
				Kubeconfig: kubeconfig.NewManager(cliParams.BinaryName),
				Resolver:   kubeapi.ResolverFromContext(ctx),
				BinaryName: cliParams.BinaryName,
				HandlerLookup: func(ctx context.Context, name string) (kubelogin.Authenticator, error) {
					return auth.GetHandler(ctx, name)
				},
			}
			if handlerName != "" {
				handler, err := auth.GetHandler(ctx, handlerName)
				if err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(fmt.Errorf("resolve auth handler %q: %w", handlerName, err), exitcode.InvalidInput)
				}
				deps.Handler = handler
			}

			res, err := kubelogin.Logout(ctx, deps, kubelogin.LogoutRequest{
				Cluster:         cluster,
				ClusterName:     clusterName,
				ContextName:     contextName,
				UserName:        userName,
				KubeconfigPath:  kubeconfigPath,
				KeepCredentials: keepCredentials,
			})
			if err != nil {
				w.Errorf("%v", err)
				if errors.Is(err, kubelogin.ErrNoCluster) {
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Report the entry that was actually targeted: a --cluster-name (or
			// --context) invocation has no positional cluster argument, so fall
			// back to those so the output is never blank.
			effectiveCluster := cluster
			if effectiveCluster == "" {
				effectiveCluster = clusterName
			}
			if effectiveCluster == "" {
				effectiveCluster = contextName
			}
			return renderLogoutResult(ioStreams, &outputFlags, w, effectiveCluster, res)
		},
	}

	cmd.Flags().StringVar(&handlerName, "handler", "", "Auth handler whose credentials to revoke (defaults to the cluster's configured handler)")
	cmd.Flags().StringVar(&clusterName, "cluster-name", "", "kubeconfig cluster entry name (defaults to the cluster)")
	cmd.Flags().StringVar(&contextName, "context", "", "kubeconfig context name (defaults to the cluster name)")
	cmd.Flags().StringVar(&userName, "user", "", "kubeconfig user entry name (defaults to the cluster name)")
	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file (defaults to KUBECONFIG or ~/.kube/config)")
	cmd.Flags().BoolVar(&keepCredentials, "keep-credentials", false, "Leave the handler's cached credentials in place")
	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)

	return cmd
}

// renderLogoutResult writes the logout result as structured output or a human
// summary.
func renderLogoutResult(ioStreams *terminal.IOStreams, outputFlags *flags.KvxOutputFlags, w *writer.Writer, cluster string, res *kubelogin.LogoutResult) error {
	row := map[string]any{
		"cluster":  cluster,
		"removed":  res.Removed,
		"fallback": res.UsedFallback,
	}
	opts := flags.ToKvxOutputOptions(outputFlags,
		skvx.WithIOStreams(ioStreams),
		skvx.WithOutputColumnOrder([]string{"cluster", "removed", "fallback"}),
	)
	if skvx.IsStructuredFormat(opts.Format) {
		return opts.Write([]map[string]any{row})
	}

	if res.Removed {
		w.Success("Removed kubeconfig entry")
	} else {
		w.Infof("No matching kubeconfig entry found")
	}
	if res.UsedFallback {
		w.Warningf("kubeconfig provider unavailable; edited the kubeconfig directly")
	}
	return nil
}
