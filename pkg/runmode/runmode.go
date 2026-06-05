// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package runmode provides execution mode signaling via context.
// It allows business logic to branch based on whether the application
// is running as a CLI, API server, or MCP server.
package runmode

import "context"

// Mode represents the execution mode of the application.
type Mode uint8

const (
	// CLI indicates the application is running as a command-line tool.
	CLI Mode = iota + 1
	// API indicates the application is running as an HTTP API server.
	API
	// TODO: Add MCP mode if needed for specific branching in MCP server context.
)

// String returns the lowercase name of the mode.
func (m Mode) String() string {
	switch m {
	case CLI:
		return "cli"
	case API:
		return "api"
	default:
		return "unknown"
	}
}

type contextKey struct{}

// WithMode returns a new context with the execution mode attached.
func WithMode(ctx context.Context, m Mode) context.Context {
	return context.WithValue(ctx, contextKey{}, m)
}

// FromContext retrieves the Mode from the context.
// Returns CLI as the default if no mode is present.
func FromContext(ctx context.Context) Mode {
	m, ok := ctx.Value(contextKey{}).(Mode)
	if !ok {
		return CLI
	}
	return m
}
