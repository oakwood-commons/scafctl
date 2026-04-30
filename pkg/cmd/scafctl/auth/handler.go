// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package auth provides CLI commands for authentication management.
package auth

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/entra"
	gcpauth "github.com/oakwood-commons/scafctl/pkg/auth/gcp"
	ghauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
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

// getEntraHandlerWithOverrides creates an Entra handler with optional tenant and client ID overrides.
// The flags take precedence over config.
//
//nolint:dupl // Entra and GitHub handler construction share structure but use different types and config paths.
func getEntraHandlerWithOverrides(ctx context.Context, tenantOverride, clientIDOverride string) (auth.Handler, error) {
	// Check for test-injected handler
	if h := handlerFromContext(ctx); h != nil {
		return h, nil
	}

	entraCfg := &entra.Config{}
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Auth.Entra != nil {
		entraCfg.ClientID = cfg.Auth.Entra.ClientID
		entraCfg.TenantID = cfg.Auth.Entra.TenantID
		entraCfg.DefaultScopes = cfg.Auth.Entra.DefaultScopes
		entraCfg.DefaultFlow = cfg.Auth.Entra.DefaultFlow
	}

	applyOverride(&entraCfg.TenantID, tenantOverride)
	applyOverride(&entraCfg.ClientID, clientIDOverride)

	var opts []entra.Option
	if entraCfg.ClientID != "" || entraCfg.TenantID != "" || len(entraCfg.DefaultScopes) > 0 || entraCfg.DefaultFlow != "" {
		opts = append(opts, entra.WithConfig(entraCfg))
	}
	opts = append(opts, entra.WithLogger(*logger.FromContext(ctx)))

	return entra.New(opts...)
}

// getGitHubHandlerWithOverrides creates a GitHub handler with optional hostname and client ID overrides.
// The flags take precedence over config.
//
//nolint:dupl // GitHub and Entra handler construction share structure but use different types and config paths.
func getGitHubHandlerWithOverrides(ctx context.Context, hostnameOverride, clientIDOverride string) (auth.Handler, error) {
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
	opts = append(opts, ghauth.WithLogger(*logger.FromContext(ctx)))

	return ghauth.New(opts...)
}

// getGCPHandlerWithOverrides creates a GCP handler with optional client ID and impersonation overrides.
// The flags take precedence over config.
func getGCPHandlerWithOverrides(ctx context.Context, clientIDOverride, impersonateOverride string) (auth.Handler, error) {
	// Check for test-injected handler
	if h := handlerFromContext(ctx); h != nil {
		return h, nil
	}

	gcpCfg := &gcpauth.Config{}
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Auth.GCP != nil {
		gcpCfg.ClientID = cfg.Auth.GCP.ClientID
		gcpCfg.ClientSecret = cfg.Auth.GCP.ClientSecret
		gcpCfg.DefaultScopes = cfg.Auth.GCP.DefaultScopes
		gcpCfg.ImpersonateServiceAccount = cfg.Auth.GCP.ImpersonateServiceAccount
		gcpCfg.Project = cfg.Auth.GCP.Project
	}

	applyOverride(&gcpCfg.ClientID, clientIDOverride)
	applyOverride(&gcpCfg.ImpersonateServiceAccount, impersonateOverride)

	var opts []gcpauth.Option
	if gcpCfg.ClientID != "" || gcpCfg.ImpersonateServiceAccount != "" || len(gcpCfg.DefaultScopes) > 0 {
		opts = append(opts, gcpauth.WithConfig(gcpCfg))
	}
	opts = append(opts, gcpauth.WithLogger(*logger.FromContext(ctx)))

	return gcpauth.New(opts...)
}

// applyOverride sets the target to the override value if it is non-empty.
func applyOverride(target *string, override string) {
	if override != "" {
		*target = override
	}
}
