// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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

func TestHandleListCachedTokens(t *testing.T) {
	t.Run("no auth registry", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		srv.authReg = nil

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_list_tokens"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListCachedTokens(context.Background(), request)
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
		request.Params.Name = "auth_list_tokens"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListCachedTokens(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No auth handlers registered")
	})

	t.Run("with cached tokens", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock := auth.NewMockHandler("gitlab")
		mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
			{
				Handler:   "gitlab",
				TokenKind: "access",
				Scope:     "read_user",
				TokenType: "Bearer",
				Flow:      auth.FlowInteractive,
				ExpiresAt: time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
				CachedAt:  time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
				IsExpired: false,
			},
		}
		require.NoError(t, reg.Register(mock))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_list_tokens"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListCachedTokens(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Tokens []map[string]any `json:"tokens"`
			Count  int              `json:"count"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 1, parsed.Count)
		assert.Equal(t, "gitlab", parsed.Tokens[0]["handler"])
		assert.Equal(t, "read_user", parsed.Tokens[0]["scope"])
		assert.Equal(t, false, parsed.Tokens[0]["isExpired"])
	})

	t.Run("filter by handler", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock1 := auth.NewMockHandler("gitlab")
		mock1.ListCachedTokensResult = []*auth.CachedTokenInfo{
			{Handler: "gitlab", TokenKind: "access", Scope: "read_user"},
		}
		mock2 := auth.NewMockHandler("entra")
		mock2.ListCachedTokensResult = []*auth.CachedTokenInfo{
			{Handler: "entra", TokenKind: "access", Scope: "api://example/.default"},
		}
		require.NoError(t, reg.Register(mock1))
		require.NoError(t, reg.Register(mock2))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_list_tokens"
		request.Params.Arguments = map[string]any{"handler": "gitlab"}

		result, err := srv.handleListCachedTokens(context.Background(), request)
		require.NoError(t, err)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Tokens []map[string]any `json:"tokens"`
			Count  int              `json:"count"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 1, parsed.Count)
		assert.Equal(t, "gitlab", parsed.Tokens[0]["handler"])
	})
}

