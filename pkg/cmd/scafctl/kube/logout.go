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
		all             bool
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

			mgr := kubeconfig.NewManager(cliParams.BinaryName)
			defer func() { _ = mgr.Close() }()
			deps := kubelogin.Deps{
				Kubeconfig: mgr,
				Resolver:   kubeapi.ResolverFromContext(ctx),
				BinaryName: cliParams.BinaryName,
				// Mirror the login auto-routing policy so logout revokes the same
				// handler an auto-routed login selected when the resolved cluster
				// carries an AuthType but no explicit DefaultHandler.
				AuthTypeHandlers: kubelogin.DefaultAuthTypeHandlers(),
				HandlerLookup: func(ctx context.Context, name string) (kubelogin.Authenticator, error) {
					return auth.GetHandler(ctx, name)
				},
			}

			if all {
				if cluster != "" {
					w.Errorf("--all cannot be combined with a cluster argument")
					return exitcode.WithCode(fmt.Errorf("--all cannot be combined with a cluster argument"), exitcode.InvalidInput)
				}
				res, err := kubelogin.LogoutAll(ctx, deps, kubelogin.LogoutAllRequest{
					KubeconfigPath: kubeconfigPath,
				})
				if err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				return renderLogoutAllResult(ioStreams, &outputFlags, w, cliParams.BinaryName, res)
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
	cmd.Flags().BoolVar(&all, "all", false, fmt.Sprintf("Remove every %s-managed kubeconfig entry (credentials are left in place)", cliParams.BinaryName))
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
		w.WarnStderrf("kubeconfig provider unavailable; edited the kubeconfig directly")
	}
	return nil
}

// renderLogoutAllResult writes the outcome of a "logout --all" as structured
// output or a human summary. binaryName keys the human strings so embedder CLIs
// never leak the default brand.
func renderLogoutAllResult(ioStreams *terminal.IOStreams, outputFlags *flags.KvxOutputFlags, w *writer.Writer, binaryName string, res *kubelogin.LogoutAllResult) error {
	rows := make([]map[string]any, 0, len(res.Removed))
	for _, ctxName := range res.Removed {
		rows = append(rows, map[string]any{"context": ctxName, "removed": true})
	}
	opts := flags.ToKvxOutputOptions(outputFlags,
		skvx.WithIOStreams(ioStreams),
		skvx.WithOutputColumnOrder([]string{"context", "removed"}),
	)
	if skvx.IsStructuredFormat(opts.Format) {
		return opts.Write(rows)
	}

	if len(res.Removed) == 0 {
		w.Infof("No %s-managed kubeconfig entries found", binaryName)
	} else {
		w.Successf("Removed %d %s-managed kubeconfig entr%s", len(res.Removed), binaryName, plural(len(res.Removed)))
	}
	if res.UsedFallback {
		w.WarnStderrf("kubeconfig provider unavailable; edited the kubeconfig directly")
	}
	return nil
}

// plural returns the pluralizing suffix for count "entry"/"entries".
func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
