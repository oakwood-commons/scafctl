// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestInjectAuthHandlerSettings(t *testing.T) {
	tests := []struct {
		name        string
		handlerName string
		appCfg      *config.Config
		wantKey     string
		wantField   string
		wantValue   string
	}{
		{
			name:        "github config forwarded",
			handlerName: "github",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{
						ClientID:      "test-client-id",
						Hostname:      "github.example.com",
						DefaultScopes: []string{"read:org"},
					},
				},
			},
			wantKey:   "github",
			wantField: "clientId",
			wantValue: "test-client-id",
		},
		{
			name:        "entra config forwarded",
			handlerName: "entra",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						ClientID: "entra-client-id",
						TenantID: "tenant-123",
					},
				},
			},
			wantKey:   "entra",
			wantField: "clientId",
			wantValue: "entra-client-id",
		},
		{
			name:        "gcp config forwarded",
			handlerName: "gcp",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GCP: &config.GCPAuthConfig{
						ClientID: "gcp-client-id",
						Project:  "my-project",
					},
				},
			},
			wantKey:   "gcp",
			wantField: "clientId",
			wantValue: "gcp-client-id",
		},
		{
			name:        "open handler namespace forwarded",
			handlerName: "openshift",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Handlers: map[string]config.HandlerConfig{
						"openshift": {
							Settings: map[string]any{
								"apitimeout":   float64(30),
								"cabundlepath": "/etc/ssl/corp.pem",
							},
						},
					},
				},
			},
			wantKey:   "openshift",
			wantField: "cabundlepath",
			wantValue: "/etc/ssl/corp.pem",
		},
		{
			name:        "host-consumed hostname block not forwarded",
			handlerName: "openshift",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Handlers: map[string]config.HandlerConfig{
						"openshift": {
							Hostname: &config.HostnameConfig{
								Aliases: map[string]string{"cluster-a": "https://api.example.com:6443"},
							},
						},
					},
				},
			},
			// wantKey empty: a handler with only a hostname block (and no
			// opaque settings) delivers nothing to the plugin.
		},
		{
			name:        "nil github config skipped",
			handlerName: "github",
			appCfg:      &config.Config{},
		},
		{
			name:        "unknown handler skipped",
			handlerName: "unknown",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{ClientID: "test"},
				},
			},
		},
		{
			name:        "nil app config skipped",
			handlerName: "github",
			appCfg:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.appCfg != nil {
				ctx = config.WithConfig(ctx, tt.appCfg)
			}

			cfg := &ProviderConfig{}
			injectAuthHandlerSettings(ctx, tt.handlerName, cfg)

			if tt.wantKey == "" {
				assert.Nil(t, cfg.Settings)
				return
			}

			require.NotNil(t, cfg.Settings)
			raw, ok := cfg.Settings[tt.wantKey]
			require.True(t, ok, "expected settings key %q", tt.wantKey)

			var m map[string]any
			require.NoError(t, json.Unmarshal(raw, &m))
			assert.Equal(t, tt.wantValue, m[tt.wantField])
		})
	}
}

func TestInjectAuthHandlerSettings_ProfileMerge(t *testing.T) {
	tests := []struct {
		name        string
		handlerName string
		profile     string
		appCfg      *config.Config
		wantField   string
		wantValue   string
	}{
		{
			name:        "entra profile overrides clientId",
			handlerName: "entra",
			profile:     "machine",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						ClientID: "top-level-client",
						TenantID: "shared-tenant",
						Profiles: map[string]*config.EntraProfileConfig{
							"machine": {ClientID: "machine-client"},
						},
					},
				},
			},
			wantField: "clientId",
			wantValue: "machine-client",
		},
		{
			name:        "entra profile inherits tenantId",
			handlerName: "entra",
			profile:     "machine",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						ClientID: "top-level-client",
						TenantID: "shared-tenant",
						Profiles: map[string]*config.EntraProfileConfig{
							"machine": {ClientID: "machine-client"},
						},
					},
				},
			},
			wantField: "tenantId",
			wantValue: "shared-tenant",
		},
		{
			name:        "entra profile includes federatedTokenFile",
			handlerName: "entra",
			profile:     "wif",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						Profiles: map[string]*config.EntraProfileConfig{
							"wif": {
								ClientID:           "wif-client",
								FederatedTokenFile: "/var/run/secrets/token",
							},
						},
					},
				},
			},
			wantField: "federatedTokenFile",
			wantValue: "/var/run/secrets/token",
		},
		{
			name:        "gcp profile includes serviceAccountKeyFile",
			handlerName: "gcp",
			profile:     "deploy",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GCP: &config.GCPAuthConfig{
						Profiles: map[string]*config.GCPProfileConfig{
							"deploy": {ServiceAccountKeyFile: "/path/to/sa.json"},
						},
					},
				},
			},
			wantField: "serviceAccountKeyFile",
			wantValue: "/path/to/sa.json",
		},
		{
			name:        "no profile sends top-level config",
			handlerName: "entra",
			profile:     "",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						ClientID: "top-level-client",
						Profiles: map[string]*config.EntraProfileConfig{
							"work": {ClientID: "work-client"},
						},
					},
				},
			},
			wantField: "clientId",
			wantValue: "top-level-client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := config.WithConfig(context.Background(), tt.appCfg)
			if tt.profile != "" {
				ctx = auth.WithProfile(ctx, tt.profile)
			}

			cfg := &ProviderConfig{}
			injectAuthHandlerSettings(ctx, tt.handlerName, cfg)

			require.NotNil(t, cfg.Settings)
			raw, ok := cfg.Settings[tt.handlerName]
			require.True(t, ok)

			var m map[string]any
			require.NoError(t, json.Unmarshal(raw, &m))
			assert.Equal(t, tt.wantValue, m[tt.wantField])

			// Verify profiles and activeProfile are not forwarded to the plugin.
			assert.Nil(t, m["profiles"])
			assert.Empty(t, m["activeProfile"])
		})
	}
}

func TestPropagateStartupLatency(t *testing.T) {
	tests := []struct {
		name           string
		registerNames  []string
		propagateNames []string
		wantLatency    map[string]time.Duration
	}{
		{
			name:           "sets latency on all registered wrappers",
			registerNames:  []string{"handler-a", "handler-b"},
			propagateNames: []string{"handler-a", "handler-b"},
			wantLatency:    map[string]time.Duration{"handler-a": 42 * time.Millisecond, "handler-b": 42 * time.Millisecond},
		},
		{
			name:           "only propagates to specified names",
			registerNames:  []string{"handler-a", "handler-b"},
			propagateNames: []string{"handler-a"},
			wantLatency:    map[string]time.Duration{"handler-a": 42 * time.Millisecond, "handler-b": 0},
		},
		{
			name:           "unknown names are skipped",
			registerNames:  []string{"handler-a"},
			propagateNames: []string{"handler-a", "nonexistent"},
			wantLatency:    map[string]time.Duration{"handler-a": 42 * time.Millisecond},
		},
		{
			name:           "empty names slice is no-op",
			registerNames:  []string{"handler-a"},
			propagateNames: []string{},
			wantLatency:    map[string]time.Duration{"handler-a": 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := auth.NewRegistry()
			client := &AuthHandlerClient{startupDuration: 42 * time.Millisecond}

			for _, name := range tt.registerNames {
				w := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: name})
				require.NoError(t, reg.Register(w))
			}

			propagateStartupLatency(ctx, reg, client, tt.propagateNames)

			for name, want := range tt.wantLatency {
				h, err := reg.Get(name)
				require.NoError(t, err)
				w, ok := h.(*AuthHandlerWrapper)
				require.True(t, ok)
				assert.Equal(t, want, w.startupLatency, "latency mismatch for %s", name)
			}
		})
	}
}

func TestConfigureAndRegisterAuthHandlers_ReturnsRegisteredNames(t *testing.T) {
	ctx := context.Background()
	reg := auth.NewRegistry()
	client := &AuthHandlerClient{startupDuration: 10 * time.Millisecond}

	handlers := []AuthHandlerInfo{
		{Name: "handler-a", DisplayName: "Handler A"},
		{Name: "handler-b", DisplayName: "Handler B"},
	}

	registered := configureAndRegisterAuthHandlers(ctx, reg, client, handlers, nil)
	assert.Equal(t, []string{"handler-a", "handler-b"}, registered)
	assert.True(t, reg.Has("handler-a"))
	assert.True(t, reg.Has("handler-b"))
}

func TestConfigureAndRegisterAuthHandlers_SkipsDuplicates(t *testing.T) {
	ctx := context.Background()
	reg := auth.NewRegistry()
	client := &AuthHandlerClient{startupDuration: 10 * time.Millisecond}

	// Pre-register handler-a.
	existing := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "handler-a"})
	require.NoError(t, reg.Register(existing))

	handlers := []AuthHandlerInfo{
		{Name: "handler-a", DisplayName: "Handler A"},
		{Name: "handler-b", DisplayName: "Handler B"},
	}

	registered := configureAndRegisterAuthHandlers(ctx, reg, client, handlers, nil)
	assert.Equal(t, []string{"handler-b"}, registered)
}

func TestAuthHandlerWrapper_ApplyOverrides_Empty(t *testing.T) {
	t.Parallel()
	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "entra"})

	err := wrapper.ApplyOverrides(context.Background(), map[string]string{})
	require.NoError(t, err)
	// No call to ConfigureAuthHandler when overrides are empty.
	assert.Nil(t, mock.lastConfig)
}

func TestAuthHandlerWrapper_ApplyOverrides_MergesOntoBase(t *testing.T) {
	t.Parallel()
	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "entra"})

	overrides := map[string]string{
		"clientId": "my-client",
		"tenantId": "my-tenant",
	}
	err := wrapper.ApplyOverrides(context.Background(), overrides)
	require.NoError(t, err)
	require.NotNil(t, mock.lastConfig)

	// The settings should contain the handler name key with the merged JSON.
	raw, ok := mock.lastConfig.Settings["entra"]
	require.True(t, ok)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &settings))
	assert.Equal(t, "my-client", settings["clientId"])
	assert.Equal(t, "my-tenant", settings["tenantId"])
}

