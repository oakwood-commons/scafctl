// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package clusterconfig

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth/hostname"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/kube"
)

// inventoryDeps returns hostname.Deps that yield the given entries without any
// network access.
func inventoryDeps(entries []hostname.Entry) hostname.Deps {
	return hostname.Deps{
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
			return []byte("{}"), nil
		},
		Transform: func(context.Context, string, []byte) ([]hostname.Entry, error) {
			return entries, nil
		},
	}
}

func resolverConfig() *config.HostnameResolverConfig {
	return &config.HostnameResolverConfig{
		Source:    config.HostnameResolverSource{URL: "https://clusters.example.com"},
		Transform: "_",
	}
}

// fakeCache is an in-memory hostname.InventoryCache for exercising the cached
// (non-fetching) completion path.
type fakeCache struct {
	entries []hostname.Entry
	hit     bool
}

func (f *fakeCache) Get(context.Context, string) ([]hostname.Entry, bool) {
	return f.entries, f.hit
}

func (f *fakeCache) Set(context.Context, string, []hostname.Entry, time.Duration) {}

func TestResolver_ListCached_AliasesOnlyNoFetch(t *testing.T) {
	t.Parallel()

	fetched := false
	deps := hostname.Deps{
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
			fetched = true
			return nil, errors.New("network must not be touched during completion")
		},
		// No cache: CachedInventory returns (nil,false) so inventory is skipped.
	}
	r := New(config.ClusterResolutionConfig{
		Aliases:  map[string]config.ClusterAlias{"lab": {Server: "https://api.lab:6443"}},
		Resolver: resolverConfig(),
	}, WithDeps(deps))

	list := r.ListCached(context.Background())
	require.Len(t, list, 1)
	assert.Equal(t, "lab", list[0].Name)
	assert.False(t, fetched, "ListCached must not trigger a network fetch")
}

func TestResolver_ListCached_UsesCachedInventory(t *testing.T) {
	t.Parallel()

	deps := hostname.Deps{
		Cache: &fakeCache{
			hit:     true,
			entries: []hostname.Entry{{Name: "cluster-a", URL: "https://api.pd:6443"}},
		},
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
			t.Fatal("Fetch must not be called when the inventory is cached")
			return nil, nil
		},
	}
	r := New(config.ClusterResolutionConfig{
		Aliases:  map[string]config.ClusterAlias{"lab": {Server: "https://api.lab:6443"}},
		Resolver: resolverConfig(),
	}, WithDeps(deps))

	list := r.ListCached(context.Background())
	names := make([]string, len(list))
	for i, c := range list {
		names[i] = c.Name
	}
	assert.ElementsMatch(t, []string{"lab", "cluster-a"}, names)
}

func TestResolver_Enabled(t *testing.T) {
	t.Parallel()

	assert.False(t, New(config.ClusterResolutionConfig{}).Enabled())
	assert.True(t, New(config.ClusterResolutionConfig{
		Aliases: map[string]config.ClusterAlias{"lab": {Server: "https://api.lab:6443"}},
	}).Enabled())
	assert.True(t, New(config.ClusterResolutionConfig{Resolver: resolverConfig()}).Enabled())
}

func TestResolver_Resolve_Alias(t *testing.T) {
	t.Parallel()

	r := New(config.ClusterResolutionConfig{
		Aliases: map[string]config.ClusterAlias{
			"lab": {
				Server:          "https://api.lab.example.com:6443",
				DefaultHandler:  "openshift",
				AuthType:        "oauth",
				OIDCAudience:    "lab-aud",
				CAData:          "ca-bytes",
				ConsoleURL:      "https://console.lab.example.com",
				InsecureSkipTLS: true,
			},
		},
	})

	info, err := r.Resolve(context.Background(), "lab")
	require.NoError(t, err)
	assert.Equal(t, "lab", info.Name)
	assert.Equal(t, "https://api.lab.example.com:6443", info.APIServerURL)
	assert.Equal(t, "openshift", info.DefaultHandler)
	assert.Equal(t, kube.AuthTypeOAuth, info.AuthType)
	assert.Equal(t, "lab-aud", info.OIDCAudience)
	assert.Equal(t, "ca-bytes", info.CAData)
	assert.Equal(t, "https://console.lab.example.com", info.ConsoleURL)
	assert.True(t, info.InsecureSkipTLS)
}

func TestResolver_Resolve_Inventory(t *testing.T) {
	t.Parallel()

	deps := inventoryDeps([]hostname.Entry{
		{Name: "cluster-a", URL: "https://api.pd.example.com:6443", DefaultHandler: "openshift", AuthType: "oauth", Audience: "pd-aud"},
	})
	r := New(config.ClusterResolutionConfig{Resolver: resolverConfig()}, WithDeps(deps))

	info, err := r.Resolve(context.Background(), "cluster-a")
	require.NoError(t, err)
	assert.Equal(t, "cluster-a", info.Name)
	assert.Equal(t, "https://api.pd.example.com:6443", info.APIServerURL)
	assert.Equal(t, "openshift", info.DefaultHandler)
	assert.Equal(t, kube.AuthTypeOAuth, info.AuthType)
	assert.Equal(t, "pd-aud", info.OIDCAudience)
}

