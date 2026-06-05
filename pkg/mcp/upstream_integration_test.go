// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/mcp/upstream"
)

// newTestUpstreamMCPServer creates a mock upstream MCP server for integration tests.
func newTestUpstreamMCPServer(t *testing.T, tools map[string]string) *httptest.Server {
	t.Helper()
	srv := mcpserver.NewMCPServer("test-upstream", "1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	for name, response := range tools {
		resp := response
		srv.AddTool(
			mcp.NewTool(name, mcp.WithDescription("mock: "+name)),
			func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText(resp), nil
			},
		)
	}
	return mcpserver.NewTestStreamableHTTPServer(srv)
}

func TestWithUpstreamServer(t *testing.T) {
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"remote_tool": "remote_result",
	})
	defer ts.Close()

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("test", config.MCPServerConfig{
			URL: ts.URL,
		}),
	)
	require.NoError(t, err)
	defer srv.Close()

	assert.Len(t, srv.upstreamProxies, 1)
	assert.Contains(t, srv.upstreamProxies, "test")
}

func TestRegisterUpstreamTools_ViaCoreServer(t *testing.T) {
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"remote_greet": "hello",
		"remote_calc":  "42",
	})
	defer ts.Close()

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("backend", config.MCPServerConfig{
			URL: ts.URL,
		}),
	)
	require.NoError(t, err)
	defer srv.Close()

	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)

	// Upstream tools should be tracked.
	assert.Contains(t, srv.upstreamTools, "remote_greet")
	assert.Contains(t, srv.upstreamTools, "remote_calc")
	assert.Equal(t, "backend", srv.upstreamTools["remote_greet"])
}

func TestRegisterUpstreamTools_Idempotent(t *testing.T) {
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"tool_x": "result",
	})
	defer ts.Close()

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("test", config.MCPServerConfig{
			URL: ts.URL,
		}),
	)
	require.NoError(t, err)
	defer srv.Close()

	// Call twice -- should only register once (sealed after success).
	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)

	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)

	assert.Len(t, srv.upstreamTools, 1)
}

func TestRegisterUpstreamTools_RetriesOnFailure(t *testing.T) {
	// First attempt: upstream is unreachable.
	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("bad", config.MCPServerConfig{
			URL:     "http://127.0.0.1:1",
			Timeout: "200ms",
		}),
	)
	require.NoError(t, err)
	defer srv.Close()

	err = srv.registerUpstreamTools(context.Background())
	require.Error(t, err)
	assert.Empty(t, srv.upstreamTools)

	// Replace the proxy with one pointing at a live server.
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"recovered_tool": "ok",
	})
	defer ts.Close()

	srv.upstreamProxies["bad"].Close()
	srv.upstreamProxies["bad"] = upstream.NewProxy(
		"bad", config.MCPServerConfig{URL: ts.URL}, nil, logr.Discard(), srv.name,
	)

	// Second attempt should succeed now.
	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)
	assert.Contains(t, srv.upstreamTools, "recovered_tool")
}

func TestRegisterUpstreamTools_SkipsCoreToolCollision(t *testing.T) {
	// Create an upstream that exposes a tool with the same name as a core tool.
	coreName := ""
	for name := range coreToolNames() {
		coreName = name
		break
	}
	require.NotEmpty(t, coreName, "need at least one core tool name")

	ts := newTestUpstreamMCPServer(t, map[string]string{
		coreName:    "should-be-skipped",
		"safe_tool": "allowed",
	})
	defer ts.Close()

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("conflict", config.MCPServerConfig{
			URL: ts.URL,
		}),
	)
	require.NoError(t, err)
	defer srv.Close()

	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)

	// Core-conflicting tool should be skipped.
	assert.NotContains(t, srv.upstreamTools, coreName)
	// Non-conflicting tool should be registered.
	assert.Contains(t, srv.upstreamTools, "safe_tool")
}

func TestRegisterUpstreamTools_ConfigBased(t *testing.T) {
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"cfg_tool": "from-config",
	})
	defer ts.Close()

	cfg := &config.Config{
		Version: 1,
		MCP: config.MCPConfig{
			Servers: map[string]config.MCPServerConfig{
				"from-config": {URL: ts.URL},
			},
		},
	}

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithServerConfig(cfg),
	)
	require.NoError(t, err)
	defer srv.Close()

	assert.Len(t, srv.upstreamProxies, 1)
	assert.Contains(t, srv.upstreamProxies, "from-config")

	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)
	assert.Contains(t, srv.upstreamTools, "cfg_tool")
}

