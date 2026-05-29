// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// AuthTokenResolver resolves fresh registry credentials by inferring
// the auth handler for a registry host, resolving the active profile,
// and acquiring a fresh token via BridgeAuthToRegistry.
type AuthTokenResolver struct {
	registry       *auth.Registry
	customHandlers []config.CustomOAuth2Config
}

// ResolverOption configures the AuthTokenResolver.
type ResolverOption func(*AuthTokenResolver)

// WithCustomHandlers sets the custom OAuth2 handler configs used for
// registry-to-handler inference.
func WithCustomHandlers(handlers []config.CustomOAuth2Config) ResolverOption {
	return func(r *AuthTokenResolver) { r.customHandlers = handlers }
}

// NewAuthTokenResolver creates a resolver that dynamically acquires fresh
// registry credentials from auth handlers.
func NewAuthTokenResolver(registry *auth.Registry, opts ...ResolverOption) *AuthTokenResolver {
	r := &AuthTokenResolver{registry: registry}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Resolve infers the auth handler for serverURL, resolves the active profile,
// and returns fresh credentials. Returns an error if no handler can be inferred
// or the handler is not registered/authenticated.
func (r *AuthTokenResolver) Resolve(ctx context.Context, serverURL string) (*Credential, error) {
	host := normalizeRegistryHost(serverURL)
	handlerName := catalog.InferAuthHandler(host, r.customHandlers)
	if handlerName == "" {
		return nil, fmt.Errorf("no auth handler for registry %q", serverURL)
	}

	handler, err := r.registry.Get(handlerName)
	if err != nil {
		return nil, fmt.Errorf("auth handler %q not available: %w", handlerName, err)
	}

	// Resolve profile and inject into context.
	profile := auth.ResolveActiveProfile(ctx, handlerName)
	if profile != "" {
		ctx = auth.WithProfile(ctx, profile)
	}

	scope := catalog.InferDefaultScope(host)
	username, password, err := catalog.BridgeAuthToRegistry(ctx, handler, host, scope)
	if err != nil {
		return nil, fmt.Errorf("bridge auth for %q: %w", serverURL, err)
	}

	return &Credential{
		ServerURL: serverURL,
		Username:  username,
		Secret:    password,
	}, nil
}

// normalizeRegistryHost extracts the bare host (with optional port) from a
// server URL that may include a scheme or path. Docker credential helpers
// receive URLs like "https://ghcr.io" but InferAuthHandler expects bare
// hosts like "ghcr.io".
func normalizeRegistryHost(serverURL string) string {
	s := strings.TrimSpace(serverURL)
	if s == "" {
		return s
	}
	// If there's a scheme, parse as URL.
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			return u.Host
		}
	}
	// Strip any path component from bare host.
	if idx := strings.Index(s, "/"); idx != -1 {
		s = s[:idx]
	}
	return s
}
