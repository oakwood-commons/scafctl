// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// fakeCache is an in-memory InventoryCache for tests.
type fakeCache struct {
	store map[string][]Entry
	sets  int
}

func (f *fakeCache) Get(_ context.Context, key string) ([]Entry, bool) {
	e, ok := f.store[key]
	return e, ok
}

func (f *fakeCache) Set(_ context.Context, key string, entries []Entry, _ time.Duration) {
	if f.store == nil {
		f.store = map[string][]Entry{}
	}
	f.store[key] = entries
	f.sets++
}

func resolverCfg(authProvider string) *config.HostnameConfig {
	return &config.HostnameConfig{
		Resolver: &config.HostnameResolverConfig{
			Source: config.HostnameResolverSource{
				URL:          "https://inv.example.com",
				AuthProvider: authProvider,
			},
			Transform: "_",
			TTL:       "1h",
		},
	}
}

func TestResolveWith_Precedence(t *testing.T) {
	t.Parallel()

	inventory := []Entry{{Name: "cluster-a", URL: "https://api.cluster-a.example.com:6443"}}

	tests := []struct {
		name     string
		cfg      *config.HostnameConfig
		handler  string
		selector string
		fetch    FetchFunc
		token    TokenFunc
		want     string
		wantErr  error
	}{
		{
			name:     "concrete https URL passthrough",
			cfg:      resolverCfg(""),
			selector: "https://api.custom.example.com:6443",
			want:     "https://api.custom.example.com:6443",
		},
		{
			name:     "concrete http URL passthrough",
			selector: "http://localhost:8080",
			want:     "http://localhost:8080",
		},
		{
			name:     "nil config returns selector unchanged",
			cfg:      nil,
			selector: "some-ghes-host",
			want:     "some-ghes-host",
		},
		{
			name:     "static alias wins",
			cfg:      &config.HostnameConfig{Aliases: map[string]string{"prod": "https://api.prod.example.com:6443"}},
			selector: "prod",
			want:     "https://api.prod.example.com:6443",
		},
		{
			name:     "dynamic resolver lookup",
			cfg:      resolverCfg(""),
			selector: "cluster-a",
			want:     "https://api.cluster-a.example.com:6443",
		},
		{
			name: "static alias overlays inventory entry with same name",
			cfg: &config.HostnameConfig{
				Aliases: map[string]string{"cluster-a": "https://override.example.com:6443"},
				Resolver: &config.HostnameResolverConfig{
					Source:    config.HostnameResolverSource{URL: "https://inv.example.com"},
					Transform: "_",
				},
			},
			selector: "cluster-a",
			want:     "https://override.example.com:6443",
		},
		{
			name:     "selector not found in aliases only",
			cfg:      &config.HostnameConfig{Aliases: map[string]string{"prod": "https://api.prod.example.com"}},
			selector: "staging",
			wantErr:  ErrSelectorNotFound,
		},
		{
			name:     "selector not found in inventory",
			cfg:      resolverCfg(""),
			selector: "missing",
			wantErr:  ErrSelectorNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deps := Deps{
				Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
					return []byte(`{}`), nil
				},
				Transform: func(context.Context, string, []byte) ([]Entry, error) {
					return inventory, nil
				},
			}
			got, err := ResolveWith(context.Background(), tt.cfg, "openshift", tt.selector, deps)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveWith_LoopGuard(t *testing.T) {
	t.Parallel()

	cfg := resolverCfg("openshift")
	deps := Deps{
		Token: func(context.Context, string, string) (string, error) { return "tok", nil },
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) { return []byte(`{}`), nil },
		Transform: func(context.Context, string, []byte) ([]Entry, error) {
			return nil, nil
		},
	}

	_, err := ResolveWith(context.Background(), cfg, "openshift", "cluster-a", deps)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResolverLoop)
}

func TestResolveWith_NoCredentials(t *testing.T) {
	t.Parallel()

	cfg := resolverCfg("entra")
	tokenErr := errors.New("not authenticated")
	fetchCalled := false
	deps := Deps{
		Token: func(context.Context, string, string) (string, error) { return "", tokenErr },
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
			fetchCalled = true
			return nil, nil
		},
	}

	_, err := ResolveWith(context.Background(), cfg, "openshift", "cluster-a", deps)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoCredentials)
	assert.False(t, fetchCalled, "fetch must not run when credentials are missing")
}