func TestAuthHandlerWrapper_ApplyOverrides_SkipsEmptyValues(t *testing.T) {
	t.Parallel()
	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "gcp"})

	// Empty values are filtered out before marshaling — they should not
	// appear in the settings sent to the plugin.
	overrides := map[string]string{
		"projectId": "proj-123",
		"region":    "",
	}
	err := wrapper.ApplyOverrides(context.Background(), overrides)
	require.NoError(t, err)
	require.NotNil(t, mock.lastConfig)

	raw := mock.lastConfig.Settings["gcp"]
	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &settings))
	assert.Equal(t, "proj-123", settings["projectId"])
	// Empty string is filtered out — key should not be present.
	assert.NotContains(t, settings, "region")
}

func TestAuthHandlerWrapper_ApplyOverrides_PreservesHostConfig(t *testing.T) {
	t.Parallel()
	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "entra"})
	wrapper.hostCfg = hostConfig{
		Quiet:      true,
		NoColor:    true,
		BinaryName: "mycli",
		Profile:    "work",
	}

	overrides := map[string]string{"clientId": "abc"}
	err := wrapper.ApplyOverrides(context.Background(), overrides)
	require.NoError(t, err)
	require.NotNil(t, mock.lastConfig)

	// Host-level fields must be preserved from initial config.
	assert.True(t, mock.lastConfig.Quiet, "Quiet should be preserved")
	assert.True(t, mock.lastConfig.NoColor, "NoColor should be preserved")
	assert.Equal(t, "mycli", mock.lastConfig.BinaryName, "BinaryName should be preserved")
	assert.Equal(t, "work", mock.lastConfig.Profile, "Profile should be preserved")
}

func TestAuthHandlerWrapper_Status_ResolvesActiveProfile(t *testing.T) {
	t.Parallel()
	var capturedProfile string
	mock := &MockAuthHandlerPlugin{
		statusFunc: func(ctx context.Context, _ string, _ StatusRequest) (*auth.Status, error) {
			capturedProfile = auth.ProfileFromContext(ctx)
			return &auth.Status{Authenticated: true}, nil
		},
	}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "github"})

	// Set up config with activeProfile = "work".
	cfg := &config.Config{}
	cfg.Auth.GitHub = &config.GitHubAuthConfig{ActiveProfile: "work"}
	ctx := config.WithConfig(context.Background(), cfg)

	status, err := wrapper.Status(ctx)
	require.NoError(t, err)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "work", capturedProfile, "should resolve active profile from config")
}

func TestAuthHandlerWrapper_Status_ExplicitProfileTakesPrecedence(t *testing.T) {
	t.Parallel()
	var capturedProfile string
	mock := &MockAuthHandlerPlugin{
		statusFunc: func(ctx context.Context, _ string, _ StatusRequest) (*auth.Status, error) {
			capturedProfile = auth.ProfileFromContext(ctx)
			return &auth.Status{Authenticated: true}, nil
		},
	}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "github"})

	cfg := &config.Config{}
	cfg.Auth.GitHub = &config.GitHubAuthConfig{ActiveProfile: "work"}
	ctx := config.WithConfig(context.Background(), cfg)
	ctx = auth.WithProfile(ctx, "staging")

	_, err := wrapper.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, "staging", capturedProfile, "explicit profile should take precedence")
}

func TestAuthHandlerWrapper_GetToken_ResolvesActiveProfile(t *testing.T) {
	t.Parallel()
	var capturedProfile string
	mock := &MockAuthHandlerPlugin{
		tokenFunc: func(ctx context.Context, _ string, _ TokenRequest) (*TokenResponse, error) {
			capturedProfile = auth.ProfileFromContext(ctx)
			return &TokenResponse{AccessToken: "tok"}, nil
		},
	}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "github"})

	cfg := &config.Config{}
	cfg.Auth.GitHub = &config.GitHubAuthConfig{ActiveProfile: "work"}
	ctx := config.WithConfig(context.Background(), cfg)

	token, err := wrapper.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "tok", token.AccessToken)
	assert.Equal(t, "work", capturedProfile, "should resolve active profile from config")
}

func TestAuthHandlerWrapper_GetToken_ForwardsHostname(t *testing.T) {
	t.Parallel()
	var capturedReq TokenRequest
	mock := &MockAuthHandlerPlugin{
		tokenFunc: func(_ context.Context, _ string, req TokenRequest) (*TokenResponse, error) {
			capturedReq = req
			return &TokenResponse{AccessToken: "tok"}, nil
		},
	}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "openshift"})

	// #581: the per-cluster hostname on TokenOptions must reach the plugin
	// TokenRequest so the handler can select the right cluster's token.
	_, err := wrapper.GetToken(context.Background(), auth.TokenOptions{
		Hostname: "https://api.a.example.com:6443",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://api.a.example.com:6443", capturedReq.Hostname)
}

func TestAuthHandlerWrapper_GetToken_ProfileResolvedSkipsReResolution(t *testing.T) {
	t.Parallel()
	var capturedProfile string
	mock := &MockAuthHandlerPlugin{
		tokenFunc: func(ctx context.Context, _ string, _ TokenRequest) (*TokenResponse, error) {
			capturedProfile = auth.ProfileFromContext(ctx)
			return &TokenResponse{AccessToken: "tok"}, nil
		},
	}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "github"})

	// Config has activeProfile "work", but context is marked as resolved (built-in).
	cfg := &config.Config{}
	cfg.Auth.GitHub = &config.GitHubAuthConfig{ActiveProfile: "work"}
	ctx := config.WithConfig(context.Background(), cfg)
	ctx = auth.WithProfileResolved(ctx)

	_, err := wrapper.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Empty(t, capturedProfile, "should not re-resolve when already resolved")
}

func TestAuthHandlerWrapper_ApplyOverrides_SelectedDefaultProfileOverridesActiveProfile(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{plugin: mock}
	reg := auth.NewRegistry()

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ClientID:      "top-level-client-id",
				TenantID:      "top-level-tenant-id",
				DefaultFlow:   "interactive",
				ActiveProfile: "work",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID:     "work-profile-client-id",
						TenantID:     "work-profile-tenant-id",
						ClientSecret: "work-profile-secret",
					},
				},
			},
		},
	}

	baseCtx := config.WithConfig(context.Background(), appCfg)
	registered := configureAndRegisterAuthHandlers(
		baseCtx,
		reg,
		client,
		[]AuthHandlerInfo{{Name: "entra", DisplayName: "Microsoft Entra ID"}},
		&ProviderConfig{BinaryName: "scafctl"},
	)
	require.Equal(t, []string{"entra"}, registered)
	require.NotNil(t, mock.lastConfig)
	assert.Equal(t, "work", mock.lastConfig.Profile)

	var initialSettings map[string]any
	require.NoError(t, json.Unmarshal(mock.lastConfig.Settings["entra"], &initialSettings))
	assert.Equal(t, "work-profile-client-id", initialSettings["clientId"])
	assert.Equal(t, "work-profile-tenant-id", initialSettings["tenantId"])
	assert.Equal(t, "work-profile-secret", initialSettings["clientSecret"])

	h, ok := reg.GetRegistered("entra")
	require.True(t, ok)
	cfgr, ok := h.(auth.Configurer)
	require.True(t, ok)

	loginCtx := auth.WithProfileResolved(baseCtx)
	require.NoError(t, cfgr.ApplyOverrides(loginCtx, map[string]string{"defaultFlow": "interactive"}))

	assert.Empty(t, mock.lastConfig.Profile)
	var updatedSettings map[string]any
	require.NoError(t, json.Unmarshal(mock.lastConfig.Settings["entra"], &updatedSettings))
	assert.Equal(t, "top-level-client-id", updatedSettings["clientId"])
	assert.Equal(t, "top-level-tenant-id", updatedSettings["tenantId"])
	assert.Equal(t, "interactive", updatedSettings["defaultFlow"])
	assert.NotContains(t, updatedSettings, "clientSecret")
}

// recordingProfilePlugin records both stateful ConfigureAuthHandler updates and
// per-RPC profile/settings observed on every method.
type recordingProfilePlugin struct {
	mu          sync.Mutex
	configCalls []ProviderConfig
	rpcRecords  []recordedAuthRPC
}

type recordedAuthRPC struct {
	method         string
	profile        string
	hasReqSettings bool
	reqSettings    map[string]json.RawMessage
}

