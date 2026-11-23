// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	hplugin "github.com/hashicorp/go-plugin"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
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

type streamingBrokerMockPlugin struct {
	MockProviderPlugin
	streamFn func(ctx context.Context, name string, input map[string]any, cb func(StreamChunk)) error
}

func (s *streamingBrokerMockPlugin) GetProviderDescriptor(_ context.Context, providerName string) (*provider.Descriptor, error) {
	return &provider.Descriptor{
		Name:         providerName,
		Version:      semver.MustParse("1.0.0"),
		Capabilities: []provider.Capability{provider.CapabilityFrom, provider.CapabilityAction},
	}, nil
}

func (s *streamingBrokerMockPlugin) ExecuteProviderStream(ctx context.Context, name string, input map[string]any, cb func(StreamChunk)) error {
	if s.streamFn != nil {
		return s.streamFn(ctx, name, input, cb)
	}
	return ErrStreamingNotSupported
}

func TestHostServiceBroker_TracksCredentialsWrapsStreamsAndIsolatesPooledRuns(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "false")

	store := secrets.NewMockStore()
	store.Data["run-a-secret"] = []byte("broker_secret_alpha_1111")
	store.Data["run-b-secret"] = []byte("broker_secret_bravo_2222")

	authReg := auth.NewRegistry()
	gh := auth.NewMockHandler("github")
	gh.GetTokenResult = &auth.Token{
		AccessToken: "gho_broker_minted_token_3333",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	require.NoError(t, authReg.Register(gh))

	baseCtx := auth.WithRegistry(context.Background(), authReg)
	baseCtx = secrets.WithStore(baseCtx, store)
	opts := AuthClientOptsFromContext(baseCtx)

	var hostServer *HostServiceServer
	mockImpl := &streamingBrokerMockPlugin{}
	client, err := newClientWithConnector("scafctl-plugin-broker-test", func(_ string, cfg pluginConfig) (any, *hplugin.Client, error) {
		gp := cfg.grpcPlugin.(*GRPCPlugin)
		hostServer = &HostServiceServer{Deps: *gp.HostDeps}
		return mockImpl, &hplugin.Client{}, nil
	}, opts...)
	require.NoError(t, err)
	require.NotNil(t, hostServer)

	wrapper, err := NewProviderWrapper(client, "broker-prov")
	require.NoError(t, err)

	// 1. Streaming execution starting with an EMPTY tracker: plugin fetches both a
	// secret and an auth token via HostService over an independent background RPC
	// context, then echoes them across split stream chunks, warnings, and metadata.
	mockImpl.streamFn = func(_ context.Context, _ string, _ map[string]any, cb func(StreamChunk)) error {
		secResp, sErr := hostServer.GetSecret(context.Background(), &proto.GetSecretRequest{Name: "run-a-secret"})
		if sErr != nil {
			return sErr
		}
		tokResp, tErr := hostServer.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{HandlerName: "github"})
		if tErr != nil {
			return tErr
		}
		cb(StreamChunk{Stdout: []byte("out=" + secResp.Value[:10])})
		cb(StreamChunk{Stdout: []byte(secResp.Value[10:] + " tok=" + tokResp.AccessToken + "\n")})
		cb(StreamChunk{Stderr: []byte("err=" + secResp.Value + "\n")})
		cb(StreamChunk{Result: &provider.Output{
			Data:     map[string]any{"echo": "val=" + secResp.Value},
			Warnings: []string{"warn=" + tokResp.AccessToken},
			Metadata: map[string]any{"meta": secResp.Value},
			Streamed: true,
		}})
		return nil
	}

	var outBuf, errBuf bytes.Buffer
	runTracker := redact.NewTracker()
	execCtx := redact.WithTracker(context.Background(), runTracker)
	execCtx = provider.WithExecutionMode(execCtx, provider.CapabilityAction)
	execCtx = provider.WithIOStreams(execCtx, &provider.IOStreams{Out: &outBuf, ErrOut: &errBuf})

	execRes, err := provider.NewExecutor().Execute(execCtx, wrapper, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "out=[REDACTED] tok=[REDACTED]\n", outBuf.String())
	assert.Equal(t, "err=[REDACTED]\n", errBuf.String())
	assert.Equal(t, []string{"warn=[REDACTED]"}, execRes.Output.Warnings)
	assert.Equal(t, "[REDACTED]", execRes.Output.Metadata["meta"])
	assert.Equal(t, "val=[REDACTED]", runTracker.RedactString(execRes.Output.Data.(map[string]any)["echo"].(string)))

	// 2. Concurrent runs sharing the same pooled *Client keep their trackers strictly isolated.
	mockImpl.streamFn = nil
	mockImpl.execFunc = func(_ context.Context, _ string, input map[string]any) (*provider.Output, error) {
		secName := input["secretName"].(string)
		secResp, sErr := hostServer.GetSecret(context.Background(), &proto.GetSecretRequest{Name: secName})
		if sErr != nil {
			return nil, sErr
		}
		time.Sleep(15 * time.Millisecond)
		if input["fail"] == true {
			return nil, fmt.Errorf("plugin failed with %s", secResp.Value)
		}
		return &provider.Output{Data: map[string]any{"ok": true}}, nil
	}

	trackerA := redact.NewTracker()
	trackerB := redact.NewTracker()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ctxA := redact.WithTracker(context.Background(), trackerA)
		_, execErr := wrapper.Execute(ctxA, map[string]any{"secretName": "run-a-secret", "fail": true})
		require.Error(t, execErr)
		assert.NotContains(t, execErr.Error(), "broker_secret_alpha_1111")
		assert.Contains(t, execErr.Error(), "[REDACTED]")
	}()
	go func() {
		defer wg.Done()
		ctxB := redact.WithTracker(context.Background(), trackerB)
		_, execErr := wrapper.Execute(ctxB, map[string]any{"secretName": "run-b-secret", "fail": false})
		require.NoError(t, execErr)
	}()
	wg.Wait()

	assert.True(t, trackerA.ContainsTracked("broker_secret_alpha_1111"))
	assert.False(t, trackerA.ContainsTracked("broker_secret_bravo_2222"), "Run A tracker must not inherit Run B broker secrets")
	assert.True(t, trackerB.ContainsTracked("broker_secret_bravo_2222"))
	assert.False(t, trackerB.ContainsTracked("broker_secret_alpha_1111"), "Run B tracker must not inherit Run A broker secrets")
}

