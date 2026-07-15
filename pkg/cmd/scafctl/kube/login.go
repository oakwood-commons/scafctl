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
	"github.com/oakwood-commons/scafctl/pkg/auth/loginui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/aliassave"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	kubeapi "github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kube/clusterconfig"
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
		namespace      string
		kubeconfigPath string
		profile        string
		setCurrent     bool
		insecure       bool
		verify         bool
		refresh        bool
		saveAlias      string
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

			// --save-alias needs a non-empty name to key the persisted alias.
			// Fail fast, before login, so the user isn't logged in only to have
			// the alias silently dropped.
			if cmd.Flags().Changed("save-alias") && strings.TrimSpace(saveAlias) == "" {
				err := fmt.Errorf("--save-alias requires a non-empty alias name")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			mgr := kubeconfig.NewManager(cliParams.BinaryName)
			defer func() { _ = mgr.Close() }()
			deps := kubelogin.Deps{
				Kubeconfig: mgr,
				Resolver:   kubeapi.ResolverFromContext(ctx),
				BinaryName: cliParams.BinaryName,
				HandlerLookup: func(ctx context.Context, name string) (kubelogin.Authenticator, error) {
					return auth.GetHandler(ctx, name)
				},
			}
			// Present the shared interactive login TUI (used by 'auth login')
			// when stdout is a terminal and the output is not structured or
			// quiet. Piped, non-TTY, -o json/table, or -o quiet output keeps the
			// plain line-based path.
			if skvx.IsTerminal(ioStreams.Out) && loginTUIEligibleFormat(outputFlags.Output) {
				deps.LoginRunner = func(ctx context.Context, h kubelogin.Authenticator, opts auth.LoginOptions) (*auth.Result, error) {
					if handler, ok := h.(auth.Handler); ok {
						return loginui.RunLogin(ctx, w, cliParams.BinaryName, handler, opts)
					}
					return h.Login(ctx, opts)
				}
			}
			req := kubelogin.Request{
				Cluster:           cluster,
				Handler:           handlerName,
				Server:            server,
				Audience:          audience,
				ClusterName:       clusterName,
				ContextName:       contextName,
				UserName:          userName,
				Namespace:         namespace,
				KubeconfigPath:    kubeconfigPath,
				Profile:           profile,
				SetCurrentContext: setCurrent,
				InsecureSkipTLS:   insecure,
				Verify:            verify,
				Refresh:           refresh,
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

			if !res.SupportsPerClusterTokens {
				w.WarnStderrf(
					"handler %q does not support per-cluster token routing; logging into "+
						"multiple clusters with it may return the wrong token. Upgrade the handler to enable multi-cluster.",
					res.Handler,
				)
			}

			// --save-alias: remember this cluster under a short name by persisting
			// a static ClusterAlias built from the resolved connection details.
			if saveAlias != "" {
				saveClusterAlias(cmd, w, saveAlias, res)
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
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Default namespace set on the written context (overrides the resolved cluster)")
	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file (defaults to KUBECONFIG or ~/.kube/config)")
	cmd.Flags().StringVar(&profile, "profile", "", "Auth profile baked into the exec credential args")
	cmd.Flags().BoolVar(&setCurrent, "current", false, "Set the new context as the current context")
	cmd.Flags().BoolVar(&insecure, "insecure-skip-tls-verify", false, "Disable API server TLS verification (development only)")
	cmd.Flags().BoolVar(&verify, "verify", false, "Verify the authenticated identity via a post-login whoami (requires the kubeconfig provider)")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Re-apply resolved cluster details (CA, server, namespace) without a fresh interactive login when a valid token is cached")
	cmd.Flags().StringVar(&saveAlias, "save-alias", "", "After login, remember this cluster under this short name (persists a kube.clusters alias)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Login timeout (e.g. 5m); zero uses the handler default")
	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)

	return cmd
}

// renderLoginResult writes the login result as structured output or a human
// summary.
func renderLoginResult(ioStreams *terminal.IOStreams, outputFlags *flags.KvxOutputFlags, w *writer.Writer, res *kubelogin.Result) error {
	row := map[string]any{
		"context":          res.ContextName,
		"server":           res.Server,
		"kubeconfig":       res.KubeconfigPath,
		"authType":         string(res.AuthType),
		"handler":          res.Handler,
		"user":             res.Username,
		"fallback":         res.UsedFallback,
		"perClusterTokens": res.SupportsPerClusterTokens,
	}
	opts := flags.ToKvxOutputOptions(outputFlags,
		skvx.WithIOStreams(ioStreams),
		skvx.WithOutputColumnOrder([]string{"context", "server", "kubeconfig", "authType", "handler", "user", "fallback", "perClusterTokens"}),
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

// saveClusterAlias persists the resolved cluster as a static alias under
// kube.clusters.aliases so a later `kube login <alias>` resolves it. The login
// already succeeded, so any problem here only warns.
func saveClusterAlias(cmd *cobra.Command, w *writer.Writer, name string, res *kubelogin.Result) {
	alias := clusterconfig.InfoToAlias(res.ResolvedCluster)
	if alias.Server == "" {
		w.WarnStderrf("could not save alias %q: the login produced no resolved server", name)
		return
	}
	// Persist the handler actually used so `kube login <alias>` works without
	// --handler, even when the resolver supplied no default.
	if alias.DefaultHandler == "" {
		alias.DefaultHandler = res.Handler
	}
	aliassave.Persist(w, configPathFromCmd(cmd), name, func(cfg *config.Config) bool {
		if cfg.Kube.Clusters.Aliases == nil {
			cfg.Kube.Clusters.Aliases = map[string]config.ClusterAlias{}
		}
		_, existed := cfg.Kube.Clusters.Aliases[name]
		cfg.Kube.Clusters.Aliases[name] = alias
		return existed
	})
}

// loginTUIEligibleFormat reports whether the requested output format permits the
// interactive login TUI. Structured (json/yaml/csv/toml/mermaid) and quiet
// formats keep the plain line-based path; auto/table/list are eligible. An
// unrecognized format parses as auto and is treated as eligible. The caller
// additionally gates on stdout being a terminal.
func loginTUIEligibleFormat(output string) bool {
	format, _ := skvx.ParseOutputFormat(output)
	return !skvx.IsStructuredFormat(format) && !skvx.IsQuietFormat(format)
}
