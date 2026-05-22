package authdelegation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Constructor tests ──

func TestNewEntraDelegator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  EntraDelegatorConfig
		err  error
	}{
		{"no tenant", EntraDelegatorConfig{ClientID: "c", CredentialType: CredentialTypeWIF, FederatedTokenFile: "f"}, ErrEntraNoTenantID},
		{"no client", EntraDelegatorConfig{TenantID: "t", CredentialType: CredentialTypeWIF, FederatedTokenFile: "f"}, ErrEntraNoClientID},
		{"invalid cred type", EntraDelegatorConfig{TenantID: "t", ClientID: "c", CredentialType: "bad"}, ErrEntraInvalidCredentialType},
		{"empty cred type", EntraDelegatorConfig{TenantID: "t", ClientID: "c"}, ErrEntraInvalidCredentialType},
		{"wif no file", EntraDelegatorConfig{TenantID: "t", ClientID: "c", CredentialType: CredentialTypeWIF}, ErrEntraWIFMissingTokenFile},
		{"secret no value", EntraDelegatorConfig{TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret}, ErrEntraSecretMissing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewEntraDelegator(tc.cfg)
			assert.ErrorIs(t, err, tc.err)
		})
	}

	t.Run("valid wif", func(t *testing.T) {
		t.Parallel()
		d, err := NewEntraDelegator(EntraDelegatorConfig{
			TenantID: "t", ClientID: "c", CredentialType: CredentialTypeWIF, FederatedTokenFile: "/tok",
		})
		require.NoError(t, err)
		assert.NotNil(t, d)
		assert.NotNil(t, d.flowRegistry)
	})

	t.Run("valid secret", func(t *testing.T) {
		t.Parallel()
		d, err := NewEntraDelegator(EntraDelegatorConfig{
			TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret, ClientSecret: "s",
		})
		require.NoError(t, err)
		assert.NotNil(t, d)
	})

	t.Run("with manager option sets manager", func(t *testing.T) {
		t.Parallel()
		mgr := manager.NewManager[string, TokenResult]()
		d, err := NewEntraDelegator(EntraDelegatorConfig{
			TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret, ClientSecret: "s",
		}, WithManager(mgr))
		require.NoError(t, err)
		assert.NotNil(t, d.manager)
	})

	t.Run("without manager option leaves manager nil", func(t *testing.T) {
		t.Parallel()
		d, err := NewEntraDelegator(EntraDelegatorConfig{
			TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret, ClientSecret: "s",
		})
		require.NoError(t, err)
		assert.Nil(t, d.manager)
	})
}

// ── DelegateToken tests ──

