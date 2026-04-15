// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockAuthHandlerPlugin implements AuthHandlerPlugin for testing.
type MockAuthHandlerPlugin struct {
	handlers     []AuthHandlerInfo
	loginFunc    func(ctx context.Context, name string, req LoginRequest, cb func(DeviceCodePrompt)) (*LoginResponse, error)
	logoutFunc   func(ctx context.Context, name string) error
	statusFunc   func(ctx context.Context, name string) (*auth.Status, error)
	tokenFunc    func(ctx context.Context, name string, req TokenRequest) (*TokenResponse, error)
	listFunc     func(ctx context.Context, name string) ([]*auth.CachedTokenInfo, error)
	purgeFunc    func(ctx context.Context, name string) (int, error)
	configureErr error
	lastConfig   *ProviderConfig
	stopErr      error
	stopCalled   bool
	stopHandler  string
}

func (m *MockAuthHandlerPlugin) GetAuthHandlers(ctx context.Context) ([]AuthHandlerInfo, error) {
	if m.handlers != nil {
		return m.handlers, nil
	}
	return []AuthHandlerInfo{
		{
			Name:        "test-handler",
			DisplayName: "Test Auth Handler",
			Flows:       []auth.Flow{auth.FlowDeviceCode, auth.FlowServicePrincipal},
			Capabilities: []auth.Capability{
				auth.CapScopesOnLogin,
				auth.CapTenantID,
			},
		},
	}, nil
}

func (m *MockAuthHandlerPlugin) Login(ctx context.Context, handlerName string, req LoginRequest, cb func(DeviceCodePrompt)) (*LoginResponse, error) {
	if m.loginFunc != nil {
		return m.loginFunc(ctx, handlerName, req, cb)
	}
	return &LoginResponse{
		Claims: &auth.Claims{
			Issuer:    "https://test.example.com",
			Subject:   "user-123",
			Email:     "test@example.com",
			Name:      "Test User",
			ExpiresAt: time.Now().Add(1 * time.Hour),
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}, nil
}

func (m *MockAuthHandlerPlugin) Logout(ctx context.Context, handlerName string) error {
	if m.logoutFunc != nil {
		return m.logoutFunc(ctx, handlerName)
	}
	return nil
}

func (m *MockAuthHandlerPlugin) GetStatus(ctx context.Context, handlerName string) (*auth.Status, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx, handlerName)
	}
	return &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Email: "test@example.com",
			Name:  "Test User",
		},
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		TenantID:     "tenant-abc",
		IdentityType: auth.IdentityTypeUser,
		Scopes:       []string{"read", "write"},
	}, nil
}

func (m *MockAuthHandlerPlugin) GetToken(ctx context.Context, handlerName string, req TokenRequest) (*TokenResponse, error) {
	if m.tokenFunc != nil {
		return m.tokenFunc(ctx, handlerName, req)
	}
	return &TokenResponse{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       req.Scope,
		Flow:        auth.FlowDeviceCode,
		SessionID:   "session-123",
	}, nil
}

func (m *MockAuthHandlerPlugin) ListCachedTokens(ctx context.Context, handlerName string) ([]*auth.CachedTokenInfo, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, handlerName)
	}
	return []*auth.CachedTokenInfo{
		{
			Handler:   handlerName,
			TokenKind: "access",
			Scope:     "read",
			TokenType: "Bearer",
			Flow:      auth.FlowDeviceCode,
			ExpiresAt: time.Now().Add(1 * time.Hour),
			IsExpired: false,
			SessionID: "session-123",
		},
	}, nil
}

func (m *MockAuthHandlerPlugin) PurgeExpiredTokens(ctx context.Context, handlerName string) (int, error) {
	if m.purgeFunc != nil {
		return m.purgeFunc(ctx, handlerName)
	}
	return 0, nil
}

//nolint:revive // all params required by interface
func (m *MockAuthHandlerPlugin) ConfigureAuthHandler(_ context.Context, _ string, cfg ProviderConfig) error {
	m.lastConfig = &cfg
	return m.configureErr
}

