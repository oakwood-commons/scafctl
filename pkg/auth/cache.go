// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

// TokenCache provides disk-based caching for access tokens via pkg/secrets.
// Tokens are stored encrypted and survive process restarts.
// Each (flow, fingerprint, scope) tuple has its own secret key for atomic updates
// and fault isolation. The fingerprint prevents cross-config cache contamination
// when users switch between different authentication configurations (e.g., different
// tenant IDs, client IDs, WIF audiences).
//
// TokenCache is shared by all auth handlers (entra, gcp, github). Each handler
// provides its own key prefix at construction time to namespace its entries.
type TokenCache struct {
	secretStore secrets.Store
	prefix      string
}

// CachedToken is the structure stored on disk for each cached token.
type CachedToken struct {
	AccessToken string    `json:"accessToken" yaml:"accessToken" doc:"The cached access token" maxLength:"65536"` //nolint:gosec // G117: not a hardcoded credential, stores runtime token data
	TokenType   string    `json:"tokenType" yaml:"tokenType" doc:"Token type, typically Bearer" example:"Bearer" maxLength:"64"`
	ExpiresAt   time.Time `json:"expiresAt" yaml:"expiresAt" doc:"Time the token expires"`
	Scope       string    `json:"scope" yaml:"scope" doc:"OAuth scope the token was issued for" maxLength:"1024"`
	Flow        Flow      `json:"flow,omitempty" yaml:"flow,omitempty" doc:"Authentication flow that produced this token" example:"device_code" maxLength:"64"`
	Fingerprint string    `json:"fingerprint,omitempty" yaml:"fingerprint,omitempty" doc:"Truncated SHA-256 hash of the config identity" maxLength:"128"`
	CachedAt    time.Time `json:"cachedAt" yaml:"cachedAt" doc:"Time the token was written to cache"`
	SessionID   string    `json:"sessionId,omitempty" yaml:"sessionId,omitempty" doc:"Stable identifier of the authentication session" maxLength:"128"`
}

// CacheEntry represents a unique cache slot identified by flow, fingerprint, and scope.
type CacheEntry struct {
	Flow        Flow   `json:"flow" yaml:"flow" doc:"Authentication flow for this cache entry"`
	Fingerprint string `json:"fingerprint" yaml:"fingerprint" doc:"Identity fingerprint for cache partitioning" maxLength:"128"`
	Scope       string `json:"scope" yaml:"scope" doc:"OAuth scope for this cache entry" maxLength:"1024"`
}

// NewTokenCache creates a new disk-based token cache.
// The prefix parameter namespaces cache entries per handler (e.g., "scafctl.auth.entra.token.").
func NewTokenCache(secretStore secrets.Store, prefix string) *TokenCache {
	return &TokenCache{
		secretStore: secretStore,
		prefix:      prefix,
	}
}

// Get retrieves a token for the given flow, fingerprint, and scope from disk cache.
// Returns nil, nil if no token is cached for this combination.
// Returns nil, error if there was an error reading the cache.
func (c *TokenCache) Get(ctx context.Context, flow Flow, fingerprint, scope string) (*Token, error) {
	key := c.CacheKey(flow, fingerprint, scope)

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

	return &Token{
		AccessToken: cached.AccessToken,
		TokenType:   cached.TokenType,
		ExpiresAt:   cached.ExpiresAt,
		Scope:       cached.Scope,
		Flow:        cached.Flow,
		CachedAt:    cached.CachedAt,
		SessionID:   cached.SessionID,
	}, nil
}

// Set stores a token for the given flow, fingerprint, and scope to disk cache.
func (c *TokenCache) Set(ctx context.Context, flow Flow, fingerprint, scope string, token *Token) error {
	key := c.CacheKey(flow, fingerprint, scope)

	cached := CachedToken{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
		ExpiresAt:   token.ExpiresAt,
		Scope:       scope,
		Flow:        flow,
		Fingerprint: fingerprint,
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

// Delete removes a cached token for the given flow, fingerprint, and scope.
func (c *TokenCache) Delete(ctx context.Context, flow Flow, fingerprint, scope string) error {
	key := c.CacheKey(flow, fingerprint, scope)
	return c.secretStore.Delete(ctx, key)
}

// Clear removes all cached tokens by listing secrets with the handler's token
// prefix and deleting them.
func (c *TokenCache) Clear(ctx context.Context) error {
	names, err := c.secretStore.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list secrets: %w", err)
	}

	var errs []error
	for _, name := range names {
		if strings.HasPrefix(name, c.prefix) {
			if err := c.secretStore.Delete(ctx, name); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

// PurgeExpired removes all expired access tokens from the cache.
// Returns the number of tokens removed.
func (c *TokenCache) PurgeExpired(ctx context.Context) (int, error) {
	entries, err := c.ListCachedEntries(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list cached entries: %w", err)
	}

	purged := 0
	var errs []error
	for _, entry := range entries {
		token, err := c.Get(ctx, entry.Flow, entry.Fingerprint, entry.Scope)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to read cached entry %s/%s: %w", entry.Flow, entry.Fingerprint, err))
			continue
		}
		if token == nil {
			continue
		}
		if token.IsExpired() {
			if delErr := c.Delete(ctx, entry.Flow, entry.Fingerprint, entry.Scope); delErr != nil {
				errs = append(errs, fmt.Errorf("failed to delete expired token %s/%s: %w", entry.Flow, entry.Fingerprint, delErr))
			} else {
				purged++
			}
		}
	}
	return purged, errors.Join(errs...)
}

// ListCachedEntries returns all cached (flow, fingerprint, scope) entries.
func (c *TokenCache) ListCachedEntries(ctx context.Context) ([]CacheEntry, error) {
	names, err := c.secretStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	entries := make([]CacheEntry, 0)
	for _, name := range names {
		if flow, fingerprint, scope, ok := c.ParseKey(name); ok {
			entries = append(entries, CacheEntry{Flow: flow, Fingerprint: fingerprint, Scope: scope})
		}
	}

	return entries, nil
}

// CacheKey builds the secret store key for a given flow, fingerprint, and scope.
// Format: <prefix><flow>.<fingerprint>.<base64url(scope)>
func (c *TokenCache) CacheKey(flow Flow, fingerprint, scope string) string {
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(scope))
	return c.prefix + string(flow) + "." + fingerprint + "." + encoded
}

// ParseKey extracts the flow, fingerprint, and scope from a secret key.
// Returns false if the key is not a valid token cache key for this cache's prefix.
func (c *TokenCache) ParseKey(key string) (Flow, string, string, bool) {
	if !strings.HasPrefix(key, c.prefix) {
		return "", "", "", false
	}
	rest := strings.TrimPrefix(key, c.prefix)
	// Format: <flow>.<fingerprint>.<base64url(scope)>
	parts := strings.SplitN(rest, ".", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	flow := Flow(parts[0])
	fingerprint := parts[1]
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(parts[2])
	if err != nil {
		return "", "", "", false
	}
	return flow, fingerprint, string(decoded), true
}

// Prefix returns the secret key prefix used by this TokenCache.
func (c *TokenCache) Prefix() string {
	return c.prefix
}
