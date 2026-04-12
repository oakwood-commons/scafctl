// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/plugin/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- Mock AuthHandlerServiceClient ---

type mockAuthHandlerServiceClient struct {
	proto.AuthHandlerServiceClient

	getAuthHandlersResp      *proto.GetAuthHandlersResponse
	getAuthHandlersErr       error
	configureAuthHandlerResp *proto.ConfigureAuthHandlerResponse
	configureAuthHandlerErr  error
	loginFunc                func(ctx context.Context, req *proto.LoginRequest) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error)
	logoutResp               *proto.LogoutResponse
	logoutErr                error
	getStatusResp            *proto.GetStatusResponse
	getStatusErr             error
	getTokenResp             *proto.GetTokenResponse
	getTokenErr              error
	listCachedTokensResp     *proto.ListCachedTokensResponse
	listCachedTokensErr      error
	purgeExpiredTokensResp   *proto.PurgeExpiredTokensResponse
	purgeExpiredTokensErr    error
	stopAuthHandlerResp      *proto.StopAuthHandlerResponse
	stopAuthHandlerErr       error
}

func (m *mockAuthHandlerServiceClient) GetAuthHandlers(_ context.Context, _ *proto.GetAuthHandlersRequest, _ ...grpc.CallOption) (*proto.GetAuthHandlersResponse, error) {
	return m.getAuthHandlersResp, m.getAuthHandlersErr
}

func (m *mockAuthHandlerServiceClient) ConfigureAuthHandler(_ context.Context, req *proto.ConfigureAuthHandlerRequest, _ ...grpc.CallOption) (*proto.ConfigureAuthHandlerResponse, error) {
	return m.configureAuthHandlerResp, m.configureAuthHandlerErr
}

func (m *mockAuthHandlerServiceClient) Login(ctx context.Context, req *proto.LoginRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error) {
	if m.loginFunc != nil {
		return m.loginFunc(ctx, req)
	}
	return nil, fmt.Errorf("login not configured")
}

func (m *mockAuthHandlerServiceClient) Logout(_ context.Context, _ *proto.LogoutRequest, _ ...grpc.CallOption) (*proto.LogoutResponse, error) {
	return m.logoutResp, m.logoutErr
}

func (m *mockAuthHandlerServiceClient) GetStatus(_ context.Context, _ *proto.GetStatusRequest, _ ...grpc.CallOption) (*proto.GetStatusResponse, error) {
	return m.getStatusResp, m.getStatusErr
}

func (m *mockAuthHandlerServiceClient) GetToken(_ context.Context, _ *proto.GetTokenRequest, _ ...grpc.CallOption) (*proto.GetTokenResponse, error) {
	return m.getTokenResp, m.getTokenErr
}

func (m *mockAuthHandlerServiceClient) ListCachedTokens(_ context.Context, _ *proto.ListCachedTokensRequest, _ ...grpc.CallOption) (*proto.ListCachedTokensResponse, error) {
	return m.listCachedTokensResp, m.listCachedTokensErr
}

func (m *mockAuthHandlerServiceClient) PurgeExpiredTokens(_ context.Context, _ *proto.PurgeExpiredTokensRequest, _ ...grpc.CallOption) (*proto.PurgeExpiredTokensResponse, error) {
	return m.purgeExpiredTokensResp, m.purgeExpiredTokensErr
}

func (m *mockAuthHandlerServiceClient) StopAuthHandler(_ context.Context, _ *proto.StopAuthHandlerRequest, _ ...grpc.CallOption) (*proto.StopAuthHandlerResponse, error) {
	return m.stopAuthHandlerResp, m.stopAuthHandlerErr
}

// --- Mock login stream ---

type mockLoginStreamClient struct {
	grpc.ClientStream
	messages []*proto.LoginStreamMessage
	idx      int
}

func (s *mockLoginStreamClient) Recv() (*proto.LoginStreamMessage, error) {
	if s.idx >= len(s.messages) {
		return nil, io.EOF
	}
	msg := s.messages[s.idx]
	s.idx++
	return msg, nil
}

