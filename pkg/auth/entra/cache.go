// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

// TokenCache provides disk-based caching for access tokens via pkg/secrets.
// Tokens are stored encrypted and survive process restarts.
// Each scope has its own secret key for atomic updates and fault isolation.
type TokenCache struct {
	secretStore secrets.Store
}

// CachedToken is the structure stored on disk for each cached token.
type CachedToken struct {
	AccessToken string    `json:"accessToken"` //nolint:gosec // G117: not a hardcoded credential, stores runtime token data
	TokenType   string    `json:"tokenType"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Scope       string    `json:"scope"`
	CachedAt    time.Time `json:"cachedAt"`
	SessionID   string    `json:"sessionId,omitempty"`
}

// NewTokenCache creates a new disk-based token cache.
func NewTokenCache(secretStore secrets.Store) *TokenCache {
	return &TokenCache{
		secretStore: secretStore,
	}
}

// Get retrieves a token for the given scope from disk cache.
// Returns nil, nil if no token is cached for this scope.
// Returns nil, error if there was an error reading the cache.
func (c *TokenCache) Get(ctx context.Context, scope string) (*auth.Token, error) {
	key := c.scopeToKey(scope)

	data, err := c.secretStore.Get(ctx, key)
	if err != nil {
		// Check if it's a "not found" error - that's expected
		if errors.Is(err, secrets.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read cached token: %w", err)
	}

	var cached CachedToken
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached token: %w", err)
	}

	return &auth.Token{
		AccessToken: cached.AccessToken,
		TokenType:   cached.TokenType,
		ExpiresAt:   cached.ExpiresAt,
		Scope:       cached.Scope,
		CachedAt:    cached.CachedAt,
		SessionID:   cached.SessionID,
	}, nil
}

// Set stores a token for the given scope to disk cache.
func (c *TokenCache) Set(ctx context.Context, scope string, token *auth.Token) error {
	key := c.scopeToKey(scope)

	cached := CachedToken{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
		ExpiresAt:   token.ExpiresAt,
		Scope:       scope,
		CachedAt:    time.Now(),
		SessionID:   token.SessionID,
	}

	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("failed to marshal token for cache: %w", err)
	}

	if err := c.secretStore.Set(ctx, key, data); err != nil {
		return fmt.Errorf("failed to write cached token: %w", err)
	}

	return nil
}

// Delete removes a cached token for the given scope.
func (c *TokenCache) Delete(ctx context.Context, scope string) error {
	key := c.scopeToKey(scope)
	return c.secretStore.Delete(ctx, key)
}

// Clear removes all cached tokens by listing secrets with the token prefix
// and deleting them.
func (c *TokenCache) Clear(ctx context.Context) error {
	names, err := c.secretStore.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list secrets: %w", err)
	}

	var lastErr error
	for _, name := range names {
		if strings.HasPrefix(name, SecretKeyTokenPrefix) {
			if err := c.secretStore.Delete(ctx, name); err != nil {
				lastErr = err
			}
		}
	}

	return lastErr
}

// PurgeExpired removes all expired access tokens from the cache.
// Returns the number of tokens removed.
func (c *TokenCache) PurgeExpired(ctx context.Context) (int, error) {
	scopes, err := c.ListCachedScopes(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list cached scopes: %w", err)
	}

	purged := 0
	for _, scope := range scopes {
		token, err := c.Get(ctx, scope)
		if err != nil || token == nil {
			continue
		}
		if token.IsExpired() {
			if delErr := c.Delete(ctx, scope); delErr == nil {
				purged++
			}
		}
	}
	return purged, nil
}

// ListCachedScopes returns a list of scopes that have cached tokens.
func (c *TokenCache) ListCachedScopes(ctx context.Context) ([]string, error) {
	names, err := c.secretStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	scopes := make([]string, 0)
	for _, name := range names {
		if scope := c.keyToScope(name); scope != "" {
			scopes = append(scopes, scope)
		}
	}

	return scopes, nil
}

// scopeToKey converts a scope to a secret key.
// Uses base64url encoding for the scope to create a valid key.
func (c *TokenCache) scopeToKey(scope string) string {
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(scope))
	return SecretKeyTokenPrefix + encoded
}

// keyToScope converts a secret key back to a scope.
// Returns empty string if the key is not a valid token cache key.
func (c *TokenCache) keyToScope(key string) string {
	if !strings.HasPrefix(key, SecretKeyTokenPrefix) {
		return ""
	}
	encoded := strings.TrimPrefix(key, SecretKeyTokenPrefix)
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(decoded)
}
