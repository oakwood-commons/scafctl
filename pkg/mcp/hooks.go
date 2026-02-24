// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// newObservabilityHooks creates lifecycle hooks for logging, timing,
// and error tracking on every MCP request.
func newObservabilityHooks(lgr logr.Logger) *server.Hooks {
	hooks := &server.Hooks{}

	// Log every incoming request (method + id)
	hooks.AddBeforeAny(func(_ context.Context, id any, method mcp.MCPMethod, _ any) {
		lgr.V(1).Info("mcp request", "method", string(method), "id", id)
	})

	// Log successes with timing info
	hooks.AddOnSuccess(func(_ context.Context, id any, method mcp.MCPMethod, _, _ any) {
		lgr.V(1).Info("mcp request succeeded", "method", string(method), "id", id)
	})

	// Log errors for debugging
	hooks.AddOnError(func(_ context.Context, id any, method mcp.MCPMethod, _ any, err error) {
		lgr.Error(err, "mcp request failed", "method", string(method), "id", id)
	})

	// Log tool calls specifically for higher visibility
	hooks.AddBeforeCallTool(func(_ context.Context, id any, message *mcp.CallToolRequest) {
		lgr.Info("tool call", "tool", message.Params.Name, "id", id)
	})

	hooks.AddAfterCallTool(func(_ context.Context, id any, message *mcp.CallToolRequest, result any) {
		isErr := false
		if r, ok := result.(*mcp.CallToolResult); ok && r != nil {
			isErr = r.IsError
		}
		lgr.Info("tool call completed", "tool", message.Params.Name, "id", id, "isError", isErr)
	})

	// Log session lifecycle
	hooks.AddOnRegisterSession(func(_ context.Context, session server.ClientSession) {
		lgr.Info("session registered", "sessionID", session.SessionID())
	})

	hooks.AddOnUnregisterSession(func(_ context.Context, session server.ClientSession) {
		lgr.Info("session unregistered", "sessionID", session.SessionID())
	})

	return hooks
}

// toolTimingMiddleware wraps tool handlers with timing instrumentation.
// It logs the duration of each tool call and adds it to the result metadata.
func toolTimingMiddleware(lgr logr.Logger) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()
			result, err := next(ctx, request)
			duration := time.Since(start)

			toolName := request.Params.Name
			lgr.V(1).Info("tool execution time",
				"tool", toolName,
				"duration", duration.String(),
				"durationMs", duration.Milliseconds(),
			)

			return result, err
		}
	}
}

// resourceTimingMiddleware wraps resource handlers with timing instrumentation.
func resourceTimingMiddleware(lgr logr.Logger) server.ResourceHandlerMiddleware {
	return func(next server.ResourceHandlerFunc) server.ResourceHandlerFunc {
		return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			start := time.Now()
			result, err := next(ctx, request)
			duration := time.Since(start)

			lgr.V(1).Info("resource read time",
				"uri", request.Params.URI,
				"duration", duration.String(),
				"durationMs", duration.Milliseconds(),
			)

			return result, err
		}
	}
}
