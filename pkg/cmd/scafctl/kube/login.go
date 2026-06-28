// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package kube provides the 'kube' command group and its 'login' / 'logout'
// subcommands that authenticate to a Kubernetes / OpenShift cluster and write a
// kubeconfig exec-credential entry. The commands are thin wiring around the
// pkg/kube/login domain package.
package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

// CommandLogin creates the top-level 'login' command.
func CommandLogin(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var (
		handlerName    string
		server         string
		audience       string
		clusterName    string
		contextName    string
		userName       string
		kubeconfigPath string
		profile        string
		setCurrent     bool
		insecure       bool
		verify         bool
		timeout        time.Duration
		outputFlags    flags.KvxOutputFlags
	)
	outputFlags.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:   "login [cluster]",
		Short: "Authenticate to a Kubernetes or OpenShift cluster",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Authenticate to a Kubernetes or OpenShift cluster and write a kubeconfig
			entry that mints credentials on demand.

			The command runs an auth handler's interactive login, then writes a
			kubeconfig user entry whose exec block invokes this binary as a client-go
			credential plugin. Subsequent kubectl/oc calls obtain fresh tokens without
			re-running login.

			The cluster argument is resolved through the configured cluster resolver,
			which also supplies the default auth handler. Pass --handler to override it,
			or --server to target a cluster directly without a resolver.

			Examples:
			  # Login to a resolver-known cluster (handler comes from the resolver)
			  scafctl kube login prod

			  # Override the handler the resolver would choose
			  scafctl kube login prod --handler oidc

			  # Login directly to an API server (no resolver; handler required)
			  scafctl kube login --handler oidc --server https://api.example.com:6443

			  # Login, make the new context current, and verify the identity
			  scafctl kube login prod --current --verify
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
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
			req := kubelogin.Request{
				Cluster:           cluster,
				Handler:           handlerName,
				Server:            server,
				Audience:          audience,
				ClusterName:       clusterName,
				ContextName:       contextName,
				UserName:          userName,
				KubeconfigPath:    kubeconfigPath,
				Profile:           profile,
				SetCurrentContext: setCurrent,
				InsecureSkipTLS:   insecure,
				Verify:            verify,
				Timeout:           timeout,
			}

			res, err := kubelogin.Login(ctx, deps, req)
			if err != nil {
				w.Errorf("%v", err)
				if errors.Is(err, kubelogin.ErrNoHandler) || errors.Is(err, kubelogin.ErrNoServer) {
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				if errors.Is(err, kubelogin.ErrVerificationFailed) && res != nil {
					w.Warningf("kubeconfig entry %q was written; only identity verification failed", res.ContextName)
				}
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			return renderLoginResult(ioStreams, &outputFlags, w, res)
		},
	}

	cmd.Flags().StringVar(&handlerName, "handler", "", "Auth handler to authenticate with (defaults to the cluster's configured handler)")
	cmd.Flags().StringVar(&server, "server", "", "API server URL (overrides the resolved cluster)")
	cmd.Flags().StringVar(&audience, "audience", "", "OIDC audience the minted token must target")
	cmd.Flags().StringVar(&clusterName, "cluster-name", "", "kubeconfig cluster entry name (defaults to the cluster)")
	cmd.Flags().StringVar(&contextName, "context", "", "kubeconfig context name (defaults to the cluster name)")
	cmd.Flags().StringVar(&userName, "user", "", "kubeconfig user entry name (defaults to the cluster name)")
	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file (defaults to KUBECONFIG or ~/.kube/config)")
	cmd.Flags().StringVar(&profile, "profile", "", "Auth profile baked into the exec credential args")
	cmd.Flags().BoolVar(&setCurrent, "current", false, "Set the new context as the current context")
	cmd.Flags().BoolVar(&insecure, "insecure-skip-tls-verify", false, "Disable API server TLS verification (development only)")
	cmd.Flags().BoolVar(&verify, "verify", false, "Verify the authenticated identity via a post-login whoami (requires the kubeconfig provider)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Login timeout (e.g. 5m); zero uses the handler default")
	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)

	return cmd
}

// renderLoginResult writes the login result as structured output or a human
// summary.
func renderLoginResult(ioStreams *terminal.IOStreams, outputFlags *flags.KvxOutputFlags, w *writer.Writer, res *kubelogin.Result) error {
	row := map[string]any{
		"context":    res.ContextName,
		"server":     res.Server,
		"kubeconfig": res.KubeconfigPath,
		"authType":   string(res.AuthType),
		"handler":    res.Handler,
		"user":       res.Username,
		"fallback":   res.UsedFallback,
	}
	opts := flags.ToKvxOutputOptions(outputFlags,
		skvx.WithIOStreams(ioStreams),
		skvx.WithOutputColumnOrder([]string{"context", "server", "kubeconfig", "authType", "handler", "user", "fallback"}),
	)
	if skvx.IsStructuredFormat(opts.Format) {
		return opts.Write([]map[string]any{row})
	}

	w.Successf("Logged in to %q", res.ContextName)
	w.Infof("Kubeconfig: %s", res.KubeconfigPath)
	if res.Username != "" {
		w.Infof("User: %s", res.Username)
	}
	if res.UsedFallback {
		w.Warningf("kubeconfig provider unavailable; wrote a static kubeconfig entry")
	}
	return nil
}
