// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// fakeGRPCAuthPlugin implements plugin.AuthHandlerPlugin behind a real
// plugin.AuthHandlerGRPCServer + plugin.AuthHandlerGRPCClient +
// plugin.AuthHandlerWrapper stack so CLI tests verify exact wire-level
// Profile and Settings payloads.
type fakeGRPCAuthPlugin struct {
	mu            sync.Mutex
	handlerName   string
	authenticated map[string]bool
	configCalls   []plugin.ProviderConfig
	rpcCalls      []fakePluginRPCCall
}

type fakePluginRPCCall struct {
	method      string
	profile     string
	hasSettings bool
	settings    map[string]any
	tenantFlag  string
}

func newFakePluginHandler(handlerName string, caps []auth.Capability, flows []auth.Flow) (*plugin.AuthHandlerWrapper, *fakeGRPCAuthPlugin) {
	fake := &fakeGRPCAuthPlugin{
		handlerName:   handlerName,
		authenticated: make(map[string]bool),
	}
	server := &plugin.AuthHandlerGRPCServer{Impl: fake}
	svc := &inProcAuthServiceClient{server: server}
	grpcClient := plugin.NewAuthHandlerGRPCClient(svc)
	ahClient := plugin.NewAuthHandlerClientFromPlugin(grpcClient)
	wrapper := plugin.NewAuthHandlerWrapper(ahClient, plugin.AuthHandlerInfo{
		Name:         handlerName,
		DisplayName:  handlerName,
		Capabilities: caps,
		Flows:        flows,
	})
	return wrapper, fake
}

