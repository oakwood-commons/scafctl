// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"github.com/spf13/cobra"

	"github.com/oakwood-commons/scafctl/pkg/config"
	kubeapi "github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kube/clusterconfig"
)

// clusterArgCompletion is the ValidArgsFunction for commands that take a single
// optional <cluster> positional argument (kube login / kube logout).
func clusterArgCompletion(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeClusterNames(cmd), cobra.ShellCompDirectiveNoFileComp
}

// completeClusterNames returns cluster names for shell completion of the
// <cluster> argument. It never blocks on network I/O: the stock config-driven
// resolver (whether built here or supplied in context) lists static aliases
// plus already-cached inventory only via ListCached, so a cold dynamic
// inventory is skipped and the shell never stalls on a fetch. Any other
// embedder-supplied context resolver is used via its List (the embedder owns
// its latency). It is best-effort -- any error yields no completions.
func completeClusterNames(cmd *cobra.Command) []string {
	ctx := cmd.Context()

	// Embedder/context resolver wins (present when an embedder wired one). The
	// stock config-driven resolver uses ListCached so a slow or unreachable
	// dynamic inventory can never hang the shell on <TAB>, even if it was wired
	// into the context.
	if r := kubeapi.ResolverFromContext(ctx); r != nil {
		if cr, ok := r.(*clusterconfig.Resolver); ok {
			return clusterNames(cr.ListCached(ctx))
		}
		infos, _ := r.List(ctx)
		return clusterNames(infos)
	}

	// Shell completion runs without the root PersistentPreRun, so the
	// config-driven resolver is built directly from the --config file. Use the
	// non-blocking ListCached so a slow or unreachable dynamic inventory can
	// never hang the shell on <TAB>.
	cfg, err := config.NewManager(configPathFromCmd(cmd)).Load()
	if err != nil {
		return nil
	}
	cr := clusterconfig.New(cfg.Kube.Clusters)
	if !cr.Enabled() {
		return nil
	}
	return clusterNames(cr.ListCached(ctx))
}

// clusterNames extracts the Name field from a slice of ClusterInfos.
func clusterNames(infos []kubeapi.ClusterInfo) []string {
	names := make([]string, 0, len(infos))
	for i := range infos {
		names = append(names, infos[i].Name)
	}
	return names
}

// configPathFromCmd returns the value of the root --config flag, or "" when the
// flag is absent (config then resolves via the default XDG path).
func configPathFromCmd(cmd *cobra.Command) string {
	if f := cmd.Root().Flag("config"); f != nil {
		return f.Value.String()
	}
	return ""
}
