// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth/hostname"
	"github.com/oakwood-commons/scafctl/pkg/config"
	kubeapi "github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kube/clusterconfig"
)

// cmdWithContext returns a child command under a root that carries a --config
// flag, with the given context set, mirroring the real command tree.
func cmdWithContext(t *testing.T, ctx context.Context, configPath string) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "scafctl"}
	root.PersistentFlags().String("config", configPath, "")
	child := &cobra.Command{Use: "login"}
	root.AddCommand(child)
	child.SetContext(ctx)
	return child
}

func TestCompleteClusterNames_ContextResolverWins(t *testing.T) {
	t.Parallel()

	resolver := &kubeapi.MockResolver{
		ListResult: []kubeapi.ClusterInfo{{Name: "lab"}, {Name: "prod"}},
	}
	ctx := kubeapi.WithResolver(context.Background(), resolver)
	cmd := cmdWithContext(t, ctx, "")

	assert.Equal(t, []string{"lab", "prod"}, completeClusterNames(cmd))
}

func TestCompleteClusterNames_ConfigResolverInContextUsesCache(t *testing.T) {
	t.Parallel()

	// A stock config-driven resolver wired into the context must be completed
	// via ListCached (aliases + already-cached inventory only), never List, so
	// <TAB> can never stall on a dynamic-inventory fetch.
	fetched := false
	deps := hostname.Deps{
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
			fetched = true
			return nil, nil
		},
	}
	cr := clusterconfig.New(config.ClusterResolutionConfig{
		Aliases: map[string]config.ClusterAlias{"lab": {Server: "https://api.lab.example.com:6443"}},
		Resolver: &config.HostnameResolverConfig{
			Source:    config.HostnameResolverSource{URL: "https://clusters.example.com"},
			Transform: "_",
		},
	}, clusterconfig.WithDeps(deps))
	ctx := kubeapi.WithResolver(context.Background(), cr)
	cmd := cmdWithContext(t, ctx, "")

	assert.Equal(t, []string{"lab"}, completeClusterNames(cmd))
	assert.False(t, fetched, "completion of a config resolver must not trigger a network fetch")
}

func TestCompleteClusterNames_ConfigFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	seed := `kube:
  clusters:
    aliases:
      lab:
        server: https://api.lab.example.com:6443
      prod:
        server: https://api.prod.example.com:6443
`
	require.NoError(t, os.WriteFile(configPath, []byte(seed), 0o600))

	// No resolver in context: completion must load the config directly.
	cmd := cmdWithContext(t, context.Background(), configPath)

	names := completeClusterNames(cmd)
	assert.ElementsMatch(t, []string{"lab", "prod"}, names)
}

func TestCompleteClusterNames_NoResolverNoConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cmd := cmdWithContext(t, context.Background(), filepath.Join(dir, "missing.yaml"))

	assert.Empty(t, completeClusterNames(cmd),
		"no context resolver and no kube.clusters config must yield no completions")
}

func TestClusterArgCompletion(t *testing.T) {
	t.Parallel()

	resolver := &kubeapi.MockResolver{ListResult: []kubeapi.ClusterInfo{{Name: "lab"}}}
	ctx := kubeapi.WithResolver(context.Background(), resolver)
	cmd := cmdWithContext(t, ctx, "")

	t.Run("no arg yet completes cluster names", func(t *testing.T) {
		t.Parallel()
		names, directive := clusterArgCompletion(cmd, nil, "")
		assert.Equal(t, []string{"lab"}, names)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	})

	t.Run("second arg gets no completion", func(t *testing.T) {
		t.Parallel()
		names, directive := clusterArgCompletion(cmd, []string{"lab"}, "")
		assert.Nil(t, names)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	})
}
