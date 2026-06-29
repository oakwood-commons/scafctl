// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	kubeapi "github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

func TestCommandLogout_NoCluster(t *testing.T) {
	t.Parallel()
	ctx, _ := newTestContext(t)

	cmd := CommandLogout(embedderParams(), terminal.NewIOStreams(nil, nil, nil, false), "mycli")
	err := runCmd(t, cmd, ctx)
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestCommandLogout_RemovesEntryAndRevokes(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx, mock := withMockHandler(t, ctx)

	// Seed a kubeconfig entry by logging in first (fallback path).
	path := filepath.Join(t.TempDir(), "config")
	loginCmd := CommandLogin(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, loginCmd, ctx, "prod",
		"--handler", "oidc",
		"--server", "https://api.example.com:6443",
		"--kubeconfig", path,
	))

	buf.Reset()
	logoutCmd := CommandLogout(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, logoutCmd, ctx, "prod",
		"--handler", "oidc",
		"--kubeconfig", path,
	))
	assert.Equal(t, 1, mock.LogoutCalls)
	assert.Contains(t, buf.String(), "Removed kubeconfig entry")
}

func TestCommandLogout_KeepCredentials(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx, mock := withMockHandler(t, ctx)

	path := filepath.Join(t.TempDir(), "config")
	loginCmd := CommandLogin(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, loginCmd, ctx, "prod",
		"--handler", "oidc",
		"--server", "https://api.example.com:6443",
		"--kubeconfig", path,
	))

	logoutCmd := CommandLogout(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, logoutCmd, ctx, "prod",
		"--kubeconfig", path,
		"--keep-credentials",
	))
	assert.Equal(t, 0, mock.LogoutCalls, "keep-credentials must skip handler logout")
}

func TestCommandLogout_RevokesResolverDefaultHandler(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx, mock := withMockHandler(t, ctx)

	// The resolver names the default handler, so "logout prod" with no --handler
	// revokes the same handler that login used.
	resolver := &kubeapi.MockResolver{ResolveResult: &kubeapi.ClusterInfo{
		Name:           "prod",
		APIServerURL:   "https://api.example.com:6443",
		DefaultHandler: mockHandlerName,
	}}
	ctx = kubeapi.WithResolver(ctx, resolver)

	path := filepath.Join(t.TempDir(), "config")
	loginCmd := CommandLogin(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, loginCmd, ctx, "prod", "--kubeconfig", path))

	buf.Reset()
	logoutCmd := CommandLogout(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, logoutCmd, ctx, "prod", "--kubeconfig", path))
	assert.Equal(t, 1, mock.LogoutCalls, "resolver default handler must be revoked without --handler")
}

func TestCommandLogout_OutputUsesEffectiveClusterFromFlag(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx, _ = withMockHandler(t, ctx)

	// Seed an entry, then log out via --cluster-name with no positional cluster
	// argument: the structured output must still report the targeted entry.
	path := filepath.Join(t.TempDir(), "config")
	loginCmd := CommandLogin(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, loginCmd, ctx, "prod",
		"--handler", "oidc",
		"--server", "https://api.example.com:6443",
		"--kubeconfig", path,
	))

	buf.Reset()
	logoutCmd := CommandLogout(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, logoutCmd, ctx,
		"--cluster-name", "prod",
		"--handler", "oidc",
		"--kubeconfig", path,
		"-o", "json",
	))

	var rows []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "prod", rows[0]["cluster"], "cluster field must fall back to --cluster-name")
}
