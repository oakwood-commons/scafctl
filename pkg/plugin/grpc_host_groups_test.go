// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"

	sdkauth "github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// --- GetAuthGroups server tests ---

func TestHostServiceServer_GetAuthGroups_Success(t *testing.T) {
	deps := HostServiceDeps{
		AuthGroupsFunc: func(_ context.Context, _ string) ([]string, error) {
			return []string{"group-a", "group-b"}, nil
		},
	}
	server := &HostServiceServer{Deps: deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, []string{"group-a", "group-b"}, resp.Groups)
}

func TestHostServiceServer_GetAuthGroups_NotAvailable(t *testing.T) {
	server := &HostServiceServer{Deps: HostServiceDeps{}}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "auth groups not available")
}

func TestHostServiceServer_GetAuthGroups_Denied(t *testing.T) {
	deps := HostServiceDeps{
		AllowedAuthHandlers: []string{"github"},
		AuthGroupsFunc: func(_ context.Context, _ string) ([]string, error) {
			t.Fatal("should not be called for denied handler")
			return nil, nil
		},
	}
	server := &HostServiceServer{Deps: deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")
}

func TestHostServiceServer_GetAuthGroups_FuncError(t *testing.T) {
	deps := HostServiceDeps{
		AuthGroupsFunc: func(_ context.Context, _ string) ([]string, error) {
			return nil, fmt.Errorf("graph API unavailable")
		},
	}
	server := &HostServiceServer{Deps: deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "graph API unavailable")
}

// TestHostServiceServer_GetAuthGroups_WiredFromRegistry exercises the full path:
// deps produced by HostDepsFromAuthRegistry driving the RPC server against a
// real auth.Registry whose handler implements GroupsProvider.
func TestHostServiceServer_GetAuthGroups_WiredFromRegistry(t *testing.T) {
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(&mockGroupsHandler{
		MockHandler: auth.NewMockHandler("entra"),
		groups:      []string{"g1", "g2", "g3"},
	}))

	deps := HostDepsFromAuthRegistry(reg)
	require.NotNil(t, deps)
	server := &HostServiceServer{Deps: *deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, []string{"g1", "g2", "g3"}, resp.Groups)
}

// TestHostServiceServer_GetAuthGroups_WiredFromRegistry_Denied verifies the
// allowlist gate still blocks a disallowed handler even when the callback is
// wired from a real registry.
func TestHostServiceServer_GetAuthGroups_WiredFromRegistry_Denied(t *testing.T) {
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(&mockGroupsHandler{
		MockHandler: auth.NewMockHandler("entra"),
		groups:      []string{"g1"},
	}))

	deps := HostDepsFromAuthRegistry(reg)
	require.NotNil(t, deps)
	deps.AllowedAuthHandlers = []string{"github"}
	server := &HostServiceServer{Deps: *deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")
	assert.Nil(t, resp.Groups)

	// Empty HandlerName must also be rejected rather than bypassing the allowlist via default handler.
	resp, err = server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")
	assert.Nil(t, resp.Groups)
}

// --- GetAuthGroups client tests ---

func TestHostServiceClient_GetAuthGroups_Success(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthGroupsResp: &proto.GetAuthGroupsResponse{
			Groups: []string{"group-x"},
		},
	}
	client := &HostServiceClient{client: mock}

	groups, err := client.GetAuthGroups(context.Background(), "entra")
	require.NoError(t, err)
	assert.Equal(t, []string{"group-x"}, groups)
}

func TestHostServiceClient_GetAuthGroups_RPCError(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthGroupsErr: fmt.Errorf("connection lost"),
	}
	client := &HostServiceClient{client: mock}

	_, err := client.GetAuthGroups(context.Background(), "entra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}

func TestHostServiceClient_GetAuthGroups_ResponseError(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthGroupsResp: &proto.GetAuthGroupsResponse{
			Error: "handler does not support groups",
		},
	}
	client := &HostServiceClient{client: mock}

	_, err := client.GetAuthGroups(context.Background(), "github")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler does not support groups")
}