func TestDelegateToken(t *testing.T) {
	t.Parallel()

	t.Run("missing token in context returns error", func(t *testing.T) {
		t.Parallel()
		d := mustDelegator(t, CredentialTypeSecret, "secret-val")
		_, err := d.DelegateToken(context.Background(), "scope/.default")
		assert.ErrorContains(t, err, "no caller token in context")
	})

	t.Run("empty scope returns error", func(t *testing.T) {
		t.Parallel()
		d := mustDelegator(t, CredentialTypeSecret, "secret-val")
		ctx := middleware.WithAccessToken(context.Background(), "some-jwt")
		ctx = middleware.WithAuthClaims(ctx, &middleware.AuthClaims{IDType: "user"})
		_, err := d.DelegateToken(ctx, "")
		assert.ErrorIs(t, err, ErrNoScope)
	})

	t.Run("OBO success for user caller", func(t *testing.T) {
		t.Parallel()
		srv := fakeTokenEndpoint(t, http.StatusOK, map[string]any{
			"access_token": "delegated-token", "token_type": "Bearer", "expires_in": 3600, "scope": "api/.default",
		})

		d := mustDelegatorWithURL(t, "secret-val", srv.URL)
		ctx := authContext("caller-jwt", "user")

		tok, err := d.DelegateToken(ctx, "api/.default")
		require.NoError(t, err)
		assert.Equal(t, "delegated-token", tok.AccessToken)
		assert.Equal(t, int64(3600), tok.ExpiresIn)
	})

	t.Run("app caller uses client credentials", func(t *testing.T) {
		t.Parallel()
		srv := fakeTokenEndpoint(t, http.StatusOK, map[string]any{
			"access_token": "cc-token", "token_type": "Bearer", "expires_in": 3600, "scope": "api/.default",
		})

		d := mustDelegatorWithURL(t, "s", srv.URL)
		ctx := authContext("app-jwt", "app")

		tok, err := d.DelegateToken(ctx, "api/.default")
		require.NoError(t, err)
		assert.Equal(t, "cc-token", tok.AccessToken)
		assert.Equal(t, int64(3600), tok.ExpiresIn)
	})

	t.Run("token endpoint error propagates", func(t *testing.T) {
		t.Parallel()
		srv := fakeTokenEndpoint(t, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})

		d := mustDelegatorWithURL(t, "s", srv.URL)
		ctx := authContext("bad-jwt", "user")

		_, err := d.DelegateToken(ctx, "scope/.default")
		assert.ErrorContains(t, err, "token endpoint returned 400")
	})

	t.Run("nil claims defaults callerType to empty", func(t *testing.T) {
		t.Parallel()
		srv := fakeTokenEndpoint(t, http.StatusOK, map[string]any{
			"access_token": "tok", "token_type": "Bearer", "expires_in": 60, "scope": "s",
		})

		d := mustDelegatorWithURL(t, "s", srv.URL)
		ctx := middleware.WithAccessToken(context.Background(), "some-jwt")

		tok, err := d.DelegateToken(ctx, "s")
		require.NoError(t, err)
		assert.Equal(t, "tok", tok.AccessToken)
	})
}

// ── DelegateToken with Manager tests ──

func TestDelegateToken_WithManager_CacheHit(t *testing.T) {
	t.Parallel()

	var hitCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "cached-token", "expires_in": 3600,
		})
	}))
	t.Cleanup(srv.Close)

	tokenCache := NewTokenCache[string, TokenResult](context.Background(), 100, 0, 5*time.Minute)
	mgr := manager.NewManager(
		manager.WithStore("test", tokenCache),
	)

	d := mustDelegatorWithURL(t, "s", srv.URL)
	d.manager = mgr

	ctx := authContext("caller-jwt", "user")

	// First call — should hit the server
	tok1, err := d.DelegateToken(ctx, "api/.default")
	require.NoError(t, err)
	assert.Equal(t, "cached-token", tok1.AccessToken)
	assert.Equal(t, int64(3600), tok1.ExpiresIn)
	assert.Equal(t, int32(1), hitCount.Load())

	// Second call — same params, should come from cache
	tok2, err := d.DelegateToken(ctx, "api/.default")
	require.NoError(t, err)
	assert.Equal(t, "cached-token", tok2.AccessToken)
	assert.Equal(t, int32(1), hitCount.Load(), "expected flow to be called only once")
}

func TestDelegateToken_WithManager_CacheMiss(t *testing.T) {
	t.Parallel()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh-token", "expires_in": 1800,
		})
	}))
	t.Cleanup(srv.Close)

	store := &spyStore[string, TokenResult]{}
	mgr := manager.NewManager(
		manager.WithStore("spy", store),
	)

	d := mustDelegatorWithURL(t, "s", srv.URL)
	d.manager = mgr

	ctx := authContext("caller-jwt", "user")

	tok, err := d.DelegateToken(ctx, "api/.default")
	require.NoError(t, err)
	assert.Equal(t, "fresh-token", tok.AccessToken)
	assert.Equal(t, int64(1800), tok.ExpiresIn)

	// Store was empty → miss on Get, then Set was called after fetch
	assert.Equal(t, int32(1), store.gets.Load(), "expected one cache lookup")
	assert.Equal(t, int32(1), store.sets.Load(), "expected result to be stored after miss")
	assert.Equal(t, int32(1), fetchCount.Load(), "expected one fetch from token endpoint")
}