func TestRegisterUpstreamTools_OptionOverridesConfig(t *testing.T) {
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"overridden_tool": "from-option",
	})
	defer ts.Close()

	cfg := &config.Config{
		Version: 1,
		MCP: config.MCPConfig{
			Servers: map[string]config.MCPServerConfig{
				"srv": {URL: "http://127.0.0.1:1"}, // bad URL in config
			},
		},
	}

	// Option with same name should override the config entry.
	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithServerConfig(cfg),
		WithUpstreamServer("srv", config.MCPServerConfig{URL: ts.URL}),
	)
	require.NoError(t, err)
	defer srv.Close()

	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)
	assert.Contains(t, srv.upstreamTools, "overridden_tool")
}

func TestRegisterUpstreamTools_DisabledServer(t *testing.T) {
	disabled := false
	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("disabled", config.MCPServerConfig{
			URL:     "http://127.0.0.1:1",
			Enabled: &disabled,
		}),
	)
	require.NoError(t, err)
	defer srv.Close()

	// Disabled servers should not create proxies.
	assert.Empty(t, srv.upstreamProxies)
}

func TestServerClose_ClosesUpstreamProxies(t *testing.T) {
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"tool_a": "a",
	})
	defer ts.Close()

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("test", config.MCPServerConfig{URL: ts.URL}),
	)
	require.NoError(t, err)

	// Connect the proxy.
	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)

	// Close should not panic.
	srv.Close()
	// Double close should be safe.
	srv.Close()
}

func TestServerClose_ConcurrentWithRegister(t *testing.T) {
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"tool_a": "a",
	})
	defer ts.Close()

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("test", config.MCPServerConfig{URL: ts.URL}),
	)
	require.NoError(t, err)

	// Run Close and registerUpstreamTools concurrently to verify no race.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		srv.Close()
	}()
	go func() {
		defer wg.Done()
		_ = srv.registerUpstreamTools(context.Background())
	}()
	wg.Wait()
}

func TestUpstreamTools_InListCapabilities(t *testing.T) {
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"upstream_only_tool": "data",
	})
	defer ts.Close()

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("remote", config.MCPServerConfig{URL: ts.URL}),
	)
	require.NoError(t, err)
	defer srv.Close()

	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)

	caps := srv.ListCapabilities()
	found := false
	for _, c := range caps {
		if c.Name == "upstream_only_tool" {
			found = true
			assert.Equal(t, SourceUpstream, c.Source)
			assert.Equal(t, CapabilityTool, c.Kind)
			break
		}
	}
	assert.True(t, found, "upstream tool should appear in ListCapabilities")
}

func TestInfo_IncludesUpstreamTools(t *testing.T) {
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"info_upstream_tool": "data",
	})
	defer ts.Close()

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("info-test", config.MCPServerConfig{URL: ts.URL}),
	)
	require.NoError(t, err)
	defer srv.Close()

	// Info() should trigger upstream registration automatically.
	infoJSON, err := srv.Info()
	require.NoError(t, err)
	assert.Contains(t, string(infoJSON), "info_upstream_tool")
}

func TestRegisterUpstreamTools_SkipsEmbedderToolCollision(t *testing.T) {
	// Register a tool via the embedder API before upstream discovery.
	ts := newTestUpstreamMCPServer(t, map[string]string{
		"custom_embedder_tool": "should-be-skipped",
		"unique_upstream":      "allowed",
	})
	defer ts.Close()

	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithUpstreamServer("emb-test", config.MCPServerConfig{URL: ts.URL}),
	)
	require.NoError(t, err)
	defer srv.Close()

	// Simulate embedder adding a tool before Serve is called.
	srv.MCPServer().AddTool(
		mcp.NewTool("custom_embedder_tool", mcp.WithDescription("embedder tool")),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("embedder"), nil
		},
	)

	err = srv.registerUpstreamTools(context.Background())
	require.NoError(t, err)

	// Embedder-conflicting tool should be skipped.
	assert.NotContains(t, srv.upstreamTools, "custom_embedder_tool")
	// Non-conflicting tool should be registered.
	assert.Contains(t, srv.upstreamTools, "unique_upstream")
}

// coreToolNames returns the set of core tool names from a fresh server.
func coreToolNames() map[string]struct{} {
	srv, err := NewServer(WithServerLogger(logr.Discard()))
	if err != nil {
		return nil
	}
	return srv.coreTools
}
