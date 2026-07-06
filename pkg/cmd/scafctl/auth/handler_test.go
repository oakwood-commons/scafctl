// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithTestHandler(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := context.Background()

	// Without injection, returns nil
	h := handlerFromContext(ctx)
	assert.Nil(t, h)

	// With injection, returns the handler
	ctx = withTestHandler(ctx, mock)
	h = handlerFromContext(ctx)
	require.NotNil(t, h)
	assert.Equal(t, "test", h.Name())
}

func TestIsHandlerRegistered_WithRegistry(t *testing.T) {
	registry := auth.NewRegistry()
	mock := auth.NewMockHandler("entra")
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	assert.True(t, isHandlerRegistered(ctx, "entra"))
	assert.False(t, isHandlerRegistered(ctx, "unknown"))
}

func TestIsHandlerRegistered_WithTestHandler(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := withTestHandler(context.Background(), mock)

	// Test-injected handler matches any name
	assert.True(t, isHandlerRegistered(ctx, "test"))
	assert.True(t, isHandlerRegistered(ctx, "anything"))
}

func TestIsHandlerRegistered_NoContext(t *testing.T) {
	ctx := context.Background()
	assert.False(t, isHandlerRegistered(ctx, "entra"))
}

func TestListHandlers_WithRegistry(t *testing.T) {
	registry := auth.NewRegistry()
	mockEntra := auth.NewMockHandler("entra")
	mockGH := auth.NewMockHandler("github")
	require.NoError(t, registry.Register(mockEntra))
	require.NoError(t, registry.Register(mockGH))
	ctx := auth.WithRegistry(context.Background(), registry)

	handlers := listHandlers(ctx)
	assert.Contains(t, handlers, "entra")
	assert.Contains(t, handlers, "github")
	assert.Len(t, handlers, 2)
}

func TestListActiveHandlers_TestHandlerShortCircuit(t *testing.T) {
	mock := auth.NewMockHandler("openshift")
	ctx := withTestHandler(context.Background(), mock)
	assert.Equal(t, []string{"openshift"}, listActiveHandlers(ctx, "scafctl"))
}

func TestListActiveHandlers_EagerOnlyWhenNoInstalledPlugins(t *testing.T) {
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("entra")))
	require.NoError(t, registry.Register(auth.NewMockHandler("gcp")))
	ctx := auth.WithRegistry(context.Background(), registry)

	// A binary name whose plugin cache dir does not exist yields just the eager
	// handlers (the installed-plugin scan is an empty/graceful miss).
	got := listActiveHandlers(ctx, "scafctl-test-nonexistent-cache-xyz")
	assert.Equal(t, []string{"entra", "gcp"}, got)
}

func TestListActiveHandlers_ThirdPartyDisabledSkipsInstalledScan(t *testing.T) {
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("gcp")))
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
	})

	// Even if the local machine has installed plugins under "scafctl", policy
	// disables the scan, so only the eager handler surfaces.
	got := listActiveHandlers(ctx, "scafctl")
	assert.Equal(t, []string{"gcp"}, got)
}

func TestListActiveHandlers_SurfacesInstalledAuthPlugin(t *testing.T) {
	t.Parallel()

	// The package TestMain isolates xdg.CacheHome, so seeding under a unique
	// binary-name subdir is hermetic without mutating any global.
	// Layout: <cacheDir>/<key>/<ver>/<plat>/<key>.
	const binaryName = "mycli-surfaces-test"
	cacheDir := settings.PluginCacheDirFor(binaryName)
	platformDir := runtime.GOOS + "-" + runtime.GOARCH
	pluginDir := filepath.Join(cacheDir, "auth-handler-openshift", "0.1.0", platformDir)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "auth-handler-openshift"), []byte("binary"), 0o600))

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("gcp")))
	ctx := auth.WithRegistry(context.Background(), registry)

	got := listActiveHandlers(ctx, binaryName)
	assert.Equal(t, []string{"gcp", "openshift"}, got, "installed auth plugin surfaces alongside eager handlers, deduped and sorted")
}

func TestListHandlers_WithTestHandler(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := withTestHandler(context.Background(), mock)

	handlers := listHandlers(ctx)
	assert.Equal(t, []string{"test"}, handlers)
}

func TestValidateHandlerName(t *testing.T) {
	registry := auth.NewRegistry()
	mock := auth.NewMockHandler("entra")
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	// Registered handler passes.
	assert.NoError(t, validateHandlerName(ctx, "entra"))

	// Empty name is rejected.
	assert.Error(t, validateHandlerName(ctx, ""))

	// An unknown name now defers to getHandler (catalog resolution) instead of
	// failing early, so validation passes when third-party resolution is
	// enabled (the default).
	assert.NoError(t, validateHandlerName(ctx, "unknown"))
}

func TestValidateHandlerName_ThirdPartyDisabled(t *testing.T) {
	registry := auth.NewRegistry()
	mock := auth.NewMockHandler("entra")
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	official := authofficial.NewRegistry()
	ctx = authofficial.WithRegistry(ctx, official)
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
	})

	// With third-party resolution disabled, an unknown (non-official) name is
	// rejected up front.
	err := validateHandlerName(ctx, "unknown")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")

	// Official names still resolve.
	assert.NoError(t, validateHandlerName(ctx, "github"))
}