func TestDelegateToken_WithManager_KeyGenFails_BypassesCache(t *testing.T) {
	t.Parallel()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "direct-token", "expires_in": 900,
		})
	}))
	t.Cleanup(srv.Close)

	store := &spyStore[string, TokenResult]{}
	mgr := manager.NewManager(
		manager.WithStore("spy", store),
	)

	d := mustDelegatorWithURL(t, "s", srv.URL)
	d.manager = mgr

	// Use a context with token but no claims → callerType="" → NoOpKeyGenerator → returns false
	ctx := middleware.WithAccessToken(context.Background(), "some-jwt")

	tok, err := d.DelegateToken(ctx, "api/.default")
	require.NoError(t, err)
	assert.Equal(t, "direct-token", tok.AccessToken)
	assert.Equal(t, int64(900), tok.ExpiresIn)

	// Cache was never consulted — flow called directly
	assert.Equal(t, int32(0), store.gets.Load(), "expected no cache lookup when key gen fails")
	assert.Equal(t, int32(0), store.sets.Load(), "expected no cache write when key gen fails")
	assert.Equal(t, int32(1), fetchCount.Load())
}

func TestDelegateToken_WithManager_FlowError_NotCached(t *testing.T) {
	t.Parallel()

	srv := fakeTokenEndpoint(t, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})

	store := &spyStore[string, TokenResult]{}
	mgr := manager.NewManager(
		manager.WithStore("spy", store),
	)

	d := mustDelegatorWithURL(t, "s", srv.URL)
	d.manager = mgr

	ctx := authContext("caller-jwt", "user")

	_, err := d.DelegateToken(ctx, "api/.default")
	assert.ErrorContains(t, err, "token endpoint returned 400")

	// Error result must not be cached
	assert.Equal(t, int32(1), store.gets.Load(), "expected one cache lookup")
	assert.Equal(t, int32(0), store.sets.Load(), "expected no cache write on flow error")
}

func TestDelegateToken_WithManager_TTLDerivedFromExpiresIn(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ttl-token", "expires_in": 7200,
		})
	}))
	t.Cleanup(srv.Close)

	store := &spyStore[string, TokenResult]{}
	mgr := manager.NewManager(
		manager.WithStore("spy", store),
	)

	d := mustDelegatorWithURL(t, "s", srv.URL)
	d.manager = mgr

	ctx := authContext("caller-jwt", "user")

	_, err := d.DelegateToken(ctx, "api/.default")
	require.NoError(t, err)

	assert.Equal(t, int32(1), store.sets.Load())
	assert.Equal(t, 7200*time.Second, store.lastTTL, "TTL should be derived from expires_in seconds")
}

func TestDelegateToken_WithManager_DifferentScopes_DifferentKeys(t *testing.T) {
	t.Parallel()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		_ = r.ParseForm()
		scope := r.PostForm.Get("scope")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-for-" + scope, "expires_in": 3600,
		})
	}))
	t.Cleanup(srv.Close)

	tokenCache := NewTokenCache[string, TokenResult](context.Background(), 100, 0, 5*time.Minute)
	mgr := manager.NewManager(
		manager.WithStore("test", tokenCache),
	)

	d := mustDelegatorWithURL(t, "s", srv.URL)
	d.manager = mgr

	ctx := authContext("caller-jwt", "user")

	tok1, err := d.DelegateToken(ctx, "scope-a/.default")
	require.NoError(t, err)
	assert.Equal(t, "token-for-scope-a/.default", tok1.AccessToken)

	tok2, err := d.DelegateToken(ctx, "scope-b/.default")
	require.NoError(t, err)
	assert.Equal(t, "token-for-scope-b/.default", tok2.AccessToken)

	// Both scopes triggered separate fetches
	assert.Equal(t, int32(2), fetchCount.Load(), "different scopes must produce different cache keys")

	// Re-requesting scope-a should hit cache, not fetch again
	tok3, err := d.DelegateToken(ctx, "scope-a/.default")
	require.NoError(t, err)
	assert.Equal(t, "token-for-scope-a/.default", tok3.AccessToken)
	assert.Equal(t, int32(2), fetchCount.Load(), "repeated scope should be served from cache")
}

