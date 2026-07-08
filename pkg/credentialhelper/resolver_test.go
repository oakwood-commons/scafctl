// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock TokenResolver for Helper.Get tests ---
// configureAuthRegistry registers the given mock handler in an
// auth.Registry, then attaches it to ctx.
func configureAuthRegistry(t *testing.T, ctx context.Context, mock *mockAuthHandler) (context.Context, *auth.Registry) {
	t.Helper()
	authRegistry := auth.NewRegistry()
	_ = authRegistry.Register(mock)
	ctx = auth.WithRegistry(ctx, authRegistry)
	return ctx, authRegistry
}

type mockResolver struct {
	cred *Credential
	err  error
}

func (m *mockResolver) Resolve(_ context.Context, _ string) (*Credential, error) {
	return m.cred, m.err
}

func TestHelperGet_DynamicResolution(t *testing.T) {
	t.Parallel()

	t.Run("resolver succeeds", func(t *testing.T) {
		t.Parallel()
		store := secrets.NewMockStore()
		resolver := &mockResolver{
			cred: &Credential{ServerURL: "ghcr.io", Username: "user1", Secret: "fresh-token"},
		}
		helper := New(store, WithTokenResolver(resolver))

		got, err := helper.Get(context.Background(), "ghcr.io")
		require.NoError(t, err)
		assert.Equal(t, "fresh-token", got.Secret)
		assert.Equal(t, "user1", got.Username)
	})

	t.Run("resolver preferred over stored", func(t *testing.T) {
		t.Parallel()
		store := secrets.NewMockStore()
		cred := Credential{ServerURL: "ghcr.io", Username: "stale", Secret: "stale-token"}
		data, _ := json.Marshal(cred)
		store.Data["credhelper:ghcr.io"] = data

		resolver := &mockResolver{
			cred: &Credential{ServerURL: "ghcr.io", Username: "fresh", Secret: "fresh-token"},
		}
		helper := New(store, WithTokenResolver(resolver))

		got, err := helper.Get(context.Background(), "ghcr.io")
		require.NoError(t, err)
		assert.Equal(t, "fresh-token", got.Secret, "dynamic token should be preferred over stored")
	})

	t.Run("resolver fails falls back to stored", func(t *testing.T) {
		t.Parallel()
		store := secrets.NewMockStore()
		cred := Credential{ServerURL: "ghcr.io", Username: "stored-user", Secret: "stored-token"}
		data, _ := json.Marshal(cred)
		store.Data["credhelper:ghcr.io"] = data

		resolver := &mockResolver{err: fmt.Errorf("handler not authenticated")}
		helper := New(store, WithTokenResolver(resolver))

		got, err := helper.Get(context.Background(), "ghcr.io")
		require.NoError(t, err)
		assert.Equal(t, "stored-token", got.Secret, "should fall back to stored credential")
	})

	t.Run("resolver returns nil falls back to stored", func(t *testing.T) {
		t.Parallel()
		store := secrets.NewMockStore()
		cred := Credential{ServerURL: "ghcr.io", Username: "stored-user", Secret: "stored-token"}
		data, _ := json.Marshal(cred)
		store.Data["credhelper:ghcr.io"] = data

		resolver := &mockResolver{cred: nil, err: nil}
		helper := New(store, WithTokenResolver(resolver))

		got, err := helper.Get(context.Background(), "ghcr.io")
		require.NoError(t, err)
		assert.Equal(t, "stored-token", got.Secret, "nil credential should fall through")
	})

	t.Run("reauth error surfaced when no fallback", func(t *testing.T) {
		t.Parallel()
		store := secrets.NewMockStore()
		resolver := &mockResolver{err: &ReauthRequiredError{
			Handler:   "github",
			ServerURL: "ghcr.io",
			Err:       fmt.Errorf("bridge auth for %q: token expired", "ghcr.io"),
		}}
		helper := New(store, WithTokenResolver(resolver))

		_, err := helper.Get(context.Background(), "ghcr.io")
		require.Error(t, err)
		var reauth *ReauthRequiredError
		require.ErrorAs(t, err, &reauth, "reauth error should be surfaced, not masked as 'credentials not found'")
		assert.Equal(t, "github", reauth.Handler)
		assert.Equal(t, "ghcr.io", reauth.ServerURL)
	})

	t.Run("reauth error masked by stored fallback", func(t *testing.T) {
		t.Parallel()
		store := secrets.NewMockStore()
		cred := Credential{ServerURL: "ghcr.io", Username: "stored-user", Secret: "stored-token"}
		data, _ := json.Marshal(cred)
		store.Data["credhelper:ghcr.io"] = data

		resolver := &mockResolver{err: &ReauthRequiredError{Handler: "github", ServerURL: "ghcr.io"}}
		helper := New(store, WithTokenResolver(resolver))

		got, err := helper.Get(context.Background(), "ghcr.io")
		require.NoError(t, err, "a usable stored credential should win over a reauth hint")
		assert.Equal(t, "stored-token", got.Secret)
	})

	t.Run("plain resolver error stays credentials not found", func(t *testing.T) {
		t.Parallel()
		store := secrets.NewMockStore()
		resolver := &mockResolver{err: fmt.Errorf("no auth handler for registry")}
		helper := New(store, WithTokenResolver(resolver))

		_, err := helper.Get(context.Background(), "unknown.io")
		require.Error(t, err)
		var reauth *ReauthRequiredError
		assert.False(t, errors.As(err, &reauth))
		assert.Contains(t, err.Error(), "credentials not found")
	})
}

