// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/oakwood-commons/scafctl/pkg/auth/execcredential"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

func readKubeconfig(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	return cfg
}

func namedEntry(t *testing.T, cfg map[string]any, key, name string) map[string]any {
	t.Helper()
	list, ok := cfg[key].([]any)
	require.True(t, ok, "expected list for %q", key)
	for _, item := range list {
		m, ok := item.(map[string]any)
		require.True(t, ok)
		if n, _ := m["name"].(string); n == name {
			return m
		}
	}
	t.Fatalf("entry %q not found in %q", name, key)
	return nil
}

func TestWriteStaticKubeconfig_NewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config")
	in := kubeconfig.WriteInput{
		Server:            "https://api.example.com:6443",
		ClusterName:       "prod",
		ContextName:       "prod-ctx",
		UserName:          "prod-user",
		KubeconfigPath:    path,
		ExecCommand:       "mycli",
		ExecArgs:          []string{"auth", "token", "oidc", "--exec-credential"},
		InteractiveMode:   kubeconfig.InteractiveModeNever,
		InstallHint:       "install mycli",
		InsecureSkipTLS:   true,
		SetCurrentContext: true,
	}

	res, err := writeStaticKubeconfig(in)
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, "prod-ctx", res.ContextName)
	assert.Equal(t, path, res.KubeconfigPath)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(kubeconfigFileMode), info.Mode().Perm())

	cfg := readKubeconfig(t, path)
	assert.Equal(t, "v1", cfg["apiVersion"])
	assert.Equal(t, "Config", cfg["kind"])
	assert.Equal(t, "prod-ctx", cfg["current-context"])

	cluster := namedEntry(t, cfg, "clusters", "prod")["cluster"].(map[string]any)
	assert.Equal(t, "https://api.example.com:6443", cluster["server"])
	assert.Equal(t, true, cluster["insecure-skip-tls-verify"])

	user := namedEntry(t, cfg, "users", "prod-user")["user"].(map[string]any)
	exec := user["exec"].(map[string]any)
	assert.Equal(t, execcredential.DefaultAPIVersion, exec["apiVersion"])
	assert.Equal(t, "mycli", exec["command"])
	assert.Equal(t, "Never", exec["interactiveMode"])
	assert.Equal(t, "install mycli", exec["installHint"])
	assert.Equal(t, false, exec["provideClusterInfo"])

	ctx := namedEntry(t, cfg, "contexts", "prod-ctx")["context"].(map[string]any)
	assert.Equal(t, "prod", ctx["cluster"])
	assert.Equal(t, "prod-user", ctx["user"])
}

func TestWriteStaticKubeconfig_CADataPreferred(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	in := kubeconfig.WriteInput{
		Server:          "https://api.example.com:6443",
		ClusterName:     "prod",
		KubeconfigPath:  path,
		ExecCommand:     "mycli",
		CAData:          "PEM-DATA",
		InsecureSkipTLS: true,
	}
	_, err := writeStaticKubeconfig(in)
	require.NoError(t, err)

	cfg := readKubeconfig(t, path)
	cluster := namedEntry(t, cfg, "clusters", "prod")["cluster"].(map[string]any)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("PEM-DATA")), cluster["certificate-authority-data"])
	_, hasInsecure := cluster["insecure-skip-tls-verify"]
	assert.False(t, hasInsecure, "CA data must take precedence over insecure-skip-tls-verify")
}

func TestWriteStaticKubeconfig_PreservesExistingEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	existing := map[string]any{
		"apiVersion": "v1",
		"kind":       "Config",
		"clusters": []any{
			map[string]any{"name": "other", "cluster": map[string]any{"server": "https://other:6443"}},
		},
		"custom-extension": "keep-me",
	}
	data, err := yaml.Marshal(existing)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, kubeconfigFileMode))

	in := kubeconfig.WriteInput{
		Server:         "https://api.example.com:6443",
		ClusterName:    "prod",
		KubeconfigPath: path,
		ExecCommand:    "mycli",
	}
	_, err = writeStaticKubeconfig(in)
	require.NoError(t, err)

	cfg := readKubeconfig(t, path)
	assert.Equal(t, "keep-me", cfg["custom-extension"], "unknown top-level keys must be preserved")
	// Both the pre-existing and new cluster entries are present.
	clusters := cfg["clusters"].([]any)
	assert.Len(t, clusters, 2)
}

