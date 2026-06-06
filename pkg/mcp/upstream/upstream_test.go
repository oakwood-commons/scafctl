// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package upstream

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// newMockUpstreamServer creates a test MCP server with the given tools.
func newMockUpstreamServer(t *testing.T, tools map[string]string) *server.MCPServer {
	t.Helper()
	mcpServer := server.NewMCPServer("mock-upstream", "1.0.0",
		server.WithToolCapabilities(true),
	)
	for name, response := range tools {
		resp := response // capture
		mcpServer.AddTool(
			mcp.NewTool(name, mcp.WithDescription("mock tool: "+name)),
			func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText(resp), nil
			},
		)
	}
	return mcpServer
}

func TestProxy_ListTools(t *testing.T) {
	mcpServer := newMockUpstreamServer(t, map[string]string{
		"tool_a": "response_a",
		"tool_b": "response_b",
	})
	ts := server.NewTestStreamableHTTPServer(mcpServer)
	defer ts.Close()

	cfg := config.MCPServerConfig{
		URL: ts.URL,
	}
	proxy := NewProxy("test-upstream", cfg, nil, logr.Discard(), "")
	defer proxy.Close()

	tools, err := proxy.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 2)

	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	assert.Contains(t, names, "tool_a")
	assert.Contains(t, names, "tool_b")
}

func TestProxy_ListTools_Cached(t *testing.T) {
	mcpServer := newMockUpstreamServer(t, map[string]string{
		"tool_a": "response_a",
	})
	ts := server.NewTestStreamableHTTPServer(mcpServer)
	defer ts.Close()

	cfg := config.MCPServerConfig{URL: ts.URL}
	proxy := NewProxy("test", cfg, nil, logr.Discard(), "")
	defer proxy.Close()

	tools1, err := proxy.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools1, 1)

	tools2, err := proxy.ListTools(context.Background())
	require.NoError(t, err)
	assert.Equal(t, tools1, tools2)
}