func (r *recordingProfilePlugin) recordRPC(ctx context.Context, method string, explicit map[string]json.RawMessage) {
	settings := explicit
	hasSettings := explicit != nil
	if !hasSettings {
		settings, hasSettings = RequestSettingsFromContext(ctx)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rpcRecords = append(r.rpcRecords, recordedAuthRPC{
		method:         method,
		profile:        auth.ProfileFromContext(ctx),
		hasReqSettings: hasSettings,
		reqSettings:    cloneSettingsMap(settings),
	})
}

func (r *recordingProfilePlugin) GetAuthHandlers(_ context.Context) ([]AuthHandlerInfo, error) {
	return []AuthHandlerInfo{{Name: "entra", DisplayName: "Entra"}}, nil
}

func (r *recordingProfilePlugin) ConfigureAuthHandler(_ context.Context, _ string, cfg ProviderConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configCalls = append(r.configCalls, cfg)
	return nil
}

func (r *recordingProfilePlugin) Login(ctx context.Context, _ string, req LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
	r.recordRPC(ctx, "Login", req.Settings)
	return &LoginResponse{
		Claims:    &auth.Claims{Email: "user@example.com"},
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (r *recordingProfilePlugin) Logout(ctx context.Context, _ string, _ LogoutRequest) error {
	r.recordRPC(ctx, "Logout", nil)
	return nil
}

func (r *recordingProfilePlugin) GetStatus(ctx context.Context, _ string, req StatusRequest) (*auth.Status, error) {
	r.recordRPC(ctx, "GetStatus", req.Settings)
	var clientID, tenantID string
	if raw, ok := req.Settings["clientId"]; ok {
		_ = json.Unmarshal(raw, &clientID)
	}
	if raw, ok := req.Settings["tenantId"]; ok {
		_ = json.Unmarshal(raw, &tenantID)
	}
	return &auth.Status{
		Authenticated: true,
		ClientID:      clientID,
		TenantID:      tenantID,
		Claims:        &auth.Claims{Email: fmt.Sprintf("%s@example.com", auth.DisplayProfileName(auth.ProfileFromContext(ctx)))},
	}, nil
}

func (r *recordingProfilePlugin) GetToken(ctx context.Context, _ string, req TokenRequest) (*TokenResponse, error) {
	r.recordRPC(ctx, "GetToken", req.Settings)
	var clientID, tenantID string
	if raw, ok := req.Settings["clientId"]; ok {
		_ = json.Unmarshal(raw, &clientID)
	}
	if raw, ok := req.Settings["tenantId"]; ok {
		_ = json.Unmarshal(raw, &tenantID)
	}
	return &TokenResponse{
		AccessToken: fmt.Sprintf("%s:%s:%s", auth.ProfileFromContext(ctx), clientID, tenantID),
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}, nil
}

func (r *recordingProfilePlugin) ListCachedTokens(ctx context.Context, handlerName string) ([]*auth.CachedTokenInfo, error) {
	r.recordRPC(ctx, "ListCachedTokens", nil)
	return []*auth.CachedTokenInfo{{Handler: handlerName}}, nil
}

func (r *recordingProfilePlugin) PurgeExpiredTokens(ctx context.Context, _ string) (int, error) {
	r.recordRPC(ctx, "PurgeExpiredTokens", nil)
	return 1, nil
}

func (r *recordingProfilePlugin) DetectAvailableFlows(ctx context.Context, _ string) ([]FlowAvailability, error) {
	r.recordRPC(ctx, "DetectAvailableFlows", nil)
	return []FlowAvailability{{Flow: auth.FlowInteractive, Available: true}}, nil
}

func (r *recordingProfilePlugin) StopAuthHandler(_ context.Context, _ string) error {
	return nil
}

func TestAuthHandlerWrapper_PerProfileReconfigureAndStatelessSettings(t *testing.T) {
	t.Parallel()

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ActiveProfile: "work",
				Authority:     "https://login.microsoftonline.com",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID:      "work-client",
						TenantID:      "work-tenant",
						DefaultScopes: []string{"api://work/.default"},
						ClientSecret:  "work-secret",
					},
					"staging": {
						ClientID:           "staging-client",
						TenantID:           "staging-tenant",
						FederatedTokenFile: "/var/run/secrets/staging-token",
					},
				},
			},
		},
	}

	rec := &recordingProfilePlugin{}
	reg := auth.NewRegistry()
	baseCtx := config.WithConfig(context.Background(), appCfg)
	configureAndRegisterAuthHandlers(baseCtx, reg, NewAuthHandlerClientFromPlugin(rec), []AuthHandlerInfo{
		{Name: "entra", DisplayName: "Microsoft Entra ID"},
	}, &ProviderConfig{BinaryName: "mycli"})

	h, err := reg.Get("entra")
	require.NoError(t, err)
	wrapper := h.(*AuthHandlerWrapper)

	// Initial startup configured activeProfile ("work").
	require.Len(t, rec.configCalls, 1)
	assert.Equal(t, "work", rec.configCalls[0].Profile)

	// 1. Switch to "staging" profile: reconfigures stateful plugin AND sends staging settings per RPC.
	stagingCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, "staging"))
	_, err = wrapper.DetectAvailableFlows(stagingCtx)
	require.NoError(t, err)
	st, err := wrapper.Status(stagingCtx)
	require.NoError(t, err)
	assert.Equal(t, "staging-client", st.ClientID)
	assert.Equal(t, "staging-tenant", st.TenantID)

	// ConfigureAuthHandler ran once for the staging switch (not duplicated on Status).
	require.Len(t, rec.configCalls, 2)
	assert.Equal(t, "staging", rec.configCalls[1].Profile)
	assert.Equal(t, "mycli", rec.configCalls[1].BinaryName)

	// 2. Explicit --profile default when activeProfile is "work":
	// resets ConfigureAuthHandler to Profile "" and top-level settings (no work leak),
	// and sends top-level settings (authority only, no clientId/clientSecret) per RPC.
	defaultCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, ""))
	_, err = wrapper.Login(defaultCtx, auth.LoginOptions{Flow: auth.FlowInteractive})
	require.NoError(t, err)
	require.NoError(t, wrapper.Logout(defaultCtx))
	stDefault, err := wrapper.Status(defaultCtx)
	require.NoError(t, err)
	assert.Empty(t, stDefault.ClientID, "default profile must not inherit work clientId")
	tokDefault, err := wrapper.GetToken(defaultCtx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "::", tokDefault.AccessToken)
	_, err = wrapper.ListCachedTokens(defaultCtx)
	require.NoError(t, err)
	_, err = wrapper.PurgeExpiredTokens(defaultCtx)
	require.NoError(t, err)

	require.Len(t, rec.configCalls, 3)
	assert.Equal(t, "", rec.configCalls[2].Profile)
	var defaultCfgMap map[string]any
	require.NoError(t, json.Unmarshal(rec.configCalls[2].Settings["entra"], &defaultCfgMap))
	assert.Equal(t, "https://login.microsoftonline.com", defaultCfgMap["authority"])
	assert.NotContains(t, defaultCfgMap, "clientId")
	assert.NotContains(t, defaultCfgMap, "clientSecret")

	// Verify every defaultCtx RPC saw Profile == "".
	for _, call := range rec.rpcRecords[2:] {
		assert.Empty(t, call.profile, "method %s should receive empty default profile", call.method)
	}
}

func TestAuthHandlerWrapper_ExplicitEmptyDefaultConfigSendsEmptyObjectSettings(t *testing.T) {
	t.Parallel()

	// Top-level Entra config has ONLY activeProfile + profiles.work (zero top-level settings).
	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ActiveProfile: "work",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID: "work-client",
						TenantID: "work-tenant",
					},
				},
			},
		},
	}

	rec := &recordingProfilePlugin{}
	reg := auth.NewRegistry()
	baseCtx := config.WithConfig(context.Background(), appCfg)
	configureAndRegisterAuthHandlers(baseCtx, reg, NewAuthHandlerClientFromPlugin(rec), []AuthHandlerInfo{
		{Name: "entra", DisplayName: "Microsoft Entra ID"},
	}, &ProviderConfig{BinaryName: "scafctl"})

	h, err := reg.Get("entra")
	require.NoError(t, err)
	wrapper := h.(*AuthHandlerWrapper)

	// Explicit --profile default: top-level marshals to "{}".
	// ConfigureAuthHandler must clear Settings (nil) and Profile (""), while
	// per-RPC Settings must be a non-nil empty map so stateless plugins do not
	// fall back to ConfigureAuthHandler's prior "work" state.
	defaultCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, ""))
	st, err := wrapper.Status(defaultCtx)
	require.NoError(t, err)
	assert.Empty(t, st.ClientID)

	require.Len(t, rec.configCalls, 2)
	assert.Empty(t, rec.configCalls[1].Profile)
	assert.Nil(t, rec.configCalls[1].Settings)

	require.Len(t, rec.rpcRecords, 1)
	assert.Empty(t, rec.rpcRecords[0].profile)
	assert.True(t, rec.rpcRecords[0].hasReqSettings)
	assert.NotNil(t, rec.rpcRecords[0].reqSettings)
	assert.Empty(t, rec.rpcRecords[0].reqSettings)
}

func TestAuthHandlerWrapper_ApplyOverrides_PreservesResolvedProfileSettings(t *testing.T) {
	t.Parallel()

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				Authority: "https://login.microsoftonline.com",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID:      "profile-client",
						TenantID:      "profile-tenant",
						DefaultScopes: []string{"api://work/.default"},
						ClientSecret:  "profile-secret",
					},
				},
			},
		},
	}

	rec := &recordingProfilePlugin{}
	wrapper := NewAuthHandlerWrapper(NewAuthHandlerClientFromPlugin(rec), AuthHandlerInfo{Name: "entra"})
	wrapper.setHostConfig(hostConfig{BinaryName: "mycli"})

	workCtx := auth.WithProfileResolved(auth.WithProfile(config.WithConfig(context.Background(), appCfg), "work"))

	// CLI passes --profile work --client-id override-client (without --tenant).
	require.NoError(t, wrapper.ApplyOverrides(workCtx, map[string]string{
		"clientId": "override-client",
	}))
	_, err := wrapper.DetectAvailableFlows(workCtx)
	require.NoError(t, err)
	st, err := wrapper.Status(workCtx)
	require.NoError(t, err)
	_, err = wrapper.Login(workCtx, auth.LoginOptions{Flow: auth.FlowServicePrincipal})
	require.NoError(t, err)

	assert.Equal(t, "override-client", st.ClientID)
	assert.Equal(t, "profile-tenant", st.TenantID)

	// ConfigureAuthHandler ran once during ApplyOverrides and was not clobbered by subsequent RPCs.
	require.Len(t, rec.configCalls, 1)
	assert.Equal(t, "work", rec.configCalls[0].Profile)

	for _, call := range rec.rpcRecords {
		assert.Equal(t, "work", call.profile)
		assert.Equal(t, `"override-client"`, string(call.reqSettings["clientId"]), "method %s must keep CLI override", call.method)
		assert.Equal(t, `"profile-tenant"`, string(call.reqSettings["tenantId"]), "method %s must keep profile tenantId", call.method)
		assert.Equal(t, `"profile-secret"`, string(call.reqSettings["clientSecret"]), "method %s must keep profile credentials", call.method)
		assert.Equal(t, `"https://login.microsoftonline.com"`, string(call.reqSettings["authority"]), "method %s must keep inherited authority", call.method)
	}
}