// --- mock auth handler for AuthTokenResolver tests ---

type mockAuthHandler struct {
	name        string
	tokenErr    error
	statusErr   error
	claims      *auth.Claims
	capturedCtx context.Context // records the context passed to GetToken
}

func (m *mockAuthHandler) Name() string                    { return m.name }
func (m *mockAuthHandler) DisplayName() string             { return m.name }
func (m *mockAuthHandler) SupportedFlows() []auth.Flow     { return []auth.Flow{auth.FlowDeviceCode} }
func (m *mockAuthHandler) Capabilities() []auth.Capability { return nil }
func (m *mockAuthHandler) Login(_ context.Context, _ auth.LoginOptions) (*auth.Result, error) {
	return nil, nil
}
func (m *mockAuthHandler) Logout(_ context.Context) error { return nil }
func (m *mockAuthHandler) Status(ctx context.Context) (*auth.Status, error) {
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	return &auth.Status{Authenticated: true, Claims: m.claims}, nil
}

func (m *mockAuthHandler) GetToken(ctx context.Context, _ auth.TokenOptions) (*auth.Token, error) {
	m.capturedCtx = ctx
	if m.tokenErr != nil {
		return nil, m.tokenErr
	}
	return &auth.Token{AccessToken: "fresh-access-token", TokenType: "bearer"}, nil
}

func (m *mockAuthHandler) InjectAuth(_ context.Context, _ *http.Request, _ auth.TokenOptions) error {
	return nil
}