func (f *fakeGRPCAuthPlugin) record(ctx context.Context, method string, explicit map[string]json.RawMessage, tenantFlag string) map[string]any {
	rawMap := explicit
	hasSettings := explicit != nil
	if !hasSettings {
		rawMap, hasSettings = plugin.RequestSettingsFromContext(ctx)
	}
	var decoded map[string]any
	if hasSettings {
		decoded = make(map[string]any, len(rawMap))
		for k, raw := range rawMap {
			var v any
			if err := json.Unmarshal(raw, &v); err == nil {
				decoded[k] = v
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rpcCalls = append(f.rpcCalls, fakePluginRPCCall{
		method:      method,
		profile:     auth.ProfileFromContext(ctx),
		hasSettings: hasSettings,
		settings:    decoded,
		tenantFlag:  tenantFlag,
	})
	return decoded
}

func (f *fakeGRPCAuthPlugin) GetAuthHandlers(_ context.Context) ([]plugin.AuthHandlerInfo, error) {
	return []plugin.AuthHandlerInfo{{Name: f.handlerName, DisplayName: f.handlerName}}, nil
}

func (f *fakeGRPCAuthPlugin) ConfigureAuthHandler(_ context.Context, _ string, cfg plugin.ProviderConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configCalls = append(f.configCalls, cfg)
	return nil
}

func (f *fakeGRPCAuthPlugin) Login(ctx context.Context, _ string, req plugin.LoginRequest, _ func(plugin.DeviceCodePrompt)) (*plugin.LoginResponse, error) {
	settings := f.record(ctx, "Login", req.Settings, req.TenantID)
	prof := auth.ProfileFromContext(ctx)
	f.mu.Lock()
	f.authenticated[prof] = true
	f.mu.Unlock()
	tenant, _ := settings["tenantId"].(string)
	if req.TenantID != "" {
		tenant = req.TenantID
	}
	clientID, _ := settings["clientId"].(string)
	return &plugin.LoginResponse{
		Claims: &auth.Claims{
			Email:    fmt.Sprintf("%s@example.com", auth.DisplayProfileName(prof)),
			TenantID: tenant,
			ClientID: clientID,
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (f *fakeGRPCAuthPlugin) Logout(ctx context.Context, _ string, _ plugin.LogoutRequest) error {
	f.record(ctx, "Logout", nil, "")
	prof := auth.ProfileFromContext(ctx)
	f.mu.Lock()
	delete(f.authenticated, prof)
	f.mu.Unlock()
	return nil
}

func (f *fakeGRPCAuthPlugin) GetStatus(ctx context.Context, _ string, req plugin.StatusRequest) (*auth.Status, error) {
	settings := f.record(ctx, "GetStatus", req.Settings, "")
	prof := auth.ProfileFromContext(ctx)
	f.mu.Lock()
	authed := f.authenticated[prof]
	f.mu.Unlock()
	clientID, _ := settings["clientId"].(string)
	tenantID, _ := settings["tenantId"].(string)
	tokenFile, _ := settings["federatedTokenFile"].(string)
	flow := auth.FlowInteractive
	idType := auth.IdentityTypeUser
	if tokenFile != "" {
		flow = auth.FlowWorkloadIdentity
		idType = auth.IdentityTypeWorkloadIdentity
		authed = true
	} else if secret, _ := settings["clientSecret"].(string); secret != "" && clientID != "" && authed {
		flow = auth.FlowServicePrincipal
		idType = auth.IdentityTypeServicePrincipal
	}
	if !authed {
		return &auth.Status{
			Authenticated: false,
			ClientID:      clientID,
			TenantID:      tenantID,
		}, nil
	}
	return &auth.Status{
		Authenticated: true,
		Flow:          flow,
		IdentityType:  idType,
		ClientID:      clientID,
		TenantID:      tenantID,
		TokenFile:     tokenFile,
		ExpiresAt:     time.Now().Add(time.Hour),
		Claims: &auth.Claims{
			Email:    fmt.Sprintf("%s@example.com", auth.DisplayProfileName(prof)),
			TenantID: tenantID,
			ClientID: clientID,
		},
	}, nil
}

func (f *fakeGRPCAuthPlugin) GetToken(ctx context.Context, _ string, req plugin.TokenRequest) (*plugin.TokenResponse, error) {
	settings := f.record(ctx, "GetToken", req.Settings, "")
	prof := auth.ProfileFromContext(ctx)
	clientID, _ := settings["clientId"].(string)
	tenantID, _ := settings["tenantId"].(string)
	return &plugin.TokenResponse{
		AccessToken: fmt.Sprintf("tok:%s:%s:%s", auth.DisplayProfileName(prof), clientID, tenantID),
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Flow:        auth.FlowInteractive,
	}, nil
}

func (f *fakeGRPCAuthPlugin) ListCachedTokens(ctx context.Context, handlerName string) ([]*auth.CachedTokenInfo, error) {
	f.record(ctx, "ListCachedTokens", nil, "")
	prof := auth.ProfileFromContext(ctx)
	return []*auth.CachedTokenInfo{
		{
			Handler:   handlerName,
			TokenKind: "access",
			Scope:     "profile:" + auth.DisplayProfileName(prof),
			TokenType: "Bearer",
			Flow:      auth.FlowInteractive,
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}, nil
}

func (f *fakeGRPCAuthPlugin) PurgeExpiredTokens(ctx context.Context, _ string) (int, error) {
	f.record(ctx, "PurgeExpiredTokens", nil, "")
	return 1, nil
}

func (f *fakeGRPCAuthPlugin) DetectAvailableFlows(ctx context.Context, _ string) ([]plugin.FlowAvailability, error) {
	settings := f.record(ctx, "DetectAvailableFlows", nil, "")
	if secret, _ := settings["clientSecret"].(string); secret != "" {
		return []plugin.FlowAvailability{
			{Flow: auth.FlowServicePrincipal, Available: true, Reason: "profile clientSecret configured"},
		}, nil
	}
	return []plugin.FlowAvailability{
		{Flow: auth.FlowDeviceCode, Available: true, Reason: "default flow"},
	}, nil
}

func (f *fakeGRPCAuthPlugin) StopAuthHandler(_ context.Context, _ string) error {
	return nil
}

type inProcAuthServiceClient struct {
	proto.AuthHandlerServiceClient
	server *plugin.AuthHandlerGRPCServer
}

func (c *inProcAuthServiceClient) ConfigureAuthHandler(ctx context.Context, req *proto.ConfigureAuthHandlerRequest, _ ...grpc.CallOption) (*proto.ConfigureAuthHandlerResponse, error) {
	return c.server.ConfigureAuthHandler(ctx, req)
}

func (c *inProcAuthServiceClient) Login(ctx context.Context, req *proto.LoginRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error) {
	srvStream := &inProcLoginServerStream{ctx: ctx}
	if err := c.server.Login(req, srvStream); err != nil {
		return nil, err
	}
	return &inProcLoginClientStream{messages: srvStream.messages}, nil
}

func (c *inProcAuthServiceClient) Logout(ctx context.Context, req *proto.LogoutRequest, _ ...grpc.CallOption) (*proto.LogoutResponse, error) {
	return c.server.Logout(ctx, req)
}

func (c *inProcAuthServiceClient) GetStatus(ctx context.Context, req *proto.GetStatusRequest, _ ...grpc.CallOption) (*proto.GetStatusResponse, error) {
	return c.server.GetStatus(ctx, req)
}

func (c *inProcAuthServiceClient) GetToken(ctx context.Context, req *proto.GetTokenRequest, _ ...grpc.CallOption) (*proto.GetTokenResponse, error) {
	return c.server.GetToken(ctx, req)
}

func (c *inProcAuthServiceClient) ListCachedTokens(ctx context.Context, req *proto.ListCachedTokensRequest, _ ...grpc.CallOption) (*proto.ListCachedTokensResponse, error) {
	return c.server.ListCachedTokens(ctx, req)
}

func (c *inProcAuthServiceClient) PurgeExpiredTokens(ctx context.Context, req *proto.PurgeExpiredTokensRequest, _ ...grpc.CallOption) (*proto.PurgeExpiredTokensResponse, error) {
	return c.server.PurgeExpiredTokens(ctx, req)
}

func (c *inProcAuthServiceClient) DetectAvailableFlows(ctx context.Context, req *proto.DetectAvailableFlowsRequest, _ ...grpc.CallOption) (*proto.DetectAvailableFlowsResponse, error) {
	return c.server.DetectAvailableFlows(ctx, req)
}

type inProcLoginServerStream struct {
	grpc.ServerStream
	ctx      context.Context
	messages []*proto.LoginStreamMessage
}

func (s *inProcLoginServerStream) Context() context.Context { return s.ctx }

func (s *inProcLoginServerStream) Send(msg *proto.LoginStreamMessage) error {
	s.messages = append(s.messages, msg)
	return nil
}

type inProcLoginClientStream struct {
	grpc.ClientStream
	messages []*proto.LoginStreamMessage
	idx      int
}

func (s *inProcLoginClientStream) Recv() (*proto.LoginStreamMessage, error) {
	if s.idx >= len(s.messages) {
		return nil, io.EOF
	}
	msg := s.messages[s.idx]
	s.idx++
	return msg, nil
}

func TestCommandLogin_PluginProfileSettingsAndOverrides(t *testing.T) {
	entraCaps := []auth.Capability{auth.CapScopesOnLogin, auth.CapScopesOnTokenRequest, auth.CapTenantID, auth.CapFederatedToken}
	entraFlows := []auth.Flow{auth.FlowDeviceCode, auth.FlowInteractive, auth.FlowServicePrincipal, auth.FlowWorkloadIdentity}

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ClientID:  "default-client",
				TenantID:  "default-tenant",
				Authority: "https://login.microsoftonline.com",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID:      "work-client",
						TenantID:      "work-tenant",
						Authority:     "https://login.microsoftonline.us",
						DefaultScopes: []string{"api://work/.default"},
						ClientSecret:  "work-secret",
					},
				},
			},
		},
	}

	t.Run("login --profile work forwards work settings to DetectAvailableFlows, GetStatus, and Login", func(t *testing.T) {
		ctx, buf := newTestContext(t)
		ctx = config.WithConfig(ctx, appCfg)

		wrapper, fake := newFakePluginHandler("entra", entraCaps, entraFlows)
		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(wrapper))
		ctx = auth.WithRegistry(ctx, reg)

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "work"})

		require.NoError(t, cmd.Execute())
		require.NotEmpty(t, fake.rpcCalls)

		var sawDetect, sawLogin bool
		for _, call := range fake.rpcCalls {
			assert.Equal(t, "work", call.profile, "method %s must receive profile=work", call.method)
			switch call.method {
			case "DetectAvailableFlows":
				sawDetect = true
				assert.Equal(t, "work-client", call.settings["clientId"])
				assert.Equal(t, "work-secret", call.settings["clientSecret"])
			case "Login":
				sawLogin = true
				assert.Equal(t, "work-client", call.settings["clientId"])
				assert.Equal(t, "work-tenant", call.settings["tenantId"])
				assert.Equal(t, "https://login.microsoftonline.us", call.settings["authority"])
				assert.Equal(t, "work-secret", call.settings["clientSecret"])
				assert.Equal(t, []any{"api://work/.default"}, call.settings["defaultScopes"])
			}
		}
		assert.True(t, sawDetect, "DetectAvailableFlows should be invoked")
		assert.True(t, sawLogin, "Login should be invoked")
	})

	t.Run("login --profile work preserves --client-id and --tenant overrides on top of work profile", func(t *testing.T) {
		ctx, buf := newTestContext(t)
		ctx = config.WithConfig(ctx, appCfg)

		wrapper, fake := newFakePluginHandler("entra", entraCaps, entraFlows)
		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(wrapper))
		ctx = auth.WithRegistry(ctx, reg)

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{
			"entra",
			"--profile", "work",
			"--client-id", "cli-override-client",
			"--tenant", "cli-override-tenant",
		})

		require.NoError(t, cmd.Execute())

		for _, call := range fake.rpcCalls {
			assert.Equal(t, "work", call.profile)
			assert.Equal(t, "cli-override-client", call.settings["clientId"], "method %s must preserve --client-id override", call.method)
			assert.Equal(t, "cli-override-tenant", call.settings["tenantId"], "method %s must preserve --tenant override", call.method)
			assert.Equal(t, "https://login.microsoftonline.us", call.settings["authority"], "method %s must preserve profile authority", call.method)
			assert.Equal(t, "work-secret", call.settings["clientSecret"], "method %s must preserve profile credentials", call.method)
		}
	})
}

