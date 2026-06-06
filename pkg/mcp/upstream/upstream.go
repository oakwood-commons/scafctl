// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package upstream provides MCP upstream server proxying with automatic
// auth token injection. It connects to remote MCP servers, discovers their
// tools, and registers them as local tools that transparently forward
// tools/call requests with authentication.
package upstream

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// DefaultTimeout is the default request timeout for upstream MCP server calls.
const DefaultTimeout = 30 * time.Second

// Proxy manages a connection to a single upstream MCP server.
// It lazily connects on the first ListTools or CallTool request.
type Proxy struct {
	name       string
	clientName string
	cfg        config.MCPServerConfig
	logger     logr.Logger

	authReg *auth.Registry

	mu           sync.Mutex
	client       *mcpclient.Client
	tools        []mcp.Tool
	toolsFetched bool
}

// NewProxy creates a new upstream proxy for the named server.
// clientName is the MCP client identity advertised during Initialize
// (typically the embedder's binary name).
func NewProxy(name string, cfg config.MCPServerConfig, authReg *auth.Registry, logger logr.Logger, clientName string) *Proxy {
	if clientName == "" {
		clientName = settings.CliBinaryName
	}
	return &Proxy{
		name:       name,
		clientName: clientName,
		cfg:        cfg,
		authReg:    authReg,
		logger:     logger.WithValues("upstream", name),
	}
}

// Name returns the upstream server name.
func (p *Proxy) Name() string {
	return p.name
}

// connect establishes the connection to the upstream server.
// Must be called with p.mu held.
func (p *Proxy) connect(ctx context.Context) error {
	if p.client != nil {
		return nil
	}

	p.logger.V(1).Info("connecting to upstream MCP server", "url", p.cfg.URL)

	var transportOpts []transport.StreamableHTTPCOption

	// Apply request timeout from config (falls back to DefaultTimeout).
	timeout := DefaultTimeout
	if p.cfg.Timeout != "" {
		parsed, err := time.ParseDuration(p.cfg.Timeout)
		if err != nil {
			return fmt.Errorf("upstream %q: invalid timeout %q: %w", p.name, p.cfg.Timeout, err)
		}
		if parsed <= 0 {
			return fmt.Errorf("upstream %q: timeout must be positive, got %q", p.name, p.cfg.Timeout)
		}
		timeout = parsed
	}
	transportOpts = append(transportOpts, transport.WithHTTPTimeout(timeout))

	// Inject auth headers if an auth handler is configured.
	if p.cfg.Auth.Handler != "" {
		headerFunc := p.buildAuthHeaderFunc()
		transportOpts = append(transportOpts, transport.WithHTTPHeaderFunc(headerFunc))
	}

	t, err := transport.NewStreamableHTTP(p.cfg.URL, transportOpts...)
	if err != nil {
		return fmt.Errorf("upstream %q: failed to create transport: %w", p.name, err)
	}

	c := mcpclient.NewClient(t)

	// Use the configured timeout for the handshake so a hanging upstream
	// cannot block connect() indefinitely.
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := c.Start(connectCtx); err != nil {
		return fmt.Errorf("upstream %q: failed to start transport: %w", p.name, err)
	}

	_, err = c.Initialize(connectCtx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ClientInfo: mcp.Implementation{
				Name:    p.clientName,
				Version: settings.VersionInformation.BuildVersion,
			},
		},
	})
	if err != nil {
		_ = c.Close()
		return fmt.Errorf("upstream %q: initialize failed: %w", p.name, err)
	}

	p.client = c
	p.logger.V(1).Info("connected to upstream MCP server")
	return nil
}

// buildAuthHeaderFunc returns a transport.HTTPHeaderFunc that injects
// auth tokens from the configured auth handler.
func (p *Proxy) buildAuthHeaderFunc() transport.HTTPHeaderFunc {
	return func(ctx context.Context) map[string]string {
		if p.authReg == nil || p.cfg.Auth.Handler == "" {
			return nil
		}

		handler, err := p.authReg.GetContext(ctx, p.cfg.Auth.Handler)
		if err != nil {
			p.logger.Error(err, "upstream auth handler not found", "handler", p.cfg.Auth.Handler)
			return nil
		}

		token, err := handler.GetToken(ctx, auth.TokenOptions{
			Scope: p.cfg.Auth.Scope,
		})
		if err != nil {
			p.logger.Error(err, "upstream auth token acquisition failed", "handler", p.cfg.Auth.Handler)
			return nil
		}

		tokenType := token.TokenType
		if tokenType == "" {
			tokenType = "Bearer"
		}
		return map[string]string{
			"Authorization": tokenType + " " + token.AccessToken,
		}
	}
}

// ListTools connects lazily and returns the upstream server's tools.
// Tools are cached after the first successful fetch.
func (p *Proxy) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.connect(ctx); err != nil {
		return nil, err
	}

	// Use cached tools if available (including empty results).
	if p.toolsFetched {
		return p.tools, nil
	}

	result, err := p.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("upstream %q: list tools failed: %w", p.name, err)
	}

	tools := filterTools(result.Tools, p.cfg.Tools)
	tools = prefixTools(tools, p.cfg.ToolPrefix)
	p.tools = tools
	p.toolsFetched = true

	p.logger.Info("discovered upstream tools", "count", len(tools))
	return tools, nil
}

// CallTool forwards a tool call to the upstream server.
func (p *Proxy) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	p.mu.Lock()
	if err := p.connect(ctx); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	c := p.client
	p.mu.Unlock()

	// Strip the tool prefix to get the original upstream tool name.
	remoteName := name
	if p.cfg.ToolPrefix != "" {
		remoteName = strings.TrimPrefix(name, p.cfg.ToolPrefix)
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      remoteName,
			Arguments: arguments,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upstream %q: call tool %q failed: %w", p.name, remoteName, err)
	}

	return result, nil
}

// Close shuts down the upstream connection.
func (p *Proxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		err := p.client.Close()
		p.client = nil
		p.tools = nil
		p.toolsFetched = false
		return err
	}
	return nil
}

// filterTools returns only tools whose names match the allowlist patterns.
// An empty allowlist means all tools are allowed.
func filterTools(tools []mcp.Tool, patterns []string) []mcp.Tool {
	if len(patterns) == 0 {
		return tools
	}

	var filtered []mcp.Tool
	for _, tool := range tools {
		for _, pattern := range patterns {
			matched, err := path.Match(pattern, tool.Name)
			if err != nil {
				// Invalid pattern -- skip it.
				continue
			}
			if matched {
				filtered = append(filtered, tool)
				break
			}
		}
	}
	return filtered
}

// prefixTools adds a prefix to all tool names. If prefix is empty, tools are
// returned unchanged.
func prefixTools(tools []mcp.Tool, prefix string) []mcp.Tool {
	if prefix == "" {
		return tools
	}
	prefixed := make([]mcp.Tool, len(tools))
	for i, tool := range tools {
		tool.Name = prefix + tool.Name
		prefixed[i] = tool
	}
	return prefixed
}
