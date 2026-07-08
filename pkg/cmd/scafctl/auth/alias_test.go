// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

// aliasFixture seeds an isolated XDG home and a mock handler with the given
// capabilities, returning a context wired for the auth alias command.
func aliasFixture(t *testing.T, handlerName string, caps ...auth.Capability) context.Context {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_STATE_HOME", tmp)
	xdg.Reload()

	ctx, _ := newTestContext(t)
	mock := auth.NewMockHandler(handlerName)
	mock.CapabilitiesValue = caps
	ctx = withTestHandler(ctx, mock)
	ctx = configureAuthRegistry(t, ctx, mock)
	return ctx
}

func runAlias(t *testing.T, ctx context.Context, args ...string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandAlias(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// TestCommandAliasSet_PersistsNestedAlias verifies 'set' allocates the nested
// config structure and persists the alias to disk.
func TestCommandAliasSet_PersistsNestedAlias(t *testing.T) {
	ctx := aliasFixture(t, "openshift", auth.CapHostname)

	_, err := runAlias(t, ctx, "set", "openshift", "prod", "https://api.prod.example.com:6443")
	require.NoError(t, err)

	cfg, err := config.NewManager("").Load()
	require.NoError(t, err)
	hc, ok := cfg.Auth.Handlers["openshift"]
	require.True(t, ok, "handler entry must be persisted")
	require.NotNil(t, hc.Hostname)
	assert.Equal(t, "https://api.prod.example.com:6443", hc.Hostname.Aliases["prod"])
}

// TestCommandAliasSet_UpdatesExistingAlias verifies 'set' overwrites an
// existing selector.
func TestCommandAliasSet_UpdatesExistingAlias(t *testing.T) {
	ctx := aliasFixture(t, "openshift", auth.CapHostname)

	_, err := runAlias(t, ctx, "set", "openshift", "prod", "https://old.example.com:6443")
	require.NoError(t, err)
	_, err = runAlias(t, ctx, "set", "openshift", "prod", "https://new.example.com:6443")
	require.NoError(t, err)

	cfg, err := config.NewManager("").Load()
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.com:6443", cfg.Auth.Handlers["openshift"].Hostname.Aliases["prod"])
}

// TestCommandAliasSet_RejectsHandlerWithoutHostnameCap verifies the capability
// gate blocks handlers that do not support hostname aliasing.
func TestCommandAliasSet_RejectsHandlerWithoutHostnameCap(t *testing.T) {
	ctx := aliasFixture(t, "github") // no CapHostname

	_, err := runAlias(t, ctx, "set", "github", "prod", "https://api.prod.example.com:6443")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support hostname aliases")
}

// TestCommandAliasList_RendersAliases verifies 'list' emits configured aliases.
func TestCommandAliasList_RendersAliases(t *testing.T) {
	ctx := aliasFixture(t, "openshift", auth.CapHostname)
	_, err := runAlias(t, ctx, "set", "openshift", "prod", "https://api.prod.example.com:6443")
	require.NoError(t, err)
	_, err = runAlias(t, ctx, "set", "openshift", "staging", "https://api.staging.example.com:6443")
	require.NoError(t, err)

	out, err := runAlias(t, ctx, "list", "openshift", "-o", "json")
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &items))
	require.Len(t, items, 2)
	assert.Equal(t, "prod", items[0]["selector"])
	assert.Equal(t, "https://api.prod.example.com:6443", items[0]["url"])
	assert.Equal(t, "staging", items[1]["selector"])
}

// TestCommandAliasList_EmptyIsNotAnError verifies listing a handler with no
// aliases succeeds and renders nothing.
func TestCommandAliasList_EmptyIsNotAnError(t *testing.T) {
	ctx := aliasFixture(t, "openshift", auth.CapHostname)

	out, err := runAlias(t, ctx, "list", "openshift", "-o", "json")
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &items))
	assert.Empty(t, items)
}

// TestCommandAliasRemove_DeletesAndPrunes verifies 'remove' deletes the alias
// and prunes the now-empty hostname/handler entry.
func TestCommandAliasRemove_DeletesAndPrunes(t *testing.T) {
	ctx := aliasFixture(t, "openshift", auth.CapHostname)
	_, err := runAlias(t, ctx, "set", "openshift", "prod", "https://api.prod.example.com:6443")
	require.NoError(t, err)

	_, err = runAlias(t, ctx, "remove", "openshift", "prod")
	require.NoError(t, err)

	cfg, err := config.NewManager("").Load()
	require.NoError(t, err)
	_, ok := cfg.Auth.Handlers["openshift"]
	assert.False(t, ok, "empty handler entry must be pruned after last alias removed")
}

// TestCommandAliasRemove_UnknownSelectorErrors verifies removing a missing
// alias fails clearly.
func TestCommandAliasRemove_UnknownSelectorErrors(t *testing.T) {
	ctx := aliasFixture(t, "openshift", auth.CapHostname)
	_, err := runAlias(t, ctx, "set", "openshift", "prod", "https://api.prod.example.com:6443")
	require.NoError(t, err)

	_, err = runAlias(t, ctx, "remove", "openshift", "staging")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no alias")
}

// TestCommandAliasRemove_RejectsHandlerWithoutHostnameCap verifies the
// capability gate blocks 'remove' for handlers that do not support hostname
// aliasing, matching the 'set' semantics.
func TestCommandAliasRemove_RejectsHandlerWithoutHostnameCap(t *testing.T) {
	ctx := aliasFixture(t, "github") // no CapHostname

	_, err := runAlias(t, ctx, "remove", "github", "prod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support hostname aliases")
}

// TestCommandAliasSet_PreservesOpaqueSettings verifies that a save-through of
// the alias write does not drop passthrough handler settings.
func TestCommandAliasSet_PreservesOpaqueSettings(t *testing.T) {
	ctx := aliasFixture(t, "openshift", auth.CapHostname)

	// Seed a config file with opaque handler settings.
	seed := config.NewManager("")
	cfg, err := seed.Load()
	require.NoError(t, err)
	// viper lowercases config keys, so the passthrough key round-trips as "apitimeout".
	cfg.Auth.Handlers = map[string]config.HandlerConfig{
		"openshift": {Settings: map[string]any{"apitimeout": "30s"}},
	}
	require.NoError(t, seed.Save())

	_, err = runAlias(t, ctx, "set", "openshift", "prod", "https://api.prod.example.com:6443")
	require.NoError(t, err)

	reloaded, err := config.NewManager("").Load()
	require.NoError(t, err)
	hc := reloaded.Auth.Handlers["openshift"]
	assert.Equal(t, "https://api.prod.example.com:6443", hc.Hostname.Aliases["prod"])
	assert.Equal(t, "30s", hc.Settings["apitimeout"], "opaque handler settings must survive an alias write")
}

// TestCommandAliasSet_Embedder verifies the command works under a non-default
// embedder binary name.
func TestCommandAliasSet_Embedder(t *testing.T) {
	ctx := aliasFixture(t, "openshift", auth.CapHostname)

	params := settings.NewCliParams()
	params.BinaryName = "mycli"
	buf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandAlias(params, ioStreams, "mycli/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"set", "openshift", "prod", "https://api.prod.example.com:6443"})
	require.NoError(t, cmd.Execute())

	cfg, err := config.NewManager("").Load()
	require.NoError(t, err)
	assert.Equal(t, "https://api.prod.example.com:6443", cfg.Auth.Handlers["openshift"].Hostname.Aliases["prod"])
}
