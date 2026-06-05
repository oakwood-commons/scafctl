// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package tokenprovider

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/auth"
)

// AuthHandlerAdapter wraps an auth.Handler as a TokenProvider for CLI mode.
type AuthHandlerAdapter struct {
	handler auth.Handler
}

// NewAuthHandlerAdapter creates a TokenProvider backed by a CLI auth handler.
func NewAuthHandlerAdapter(h auth.Handler) *AuthHandlerAdapter {
	return &AuthHandlerAdapter{handler: h}
}

// Name returns the handler's name.
func (a *AuthHandlerAdapter) Name() string { return a.handler.Name() }

// GetToken delegates to the underlying auth handler's GetToken method.
// It validates scope requirements based on handler capabilities:
// handlers with CapScopesOnTokenRequest require a non-empty scope,
// while handlers without that capability have the scope cleared.
func (a *AuthHandlerAdapter) GetToken(ctx context.Context, opts RequestOptions) (Token, error) {
	requiresScope := auth.HasCapability(a.handler.Capabilities(), auth.CapScopesOnTokenRequest)
	if opts.Scope == "" && requiresScope {
		return Token{}, fmt.Errorf("scope is required for auth provider %q (supports per-request scopes)", a.handler.Name())
	}
	scope := opts.Scope
	if !requiresScope {
		scope = ""
	}

	t, err := a.handler.GetToken(ctx, auth.TokenOptions{
		Scope:        scope,
		ForceRefresh: opts.ForceRefresh,
		MinValidFor:  opts.MinValidFor,
	})
	if err != nil {
		return Token{}, err
	}
	return Token{
		AccessToken: t.AccessToken,
		ExpiresAt:   t.ExpiresAt,
		TokenType:   t.TokenType,
	}, nil
}
