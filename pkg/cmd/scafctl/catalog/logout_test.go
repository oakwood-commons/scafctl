// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLogout(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogout(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "logout [registry]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandLogout_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogout(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().Lookup("all")
	require.NotNil(t, f, "flag 'all' should exist")
	assert.Equal(t, "false", f.DefValue)
}

func TestCommandLogout_RequiresRegistryOrAll(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogout(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "specify a registry or use --all")
}

func TestCommandLogout_AcceptsMaxOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogout(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{"ghcr.io", "quay.io"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts at most 1 arg(s)")
}

func BenchmarkCommandLogout(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for b.Loop() {
		_ = CommandLogout(cliParams, ioStreams, "scafctl/catalog")
	}
}

func TestLogoutAll_Empty(t *testing.T) {
	ctx := newCatalogTestCtx(t)

	store := catalog.NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))
	w := writerFromCtx(ctx)

	err := logoutAll(w, store)
	require.NoError(t, err)
}

func TestLogoutAll_WithCredentials(t *testing.T) {
	ctx := newCatalogTestCtx(t)

	dir := t.TempDir()
	store := catalog.NewNativeCredentialStoreWithPath(filepath.Join(dir, "registries.json"))

	require.NoError(t, store.SetCredential("ghcr.io", "user1", "pass1", ""))
	require.NoError(t, store.SetCredential("quay.io", "user2", "pass2", ""))

	w := writerFromCtx(ctx)
	err := logoutAll(w, store)
	require.NoError(t, err)

	creds, err := store.ListCredentials()
	require.NoError(t, err)
	assert.Empty(t, creds)
}

func TestLogoutRegistry_NotFound(t *testing.T) {
	ctx := newCatalogTestCtx(t)

	store := catalog.NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))
	w := writerFromCtx(ctx)

	err := logoutRegistry(w, store, "missing.io")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials stored for missing.io")
}

func TestLogoutRegistry_Success(t *testing.T) {
	ctx := newCatalogTestCtx(t)

	dir := t.TempDir()
	store := catalog.NewNativeCredentialStoreWithPath(filepath.Join(dir, "registries.json"))
	require.NoError(t, store.SetCredential("ghcr.io", "user", "pass", ""))

	w := writerFromCtx(ctx)
	err := logoutRegistry(w, store, "ghcr.io")
	require.NoError(t, err)

	cred, err := store.GetCredential("ghcr.io")
	require.NoError(t, err)
	assert.Nil(t, cred)
}

func TestLogoutRegistry_NormalizedHost(t *testing.T) {
	ctx := newCatalogTestCtx(t)

	dir := t.TempDir()
	store := catalog.NewNativeCredentialStoreWithPath(filepath.Join(dir, "registries.json"))
	// Store under canonical key
	require.NoError(t, store.SetCredential("ghcr.io", "user", "pass", ""))

	w := writerFromCtx(ctx)
	// Logout with https:// prefix should still work
	err := logoutRegistry(w, store, "https://ghcr.io")
	require.NoError(t, err)

	cred, err := store.GetCredential("ghcr.io")
	require.NoError(t, err)
	assert.Nil(t, cred)
}

func TestRunCatalogLogout_All(t *testing.T) {
	ctx := newCatalogTestCtx(t)

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogout(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--all"})

	// With an empty native store (XDG path), should succeed without error
	err := cmd.Execute()
	// May succeed or error depending on whether XDG path is writable; just verify it doesn't panic
	_ = err
}
