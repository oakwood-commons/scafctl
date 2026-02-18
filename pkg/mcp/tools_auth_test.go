// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleAuthStatus(t *testing.T) {
	t.Run("no auth registry", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		srv.authReg = nil

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_status"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleAuthStatus(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No auth registry configured")
	})

	t.Run("empty registry", func(t *testing.T) {
		reg := auth.NewRegistry()
		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_status"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleAuthStatus(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No auth handlers registered")
	})

	t.Run("with authenticated provider", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock := auth.NewMockHandler("test-provider")
		mock.DisplayNameValue = "Test Provider"
		mock.SetAuthenticated(&auth.Claims{
			Email:    "test@example.com",
			Username: "testuser",
		})
		mock.StatusResult.ExpiresAt = time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
		mock.StatusResult.IdentityType = auth.IdentityTypeUser
		require.NoError(t, reg.Register(mock))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_status"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleAuthStatus(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Handlers []authHandlerStatus `json:"handlers"`
			Count    int                 `json:"count"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 1, parsed.Count)
		assert.Equal(t, "test-provider", parsed.Handlers[0].Name)
		assert.True(t, parsed.Handlers[0].Authenticated)
		assert.Equal(t, "test@example.com", parsed.Handlers[0].Email)
		assert.Equal(t, "testuser", parsed.Handlers[0].Username)
		assert.Equal(t, "user", parsed.Handlers[0].IdentityType)
	})

	t.Run("with unauthenticated provider", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock := auth.NewMockHandler("azure")
		mock.SetNotAuthenticated()
		require.NoError(t, reg.Register(mock))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_status"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleAuthStatus(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Handlers []authHandlerStatus `json:"handlers"`
			Count    int                 `json:"count"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 1, parsed.Count)
		assert.False(t, parsed.Handlers[0].Authenticated)
	})

	t.Run("with status error", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock := auth.NewMockHandler("failing-provider")
		mock.StatusErr = fmt.Errorf("token expired")
		mock.StatusResult = nil
		require.NoError(t, reg.Register(mock))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_status"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleAuthStatus(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Handlers []authHandlerStatus `json:"handlers"`
			Count    int                 `json:"count"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 1, parsed.Count)
		assert.Contains(t, parsed.Handlers[0].Error, "token expired")
	})

	t.Run("multiple providers sorted", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock1 := auth.NewMockHandler("zzz-last")
		mock1.SetNotAuthenticated()
		mock2 := auth.NewMockHandler("aaa-first")
		mock2.SetAuthenticated(&auth.Claims{Email: "first@test.com"})
		require.NoError(t, reg.Register(mock1))
		require.NoError(t, reg.Register(mock2))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_status"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleAuthStatus(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Handlers []authHandlerStatus `json:"handlers"`
			Count    int                 `json:"count"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 2, parsed.Count)
		// Should be sorted alphabetically
		assert.Equal(t, "aaa-first", parsed.Handlers[0].Name)
		assert.Equal(t, "zzz-last", parsed.Handlers[1].Name)
	})

	t.Run("with expired token", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock := auth.NewMockHandler("expired-provider")
		mock.DisplayNameValue = "Expired Provider"
		mock.SetAuthenticated(&auth.Claims{
			Email:    "expired@example.com",
			Username: "expireduser",
		})
		mock.StatusResult.ExpiresAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		mock.StatusResult.IdentityType = auth.IdentityTypeUser
		require.NoError(t, reg.Register(mock))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_status"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleAuthStatus(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Handlers []authHandlerStatus `json:"handlers"`
			Count    int                 `json:"count"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 1, parsed.Count)
		assert.True(t, parsed.Handlers[0].Authenticated)
		assert.Contains(t, parsed.Handlers[0].ExpiresAt, "2020-01-01")
		assert.Equal(t, "expired@example.com", parsed.Handlers[0].Email)
	})

	t.Run("with capabilities and flows", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock := auth.NewMockHandler("full-provider")
		mock.FlowsValue = []auth.Flow{auth.FlowDeviceCode, auth.FlowInteractive}
		mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin}
		mock.SetAuthenticated(&auth.Claims{Email: "full@example.com"})
		require.NoError(t, reg.Register(mock))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_status"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleAuthStatus(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Handlers []authHandlerStatus `json:"handlers"`
			Count    int                 `json:"count"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 1, parsed.Count)
		assert.NotEmpty(t, parsed.Handlers[0].Flows)
		assert.NotEmpty(t, parsed.Handlers[0].Capabilities)
	})
}
