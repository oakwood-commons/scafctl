// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"testing"

	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAuthScope_FromNamedCatalog(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name:      "my-registry",
				Type:      appconfig.CatalogTypeOCI,
				URL:       "oci://ghcr.io/myorg",
				AuthScope: "repo:read",
			},
		},
	}

	ctx := appconfig.WithConfig(context.Background(), cfg)
	scope := resolveAuthScope(ctx, "my-registry")
	assert.Equal(t, "repo:read", scope)
}

func TestResolveAuthScope_EmptyWhenNoCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scope := resolveAuthScope(ctx, "nonexistent")
	assert.Equal(t, "", scope)
}

func TestResolveAuthScope_EmptyWhenNoConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scope := resolveAuthScope(ctx, "")
	assert.Equal(t, "", scope)
}

func TestResolveAuthScope_EmptyWhenCatalogHasNoScope(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name: "my-registry",
				Type: appconfig.CatalogTypeOCI,
				URL:  "oci://ghcr.io/myorg",
			},
		},
	}

	ctx := appconfig.WithConfig(context.Background(), cfg)
	scope := resolveAuthScope(ctx, "my-registry")
	assert.Equal(t, "", scope)
}

func TestResolveAuthHandler_NilWhenNoConfig(t *testing.T) {
	t.Parallel()

	w := writer.New(
		testIOStreams(),
		settings.NewCliParams(),
	)
	ctx := writer.WithWriter(context.Background(), w)

	handler := resolveAuthHandler(ctx, "ghcr.io", "")
	assert.Nil(t, handler, "should return nil when no config in context")
}

func TestResolveAuthHandler_NilWhenNoMatchingCatalog(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name: "my-registry",
				Type: appconfig.CatalogTypeOCI,
				URL:  "oci://ghcr.io/myorg",
			},
		},
	}

	w := writer.New(
		testIOStreams(),
		settings.NewCliParams(),
	)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = appconfig.WithConfig(ctx, cfg)

	handler := resolveAuthHandler(ctx, "custom.io", "nonexistent")
	assert.Nil(t, handler, "should return nil when catalog not found and no inference match")
}

func TestResolveAuthHandler_FromCatalogConfig(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name:         "gh-registry",
				Type:         appconfig.CatalogTypeOCI,
				URL:          "oci://ghcr.io/myorg",
				AuthProvider: "github",
			},
		},
	}

	w := writer.New(
		testIOStreams(),
		settings.NewCliParams(),
	)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = appconfig.WithConfig(ctx, cfg)

	// github handler may or may not be loadable in test environment,
	// but the function should at least try (not panic)
	_ = resolveAuthHandler(ctx, "ghcr.io", "gh-registry")
}

func TestCommandCatalog_HasSubcommands(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandCatalog(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "catalog", cmd.Use)

	// Verify expected subcommands exist
	expectedSubs := []string{"list", "pull", "push", "delete", "login", "logout", "remote", "inspect", "tags", "tag", "attach"}
	for _, name := range expectedSubs {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		assert.True(t, found, "expected subcommand %q", name)
	}
}

func TestHintOnAuthError_401(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	testErr := fmt.Errorf("request failed: 401 Unauthorized")
	hintOnAuthError(ctx, w, "ghcr.io", testErr)
	assert.Contains(t, outBuf.String(), "catalog login")
}

func TestHintOnAuthError_403(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	testErr := fmt.Errorf("request failed: 403 Forbidden")
	hintOnAuthError(ctx, w, "ghcr.io", testErr)
	assert.Contains(t, outBuf.String(), "catalog login")
}

func TestHintOnAuthError_NonAuthError(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	testErr := fmt.Errorf("network timeout")
	hintOnAuthError(ctx, w, "ghcr.io", testErr)
	assert.Empty(t, outBuf.String(), "should not print hint for non-auth errors")
}

func testIOStreams() *terminal.IOStreams {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	return ioStreams
}