func TestResolver_Resolve_AliasWinsOverInventory(t *testing.T) {
	t.Parallel()

	deps := inventoryDeps([]hostname.Entry{
		{Name: "prod", URL: "https://inventory.example.com:6443", DefaultHandler: "entra"},
	})
	r := New(config.ClusterResolutionConfig{
		Aliases:  map[string]config.ClusterAlias{"prod": {Server: "https://alias.example.com:6443", DefaultHandler: "openshift"}},
		Resolver: resolverConfig(),
	}, WithDeps(deps))

	info, err := r.Resolve(context.Background(), "prod")
	require.NoError(t, err)
	assert.Equal(t, "https://alias.example.com:6443", info.APIServerURL,
		"static alias must win over an inventory entry of the same name")
	assert.Equal(t, "openshift", info.DefaultHandler)
}

func TestResolver_Resolve_NotFound(t *testing.T) {
	t.Parallel()

	r := New(config.ClusterResolutionConfig{
		Aliases: map[string]config.ClusterAlias{"lab": {Server: "https://api.lab:6443"}},
	})

	_, err := r.Resolve(context.Background(), "missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClusterNotFound)
}

func TestResolver_Resolve_InventoryMiss(t *testing.T) {
	t.Parallel()

	deps := inventoryDeps([]hostname.Entry{
		{Name: "cluster-a", URL: "https://api.pd.example.com:6443"},
	})
	r := New(config.ClusterResolutionConfig{Resolver: resolverConfig()}, WithDeps(deps))

	_, err := r.Resolve(context.Background(), "nope")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClusterNotFound,
		"an inventory miss must normalize to ErrClusterNotFound like an alias miss")
}

func TestResolver_Resolve_AliasMissingServer(t *testing.T) {
	t.Parallel()

	// A static alias with no server is a config mistake; resolving it must fail
	// with a targeted message rather than flowing through as an empty server.
	r := New(config.ClusterResolutionConfig{
		Aliases: map[string]config.ClusterAlias{"lab": {DefaultHandler: "openshift"}},
	})

	_, err := r.Resolve(context.Background(), "lab")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster alias \"lab\" is missing a server")
}

func TestResolver_Resolve_InventoryFetchError(t *testing.T) {
	t.Parallel()

	// A non-not-found inventory failure (e.g. network down) is surfaced as-is,
	// not normalized to ErrClusterNotFound.
	failDeps := hostname.Deps{
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
			return nil, errors.New("network down")
		},
	}
	r := New(config.ClusterResolutionConfig{Resolver: resolverConfig()}, WithDeps(failDeps))

	_, err := r.Resolve(context.Background(), "prod")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrClusterNotFound)
	assert.Contains(t, err.Error(), "resolve cluster \"prod\" from inventory")
}

func TestResolver_Resolve_EmptyName(t *testing.T) {
	t.Parallel()

	_, err := New(config.ClusterResolutionConfig{}).Resolve(context.Background(), "")
	require.Error(t, err)
}

func TestResolver_List(t *testing.T) {
	t.Parallel()

	deps := inventoryDeps([]hostname.Entry{
		{Name: "prod", URL: "https://inventory-prod:6443"}, // shadowed by alias
		{Name: "stage", URL: "https://inventory-stage:6443", DefaultHandler: "openshift"},
	})
	r := New(config.ClusterResolutionConfig{
		Aliases: map[string]config.ClusterAlias{
			"lab":  {Server: "https://api.lab:6443"},
			"prod": {Server: "https://alias-prod:6443"},
		},
		Resolver: resolverConfig(),
	}, WithDeps(deps))

	list, err := r.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 3)

	byName := map[string]kube.ClusterInfo{}
	for _, c := range list {
		byName[c.Name] = c
	}
	// Aliases come first, sorted; alias "prod" shadows the inventory entry.
	assert.Equal(t, "lab", list[0].Name)
	assert.Equal(t, "prod", list[1].Name)
	assert.Equal(t, "https://alias-prod:6443", byName["prod"].APIServerURL)
	assert.Equal(t, "https://inventory-stage:6443", byName["stage"].APIServerURL)
}

func TestResolver_List_AliasesOnly(t *testing.T) {
	t.Parallel()

	r := New(config.ClusterResolutionConfig{
		Aliases: map[string]config.ClusterAlias{
			"b": {Server: "https://b:6443"},
			"a": {Server: "https://a:6443"},
		},
	})
	list, err := r.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "a", list[0].Name, "aliases must be sorted")
	assert.Equal(t, "b", list[1].Name)
}

func TestResolver_List_InventoryErrorReturnsAliases(t *testing.T) {
	t.Parallel()

	failDeps := hostname.Deps{
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
			return nil, errors.New("network down")
		},
	}
	r := New(config.ClusterResolutionConfig{
		Aliases:  map[string]config.ClusterAlias{"lab": {Server: "https://api.lab:6443"}},
		Resolver: resolverConfig(),
	}, WithDeps(failDeps))

	list, err := r.List(context.Background())
	require.Error(t, err)
	require.Len(t, list, 1, "static aliases must still be returned when inventory fetch fails")
	assert.Equal(t, "lab", list[0].Name)
}