//nolint:revive // all params required by interface
func (m *MockAuthHandlerPlugin) StopAuthHandler(_ context.Context, handlerName string) error {
	m.stopCalled = true
	m.stopHandler = handlerName
	return m.stopErr
}

func TestAuthHandlerGRPC_GetAuthHandlers(t *testing.T) {
	mock := &MockAuthHandlerPlugin{}

	handlers, err := mock.GetAuthHandlers(context.Background())
	require.NoError(t, err)
	require.Len(t, handlers, 1)

	h := handlers[0]
	assert.Equal(t, "test-handler", h.Name)
	assert.Equal(t, "Test Auth Handler", h.DisplayName)
	assert.Equal(t, []auth.Flow{auth.FlowDeviceCode, auth.FlowServicePrincipal}, h.Flows)
	assert.Equal(t, []auth.Capability{auth.CapScopesOnLogin, auth.CapTenantID}, h.Capabilities)
}

func TestAuthHandlerGRPC_Login(t *testing.T) {
	mock := &MockAuthHandlerPlugin{}

	resp, err := mock.Login(context.Background(), "test-handler", LoginRequest{
		TenantID: "tenant-abc",
		Scopes:   []string{"read"},
		Flow:     auth.FlowDeviceCode,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", resp.Claims.Email)
	assert.False(t, resp.ExpiresAt.IsZero())
}

func TestAuthHandlerGRPC_LoginWithDeviceCode(t *testing.T) {
	mock := &MockAuthHandlerPlugin{
		loginFunc: func(_ context.Context, _ string, _ LoginRequest, cb func(DeviceCodePrompt)) (*LoginResponse, error) {
			// Simulate device code flow: send a prompt, then return result.
			if cb != nil {
				cb(DeviceCodePrompt{
					UserCode:        "ABCD-1234",
					VerificationURI: "https://example.com/device",
					Message:         "Enter code ABCD-1234",
				})
			}
			return &LoginResponse{
				Claims: &auth.Claims{
					Email: "test@example.com",
				},
				ExpiresAt: time.Now().Add(1 * time.Hour),
			}, nil
		},
	}

	var receivedPrompt *DeviceCodePrompt
	cb := func(p DeviceCodePrompt) {
		receivedPrompt = &p
	}

	resp, err := mock.Login(context.Background(), "test-handler", LoginRequest{
		Flow: auth.FlowDeviceCode,
	}, cb)
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", resp.Claims.Email)
	require.NotNil(t, receivedPrompt)
	assert.Equal(t, "ABCD-1234", receivedPrompt.UserCode)
	assert.Equal(t, "https://example.com/device", receivedPrompt.VerificationURI)
}

func TestAuthHandlerGRPC_GetStatus(t *testing.T) {
	mock := &MockAuthHandlerPlugin{}

	status, err := mock.GetStatus(context.Background(), "test-handler")
	require.NoError(t, err)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "test@example.com", status.Claims.Email)
	assert.Equal(t, "tenant-abc", status.TenantID)
	assert.Equal(t, auth.IdentityTypeUser, status.IdentityType)
	assert.Equal(t, []string{"read", "write"}, status.Scopes)
}

func TestAuthHandlerGRPC_GetToken(t *testing.T) {
	mock := &MockAuthHandlerPlugin{}

	token, err := mock.GetToken(context.Background(), "test-handler", TokenRequest{
		Scope:       "read",
		MinValidFor: 60 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "test-access-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.Equal(t, "read", token.Scope)
	assert.Equal(t, auth.FlowDeviceCode, token.Flow)
	assert.Equal(t, "session-123", token.SessionID)
}

func TestAuthHandlerGRPC_ListCachedTokens(t *testing.T) {
	mock := &MockAuthHandlerPlugin{}

	tokens, err := mock.ListCachedTokens(context.Background(), "test-handler")
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.Equal(t, "test-handler", tokens[0].Handler)
	assert.Equal(t, "access", tokens[0].TokenKind)
	assert.Equal(t, "read", tokens[0].Scope)
	assert.False(t, tokens[0].IsExpired)
}

func TestAuthHandlerGRPC_PurgeExpiredTokens(t *testing.T) {
	mock := &MockAuthHandlerPlugin{
		purgeFunc: func(_ context.Context, _ string) (int, error) {
			return 3, nil
		},
	}

	count, err := mock.PurgeExpiredTokens(context.Background(), "test-handler")
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

// TestAuthHandlerClaimsConversion tests Claims roundtrip through proto conversion.
func TestAuthHandlerClaimsConversion(t *testing.T) {
	original := &auth.Claims{
		Issuer:    "https://login.example.com",
		Subject:   "user-abc",
		TenantID:  "tenant-123",
		ObjectID:  "obj-456",
		ClientID:  "client-789",
		Email:     "user@example.com",
		Name:      "Example User",
		Username:  "exampleuser",
		IssuedAt:  time.Unix(1700000000, 0),
		ExpiresAt: time.Unix(1700003600, 0),
	}

	// Convert to proto and back
	protoClaims := claimsToProto(original)
	converted := protoToClaims(protoClaims)

	assert.Equal(t, original.Issuer, converted.Issuer)
	assert.Equal(t, original.Subject, converted.Subject)
	assert.Equal(t, original.TenantID, converted.TenantID)
	assert.Equal(t, original.ObjectID, converted.ObjectID)
	assert.Equal(t, original.ClientID, converted.ClientID)
	assert.Equal(t, original.Email, converted.Email)
	assert.Equal(t, original.Name, converted.Name)
	assert.Equal(t, original.Username, converted.Username)
	assert.Equal(t, original.IssuedAt.Unix(), converted.IssuedAt.Unix())
	assert.Equal(t, original.ExpiresAt.Unix(), converted.ExpiresAt.Unix())
}

// TestAuthHandlerStatusConversion tests Status roundtrip through proto conversion.
func TestAuthHandlerStatusConversion(t *testing.T) {
	original := &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Email: "user@example.com",
			Name:  "Test User",
		},
		ExpiresAt:    time.Unix(1700003600, 0),
		LastRefresh:  time.Unix(1700000000, 0),
		TenantID:     "tenant-abc",
		IdentityType: auth.IdentityTypeServicePrincipal,
		ClientID:     "client-xyz",
		TokenFile:    "/tmp/token",
		Scopes:       []string{"read", "write", "admin"},
	}

	protoStatus := statusToProto(original)
	converted := protoToStatus(protoStatus)

	assert.Equal(t, original.Authenticated, converted.Authenticated)
	assert.Equal(t, original.Claims.Email, converted.Claims.Email)
	assert.Equal(t, original.ExpiresAt.Unix(), converted.ExpiresAt.Unix())
	assert.Equal(t, original.LastRefresh.Unix(), converted.LastRefresh.Unix())
	assert.Equal(t, original.TenantID, converted.TenantID)
	assert.Equal(t, original.IdentityType, converted.IdentityType)
	assert.Equal(t, original.ClientID, converted.ClientID)
	assert.Equal(t, original.TokenFile, converted.TokenFile)
	assert.Equal(t, original.Scopes, converted.Scopes)
}

// TestAuthHandlerTokenConversion tests TokenResponse roundtrip through proto conversion.
func TestAuthHandlerTokenConversion(t *testing.T) {
	original := &TokenResponse{
		AccessToken: "eyJ...",
		TokenType:   "Bearer",
		ExpiresAt:   time.Unix(1700003600, 0),
		Scope:       "read write",
		CachedAt:    time.Unix(1700000000, 0),
		Flow:        auth.FlowServicePrincipal,
		SessionID:   "session-abc",
	}

	protoToken := tokenResponseToProto(original)
	converted := protoToTokenResponse(protoToken)

	assert.Equal(t, original.AccessToken, converted.AccessToken)
	assert.Equal(t, original.TokenType, converted.TokenType)
	assert.Equal(t, original.ExpiresAt.Unix(), converted.ExpiresAt.Unix())
	assert.Equal(t, original.Scope, converted.Scope)
	assert.Equal(t, original.CachedAt.Unix(), converted.CachedAt.Unix())
	assert.Equal(t, original.Flow, converted.Flow)
	assert.Equal(t, original.SessionID, converted.SessionID)
}

// TestAuthHandlerCachedTokenInfoConversion tests CachedTokenInfo roundtrip.
func TestAuthHandlerCachedTokenInfoConversion(t *testing.T) {
	original := &auth.CachedTokenInfo{
		Handler:   "test-handler",
		TokenKind: "refresh",
		Scope:     "openid",
		TokenType: "Bearer",
		Flow:      auth.FlowWorkloadIdentity,
		ExpiresAt: time.Unix(1700003600, 0),
		CachedAt:  time.Unix(1700000000, 0),
		IsExpired: true,
		SessionID: "session-xyz",
	}

	protoInfo := cachedTokenInfoToProto(original)
	converted := protoToCachedTokenInfo(protoInfo)

	assert.Equal(t, original.Handler, converted.Handler)
	assert.Equal(t, original.TokenKind, converted.TokenKind)
	assert.Equal(t, original.Scope, converted.Scope)
	assert.Equal(t, original.TokenType, converted.TokenType)
	assert.Equal(t, original.Flow, converted.Flow)
	assert.Equal(t, original.ExpiresAt.Unix(), converted.ExpiresAt.Unix())
	assert.Equal(t, original.CachedAt.Unix(), converted.CachedAt.Unix())
	assert.Equal(t, original.IsExpired, converted.IsExpired)
	assert.Equal(t, original.SessionID, converted.SessionID)
}

// TestAuthHandlerInfoConversion tests AuthHandlerInfo roundtrip through gRPC server/client.
func TestAuthHandlerInfoConversion(t *testing.T) {
	original := []AuthHandlerInfo{
		{
			Name:         "okta",
			DisplayName:  "Okta SSO",
			Flows:        []auth.Flow{auth.FlowDeviceCode, auth.FlowInteractive},
			Capabilities: []auth.Capability{auth.CapScopesOnLogin, auth.CapScopesOnTokenRequest, auth.CapTenantID},
		},
		{
			Name:         "aws",
			DisplayName:  "AWS IAM",
			Flows:        []auth.Flow{auth.FlowServicePrincipal},
			Capabilities: []auth.Capability{auth.CapHostname},
		},
	}

	// Simulate server-side conversion: AuthHandlerInfo → proto
	protoHandlers := make([]*proto.AuthHandlerInfo, len(original))
	for i, h := range original {
		flows := make([]string, len(h.Flows))
		for j, f := range h.Flows {
			flows[j] = string(f)
		}
		caps := make([]string, len(h.Capabilities))
		for j, c := range h.Capabilities {
			caps[j] = string(c)
		}
		protoHandlers[i] = &proto.AuthHandlerInfo{
			Name:         h.Name,
			DisplayName:  h.DisplayName,
			Flows:        flows,
			Capabilities: caps,
		}
	}

	// Simulate client-side conversion: proto → AuthHandlerInfo
	converted := make([]AuthHandlerInfo, len(protoHandlers))
	for i, ph := range protoHandlers {
		flows := make([]auth.Flow, len(ph.Flows))
		for j, f := range ph.Flows {
			flows[j] = auth.Flow(f)
		}
		caps := make([]auth.Capability, len(ph.Capabilities))
		for j, c := range ph.Capabilities {
			caps[j] = auth.Capability(c)
		}
		converted[i] = AuthHandlerInfo{
			Name:         ph.Name,
			DisplayName:  ph.DisplayName,
			Flows:        flows,
			Capabilities: caps,
		}
	}

	require.Len(t, converted, 2)
	assert.Equal(t, original[0].Name, converted[0].Name)
	assert.Equal(t, original[0].DisplayName, converted[0].DisplayName)
	assert.Equal(t, original[0].Flows, converted[0].Flows)
	assert.Equal(t, original[0].Capabilities, converted[0].Capabilities)
	assert.Equal(t, original[1].Name, converted[1].Name)
	assert.Equal(t, original[1].Flows, converted[1].Flows)
}

// TestAuthHandlerWrapper_InjectAuth verifies the GetToken + header injection decomposition.
func TestAuthHandlerWrapper_InjectAuth(t *testing.T) {
	mock := &MockAuthHandlerPlugin{
		tokenFunc: func(_ context.Context, _ string, req TokenRequest) (*TokenResponse, error) {
			return &TokenResponse{
				AccessToken: "injected-token-abc",
				TokenType:   "Bearer",
				ExpiresAt:   time.Now().Add(1 * time.Hour),
				Scope:       req.Scope,
			}, nil
		},
	}

	// Create a wrapper around the mock (via an in-process client simulation).
	// Since we can't easily create an AuthHandlerClient without a real plugin process,
	// we test the InjectAuth logic directly on a wrapper with a mock.
	w := &AuthHandlerWrapper{
		client: &AuthHandlerClient{
			plugin: mock,
			name:   "test-plugin",
		},
		handlerName: "test-handler",
		info: AuthHandlerInfo{
			Name:        "test-handler",
			DisplayName: "Test Handler",
		},
	}

	// Create an HTTP request and inject auth.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.com/data", nil)
	require.NoError(t, err)

	err = w.InjectAuth(context.Background(), req, auth.TokenOptions{
		Scope: "api://test/.default",
	})
	require.NoError(t, err)

	// Verify the Authorization header was set.
	assert.Equal(t, "Bearer injected-token-abc", req.Header.Get("Authorization"))
}

// TestAuthHandlerWrapper_Interface verifies wrapper implements auth.Handler.
func TestAuthHandlerWrapper_Interface(t *testing.T) {
	mock := &MockAuthHandlerPlugin{}
	w := &AuthHandlerWrapper{
		client: &AuthHandlerClient{
			plugin: mock,
			name:   "test-plugin",
		},
		handlerName: "test-handler",
		info: AuthHandlerInfo{
			Name:         "test-handler",
			DisplayName:  "Test Auth Handler",
			Flows:        []auth.Flow{auth.FlowDeviceCode},
			Capabilities: []auth.Capability{auth.CapScopesOnLogin},
		},
	}

	// Verify static methods
	assert.Equal(t, "test-handler", w.Name())
	assert.Equal(t, "Test Auth Handler", w.DisplayName())
	assert.Equal(t, []auth.Flow{auth.FlowDeviceCode}, w.SupportedFlows())
	assert.Equal(t, []auth.Capability{auth.CapScopesOnLogin}, w.Capabilities())

	// Verify Login
	result, err := w.Login(context.Background(), auth.LoginOptions{
		Flow: auth.FlowDeviceCode,
	})
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", result.Claims.Email)

	// Verify Logout
	err = w.Logout(context.Background())
	require.NoError(t, err)

	// Verify Status
	status, err := w.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Authenticated)

	// Verify GetToken
	token, err := w.GetToken(context.Background(), auth.TokenOptions{Scope: "read"})
	require.NoError(t, err)
	assert.Equal(t, "test-access-token", token.AccessToken)

	// Verify ListCachedTokens (optional interface)
	tokens, err := w.ListCachedTokens(context.Background())
	require.NoError(t, err)
	assert.Len(t, tokens, 1)

	// Verify PurgeExpiredTokens (optional interface)
	count, err := w.PurgeExpiredTokens(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestNilClaimsConversion verifies nil handling in conversion helpers.
func TestNilClaimsConversion(t *testing.T) {
	assert.Nil(t, claimsToProto(nil))
	assert.Nil(t, protoToClaims(nil))
}

// ── ConfigureAuthHandler gRPC server/client tests ─────────────────────────────

func TestAuthHandlerGRPCServer_ConfigureAuthHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		configureErr error
		wantErr      string
	}{
		{
			name: "success",
		},
		{
			name:         "plugin returns error",
			configureErr: assert.AnError,
			wantErr:      assert.AnError.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &MockAuthHandlerPlugin{
				configureErr: tt.configureErr,
			}
			server := &AuthHandlerGRPCServer{Impl: mock}

			resp, err := server.ConfigureAuthHandler(context.Background(), &proto.ConfigureAuthHandlerRequest{
				HandlerName:     "test-handler",
				Quiet:           true,
				NoColor:         true,
				BinaryName:      "mycli",
				ProtocolVersion: PluginProtocolVersion,
				Settings: map[string][]byte{
					"timeout": []byte(`30`),
				},
			})
			require.NoError(t, err)

			if tt.wantErr != "" {
				assert.Contains(t, resp.Error, tt.wantErr)
				return
			}

			assert.Empty(t, resp.Error)
			assert.Equal(t, PluginProtocolVersion, resp.ProtocolVersion)
			require.NotNil(t, mock.lastConfig)
			assert.True(t, mock.lastConfig.Quiet)
			assert.True(t, mock.lastConfig.NoColor)
			assert.Equal(t, "mycli", mock.lastConfig.BinaryName)
			assert.Contains(t, mock.lastConfig.Settings, "timeout")
		})
	}
}

