// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"testing"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	kubeapi "github.com/oakwood-commons/scafctl/pkg/kube"
	kubelogin "github.com/oakwood-commons/scafctl/pkg/kube/login"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// isolateKubeConfig points the config dir at a temp location so save-alias
// writes never touch the developer's real config.
func isolateKubeConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	// xdg caches env-derived paths globally. Register the reload before Setenv so
	// the cleanup runs after t.Setenv restores the original env, preventing temp
	// paths from leaking into later tests in this package.
	t.Cleanup(func() { xdg.Reload() })
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_STATE_HOME", tmp)
	xdg.Reload()
}

func TestSaveClusterAlias_PersistsResolvedCluster(t *testing.T) {
	isolateKubeConfig(t)
	ctx, buf := newTestContext(t)
	w := writer.FromContext(ctx)
	cmd := &cobra.Command{Use: "login"}
	cmd.SetContext(ctx)

	res := &kubelogin.Result{
		Handler: "openshift",
		ResolvedCluster: kubeapi.ClusterInfo{
			Name:           "prod",
			APIServerURL:   "https://api.prod.example.com:6443",
			AuthType:       kubeapi.AuthTypeOIDC,
			DefaultHandler: "openshift",
			OIDCAudience:   "api://aud",
		},
	}

	saveClusterAlias(cmd, w, "prod", res)

	assert.Contains(t, buf.String(), `Saved alias "prod"`)
	cfg, err := config.NewManager("").Load()
	require.NoError(t, err)
	al, ok := cfg.Kube.Clusters.Aliases["prod"]
	require.True(t, ok, "the resolved cluster must be persisted as an alias")
	assert.Equal(t, "https://api.prod.example.com:6443", al.Server)
	assert.Equal(t, "openshift", al.DefaultHandler)
	assert.Equal(t, "oidc", al.AuthType)
	assert.Equal(t, "api://aud", al.OIDCAudience)
}

func TestSaveClusterAlias_FallsBackToUsedHandler(t *testing.T) {
	isolateKubeConfig(t)
	ctx, _ := newTestContext(t)
	w := writer.FromContext(ctx)
	cmd := &cobra.Command{Use: "login"}
	cmd.SetContext(ctx)

	// The resolver supplied no default handler; the alias must record the
	// handler actually used so `kube login <alias>` works without --handler.
	res := &kubelogin.Result{
		Handler: "oidc",
		ResolvedCluster: kubeapi.ClusterInfo{
			Name:         "lab",
			APIServerURL: "https://api.lab.example.com:6443",
		},
	}

	saveClusterAlias(cmd, w, "lab", res)

	cfg, err := config.NewManager("").Load()
	require.NoError(t, err)
	assert.Equal(t, "oidc", cfg.Kube.Clusters.Aliases["lab"].DefaultHandler)
}

func TestSaveClusterAlias_NoServerWarnsAndSkips(t *testing.T) {
	isolateKubeConfig(t)
	ctx, buf := newTestContext(t)
	w := writer.FromContext(ctx)
	cmd := &cobra.Command{Use: "login"}
	cmd.SetContext(ctx)

	res := &kubelogin.Result{Handler: "oidc", ResolvedCluster: kubeapi.ClusterInfo{Name: "x"}}
	saveClusterAlias(cmd, w, "x", res)

	assert.Contains(t, buf.String(), "no resolved server")
	cfg, err := config.NewManager("").Load()
	require.NoError(t, err)
	_, ok := cfg.Kube.Clusters.Aliases["x"]
	assert.False(t, ok, "nothing is persisted without a resolved server")
}
