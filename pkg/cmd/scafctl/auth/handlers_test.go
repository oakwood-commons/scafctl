// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

	authpkg "github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectHandlerInfo_EmptyRegistries(t *testing.T) {
	ctx, _ := newTestContext(t)

	results := collectHandlerInfo(ctx)
	assert.Empty(t, results)
}

func TestCollectHandlerInfo_WithRegisteredHandler(t *testing.T) {
	ctx, _ := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	mock.StatusResult = &authpkg.Status{Authenticated: true}
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	results := collectHandlerInfo(ctx)
	require.Len(t, results, 1)
	assert.Equal(t, "github", results[0]["name"])
	assert.Equal(t, "installed", results[0]["status"])
	assert.Equal(t, true, results[0]["loggedIn"])
}

func TestCollectHandlerInfo_WithOfficialAndRegistered(t *testing.T) {
	ctx, _ := newTestContext(t)

	// Set up auth registry with one handler.
	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("entra")
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	// Set up official registry with two handlers.
	officialReg := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "auth-entra"},
		{Name: "gcp", CatalogRef: "auth-gcp"},
	})
	ctx = authofficial.WithRegistry(ctx, officialReg)

	results := collectHandlerInfo(ctx)
	require.Len(t, results, 2)

	// Results should be sorted by name.
	assert.Equal(t, "entra", results[0]["name"])
	assert.Equal(t, "installed", results[0]["status"])

	assert.Equal(t, "gcp", results[1]["name"])
	assert.Equal(t, "available", results[1]["status"])
	assert.Equal(t, "catalog", results[1]["source"])
}

func TestBuildHandlerInfoResult_NotFound(t *testing.T) {
	ctx, _ := newTestContext(t)

	result := buildHandlerInfoResult(ctx, "unknown", nil, nil)
	assert.Equal(t, "unknown", result["name"])
	assert.Equal(t, "not-found", result["status"])
	assert.Equal(t, false, result["loggedIn"])
}

func TestBuildHandlerInfoResult_InstalledBuiltIn(t *testing.T) {
	ctx, _ := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.FlowsValue = []authpkg.Flow{authpkg.FlowDeviceCode, authpkg.FlowInteractive}
	mock.StatusResult = &authpkg.Status{Authenticated: false}
	require.NoError(t, registry.Register(mock))

	result := buildHandlerInfoResult(ctx, "entra", registry, nil)
	assert.Equal(t, "installed", result["status"])
	assert.Equal(t, "Microsoft Entra ID", result["displayName"])
	assert.Equal(t, "built-in", result["source"])
	assert.Contains(t, result["flows"], "device_code")
	assert.Equal(t, false, result["loggedIn"])
}

func TestBuildHandlerInfoResult_LazyPluginClassifiedAsPlugin(t *testing.T) {
	ctx, _ := newTestContext(t)

	registry := authpkg.NewRegistry()
	lazy := plugin.NewLazyAuthHandlerWrapper(plugin.LazyAuthHandlerConfig{
		Name:    "github",
		BinPath: "/fake/path",
	})
	require.NoError(t, registry.Register(lazy))

	result := buildHandlerInfoResult(ctx, "github", registry, nil)
	assert.Equal(t, "installed", result["status"])
	assert.Equal(t, "plugin", result["source"], "lazy wrappers should be classified as plugin, not built-in")
}

func TestCommandHandlers_WithRegisteredHandler(t *testing.T) {
	ctx, buf := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	mock.DisplayNameValue = "GitHub"
	mock.StatusResult = &authpkg.Status{Authenticated: true}
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandHandlers(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "github")
	assert.Contains(t, output, "installed")
}

func TestCommandHandlers_EmptyRegistries(t *testing.T) {
	ctx, buf := newTestContext(t)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandHandlers(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	// With no registries, output should be empty (no rows) or an empty table header.
	assert.NotContains(t, output, "installed")
	assert.NotContains(t, output, "available")
}

func TestCommandHandlers_JSONOutput(t *testing.T) {
	ctx, buf := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.StatusResult = &authpkg.Status{Authenticated: false}
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandHandlers(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "\"name\"")
	assert.Contains(t, output, "entra")
}

func TestCommandHandlers_NonDefaultBinaryName(t *testing.T) {
	ctx, buf := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	mock.StatusResult = &authpkg.Status{Authenticated: false}
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandHandlers(cliParams, ioStreams, "mycli/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)
}