func TestWriteStaticKubeconfig_UpsertReplaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	in := kubeconfig.WriteInput{
		Server:         "https://api.example.com:6443",
		ClusterName:    "prod",
		KubeconfigPath: path,
		ExecCommand:    "mycli",
	}
	_, err := writeStaticKubeconfig(in)
	require.NoError(t, err)

	in.Server = "https://api.updated.com:6443"
	_, err = writeStaticKubeconfig(in)
	require.NoError(t, err)

	cfg := readKubeconfig(t, path)
	clusters := cfg["clusters"].([]any)
	require.Len(t, clusters, 1, "upsert must replace, not duplicate")
	cluster := namedEntry(t, cfg, "clusters", "prod")["cluster"].(map[string]any)
	assert.Equal(t, "https://api.updated.com:6443", cluster["server"])
}

func TestRemoveStaticKubeconfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	in := kubeconfig.WriteInput{
		Server:            "https://api.example.com:6443",
		ClusterName:       "prod",
		KubeconfigPath:    path,
		ExecCommand:       "mycli",
		SetCurrentContext: true,
	}
	_, err := writeStaticKubeconfig(in)
	require.NoError(t, err)

	removed, err := removeStaticKubeconfig(kubeconfig.RemoveInput{
		ClusterName:    "prod",
		KubeconfigPath: path,
	})
	require.NoError(t, err)
	assert.True(t, removed)

	cfg := readKubeconfig(t, path)
	assert.Empty(t, cfg["clusters"])
	assert.Empty(t, cfg["users"])
	assert.Empty(t, cfg["contexts"])
	_, hasCurrent := cfg["current-context"]
	assert.False(t, hasCurrent, "current-context referencing the removed context must be cleared")
}

func TestRemoveStaticKubeconfig_MissingFile(t *testing.T) {
	t.Parallel()

	removed, err := removeStaticKubeconfig(kubeconfig.RemoveInput{
		ClusterName:    "prod",
		KubeconfigPath: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	require.NoError(t, err)
	assert.False(t, removed)
}

func TestRemoveStaticKubeconfig_NoMatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	_, err := writeStaticKubeconfig(kubeconfig.WriteInput{
		Server:         "https://api.example.com:6443",
		ClusterName:    "prod",
		KubeconfigPath: path,
		ExecCommand:    "mycli",
	})
	require.NoError(t, err)

	removed, err := removeStaticKubeconfig(kubeconfig.RemoveInput{
		ClusterName:    "nonexistent",
		KubeconfigPath: path,
	})
	require.NoError(t, err)
	assert.False(t, removed)
}

func TestWriteStaticKubeconfig_AtomicNoTempLeftover(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	in := kubeconfig.WriteInput{
		Server:         "https://api.example.com:6443",
		ClusterName:    "prod",
		KubeconfigPath: path,
		ExecCommand:    "mycli",
	}
	_, err := writeStaticKubeconfig(in)
	require.NoError(t, err)
	// Overwrite an existing file to exercise the rename-over path.
	in.Server = "https://api.updated.com:6443"
	_, err = writeStaticKubeconfig(in)
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the final kubeconfig should remain")
	assert.Equal(t, "config", entries[0].Name())

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(kubeconfigFileMode), info.Mode().Perm())
}

func TestResolveKubeconfigPath(t *testing.T) {
	t.Parallel()

	got, err := resolveKubeconfigPath("/explicit/path")
	require.NoError(t, err)
	assert.Equal(t, "/explicit/path", got)
}

func TestResolveKubeconfigPath_KubeconfigEnv(t *testing.T) {
	// Cannot run in parallel: mutates the KUBECONFIG environment variable.
	first := filepath.Join("/tmp", "first")
	second := filepath.Join("/tmp", "second")
	t.Setenv("KUBECONFIG", string(os.PathListSeparator)+first+string(os.PathListSeparator)+second)

	got, err := resolveKubeconfigPath("")
	require.NoError(t, err)
	assert.Equal(t, first, got, "first non-empty KUBECONFIG entry wins")
}

func TestResolveKubeconfigPath_HomeFallback(t *testing.T) {
	// Cannot run in parallel: mutates the KUBECONFIG environment variable.
	t.Setenv("KUBECONFIG", "")

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	got, err := resolveKubeconfigPath("")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".kube", "config"), got)
}