type failingSecretStore struct{}

func (failingSecretStore) Get(context.Context, string) ([]byte, error) {
	return nil, fmt.Errorf("SecretStore.Get must not be called in placeholder mode")
}
func (failingSecretStore) Set(context.Context, string, []byte) error { return nil }
func (failingSecretStore) Delete(context.Context, string) error      { return nil }
func (failingSecretStore) List(context.Context) ([]string, error)    { return nil, nil }
func (failingSecretStore) Exists(context.Context, string) (bool, error) {
	return false, nil
}
func (failingSecretStore) Rotate(context.Context) error { return nil }
func (failingSecretStore) KeyringBackend() string       { return "mock" }

type whatIfBrokerMockPlugin struct {
	streamingBrokerMockPlugin
	whatIfFn func(ctx context.Context, name string, input map[string]any) (string, error)
}

func (w *whatIfBrokerMockPlugin) DescribeWhatIf(ctx context.Context, name string, input map[string]any) (string, error) {
	if w.whatIfFn != nil {
		return w.whatIfFn(ctx, name, input)
	}
	return "", nil
}

func TestHostServiceBroker_PlaceholderModeInDryRunAndWhatIf(t *testing.T) {
	t.Parallel()

	var hostServer *HostServiceServer
	mockImpl := &whatIfBrokerMockPlugin{}

	deps := HostServiceDeps{
		SecretStore:          failingSecretStore{},
		AllowedSecretPrefix:  "app.",
		DeniedSecretPrefixes: []string{"scafctl."},
		AllowedAuthHandlers:  []string{"github"},
		AuthTokenWithHostnameFunc: func(context.Context, string, string, string, int64, bool) (*proto.GetAuthTokenResponse, error) {
			return nil, fmt.Errorf("AuthTokenWithHostnameFunc must not be called in placeholder mode")
		},
	}

	client, err := newClientWithConnector("scafctl-plugin-placeholder-test", func(_ string, cfg pluginConfig) (any, *hplugin.Client, error) {
		gp := cfg.grpcPlugin.(*GRPCPlugin)
		hostServer = &HostServiceServer{Deps: *gp.HostDeps}
		return mockImpl, &hplugin.Client{}, nil
	}, func(o *clientOptions) {
		cp := deps
		o.hostDeps = &cp
	})
	require.NoError(t, err)
	require.NotNil(t, hostServer)

	wrapper, err := NewProviderWrapper(client, "preview-prov")
	require.NoError(t, err)

	mockImpl.execFunc = func(_ context.Context, _ string, _ map[string]any) (*provider.Output, error) {
		// Disallowed secret prefix and disallowed auth handler still fail in placeholder mode!
		deniedSec, err := hostServer.GetSecret(context.Background(), &proto.GetSecretRequest{Name: "other.secret"})
		require.NoError(t, err)
		assert.Contains(t, deniedSec.Error, "outside allowed scope")

		internalSec, err := hostServer.GetSecret(context.Background(), &proto.GetSecretRequest{Name: "scafctl.internal"})
		require.NoError(t, err)
		assert.Contains(t, internalSec.Error, "outside allowed scope")

		deniedTok, err := hostServer.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{HandlerName: "gcp"})
		require.NoError(t, err)
		assert.Contains(t, deniedTok.Error, "not in the allowed set")

		// Allowed secret and handler return symbolic placeholders without calling SecretStore or AuthTokenFunc!
		okSec, err := hostServer.GetSecret(context.Background(), &proto.GetSecretRequest{Name: "app.api-key"})
		require.NoError(t, err)
		assert.Empty(t, okSec.Error)
		assert.True(t, okSec.Found)

		okTok, err := hostServer.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{HandlerName: "github"})
		require.NoError(t, err)
		assert.Empty(t, okTok.Error)

		return &provider.Output{Data: map[string]any{
			"secret": okSec.Value,
			"token":  okTok.AccessToken,
		}}, nil
	}

	mockImpl.whatIfFn = func(_ context.Context, _ string, _ map[string]any) (string, error) {
		okSec, err := hostServer.GetSecret(context.Background(), &proto.GetSecretRequest{Name: "app.deploy-key"})
		if err != nil {
			return "", err
		}
		okTok, err := hostServer.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{HandlerName: "github"})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Would deploy using %s and %s", okSec.Value, okTok.AccessToken), nil
	}

	previewCtx := spec.WithCredentialResolver(context.Background(), spec.PlaceholderCredentialResolver())
	out, err := wrapper.Execute(previewCtx, map[string]any{})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.Equal(t, "<redacted:scrt:app.api-key>", data["secret"])
	assert.Equal(t, "<redacted:tkn:github>", data["token"])

	whatIfMsg := wrapper.Descriptor().DescribeWhatIf(previewCtx, map[string]any{})
	assert.Equal(t, "Would deploy using <redacted:scrt:app.deploy-key> and <redacted:tkn:github>", whatIfMsg)
}