func TestResolveWith_TransformShapeError(t *testing.T) {
	t.Parallel()

	cfg := resolverCfg("")
	deps := Deps{
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) { return []byte(`{}`), nil },
		Transform: func(context.Context, string, []byte) ([]Entry, error) {
			return nil, ErrTransformShape
		},
	}

	_, err := ResolveWith(context.Background(), cfg, "openshift", "cluster-a", deps)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTransformShape)
}

func TestResolveWith_TokenInjectedIntoFetch(t *testing.T) {
	t.Parallel()

	cfg := resolverCfg("entra")
	var gotBearer string
	deps := Deps{
		Token: func(_ context.Context, provider, scope string) (string, error) {
			assert.Equal(t, "entra", provider)
			return "secret-token", nil
		},
		Fetch: func(_ context.Context, _ config.HostnameResolverSource, bearer string) ([]byte, error) {
			gotBearer = bearer
			return []byte(`{}`), nil
		},
		Transform: func(context.Context, string, []byte) ([]Entry, error) {
			return []Entry{{Name: "cluster-a", URL: "https://api.cluster-a.example.com:6443"}}, nil
		},
	}

	got, err := ResolveWith(context.Background(), cfg, "openshift", "cluster-a", deps)
	require.NoError(t, err)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", got)
	assert.Equal(t, "secret-token", gotBearer)
}

func TestResolveWith_CacheHitSkipsFetch(t *testing.T) {
	t.Parallel()

	cfg := resolverCfg("")
	cache := &fakeCache{store: map[string][]Entry{}}
	// Pre-seed the cache under the computed key.
	key := cacheKey("openshift", cfg.Resolver)
	cache.store[key] = []Entry{{Name: "cluster-a", URL: "https://cached.example.com:6443"}}

	fetchCalled := false
	deps := Deps{
		Cache: cache,
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
			fetchCalled = true
			return []byte(`{}`), nil
		},
		Transform: func(context.Context, string, []byte) ([]Entry, error) {
			return nil, errors.New("should not be called")
		},
	}

	got, err := ResolveWith(context.Background(), cfg, "openshift", "cluster-a", deps)
	require.NoError(t, err)
	assert.Equal(t, "https://cached.example.com:6443", got)
	assert.False(t, fetchCalled, "fetch must be skipped on cache hit")
}

func TestResolveWith_CacheMissStoresResult(t *testing.T) {
	t.Parallel()

	cfg := resolverCfg("")
	cache := &fakeCache{store: map[string][]Entry{}}
	deps := Deps{
		Cache: cache,
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) { return []byte(`{}`), nil },
		Transform: func(context.Context, string, []byte) ([]Entry, error) {
			return []Entry{{Name: "cluster-a", URL: "https://api.cluster-a.example.com:6443"}}, nil
		},
	}

	_, err := ResolveWith(context.Background(), cfg, "openshift", "cluster-a", deps)
	require.NoError(t, err)
	assert.Equal(t, 1, cache.sets, "resolved inventory should be cached")
}

func TestParseTTL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"1h", time.Hour},
		{"30m", 30 * time.Minute},
		{"garbage", 0},
		{"-5m", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, parseTTL(tt.in))
		})
	}
}

func TestIsConcreteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want bool
	}{
		{"https://api.example.com:6443", true},
		{"http://localhost:8080", true},
		{"cluster-a", false},
		{"ftp://example.com", false},
		{"", false},
		{"api.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isConcreteURL(tt.in))
		})
	}
}

func TestResolve_UsesContextConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {
					Hostname: &config.HostnameConfig{
						Aliases: map[string]string{"prod": "https://api.prod.example.com:6443"},
					},
				},
			},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)

	got, err := Resolve(ctx, "openshift", "prod")
	require.NoError(t, err)
	assert.Equal(t, "https://api.prod.example.com:6443", got)
}

func TestResolve_NoConfigReturnsSelector(t *testing.T) {
	t.Parallel()

	got, err := Resolve(context.Background(), "github", "github.example.com")
	require.NoError(t, err)
	assert.Equal(t, "github.example.com", got)
}

