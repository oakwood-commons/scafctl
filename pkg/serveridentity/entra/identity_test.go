package entra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/serveridentity"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider/callerscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Constructor ──

func TestNewEntraIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		err  error
	}{
		{"no tenant", Config{ClientID: "c", CredentialType: CredentialTypeWIF, FederatedTokenFile: "f"}, ErrEntraNoTenantID},
		{"no client", Config{TenantID: "t", CredentialType: CredentialTypeWIF, FederatedTokenFile: "f"}, ErrEntraNoClientID},
		{"invalid cred type", Config{TenantID: "t", ClientID: "c", CredentialType: "bad"}, ErrEntraInvalidCredentialType},
		{"wif no file", Config{TenantID: "t", ClientID: "c", CredentialType: CredentialTypeWIF}, ErrEntraWIFMissingTokenFile},
		{"secret no value", Config{TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret}, ErrEntraSecretMissing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewEntraIdentity(tc.cfg)
			assert.ErrorIs(t, err, tc.err)
		})
	}

	t.Run("valid secret registers both strategies", func(t *testing.T) {
		t.Parallel()
		e, err := NewEntraIdentity(Config{
			TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret, ClientSecret: "s",
		})
		require.NoError(t, err)
		assert.Len(t, e.strategies, 2)
		assert.Contains(t, e.TokenURL, "login.microsoftonline.com/t/oauth2/v2.0/token")
	})

	t.Run("valid wif", func(t *testing.T) {
		t.Parallel()
		e, err := NewEntraIdentity(Config{
			TenantID: "t", ClientID: "c", CredentialType: CredentialTypeWIF, FederatedTokenFile: "/tok",
		})
		require.NoError(t, err)
		assert.NotNil(t, e)
	})

	t.Run("with manager option", func(t *testing.T) {
		t.Parallel()
		mgr := manager.NewManager[string, tokenprovider.Token]()
		e, err := NewEntraIdentity(Config{
			TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret, ClientSecret: "s",
		}, WithManager(mgr))
		require.NoError(t, err)
		assert.NotNil(t, e.manager)
	})
}

// ── GetToken routing ──

func TestGetToken_RoutesToStrategy(t *testing.T) {
	t.Parallel()

	srv := fakeTokenServer(t, http.StatusOK, map[string]any{
		"access_token": "tok", "expires_in": 3600,
	})

	t.Run("server caller uses ServerToken path", func(t *testing.T) {
		t.Parallel()
		e := mustEntraWithURL(t, "s", srv.URL)

		tok, err := e.GetToken(context.Background(), tokenprovider.RequestOptions{
			Scope:  "api/.default",
			Caller: callerscope.ServerCaller,
		})
		require.NoError(t, err)
		assert.Equal(t, "tok", tok.AccessToken)
	})

	t.Run("requester caller uses DelegateToken path", func(t *testing.T) {
		t.Parallel()
		e := mustEntraWithURL(t, "s", srv.URL)
		ctx := authContext("jwt", "user")

		tok, err := e.GetToken(ctx, tokenprovider.RequestOptions{
			Scope:  "api/.default",
			Caller: callerscope.RequesterCaller,
		})
		require.NoError(t, err)
		assert.Equal(t, "tok", tok.AccessToken)
	})

	t.Run("unknown caller scope returns error", func(t *testing.T) {
		t.Parallel()
		e := mustEntraWithURL(t, "s", "http://unused")

		_, err := e.GetToken(context.Background(), tokenprovider.RequestOptions{
			Scope:  "s",
			Caller: "bogus",
		})
		assert.ErrorContains(t, err, "no strategy found")
	})
}

// ── ServerToken ──

func TestServerToken(t *testing.T) {
	t.Parallel()

	t.Run("uses client_credentials without caller JWT", func(t *testing.T) {
		t.Parallel()
		var capturedGrantType string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			capturedGrantType = r.PostForm.Get("grant_type")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "server-tok", "expires_in": 3600,
			})
		}))
		t.Cleanup(srv.Close)

		e := mustEntraWithURL(t, "s", srv.URL)
		tok, err := e.ServerToken(context.Background(), "api/.default")
		require.NoError(t, err)
		assert.Equal(t, "server-tok", tok.AccessToken)
		assert.Equal(t, "client_credentials", capturedGrantType)
	})

	t.Run("with manager caches on second call", func(t *testing.T) {
		t.Parallel()

		var hitCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hitCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "cached-server", "expires_in": 3600})
		}))
		t.Cleanup(srv.Close)

		tokenCache := serveridentity.NewTokenCache[string, tokenprovider.Token](context.Background(), 100, 0, 5*time.Minute)
		mgr := manager.NewManager(
			manager.WithStore("test", tokenCache),
		)
		e := mustEntraWithURL(t, "s", srv.URL)
		e.manager = mgr

		tok1, err := e.ServerToken(context.Background(), "api/.default")
		require.NoError(t, err)
		assert.Equal(t, "cached-server", tok1.AccessToken)

		tok2, err := e.ServerToken(context.Background(), "api/.default")
		require.NoError(t, err)
		assert.Equal(t, "cached-server", tok2.AccessToken)

		assert.Equal(t, int32(1), hitCount.Load(), "second call should be served from cache")
	})

	t.Run("with manager different scopes produce separate cache entries", func(t *testing.T) {
		t.Parallel()

		var fetchCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fetchCount.Add(1)
			_ = r.ParseForm()
			scope := r.PostForm.Get("scope")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-" + scope, "expires_in": 3600,
			})
		}))
		t.Cleanup(srv.Close)

		tokenCache := serveridentity.NewTokenCache[string, tokenprovider.Token](context.Background(), 100, 0, 5*time.Minute)
		mgr := manager.NewManager(
			manager.WithStore("test", tokenCache),
		)
		e := mustEntraWithURL(t, "s", srv.URL)
		e.manager = mgr

		tok1, err := e.ServerToken(context.Background(), "scope-a/.default")
		require.NoError(t, err)
		assert.Equal(t, "tok-scope-a/.default", tok1.AccessToken)

		tok2, err := e.ServerToken(context.Background(), "scope-b/.default")
		require.NoError(t, err)
		assert.Equal(t, "tok-scope-b/.default", tok2.AccessToken)

		assert.Equal(t, int32(2), fetchCount.Load(), "different scopes should fetch separately")
	})
}

