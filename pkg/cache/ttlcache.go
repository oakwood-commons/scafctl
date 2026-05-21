// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"container/list"
	"context"
	"sync"
	"time"
)

const defaultSize = 1024

// LRUWithTTL is a concurrency-safe, size-bounded LRU cache with per-entry TTL.
type LRUWithTTL[K comparable, V any] struct {
	mu           sync.RWMutex
	size         int
	expiryBuffer time.Duration // how much earlier than actual expiry an entry is considered stale
	store        map[K]*list.Element
	order        *list.List
}

type entry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
}

func (e *entry[K, V]) isExpired(buffer time.Duration) bool {
	return time.Now().After(e.expiresAt.Add(-buffer))
}

// Option configures an LRUWithTTL instance.
type Option[K comparable, V any] func(*LRUWithTTL[K, V])

// WithSize sets the maximum number of entries. Values <= 0 use the default (1024).
func WithSize[K comparable, V any](size int) Option[K, V] {
	return func(c *LRUWithTTL[K, V]) {
		if size > 0 {
			c.size = size
		}
	}
}

// WithExpiryBuffer sets how much earlier than actual expiry an entry is considered stale.
func WithExpiryBuffer[K comparable, V any](d time.Duration) Option[K, V] {
	return func(c *LRUWithTTL[K, V]) {
		c.expiryBuffer = d
	}
}

// NewLRUWithTTL creates a new LRU cache with TTL support.
func NewLRUWithTTL[K comparable, V any](opts ...Option[K, V]) *LRUWithTTL[K, V] {
	c := &LRUWithTTL[K, V]{
		size:  defaultSize,
		store: make(map[K]*list.Element),
		order: list.New(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get retrieves a value by key. Returns the zero value and false if the key
// is missing or expired.
func (c *LRUWithTTL[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	elem, exists := c.store[key]
	if !exists {
		c.mu.RUnlock()
		var zero V
		return zero, false
	}
	if elem == nil || elem.Value == nil {
		c.mu.RUnlock()
		c.removeBadEntry(key, elem)
		var zero V
		return zero, false
	}
	e, ok := elem.Value.(*entry[K, V])
	if !ok {
		c.mu.RUnlock()
		c.removeBadEntry(key, elem)
		var zero V
		return zero, false
	}
	if e.isExpired(c.expiryBuffer) {
		c.mu.RUnlock()
		c.removeEntry(key, elem)
		var zero V
		return zero, false
	}
	value := e.value
	c.mu.RUnlock()

	c.promote(elem)
	return value, true
}

// Set inserts or updates a key with the given value and TTL.
func (c *LRUWithTTL[K, V]) Set(key K, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, exists := c.store[key]; exists && elem != nil {
		c.order.MoveToFront(elem)
		if e, ok := elem.Value.(*entry[K, V]); ok {
			e.value = value
			e.expiresAt = time.Now().Add(ttl)
			return
		}
	}

	e := &entry[K, V]{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	elem := c.order.PushFront(e)
	c.store[key] = elem

	if c.order.Len() > c.size {
		c.evictOldest()
	}
}

// Delete removes a key from the cache.
func (c *LRUWithTTL[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, exists := c.store[key]; exists {
		if elem != nil {
			c.order.Remove(elem)
		}
		delete(c.store, key)
	}
}

// Len returns the number of entries currently in the cache (including expired but not yet cleaned).
func (c *LRUWithTTL[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}

// Cleanup runs periodic eviction of expired entries until the context is cancelled.
// The caller owns the goroutine lifecycle.
func (c *LRUWithTTL[K, V]) Cleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-ctx.Done():
			return
		}
	}
}

func (c *LRUWithTTL[K, V]) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, elem := range c.store {
		if elem == nil || elem.Value == nil {
			if elem != nil {
				c.order.Remove(elem)
			}
			delete(c.store, key)
			continue
		}
		e, ok := elem.Value.(*entry[K, V])
		if ok && e.isExpired(c.expiryBuffer) {
			c.order.Remove(elem)
			delete(c.store, key)
		}
	}
}

func (c *LRUWithTTL[K, V]) promote(elem *list.Element) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.MoveToFront(elem)
}

func (c *LRUWithTTL[K, V]) removeBadEntry(key K, elem *list.Element) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem != nil {
		c.order.Remove(elem)
	}
	delete(c.store, key)
}

func (c *LRUWithTTL[K, V]) removeEntry(key K, elem *list.Element) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Remove(elem)
	delete(c.store, key)
}

func (c *LRUWithTTL[K, V]) evictOldest() {
	oldest := c.order.Back()
	if oldest != nil {
		if e, ok := oldest.Value.(*entry[K, V]); ok {
			delete(c.store, e.key)
		}
		c.order.Remove(oldest)
	}
}