func TestDelegateToken_WithManager_Deduplication(t *testing.T) {
	t.Parallel()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetchCount.Add(1)
		// Simulate slow token endpoint to ensure concurrent calls overlap
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "deduped-token", "expires_in": 3600,
		})
	}))
	t.Cleanup(srv.Close)

	tokenCache := NewTokenCache[string, TokenResult](context.Background(), 100, 0, 5*time.Minute)
	mgr := manager.NewManager(
		manager.WithStore("test", tokenCache),
	)

	d := mustDelegatorWithURL(t, "s", srv.URL)
	d.manager = mgr

	ctx := authContext("caller-jwt", "user")
	const concurrency = 10

	var wg sync.WaitGroup
	results := make([]TokenResult, concurrency)
	errs := make([]error, concurrency)

	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = d.DelegateToken(ctx, "api/.default")
		}(i)
	}
	wg.Wait()

	for i := range concurrency {
		require.NoError(t, errs[i], "goroutine %d failed", i)
		assert.Equal(t, "deduped-token", results[i].AccessToken)
	}

	// Singleflight: only one fetch despite 10 concurrent callers
	assert.Equal(t, int32(1), fetchCount.Load(), "expected single fetch due to singleflight deduplication")
}

// ── FlowRegistry tests ──

func TestFlowRegistry(t *testing.T) {
	t.Parallel()
	d, err := NewEntraDelegator(EntraDelegatorConfig{
		TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret, ClientSecret: "s",
	})
	require.NoError(t, err)

	assert.True(t, d.flowRegistry.Has("obo"))
	assert.True(t, d.flowRegistry.Has("client_credentials"))

	fn, err := d.flowRegistry.Select("user")
	assert.NoError(t, err)
	assert.NotNil(t, fn)

	fn, err = d.flowRegistry.Select("app")
	assert.NoError(t, err)
	assert.NotNil(t, fn)

	fn, err = d.flowRegistry.Select("")
	assert.NoError(t, err)
	assert.NotNil(t, fn)
}

// ── Shared test helpers ──

func authContext(token, idType string) context.Context {
	ctx := middleware.WithAccessToken(context.Background(), token)
	claims := &middleware.AuthClaims{IDType: idType}
	return middleware.WithAuthClaims(ctx, claims)
}

func mustDelegator(t *testing.T, credType CredentialType, credVal string) *EntraDelegator {
	t.Helper()
	cfg := EntraDelegatorConfig{TenantID: "t", ClientID: "c", CredentialType: credType}
	if credType == CredentialTypeSecret {
		cfg.ClientSecret = credVal
	} else {
		cfg.FederatedTokenFile = credVal
	}
	d, err := NewEntraDelegator(cfg)
	require.NoError(t, err)
	return d
}

func mustDelegatorWithURL(t *testing.T, credVal, tokenURL string) *EntraDelegator {
	t.Helper()
	cfg := EntraDelegatorConfig{TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret}
	cfg.ClientSecret = credVal
	client := httpc.NewClient(nil)
	registry := NewFlowRegistry()
	registry.Register("obo", oboFlow(tokenURL, &SecretCredential{Secret: credVal}, client))
	registry.Register("client_credentials", clientCredentialFlow(tokenURL, &SecretCredential{Secret: credVal}, client))
	d, err := NewEntraDelegator(cfg, WithHTTPClient(client), WithFlowRegistry(registry))
	require.NoError(t, err)
	return d
}

