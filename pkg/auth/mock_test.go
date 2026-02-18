// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockHandler_Basics(t *testing.T) {
	mock := NewMockHandler("entra")
	assert.Equal(t, "entra", mock.Name())
	assert.Equal(t, "entra", mock.DisplayName())
	assert.Contains(t, mock.SupportedFlows(), FlowDeviceCode)
	assert.Empty(t, mock.Capabilities()) // Default: no capabilities

	// Set capabilities
	mock.CapabilitiesValue = []Capability{CapScopesOnLogin, CapScopesOnTokenRequest}
	assert.Len(t, mock.Capabilities(), 2)
	assert.True(t, HasCapability(mock.Capabilities(), CapScopesOnLogin))
	assert.True(t, HasCapability(mock.Capabilities(), CapScopesOnTokenRequest))
	assert.False(t, HasCapability(mock.Capabilities(), CapHostname))
}

func TestMockHandler_Login(t *testing.T) {
	mock := NewMockHandler("entra")
	mock.LoginResult = &Result{Claims: &Claims{Email: "test@example.com"}}

	result, err := mock.Login(context.Background(), LoginOptions{TenantID: "tenant-123"})
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", result.Claims.Email)
	assert.Len(t, mock.LoginCalls, 1)
}

func TestMockHandler_Logout(t *testing.T) {
	mock := NewMockHandler("entra")
	require.NoError(t, mock.Logout(context.Background()))
	assert.Equal(t, 1, mock.LogoutCalls)
}

func TestMockHandler_Status(t *testing.T) {
	mock := NewMockHandler("entra")

	status, _ := mock.Status(context.Background())
	assert.False(t, status.Authenticated)

	mock.SetAuthenticated(&Claims{Email: "test@example.com"})
	status, _ = mock.Status(context.Background())
	assert.True(t, status.Authenticated)
}

func TestMockHandler_GetToken(t *testing.T) {
	mock := NewMockHandler("entra")
	mock.SetToken(&Token{AccessToken: "test-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})

	token, err := mock.GetToken(context.Background(), TokenOptions{Scope: "test-scope"})
	require.NoError(t, err)
	assert.Equal(t, "test-token", token.AccessToken)
}

func TestMockHandler_InjectAuth(t *testing.T) {
	mock := NewMockHandler("entra")
	mock.SetToken(&Token{AccessToken: "test-token", TokenType: "Bearer"})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/api", nil)
	require.NoError(t, mock.InjectAuth(context.Background(), req, TokenOptions{}))
	assert.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
}

func TestMockHandler_Reset(t *testing.T) {
	mock := NewMockHandler("entra")

	mock.Login(context.Background(), LoginOptions{})
	mock.Logout(context.Background())
	mock.Status(context.Background())
	mock.GetToken(context.Background(), TokenOptions{})

	mock.Reset()

	assert.Nil(t, mock.LoginCalls)
	assert.Equal(t, 0, mock.LogoutCalls)
	assert.Equal(t, 0, mock.StatusCalls)
	assert.Nil(t, mock.GetTokenCalls)
}

func TestMockHandler_ImplementsInterface(t *testing.T) {
	var handler Handler = NewMockHandler("test")
	assert.NotNil(t, handler)
}