func TestHostServiceClient_GetAuthGroups_ForwardsProfileAndNormalizesNilGroups(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthGroupsResp: &proto.GetAuthGroupsResponse{},
	}
	client := &HostServiceClient{client: mock}

	ctx := auth.WithProfile(context.Background(), "work")
	groups, err := client.GetAuthGroups(ctx, "entra")
	require.NoError(t, err)
	assert.NotNil(t, groups)
	assert.Empty(t, groups)
	require.NotNil(t, mock.lastGetAuthGroupsReq)
	assert.Equal(t, "entra", mock.lastGetAuthGroupsReq.HandlerName)
	assert.Equal(t, "work", mock.lastGetAuthGroupsReq.Profile)

	// Explicitly resolved empty profile is encoded as auth.DefaultProfileName on both GetAuthGroups and GetAuthToken.
	resolvedBuiltinCtx := auth.WithProfileResolved(sdkauth.WithProfile(context.Background(), "work"))
	_, err = client.GetAuthGroups(resolvedBuiltinCtx, "entra")
	require.NoError(t, err)
	assert.Equal(t, auth.DefaultProfileName, mock.lastGetAuthGroupsReq.Profile)

	mock.getAuthTokenResp = &proto.GetAuthTokenResponse{AccessToken: "tok", TokenType: "Bearer"}
	_, err = client.GetAuthToken(resolvedBuiltinCtx, "entra", "scope", 60, false)
	require.NoError(t, err)
	require.NotNil(t, mock.lastGetAuthTokenReq)
	assert.Equal(t, auth.DefaultProfileName, mock.lastGetAuthTokenReq.Profile)

	// Also supports sdkauth.WithProfile and degrades gracefully on Unimplemented.
	unimplMock := &mockHostServiceClient{
		getAuthGroupsErr: status.Error(codes.Unimplemented, "old host"),
	}
	unimplClient := &HostServiceClient{client: unimplMock}
	groups, err = unimplClient.GetAuthGroups(sdkauth.WithProfile(context.Background(), "sdk-prof"), "entra")
	require.NoError(t, err)
	assert.NotNil(t, groups)
	assert.Empty(t, groups)
	assert.Equal(t, "sdk-prof", unimplMock.lastGetAuthGroupsReq.Profile)
}

