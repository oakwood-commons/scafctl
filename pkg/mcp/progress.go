// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// progressReporter sends progress notifications to connected MCP clients
// during long-running tool operations. It gracefully handles cases where
// no session is available (e.g., stdio transport without session context)
// by logging progress instead.
type progressReporter struct {
	mcpServer *server.MCPServer
	logger    logr.Logger
	token     mcp.ProgressToken
	total     float64
}

// newProgressReporter creates a reporter for a tool invocation. It extracts the
// progress token from the request's _meta field if the client provided one.
// If no token is present, the reporter becomes a no-op (progress is only logged).
func newProgressReporter(s *Server, request mcp.CallToolRequest) *progressReporter {
	r := &progressReporter{
		mcpServer: s.mcpServer,
		logger:    s.logger,
	}

	// Extract progress token from the request _meta if provided by the client
	if request.Params.Meta != nil {
		r.token = request.Params.Meta.ProgressToken
	}

	return r
}

// setTotal sets the total number of work items for progress calculation.
func (r *progressReporter) setTotal(total float64) {
	r.total = total
}

// report sends a progress notification to the client.
// progress is the current step (1-based), message is a human-readable status.
// If no progress token was provided by the client, it logs instead.
func (r *progressReporter) report(ctx context.Context, progress float64, message string) {
	if r.token == nil {
		r.logger.V(1).Info("progress", "step", progress, "total", r.total, "message", message)
		return
	}

	var totalPtr *float64
	if r.total > 0 {
		totalPtr = &r.total
	}
	var msgPtr *string
	if message != "" {
		msgPtr = &message
	}

	notification := mcp.NewProgressNotification(r.token, progress, totalPtr, msgPtr)

	// Convert typed params to map[string]any for SendNotificationToClient.
	paramsMap := map[string]any{
		"progressToken": r.token,
		"progress":      progress,
	}
	if totalPtr != nil {
		paramsMap["total"] = *totalPtr
	}
	if msgPtr != nil {
		paramsMap["message"] = *msgPtr
	}
	_ = notification // ensure the notification type stays validated at compile time

	if err := r.mcpServer.SendNotificationToClient(ctx, "notifications/progress", paramsMap); err != nil {
		r.logger.V(1).Info("failed to send progress notification",
			"error", err,
			"progress", progress,
			"total", r.total,
		)
	}
}

// sendLog sends a structured log message to the connected MCP client.
// This uses the MCP logging capability (notifications/message) for
// real-time log streaming during tool execution.
func (s *Server) sendLog(ctx context.Context, level mcp.LoggingLevel, loggerName, message string) {
	notification := mcp.LoggingMessageNotification{
		Notification: mcp.Notification{
			Method: "notifications/message",
		},
	}
	notification.Params.Level = level
	notification.Params.Logger = loggerName
	notification.Params.Data = message

	if err := s.mcpServer.SendLogMessageToClient(ctx, notification); err != nil {
		s.logger.V(1).Info("failed to send log to client",
			"error", err,
			"level", string(level),
			"message", message,
		)
	}
}
