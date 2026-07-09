// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package statusrows

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

func instanceConfigCtx(handler string, aliases map[string]string) context.Context {
	return config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				handler: {
					Hostname: &config.HostnameConfig{Aliases: aliases},
				},
			},
		},
	})
}

func TestMostRecentTokenIndex(t *testing.T) {
	t.Parallel()

	now := time.Now()
	assert.Equal(t, -1, mostRecentTokenIndex(nil))
	assert.Equal(t, -1, mostRecentTokenIndex([]*auth.CachedTokenInfo{{}, {}}))

	tokens := []*auth.CachedTokenInfo{
		{CachedAt: now.Add(-time.Hour)},
		{CachedAt: now},
		{CachedAt: now.Add(-2 * time.Hour)},
	}
	assert.Equal(t, 1, mostRecentTokenIndex(tokens))
}

// TestPreferInstanceToken covers representative selection: a user/login flow
// beats a machine flow regardless of age; same-class tokens break ties by the
// most-recent CachedAt.
func TestPreferInstanceToken(t *testing.T) {
	t.Parallel()

	now := time.Now()
	user := &auth.CachedTokenInfo{Flow: auth.FlowInteractive, CachedAt: now.Add(-time.Hour)}
	machine := &auth.CachedTokenInfo{Flow: auth.FlowServicePrincipal, CachedAt: now}

	// User/login flow wins even when the machine token is newer.
	assert.True(t, preferInstanceToken(user, machine))
	assert.False(t, preferInstanceToken(machine, user))

	// Same class -> newer CachedAt wins.
	older := &auth.CachedTokenInfo{Flow: auth.FlowInteractive, CachedAt: now.Add(-time.Hour)}
	newer := &auth.CachedTokenInfo{Flow: auth.FlowInteractive, CachedAt: now}
	assert.True(t, preferInstanceToken(newer, older))
	assert.False(t, preferInstanceToken(older, newer))
}

func TestIsUserFlow(t *testing.T) {
	t.Parallel()

	assert.True(t, isUserFlow(auth.FlowInteractive))
	assert.True(t, isUserFlow(auth.FlowDeviceCode))
	assert.True(t, isUserFlow(auth.FlowPAT))
	assert.False(t, isUserFlow(auth.FlowServicePrincipal))
	assert.False(t, isUserFlow(auth.FlowClientCredentials))
	assert.False(t, isUserFlow(""))
}

func TestDedupInstanceTokens(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tokens := []*auth.CachedTokenInfo{
		nil,            // skipped
		{Hostname: ""}, // skipped (no host)
		{Hostname: "https://a.example.com", Flow: auth.FlowServicePrincipal, CachedAt: now},
		{Hostname: "https://a.example.com", Flow: auth.FlowInteractive, CachedAt: now.Add(-time.Hour)},
		{Hostname: "https://b.example.com", Flow: auth.FlowInteractive, CachedAt: now},
	}

	out := dedupInstanceTokens(tokens)
	require.Len(t, out, 2, "one representative per hostname; nil/no-host skipped")

	byHost := map[string]*auth.CachedTokenInfo{}
	for _, tk := range out {
		byHost[tk.Hostname] = tk
	}
	// The user/login flow represents host a even though the SA token is newer.
	assert.Equal(t, auth.FlowInteractive, byHost["https://a.example.com"].Flow)
	assert.Equal(t, auth.FlowInteractive, byHost["https://b.example.com"].Flow)
}

func TestClusterLabel(t *testing.T) {
	t.Parallel()

	aliases := map[string]string{"pd1020": "https://api.pd1020.example.com:6443"}

	// Alias match -> short selector.
	assert.Equal(t, "pd1020", ClusterLabel(aliases, "https://api.pd1020.example.com:6443"))
	// No alias -> trimmed display host.
	assert.Equal(t, "api.other.example.com", ClusterLabel(aliases, "https://api.other.example.com:6443"))
	// No aliases at all -> display host.
	assert.Equal(t, "api.x.example.com", ClusterLabel(nil, "https://api.x.example.com"))
}

