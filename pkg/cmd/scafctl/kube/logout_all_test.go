// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

// writeLogoutAllKubeconfig writes a fixture with one managed context (prod) for
// binaryName and one foreign context (staging).
func writeLogoutAllKubeconfig(t *testing.T, binaryName string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
current-context: prod
clusters:
  - name: prod
    cluster:
      server: https://api.prod.example.com:6443
  - name: staging
    cluster:
      server: https://api.staging.example.com:6443
contexts:
  - name: prod
    context:
      cluster: prod
      user: prod
  - name: staging
    context:
      cluster: staging
      user: staging
users:
  - name: prod
    user:
      exec:
        command: ` + binaryName + `
        args:
          - auth
          - token
          - oidc
  - name: staging
    user:
      token: static
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestCommandLogout_AllRemovesManagedEntries(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	path := writeLogoutAllKubeconfig(t, "mycli")

	// No official registry in context: the kubeconfig provider is unavailable,
	// so LogoutAll takes the static-fallback removal path.
	cmd := CommandLogout(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx, "--all", "--kubeconfig", path))

	// Output is branding-safe: it uses the embedder binary name, not "scafctl".
	assert.Contains(t, buf.String(), "Removed 1 mycli-managed")
	assert.NotContains(t, buf.String(), "scafctl-managed")

	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	// The managed prod entry is gone; the foreign staging entry remains.
	assert.NotContains(t, string(data), "api.prod.example.com")
	assert.Contains(t, string(data), "staging")
}

func TestCommandLogout_AllRejectsClusterArg(t *testing.T) {
	t.Parallel()
	ctx, _ := newTestContext(t)

	cmd := CommandLogout(embedderParams(), terminal.NewIOStreams(nil, nil, nil, false), "mycli")
	err := runCmd(t, cmd, ctx, "--all", "prod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all cannot be combined with a cluster argument")
}

func TestCommandLogout_AllRemovesMultipleEntries(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)

	// Two managed contexts exercise the pluralized summary ("entries").
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
clusters:
  - name: prod
    cluster:
      server: https://api.prod.example.com:6443
  - name: dev
    cluster:
      server: https://api.dev.example.com:6443
contexts:
  - name: prod
    context:
      cluster: prod
      user: prod
  - name: dev
    context:
      cluster: dev
      user: dev
users:
  - name: prod
    user:
      exec:
        command: mycli
        args: [auth, token, oidc]
  - name: dev
    user:
      exec:
        command: mycli
        args: [auth, token, oidc]
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cmd := CommandLogout(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx, "--all", "--kubeconfig", path))

	out := buf.String()
	assert.Contains(t, out, "Removed 2 mycli-managed")
	assert.Contains(t, out, "entries")
	assert.NotContains(t, out, "scafctl-managed")

	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.NotContains(t, string(data), "api.prod.example.com")
	assert.NotContains(t, string(data), "api.dev.example.com")
}

func TestCommandLogout_AllNoManagedEntries(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0o600))

	cmd := CommandLogout(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx, "--all", "--kubeconfig", path))
	assert.Contains(t, buf.String(), "No mycli-managed kubeconfig entries found")
}
