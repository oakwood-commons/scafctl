// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

// writeStatusKubeconfig writes a kubeconfig fixture whose current-context (prod)
// is a scafctl-managed exec-credential entry for binaryName.
func writeStatusKubeconfig(t *testing.T, binaryName string) string {
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
contexts:
  - name: prod
    context:
      cluster: prod
      user: prod
      namespace: team-a
users:
  - name: prod
    user:
      exec:
        command: ` + binaryName + `
        args:
          - auth
          - token
          - oidc
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestCommandStatus_ShowsCurrentContext(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	path := writeStatusKubeconfig(t, "mycli")

	cmd := CommandStatus(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx, "--kubeconfig", path))
	out := buf.String()
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "https://api.prod.example.com:6443")
	assert.Contains(t, out, "team-a")
}

func TestCommandStatus_JSONReportsManaged(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	path := writeStatusKubeconfig(t, "mycli")

	cmd := CommandStatus(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx, "--kubeconfig", path, "-o", "json"))
	out := buf.String()
	assert.Contains(t, out, "\"managed\"")
	assert.Contains(t, out, "true")
	// A single status object (not an array) is emitted, and empty optional
	// fields (identity/groups, absent without a successful whoami) are omitted.
	assert.True(t, strings.HasPrefix(strings.TrimSpace(out), "{"), "status JSON must be a single object")
	assert.NotContains(t, out, "\"identity\"")
	assert.NotContains(t, out, "\"groups\"")
}

func TestCommandStatus_NoCurrentContext(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0o600))

	cmd := CommandStatus(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx, "--kubeconfig", path))
	assert.Contains(t, buf.String(), "No current kubeconfig context")
}