func TestAuthHandlerGRPCServer_ConfigureAuthHandler_NoBroker(t *testing.T) {
	t.Parallel()

	mock := &MockAuthHandlerPlugin{}
	server := &AuthHandlerGRPCServer{Impl: mock}

	resp, err := server.ConfigureAuthHandler(context.Background(), &proto.ConfigureAuthHandlerRequest{
		HandlerName:   "test-handler",
		HostServiceId: 99,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	// Without a broker, HostServiceID should not be passed through.
	require.NotNil(t, mock.lastConfig)
	assert.Equal(t, uint32(0), mock.lastConfig.HostServiceID)
}

// ── StopAuthHandler gRPC server/client tests ──────────────────────────────────

func TestAuthHandlerGRPCServer_StopAuthHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stopErr error
		wantErr string
	}{
		{
			name: "success",
		},
		{
			name:    "plugin returns error",
			stopErr: assert.AnError,
			wantErr: assert.AnError.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &MockAuthHandlerPlugin{
				stopErr: tt.stopErr,
			}
			server := &AuthHandlerGRPCServer{Impl: mock}

			resp, err := server.StopAuthHandler(context.Background(), &proto.StopAuthHandlerRequest{
				HandlerName: "test-handler",
			})
			require.NoError(t, err)

			if tt.wantErr != "" {
				assert.Contains(t, resp.Error, tt.wantErr)
				return
			}

			assert.Empty(t, resp.Error)
			assert.True(t, mock.stopCalled)
			assert.Equal(t, "test-handler", mock.stopHandler)
		})
	}
}

// ── KillAllAuthHandlers tests ─────────────────────────────────────────────────

func TestKillAllAuthHandlers_NilSlice(t *testing.T) {
	t.Parallel()
	KillAllAuthHandlers(nil)
}

func TestKillAllAuthHandlers_EmptySlice(t *testing.T) {
	t.Parallel()
	KillAllAuthHandlers([]*AuthHandlerClient{})
}

// ── AuthHandlerClient.HostServiceID tests ─────────────────────────────────────

func TestAuthHandlerClient_HostServiceID_NonGRPC(t *testing.T) {
	t.Parallel()

	// When the underlying plugin is not a GRPCClient, HostServiceID returns 0.
	client := &AuthHandlerClient{
		plugin: &MockAuthHandlerPlugin{},
	}
	assert.Equal(t, uint32(0), client.HostServiceID())
}

func TestAuthHandlerClient_HostServiceID_GRPCClient(t *testing.T) {
	t.Parallel()

	grpcClient := &AuthHandlerGRPCClient{
		hostServiceID: 42,
	}
	client := &AuthHandlerClient{
		plugin: grpcClient,
	}
	assert.Equal(t, uint32(42), client.HostServiceID())
}