func TestAuthTokenResolver_Resolve(t *testing.T) {
	t.Parallel()

	t.Run("ghcr.io infers github handler", func(t *testing.T) {
		t.Parallel()
		handler := &mockAuthHandler{
			name:   "github",
			claims: &auth.Claims{Username: "octocat"},
		}
		registry := auth.NewRegistry()
		require.NoError(t, registry.Register(handler))

		ctx, registry := configureAuthRegistry(t, context.Background(), handler)

		resolver := NewAuthTokenResolver(registry)
		cred, err := resolver.Resolve(ctx, "ghcr.io")
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io", cred.ServerURL)
		assert.Equal(t, "octocat", cred.Username)
		assert.Equal(t, "fresh-access-token", cred.Secret)
	})

	t.Run("https URL is normalized to bare host", func(t *testing.T) {
		t.Parallel()
		handler := &mockAuthHandler{
			name:   "github",
			claims: &auth.Claims{Username: "octocat"},
		}
		ctx, registry := configureAuthRegistry(t, context.Background(), handler)

		resolver := NewAuthTokenResolver(registry)
		cred, err := resolver.Resolve(ctx, "https://ghcr.io")
		require.NoError(t, err)
		assert.Equal(t, "https://ghcr.io", cred.ServerURL, "ServerURL should preserve the original input")
		assert.Equal(t, "octocat", cred.Username)
		assert.Equal(t, "fresh-access-token", cred.Secret)
	})

	t.Run("URL with path is normalized", func(t *testing.T) {
		t.Parallel()
		handler := &mockAuthHandler{
			name:   "github",
			claims: &auth.Claims{Username: "octocat"},
		}
		ctx, registry := configureAuthRegistry(t, context.Background(), handler)
		resolver := NewAuthTokenResolver(registry)
		cred, err := resolver.Resolve(ctx, "https://ghcr.io/v2/")
		require.NoError(t, err)
		assert.Equal(t, "https://ghcr.io/v2/", cred.ServerURL)
		assert.Equal(t, "octocat", cred.Username)
	})

	t.Run("azurecr.io infers entra handler", func(t *testing.T) {
		t.Parallel()
		handler := &mockAuthHandler{name: "entra"}

		ctx, registry := configureAuthRegistry(t, context.Background(), handler)
		resolver := NewAuthTokenResolver(registry)
		cred, err := resolver.Resolve(ctx, "myregistry.azurecr.io")
		require.NoError(t, err)
		assert.Equal(t, "00000000-0000-0000-0000-000000000000", cred.Username)
		assert.Equal(t, "fresh-access-token", cred.Secret)
	})

	t.Run("pkg.dev infers gcp handler", func(t *testing.T) {
		t.Parallel()
		handler := &mockAuthHandler{name: "gcp"}
		registry := auth.NewRegistry()
		require.NoError(t, registry.Register(handler))

		ctx, registry := configureAuthRegistry(t, context.Background(), handler)

		resolver := NewAuthTokenResolver(registry)
		cred, err := resolver.Resolve(ctx, "us-docker.pkg.dev")
		require.NoError(t, err)
		assert.Equal(t, "oauth2accesstoken", cred.Username)
		assert.Equal(t, "fresh-access-token", cred.Secret)
	})

	t.Run("unknown registry returns error", func(t *testing.T) {
		t.Parallel()
		registry := auth.NewRegistry()
		resolver := NewAuthTokenResolver(registry)

		_, err := resolver.Resolve(context.Background(), "unknown.registry.io")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no auth handler")
	})

	t.Run("handler not registered returns error", func(t *testing.T) {
		t.Parallel()
		registry := auth.NewRegistry()
		resolver := NewAuthTokenResolver(registry)

		_, err := resolver.Resolve(context.Background(), "ghcr.io")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("handler token error returns error", func(t *testing.T) {
		t.Parallel()
		handler := &mockAuthHandler{
			name:     "github",
			tokenErr: fmt.Errorf("refresh token expired"),
			claims:   &auth.Claims{Username: "octocat"},
		}
		ctx, registry := configureAuthRegistry(t, context.Background(), handler)

		resolver := NewAuthTokenResolver(registry)
		_, err := resolver.Resolve(ctx, "ghcr.io")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get token for")

		var reauth *ReauthRequiredError
		require.ErrorAs(t, err, &reauth, "a failed token bridge should yield a ReauthRequiredError")
		assert.Equal(t, "github", reauth.Handler)
		assert.Equal(t, "ghcr.io", reauth.ServerURL)
	})

	t.Run("custom handler via config", func(t *testing.T) {
		t.Parallel()
		handler := &mockAuthHandler{name: "custom-idp"}
		ctx, registry := configureAuthRegistry(t, context.Background(), handler)

		customHandlers := []config.CustomOAuth2Config{
			{Name: "custom-idp", Registry: "registry.example.com"},
		}
		resolver := NewAuthTokenResolver(registry, WithCustomHandlers(customHandlers))

		cred, err := resolver.Resolve(ctx, "registry.example.com")
		require.NoError(t, err)
		assert.Equal(t, "oauth2accesstoken", cred.Username)
		assert.Equal(t, "fresh-access-token", cred.Secret)
	})

	t.Run("profile is resolved from context", func(t *testing.T) {
		t.Parallel()
		handler := &mockAuthHandler{name: "github", claims: &auth.Claims{Username: "work-user"}}
		ctx, registry := configureAuthRegistry(t, context.Background(), handler)
		resolver := NewAuthTokenResolver(registry)
		// Set a global profile in context
		ctx = auth.WithGlobalProfile(ctx, "work")
		cred, err := resolver.Resolve(ctx, "ghcr.io")
		require.NoError(t, err)
		assert.Equal(t, "work-user", cred.Username)

		// Verify the profile was propagated to the handler's GetToken call
		require.NotNil(t, handler.capturedCtx, "GetToken should have been called")
		assert.Equal(t, "work", auth.ProfileFromContext(handler.capturedCtx))
	})
}

func TestAuthTokenResolver_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ TokenResolver = (*AuthTokenResolver)(nil)
}

func TestNormalizeRegistryHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare host", "ghcr.io", "ghcr.io"},
		{"https scheme", "https://ghcr.io", "ghcr.io"},
		{"https with path", "https://ghcr.io/v2/", "ghcr.io"},
		{"https with port", "https://ghcr.io:443", "ghcr.io:443"},
		{"http scheme", "http://localhost:5000", "localhost:5000"},
		{"bare host with path", "ghcr.io/v2/library", "ghcr.io"},
		{"empty string", "", ""},
		{"whitespace only", "  ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, normalizeRegistryHost(tt.input))
		})
	}
}
