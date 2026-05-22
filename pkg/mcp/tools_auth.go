// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/auth"
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
	s.addTool(authStatusTool, s.handleAuthStatus)

	listAuthHandlersTool := mcp.NewTool("list_auth_handlers",
		mcp.WithDescription("List all registered auth handlers with their supported flows and capabilities. Unlike auth_status which shows credential state, this tool shows what handlers are available and what they support (device-code, interactive, service-principal, workload-identity, PAT, metadata flows). Use this to understand which auth methods are available before attempting login."),
		mcp.WithTitleAnnotation("List Auth Handlers"),
		mcp.WithToolIcons(toolIcons["auth"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.addTool(listAuthHandlersTool, s.handleListAuthHandlers)

	listCachedTokensTool := mcp.NewTool("auth_list_tokens",
		mcp.WithDescription("List all cached tokens across registered auth handlers. Shows token metadata including handler, scope, flow, expiry, and whether expired. Use to inspect cached credentials without revealing actual token values."),
		mcp.WithTitleAnnotation("List Cached Auth Tokens"),
		mcp.WithToolIcons(toolIcons["auth"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("handler",
			mcp.Description("Optional handler name to filter tokens (e.g., 'entra', 'github'). If omitted, tokens from all handlers are returned."),
		),
	)
	s.addTool(listCachedTokensTool, s.handleListCachedTokens)

	purgeExpiredTool := mcp.NewTool("auth_purge_expired",
		mcp.WithDescription("Remove expired access tokens from the cache across all registered auth handlers (or a specific one). Keeps valid tokens and refresh tokens. Returns the count of purged tokens."),
		mcp.WithTitleAnnotation("Purge Expired Auth Tokens"),
		mcp.WithToolIcons(toolIcons["auth"]),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("handler",
			mcp.Description("Optional handler name to scope the purge (e.g., 'entra', 'github'). If omitted, all handlers are purged."),
		),
	)
	s.addTool(purgeExpiredTool, s.handlePurgeExpiredTokens)
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

// handleListCachedTokens lists all cached tokens across auth handlers.
func (s *Server) handleListCachedTokens(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.authReg == nil {
		return mcp.NewToolResultJSON(map[string]any{
			"tokens":  []any{},
			"count":   0,
			"message": "No auth registry configured",
		})
	}

	args := req.GetArguments()
	handlerFilter, _ := args["handler"].(string)

	names := s.authReg.List()
	if len(names) == 0 {
		return mcp.NewToolResultJSON(map[string]any{
			"tokens":  []any{},
			"count":   0,
			"message": "No auth handlers registered",
		})
	}

	type tokenInfo struct {
		Handler     string `json:"handler"`
		TokenKind   string `json:"tokenKind"`
		Scope       string `json:"scope,omitempty"`
		TokenType   string `json:"tokenType,omitempty"`
		Flow        string `json:"flow,omitempty"`
		ExpiresAt   string `json:"expiresAt,omitempty"`
		CachedAt    string `json:"cachedAt,omitempty"`
		IsExpired   bool   `json:"isExpired"`
		Fingerprint string `json:"fingerprint,omitempty"`
		SessionID   string `json:"sessionId,omitempty"`
	}

	var tokens []tokenInfo
	var warnings []string
	for _, name := range names {
		if handlerFilter != "" && name != handlerFilter {
			continue
		}

		h, err := s.authReg.Get(name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("handler %q: failed to get: %v", name, err))
			continue
		}

		lister, ok := h.(auth.TokenLister)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("handler %q: does not support token listing", name))
			continue
		}

		cached, err := lister.ListCachedTokens(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("handler %q: %v", name, err))
		}

		for _, t := range cached {
			info := tokenInfo{
				Handler:     t.Handler,
				TokenKind:   t.TokenKind,
				Scope:       t.Scope,
				TokenType:   t.TokenType,
				Flow:        string(t.Flow),
				IsExpired:   t.IsExpired,
				Fingerprint: t.Fingerprint,
				SessionID:   t.SessionID,
			}
			if !t.ExpiresAt.IsZero() {
				info.ExpiresAt = t.ExpiresAt.Format(time.RFC3339)
			}
			if !t.CachedAt.IsZero() {
				info.CachedAt = t.CachedAt.Format(time.RFC3339)
			}
			tokens = append(tokens, info)
		}
	}

	if tokens == nil {
		tokens = []tokenInfo{}
	}

	result := map[string]any{
		"tokens": tokens,
		"count":  len(tokens),
	}
	if len(warnings) > 0 {
		result["warnings"] = warnings
	}

	return mcp.NewToolResultJSON(result)
}

// handlePurgeExpiredTokens removes expired access tokens from the cache.
func (s *Server) handlePurgeExpiredTokens(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.authReg == nil {
		return mcp.NewToolResultJSON(map[string]any{
			"handlers":    []any{},
			"totalPurged": 0,
			"message":     "No auth registry configured",
		})
	}

	args := req.GetArguments()
	handlerFilter, _ := args["handler"].(string)

	names := s.authReg.List()
	if len(names) == 0 {
		return mcp.NewToolResultJSON(map[string]any{
			"handlers":    []any{},
			"totalPurged": 0,
			"message":     "No auth handlers registered",
		})
	}

	type purgeResult struct {
		Handler string `json:"handler"`
		Purged  int    `json:"purged"`
		Error   string `json:"error,omitempty"`
		Status  string `json:"status,omitempty"`
	}

	var results []purgeResult
	total := 0
	for _, name := range names {
		if handlerFilter != "" && name != handlerFilter {
			continue
		}

		h, err := s.authReg.Get(name)
		if err != nil {
			results = append(results, purgeResult{Handler: name, Error: fmt.Sprintf("failed to get handler: %v", err)})
			continue
		}

		purger, ok := h.(auth.TokenPurger)
		if !ok {
			results = append(results, purgeResult{Handler: name, Status: "skipped: does not support token purging"})
			continue
		}

		n, err := purger.PurgeExpiredTokens(ctx)
		if err != nil {
			results = append(results, purgeResult{Handler: name, Error: fmt.Sprintf("purge failed: %v", err)})
			continue
		}

		results = append(results, purgeResult{Handler: name, Purged: n})
		total += n
	}

	if results == nil {
		results = []purgeResult{}
	}

	return mcp.NewToolResultJSON(map[string]any{
		"handlers":    results,
		"totalPurged": total,
	})
}
