// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import (
	"context"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/cache"
)

type TokenCache[K comparable, V any] struct {
	lrucache *cache.LRUWithTTL[K, V]
}

func (c *TokenCache[K, V]) Get(_ context.Context, key K) (V, bool) {
	return c.lrucache.Get(key)
}

func (c *TokenCache[K, V]) Set(_ context.Context, key K, value V, ttl time.Duration) {
	c.lrucache.Set(key, value, ttl)
}

func NewTokenCache[K comparable, V any](ctx context.Context, size int, expiryBuffer, cleanUpInterval time.Duration) *TokenCache[K, V] {
	lruCache := cache.NewLRUWithTTL(
		cache.WithSize[K, V](size),
		cache.WithExpiryBuffer[K, V](expiryBuffer),
	)
	go lruCache.Cleanup(ctx, cleanUpInterval)
	return &TokenCache[K, V]{
		lrucache: lruCache,
	}
}