// ── DelegateToken ──

func TestDelegateToken(t *testing.T) {
	t.Parallel()

	t.Run("no caller token returns error", func(t *testing.T) {
		t.Parallel()
		e := mustEntraWithURL(t, "s", "http://unused")
		_, err := e.CallerToken(context.Background(), "api/.default")
		assert.ErrorIs(t, err, ErrNoCallerToken)
	})

	t.Run("empty scope returns error", func(t *testing.T) {
		t.Parallel()
		e := mustEntraWithURL(t, "s", "http://unused")
		ctx := middleware.WithAccessToken(context.Background(), "jwt")
		_, err := e.CallerToken(ctx, "")
		assert.ErrorIs(t, err, ErrNoScope)
	})

	t.Run("user caller selects OBO", func(t *testing.T) {
		t.Parallel()
		var grantType string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			grantType = r.PostForm.Get("grant_type")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 60})
		}))
		t.Cleanup(srv.Close)

		e := mustEntraWithURL(t, "s", srv.URL)
		_, err := e.CallerToken(authContext("jwt", "user"), "api/.default")
		require.NoError(t, err)
		assert.Equal(t, grantTypeJWTBearer, grantType)
	})

	t.Run("app caller selects client_credentials", func(t *testing.T) {
		t.Parallel()
		var grantType string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			grantType = r.PostForm.Get("grant_type")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 60})
		}))
		t.Cleanup(srv.Close)

		e := mustEntraWithURL(t, "s", srv.URL)
		_, err := e.CallerToken(authContext("jwt", "app"), "api/.default")
		require.NoError(t, err)
		assert.Equal(t, grantTypeClientCreds, grantType)
	})

	t.Run("token endpoint error propagates", func(t *testing.T) {
		t.Parallel()
		srv := fakeTokenServer(t, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		e := mustEntraWithURL(t, "s", srv.URL)
		ctx := authContext("jwt", "user")

		_, err := e.CallerToken(ctx, "api/.default")
		assert.ErrorContains(t, err, "token endpoint returned 400")
	})
}

// ── DelegateToken with caching ──

func TestDelegateToken_CacheHit(t *testing.T) {
	t.Parallel()

	var hitCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "cached", "expires_in": 3600})
	}))
	t.Cleanup(srv.Close)

	tokenCache := serveridentity.NewTokenCache[string, tokenprovider.Token](context.Background(), 100, 0, 5*time.Minute)
	mgr := manager.NewManager(
		manager.WithStore("test", tokenCache),
	)
	e := mustEntraWithURL(t, "s", srv.URL)
	e.manager = mgr
	ctx := authContext("jwt", "user")

	_, err := e.CallerToken(ctx, "api/.default")
	require.NoError(t, err)
	_, err = e.CallerToken(ctx, "api/.default")
	require.NoError(t, err)

	assert.Equal(t, int32(1), hitCount.Load(), "second call should be cached")
}

func TestDelegateToken_DifferentScopes_SeparateKeys(t *testing.T) {
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

	tokenCache := serveridentity.NewTokenCache[string, tokenprovider.Token](context.Background(), 100, 0, 5*time.Minute)
	mgr := manager.NewManager(
		manager.WithStore("test", tokenCache),
	)
	e := mustEntraWithURL(t, "s", srv.URL)
	e.manager = mgr
	ctx := authContext("jwt", "user")

	tok1, err := e.CallerToken(ctx, "scope-a/.default")
	require.NoError(t, err)
	assert.Equal(t, "token-for-scope-a/.default", tok1.AccessToken)

	tok2, err := e.CallerToken(ctx, "scope-b/.default")
	require.NoError(t, err)
	assert.Equal(t, "token-for-scope-b/.default", tok2.AccessToken)

	assert.Equal(t, int32(2), fetchCount.Load())
}

// ── Helpers ──

func authContext(token, idType string) context.Context { //nolint:unparam // token varies across test scenarios
	ctx := middleware.WithAccessToken(context.Background(), token)
	return middleware.WithAuthClaims(ctx, &middleware.AuthClaims{IDType: idType})
}

func mustEntraWithURL(t *testing.T, secret, tokenURL string) *Entra { //nolint:unparam // secret kept as param for clarity
	t.Helper()
	e, err := NewEntraIdentity(Config{
		TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret, ClientSecret: secret,
	}, WithHTTPClient(httpc.NewClient(nil)))
	require.NoError(t, err)
	e.TokenURL = tokenURL
	return e
}

func fakeTokenServer(t *testing.T, status int, body any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}