func TestHostServiceServer_GetAuthGroups_PluginWrapperRoundTripWithProfileAndSettings(t *testing.T) {
	t.Parallel()

	var capturedReq *proto.GetGroupsRequest
	mockPluginClient := &mockAuthHandlerServiceClient{
		getGroupsFunc: func(_ context.Context, req *proto.GetGroupsRequest) (*proto.GetGroupsResponse, error) {
			capturedReq = req
			return &proto.GetGroupsResponse{
				Groups: []string{"entra-grp-1", "entra-grp-2"},
			}, nil
		},
	}

	reg := auth.NewRegistry()
	wrapper := NewAuthHandlerWrapper(
		&AuthHandlerClient{plugin: &AuthHandlerGRPCClient{client: mockPluginClient}},
		AuthHandlerInfo{Name: "entra", DisplayName: "Microsoft Entra ID"},
	)
	require.NoError(t, reg.Register(wrapper))

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ClientID: "top-client",
				TenantID: "top-tenant",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {
						ClientID: "work-client",
						TenantID: "work-tenant",
					},
				},
			},
		},
	}

	deps := HostDepsFromAuthRegistry(reg)
	require.NotNil(t, deps)
	deps.HostConfig = appCfg
	deps.ProfileResolverFunc = func(_ string) string { return "work" }
	server := &HostServiceServer{Deps: *deps}

	// 1. Provider omits profile -> HostServiceServer resolves "work" via ProfileResolverFunc + HostConfig.
	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, []string{"entra-grp-1", "entra-grp-2"}, resp.Groups)
	require.NotNil(t, capturedReq)
	assert.Equal(t, "entra", capturedReq.HandlerName)
	assert.Equal(t, "work", capturedReq.Profile)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(capturedReq.Settings, &settings))
	assert.Equal(t, "work-client", settings["clientId"])
	assert.Equal(t, "work-tenant", settings["tenantId"])

	// 2. Explicit "default" (or "built-in") -> overrides ProfileResolverFunc / activeProfile AND any parent ctx profile.
	capturedReq = nil
	resp, err = server.GetAuthGroups(auth.WithProfile(context.Background(), "work"), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
		Profile:     "default",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	require.NotNil(t, capturedReq)
	assert.Empty(t, capturedReq.Profile)
	settings = nil
	require.NoError(t, json.Unmarshal(capturedReq.Settings, &settings))
	assert.Equal(t, "top-client", settings["clientId"])

	// 3. Invalid profile from plugin is rejected cleanly.
	resp, err = server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
		Profile:     "invalid profile",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "invalid profile")

	// 4. Host running with --auth-profile default (ProfileResolverFunc returning "")
	// must NOT leak HostConfig's activeProfile ("work") when req.Profile is empty.
	appCfg.Auth.Entra.ActiveProfile = "work"
	hostDefaultCtx := auth.WithGlobalProfile(config.WithConfig(context.Background(), appCfg), "default")
	deps.ProfileResolverFunc = func(h string) string {
		return auth.ResolveActiveProfile(hostDefaultCtx, h)
	}
	serverWithDefaultFlag := &HostServiceServer{Deps: *deps}
	capturedReq = nil
	resp, err = serverWithDefaultFlag.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	require.NotNil(t, capturedReq)
	assert.Empty(t, capturedReq.Profile, "--auth-profile default must not leak activeProfile=work")
	settings = nil
	require.NoError(t, json.Unmarshal(capturedReq.Settings, &settings))
	assert.Equal(t, "top-client", settings["clientId"])

	// 5. Empty HandlerName with no --auth-profile resolves default handler ("entra")
	// before consulting ProfileResolverFunc, so activeProfile="work" applies cleanly.
	hostNoFlagCtx := config.WithConfig(context.Background(), appCfg)
	deps.ProfileResolverFunc = func(h string) string {
		return auth.ResolveActiveProfile(hostNoFlagCtx, h)
	}
	serverDefaultHandler := &HostServiceServer{Deps: *deps}
	capturedReq = nil
	resp, err = serverDefaultHandler.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	require.NotNil(t, capturedReq)
	assert.Equal(t, "work", capturedReq.Profile)
	settings = nil
	require.NoError(t, json.Unmarshal(capturedReq.Settings, &settings))
	assert.Equal(t, "work-client", settings["clientId"])
}

func BenchmarkHostServiceServer_GetAuthGroups(b *testing.B) {
	reg := auth.NewRegistry()
	_ = reg.Register(&mockGroupsHandler{
		MockHandler: auth.NewMockHandler("entra"),
		groups:      []string{"g1", "g2"},
	})
	deps := HostDepsFromAuthRegistry(reg)
	server := &HostServiceServer{Deps: *deps}
	req := &proto.GetAuthGroupsRequest{HandlerName: "entra"}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = server.GetAuthGroups(ctx, req)
	}
}

func TestHostServiceServer_GetAuthGroups_OlderPluginUnimplementedReturnsCleanError(t *testing.T) {
	t.Parallel()

	mockPluginClient := &mockAuthHandlerServiceClient{
		getGroupsErr: status.Error(codes.Unimplemented, "unknown method GetGroups"),
	}
	reg := auth.NewRegistry()
	lazy := &LazyAuthHandlerWrapper{
		name:        "github",
		displayName: "github",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return NewAuthHandlerWrapper(
				&AuthHandlerClient{plugin: &AuthHandlerGRPCClient{client: mockPluginClient}},
				AuthHandlerInfo{Name: "github"},
			), nil
		},
	}
	require.NoError(t, reg.Register(lazy))

	deps := HostDepsFromAuthRegistry(reg)
	server := &HostServiceServer{Deps: *deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "github",
	})
	require.NoError(t, err)
	assert.Nil(t, resp.Groups)
	assert.Equal(t, `get auth groups: auth handler "github" does not support group membership queries (GroupsProvider not implemented)`, resp.Error)
}