func TestProxy_CallTool(t *testing.T) {
	mcpServer := newMockUpstreamServer(t, map[string]string{
		"greet": "hello world",
	})
	ts := server.NewTestStreamableHTTPServer(mcpServer)
	defer ts.Close()

	cfg := config.MCPServerConfig{URL: ts.URL}
	proxy := NewProxy("test", cfg, nil, logr.Discard(), "")
	defer proxy.Close()

	_, err := proxy.ListTools(context.Background())
	require.NoError(t, err)

	result, err := proxy.CallTool(context.Background(), "greet", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello world", textContent.Text)
}

func TestProxy_CallTool_WithPrefix(t *testing.T) {
	mcpServer := newMockUpstreamServer(t, map[string]string{
		"greet": "hello prefixed",
	})
	ts := server.NewTestStreamableHTTPServer(mcpServer)
	defer ts.Close()

	cfg := config.MCPServerConfig{
		URL:        ts.URL,
		ToolPrefix: "upstream_",
	}
	proxy := NewProxy("test", cfg, nil, logr.Discard(), "")
	defer proxy.Close()

	tools, err := proxy.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "upstream_greet", tools[0].Name)

	result, err := proxy.CallTool(context.Background(), "upstream_greet", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello prefixed", textContent.Text)
}

func TestProxy_CallTool_LazyConnect(t *testing.T) {
	mcpServer := newMockUpstreamServer(t, map[string]string{
		"greet": "hello lazy",
	})
	ts := server.NewTestStreamableHTTPServer(mcpServer)
	defer ts.Close()

	cfg := config.MCPServerConfig{URL: ts.URL}
	proxy := NewProxy("test", cfg, nil, logr.Discard(), "")
	defer proxy.Close()

	result, err := proxy.CallTool(context.Background(), "greet", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello lazy", textContent.Text)
}

func TestProxy_Close(t *testing.T) {
	mcpServer := newMockUpstreamServer(t, map[string]string{
		"tool_a": "response_a",
	})
	ts := server.NewTestStreamableHTTPServer(mcpServer)
	defer ts.Close()

	cfg := config.MCPServerConfig{URL: ts.URL}
	proxy := NewProxy("test", cfg, nil, logr.Discard(), "")

	_, err := proxy.ListTools(context.Background())
	require.NoError(t, err)

	err = proxy.Close()
	assert.NoError(t, err)

	// Close again should be safe (idempotent).
	err = proxy.Close()
	assert.NoError(t, err)
}

func TestProxy_Name(t *testing.T) {
	proxy := NewProxy("my-upstream", config.MCPServerConfig{}, nil, logr.Discard(), "")
	assert.Equal(t, "my-upstream", proxy.Name())
}

func TestProxy_ClientName(t *testing.T) {
	t.Run("custom name", func(t *testing.T) {
		proxy := NewProxy("test", config.MCPServerConfig{}, nil, logr.Discard(), "mycli")
		assert.Equal(t, "mycli", proxy.clientName)
	})
	t.Run("empty falls back to default", func(t *testing.T) {
		proxy := NewProxy("test", config.MCPServerConfig{}, nil, logr.Discard(), "")
		assert.Equal(t, settings.CliBinaryName, proxy.clientName)
	})
}

func TestProxy_ConnectError(t *testing.T) {
	cfg := config.MCPServerConfig{
		URL: "http://127.0.0.1:1", // nothing listening
	}
	proxy := NewProxy("bad", cfg, nil, logr.Discard(), "")
	defer proxy.Close()

	_, err := proxy.ListTools(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad")
}

func TestProxy_BuildAuthHeaderFunc_NoAuthReg(t *testing.T) {
	proxy := NewProxy("test", config.MCPServerConfig{
		Auth: config.MCPServerAuthConfig{Handler: "entra"},
	}, nil, logr.Discard(), "")

	fn := proxy.buildAuthHeaderFunc()
	headers := fn(context.Background())
	assert.Nil(t, headers)
}

func TestProxy_BuildAuthHeaderFunc_NoHandler(t *testing.T) {
	proxy := NewProxy("test", config.MCPServerConfig{}, nil, logr.Discard(), "")

	fn := proxy.buildAuthHeaderFunc()
	headers := fn(context.Background())
	assert.Nil(t, headers)
}

func TestProxy_ToolFilter_WithAllowlist(t *testing.T) {
	mcpServer := newMockUpstreamServer(t, map[string]string{
		"auth_login":  "ok",
		"auth_logout": "ok",
		"data_query":  "ok",
	})
	ts := server.NewTestStreamableHTTPServer(mcpServer)
	defer ts.Close()

	cfg := config.MCPServerConfig{
		URL:   ts.URL,
		Tools: []string{"auth_*"},
	}
	proxy := NewProxy("test", cfg, nil, logr.Discard(), "")
	defer proxy.Close()

	tools, err := proxy.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 2)

	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	assert.Contains(t, names, "auth_login")
	assert.Contains(t, names, "auth_logout")
	assert.NotContains(t, names, "data_query")
}

func TestFilterTools(t *testing.T) {
	tools := []mcp.Tool{
		mcp.NewTool("auth_login"),
		mcp.NewTool("auth_logout"),
		mcp.NewTool("data_query"),
		mcp.NewTool("data_insert"),
		mcp.NewTool("admin_reset"),
	}

	tests := []struct {
		name     string
		patterns []string
		want     []string
	}{
		{
			name:     "empty patterns allows all",
			patterns: nil,
			want:     []string{"auth_login", "auth_logout", "data_query", "data_insert", "admin_reset"},
		},
		{
			name:     "wildcard allows all",
			patterns: []string{"*"},
			want:     []string{"auth_login", "auth_logout", "data_query", "data_insert", "admin_reset"},
		},
		{
			name:     "prefix pattern",
			patterns: []string{"auth_*"},
			want:     []string{"auth_login", "auth_logout"},
		},
		{
			name:     "multiple patterns",
			patterns: []string{"auth_*", "admin_*"},
			want:     []string{"auth_login", "auth_logout", "admin_reset"},
		},
		{
			name:     "no match",
			patterns: []string{"nonexistent_*"},
			want:     nil,
		},
		{
			name:     "exact match",
			patterns: []string{"data_query"},
			want:     []string{"data_query"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterTools(tools, tt.patterns)
			gotNames := make([]string, len(got))
			for i, tool := range got {
				gotNames[i] = tool.Name
			}
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, gotNames)
			}
		})
	}
}

func TestPrefixTools(t *testing.T) {
	tools := []mcp.Tool{
		mcp.NewTool("tool_a"),
		mcp.NewTool("tool_b"),
	}

	t.Run("empty prefix is no-op", func(t *testing.T) {
		result := prefixTools(tools, "")
		assert.Equal(t, tools, result)
	})

	t.Run("applies prefix", func(t *testing.T) {
		result := prefixTools(tools, "remote_")
		require.Len(t, result, 2)
		assert.Equal(t, "remote_tool_a", result[0].Name)
		assert.Equal(t, "remote_tool_b", result[1].Name)
	})

	t.Run("does not modify original", func(t *testing.T) {
		_ = prefixTools(tools, "remote_")
		assert.Equal(t, "tool_a", tools[0].Name)
		assert.Equal(t, "tool_b", tools[1].Name)
	})
}