func TestHandlePurgeExpiredTokens(t *testing.T) {
	t.Run("no auth registry", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		srv.authReg = nil

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_purge_expired"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handlePurgeExpiredTokens(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No auth registry configured")
	})

	t.Run("purges expired tokens", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock := auth.NewMockHandler("entra")
		mock.PurgeExpiredResult = 3
		require.NoError(t, reg.Register(mock))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_purge_expired"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handlePurgeExpiredTokens(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Handlers    []map[string]any `json:"handlers"`
			TotalPurged int              `json:"totalPurged"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 3, parsed.TotalPurged)
		assert.Equal(t, "entra", parsed.Handlers[0]["handler"])
	})

	t.Run("filter by handler", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock1 := auth.NewMockHandler("entra")
		mock1.PurgeExpiredResult = 2
		mock2 := auth.NewMockHandler("github")
		mock2.PurgeExpiredResult = 5
		require.NoError(t, reg.Register(mock1))
		require.NoError(t, reg.Register(mock2))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_purge_expired"
		request.Params.Arguments = map[string]any{"handler": "entra"}

		result, err := srv.handlePurgeExpiredTokens(context.Background(), request)
		require.NoError(t, err)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Handlers    []map[string]any `json:"handlers"`
			TotalPurged int              `json:"totalPurged"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 2, parsed.TotalPurged)
		assert.Len(t, parsed.Handlers, 1)
		assert.Equal(t, "entra", parsed.Handlers[0]["handler"])
	})

	t.Run("skipped handler without TokenPurger", func(t *testing.T) {
		reg := auth.NewRegistry()
		minimal := &mcpMinimalHandler{name: "basic"}
		require.NoError(t, reg.Register(minimal))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_purge_expired"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handlePurgeExpiredTokens(context.Background(), request)
		require.NoError(t, err)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Handlers    []map[string]any `json:"handlers"`
			TotalPurged int              `json:"totalPurged"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 0, parsed.TotalPurged)
		assert.Len(t, parsed.Handlers, 1)
		assert.Equal(t, "basic", parsed.Handlers[0]["handler"])
		assert.Contains(t, parsed.Handlers[0]["status"], "does not support token purging")
	})
}

func TestHandleListCachedTokens_Warnings(t *testing.T) {
	t.Run("handler without TokenLister", func(t *testing.T) {
		reg := auth.NewRegistry()
		minimal := &mcpMinimalHandler{name: "basic"}
		require.NoError(t, reg.Register(minimal))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_list_tokens"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListCachedTokens(context.Background(), request)
		require.NoError(t, err)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Tokens   []map[string]any `json:"tokens"`
			Count    int              `json:"count"`
			Warnings []string         `json:"warnings"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 0, parsed.Count)
		require.Len(t, parsed.Warnings, 1)
		assert.Contains(t, parsed.Warnings[0], "does not support token listing")
	})

	t.Run("handler with ListCachedTokens error", func(t *testing.T) {
		reg := auth.NewRegistry()
		mock := auth.NewMockHandler("gitlab")
		mock.ListCachedTokensErr = fmt.Errorf("cache corrupted")
		require.NoError(t, reg.Register(mock))

		srv, err := NewServer(
			WithServerVersion("test"),
			WithServerAuthRegistry(reg),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_list_tokens"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListCachedTokens(context.Background(), request)
		require.NoError(t, err)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Tokens   []map[string]any `json:"tokens"`
			Count    int              `json:"count"`
			Warnings []string         `json:"warnings"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, 0, parsed.Count)
		require.Len(t, parsed.Warnings, 1)
		assert.Contains(t, parsed.Warnings[0], "cache corrupted")
	})
}

// mcpMinimalHandler implements only auth.Handler (not TokenLister or TokenPurger).
type mcpMinimalHandler struct {
	name string
}

func (m *mcpMinimalHandler) Name() string        { return m.name }
func (m *mcpMinimalHandler) DisplayName() string { return m.name }

func (m *mcpMinimalHandler) SupportedFlows() []auth.Flow {
	return []auth.Flow{auth.FlowDeviceCode}
}

func (m *mcpMinimalHandler) Capabilities() []auth.Capability { return nil }

func (m *mcpMinimalHandler) Login(_ context.Context, _ auth.LoginOptions) (*auth.Result, error) {
	return nil, nil
}

func (m *mcpMinimalHandler) Logout(_ context.Context) error { return nil }

func (m *mcpMinimalHandler) Status(_ context.Context) (*auth.Status, error) {
	return &auth.Status{}, nil
}

func (m *mcpMinimalHandler) GetToken(_ context.Context, _ auth.TokenOptions) (*auth.Token, error) {
	return nil, nil
}

func (m *mcpMinimalHandler) InjectAuth(_ context.Context, _ *http.Request, _ auth.TokenOptions) error {
	return nil
}

var _ auth.Handler = (*mcpMinimalHandler)(nil)

func TestProfileFromRequest(t *testing.T) {
	t.Run("empty args returns empty", func(t *testing.T) {
		profile, err := profileFromRequest(map[string]any{})
		assert.NoError(t, err)
		assert.Empty(t, profile)
	})

	t.Run("missing profile key returns empty", func(t *testing.T) {
		profile, err := profileFromRequest(map[string]any{"other": "value"})
		assert.NoError(t, err)
		assert.Empty(t, profile)
	})

	t.Run("empty string returns empty", func(t *testing.T) {
		profile, err := profileFromRequest(map[string]any{"profile": ""})
		assert.NoError(t, err)
		assert.Empty(t, profile)
	})

	t.Run("valid profile returns normalized", func(t *testing.T) {
		profile, err := profileFromRequest(map[string]any{"profile": "work"})
		assert.NoError(t, err)
		assert.Equal(t, "work", profile)
	})

	t.Run("built-in alias returns empty", func(t *testing.T) {
		profile, err := profileFromRequest(map[string]any{"profile": "built-in"})
		assert.NoError(t, err)
		assert.Empty(t, profile)
	})

	t.Run("default alias returns empty", func(t *testing.T) {
		profile, err := profileFromRequest(map[string]any{"profile": "default"})
		assert.NoError(t, err)
		assert.Empty(t, profile)
	})

	t.Run("invalid profile returns error", func(t *testing.T) {
		profile, err := profileFromRequest(map[string]any{"profile": "has spaces"})
		assert.Error(t, err)
		assert.Empty(t, profile)
		assert.Contains(t, err.Error(), "invalid profile")
	})

	t.Run("non-string type returns empty", func(t *testing.T) {
		profile, err := profileFromRequest(map[string]any{"profile": 42})
		assert.NoError(t, err)
		assert.Empty(t, profile)
	})
}

func TestHandleSetProfile(t *testing.T) {
	t.Run("sets profile", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_set_profile"
		request.Params.Arguments = map[string]any{"profile": "work"}

		result, err := srv.handleSetProfile(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "work")

		// Verify the override was stored.
		p := srv.profileOverride.Load().(*string)
		require.NotNil(t, p)
		assert.Equal(t, "work", *p)
	})

	t.Run("clears with empty string", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Set first.
		v := "work"
		srv.profileOverride.Store(&v)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_set_profile"
		request.Params.Arguments = map[string]any{"profile": ""}

		result, err := srv.handleSetProfile(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "default")

		p := srv.profileOverride.Load().(*string)
		assert.Nil(t, p)
	})

	t.Run("clears with default alias", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		v := "personal"
		srv.profileOverride.Store(&v)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_set_profile"
		request.Params.Arguments = map[string]any{"profile": "default"}

		result, err := srv.handleSetProfile(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		p := srv.profileOverride.Load().(*string)
		assert.Nil(t, p)
	})

	t.Run("rejects invalid profile", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "auth_set_profile"
		request.Params.Arguments = map[string]any{"profile": "has spaces"}

		result, err := srv.handleSetProfile(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "invalid profile")
	})
}

type mcpRecordingAuthPlugin struct {
	configuredProfile  string
	configuredClientID string
	lastProfile        string
	lastClientID       string
}

func (m *mcpRecordingAuthPlugin) GetAuthHandlers(_ context.Context) ([]plugin.AuthHandlerInfo, error) {
	return []plugin.AuthHandlerInfo{{Name: "entra", DisplayName: "Microsoft Entra ID"}}, nil
}

func (m *mcpRecordingAuthPlugin) ConfigureAuthHandler(_ context.Context, _ string, cfg plugin.ProviderConfig) error {
	m.configuredProfile = cfg.Profile
	m.configuredClientID = ""
	if raw, ok := cfg.Settings["entra"]; ok && len(raw) > 0 {
		var parsed struct {
			ClientID string `json:"clientId"`
		}
		_ = json.Unmarshal(raw, &parsed)
		m.configuredClientID = parsed.ClientID
	}
	return nil
}

func (m *mcpRecordingAuthPlugin) Login(_ context.Context, _ string, _ plugin.LoginRequest, _ func(plugin.DeviceCodePrompt)) (*plugin.LoginResponse, error) {
	return nil, nil
}

func (m *mcpRecordingAuthPlugin) Logout(_ context.Context, _ string, _ plugin.LogoutRequest) error {
	return nil
}

func (m *mcpRecordingAuthPlugin) GetStatus(ctx context.Context, _ string, req plugin.StatusRequest) (*auth.Status, error) {
	m.lastProfile = auth.ProfileFromContext(ctx)
	m.lastClientID = ""
	if raw, ok := req.Settings["clientId"]; ok {
		_ = json.Unmarshal(raw, &m.lastClientID)
	}
	return &auth.Status{
		Authenticated: true,
		ClientID:      m.lastClientID,
		Claims:        &auth.Claims{Email: m.lastClientID + "@example.com"},
	}, nil
}

func (m *mcpRecordingAuthPlugin) GetToken(_ context.Context, _ string, _ plugin.TokenRequest) (*plugin.TokenResponse, error) {
	return nil, nil
}

func (m *mcpRecordingAuthPlugin) ListCachedTokens(ctx context.Context, handlerName string) ([]*auth.CachedTokenInfo, error) {
	// Over real gRPC, ListCachedTokens carries Profile (not per-RPC Settings) and
	// relies on ConfigureAuthHandler for stateful profile settings.
	m.lastProfile = auth.ProfileFromContext(ctx)
	m.lastClientID = m.configuredClientID
	return []*auth.CachedTokenInfo{
		{
			Handler:   handlerName,
			TokenKind: "access",
			Scope:     "profile:" + auth.DisplayProfileName(m.lastProfile),
		},
	}, nil
}

func (m *mcpRecordingAuthPlugin) PurgeExpiredTokens(ctx context.Context, _ string) (int, error) {
	m.lastProfile = auth.ProfileFromContext(ctx)
	return 1, nil
}

func (m *mcpRecordingAuthPlugin) DetectAvailableFlows(_ context.Context, _ string) ([]plugin.FlowAvailability, error) {
	return nil, nil
}

func (m *mcpRecordingAuthPlugin) StopAuthHandler(_ context.Context, _ string) error {
	return nil
}

type mcpInProcAuthServiceClient struct {
	proto.AuthHandlerServiceClient
	server *plugin.AuthHandlerGRPCServer
}

func (c *mcpInProcAuthServiceClient) ConfigureAuthHandler(ctx context.Context, req *proto.ConfigureAuthHandlerRequest, _ ...grpc.CallOption) (*proto.ConfigureAuthHandlerResponse, error) {
	return c.server.ConfigureAuthHandler(ctx, req)
}

func (c *mcpInProcAuthServiceClient) GetStatus(ctx context.Context, req *proto.GetStatusRequest, _ ...grpc.CallOption) (*proto.GetStatusResponse, error) {
	return c.server.GetStatus(ctx, req)
}

func (c *mcpInProcAuthServiceClient) ListCachedTokens(ctx context.Context, req *proto.ListCachedTokensRequest, _ ...grpc.CallOption) (*proto.ListCachedTokensResponse, error) {
	return c.server.ListCachedTokens(ctx, req)
}

func (c *mcpInProcAuthServiceClient) PurgeExpiredTokens(ctx context.Context, req *proto.PurgeExpiredTokensRequest, _ ...grpc.CallOption) (*proto.PurgeExpiredTokensResponse, error) {
	return c.server.PurgeExpiredTokens(ctx, req)
}

func TestMCPAuthTools_PluginProfileResolution(t *testing.T) {
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
					"staging": {
						ClientID: "staging-client",
						TenantID: "staging-tenant",
					},
				},
			},
		},
	}

	fake := &mcpRecordingAuthPlugin{}
	grpcServer := &plugin.AuthHandlerGRPCServer{Impl: fake}
	grpcClient := plugin.NewAuthHandlerGRPCClient(&mcpInProcAuthServiceClient{server: grpcServer})
	wrapper := plugin.NewAuthHandlerWrapper(
		plugin.NewAuthHandlerClientFromPlugin(grpcClient),
		plugin.AuthHandlerInfo{Name: "entra", DisplayName: "Microsoft Entra ID"},
	)
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(wrapper))

	baseCtx := config.WithConfig(context.Background(), appCfg)
	srv, err := NewServer(
		WithServerVersion("test"),
		WithServerContext(baseCtx),
		WithServerAuthRegistry(reg),
	)
	require.NoError(t, err)
	srv.cfgReloader = nil

	t.Run("auth_status with profile=staging resolves staging settings", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = "auth_status"
		req.Params.Arguments = map[string]any{"profile": "staging"}

		res, err := srv.handleAuthStatus(srv.freshConfigContext(baseCtx), req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.Equal(t, "staging", fake.lastProfile)
		assert.Equal(t, "staging-client", fake.lastClientID)
	})

	t.Run("auth_status with profile=default overrides both config activeProfile and auth_set_profile", func(t *testing.T) {
		override := "work"
		srv.profileOverride.Store(&override)
		t.Cleanup(func() { srv.profileOverride.Store((*string)(nil)) })

		req := mcp.CallToolRequest{}
		req.Params.Name = "auth_status"
		req.Params.Arguments = map[string]any{"profile": "default"}

		res, err := srv.handleAuthStatus(srv.freshConfigContext(baseCtx), req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.Empty(t, fake.lastProfile, "explicit profile=default must target empty default profile, not work")
		assert.Equal(t, "default-client", fake.lastClientID)
	})

	t.Run("auth_list_tokens and auth_purge_expired honor profile=work and profile=default", func(t *testing.T) {
		override := "work"
		srv.profileOverride.Store(&override)
		t.Cleanup(func() { srv.profileOverride.Store((*string)(nil)) })

		listWork := mcp.CallToolRequest{}
		listWork.Params.Name = "auth_list_tokens"
		listWork.Params.Arguments = map[string]any{"handler": "entra", "profile": "work"}
		res, err := srv.handleListCachedTokens(srv.freshConfigContext(baseCtx), listWork)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.Equal(t, "work", fake.lastProfile)
		assert.Equal(t, "work-client", fake.lastClientID)

		listDefault := mcp.CallToolRequest{}
		listDefault.Params.Name = "auth_list_tokens"
		listDefault.Params.Arguments = map[string]any{"handler": "entra", "profile": "default"}
		res, err = srv.handleListCachedTokens(srv.freshConfigContext(baseCtx), listDefault)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.Empty(t, fake.lastProfile)
		assert.Equal(t, "default-client", fake.lastClientID)

		purgeDefault := mcp.CallToolRequest{}
		purgeDefault.Params.Name = "auth_purge_expired"
		purgeDefault.Params.Arguments = map[string]any{"handler": "entra", "profile": "default"}
		res, err = srv.handlePurgeExpiredTokens(srv.freshConfigContext(baseCtx), purgeDefault)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.Empty(t, fake.lastProfile)
	})
}