func TestHostServiceClientToServer_ResolvedBuiltinProfileRoundTrip(t *testing.T) {
	t.Parallel()

	var capturedGroupsReq *proto.GetGroupsRequest
	mockPluginClient := &mockAuthHandlerServiceClient{
		getTokenResp: &proto.GetTokenResponse{
			AccessToken:   "tok-builtin",
			TokenType:     "Bearer",
			ExpiresAtUnix: 1900000000,
		},
		getGroupsFunc: func(_ context.Context, req *proto.GetGroupsRequest) (*proto.GetGroupsResponse, error) {
			capturedGroupsReq = req
			return &proto.GetGroupsResponse{Groups: []string{"builtin-grp"}}, nil
		},
	}

	reg := auth.NewRegistry()
	wrapper := NewAuthHandlerWrapper(
		&AuthHandlerClient{plugin: &AuthHandlerGRPCClient{client: mockPluginClient}},
		AuthHandlerInfo{Name: "entra", DisplayName: "Microsoft Entra ID"},
	)
	require.NoError(t, reg.Register(wrapper))

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ClientID:      "top-client",
				TenantID:      "top-tenant",
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

	deps := HostDepsFromAuthRegistry(reg)
	require.NotNil(t, deps)
	deps.HostConfig = appCfg
	deps.ProfileResolverFunc = func(_ string) string { return "work" }

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	proto.RegisterHostServiceServer(grpcServer, &HostServiceServer{Deps: *deps})
	go func() {
		_ = grpcServer.Serve(lis)
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	hostClient := NewHostServiceClient(conn)

	// 1. Explicitly resolved built-in profile ("" with IsProfileResolved, even when sdkauth carries "work")
	// survives the client->server gRPC round-trip instead of falling back to activeProfile="work".
	builtinCtx := auth.WithProfileResolved(sdkauth.WithProfile(context.Background(), "work"))

	groups, err := hostClient.GetAuthGroups(builtinCtx, "entra")
	require.NoError(t, err)
	assert.Equal(t, []string{"builtin-grp"}, groups)
	require.NotNil(t, capturedGroupsReq)
	assert.Empty(t, capturedGroupsReq.Profile)
	var groupSettings map[string]any
	require.NoError(t, json.Unmarshal(capturedGroupsReq.Settings, &groupSettings))
	assert.Equal(t, "top-client", groupSettings["clientId"])

	tokResp, err := hostClient.GetAuthToken(builtinCtx, "entra", "https://graph.microsoft.com/.default", 60, false)
	require.NoError(t, err)
	assert.Equal(t, "tok-builtin", tokResp.AccessToken)
	require.NotNil(t, mockPluginClient.capturedGetTokenReq)
	assert.Empty(t, mockPluginClient.capturedGetTokenReq.Profile)
	var tokenSettings map[string]any
	require.NoError(t, json.Unmarshal(mockPluginClient.capturedGetTokenReq.Settings, &tokenSettings))
	assert.Equal(t, "top-client", tokenSettings["clientId"])

	// 2. Unresolved empty context still falls back to the host's activeProfile ("work").
	capturedGroupsReq = nil
	_, err = hostClient.GetAuthGroups(context.Background(), "entra")
	require.NoError(t, err)
	require.NotNil(t, capturedGroupsReq)
	assert.Equal(t, "work", capturedGroupsReq.Profile)
	groupSettings = nil
	require.NoError(t, json.Unmarshal(capturedGroupsReq.Settings, &groupSettings))
	assert.Equal(t, "work-client", groupSettings["clientId"])
}