func TestAuthHandlerWrapper_GenericHandlerProfiles(t *testing.T) {
	t.Parallel()

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {
					ActiveProfile: "work",
					Settings: map[string]any{
						"clientId": "default-ocp-client",
						"tenantId": "default-cluster",
					},
					Profiles: map[string]map[string]any{
						"work": {
							"clientId": "work-ocp-client",
							"tenantId": "work-cluster",
						},
					},
				},
			},
		},
	}

	rec := &recordingProfilePlugin{}
	wrapper := NewAuthHandlerWrapper(NewAuthHandlerClientFromPlugin(rec), AuthHandlerInfo{Name: "openshift"})
	baseCtx := config.WithConfig(context.Background(), appCfg)

	stWork, err := wrapper.Status(baseCtx)
	require.NoError(t, err)
	assert.Equal(t, "work-ocp-client", stWork.ClientID)
	assert.Equal(t, "work-cluster", stWork.TenantID)

	defaultCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, ""))
	stDefault, err := wrapper.Status(defaultCtx)
	require.NoError(t, err)
	assert.Equal(t, "default-ocp-client", stDefault.ClientID)
	assert.Equal(t, "default-cluster", stDefault.TenantID)
}

func TestAuthHandlerWrapper_ConcurrentMultiProfileRace(t *testing.T) {
	t.Parallel()

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ClientID: "default-client",
				TenantID: "default-tenant",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID: "work-client",
						TenantID: "work-tenant",
					},
					"prod": {
						ClientID: "prod-client",
						TenantID: "prod-tenant",
					},
				},
			},
		},
	}

	rec := &recordingProfilePlugin{}
	grpcServer := &AuthHandlerGRPCServer{Impl: rec}
	wireSvc := &concurrentWireServiceClient{server: grpcServer}
	wrapper := NewAuthHandlerWrapper(NewAuthHandlerClientFromPlugin(NewAuthHandlerGRPCClient(wireSvc)), AuthHandlerInfo{Name: "entra"})
	baseCtx := config.WithConfig(context.Background(), appCfg)

	type target struct {
		profile  string
		expected string
	}
	targets := []target{
		{profile: "", expected: ":default-client:default-tenant"},
		{profile: "work", expected: "work:work-client:work-tenant"},
		{profile: "prod", expected: "prod:prod-client:prod-tenant"},
	}

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		tgt := targets[i%len(targets)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := auth.WithProfileResolved(auth.WithProfile(baseCtx, tgt.profile))
			tok, err := wrapper.GetToken(ctx, auth.TokenOptions{Scope: "api://app/.default"})
			require.NoError(t, err)
			assert.Equal(t, tgt.expected, tok.AccessToken)

			_, err = wrapper.Status(ctx)
			require.NoError(t, err)
			_, err = wrapper.DetectAvailableFlows(ctx)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
}

func simulateGRPCWireHop(ctx context.Context, strip bool) context.Context {
	if !strip {
		return ctx
	}
	wireCtx := context.Background()
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		wireCtx = metadata.NewIncomingContext(wireCtx, md.Copy())
	}
	return wireCtx
}

type concurrentWireServiceClient struct {
	proto.AuthHandlerServiceClient
	server       *AuthHandlerGRPCServer
	stripWireCtx bool
	onActivate   func(ctx context.Context, settings json.RawMessage) error
}

func (c *concurrentWireServiceClient) ConfigureAuthHandler(ctx context.Context, req *proto.ConfigureAuthHandlerRequest, _ ...grpc.CallOption) (*proto.ConfigureAuthHandlerResponse, error) {
	return c.server.ConfigureAuthHandler(simulateGRPCWireHop(ctx, c.stripWireCtx), req)
}

func (c *concurrentWireServiceClient) GetStatus(ctx context.Context, req *proto.GetStatusRequest, _ ...grpc.CallOption) (*proto.GetStatusResponse, error) {
	return c.server.GetStatus(simulateGRPCWireHop(ctx, c.stripWireCtx), req)
}

func (c *concurrentWireServiceClient) GetToken(ctx context.Context, req *proto.GetTokenRequest, _ ...grpc.CallOption) (*proto.GetTokenResponse, error) {
	return c.server.GetToken(simulateGRPCWireHop(ctx, c.stripWireCtx), req)
}

func (c *concurrentWireServiceClient) DetectAvailableFlows(ctx context.Context, req *proto.DetectAvailableFlowsRequest, _ ...grpc.CallOption) (*proto.DetectAvailableFlowsResponse, error) {
	return c.server.DetectAvailableFlows(simulateGRPCWireHop(ctx, c.stripWireCtx), req)
}

func (c *concurrentWireServiceClient) Logout(ctx context.Context, req *proto.LogoutRequest, _ ...grpc.CallOption) (*proto.LogoutResponse, error) {
	return c.server.Logout(simulateGRPCWireHop(ctx, c.stripWireCtx), req)
}

func (c *concurrentWireServiceClient) ListCachedTokens(ctx context.Context, req *proto.ListCachedTokensRequest, _ ...grpc.CallOption) (*proto.ListCachedTokensResponse, error) {
	return c.server.ListCachedTokens(simulateGRPCWireHop(ctx, c.stripWireCtx), req)
}

func (c *concurrentWireServiceClient) PurgeExpiredTokens(ctx context.Context, req *proto.PurgeExpiredTokensRequest, _ ...grpc.CallOption) (*proto.PurgeExpiredTokensResponse, error) {
	return c.server.PurgeExpiredTokens(simulateGRPCWireHop(ctx, c.stripWireCtx), req)
}

func (c *concurrentWireServiceClient) ActivateServerMode(ctx context.Context, req *proto.ActivateServerModeRequest, _ ...grpc.CallOption) (*proto.ActivateServerModeResponse, error) {
	hopCtx := simulateGRPCWireHop(ctx, c.stripWireCtx)
	if c.onActivate != nil {
		if err := c.onActivate(hopCtx, req.Settings); err != nil {
			//nolint:nilerr // Error is communicated via response, not gRPC error
			return &proto.ActivateServerModeResponse{Error: err.Error()}, nil
		}
	}
	return &proto.ActivateServerModeResponse{}, nil
}

