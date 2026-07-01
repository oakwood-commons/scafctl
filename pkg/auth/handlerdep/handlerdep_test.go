// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package handlerdep

import (
	"context"
	"errors"
	"strings"
	"testing"

	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func officialCtx(t *testing.T, handlers ...authofficial.AuthHandler) context.Context {
	t.Helper()
	reg := authofficial.NewRegistryFrom(handlers)
	return authofficial.WithRegistry(context.Background(), reg)
}

func githubEntry() authofficial.AuthHandler {
	return authofficial.AuthHandler{Name: "github", CatalogRef: "github", DefaultVersion: "latest"}
}

func TestResolve_BareCatalogName(t *testing.T) {
	ctx := officialCtx(t, githubEntry())

	dep, source, err := Resolve(ctx, "openshift")
	require.NoError(t, err)
	assert.Equal(t, SourceCatalog, source)
	assert.Equal(t, solution.PluginDependency{
		Name:    "openshift",
		Kind:    solution.PluginKindAuthHandler,
		Version: "latest",
	}, dep)
}

func TestResolve_OfficialName(t *testing.T) {
	ctx := officialCtx(t, githubEntry())

	dep, source, err := Resolve(ctx, "github")
	require.NoError(t, err)
	assert.Equal(t, SourceOfficial, source)
	assert.Equal(t, "github", dep.Name)
	assert.Equal(t, solution.PluginKindAuthHandler, dep.Kind)
	assert.Equal(t, "latest", dep.Version)
}

func TestResolve_ConfigPinWinsOverCatalog(t *testing.T) {
	ctx := officialCtx(t, githubEntry())
	ctx = config.WithConfig(ctx, &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {Plugin: &config.HandlerPluginConfig{Ref: "openshift-auth", Version: "^0.2.0"}},
			},
		},
	})

	dep, source, err := Resolve(ctx, "openshift")
	require.NoError(t, err)
	assert.Equal(t, SourceConfigPin, source)
	assert.Equal(t, "openshift-auth", dep.Name)
	assert.Equal(t, "^0.2.0", dep.Version)
	assert.Equal(t, solution.PluginKindAuthHandler, dep.Kind)
}

func TestResolve_ConfigPinWinsOverOfficial(t *testing.T) {
	ctx := officialCtx(t, githubEntry())
	ctx = config.WithConfig(ctx, &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"github": {Plugin: &config.HandlerPluginConfig{Ref: "forked-github", Version: "1.2.3"}},
			},
		},
	})

	dep, source, err := Resolve(ctx, "github")
	require.NoError(t, err)
	assert.Equal(t, SourceConfigPin, source)
	assert.Equal(t, "forked-github", dep.Name)
	assert.Equal(t, "1.2.3", dep.Version)
}

func TestResolve_ConfigPinDefaults(t *testing.T) {
	ctx := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				// Empty Ref/Version: Ref defaults to the handler name, Version to "latest".
				"openshift": {Plugin: &config.HandlerPluginConfig{}},
			},
		},
	})

	dep, source, err := Resolve(ctx, "openshift")
	require.NoError(t, err)
	assert.Equal(t, SourceConfigPin, source)
	assert.Equal(t, "openshift", dep.Name)
	assert.Equal(t, "latest", dep.Version)
}

func TestResolve_NoConfigNoOfficial(t *testing.T) {
	// Nil config and no official registry: any name resolves as a bare catalog
	// name (third-party enabled by default).
	dep, source, err := Resolve(context.Background(), "openshift")
	require.NoError(t, err)
	assert.Equal(t, SourceCatalog, source)
	assert.Equal(t, "openshift", dep.Name)
}

func TestResolve_ThirdPartyDisabled_Catalog(t *testing.T) {
	ctx := officialCtx(t, githubEntry())
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
	})

	_, _, err := Resolve(ctx, "openshift")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrThirdPartyDisabled)
}

func TestResolve_ThirdPartyDisabled_ConfigPin(t *testing.T) {
	// A config pin is still third-party; the disable switch blocks it.
	ctx := config.WithConfig(context.Background(), &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {Plugin: &config.HandlerPluginConfig{}},
			},
		},
	})

	_, _, err := Resolve(ctx, "openshift")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrThirdPartyDisabled)
}

