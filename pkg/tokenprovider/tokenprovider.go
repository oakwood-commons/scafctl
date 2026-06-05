package tokenprovider

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package tokenprovider provides a unified token retrieval interface
// that abstracts runtime mode differences (CLI vs API). Consumers call
// GetToken without knowing whether the underlying mechanism is a CLI
// auth handler or an API token delegator.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider/callerscope"
)

// Sentinel errors.
var (
	// ErrNotAuthenticated indicates the caller is not authenticated.
	ErrNotAuthenticated = errors.New("tokenprovider: not authenticated")
	// ErrSourceNotFound indicates no token provider is registered under the requested name.
	ErrSourceNotFound = errors.New("tokenprovider: source not found")
	// ErrDuplicateSource indicates a source with the same name is already registered.
	ErrDuplicateSource = errors.New("tokenprovider: duplicate source name")
	// ErrNoRegistry indicates no token provider registry is present in the context.
	ErrNoRegistry = errors.New("tokenprovider: no registry in context")
)

// TokenProvider is the unified contract for retrieving tokens.
type TokenProvider interface {
	// Name returns the unique identifier for this token provider.
	Name() string
	// GetToken retrieves a token appropriate for the current runtime mode.
	GetToken(ctx context.Context, opts RequestOptions) (Token, error)
}

// Token is the mode-agnostic result of a token acquisition.
type Token struct {
	AccessToken string
	ExpiresAt   time.Time
	TokenType   string
}

// RequestOptions are consumer-provided parameters at the call site.
type RequestOptions struct {
	Scope        string
	ForceRefresh bool
	MinValidFor  time.Duration
	Caller       callerscope.CallerScope
}

func ExtractPassthroughTokenFromContext(ctx context.Context, provider string) (Token, bool) {
	canonicalizeProvider := http.CanonicalHeaderKey(provider)
	headerTokens := middleware.TokensFromContext(ctx)
	if headerTokens != nil {
		if token := headerTokens[canonicalizeProvider]; token != "" {
			return Token{AccessToken: token, TokenType: "Bearer"}, true
		}
	}
	return Token{}, false
}

// GetToken is the primary convenience function for consumers. It extracts
// the registry from context, looks up the named source, and retrieves a token.
func GetToken(ctx context.Context, provider string, opts RequestOptions) (Token, error) {
	reg := RegistryFromContext(ctx)
	if reg == nil {
		return Token{}, ErrNoRegistry
	}

	source, ok := reg.Get(provider)
	if !ok {
		return Token{}, fmt.Errorf("%w: %s", ErrSourceNotFound, provider)
	}

	return source.GetToken(ctx, opts)
}