func TestAuthHandlerWrapper_ServerModeProtectsCredentialsAndConfigLessCtxPreservesStartup(t *testing.T) {
	t.Parallel()

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ActiveProfile: "work",
				ClientID:      "cli-default-client",
				TenantID:      "cli-default-tenant",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID: "cli-work-client",
						TenantID: "cli-work-tenant",
					},
				},
			},
		},
	}

	t.Run("config-less context preserves startup AppConfig and activeProfile", func(t *testing.T) {
		t.Parallel()
		rec := &recordingProfilePlugin{}
		reg := auth.NewRegistry()
		startupCtx := config.WithConfig(context.Background(), appCfg)
		configureAndRegisterAuthHandlers(startupCtx, reg, NewAuthHandlerClientFromPlugin(rec), []AuthHandlerInfo{
			{Name: "entra", DisplayName: "Microsoft Entra ID"},
		}, &ProviderConfig{BinaryName: "mycli"})

		h, err := reg.Get("entra")
		require.NoError(t, err)
		wrapper := h.(*AuthHandlerWrapper)

		// Bare context.Background() (e.g. HostService or embedder) must NOT wipe startup config.
		st, err := wrapper.Status(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "cli-work-client", st.ClientID)
		assert.Equal(t, "cli-work-tenant", st.TenantID)
		assert.Len(t, rec.configCalls, 1, "no redundant ConfigureAuthHandler on matching startup profile")
	})

	t.Run("ActivateServerMode prevents CLI settings and ConfigureAuthHandler from clobbering server mode", func(t *testing.T) {
		t.Parallel()
		var gotTokenReq *proto.GetTokenRequest
		mockSvc := &mockAuthHandlerServiceClient{
			activateServerModeResp:   &proto.ActivateServerModeResponse{},
			configureAuthHandlerResp: &proto.ConfigureAuthHandlerResponse{ProtocolVersion: PluginProtocolVersion},
			getTokenResp:             &proto.GetTokenResponse{AccessToken: "srv-tok", TokenType: "Bearer"},
		}
		grpcClient := &AuthHandlerGRPCClient{client: mockSvc, hostServiceID: 77}
		wrapper := NewAuthHandlerWrapper(NewAuthHandlerClientFromPlugin(grpcClient), AuthHandlerInfo{Name: "entra"})

		// Nil or empty/null settings is a CLI-mode no-op and must not mark wrapper as serverMode.
		require.NoError(t, wrapper.ActivateServerMode(context.Background(), nil))
		require.NoError(t, wrapper.ActivateServerMode(context.Background(), json.RawMessage{}))
		require.NoError(t, wrapper.ActivateServerMode(context.Background(), json.RawMessage("null")))
		reqCtx := config.WithConfig(context.Background(), appCfg)
		_, err := wrapper.GetToken(reqCtx, auth.TokenOptions{Scope: "https://graph.microsoft.com/.default"})
		require.NoError(t, err)
		require.NotNil(t, mockSvc.lastConfigureReq, "nil ActivateServerMode must leave CLI reconfiguration enabled")
		require.NotNil(t, mockSvc.capturedGetTokenReq)
		assert.NotEmpty(t, mockSvc.capturedGetTokenReq.Settings, "nil ActivateServerMode must preserve per-RPC settings")
		require.NoError(t, wrapper.ApplyOverrides(reqCtx, map[string]string{"clientId": "cli-override"}))

		// Non-nil settings enables serverMode; subsequent nil call leaves serverMode unchanged.
		mockSvc.lastConfigureReq = nil
		mockSvc.capturedGetTokenReq = nil
		require.NoError(t, wrapper.ActivateServerMode(context.Background(), json.RawMessage(`{"clientId":"ConfidentialSrv"}`)))
		require.NoError(t, wrapper.ActivateServerMode(context.Background(), nil))

		tok, err := wrapper.GetToken(reqCtx, auth.TokenOptions{Scope: "https://graph.microsoft.com/.default"})
		require.NoError(t, err)
		assert.Equal(t, "srv-tok", tok.AccessToken)
		assert.Nil(t, mockSvc.lastConfigureReq, "server-mode wrapper must never call ConfigureAuthHandler")
		gotTokenReq = mockSvc.capturedGetTokenReq
		require.NotNil(t, gotTokenReq)
		assert.Empty(t, gotTokenReq.Settings, "server-mode wrapper must not attach CLI per-RPC settings")
	})

	t.Run("Profile-scoped ApplyOverrides isolation, error rollback, and HostServiceID preservation", func(t *testing.T) {
		t.Parallel()
		var failConfigure bool
		mockSvc := &mockAuthHandlerServiceClient{
			configureAuthHandlerResp: &proto.ConfigureAuthHandlerResponse{ProtocolVersion: PluginProtocolVersion},
			getStatusResp:            &proto.GetStatusResponse{Authenticated: true},
		}
		grpcClient := &AuthHandlerGRPCClient{client: mockSvc, hostServiceID: 99}
		wrapper := NewAuthHandlerWrapper(NewAuthHandlerClientFromPlugin(grpcClient), AuthHandlerInfo{Name: "openshift"})

		genericCfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				Handlers: map[string]config.HandlerConfig{
					"openshift": {
						Settings: map[string]any{"clientid": "default-ocp"},
						Profiles: map[string]map[string]any{
							"work": {"clientid": "work-ocp"},
							"prod": {"clientid": "prod-ocp"},
						},
					},
				},
			},
		}
		baseCtx := config.WithConfig(context.Background(), genericCfg)
		workCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, "work"))
		prodCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, "prod"))

		require.NoError(t, wrapper.ApplyOverrides(workCtx, map[string]string{"clientId": "work-cli-override"}))
		require.NotNil(t, mockSvc.lastConfigureReq)
		assert.Equal(t, uint32(99), mockSvc.lastConfigureReq.HostServiceId, "HostServiceID must be preserved on reconfigure")
		var workSettings map[string]any
		require.NoError(t, json.Unmarshal(mockSvc.lastConfigureReq.Settings["openshift"], &workSettings))
		assert.Equal(t, "work-cli-override", workSettings["clientId"])
		assert.NotContains(t, workSettings, "clientid", "case-colliding lowercased key must be removed")

		// Switching to prod must NOT inherit work's CLI override.
		_, err := wrapper.Status(prodCtx)
		require.NoError(t, err)
		var prodSettings map[string]any
		require.NoError(t, json.Unmarshal(mockSvc.lastConfigureReq.Settings["openshift"], &prodSettings))
		assert.Equal(t, "prod-ocp", prodSettings["clientid"])
		assert.NotContains(t, prodSettings, "clientId")

		// Simulate ConfigureAuthHandler failure on ApplyOverrides and verify rollback.
		failConfigure = true
		if failConfigure {
			mockSvc.configureAuthHandlerResp = &proto.ConfigureAuthHandlerResponse{Error: "rejected"}
		}
		err = wrapper.ApplyOverrides(prodCtx, map[string]string{"clientId": "bad-override"})
		require.Error(t, err)
		mockSvc.configureAuthHandlerResp = &proto.ConfigureAuthHandlerResponse{ProtocolVersion: PluginProtocolVersion}

		// Invalid profile in context is rejected before gRPC.
		_, err = wrapper.Status(auth.WithProfile(baseCtx, "../evil"))
		require.Error(t, err)
	})

	t.Run("HostService GetAuthToken and GetAuthIdentity honor explicit default profile", func(t *testing.T) {
		t.Parallel()
		var tokenCtxProfile string
		var tokenCtxResolved bool
		var idCtxProfile string
		var idCtxResolved bool

		srv := &HostServiceServer{
			Deps: HostServiceDeps{
				HostConfig: appCfg,
				ProfileResolverFunc: func(_ string) string {
					// Simulates --auth-profile default normalizing to "".
					return ""
				},
				AuthTokenWithHostnameFunc: func(ctx context.Context, _, _, _ string, _ int64, _ bool) (*proto.GetAuthTokenResponse, error) {
					tokenCtxProfile = auth.ProfileFromContext(ctx)
					tokenCtxResolved = auth.IsProfileResolved(ctx)
					return &proto.GetAuthTokenResponse{AccessToken: "ok", TokenType: "Bearer"}, nil
				},
				AuthIdentityFunc: func(ctx context.Context, _, _ string) (*proto.Claims, error) {
					idCtxProfile = auth.ProfileFromContext(ctx)
					idCtxResolved = auth.IsProfileResolved(ctx)
					return &proto.Claims{Email: "default@example.com"}, nil
				},
			},
		}

		tokResp, err := srv.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{HandlerName: "entra"})
		require.NoError(t, err)
		assert.Empty(t, tokResp.Error)
		assert.Empty(t, tokenCtxProfile)
		assert.True(t, tokenCtxResolved, "GetAuthToken must mark empty resolved profile so HostConfig activeProfile does not win")

		idResp, err := srv.GetAuthIdentity(context.Background(), &proto.GetAuthIdentityRequest{HandlerName: "entra"})
		require.NoError(t, err)
		assert.Empty(t, idResp.Error)
		assert.Empty(t, idCtxProfile)
		assert.True(t, idCtxResolved, "GetAuthIdentity must mark resolved profile")

		execCtx := auth.WithProfileResolved(auth.WithProfile(context.Background(), ""))
		execReq, err := buildExecuteProviderRequest(execCtx, "http", []byte(`{"url":"https://example.com"}`))
		require.NoError(t, err)
		assert.Equal(t, auth.DefaultProfileName, execReq.AuthProfile, "explicit built-in profile must serialize as default on ExecuteProviderRequest")
	})
}

// blockingStatefulAuthPlugin simulates a legacy stateful auth plugin that
// ignores per-request Settings and reads only the state stored during
// ConfigureAuthHandler. Optional hooks block an in-flight RPC so tests can
// deterministically interleave concurrent profile switches.
type blockingStatefulAuthPlugin struct {
	mu                sync.Mutex
	configuredProfile string
	configuredClient  string
	configuredTenant  string
	loggedOutProfiles []string

	onEnterWorkToken chan struct{}
	unblockWorkToken chan struct{}
	sameProfBarrier  chan struct{}
}

func (p *blockingStatefulAuthPlugin) GetAuthHandlers(_ context.Context) ([]AuthHandlerInfo, error) {
	return []AuthHandlerInfo{{Name: "entra", DisplayName: "Entra"}}, nil
}

func (p *blockingStatefulAuthPlugin) ConfigureAuthHandler(_ context.Context, handlerName string, cfg ProviderConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configuredProfile = cfg.Profile
	p.configuredClient = ""
	p.configuredTenant = ""
	if raw, ok := cfg.Settings[handlerName]; ok && len(raw) > 0 {
		var m map[string]string
		_ = json.Unmarshal(raw, &m)
		p.configuredClient = m["clientId"]
		p.configuredTenant = m["tenantId"]
	}
	return nil
}

func (p *blockingStatefulAuthPlugin) snapshotState() (profile, clientID, tenantID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.configuredProfile, p.configuredClient, p.configuredTenant
}

