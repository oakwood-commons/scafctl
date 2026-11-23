// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentials

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolver_ResolveSecret(t *testing.T) {
	store := secrets.NewMockStore()
	store.Data["npm-token"] = []byte("npm_secret_value_123")
	store.Data["scafctl.auth.github.token"] = []byte("internal_gh_token")
	store.Data["mycli.auth.github.token"] = []byte("embedder_internal_token")

	r := NewResolver()

	t.Run("resolves user secret", func(t *testing.T) {
		ctx := secrets.WithStore(context.Background(), store)
		val, err := r.ResolveSecret(ctx, "npm-token")
		require.NoError(t, err)
		assert.Equal(t, "npm_secret_value_123", val)
	})

	t.Run("denies scafctl internal secret prefix", func(t *testing.T) {
		ctx := secrets.WithStore(context.Background(), store)
		_, err := r.ResolveSecret(ctx, "scafctl.auth.github.token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved internal namespace")
	})

	t.Run("denies embedder custom binary internal secret prefix", func(t *testing.T) {
		ctx := secrets.WithStore(context.Background(), store)
		ctx = settings.IntoContext(ctx, &settings.Run{BinaryName: "mycli"})

		// Both scafctl.* and mycli.* are denied when running as mycli.
		_, err := r.ResolveSecret(ctx, "mycli.auth.github.token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved internal namespace")

		_, err = r.ResolveSecret(ctx, "scafctl.auth.github.token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved internal namespace")

		prefixes := DeniedSecretPrefixesForContext(ctx)
		assert.Contains(t, prefixes, "scafctl.")
		assert.Contains(t, prefixes, "mycli.")
	})

	t.Run("rejects invalid secret name", func(t *testing.T) {
		ctx := secrets.WithStore(context.Background(), store)
		_, err := r.ResolveSecret(ctx, "../bad")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid secret reference")
	})

	t.Run("errors when secret store missing", func(t *testing.T) {
		_, err := r.ResolveSecret(context.Background(), "npm-token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "secret store not available")
	})

	t.Run("errors when secret not found", func(t *testing.T) {
		ctx := secrets.WithStore(context.Background(), store)
		_, err := r.ResolveSecret(ctx, "missing-secret")
		require.Error(t, err)
		assert.ErrorIs(t, err, secrets.ErrNotFound)
	})
}

func TestResolver_ResolveToken(t *testing.T) {
	r := NewResolver()

	t.Run("shorthand handler token", func(t *testing.T) {
		reg := auth.NewRegistry()
		gh := auth.NewMockHandler("github")
		gh.GetTokenResult = &auth.Token{
			AccessToken: "gho_live_token_1234",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		require.NoError(t, reg.Register(gh))
		ctx := auth.WithRegistry(context.Background(), reg)

		tok, err := r.ResolveToken(ctx, spec.TokenRef{Handler: "github"})
		require.NoError(t, err)
		assert.Equal(t, "gho_live_token_1234", tok)
	})

	t.Run("scope profile minValidFor and hostname resolution", func(t *testing.T) {
		reg := auth.NewRegistry()
		ocp := auth.NewMockHandler("openshift")
		ocp.CapabilitiesValue = []auth.Capability{
			auth.CapScopesOnTokenRequest,
			auth.CapTokenHostname,
		}
		ocp.GetTokenResult = &auth.Token{
			AccessToken: "sha256~cluster_token_ok",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		require.NoError(t, reg.Register(ocp))

		cfg := &config.Config{}
		ctx := auth.WithRegistry(context.Background(), reg)
		ctx = config.WithConfig(ctx, cfg)

		tok, err := r.ResolveToken(ctx, spec.TokenRef{
			Handler:     "openshift",
			Scope:       "user:full",
			Profile:     "work",
			Hostname:    "https://api.prod-east.example.com:6443",
			MinValidFor: "5m",
		})
		require.NoError(t, err)
		assert.Equal(t, "sha256~cluster_token_ok", tok)
		assert.Equal(t, "work", ocp.LastContextProfile)
		require.Len(t, ocp.GetTokenCalls, 1)
		assert.Equal(t, "user:full", ocp.GetTokenCalls[0].Scope)
		assert.Equal(t, "https://api.prod-east.example.com:6443", ocp.GetTokenCalls[0].Hostname)
		assert.Equal(t, 5*time.Minute, ocp.GetTokenCalls[0].MinValidFor)
	})

	t.Run("built-in profile override clears active profile", func(t *testing.T) {
		reg := auth.NewRegistry()
		gh := auth.NewMockHandler("github")
		gh.GetTokenResult = &auth.Token{AccessToken: "gho_builtin_1234", TokenType: "Bearer"}
		require.NoError(t, reg.Register(gh))

		ctx := auth.WithRegistry(context.Background(), reg)
		ctx = auth.WithGlobalProfile(ctx, "work")

		tok, err := r.ResolveToken(ctx, spec.TokenRef{Handler: "github", Profile: "built-in"})
		require.NoError(t, err)
		assert.Equal(t, "gho_builtin_1234", tok)
		assert.Empty(t, gh.LastContextProfile)
	})

	t.Run("rejects scope when CapScopesOnTokenRequest unsupported", func(t *testing.T) {
		reg := auth.NewRegistry()
		gh := auth.NewMockHandler("github")
		gh.CapabilitiesValue = nil
		require.NoError(t, reg.Register(gh))
		ctx := auth.WithRegistry(context.Background(), reg)

		_, err := r.ResolveToken(ctx, spec.TokenRef{Handler: "github", Scope: "repo"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support per-request scopes")
	})

	t.Run("rejects hostname when CapTokenHostname unsupported", func(t *testing.T) {
		reg := auth.NewRegistry()
		gh := auth.NewMockHandler("github")
		gh.CapabilitiesValue = nil
		require.NoError(t, reg.Register(gh))
		ctx := auth.WithRegistry(context.Background(), reg)

		_, err := r.ResolveToken(ctx, spec.TokenRef{Handler: "github", Hostname: "ghe.internal"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support per-request hostname")
	})

	t.Run("rejects invalid handler profile minValidFor and handler errors", func(t *testing.T) {
		reg := auth.NewRegistry()
		gh := auth.NewMockHandler("github")
		gh.GetTokenErr = errors.New("not logged in")
		require.NoError(t, reg.Register(gh))
		ctx := auth.WithRegistry(context.Background(), reg)

		_, err := r.ResolveToken(ctx, spec.TokenRef{Handler: ""})
		require.Error(t, err)

		_, err = r.ResolveToken(context.Background(), spec.TokenRef{Handler: "github"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no auth registry")

		_, err = r.ResolveToken(ctx, spec.TokenRef{Handler: "unknown"})
		require.Error(t, err)

		_, err = r.ResolveToken(ctx, spec.TokenRef{Handler: "github", Profile: "bad@profile"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid profile")

		_, err = r.ResolveToken(ctx, spec.TokenRef{Handler: "github", MinValidFor: "-5s"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid minValidFor")

		_, err = r.ResolveToken(ctx, spec.TokenRef{Handler: "github"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not logged in")
	})

	t.Run("WithResolver wires into spec context", func(t *testing.T) {
		ctx := WithResolver(context.Background(), nil)
		require.NotNil(t, spec.CredentialResolverFromContext(ctx))
	})
}