// =====================================================================
// AuthHandlerGRPCClient tests
// =====================================================================

func TestAuthHandlerGRPCClient_GetAuthHandlers_Success(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		getAuthHandlersResp: &proto.GetAuthHandlersResponse{
			Handlers: []*proto.AuthHandlerInfo{
				{
					Name:         "azure",
					DisplayName:  "Azure AD",
					Flows:        []string{"device_code", "service_principal"},
					Capabilities: []string{"scopes_on_login", "tenant_id"},
				},
				{
					Name:        "github",
					DisplayName: "GitHub OAuth",
					Flows:       []string{"interactive"},
				},
			},
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	handlers, err := client.GetAuthHandlers(context.Background())
	require.NoError(t, err)
	require.Len(t, handlers, 2)

	assert.Equal(t, "azure", handlers[0].Name)
	assert.Equal(t, "Azure AD", handlers[0].DisplayName)
	assert.Equal(t, []auth.Flow{"device_code", "service_principal"}, handlers[0].Flows)
	assert.Equal(t, []auth.Capability{"scopes_on_login", "tenant_id"}, handlers[0].Capabilities)

	assert.Equal(t, "github", handlers[1].Name)
	assert.Len(t, handlers[1].Capabilities, 0)
}

func TestAuthHandlerGRPCClient_GetAuthHandlers_Error(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		getAuthHandlersErr: fmt.Errorf("connection error"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	_, err := client.GetAuthHandlers(context.Background())
	require.Error(t, err)
}

func TestAuthHandlerGRPCClient_Login_Success(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	mock := &mockAuthHandlerServiceClient{
		loginFunc: func(_ context.Context, _ *proto.LoginRequest) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error) {
			return &mockLoginStreamClient{
				messages: []*proto.LoginStreamMessage{
					{
						Payload: &proto.LoginStreamMessage_Result{
							Result: &proto.LoginResult{
								Claims: &proto.Claims{
									Email:         "user@example.com",
									Name:          "Test User",
									ExpiresAtUnix: now.Add(time.Hour).Unix(),
								},
								ExpiresAtUnix: now.Add(time.Hour).Unix(),
							},
						},
					},
				},
			}, nil
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	resp, err := client.Login(context.Background(), "azure", LoginRequest{
		TenantID: "tenant-123",
		Scopes:   []string{"openid"},
		Flow:     auth.FlowDeviceCode,
		Timeout:  30 * time.Second,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "user@example.com", resp.Claims.Email)
	assert.Equal(t, now.Add(time.Hour).Unix(), resp.ExpiresAt.Unix())
}

func TestAuthHandlerGRPCClient_Login_WithDeviceCodePrompt(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		loginFunc: func(_ context.Context, _ *proto.LoginRequest) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error) {
			return &mockLoginStreamClient{
				messages: []*proto.LoginStreamMessage{
					{
						Payload: &proto.LoginStreamMessage_DeviceCodePrompt{
							DeviceCodePrompt: &proto.DeviceCodePrompt{
								UserCode:        "ABCD-1234",
								VerificationUri: "https://device.login.example.com",
								Message:         "Go to the URL and enter the code",
							},
						},
					},
					{
						Payload: &proto.LoginStreamMessage_Result{
							Result: &proto.LoginResult{
								Claims: &proto.Claims{
									Email: "user@example.com",
								},
								ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
							},
						},
					},
				},
			}, nil
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	var receivedPrompt DeviceCodePrompt
	cb := func(p DeviceCodePrompt) {
		receivedPrompt = p
	}

	resp, err := client.Login(context.Background(), "azure", LoginRequest{}, cb)
	require.NoError(t, err)
	assert.Equal(t, "user@example.com", resp.Claims.Email)
	assert.Equal(t, "ABCD-1234", receivedPrompt.UserCode)
	assert.Equal(t, "https://device.login.example.com", receivedPrompt.VerificationURI)
}

func TestAuthHandlerGRPCClient_Login_DeviceCodeNilCallback(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		loginFunc: func(_ context.Context, _ *proto.LoginRequest) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error) {
			return &mockLoginStreamClient{
				messages: []*proto.LoginStreamMessage{
					{
						Payload: &proto.LoginStreamMessage_DeviceCodePrompt{
							DeviceCodePrompt: &proto.DeviceCodePrompt{UserCode: "CODE"},
						},
					},
					{
						Payload: &proto.LoginStreamMessage_Result{
							Result: &proto.LoginResult{
								Claims:        &proto.Claims{Email: "u@e.com"},
								ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
							},
						},
					},
				},
			}, nil
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	// nil callback should not panic
	resp, err := client.Login(context.Background(), "h", LoginRequest{}, nil)
	require.NoError(t, err)
	assert.Equal(t, "u@e.com", resp.Claims.Email)
}

func TestAuthHandlerGRPCClient_Login_StreamError(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		loginFunc: func(_ context.Context, _ *proto.LoginRequest) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error) {
			return nil, fmt.Errorf("stream setup failed")
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	_, err := client.Login(context.Background(), "h", LoginRequest{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "login RPC failed")
}

func TestAuthHandlerGRPCClient_Login_ErrorMessage(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		loginFunc: func(_ context.Context, _ *proto.LoginRequest) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error) {
			return &mockLoginStreamClient{
				messages: []*proto.LoginStreamMessage{
					{
						Payload: &proto.LoginStreamMessage_Error{
							Error: "auth failed: invalid credentials",
						},
					},
				},
			}, nil
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	_, err := client.Login(context.Background(), "h", LoginRequest{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid credentials")
}

func TestAuthHandlerGRPCClient_Login_EOFWithoutResult(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		loginFunc: func(_ context.Context, _ *proto.LoginRequest) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error) {
			return &mockLoginStreamClient{messages: nil}, nil
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	_, err := client.Login(context.Background(), "h", LoginRequest{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "login stream ended without result")
}

func TestAuthHandlerGRPCClient_Logout_Success(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		logoutResp: &proto.LogoutResponse{},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.Logout(context.Background(), "azure")
	require.NoError(t, err)
}

func TestAuthHandlerGRPCClient_Logout_Error(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		logoutErr: fmt.Errorf("logout failed"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.Logout(context.Background(), "azure")
	require.Error(t, err)
}

func TestAuthHandlerGRPCClient_GetStatus_Success(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	mock := &mockAuthHandlerServiceClient{
		getStatusResp: &proto.GetStatusResponse{
			Authenticated: true,
			Claims: &proto.Claims{
				Email: "user@example.com",
				Name:  "Test User",
			},
			ExpiresAtUnix:   now.Add(time.Hour).Unix(),
			LastRefreshUnix: now.Unix(),
			TenantId:        "tenant-abc",
			IdentityType:    "user",
			ClientId:        "client-123",
			TokenFile:       "/tmp/token",
			Scopes:          []string{"read", "write"},
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	s, err := client.GetStatus(context.Background(), "azure")
	require.NoError(t, err)
	assert.True(t, s.Authenticated)
	assert.Equal(t, "user@example.com", s.Claims.Email)
	assert.Equal(t, "tenant-abc", s.TenantID)
	assert.Equal(t, auth.IdentityType("user"), s.IdentityType)
	assert.Equal(t, []string{"read", "write"}, s.Scopes)
}

func TestAuthHandlerGRPCClient_GetStatus_Error(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		getStatusErr: fmt.Errorf("status error"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	_, err := client.GetStatus(context.Background(), "azure")
	require.Error(t, err)
}

func TestAuthHandlerGRPCClient_GetToken_Success(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	mock := &mockAuthHandlerServiceClient{
		getTokenResp: &proto.GetTokenResponse{
			AccessToken:   "eyJtoken123",
			TokenType:     "Bearer",
			ExpiresAtUnix: now.Add(time.Hour).Unix(),
			Scope:         "read write",
			CachedAtUnix:  now.Unix(),
			Flow:          "device_code",
			SessionId:     "session-abc",
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	resp, err := client.GetToken(context.Background(), "azure", TokenRequest{
		Scope:        "read write",
		MinValidFor:  5 * time.Minute,
		ForceRefresh: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "eyJtoken123", resp.AccessToken)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Equal(t, "read write", resp.Scope)
	assert.Equal(t, auth.Flow("device_code"), resp.Flow)
	assert.Equal(t, "session-abc", resp.SessionID)
}

func TestAuthHandlerGRPCClient_GetToken_Error(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		getTokenErr: fmt.Errorf("token error"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	_, err := client.GetToken(context.Background(), "azure", TokenRequest{})
	require.Error(t, err)
}

func TestAuthHandlerGRPCClient_ListCachedTokens_Success(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	mock := &mockAuthHandlerServiceClient{
		listCachedTokensResp: &proto.ListCachedTokensResponse{
			Tokens: []*proto.CachedTokenInfo{
				{
					Handler:       "azure",
					TokenKind:     "access",
					Scope:         "read",
					TokenType:     "Bearer",
					Flow:          "device_code",
					ExpiresAtUnix: now.Add(time.Hour).Unix(),
					CachedAtUnix:  now.Unix(),
					IsExpired:     false,
					SessionId:     "s1",
				},
				{
					Handler:       "azure",
					TokenKind:     "refresh",
					Scope:         "openid",
					TokenType:     "Bearer",
					Flow:          "interactive",
					ExpiresAtUnix: now.Add(-time.Hour).Unix(),
					IsExpired:     true,
					SessionId:     "s2",
				},
			},
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	tokens, err := client.ListCachedTokens(context.Background(), "azure")
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	assert.Equal(t, "azure", tokens[0].Handler)
	assert.Equal(t, "access", tokens[0].TokenKind)
	assert.False(t, tokens[0].IsExpired)
	assert.True(t, tokens[1].IsExpired)
}

func TestAuthHandlerGRPCClient_ListCachedTokens_Error(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		listCachedTokensErr: fmt.Errorf("list error"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	_, err := client.ListCachedTokens(context.Background(), "azure")
	require.Error(t, err)
}

func TestAuthHandlerGRPCClient_PurgeExpiredTokens_Success(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		purgeExpiredTokensResp: &proto.PurgeExpiredTokensResponse{PurgedCount: 5},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	count, err := client.PurgeExpiredTokens(context.Background(), "azure")
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}

func TestAuthHandlerGRPCClient_PurgeExpiredTokens_Error(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		purgeExpiredTokensErr: fmt.Errorf("purge error"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	_, err := client.PurgeExpiredTokens(context.Background(), "azure")
	require.Error(t, err)
}

func TestAuthHandlerGRPCClient_ConfigureAuthHandler_Success(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		configureAuthHandlerResp: &proto.ConfigureAuthHandlerResponse{
			ProtocolVersion: PluginProtocolVersion,
		},
	}
	client := &AuthHandlerGRPCClient{client: mock, hostServiceID: 42}

	err := client.ConfigureAuthHandler(context.Background(), "azure", ProviderConfig{
		Quiet:      true,
		NoColor:    true,
		BinaryName: "mycli",
	})
	require.NoError(t, err)
}

func TestAuthHandlerGRPCClient_ConfigureAuthHandler_ResponseError(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		configureAuthHandlerResp: &proto.ConfigureAuthHandlerResponse{
			Error: "configuration rejected",
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.ConfigureAuthHandler(context.Background(), "azure", ProviderConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration rejected")
}

func TestAuthHandlerGRPCClient_ConfigureAuthHandler_Unimplemented(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		configureAuthHandlerErr: status.Error(codes.Unimplemented, "not supported"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.ConfigureAuthHandler(context.Background(), "azure", ProviderConfig{})
	require.NoError(t, err) // should swallow Unimplemented
}

func TestAuthHandlerGRPCClient_ConfigureAuthHandler_OtherGRPCError(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		configureAuthHandlerErr: status.Error(codes.Internal, "server error"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.ConfigureAuthHandler(context.Background(), "azure", ProviderConfig{})
	require.Error(t, err)
}

func TestAuthHandlerGRPCClient_ConfigureAuthHandler_WithSettings(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		configureAuthHandlerResp: &proto.ConfigureAuthHandlerResponse{
			ProtocolVersion: PluginProtocolVersion,
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.ConfigureAuthHandler(context.Background(), "azure", ProviderConfig{
		Settings: map[string]json.RawMessage{
			"timeout": json.RawMessage(`30`),
			"region":  json.RawMessage(`"us-west-2"`),
		},
	})
	require.NoError(t, err)
}

func TestAuthHandlerGRPCClient_StopAuthHandler_Success(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		stopAuthHandlerResp: &proto.StopAuthHandlerResponse{},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.StopAuthHandler(context.Background(), "azure")
	require.NoError(t, err)
}

func TestAuthHandlerGRPCClient_StopAuthHandler_ResponseError(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		stopAuthHandlerResp: &proto.StopAuthHandlerResponse{
			Error: "stop failed: handler busy",
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.StopAuthHandler(context.Background(), "azure")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler busy")
}

func TestAuthHandlerGRPCClient_StopAuthHandler_Unimplemented(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		stopAuthHandlerErr: status.Error(codes.Unimplemented, "not supported"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.StopAuthHandler(context.Background(), "azure")
	require.NoError(t, err) // should swallow Unimplemented
}

func TestAuthHandlerGRPCClient_StopAuthHandler_OtherError(t *testing.T) {
	t.Parallel()

	mock := &mockAuthHandlerServiceClient{
		stopAuthHandlerErr: errors.New("transport error"),
	}
	client := &AuthHandlerGRPCClient{client: mock}

	err := client.StopAuthHandler(context.Background(), "azure")
	require.Error(t, err)
}

// =====================================================================
// AuthHandlerGRPCServer tests for uncovered server methods
// =====================================================================

func TestAuthHandlerGRPCServer_GetAuthHandlers_Error(t *testing.T) {
	t.Parallel()

	errPlugin := &errAuthPlugin{err: fmt.Errorf("plugin error")}
	server := &AuthHandlerGRPCServer{Impl: errPlugin}

	_, err := server.GetAuthHandlers(context.Background(), &proto.GetAuthHandlersRequest{})
	require.Error(t, err)
}

func TestAuthHandlerGRPCServer_Logout_Success(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{}
	server := &AuthHandlerGRPCServer{Impl: mock}

	resp, err := server.Logout(context.Background(), &proto.LogoutRequest{HandlerName: "test"})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestAuthHandlerGRPCServer_Logout_Error(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		logoutFunc: func(_ context.Context, _ string) error {
			return fmt.Errorf("logout error")
		},
	}
	server := &AuthHandlerGRPCServer{Impl: mock}

	_, err := server.Logout(context.Background(), &proto.LogoutRequest{HandlerName: "test"})
	require.Error(t, err)
}

func TestAuthHandlerGRPCServer_GetStatus_Error(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		statusFunc: func(_ context.Context, _ string) (*auth.Status, error) {
			return nil, fmt.Errorf("status error")
		},
	}
	server := &AuthHandlerGRPCServer{Impl: mock}

	_, err := server.GetStatus(context.Background(), &proto.GetStatusRequest{HandlerName: "test"})
	require.Error(t, err)
}

func TestAuthHandlerGRPCServer_GetToken_Error(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		tokenFunc: func(_ context.Context, _ string, _ TokenRequest) (*TokenResponse, error) {
			return nil, fmt.Errorf("token error")
		},
	}
	server := &AuthHandlerGRPCServer{Impl: mock}

	_, err := server.GetToken(context.Background(), &proto.GetTokenRequest{
		HandlerName: "test",
		Scope:       "read",
	})
	require.Error(t, err)
}

func TestAuthHandlerGRPCServer_ListCachedTokens_Error(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		listFunc: func(_ context.Context, _ string) ([]*auth.CachedTokenInfo, error) {
			return nil, fmt.Errorf("list error")
		},
	}
	server := &AuthHandlerGRPCServer{Impl: mock}

	_, err := server.ListCachedTokens(context.Background(), &proto.ListCachedTokensRequest{HandlerName: "test"})
	require.Error(t, err)
}

func TestAuthHandlerGRPCServer_PurgeExpiredTokens_Error(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		purgeFunc: func(_ context.Context, _ string) (int, error) {
			return 0, fmt.Errorf("purge error")
		},
	}
	server := &AuthHandlerGRPCServer{Impl: mock}

	_, err := server.PurgeExpiredTokens(context.Background(), &proto.PurgeExpiredTokensRequest{HandlerName: "test"})
	require.Error(t, err)
}

// =====================================================================
// AuthHandlerClient delegation tests
// =====================================================================

func TestAuthHandlerClient_Delegation(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{
		plugin: mock,
		name:   "test-plugin",
		path:   "/usr/local/bin/test-plugin",
	}

	// GetAuthHandlers
	handlers, err := client.GetAuthHandlers(context.Background())
	require.NoError(t, err)
	assert.Len(t, handlers, 1)

	// Login
	resp, err := client.Login(context.Background(), "test-handler", LoginRequest{}, nil)
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", resp.Claims.Email)

	// Logout
	err = client.Logout(context.Background(), "test-handler")
	require.NoError(t, err)

	// GetStatus
	s, err := client.GetStatus(context.Background(), "test-handler")
	require.NoError(t, err)
	assert.True(t, s.Authenticated)

	// GetToken
	tok, err := client.GetToken(context.Background(), "test-handler", TokenRequest{Scope: "read"})
	require.NoError(t, err)
	assert.Equal(t, "test-access-token", tok.AccessToken)

	// ConfigureAuthHandler
	err = client.ConfigureAuthHandler(context.Background(), "test-handler", ProviderConfig{BinaryName: "cli"})
	require.NoError(t, err)

	// StopAuthHandler
	err = client.StopAuthHandler(context.Background(), "test-handler")
	require.NoError(t, err)

	// Name / Path
	assert.Equal(t, "test-plugin", client.Name())
	assert.Equal(t, "/usr/local/bin/test-plugin", client.Path())
}

func TestAuthHandlerClient_Kill_NilPluginClient(t *testing.T) {
	t.Parallel()

	client := &AuthHandlerClient{
		plugin: &MockAuthHandlerPlugin{},
	}
	// Should not panic with nil pluginClient
	client.Kill()
}

// =====================================================================
// NewAuthHandlerWrapper tests
// =====================================================================

func TestNewAuthHandlerWrapper(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{}
	ahClient := &AuthHandlerClient{
		plugin: mock,
		name:   "test-plugin",
	}
	info := AuthHandlerInfo{
		Name:         "azure",
		DisplayName:  "Azure AD",
		Flows:        []auth.Flow{auth.FlowDeviceCode},
		Capabilities: []auth.Capability{auth.CapScopesOnLogin},
	}

	wrapper := NewAuthHandlerWrapper(ahClient, info)
	require.NotNil(t, wrapper)

	assert.Equal(t, "azure", wrapper.Name())
	assert.Equal(t, "Azure AD", wrapper.DisplayName())
	assert.Equal(t, []auth.Flow{auth.FlowDeviceCode}, wrapper.SupportedFlows())
	assert.Equal(t, []auth.Capability{auth.CapScopesOnLogin}, wrapper.Capabilities())
	assert.Equal(t, ahClient, wrapper.Client())
}

// =====================================================================
// configureAndRegisterAuthHandlers tests
// =====================================================================

func TestConfigureAndRegisterAuthHandlers_WithConfig(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{}
	ahClient := &AuthHandlerClient{
		plugin: mock,
		name:   "test-plugin",
	}
	registry := auth.NewRegistry()
	handlers := []AuthHandlerInfo{
		{
			Name:        "handler-a",
			DisplayName: "Handler A",
			Flows:       []auth.Flow{auth.FlowDeviceCode},
		},
		{
			Name:        "handler-b",
			DisplayName: "Handler B",
			Flows:       []auth.Flow{auth.FlowServicePrincipal},
		},
	}
	cfg := &ProviderConfig{
		Quiet:      true,
		BinaryName: "mycli",
	}

	configureAndRegisterAuthHandlers(context.Background(), registry, ahClient, handlers, cfg)

	// Verify both handlers are registered
	h, err := registry.Get("handler-a")
	require.NoError(t, err)
	assert.Equal(t, "handler-a", h.Name())

	h, err = registry.Get("handler-b")
	require.NoError(t, err)
	assert.Equal(t, "handler-b", h.Name())
}

func TestConfigureAndRegisterAuthHandlers_NilConfig(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{}
	ahClient := &AuthHandlerClient{
		plugin: mock,
		name:   "test-plugin",
	}
	registry := auth.NewRegistry()
	handlers := []AuthHandlerInfo{
		{
			Name:        "handler-a",
			DisplayName: "Handler A",
		},
	}

	configureAndRegisterAuthHandlers(context.Background(), registry, ahClient, handlers, nil)

	h, err := registry.Get("handler-a")
	require.NoError(t, err)
	assert.Equal(t, "handler-a", h.Name())
}

func TestConfigureAndRegisterAuthHandlers_DuplicateSkipped(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{}
	ahClient := &AuthHandlerClient{
		plugin: mock,
		name:   "test-plugin",
	}
	registry := auth.NewRegistry()

	// Pre-register a handler
	preWrapper := NewAuthHandlerWrapper(ahClient, AuthHandlerInfo{Name: "handler-a", DisplayName: "Pre"})
	require.NoError(t, registry.Register(preWrapper))

	// Try to register duplicate
	handlers := []AuthHandlerInfo{
		{Name: "handler-a", DisplayName: "Dup"},
	}
	configureAndRegisterAuthHandlers(context.Background(), registry, ahClient, handlers, nil)

	// Original should still be there
	h, err := registry.Get("handler-a")
	require.NoError(t, err)
	assert.Equal(t, "Pre", h.DisplayName())
}

func TestConfigureAndRegisterAuthHandlers_ConfigureError(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		configureErr: fmt.Errorf("configure failed"),
	}
	ahClient := &AuthHandlerClient{
		plugin: mock,
		name:   "test-plugin",
	}
	registry := auth.NewRegistry()
	handlers := []AuthHandlerInfo{
		{Name: "handler-a", DisplayName: "A"},
	}
	cfg := &ProviderConfig{BinaryName: "cli"}

	// Should log error but still register the handler
	configureAndRegisterAuthHandlers(context.Background(), registry, ahClient, handlers, cfg)

	h, err := registry.Get("handler-a")
	require.NoError(t, err)
	assert.Equal(t, "handler-a", h.Name())
}

// =====================================================================
// KillAll / KillAllAuthHandlers additional tests
// =====================================================================

func TestKillAll_WithNilEntries(t *testing.T) {
	t.Parallel()

	clients := []*Client{nil, nil}
	KillAll(clients) // should not panic
}

func TestKillAllAuthHandlers_WithNilEntries(t *testing.T) {
	t.Parallel()

	clients := []*AuthHandlerClient{nil, nil}
	KillAllAuthHandlers(clients) // should not panic
}

func TestKillAllAuthHandlers_WithMockClients(t *testing.T) {
	t.Parallel()

	clients := []*AuthHandlerClient{
		{plugin: &MockAuthHandlerPlugin{}, name: "a"},
		{plugin: &MockAuthHandlerPlugin{}, name: "b"},
	}
	KillAllAuthHandlers(clients) // should not panic
}

// =====================================================================
// AuthHandlerWrapper error paths
// =====================================================================

func TestAuthHandlerWrapper_Login_Error(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		loginFunc: func(_ context.Context, _ string, _ LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
			return nil, fmt.Errorf("login failed")
		},
	}
	w := &AuthHandlerWrapper{
		client:      &AuthHandlerClient{plugin: mock, name: "p"},
		handlerName: "h",
		info:        AuthHandlerInfo{Name: "h"},
	}

	_, err := w.Login(context.Background(), auth.LoginOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "login failed")
}

func TestAuthHandlerWrapper_GetToken_Error(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		tokenFunc: func(_ context.Context, _ string, _ TokenRequest) (*TokenResponse, error) {
			return nil, fmt.Errorf("token error")
		},
	}
	w := &AuthHandlerWrapper{
		client:      &AuthHandlerClient{plugin: mock, name: "p"},
		handlerName: "h",
		info:        AuthHandlerInfo{Name: "h"},
	}

	_, err := w.GetToken(context.Background(), auth.TokenOptions{Scope: "read"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token error")
}

func TestAuthHandlerWrapper_InjectAuth_Error(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		tokenFunc: func(_ context.Context, _ string, _ TokenRequest) (*TokenResponse, error) {
			return nil, fmt.Errorf("inject token error")
		},
	}
	w := &AuthHandlerWrapper{
		client:      &AuthHandlerClient{plugin: mock, name: "p"},
		handlerName: "h",
		info:        AuthHandlerInfo{Name: "h"},
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.com", nil)
	err := w.InjectAuth(context.Background(), req, auth.TokenOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inject-auth")
}

func TestAuthHandlerWrapper_InjectAuth_EmptyTokenType(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{
		tokenFunc: func(_ context.Context, _ string, _ TokenRequest) (*TokenResponse, error) {
			return &TokenResponse{
				AccessToken: "my-token",
				TokenType:   "", // empty
			}, nil
		},
	}
	w := &AuthHandlerWrapper{
		client:      &AuthHandlerClient{plugin: mock, name: "p"},
		handlerName: "h",
		info:        AuthHandlerInfo{Name: "h"},
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.com", nil)
	err := w.InjectAuth(context.Background(), req, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Bearer my-token", req.Header.Get("Authorization"))
}

// =====================================================================
// errAuthPlugin helper for error-path server tests
// =====================================================================

type errAuthPlugin struct {
	err error
}

func (e *errAuthPlugin) GetAuthHandlers(_ context.Context) ([]AuthHandlerInfo, error) {
	return nil, e.err
}

func (e *errAuthPlugin) Login(_ context.Context, _ string, _ LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
	return nil, e.err
}

func (e *errAuthPlugin) Logout(_ context.Context, _ string) error {
	return e.err
}

func (e *errAuthPlugin) GetStatus(_ context.Context, _ string) (*auth.Status, error) {
	return nil, e.err
}

func (e *errAuthPlugin) GetToken(_ context.Context, _ string, _ TokenRequest) (*TokenResponse, error) {
	return nil, e.err
}

func (e *errAuthPlugin) ListCachedTokens(_ context.Context, _ string) ([]*auth.CachedTokenInfo, error) {
	return nil, e.err
}

func (e *errAuthPlugin) PurgeExpiredTokens(_ context.Context, _ string) (int, error) {
	return 0, e.err
}

func (e *errAuthPlugin) ConfigureAuthHandler(_ context.Context, _ string, _ ProviderConfig) error {
	return e.err
}

func (e *errAuthPlugin) StopAuthHandler(_ context.Context, _ string) error {
	return e.err
}