func TestInstanceAliases(t *testing.T) {
	t.Parallel()

	// No config in context.
	assert.Nil(t, InstanceAliases(context.Background(), "openshift"))

	ctx := instanceConfigCtx("openshift", map[string]string{"pd1020": "https://api.pd1020.example.com"})
	got := InstanceAliases(ctx, "openshift")
	assert.Equal(t, "https://api.pd1020.example.com", got["pd1020"])

	// Unknown handler -> nil.
	assert.Nil(t, InstanceAliases(ctx, "other"))

	// A resolver-configured handler with no cached inventory still returns the
	// static aliases (the resolver branch is a no-op without a cache hit).
	resolverCtx := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {
					Hostname: &config.HostnameConfig{
						Aliases:  map[string]string{"pd1020": "https://api.pd1020.example.com"},
						Resolver: &config.HostnameResolverConfig{Transform: "_"},
					},
				},
			},
		},
	})
	resolved := InstanceAliases(resolverCtx, "openshift")
	assert.Equal(t, "https://api.pd1020.example.com", resolved["pd1020"])
}

func TestExpand_NonInstanceHandler(t *testing.T) {
	t.Parallel()

	h := auth.NewMockHandler("entra")
	h.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin}
	assert.Nil(t, Expand(context.Background(), "entra", h, &auth.Status{Authenticated: true}))
}

func TestExpand_NotAuthenticated(t *testing.T) {
	t.Parallel()

	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	assert.Nil(t, Expand(context.Background(), "openshift", h, &auth.Status{Authenticated: false}))
	assert.Nil(t, Expand(context.Background(), "openshift", h, nil))
}

func TestExpand_NoHostnameTokens(t *testing.T) {
	t.Parallel()

	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	h.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{Handler: "openshift", TokenKind: "access_token"}, // no Hostname
	}
	assert.Nil(t, Expand(context.Background(), "openshift", h, &auth.Status{Authenticated: true}))
}

func TestExpand_TwoClusters(t *testing.T) {
	t.Parallel()

	now := time.Now()
	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	h.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{Handler: "openshift", Hostname: "https://api.np0510.example.com:6443", Flow: auth.FlowInteractive, CachedAt: now.Add(-2 * time.Hour)},
		{Handler: "openshift", Hostname: "https://api.pd1020.example.com:6443", Flow: auth.FlowInteractive, CachedAt: now}, // most recent -> active
	}
	ctx := instanceConfigCtx("openshift", map[string]string{"pd1020": "https://api.pd1020.example.com:6443"})

	sessions := Expand(ctx, "openshift", h, &auth.Status{Authenticated: true})
	require.Len(t, sessions, 2)

	// Sorted by hostname: np0510 < pd1020.
	assert.Equal(t, "api.np0510.example.com", sessions[0].ClusterLabel)
	assert.False(t, sessions[0].Active)
	assert.Equal(t, "pd1020", sessions[1].ClusterLabel)
	assert.True(t, sessions[1].Active, "most recent CachedAt is active")
}

// TestExpand_DedupesPerCluster asserts several tokens for the same instance
// collapse to a single session that reflects the user/login flow.
func TestExpand_DedupesPerCluster(t *testing.T) {
	t.Parallel()

	now := time.Now()
	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	h.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{Handler: "openshift", Hostname: "https://api.pd1020.example.com:6443", Flow: auth.FlowInteractive, CachedAt: now.Add(-time.Hour)},
		{Handler: "openshift", Hostname: "https://api.pd1020.example.com:6443", Flow: auth.FlowServicePrincipal, CachedAt: now},
	}

	sessions := Expand(context.Background(), "openshift", h, &auth.Status{Authenticated: true})
	require.Len(t, sessions, 1)
	assert.Equal(t, auth.FlowInteractive, sessions[0].Token.Flow, "user/login flow wins over newer machine token")
}
