// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// TokenAcquireFunc is a function that acquires a token given credentials and a scope (or cache key).
type TokenAcquireFunc[T any] func(ctx context.Context, creds T, scope string) (*Token, error)

// TokenCacheAccessor provides access to the token cache.
// All three handler packages (entra, gcp, github) satisfy this via their Handler structs.
type TokenCacheAccessor interface {
	GetTokenCache() *TokenCache
}

// GetCachedOrAcquireToken is a generic helper that handles the common pattern of:
// 1. Retrieving credentials
// 2. Checking the cache (unless ForceRefresh)
// 3. Acquiring a new token if needed
// 4. Caching the new token
//
// Parameters:
//   - ctx: context for cancellation and logging
//   - cache: the token cache to read/write from
//   - opts: token acquisition options (scope, min validity, force refresh flags)
//   - flow: the authentication flow type for cache partitioning
//   - cacheKey: the cache lookup key (typically opts.Scope; GitHub uses a fixed key)
//   - getCreds: retrieves credentials; returns (T, error) — return a nil error if
//     the credential source doesn't produce errors
//   - isCredsNil: returns true when the credential value indicates "not configured"
//   - getFingerprint: computes a cache-partitioning fingerprint from the credentials
//   - acquireToken: mints a fresh token using the credentials and scope
//   - logPrefix: short label for log messages (e.g. "SP", "WI", "pat")
func GetCachedOrAcquireToken[T any](
	ctx context.Context,
	cache *TokenCache,
	opts TokenOptions,
	flow Flow,
	cacheKey string,
	getCreds func() (T, error),
	isCredsNil func(T) bool,
	getFingerprint func(T) string,
	acquireToken TokenAcquireFunc[T],
	logPrefix string,
) (*Token, error) {
	lgr := logger.FromContext(ctx)

	creds, err := getCreds()
	if err != nil {
		return nil, err
	}
	if isCredsNil(creds) {
		return nil, ErrNotAuthenticated
	}

	fingerprint := getFingerprint(creds)

	// Apply default minimum validity to match the user-flow behavior.
	minValidFor := opts.MinValidFor
	if minValidFor == 0 {
		minValidFor = DefaultMinValidFor
	}

	// Check cache first (unless ForceRefresh)
	if !opts.ForceRefresh {
		cached, cacheErr := cache.Get(ctx, flow, fingerprint, cacheKey)
		if cacheErr == nil && cached != nil && cached.IsValidFor(minValidFor) {
			lgr.V(1).Info("using cached "+logPrefix+" token", "cacheKey", cacheKey)
			return cached, nil
		}
	}

	// Acquire new token
	token, err := acquireToken(ctx, creds, cacheKey)
	if err != nil {
		return nil, err
	}

	// Cache the token
	if cacheErr := cache.Set(ctx, flow, fingerprint, cacheKey, token); cacheErr != nil {
		lgr.V(1).Info("failed to cache "+logPrefix+" token", "error", cacheErr)
	}

	return token, nil
}