func TestMCPServerConfig_IsEnabled(t *testing.T) {
	t.Run("nil defaults to true", func(t *testing.T) {
		cfg := config.MCPServerConfig{}
		assert.True(t, cfg.IsEnabled())
	})

	t.Run("explicit true", func(t *testing.T) {
		enabled := true
		cfg := config.MCPServerConfig{Enabled: &enabled}
		assert.True(t, cfg.IsEnabled())
	})

	t.Run("explicit false", func(t *testing.T) {
		enabled := false
		cfg := config.MCPServerConfig{Enabled: &enabled}
		assert.False(t, cfg.IsEnabled())
	})
}

func TestProxy_BuildAuthHeaderFunc_Success(t *testing.T) {
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.GetTokenResult = &auth.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
	}

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))

	proxy := NewProxy("test", config.MCPServerConfig{
		Auth: config.MCPServerAuthConfig{
			Handler: "entra",
			Scope:   "api://my-scope",
		},
	}, reg, logr.Discard(), "")

	fn := proxy.buildAuthHeaderFunc()
	headers := fn(context.Background())
	require.NotNil(t, headers)
	assert.Equal(t, "Bearer test-access-token", headers["Authorization"])
	require.Len(t, mockHandler.GetTokenCalls, 1)
	assert.Equal(t, "api://my-scope", mockHandler.GetTokenCalls[0].Scope)
}

func TestProxy_BuildAuthHeaderFunc_CustomTokenType(t *testing.T) {
	mockHandler := auth.NewMockHandler("github")
	mockHandler.GetTokenResult = &auth.Token{
		AccessToken: "ghp_abc123",
		TokenType:   "token",
	}

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))

	proxy := NewProxy("test", config.MCPServerConfig{
		Auth: config.MCPServerAuthConfig{Handler: "github"},
	}, reg, logr.Discard(), "")

	fn := proxy.buildAuthHeaderFunc()
	headers := fn(context.Background())
	require.NotNil(t, headers)
	assert.Equal(t, "token ghp_abc123", headers["Authorization"])
}

func TestProxy_BuildAuthHeaderFunc_EmptyTokenType(t *testing.T) {
	mockHandler := auth.NewMockHandler("test-handler")
	mockHandler.GetTokenResult = &auth.Token{
		AccessToken: "my-token",
		TokenType:   "", // empty => defaults to Bearer
	}

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))

	proxy := NewProxy("test", config.MCPServerConfig{
		Auth: config.MCPServerAuthConfig{Handler: "test-handler"},
	}, reg, logr.Discard(), "")

	fn := proxy.buildAuthHeaderFunc()
	headers := fn(context.Background())
	require.NotNil(t, headers)
	assert.Equal(t, "Bearer my-token", headers["Authorization"])
}

func TestProxy_BuildAuthHeaderFunc_GetTokenError(t *testing.T) {
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.GetTokenErr = assert.AnError

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))

	proxy := NewProxy("test", config.MCPServerConfig{
		Auth: config.MCPServerAuthConfig{Handler: "entra"},
	}, reg, logr.Discard(), "")

	fn := proxy.buildAuthHeaderFunc()
	headers := fn(context.Background())
	assert.Nil(t, headers)
}

func TestProxy_Connect_Timeout(t *testing.T) {
	mcpServer := newMockUpstreamServer(t, map[string]string{
		"tool_a": "response_a",
	})
	ts := server.NewTestStreamableHTTPServer(mcpServer)
	defer ts.Close()

	cfg := config.MCPServerConfig{
		URL:     ts.URL,
		Timeout: "5s",
	}
	proxy := NewProxy("test", cfg, nil, logr.Discard(), "")
	defer proxy.Close()

	tools, err := proxy.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
}

func TestProxy_Connect_InvalidTimeout(t *testing.T) {
	cfg := config.MCPServerConfig{
		URL:     "http://127.0.0.1:1",
		Timeout: "not-a-duration",
	}
	proxy := NewProxy("test", cfg, nil, logr.Discard(), "")
	defer proxy.Close()

	_, err := proxy.ListTools(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeout")
}

func TestProxy_Connect_ZeroTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
	}{
		{name: "zero", timeout: "0"},
		{name: "zero_seconds", timeout: "0s"},
		{name: "negative", timeout: "-5s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.MCPServerConfig{
				URL:     "http://127.0.0.1:1",
				Timeout: tt.timeout,
			}
			proxy := NewProxy("test", cfg, nil, logr.Discard(), "")
			defer proxy.Close()

			_, err := proxy.ListTools(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "timeout must be positive")
		})
	}
}