func fakeTokenEndpoint(t *testing.T, status int, body any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type failingCredential struct{}

func (f *failingCredential) Apply(_ url.Values) error {
	return fmt.Errorf("credential failure")
}

// spyStore is a test Store that tracks Get/Set calls via atomic counters.
type spyStore[K comparable, V any] struct {
	gets    atomic.Int32
	sets    atomic.Int32
	lastTTL time.Duration
	data    map[K]V
}

func (s *spyStore[K, V]) Get(_ context.Context, key K) (V, bool) {
	s.gets.Add(1)
	if s.data != nil {
		if v, ok := s.data[key]; ok {
			return v, true
		}
	}
	var zero V
	return zero, false
}

func (s *spyStore[K, V]) Set(_ context.Context, key K, value V, ttl time.Duration) {
	s.sets.Add(1)
	s.lastTTL = ttl
	if s.data == nil {
		s.data = make(map[K]V)
	}
	s.data[key] = value
}

func TestNewEntraDelegatorFromConfig(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		_, err := NewEntraDelegatorFromConfig(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "APIEntraIdentityConfig is required")
	})

	t.Run("invalid credential type returns error", func(t *testing.T) {
		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type: "invalid",
			},
		}
		_, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credential:")
	})

	t.Run("invalid allowed flows returns error", func(t *testing.T) {
		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "secret",
				ClientSecret: "env://TEST_SECRET",
			},
			AllowedFlows: &config.DelegationFlowsConfig{
				Flows: []string{"invalid_flow"},
			},
		}
		_, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "allowedFlows:")
	})

	t.Run("secret credential resolves from env", func(t *testing.T) {
		t.Setenv("TEST_DELEGATOR_SECRET", "my-secret-value")

		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "secret",
				ClientSecret: "env://TEST_DELEGATOR_SECRET",
			},
		}
		delegator, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, delegator)
	})

	t.Run("secret credential resolve failure returns error", func(t *testing.T) {
		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "secret",
				ClientSecret: "env://NONEXISTENT_VAR_FOR_TEST_XYZ",
			},
		}
		_, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolving client secret:")
	})

	t.Run("wif credential wires token path", func(t *testing.T) {
		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "wif",
				WIFTokenPath: "/var/run/secrets/token",
			},
		}
		delegator, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, delegator)
		assert.Nil(t, delegator.manager) // nil TokenManager = no caching
	})

	t.Run("nil TokenManager disables caching", func(t *testing.T) {
		t.Setenv("TEST_DELEGATOR_SECRET_MGR", "s")

		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "secret",
				ClientSecret: "env://TEST_DELEGATOR_SECRET_MGR",
			},
			TokenManager: nil,
		}
		delegator, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.NoError(t, err)
		assert.Nil(t, delegator.manager)
	})

	t.Run("non-nil TokenManager enables caching with defaults", func(t *testing.T) {
		t.Setenv("TEST_DELEGATOR_SECRET_MGR2", "s")

		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "secret",
				ClientSecret: "env://TEST_DELEGATOR_SECRET_MGR2",
			},
			TokenManager: &config.TokenManagerConfig{},
		}
		delegator, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, delegator.manager)
	})

	t.Run("TokenManager custom values are respected", func(t *testing.T) {
		t.Setenv("TEST_DELEGATOR_SECRET_MGR3", "s")

		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "secret",
				ClientSecret: "env://TEST_DELEGATOR_SECRET_MGR3",
			},
			TokenManager: &config.TokenManagerConfig{
				CacheSize:       2048,
				ExpiryBuffer:    "1m",
				CleanupInterval: "10m",
				ExpiryThreshold: "15m",
			},
		}
		delegator, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, delegator.manager)
	})

	t.Run("nil AllowedFlows registers only obo", func(t *testing.T) {
		t.Setenv("TEST_DELEGATOR_SECRET_NIL_FLOWS", "s")

		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "secret",
				ClientSecret: "env://TEST_DELEGATOR_SECRET_NIL_FLOWS",
			},
			AllowedFlows: nil, // default: OBO only
		}
		delegator, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.NoError(t, err)
		assert.True(t, delegator.flowRegistry.Has("obo"))
		assert.False(t, delegator.flowRegistry.Has("client_credentials"))
	})

	t.Run("explicit flows registers only listed", func(t *testing.T) {
		t.Setenv("TEST_DELEGATOR_SECRET_EXPLICIT", "s")

		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "secret",
				ClientSecret: "env://TEST_DELEGATOR_SECRET_EXPLICIT",
			},
			AllowedFlows: &config.DelegationFlowsConfig{
				Flows: []string{"obo", "client_credentials"},
			},
		}
		delegator, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.NoError(t, err)
		assert.True(t, delegator.flowRegistry.Has("obo"))
		assert.True(t, delegator.flowRegistry.Has("client_credentials"))
	})

	t.Run("empty flows denies all", func(t *testing.T) {
		t.Setenv("TEST_DELEGATOR_SECRET_EMPTY", "s")

		cfg := &config.APIEntraIdentityConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
			Credential: config.ServerCredentialConfig{
				Type:         "secret",
				ClientSecret: "env://TEST_DELEGATOR_SECRET_EMPTY",
			},
			AllowedFlows: &config.DelegationFlowsConfig{
				Flows: []string{},
			},
		}
		delegator, err := NewEntraDelegatorFromConfig(context.Background(), cfg)
		require.NoError(t, err)
		assert.False(t, delegator.flowRegistry.Has("obo"))
		assert.False(t, delegator.flowRegistry.Has("client_credentials"))

		// DelegateToken should fail for any caller type
		_, selectErr := delegator.flowRegistry.Select("user")
		assert.Error(t, selectErr)
		assert.Contains(t, selectErr.Error(), "not permitted")
	})
}