func (p *blockingStatefulAuthPlugin) Login(_ context.Context, _ string, _ LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
	prof, clientID, _ := p.snapshotState()
	return &LoginResponse{Claims: &auth.Claims{Subject: prof + ":" + clientID}, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (p *blockingStatefulAuthPlugin) Logout(_ context.Context, _ string, _ LogoutRequest) error {
	prof, clientID, _ := p.snapshotState()
	p.mu.Lock()
	p.loggedOutProfiles = append(p.loggedOutProfiles, prof+":"+clientID)
	p.mu.Unlock()
	return nil
}

func (p *blockingStatefulAuthPlugin) GetStatus(_ context.Context, _ string, _ StatusRequest) (*auth.Status, error) {
	_, clientID, tenantID := p.snapshotState()
	return &auth.Status{Authenticated: true, ClientID: clientID, TenantID: tenantID}, nil
}

func (p *blockingStatefulAuthPlugin) GetToken(ctx context.Context, _ string, _ TokenRequest) (*TokenResponse, error) {
	profBefore, clientBefore, tenantBefore := p.snapshotState()
	if profBefore == "work" && p.onEnterWorkToken != nil {
		select {
		case p.onEnterWorkToken <- struct{}{}:
		default:
		}
		select {
		case <-p.unblockWorkToken:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if p.sameProfBarrier != nil {
		p.sameProfBarrier <- struct{}{}
		<-p.unblockWorkToken
	}
	profAfter, clientAfter, tenantAfter := p.snapshotState()
	return &TokenResponse{
		AccessToken: fmt.Sprintf("%s:%s:%s|%s:%s:%s", profBefore, clientBefore, tenantBefore, profAfter, clientAfter, tenantAfter),
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}, nil
}

func (p *blockingStatefulAuthPlugin) ListCachedTokens(_ context.Context, _ string) ([]*auth.CachedTokenInfo, error) {
	prof, clientID, _ := p.snapshotState()
	return []*auth.CachedTokenInfo{{Flow: auth.Flow(prof + ":" + clientID)}}, nil
}

func (p *blockingStatefulAuthPlugin) PurgeExpiredTokens(_ context.Context, _ string) (int, error) {
	prof, _, _ := p.snapshotState()
	if prof == "prod" {
		return 2, nil
	}
	return 1, nil
}

func (p *blockingStatefulAuthPlugin) DetectAvailableFlows(_ context.Context, _ string) ([]FlowAvailability, error) {
	return nil, nil
}

func (p *blockingStatefulAuthPlugin) StopAuthHandler(_ context.Context, _ string) error {
	return nil
}

func TestAuthHandlerWrapper_StatefulPluginConfigurationStability(t *testing.T) {
	t.Parallel()

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ClientID: "default-client",
				TenantID: "default-tenant",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {ClientID: "work-client", TenantID: "work-tenant"},
					"prod": {ClientID: "prod-client", TenantID: "prod-tenant"},
				},
			},
		},
	}
	baseCtx := config.WithConfig(context.Background(), appCfg)
	workCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, "work"))
	prodCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, "prod"))

	t.Run("cross-profile call waits until in-flight stateful RPC finishes", func(t *testing.T) {
		t.Parallel()
		enteredWork := make(chan struct{}, 1)
		unblockWork := make(chan struct{})
		stateful := &blockingStatefulAuthPlugin{
			onEnterWorkToken: enteredWork,
			unblockWorkToken: unblockWork,
		}
		grpcServer := &AuthHandlerGRPCServer{Impl: stateful}
		wireSvc := &concurrentWireServiceClient{server: grpcServer}
		wrapper := NewAuthHandlerWrapper(NewAuthHandlerClientFromPlugin(NewAuthHandlerGRPCClient(wireSvc)), AuthHandlerInfo{Name: "entra"})

		type tokenResult struct {
			tok *auth.Token
			err error
		}
		workDone := make(chan tokenResult, 1)
		go func() {
			tok, err := wrapper.GetToken(workCtx, auth.TokenOptions{Scope: "api://app/.default"})
			workDone <- tokenResult{tok: tok, err: err}
		}()

		<-enteredWork

		// Launch concurrent prod GetToken and Logout while work GetToken is mid-flight.
		prodDone := make(chan tokenResult, 1)
		go func() {
			tok, err := wrapper.GetToken(prodCtx, auth.TokenOptions{Scope: "api://app/.default"})
			prodDone <- tokenResult{tok: tok, err: err}
		}()
		logoutDone := make(chan error, 1)
		go func() {
			logoutDone <- wrapper.Logout(prodCtx)
		}()

		// Wait until at least one writer registers on wrapper.writersWaiting so we
		// prove prod is actively blocked on the in-flight work RPC rather than racing past it.
		require.Eventually(t, func() bool {
			wrapper.configMu.Lock()
			defer wrapper.configMu.Unlock()
			return wrapper.writersWaiting >= 1
		}, 2*time.Second, 5*time.Millisecond)

		// Cancel a third caller waiting on another profile and verify prompt cancellation.
		cancelCtx, cancel := context.WithTimeout(prodCtx, 25*time.Millisecond)
		defer cancel()
		_, cancelErr := wrapper.ListCachedTokens(cancelCtx)
		require.ErrorIs(t, cancelErr, context.DeadlineExceeded)

		// Release the blocked work RPC; its configured state must still be work!
		close(unblockWork)

		workRes := <-workDone
		require.NoError(t, workRes.err)
		assert.Equal(t, "work:work-client:work-tenant|work:work-client:work-tenant", workRes.tok.AccessToken)

		prodRes := <-prodDone
		require.NoError(t, prodRes.err)
		assert.Equal(t, "prod:prod-client:prod-tenant|prod:prod-client:prod-tenant", prodRes.tok.AccessToken)

		require.NoError(t, <-logoutDone)
		assert.Equal(t, []string{"prod:prod-client"}, stateful.loggedOutProfiles)

		tokens, err := wrapper.ListCachedTokens(workCtx)
		require.NoError(t, err)
		require.Len(t, tokens, 1)
		assert.Equal(t, auth.Flow("work:work-client"), tokens[0].Flow)

		purged, err := wrapper.PurgeExpiredTokens(prodCtx)
		require.NoError(t, err)
		assert.Equal(t, 2, purged)
	})

	t.Run("same-profile calls execute concurrently without serializing", func(t *testing.T) {
		t.Parallel()
		barrier := make(chan struct{}, 2)
		unblock := make(chan struct{})
		stateful := &blockingStatefulAuthPlugin{
			sameProfBarrier:  barrier,
			unblockWorkToken: unblock,
		}
		grpcServer := &AuthHandlerGRPCServer{Impl: stateful}
		wireSvc := &concurrentWireServiceClient{server: grpcServer}
		wrapper := NewAuthHandlerWrapper(NewAuthHandlerClientFromPlugin(NewAuthHandlerGRPCClient(wireSvc)), AuthHandlerInfo{Name: "entra"})

		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tok, err := wrapper.GetToken(workCtx, auth.TokenOptions{})
				require.NoError(t, err)
				assert.Equal(t, "work:work-client:work-tenant|work:work-client:work-tenant", tok.AccessToken)
			}()
		}

		// Both same-profile calls must reach GetToken concurrently before either finishes.
		for range 2 {
			select {
			case <-barrier:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for concurrent same-profile RPCs to overlap")
			}
		}
		close(unblock)
		wg.Wait()
	})
}

type inProcHostServiceAdapter struct {
	proto.HostServiceClient
	server       *HostServiceServer
	stripWireCtx bool
}

func (a *inProcHostServiceAdapter) GetAuthIdentity(ctx context.Context, req *proto.GetAuthIdentityRequest, _ ...grpc.CallOption) (*proto.GetAuthIdentityResponse, error) {
	return a.server.GetAuthIdentity(simulateGRPCWireHop(ctx, a.stripWireCtx), req)
}

func (a *inProcHostServiceAdapter) GetAuthToken(ctx context.Context, req *proto.GetAuthTokenRequest, _ ...grpc.CallOption) (*proto.GetAuthTokenResponse, error) {
	return a.server.GetAuthToken(simulateGRPCWireHop(ctx, a.stripWireCtx), req)
}

func (a *inProcHostServiceAdapter) GetAuthGroups(ctx context.Context, req *proto.GetAuthGroupsRequest, _ ...grpc.CallOption) (*proto.GetAuthGroupsResponse, error) {
	return a.server.GetAuthGroups(simulateGRPCWireHop(ctx, a.stripWireCtx), req)
}

func TestHostService_GetAuthIdentityAndDefaultHandlerResolution(t *testing.T) {
	t.Parallel()

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ActiveProfile: "work",
				ClientID:      "default-client",
				Profiles: map[string]*config.EntraProfileConfig{
					"work":    {ClientID: "work-client"},
					"staging": {ClientID: "staging-client"},
				},
			},
		},
	}

	t.Run("GetAuthIdentity and GetAuthGroups forward staging and explicit default over ProfileResolverFunc", func(t *testing.T) {
		t.Parallel()
		var gotIdentityProfile string
		var gotIdentityResolved bool
		var gotGroupsProfile string
		var gotGroupsResolved bool

		slot := newBrokerTrackerSlot()
		srv := &HostServiceServer{
			Deps: HostServiceDeps{
				HostConfig:  appCfg,
				trackerSlot: slot,
				AuthHandlersFunc: func(_ context.Context) ([]string, string, error) {
					return []string{"entra"}, "entra", nil
				},
				ProfileResolverFunc: func(handlerName string) string {
					return auth.ResolveActiveProfile(config.WithConfig(context.Background(), appCfg), handlerName)
				},
				AuthIdentityFunc: func(ctx context.Context, _, _ string) (*proto.Claims, error) {
					gotIdentityProfile = auth.ProfileFromContext(ctx)
					gotIdentityResolved = auth.IsProfileResolved(ctx)
					return &proto.Claims{Subject: auth.DisplayProfileName(gotIdentityProfile)}, nil
				},
				AuthGroupsFunc: func(ctx context.Context, _ string) ([]string, error) {
					gotGroupsProfile = auth.ProfileFromContext(ctx)
					gotGroupsResolved = auth.IsProfileResolved(ctx)
					return []string{auth.DisplayProfileName(gotGroupsProfile)}, nil
				},
			},
		}
		hostClient := &HostServiceClient{client: &inProcHostServiceAdapter{server: srv}}

		// 1. HostServiceClient with staging profile in context overrides activeProfile "work".
		stagingCtx := auth.WithProfile(context.Background(), "staging")
		claims, err := hostClient.GetAuthIdentity(stagingCtx, "entra", "")
		require.NoError(t, err)
		assert.Equal(t, "staging", claims.Subject)
		assert.Equal(t, "staging", gotIdentityProfile)
		assert.True(t, gotIdentityResolved)

		groups, err := hostClient.GetAuthGroups(stagingCtx, "entra")
		require.NoError(t, err)
		assert.Equal(t, []string{"staging"}, groups)
		assert.Equal(t, "staging", gotGroupsProfile)
		assert.True(t, gotGroupsResolved)

		// 2. HostServiceClient with explicit default profile overrides activeProfile "work".
		defaultPluginCtx := auth.WithProfile(context.Background(), auth.DefaultProfileName)
		claims, err = hostClient.GetAuthIdentity(defaultPluginCtx, "entra", "")
		require.NoError(t, err)
		assert.Equal(t, auth.BuiltinProfileName, claims.Subject)
		assert.Empty(t, gotIdentityProfile)
		assert.True(t, gotIdentityResolved)

		// 3. External SDK v0.19.0 plugin sending bare GetAuthIdentityRequest (no metadata)
		// during Client.ExecuteProvider still inherits the provider execution's staging
		// and explicit default profile via brokerTrackerSlot!
		providerClient := &Client{
			trackerSlot: slot,
			plugin: &MockProviderPlugin{
				execFunc: func(_ context.Context, _ string, _ map[string]any) (*provider.Output, error) {
					resp, rpcErr := srv.GetAuthIdentity(context.Background(), &proto.GetAuthIdentityRequest{HandlerName: "entra"})
					if rpcErr != nil {
						return nil, rpcErr
					}
					return &provider.Output{Data: map[string]any{"subject": resp.Claims.Subject}}, nil
				},
			},
		}

		outStaging, err := providerClient.ExecuteProvider(auth.WithProfileResolved(auth.WithProfile(context.Background(), "staging")), "http", nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"subject": "staging"}, outStaging.Data)

		outDefault, err := providerClient.ExecuteProvider(auth.WithProfileResolved(auth.WithProfile(context.Background(), "")), "http", nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"subject": auth.BuiltinProfileName}, outDefault.Data)
	})

	t.Run("empty HandlerName resolves default handler before profile resolution while preserving allowlist", func(t *testing.T) {
		t.Parallel()
		reg := auth.NewRegistry()
		mockEntra := auth.NewMockHandler("entra")
		mockEntra.StatusResult = &auth.Status{
			Authenticated: true,
			Claims:        &auth.Claims{Email: "work@example.com"},
		}
		require.NoError(t, reg.Register(mockEntra))

		hostCtx := config.WithConfig(context.Background(), appCfg)
		deps := HostDepsFromAuthRegistry(reg)
		deps.HostConfig = appCfg
		deps.ProfileResolverFunc = func(handlerName string) string {
			return auth.ResolveActiveProfile(hostCtx, handlerName)
		}
		srv := &HostServiceServer{Deps: *deps}

		idResp, err := srv.GetAuthIdentity(context.Background(), &proto.GetAuthIdentityRequest{HandlerName: ""})
		require.NoError(t, err)
		assert.Empty(t, idResp.Error)
		assert.Equal(t, "work", mockEntra.LastContextProfile, "empty HandlerName must select default handler entra before resolving activeProfile work")

		mockEntra.GetTokenResult = &auth.Token{AccessToken: "work-tok", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}
		tokResp, err := srv.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{HandlerName: ""})
		require.NoError(t, err)
		assert.Empty(t, tokResp.Error)
		assert.Equal(t, "work", mockEntra.LastContextProfile)

		// Allowlist must still reject empty HandlerName before default-handler fallback.
		restrictedDeps := *deps
		restrictedDeps.AllowedAuthHandlers = []string{"entra"}
		restrictedSrv := &HostServiceServer{Deps: restrictedDeps}
		deniedResp, err := restrictedSrv.GetAuthIdentity(context.Background(), &proto.GetAuthIdentityRequest{HandlerName: ""})
		require.NoError(t, err)
		assert.Contains(t, deniedResp.Error, "access denied")
	})
}