func TestResolve_ThirdPartyDisabled_OfficialStillResolves(t *testing.T) {
	ctx := officialCtx(t, githubEntry())
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
	})

	dep, source, err := Resolve(ctx, "github")
	require.NoError(t, err)
	assert.Equal(t, SourceOfficial, source)
	assert.Equal(t, "github", dep.Name)
}

func TestResolve_OfficialDisabled(t *testing.T) {
	ctx := officialCtx(t, githubEntry())
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableOfficialAuthHandlers: true},
	})

	_, _, err := Resolve(ctx, "github")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOfficialDisabled)
}

func TestResolve_OfficialDisabled_NoRegistryStillBlocks(t *testing.T) {
	// Regression for the disable-policy bypass: when official handlers are
	// disabled, root does not attach the official registry. An official name
	// must still be classified as official (via the canonical default names)
	// and blocked, not fall through to bare catalog resolution.
	ctx := config.WithConfig(context.Background(), &config.Config{
		Settings: config.Settings{DisableOfficialAuthHandlers: true},
	})

	for _, name := range []string{"github", "gcp", "entra"} {
		t.Run(name, func(t *testing.T) {
			_, source, err := Resolve(ctx, name)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrOfficialDisabled)
			assert.Equal(t, SourceUnknown, source)
		})
	}
}

func TestResolve_OfficialDisabled_ThirdPartyStillResolves(t *testing.T) {
	ctx := officialCtx(t, githubEntry())
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableOfficialAuthHandlers: true},
	})

	dep, source, err := Resolve(ctx, "openshift")
	require.NoError(t, err)
	assert.Equal(t, SourceCatalog, source)
	assert.Equal(t, "openshift", dep.Name)
}

func TestResolve_ErrorsHaveNoBinaryName(t *testing.T) {
	// Embedder contract: resolution errors must not hardcode "scafctl".
	ctx := config.WithConfig(context.Background(), &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
	})
	_, _, err := Resolve(ctx, "openshift")
	require.Error(t, err)
	assert.NotContains(t, strings.ToLower(err.Error()), "scafctl")
}

func TestIsKnown(t *testing.T) {
	pinned := config.WithConfig(
		officialCtx(t, githubEntry()),
		&config.Config{
			Auth: config.GlobalAuthConfig{
				Handlers: map[string]config.HandlerConfig{
					"openshift": {Plugin: &config.HandlerPluginConfig{}},
					// A handler entry without a plugin pin is NOT "known".
					"other": {},
				},
			},
		},
	)

	tests := []struct {
		name string
		want bool
	}{
		{"github", true},    // official
		{"openshift", true}, // config-pinned
		{"other", false},    // config entry without plugin pin
		{"unknown", false},  // bare catalog name is not "known" (no probe)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsKnown(pinned, tc.name))
		})
	}
}

func TestIsKnown_NilConfigAndRegistry(t *testing.T) {
	assert.False(t, IsKnown(context.Background(), "github"))
}

func TestIsKnown_HonorsDisableSwitches(t *testing.T) {
	base := func(s config.Settings) context.Context {
		return config.WithConfig(
			officialCtx(t, githubEntry()),
			&config.Config{
				Settings: s,
				Auth: config.GlobalAuthConfig{
					Handlers: map[string]config.HandlerConfig{
						"openshift": {Plugin: &config.HandlerPluginConfig{}},
					},
				},
			},
		)
	}

	// Third-party disabled: config pins are not "known"; official still is.
	tp := base(config.Settings{DisableThirdPartyAuthHandlers: true})
	assert.False(t, IsKnown(tp, "openshift"), "config pin must be hidden when third-party disabled")
	assert.True(t, IsKnown(tp, "github"), "official handler stays known")

	// Official disabled (registry still attached in this synthetic ctx):
	// official name is not "known"; the config pin remains known.
	off := base(config.Settings{DisableOfficialAuthHandlers: true})
	assert.False(t, IsKnown(off, "github"), "official handler hidden when official disabled")
	assert.True(t, IsKnown(off, "openshift"), "config pin stays known")
}

func TestSourceString(t *testing.T) {
	assert.Equal(t, "config-pin", SourceConfigPin.String())
	assert.Equal(t, "official", SourceOfficial.String())
	assert.Equal(t, "catalog", SourceCatalog.String())
	assert.Equal(t, "unknown", SourceUnknown.String())
	assert.Equal(t, "unknown", Source(99).String())
}

func TestResolve_SentinelErrorsAreDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrThirdPartyDisabled, ErrOfficialDisabled))
}