func TestResolveTokenManagerDefaults(t *testing.T) {
	t.Parallel()

	boolPtr := func(b bool) *bool { return &b }

	t.Run("all zeros use defaults", func(t *testing.T) {
		t.Parallel()
		tm := &config.TokenManagerConfig{}
		s, err := resolveTokenManagerDefaults(tm)
		require.NoError(t, err)
		assert.Equal(t, 1024, s.CacheSize)
		assert.Equal(t, 10*time.Minute, s.ExpiryBuffer)
		assert.Equal(t, 5*time.Minute, s.CleanupInterval)
		assert.Equal(t, 30*time.Minute, s.ExpiryThreshold)
		assert.Equal(t, 500*time.Millisecond, s.SlowThreshold)
		assert.True(t, s.RetryOnError)
	})

	t.Run("custom values override defaults", func(t *testing.T) {
		t.Parallel()
		tm := &config.TokenManagerConfig{
			CacheSize:            512,
			ExpiryBuffer:         "2m",
			CleanupInterval:      "10m",
			ExpiryThreshold:      "15m",
			SlowThreshold:        "5s",
			RetryFollowerOnError: boolPtr(false),
		}
		s, err := resolveTokenManagerDefaults(tm)
		require.NoError(t, err)
		assert.Equal(t, 512, s.CacheSize)
		assert.Equal(t, 2*time.Minute, s.ExpiryBuffer)
		assert.Equal(t, 10*time.Minute, s.CleanupInterval)
		assert.Equal(t, 15*time.Minute, s.ExpiryThreshold)
		assert.Equal(t, 5*time.Second, s.SlowThreshold)
		assert.False(t, s.RetryOnError)
	})

	t.Run("invalid durations return error", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			cfg     *config.TokenManagerConfig
			wantMsg string
		}{
			{"bad expiryBuffer", &config.TokenManagerConfig{ExpiryBuffer: "not-a-duration"}, "expiryBuffer"},
			{"bad cleanupInterval", &config.TokenManagerConfig{CleanupInterval: "also-bad"}, "cleanupInterval"},
			{"bad expiryThreshold", &config.TokenManagerConfig{ExpiryThreshold: "nope"}, "expiryThreshold"},
			{"bad slowThreshold", &config.TokenManagerConfig{SlowThreshold: "bad"}, "slowThreshold"},
		}
		for _, tc := range cases {
			_, err := resolveTokenManagerDefaults(tc.cfg)
			require.Error(t, err, tc.name)
			assert.Contains(t, err.Error(), tc.wantMsg, tc.name)
		}
	})

	t.Run("partial override keeps other defaults", func(t *testing.T) {
		t.Parallel()
		tm := &config.TokenManagerConfig{
			ExpiryThreshold:      "45m",
			RetryFollowerOnError: boolPtr(true),
		}
		s, err := resolveTokenManagerDefaults(tm)
		require.NoError(t, err)
		assert.Equal(t, 1024, s.CacheSize)
		assert.Equal(t, 10*time.Minute, s.ExpiryBuffer)
		assert.Equal(t, 5*time.Minute, s.CleanupInterval)
		assert.Equal(t, 45*time.Minute, s.ExpiryThreshold)
		assert.Equal(t, 500*time.Millisecond, s.SlowThreshold)
		assert.True(t, s.RetryOnError)
	})
}
