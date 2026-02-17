// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package auth provides CLI commands for authentication management.
package auth

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/entra"
	ghauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
	"github.com/oakwood-commons/scafctl/pkg/config"
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

// getEntraHandler creates or retrieves an Entra handler.
// If a handler was injected via context (for testing), returns that.
// Otherwise creates a new handler with configuration from context.
func getEntraHandler(ctx context.Context) (auth.Handler, error) {
	// Check for test-injected handler
	if h := handlerFromContext(ctx); h != nil {
		return h, nil
	}

	// Build options from config
	var opts []entra.Option
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Auth.Entra != nil {
		opts = append(opts, entra.WithConfig(&entra.Config{
			ClientID:      cfg.Auth.Entra.ClientID,
			TenantID:      cfg.Auth.Entra.TenantID,
			DefaultScopes: cfg.Auth.Entra.DefaultScopes,
		}))
	}

	return entra.New(opts...)
}

// getEntraHandlerWithOverrides creates an Entra handler with optional tenant and client ID overrides.
// The flags take precedence over config.
func getEntraHandlerWithOverrides(ctx context.Context, tenantOverride, clientIDOverride string) (auth.Handler, error) { //nolint:dupl // Entra and GitHub handlers have intentionally similar structure but different types
	// Check for test-injected handler
	if h := handlerFromContext(ctx); h != nil {
		return h, nil
	}

	entraCfg := &entra.Config{}
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Auth.Entra != nil {
		entraCfg.ClientID = cfg.Auth.Entra.ClientID
		entraCfg.TenantID = cfg.Auth.Entra.TenantID
		entraCfg.DefaultScopes = cfg.Auth.Entra.DefaultScopes
	}

	applyOverride(&entraCfg.TenantID, tenantOverride)
	applyOverride(&entraCfg.ClientID, clientIDOverride)

	var opts []entra.Option
	if entraCfg.ClientID != "" || entraCfg.TenantID != "" || len(entraCfg.DefaultScopes) > 0 {
		opts = append(opts, entra.WithConfig(entraCfg))
	}

	return entra.New(opts...)
}

// SupportedHandlers returns the list of supported auth handler names.
func SupportedHandlers() []string {
	return []string{"entra", "github"}
}

// IsSupportedHandler returns true if the handler name is supported.
func IsSupportedHandler(name string) bool {
	for _, h := range SupportedHandlers() {
		if h == name {
			return true
		}
	}
	return false
}

// getGitHubHandler creates or retrieves a GitHub handler.
// If a handler was injected via context (for testing), returns that.
// Otherwise creates a new handler with configuration from context.
func getGitHubHandler(ctx context.Context) (auth.Handler, error) {
	// Check for test-injected handler
	if h := handlerFromContext(ctx); h != nil {
		return h, nil
	}

	// Build options from config
	var opts []ghauth.Option
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Auth.GitHub != nil {
		opts = append(opts, ghauth.WithConfig(&ghauth.Config{
			ClientID:      cfg.Auth.GitHub.ClientID,
			Hostname:      cfg.Auth.GitHub.Hostname,
			DefaultScopes: cfg.Auth.GitHub.DefaultScopes,
		}))
	}

	return ghauth.New(opts...)
}

// getGitHubHandlerWithOverrides creates a GitHub handler with optional hostname and client ID overrides.
// The flags take precedence over config.
func getGitHubHandlerWithOverrides(ctx context.Context, hostnameOverride, clientIDOverride string) (auth.Handler, error) { //nolint:dupl // Entra and GitHub handlers have intentionally similar structure but different types
	// Check for test-injected handler
	if h := handlerFromContext(ctx); h != nil {
		return h, nil
	}

	ghCfg := &ghauth.Config{}
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Auth.GitHub != nil {
		ghCfg.ClientID = cfg.Auth.GitHub.ClientID
		ghCfg.Hostname = cfg.Auth.GitHub.Hostname
		ghCfg.DefaultScopes = cfg.Auth.GitHub.DefaultScopes
	}

	applyOverride(&ghCfg.Hostname, hostnameOverride)
	applyOverride(&ghCfg.ClientID, clientIDOverride)

	var opts []ghauth.Option
	if ghCfg.ClientID != "" || ghCfg.Hostname != "" || len(ghCfg.DefaultScopes) > 0 {
		opts = append(opts, ghauth.WithConfig(ghCfg))
	}

	return ghauth.New(opts...)
}

// applyOverride sets the target to the override value if it is non-empty.
func applyOverride(target *string, override string) {
	if override != "" {
		*target = override
	}
}

// getHandler creates a handler for the given handler name.
func getHandler(ctx context.Context, handlerName string) (auth.Handler, error) {
	switch handlerName {
	case "entra":
		return getEntraHandler(ctx)
	case "github":
		return getGitHubHandler(ctx)
	default:
		return nil, auth.ErrHandlerNotFound
	}
}