func TestCommandStatus_MultiProfilePluginReceivesPerProfileSettings(t *testing.T) {
	ctx, buf := newTestContext(t)
	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ActiveProfile: "work",
				ClientID:      "default-client",
				TenantID:      "default-tenant",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID:           "work-wif-client",
						TenantID:           "work-tenant",
						FederatedTokenFile: "/var/run/secrets/work-token",
					},
				},
			},
		},
	}
	ctx = config.WithConfig(ctx, appCfg)

	wrapper, fake := newFakePluginHandler(
		"entra",
		[]auth.Capability{auth.CapTenantID, auth.CapFederatedToken},
		[]auth.Flow{auth.FlowInteractive, auth.FlowWorkloadIdentity},
	)
	fake.authenticated[""] = true

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(wrapper))
	ctx = auth.WithRegistry(ctx, reg)

	cmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "-o", "json"})

	require.NoError(t, cmd.Execute())

	var rows []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
	require.Len(t, rows, 2)

	assert.Equal(t, "built-in", rows[0]["profile"])
	assert.Equal(t, "default-client", rows[0]["clientId"])
	assert.Equal(t, "default-tenant", rows[0]["tenantId"])

	assert.Equal(t, "work (active)", rows[1]["profile"])
	assert.Equal(t, "work-wif-client", rows[1]["clientId"])
	assert.Equal(t, "work-tenant", rows[1]["tenantId"])
	assert.Equal(t, string(auth.FlowWorkloadIdentity), rows[1]["flow"])

	var statusCalls []fakePluginRPCCall
	for _, call := range fake.rpcCalls {
		if call.method == "GetStatus" {
			statusCalls = append(statusCalls, call)
		}
	}
	require.Len(t, statusCalls, 2)
	assert.Equal(t, "", statusCalls[0].profile)
	assert.Equal(t, "default-client", statusCalls[0].settings["clientId"])
	assert.Equal(t, "work", statusCalls[1].profile)
	assert.Equal(t, "work-wif-client", statusCalls[1].settings["clientId"])
}