type callbackAwareStatefulPlugin struct {
	mu                sync.Mutex
	configuredProfile string
	configuredClient  string
	onOuterToken      func(ctx context.Context) (string, error)
}

func (p *callbackAwareStatefulPlugin) GetAuthHandlers(_ context.Context) ([]AuthHandlerInfo, error) {
	return []AuthHandlerInfo{{Name: "entra", DisplayName: "Entra"}}, nil
}

func (p *callbackAwareStatefulPlugin) ConfigureAuthHandler(_ context.Context, handlerName string, cfg ProviderConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configuredProfile = cfg.Profile
	p.configuredClient = ""
	if raw, ok := cfg.Settings[handlerName]; ok && len(raw) > 0 {
		var m map[string]string
		_ = json.Unmarshal(raw, &m)
		p.configuredClient = m["clientId"]
	}
	return nil
}

func (p *callbackAwareStatefulPlugin) snapshot() (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.configuredProfile, p.configuredClient
}

func (p *callbackAwareStatefulPlugin) Login(_ context.Context, _ string, _ LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
	return &LoginResponse{Claims: &auth.Claims{Subject: "ok"}, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (p *callbackAwareStatefulPlugin) Logout(_ context.Context, _ string, _ LogoutRequest) error {
	return nil
}

func (p *callbackAwareStatefulPlugin) GetStatus(_ context.Context, _ string, _ StatusRequest) (*auth.Status, error) {
	prof, clientID := p.snapshot()
	return &auth.Status{
		Authenticated: true,
		Claims:        &auth.Claims{Subject: prof + ":" + clientID},
		ClientID:      clientID,
	}, nil
}

func (p *callbackAwareStatefulPlugin) GetToken(ctx context.Context, _ string, req TokenRequest) (*TokenResponse, error) {
	prof, clientID := p.snapshot()
	if req.Scope == "outer" && p.onOuterToken != nil {
		nestedVal, err := p.onOuterToken(ctx)
		if err != nil {
			return nil, err
		}
		profAfter, clientAfter := p.snapshot()
		return &TokenResponse{
			AccessToken: fmt.Sprintf("%s:%s->%s|%s:%s", prof, clientID, nestedVal, profAfter, clientAfter),
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		}, nil
	}
	return &TokenResponse{
		AccessToken: fmt.Sprintf("nested:%s:%s", prof, clientID),
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}, nil
}

func (p *callbackAwareStatefulPlugin) ListCachedTokens(_ context.Context, _ string) ([]*auth.CachedTokenInfo, error) {
	return nil, nil
}

func (p *callbackAwareStatefulPlugin) PurgeExpiredTokens(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (p *callbackAwareStatefulPlugin) DetectAvailableFlows(_ context.Context, _ string) ([]FlowAvailability, error) {
	return nil, nil
}

func (p *callbackAwareStatefulPlugin) StopAuthHandler(_ context.Context, _ string) error {
	return nil
}

func TestAuthHandlerWrapper_ReentrantCallbackLineage(t *testing.T) {
	t.Parallel()

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ActiveProfile: "work",
				ClientID:      "default-client",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {ClientID: "work-client"},
					"prod": {ClientID: "prod-client"},
				},
			},
		},
	}
	baseCtx := config.WithConfig(context.Background(), appCfg)
	workCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, "work"))
	prodCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, "prod"))

	newCallbackHarnessOpts := func(t *testing.T, wireOnlyMetadata bool, outerFn func(ctx context.Context, hostClient *HostServiceClient, hostSrv *HostServiceServer, wrapper *AuthHandlerWrapper) (string, error)) (*AuthHandlerWrapper, *HostServiceClient, *HostServiceServer, *callbackAwareStatefulPlugin, *concurrentWireServiceClient) {
		t.Helper()
		reg := auth.NewRegistry()
		deps := HostDepsFromAuthRegistry(reg)
		deps.HostConfig = appCfg
		deps.ProfileResolverFunc = func(handlerName string) string {
			return auth.ResolveActiveProfile(baseCtx, handlerName)
		}
		if wireOnlyMetadata {
			deps.callbackLineage = nil
		}
		hostSrv := &HostServiceServer{Deps: *deps}
		hostClient := &HostServiceClient{client: &inProcHostServiceAdapter{server: hostSrv, stripWireCtx: wireOnlyMetadata}}

		var wrapper *AuthHandlerWrapper
		pluginImpl := &callbackAwareStatefulPlugin{}
		pluginImpl.onOuterToken = func(ctx context.Context) (string, error) {
			return outerFn(ctx, hostClient, hostSrv, wrapper)
		}
		grpcServer := &AuthHandlerGRPCServer{Impl: pluginImpl}
		wireSvc := &concurrentWireServiceClient{server: grpcServer, stripWireCtx: wireOnlyMetadata}
		authClient := &AuthHandlerClient{
			plugin:   NewAuthHandlerGRPCClient(wireSvc),
			hostDeps: deps,
		}
		wrapper = NewAuthHandlerWrapper(authClient, AuthHandlerInfo{Name: "entra"})
		wrapper.setHostConfig(hostConfig{AppConfig: appCfg})
		require.NoError(t, reg.Register(wrapper))
		return wrapper, hostClient, hostSrv, pluginImpl, wireSvc
	}

	newCallbackHarness := func(t *testing.T, outerFn func(ctx context.Context, hostClient *HostServiceClient, hostSrv *HostServiceServer, wrapper *AuthHandlerWrapper) (string, error)) (*AuthHandlerWrapper, *HostServiceClient, *HostServiceServer) {
		w, hc, hs, _, _ := newCallbackHarnessOpts(t, false, outerFn)
		return w, hc, hs
	}

	t.Run("compatible nested HostService callback succeeds via gRPC metadata while writer is queued", func(t *testing.T) {
		t.Parallel()
		enteredOuter := make(chan struct{}, 1)
		proceedCallback := make(chan struct{})

		wrapper, _, _, _, _ := newCallbackHarnessOpts(t, true, func(ctx context.Context, hostClient *HostServiceClient, _ *HostServiceServer, _ *AuthHandlerWrapper) (string, error) {
			enteredOuter <- struct{}{}
			<-proceedCallback
			tokResp, err := hostClient.GetAuthTokenWithOptions(ctx, AuthTokenRequest{
				Handler: "entra",
				Scope:   "inner",
			})
			if err != nil {
				return "", err
			}
			claims, err := hostClient.GetAuthIdentity(ctx, "entra", "")
			if err != nil {
				return "", err
			}
			return tokResp.AccessToken + "+" + claims.Subject, nil
		})

		type result struct {
			tok *auth.Token
			err error
		}
		workDone := make(chan result, 1)
		go func() {
			tok, err := wrapper.GetToken(workCtx, auth.TokenOptions{Scope: "outer"})
			workDone <- result{tok: tok, err: err}
		}()

		<-enteredOuter

		prodDone := make(chan result, 1)
		go func() {
			tok, err := wrapper.GetToken(prodCtx, auth.TokenOptions{Scope: "prod-scope"})
			prodDone <- result{tok: tok, err: err}
		}()

		require.Eventually(t, func() bool {
			wrapper.configMu.Lock()
			defer wrapper.configMu.Unlock()
			return wrapper.writersWaiting >= 1
		}, 2*time.Second, 5*time.Millisecond)

		// Unblock nested callback while prod writer is actively waiting; must not deadlock!
		close(proceedCallback)

		select {
		case wRes := <-workDone:
			require.NoError(t, wRes.err)
			assert.Equal(t, "work:work-client->nested:work:work-client+work:work-client|work:work-client", wRes.tok.AccessToken)
		case <-time.After(2 * time.Second):
			t.Fatal("deadlock: compatible nested callback blocked behind queued prod writer")
		}

		select {
		case pRes := <-prodDone:
			require.NoError(t, pRes.err)
			assert.Equal(t, "nested:prod:prod-client", pRes.tok.AccessToken)
		case <-time.After(2 * time.Second):
			t.Fatal("queued prod writer did not complete after outer work RPC finished")
		}
	})

	t.Run("compatible nested callback without gRPC metadata succeeds via broker lineage fallback", func(t *testing.T) {
		t.Parallel()
		enteredOuter := make(chan struct{}, 1)
		proceedCallback := make(chan struct{})

		// Run outer call on prodCtx (non-active profile, since ActiveProfile is "work")
		// while workCtx queues as a writer, proving bare SDK callbacks inherit both
		// ancestor call lineage AND the active handler profile ("prod").
		wrapper, _, _ := newCallbackHarness(t, func(_ context.Context, _ *HostServiceClient, hostSrv *HostServiceServer, _ *AuthHandlerWrapper) (string, error) {
			enteredOuter <- struct{}{}
			<-proceedCallback
			// Bare context.Background() simulates an SDK v0.19.0 plugin binary that does not forward incoming gRPC metadata.
			resp, err := hostSrv.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{
				HandlerName: "entra",
				Scope:       "inner",
			})
			if err != nil {
				return "", err
			}
			if resp.Error != "" {
				return "", fmt.Errorf("%s", resp.Error)
			}
			idResp, err := hostSrv.GetAuthIdentity(context.Background(), &proto.GetAuthIdentityRequest{
				HandlerName: "entra",
			})
			if err != nil {
				return "", err
			}
			if idResp.Error != "" {
				return "", fmt.Errorf("%s", idResp.Error)
			}
			return resp.AccessToken + "+" + idResp.Claims.Subject, nil
		})

		type result struct {
			tok *auth.Token
			err error
		}
		prodDone := make(chan result, 1)
		go func() {
			tok, err := wrapper.GetToken(prodCtx, auth.TokenOptions{Scope: "outer"})
			prodDone <- result{tok: tok, err: err}
		}()

		<-enteredOuter

		workDone := make(chan error, 1)
		go func() {
			_, err := wrapper.GetToken(workCtx, auth.TokenOptions{Scope: "work-scope"})
			workDone <- err
		}()

		require.Eventually(t, func() bool {
			wrapper.configMu.Lock()
			defer wrapper.configMu.Unlock()
			return wrapper.writersWaiting >= 1
		}, 2*time.Second, 5*time.Millisecond)

		close(proceedCallback)

		select {
		case pRes := <-prodDone:
			require.NoError(t, pRes.err)
			assert.Equal(t, "prod:prod-client->nested:prod:prod-client+prod:prod-client|prod:prod-client", pRes.tok.AccessToken)
		case <-time.After(2 * time.Second):
			t.Fatal("deadlock on bare SDK nested callback while writer is queued")
		}
		require.NoError(t, <-workDone)
	})

	t.Run("incompatible nested profile switch and nested ApplyOverrides fail fast without deadlocking", func(t *testing.T) {
		t.Parallel()
		wrapper, _, _ := newCallbackHarness(t, func(ctx context.Context, hostClient *HostServiceClient, _ *HostServiceServer, w *AuthHandlerWrapper) (string, error) {
			_, switchErr := hostClient.GetAuthTokenWithOptions(ctx, AuthTokenRequest{
				Handler: "entra",
				Scope:   "inner",
				Profile: "prod",
			})
			if switchErr == nil {
				return "", fmt.Errorf("expected nested profile switch to fail")
			}
			overrideErr := w.ApplyOverrides(ctx, map[string]string{"clientId": "override-in-callback"})
			if overrideErr == nil {
				return "", fmt.Errorf("expected nested ApplyOverrides to fail")
			}
			return switchErr.Error() + " / " + overrideErr.Error(), nil
		})

		done := make(chan struct{})
		var tok *auth.Token
		var callErr error
		go func() {
			tok, callErr = wrapper.GetToken(workCtx, auth.TokenOptions{Scope: "outer"})
			close(done)
		}()

		select {
		case <-done:
			require.NoError(t, callErr)
			assert.Contains(t, tok.AccessToken, "cannot reconfigure from profile \"work\" to \"prod\" inside a nested callback")
			assert.Contains(t, tok.AccessToken, "cannot reconfigure inside a nested callback")
		case <-time.After(2 * time.Second):
			t.Fatal("deadlock: incompatible nested callback waited on its own parent")
		}
	})

	t.Run("serverMode counts in-flight RPCs, blocks concurrent ActivateServerMode, allows compatible callbacks, and rejects nested activation", func(t *testing.T) {
		t.Parallel()
		enteredOuter := make(chan struct{}, 1)
		proceedCallback := make(chan struct{})
		activateEntered := make(chan struct{}, 1)

		var (
			wrapper    *AuthHandlerWrapper
			pluginImpl *callbackAwareStatefulPlugin
			wireSvc    *concurrentWireServiceClient
			hostClient *HostServiceClient
		)
		wrapper, hostClient, _, pluginImpl, wireSvc = newCallbackHarnessOpts(t, true, func(ctx context.Context, hc *HostServiceClient, _ *HostServiceServer, w *AuthHandlerWrapper) (string, error) {
			// Nested ActivateServerMode inside an active server-mode RPC must fail fast.
			nestedActErr := w.ActivateServerMode(ctx, json.RawMessage(`{"clientId":"nested-bad"}`))
			if nestedActErr == nil {
				return "", fmt.Errorf("expected nested ActivateServerMode to fail")
			}

			enteredOuter <- struct{}{}
			<-proceedCallback

			// Compatible nested callback must succeed even while concurrent ActivateServerMode is queued.
			tokResp, err := hc.GetAuthTokenWithOptions(ctx, AuthTokenRequest{
				Handler: "entra",
				Scope:   "inner",
			})
			if err != nil {
				return "", err
			}
			return tokResp.AccessToken + "|" + nestedActErr.Error(), nil
		})

		wireSvc.onActivate = func(ctx context.Context, raw json.RawMessage) error {
			var m map[string]string
			_ = json.Unmarshal(raw, &m)
			if m["clientId"] == "reentrant-from-activate" {
				// Nested GetToken callback while ActivateServerMode is actively reconfiguring must fail fast.
				_, cbErr := hostClient.GetAuthTokenWithOptions(ctx, AuthTokenRequest{
					Handler: "entra",
					Scope:   "inner",
				})
				if cbErr == nil {
					return fmt.Errorf("expected nested callback during ActivateServerMode to fail")
				}
				return nil
			}
			select {
			case activateEntered <- struct{}{}:
			default:
			}
			pluginImpl.mu.Lock()
			pluginImpl.configuredProfile = "srv"
			pluginImpl.configuredClient = m["clientId"]
			pluginImpl.mu.Unlock()
			return nil
		}

		// Initial server-mode activation (plus verify nested callback during ActivateServerMode fails fast).
		require.NoError(t, wrapper.ActivateServerMode(context.Background(), json.RawMessage(`{"clientId":"reentrant-from-activate"}`)))
		require.NoError(t, wrapper.ActivateServerMode(context.Background(), json.RawMessage(`{"clientId":"srv-v1"}`)))
		// Drain initial activateEntered signal.
		<-activateEntered

		type result struct {
			tok *auth.Token
			err error
		}
		outerDone := make(chan result, 1)
		go func() {
			tok, err := wrapper.GetToken(baseCtx, auth.TokenOptions{Scope: "outer"})
			outerDone <- result{tok: tok, err: err}
		}()

		<-enteredOuter

		// Concurrent ActivateServerMode must wait for in-flight server-mode GetToken.
		activateDone := make(chan error, 1)
		go func() {
			activateDone <- wrapper.ActivateServerMode(context.Background(), json.RawMessage(`{"clientId":"srv-v2"}`))
		}()

		require.Eventually(t, func() bool {
			wrapper.configMu.Lock()
			defer wrapper.configMu.Unlock()
			return wrapper.writersWaiting >= 1 && wrapper.inFlight == 1
		}, 2*time.Second, 5*time.Millisecond)

		select {
		case <-activateEntered:
			t.Fatal("concurrent ActivateServerMode entered while server-mode GetToken was still in flight")
		default:
		}

		close(proceedCallback)

		select {
		case out := <-outerDone:
			require.NoError(t, out.err)
			// Outer GetToken saw srv-v1 both before and after nested callback (never srv-v2 mid-RPC).
			assert.Contains(t, out.tok.AccessToken, "srv:srv-v1->nested:srv:srv-v1|")
			assert.Contains(t, out.tok.AccessToken, "|srv:srv-v1")
			assert.Contains(t, out.tok.AccessToken, "cannot reconfigure inside a nested callback")
		case <-time.After(2 * time.Second):
			t.Fatal("deadlock: server-mode outer GetToken blocked while ActivateServerMode was queued")
		}

		select {
		case actErr := <-activateDone:
			require.NoError(t, actErr)
		case <-time.After(2 * time.Second):
			t.Fatal("queued ActivateServerMode did not complete after server-mode GetToken finished")
		}

		afterTok, err := wrapper.GetToken(baseCtx, auth.TokenOptions{Scope: "post-rotate"})
		require.NoError(t, err)
		assert.Equal(t, "nested:srv:srv-v2", afterTok.AccessToken)
	})
}