func TestResolveEntryWith_CarriesOIDCFields(t *testing.T) {
	t.Parallel()

	cfg := resolverCfg("")
	want := Entry{
		Name:            "cluster-01",
		URL:             "https://api.cluster-01.example.com:6443",
		Audience:        "api://cluster-01/.default",
		AuthType:        "oidc",
		CAData:          "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----",
		ConsoleURL:      "https://console.cluster-01.example.com",
		InsecureSkipTLS: false,
	}
	deps := Deps{
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) { return []byte(`{}`), nil },
		Transform: func(context.Context, string, []byte) ([]Entry, error) {
			return []Entry{want}, nil
		},
	}

	got, err := ResolveEntryWith(context.Background(), cfg, "openshift", "cluster-01", deps)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestResolveEntryWith_ConcreteURLPassthrough(t *testing.T) {
	t.Parallel()

	got, err := ResolveEntryWith(context.Background(), nil, "openshift", "https://api.custom.example.com:6443", Deps{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "https://api.custom.example.com:6443", got.Name)
	assert.Equal(t, "https://api.custom.example.com:6443", got.URL)
	assert.Empty(t, got.Audience)
	assert.Empty(t, got.AuthType)
}

func TestResolveEntryWith_StaticAliasEntry(t *testing.T) {
	t.Parallel()

	cfg := &config.HostnameConfig{Aliases: map[string]string{"prod": "https://api.prod.example.com:6443"}}
	got, err := ResolveEntryWith(context.Background(), cfg, "openshift", "prod", Deps{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "prod", got.Name)
	assert.Equal(t, "https://api.prod.example.com:6443", got.URL)
}

func TestResolveEntryWith_NotFound(t *testing.T) {
	t.Parallel()

	cfg := resolverCfg("")
	deps := Deps{
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) { return []byte(`{}`), nil },
		Transform: func(context.Context, string, []byte) ([]Entry, error) {
			return []Entry{{Name: "cluster-01", URL: "https://api.cluster-01.example.com:6443"}}, nil
		},
	}

	got, err := ResolveEntryWith(context.Background(), cfg, "openshift", "missing", deps)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSelectorNotFound)
	assert.Nil(t, got)
}

func TestDefaultDeps(t *testing.T) {
	t.Parallel()

	d := DefaultDeps()
	assert.NotNil(t, d.Fetch, "Fetch default must be set")
	assert.NotNil(t, d.Token, "Token default must be set")
	assert.NotNil(t, d.Transform, "Transform default must be set")
	assert.NotNil(t, d.Cache, "on-disk inventory cache must be set")
}

func TestResolveInventory(t *testing.T) {
	t.Parallel()

	rc := &config.HostnameResolverConfig{
		Source:    config.HostnameResolverSource{URL: "https://clusters.example.com"},
		Transform: "_",
		TTL:       "1h",
	}
	cache := &fakeCache{}
	deps := Deps{
		Fetch: func(context.Context, config.HostnameResolverSource, string) ([]byte, error) {
			return []byte("{}"), nil
		},
		Transform: func(context.Context, string, []byte) ([]Entry, error) {
			return []Entry{{Name: "cluster-a", URL: "https://api.pd:6443"}}, nil
		},
		Cache: cache,
	}

	entries, err := ResolveInventory(context.Background(), rc, "kube", deps)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "cluster-a", entries[0].Name)
	assert.Equal(t, 1, cache.sets, "resolved inventory must be cached")
}

func TestCachedInventory(t *testing.T) {
	t.Parallel()

	rc := &config.HostnameResolverConfig{
		Source:    config.HostnameResolverSource{URL: "https://clusters.example.com"},
		Transform: "_",
	}

	t.Run("miss when no cache", func(t *testing.T) {
		t.Parallel()
		_, ok := CachedInventory(context.Background(), rc, "kube", Deps{})
		assert.False(t, ok)
	})

	t.Run("hit returns cached entries without fetching", func(t *testing.T) {
		t.Parallel()
		cache := &fakeCache{store: map[string][]Entry{
			cacheKey("kube", rc): {{Name: "cluster-a", URL: "https://api.pd:6443"}},
		}}
		// No Fetch provided: CachedInventory must never call it.
		entries, ok := CachedInventory(context.Background(), rc, "kube", Deps{Cache: cache})
		require.True(t, ok)
		require.Len(t, entries, 1)
		assert.Equal(t, "cluster-a", entries[0].Name)
	})
}