func TestExplicitProfileDefault_NeverTargetsActiveWorkProfile(t *testing.T) {
	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ActiveProfile: "work",
				ClientID:      "default-client",
				TenantID:      "default-tenant",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID: "work-client",
						TenantID: "work-tenant",
					},
				},
			},
		},
	}

	wrapper, fake := newFakePluginHandler(
		"entra",
		[]auth.Capability{auth.CapTenantID, auth.CapScopesOnLogin, auth.CapScopesOnTokenRequest},
		[]auth.Flow{auth.FlowDeviceCode, auth.FlowInteractive},
	)
	// Pre-populate "work" as authenticated; operations with --profile default must never touch "work".
	fake.authenticated["work"] = true

	newWiredCtx := func(t *testing.T) (context.Context, *terminal.IOStreams, *syncBuffer) {
		t.Helper()
		ctx, buf := newTestContext(t)
		ctx = config.WithConfig(ctx, appCfg)
		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(wrapper))
		ctx = auth.WithRegistry(ctx, reg)
		sb := &syncBuffer{b: buf}
		return ctx, terminal.NewIOStreams(nil, buf, buf, false), sb
	}

	// 1. Prime wrapper on "work" (activeProfile) via status without --profile.
	{
		ctx, ioStreams, _ := newWiredCtx(t)
		cmd := CommandStatus(settings.NewCliParams(), ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "work", "-o", "json"})
		require.NoError(t, cmd.Execute())
	}
	fake.rpcCalls = nil

	// 2. auth login entra --profile default
	{
		ctx, ioStreams, _ := newWiredCtx(t)
		cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "default"})
		require.NoError(t, cmd.Execute())
		assert.True(t, fake.authenticated[""], "default session must be logged in")
		assert.True(t, fake.authenticated["work"], "work session must remain untouched")
	}

	// 3. auth status entra --profile default
	{
		ctx, ioStreams, sb := newWiredCtx(t)
		cmd := CommandStatus(settings.NewCliParams(), ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "default", "-o", "json"})
		require.NoError(t, cmd.Execute())
		assert.Contains(t, sb.b.String(), "default-client")
		assert.NotContains(t, sb.b.String(), "work-client")
	}

	// 4. auth token entra --profile default
	{
		ctx, ioStreams, sb := newWiredCtx(t)
		cmd := CommandToken(settings.NewCliParams(), ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "default", "--scope", "https://graph.microsoft.com/.default"})
		require.NoError(t, cmd.Execute())
		assert.Contains(t, sb.b.String(), "tok:built-in:default-client:default-tenant")
	}

	// 5. auth list entra --profile default
	{
		ctx, ioStreams, sb := newWiredCtx(t)
		cmd := CommandList(settings.NewCliParams(), ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "default", "-o", "json"})
		require.NoError(t, cmd.Execute())
		assert.Contains(t, sb.b.String(), "profile:built-in")
	}

	// 6. auth logout entra --profile default
	{
		ctx, ioStreams, _ := newWiredCtx(t)
		cmd := CommandLogout(settings.NewCliParams(), ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "default"})
		require.NoError(t, cmd.Execute())
		assert.False(t, fake.authenticated[""], "default session must be logged out")
		assert.True(t, fake.authenticated["work"], "work session must NOT be logged out by --profile default")
	}

	// Every recorded RPC across login/status/token/list/logout with --profile default must have profile == "".
	require.NotEmpty(t, fake.rpcCalls)
	for _, call := range fake.rpcCalls {
		assert.Empty(t, call.profile, "method %s must target default (empty) profile, not work", call.method)
		if call.hasSettings && len(call.settings) > 0 {
			assert.Equal(t, "default-client", call.settings["clientId"], "method %s must receive default-client", call.method)
		}
	}
}

