// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerAuthTools registers all auth-related MCP tools.
func (s *Server) registerAuthTools() {
	authStatusTool := mcp.NewTool("auth_status",
		mcp.WithDescription("Report which auth handlers (e.g. entra, gcp, github) are configured and whether their tokens are valid. Auth handlers manage authentication and identity — they are NOT solution providers. Helps verify authentication is set up correctly before attempting operations that require it."),
		mcp.WithTitleAnnotation("Auth Status"),
		mcp.WithToolIcons(toolIcons["auth"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithRawOutputSchema(outputSchemaAuthStatus),
	)
	s.mcpServer.AddTool(authStatusTool, s.handleAuthStatus)

	listAuthHandlersTool := mcp.NewTool("list_auth_handlers",
		mcp.WithDescription("List all registered auth handlers with their supported flows and capabilities. Unlike auth_status which shows credential state, this tool shows what handlers are available and what they support (device-code, interactive, service-principal, workload-identity, PAT, metadata flows). Use this to understand which auth methods are available before attempting login."),
		mcp.WithTitleAnnotation("List Auth Handlers"),
		mcp.WithToolIcons(toolIcons["auth"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.mcpServer.AddTool(listAuthHandlersTool, s.handleListAuthHandlers)
}

// authHandlerStatus represents the status of a single auth handler.
type authHandlerStatus struct {
	Name          string   `json:"name"`
	DisplayName   string   `json:"displayName,omitempty"`
	Authenticated bool     `json:"authenticated"`
	IdentityType  string   `json:"identityType,omitempty"`
	ExpiresAt     string   `json:"expiresAt,omitempty"`
	Email         string   `json:"email,omitempty"`
	Username      string   `json:"username,omitempty"`
	Flows         []string `json:"flows,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// handleAuthStatus reports auth provider status.
func (s *Server) handleAuthStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.authReg == nil {
		return mcp.NewToolResultJSON(map[string]any{
			"handlers": []any{},
			"message":  "No auth registry configured",
		})
	}

	handlers := s.authReg.All()
	if len(handlers) == 0 {
		return mcp.NewToolResultJSON(map[string]any{
			"handlers": []any{},
			"message":  "No auth handlers registered",
		})
	}

	// Sort handler names for deterministic output
	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	sort.Strings(names)

	statuses := make([]authHandlerStatus, 0, len(handlers))
	for _, name := range names {
		handler := handlers[name]
		status := authHandlerStatus{
			Name:        handler.Name(),
			DisplayName: handler.DisplayName(),
		}

		// Get supported flows
		for _, flow := range handler.SupportedFlows() {
			status.Flows = append(status.Flows, string(flow))
		}

		// Get capabilities
		for _, cap := range handler.Capabilities() {
			status.Capabilities = append(status.Capabilities, string(cap))
		}

		// Check status
		authStatus, err := handler.Status(s.ctx)
		if err != nil {
			status.Error = fmt.Sprintf("failed to get status: %v", err)
			statuses = append(statuses, status)
			continue
		}

		status.Authenticated = authStatus.Authenticated
		if authStatus.IdentityType != "" {
			status.IdentityType = string(authStatus.IdentityType)
		}
		if !authStatus.ExpiresAt.IsZero() {
			status.ExpiresAt = authStatus.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
		}
		if authStatus.Claims != nil {
			status.Email = authStatus.Claims.Email
			status.Username = authStatus.Claims.Username
		}

		statuses = append(statuses, status)
	}

	return mcp.NewToolResultJSON(map[string]any{
		"handlers": statuses,
		"count":    len(statuses),
	})
}

// handleListAuthHandlers lists all registered auth handlers with their flows and capabilities.
func (s *Server) handleListAuthHandlers(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.authReg == nil {
		return mcp.NewToolResultJSON(map[string]any{
			"handlers": []any{},
			"message":  "No auth registry configured",
		})
	}

	names := s.authReg.List()
	if len(names) == 0 {
		return mcp.NewToolResultJSON(map[string]any{
			"handlers": []any{},
			"message":  "No auth handlers registered",
		})
	}

	type handlerInfo struct {
		Name         string   `json:"name"`
		DisplayName  string   `json:"displayName,omitempty"`
		Flows        []string `json:"flows"`
		Capabilities []string `json:"capabilities"`
	}

	handlers := make([]handlerInfo, 0, len(names))
	for _, name := range names {
		h, err := s.authReg.Get(name)
		if err != nil {
			continue
		}

		info := handlerInfo{
			Name:        h.Name(),
			DisplayName: h.DisplayName(),
		}

		for _, flow := range h.SupportedFlows() {
			info.Flows = append(info.Flows, string(flow))
		}
		for _, cap := range h.Capabilities() {
			info.Capabilities = append(info.Capabilities, string(cap))
		}

		handlers = append(handlers, info)
	}

	return mcp.NewToolResultJSON(map[string]any{
		"handlers": handlers,
		"count":    len(handlers),
	})
}
