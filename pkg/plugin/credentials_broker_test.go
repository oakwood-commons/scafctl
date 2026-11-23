// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"testing"
	"time"

	hplugin "github.com/hashicorp/go-plugin"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderVsAuthPlugin_SecretStoreIsolation(t *testing.T) {
	t.Parallel()

	store := secrets.NewMockStore()
	store.Data["npm-token"] = []byte("npm_user_secret_123")
	store.Data["scafctl.auth.entra.refresh_token"] = []byte("entra_refresh")
	store.Data["mycli.auth.github.refresh_token"] = []byte("mycli_gh_refresh")

	authReg := auth.NewRegistry()
	require.NoError(t, authReg.Register(auth.NewMockHandler("entra")))

	ctx := auth.WithRegistry(context.Background(), authReg)
	ctx = secrets.WithStore(ctx, store)
	ctx = settings.IntoContext(ctx, &settings.Run{BinaryName: "mycli"})

	sharedOpts := AuthClientOptsFromContext(ctx)
	require.Len(t, sharedOpts, 1)

	// 1. ProviderPlugin client: read-only + internal prefixes ("scafctl.", "mycli.") denied.
	var providerCfg pluginConfig
	_, err := newClientWithConnector("scafctl-plugin-secret", func(_ string, cfg pluginConfig) (any, *hplugin.Client, error) {
		providerCfg = cfg
		return &MockProviderPlugin{}, &hplugin.Client{}, nil
	}, sharedOpts...)
	require.NoError(t, err)

	provGRPC, ok := providerCfg.grpcPlugin.(*GRPCPlugin)
	require.True(t, ok)
	require.NotNil(t, provGRPC.HostDeps)

	provHost := &HostServiceServer{Deps: *provGRPC.HostDeps}

	// User secret is readable.
	getResp, err := provHost.GetSecret(ctx, &proto.GetSecretRequest{Name: "npm-token"})
	require.NoError(t, err)
	assert.True(t, getResp.Found)
	assert.Equal(t, "npm_user_secret_123", getResp.Value)

	// Default and embedder internal prefixes are denied.
	denied1, err := provHost.GetSecret(ctx, &proto.GetSecretRequest{Name: "scafctl.auth.entra.refresh_token"})
	require.NoError(t, err)
	assert.Contains(t, denied1.Error, "reserved internal namespace")

	denied2, err := provHost.GetSecret(ctx, &proto.GetSecretRequest{Name: "mycli.auth.github.refresh_token"})
	require.NoError(t, err)
	assert.Contains(t, denied2.Error, "reserved internal namespace")

	// ListSecrets hides internal prefixes.
	listResp, err := provHost.ListSecrets(ctx, &proto.ListSecretsRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"npm-token"}, listResp.Names)

	// Write and Delete RPCs are blocked for provider plugins.
	setResp, err := provHost.SetSecret(ctx, &proto.SetSecretRequest{Name: "npm-token", Value: "tampered"})
	require.NoError(t, err)
	assert.Contains(t, setResp.Error, "read-only")

	delResp, err := provHost.DeleteSecret(ctx, &proto.DeleteSecretRequest{Name: "npm-token"})
	require.NoError(t, err)
	assert.Contains(t, delResp.Error, "read-only")

	// 2. AuthHandlerPlugin client using the EXACT SAME sharedOpts + WithSecretScope("entra")
	// retains read/write access to scafctl.auth.entra.*!
	authOpts := append([]ClientOption{}, sharedOpts...)
	authOpts = append(authOpts, WithSecretScope("entra"))
	var authCfg pluginConfig
	_, err = newAuthHandlerClientWithConnector("scafctl-auth-entra", func(_ string, cfg pluginConfig) (any, *hplugin.Client, error) {
		authCfg = cfg
		return &MockAuthHandlerPlugin{}, &hplugin.Client{}, nil
	}, authOpts...)
	require.NoError(t, err)

	authGRPC, ok := authCfg.grpcPlugin.(*AuthHandlerGRPCPlugin)
	require.True(t, ok)
	authHost := &HostServiceServer{Deps: *authGRPC.HostDeps}

	entraGet, err := authHost.GetSecret(ctx, &proto.GetSecretRequest{Name: "scafctl.auth.entra.refresh_token"})
	require.NoError(t, err)
	assert.Empty(t, entraGet.Error)
	assert.True(t, entraGet.Found)
	assert.Equal(t, "entra_refresh", entraGet.Value)

	entraSet, err := authHost.SetSecret(ctx, &proto.SetSecretRequest{Name: "scafctl.auth.entra.access_token", Value: "new-tok"})
	require.NoError(t, err)
	assert.Empty(t, entraSet.Error)
}

func TestHostServiceServer_GetAuthToken_HostnameForwarding(t *testing.T) {
	t.Parallel()

	reg := auth.NewRegistry()
	ocp := auth.NewMockHandler("openshift")
	ocp.CapabilitiesValue = []auth.Capability{auth.CapTokenHostname}
	ocp.GetTokenResult = &auth.Token{
		AccessToken: "sha256~ocp-prod-east",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	require.NoError(t, reg.Register(ocp))

	gh := auth.NewMockHandler("github")
	gh.CapabilitiesValue = nil
	require.NoError(t, reg.Register(gh))

	deps := HostDepsFromAuthRegistry(reg)
	deps.HostConfig = &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {
					Hostname: &config.HostnameConfig{
						Aliases: map[string]string{
							"prod-east": "https://api.prod-east.example.com:6443",
						},
					},
				},
			},
		},
	}
	server := &HostServiceServer{Deps: *deps}

	// Bare gRPC context (neither config nor auth.Registry attached by caller) resolves
	// configured hostname alias "prod-east" -> "https://api.prod-east.example.com:6443".
	resp, err := server.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{
		HandlerName: "openshift",
		Hostname:    "prod-east",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "sha256~ocp-prod-east", resp.AccessToken)
	require.Len(t, ocp.GetTokenCalls, 1)
	assert.Equal(t, "https://api.prod-east.example.com:6443", ocp.GetTokenCalls[0].Hostname)

	// Handler without CapTokenHostname rejects non-empty hostname.
	unsupportedResp, err := server.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{
		HandlerName: "github",
		Hostname:    "https://ghe.example.com",
	})
	require.NoError(t, err)
	assert.Contains(t, unsupportedResp.Error, "does not support per-request hostname")
}