type syncBuffer struct {
	b interface{ String() string }
}

func TestSwitchAndProfileDelete_GenericAuthHandlers(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	initialYAML := `version: 1
auth:
  handlers:
    openshift:
      apiTimeout: 30
      profiles:
        work:
          apiTimeout: 60
`
	require.NoError(t, os.WriteFile(configPath, []byte(initialYAML), 0o600))

	mgr := config.NewManager(configPath)
	loadedCfg, err := mgr.Load()
	require.NoError(t, err)

	ctx, buf := newTestContext(t)
	ctx = config.WithConfig(ctx, loadedCfg)
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(auth.NewMockHandler("openshift")))
	ctx = auth.WithRegistry(ctx, reg)

	// Switch openshift activeProfile to "work".
	switchCmd := CommandSwitch(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
	switchCmd.Root().PersistentFlags().String("config", configPath, "")
	switchCmd.SetContext(ctx)
	switchCmd.SetArgs([]string{"openshift", "work"})
	require.NoError(t, switchCmd.Execute())

	reloaded, err := config.NewManager(configPath).Load()
	require.NoError(t, err)
	assert.Equal(t, "work", reloaded.Auth.Handlers["openshift"].ActiveProfile)

	// Delete profile "work" from openshift.
	buf.Reset()
	ctx2 := config.WithConfig(ctx, reloaded)
	delCmd := CommandProfileDelete(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
	delCmd.Root().PersistentFlags().String("config", configPath, "")
	delCmd.SetContext(ctx2)
	delCmd.SetArgs([]string{"openshift", "work", "--force"})
	require.NoError(t, delCmd.Execute())

	afterDel, err := config.NewManager(configPath).Load()
	require.NoError(t, err)
	assert.Empty(t, afterDel.Auth.Handlers["openshift"].ActiveProfile)
	assert.NotContains(t, afterDel.Auth.Handlers["openshift"].Profiles, "work")

	// Also verify CLI status dispatches per-profile settings for generic auth.handlers.<name>.
	ocpWrapper, ocpFake := newFakePluginHandler("openshift", nil, []auth.Flow{auth.FlowInteractive})
	ocpFake.authenticated["work"] = true
	ocpReg := auth.NewRegistry()
	require.NoError(t, ocpReg.Register(ocpWrapper))
	ocpCtx := auth.WithRegistry(config.WithConfig(ctx, loadedCfg), ocpReg)
	buf.Reset()
	statusCmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
	statusCmd.SetContext(ocpCtx)
	statusCmd.SetArgs([]string{"openshift", "--profile", "work", "-o", "json"})
	require.NoError(t, statusCmd.Execute())
	require.NotEmpty(t, ocpFake.rpcCalls)
	assert.Equal(t, "work", ocpFake.rpcCalls[0].profile)
	assert.Equal(t, float64(60), ocpFake.rpcCalls[0].settings["apitimeout"])
}

func TestMixedCaseProfileSaveReloadStatusAndDelete(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("version: 1\n"), 0o600))

	initialCfg, err := config.NewManager(configPath).Load()
	require.NoError(t, err)

	ocpWrapper, ocpFake := newFakePluginHandler("openshift", nil, []auth.Flow{auth.FlowInteractive})
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(ocpWrapper))

	ctx, buf := newTestContext(t)
	ctx = auth.WithRegistry(config.WithConfig(ctx, initialCfg), reg)

	// 1. Login with mixed-case --profile myWork; credentials and registered config key
	// must share one canonical lowercase identity ("mywork") across Viper save/reload.
	loginCmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
	loginCmd.Root().PersistentFlags().String("config", configPath, "")
	loginCmd.SetContext(ctx)
	loginCmd.SetArgs([]string{"openshift", "--profile", "myWork", "--flow", "interactive"})
	require.NoError(t, loginCmd.Execute())
	assert.True(t, ocpFake.authenticated["mywork"], "login must store credentials under canonical lowercase profile")
	assert.False(t, ocpFake.authenticated["myWork"], "mixed-case credential namespace must not diverge from Viper key")

	// 2. Reload config through Viper and run unfiltered auth status -- must discover "mywork" as authenticated.
	reloadedCfg, err := config.NewManager(configPath).Load()
	require.NoError(t, err)
	assert.Contains(t, reloadedCfg.Auth.Handlers["openshift"].Profiles, "mywork")

	buf.Reset()
	reloadCtx := auth.WithRegistry(config.WithConfig(ctx, reloadedCfg), reg)
	statusCmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
	statusCmd.SetContext(reloadCtx)
	statusCmd.SetArgs([]string{"openshift", "-o", "json"})
	require.NoError(t, statusCmd.Execute())

	var rows []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
	var foundMyWork bool
	for _, r := range rows {
		if r["profile"] == "mywork" && r["authenticated"] == true {
			foundMyWork = true
		}
	}
	assert.True(t, foundMyWork, "unfiltered auth status after Viper reload must find authenticated mywork session")

	// 3. Delete profile (using reloaded lowercase or mixed-case spelling) -- must clear both config and credentials.
	buf.Reset()
	delCmd := CommandProfileDelete(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
	delCmd.Root().PersistentFlags().String("config", configPath, "")
	delCmd.SetContext(reloadCtx)
	delCmd.SetArgs([]string{"openshift", "mywork"})
	require.NoError(t, delCmd.Execute())

	assert.False(t, ocpFake.authenticated["mywork"], "profile delete must clear stored credentials")
	afterDelCfg, err := config.NewManager(configPath).Load()
	require.NoError(t, err)
	assert.NotContains(t, afterDelCfg.Auth.Handlers["openshift"].Profiles, "mywork")
}
