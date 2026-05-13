// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package auth provides CLI commands for authentication management.
package auth

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/auth"
)

// handlerContextKey is used for test injection of handlers.
// This is package-private and only used for testing.
type handlerContextKey struct{}

// withTestHandler injects a handler into context for testing.
// This is not exported and should only be used in tests.
func withTestHandler(ctx context.Context, h auth.Handler) context.Context {
	return context.WithValue(ctx, handlerContextKey{}, h)
}

// handlerFromContext retrieves a test-injected handler from context.
// Returns nil if no handler was injected.
func handlerFromContext(ctx context.Context) auth.Handler {
	if h, ok := ctx.Value(handlerContextKey{}).(auth.Handler); ok {
		return h
	}
	return nil
}

// getHandler retrieves an auth handler from the registry or test context.
// For production use, it looks up the handler by name from the auth registry.
// For tests, it returns the test-injected handler.
func getHandler(ctx context.Context, handlerName string) (auth.Handler, error) {
	// Check for test-injected handler first
	if h := handlerFromContext(ctx); h != nil {
		return h, nil
	}

	registry := auth.RegistryFromContext(ctx)
	if registry == nil {
		return nil, fmt.Errorf("%w: no auth registry in context", auth.ErrHandlerNotFound)
	}
	return registry.Get(handlerName)
}

// listHandlers returns the names of all registered handlers.
// Falls back to the registry in context, or returns nil.
func listHandlers(ctx context.Context) []string {
	// If a test handler is injected, we can't enumerate all handlers.
	// Return the known built-in names for the test context.
	if h := handlerFromContext(ctx); h != nil {
		return []string{h.Name()}
	}

	registry := auth.RegistryFromContext(ctx)
	if registry == nil {
		return nil
	}
	return registry.List()
}

// isHandlerRegistered checks if a handler name is registered.
func isHandlerRegistered(ctx context.Context, name string) bool {
	// Test-injected handlers match any name (since tests inject a single mock)
	if h := handlerFromContext(ctx); h != nil {
		return true // Let the test handler respond regardless of name
	}

	registry := auth.RegistryFromContext(ctx)
	if registry == nil {
		return false
	}
	return registry.Has(name)
}

// validateHandlerName checks if a handler name is valid and returns a formatted error if not.
func validateHandlerName(ctx context.Context, handlerName string) error {
	if isHandlerRegistered(ctx, handlerName) {
		return nil
	}
	handlers := listHandlers(ctx)
	if len(handlers) == 0 {
		return fmt.Errorf("unknown auth handler: %s (no handlers registered)", handlerName)
	}
	return fmt.Errorf("unknown auth handler: %s (registered: %v)", handlerName, handlers)
}