func TestValidateHandlerName_ConfigPinned(t *testing.T) {
	registry := auth.NewRegistry()
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = config.WithConfig(ctx, &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {Plugin: &config.HandlerPluginConfig{}},
			},
		},
	})

	// A config-pinned handler is known even before installation.
	assert.NoError(t, validateHandlerName(ctx, "openshift"))
}

func TestGetHandler_FromRegistry(t *testing.T) {
	registry := auth.NewRegistry()
	mock := auth.NewMockHandler("entra")
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	handler, err := getHandler(ctx, "entra")
	require.NoError(t, err)
	assert.Equal(t, "entra", handler.Name())

	_, err = getHandler(ctx, "unknown")
	assert.Error(t, err)
}

func TestGetHandler_FromTestContext(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := withTestHandler(context.Background(), mock)

	handler, err := getHandler(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, "test", handler.Name())
}

func TestGetHandler_NoContext(t *testing.T) {
	ctx := context.Background()
	_, err := getHandler(ctx, "entra")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no auth registry in context")
}

func TestIsHandlerRegistered_WithOfficialRegistry(t *testing.T) {
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	})
	ctx := authofficial.WithRegistry(context.Background(), official)

	assert.True(t, isHandlerRegistered(ctx, "entra"))
	assert.True(t, isHandlerRegistered(ctx, "github"))
	assert.False(t, isHandlerRegistered(ctx, "unknown"))
}

func TestIsHandlerRegistered_BothRegistries(t *testing.T) {
	// Auth registry has "custom", official has "entra"
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("custom")))
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	})
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = authofficial.WithRegistry(ctx, official)

	assert.True(t, isHandlerRegistered(ctx, "custom"))
	assert.True(t, isHandlerRegistered(ctx, "entra"))
	assert.False(t, isHandlerRegistered(ctx, "unknown"))
}

func TestListHandlers_WithOfficialRegistry(t *testing.T) {
	// listHandlers should NOT include official-only handlers (not installed).
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	})
	ctx := authofficial.WithRegistry(context.Background(), official)

	handlers := listHandlers(ctx)
	assert.Nil(t, handlers) // no eager registry → nil
}

func TestListHandlers_OnlyEagerRegistry(t *testing.T) {
	// listHandlers returns only eagerly-registered handlers, ignoring official.
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("custom")))
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	})
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = authofficial.WithRegistry(ctx, official)

	handlers := listHandlers(ctx)
	assert.Equal(t, []string{"custom"}, handlers)
}

func TestListKnownHandlers_WithOfficialRegistry(t *testing.T) {
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	})
	ctx := authofficial.WithRegistry(context.Background(), official)

	handlers := listKnownHandlers(ctx)
	assert.Contains(t, handlers, "entra")
	assert.Contains(t, handlers, "github")
	assert.Len(t, handlers, 2)
}

func TestListKnownHandlers_MergesBothRegistries(t *testing.T) {
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("custom")))
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	})
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = authofficial.WithRegistry(ctx, official)

	handlers := listKnownHandlers(ctx)
	assert.Contains(t, handlers, "custom")
	assert.Contains(t, handlers, "entra")
	assert.Len(t, handlers, 2)
}

func TestListKnownHandlers_DeduplicatesOverlap(t *testing.T) {
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("entra")))
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	})
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = authofficial.WithRegistry(ctx, official)

	handlers := listKnownHandlers(ctx)
	assert.ElementsMatch(t, []string{"entra", "github"}, handlers)
}

func TestListKnownHandlers_WithTestHandler(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := withTestHandler(context.Background(), mock)

	handlers := listKnownHandlers(ctx)
	assert.Equal(t, []string{"test"}, handlers)
}

func TestListKnownHandlers_IncludesConfigPins(t *testing.T) {
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("entra")))
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = config.WithConfig(ctx, &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {Plugin: &config.HandlerPluginConfig{}},
				// A handler entry without a plugin pin is not "known".
				"other": {},
			},
		},
	})

	handlers := listKnownHandlers(ctx)
	assert.ElementsMatch(t, []string{"entra", "openshift"}, handlers)
}

func TestListKnownHandlers_OmitsConfigPinsWhenThirdPartyDisabled(t *testing.T) {
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("entra")))
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {Plugin: &config.HandlerPluginConfig{}},
			},
		},
	})

	// The config pin is omitted because policy forbids resolving it.
	handlers := listKnownHandlers(ctx)
	assert.ElementsMatch(t, []string{"entra"}, handlers)
}

func TestListKnownHandlers_NoContext(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, listKnownHandlers(ctx))
}

func TestValidateHandlerName_OfficialHandler(t *testing.T) {
	// No auth registry, but official registry has "entra"
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	})
	ctx := authofficial.WithRegistry(context.Background(), official)

	assert.NoError(t, validateHandlerName(ctx, "entra"))

	// An unknown name defers to catalog resolution by default.
	assert.NoError(t, validateHandlerName(ctx, "unknown"))

	// With third-party resolution disabled, the unknown name is rejected while
	// the official handler still resolves.
	lockedCtx := config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
	})
	err := validateHandlerName(lockedCtx, "unknown")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
	assert.Contains(t, err.Error(), "entra")
	assert.NoError(t, validateHandlerName(lockedCtx, "entra"))
}
