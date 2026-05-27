// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRUWithTTL_New(t *testing.T) {
	t.Run("creates with default size", func(t *testing.T) {
		c := NewLRUWithTTL[string, string]()
		assert.NotNil(t, c)
		assert.Equal(t, defaultSize, c.size)
		assert.NotNil(t, c.store)
		assert.NotNil(t, c.order)
	})

	t.Run("creates with custom size", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		assert.NotNil(t, c)
		assert.Equal(t, 10, c.size)
	})

	t.Run("ignores non-positive size", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](-1))
		assert.Equal(t, defaultSize, c.size)
	})

	t.Run("creates with expiry buffer", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithExpiryBuffer[string, string](30 * time.Second))
		assert.Equal(t, 30*time.Second, c.expiryBuffer)
	})
}

func TestLRUWithTTL_Set(t *testing.T) {
	t.Run("new entry", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("key1", "token1", 1*time.Hour)

		assert.Contains(t, c.store, "key1")
		e := c.store["key1"].Value.(*entry[string, string])
		assert.Equal(t, "key1", e.key)
		assert.Equal(t, "token1", e.value)
		assert.WithinDuration(t, time.Now().Add(1*time.Hour), e.expiresAt, 5*time.Second)
		assert.Equal(t, 1, c.order.Len())
	})

	t.Run("update existing entry", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("key1", "old-token", 5*time.Minute)
		c.Set("key1", "new-token", 10*time.Minute)

		e := c.store["key1"].Value.(*entry[string, string])
		assert.Equal(t, "new-token", e.value)
		assert.Equal(t, 1, c.order.Len(), "update should not add a second list entry")
	})

	t.Run("update moves to front", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("key1", "t1", 5*time.Minute)
		c.Set("key2", "t2", 5*time.Minute)

		c.Set("key1", "t1-updated", 5*time.Minute)

		front := c.order.Front().Value.(*entry[string, string])
		assert.Equal(t, "key1", front.key)
	})

	t.Run("multiple entries", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("a", "ta", time.Minute)
		c.Set("b", "tb", time.Minute)
		c.Set("c", "tc", time.Minute)

		assert.Equal(t, 3, c.order.Len())
		assert.Len(t, c.store, 3)
	})
}

func TestLRUWithTTL_Get(t *testing.T) {
	t.Run("returns value for existing key", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("key1", "my-token", time.Hour)

		v, found := c.Get("key1")
		assert.True(t, found)
		assert.Equal(t, "my-token", v)
	})

	t.Run("returns false for missing key", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))

		v, found := c.Get("nonexistent")
		assert.False(t, found)
		assert.Equal(t, "", v)
	})

	t.Run("returns false for expired entry", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("key1", "expired-token", time.Millisecond)
		time.Sleep(5 * time.Millisecond)

		v, found := c.Get("key1")
		assert.False(t, found)
		assert.Equal(t, "", v)
		assert.NotContains(t, c.store, "key1")
		assert.Equal(t, 0, c.order.Len())
	})

	t.Run("expired entry with expiry buffer", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](
			WithSize[string, string](10),
			WithExpiryBuffer[string, string](time.Hour),
		)
		// Token TTL is 30min, but buffer is 1h — so it's already "expired"
		c.Set("key1", "buffered-token", 30*time.Minute)

		v, found := c.Get("key1")
		assert.False(t, found)
		assert.Equal(t, "", v)
	})

	t.Run("moves accessed entry to front", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("key1", "t1", time.Hour)
		c.Set("key2", "t2", time.Hour)
		// key2 is at front after Set
		c.Get("key1")
		// key1 should now be at front

		front := c.order.Front().Value.(*entry[string, string])
		assert.Equal(t, "key1", front.key)
	})

	t.Run("cleans up nil element in store", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		// Manually inject a nil element to simulate corruption
		c.store["bad-key"] = nil

		v, found := c.Get("bad-key")
		assert.False(t, found)
		assert.Equal(t, "", v)
		assert.NotContains(t, c.store, "bad-key", "nil element should be cleaned up")
	})
}

func TestLRUWithTTL_Eviction(t *testing.T) {
	t.Run("evicts oldest when over capacity", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](2))
		c.Set("key1", "t1", time.Minute)
		c.Set("key2", "t2", time.Minute)
		c.Set("key3", "t3", time.Minute) // should evict key1

		assert.Equal(t, 2, c.order.Len())
		assert.Len(t, c.store, 2)
		assert.NotContains(t, c.store, "key1", "oldest entry should be evicted")
		assert.Contains(t, c.store, "key2")
		assert.Contains(t, c.store, "key3")
	})

	t.Run("recently updated not evicted", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](2))
		c.Set("key1", "t1", time.Minute)
		c.Set("key2", "t2", time.Minute)
		// Update key1 — moves to front, key2 is now oldest
		c.Set("key1", "t1-updated", time.Minute)
		c.Set("key3", "t3", time.Minute) // should evict key2

		assert.NotContains(t, c.store, "key2", "key2 should be evicted as oldest")
		assert.Contains(t, c.store, "key1")
		assert.Contains(t, c.store, "key3")
	})

	t.Run("no eviction when under capacity", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](5))
		c.Set("key1", "t1", time.Minute)
		c.Set("key2", "t2", time.Minute)

		assert.Equal(t, 2, c.order.Len())
		assert.Len(t, c.store, 2)
	})

	t.Run("evicts down to size", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](1))
		c.Set("key1", "t1", time.Minute)
		c.Set("key2", "t2", time.Minute) // evicts key1

		assert.Equal(t, 1, c.order.Len())
		assert.Len(t, c.store, 1)
		assert.Contains(t, c.store, "key2")
	})
}

func TestLRUWithTTL_Delete(t *testing.T) {
	t.Run("removes existing key", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("k", "v", time.Minute)
		c.Delete("k")

		_, ok := c.Get("k")
		assert.False(t, ok)
		assert.Equal(t, 0, c.Len())
	})

	t.Run("noop for missing key", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Delete("nope") // should not panic
	})
}

func TestLRUWithTTL_Concurrency(t *testing.T) {
	t.Run("concurrent reads and writes", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](100))
		const goroutines = 20
		const ops = 500

		var wg sync.WaitGroup
		wg.Add(goroutines * 2)

		for i := range goroutines {
			go func(id int) {
				defer wg.Done()
				for j := range ops {
					key := fmt.Sprintf("key-%d-%d", id, j)
					c.Set(key, fmt.Sprintf("token-%d-%d", id, j), time.Minute)
				}
			}(i)
		}

		for i := range goroutines {
			go func(id int) {
				defer wg.Done()
				for j := range ops {
					key := fmt.Sprintf("key-%d-%d", id, j)
					c.Get(key)
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("concurrent writes same key", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		const goroutines = 50

		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := range goroutines {
			go func(id int) {
				defer wg.Done()
				c.Set("same-key", fmt.Sprintf("token-%d", id), time.Minute)
			}(i)
		}

		wg.Wait()
		assert.Contains(t, c.store, "same-key")
		assert.Equal(t, 1, c.order.Len(), "should have exactly 1 entry for same key")
	})

	t.Run("concurrent get during eviction", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](5))
		const writers = 10
		const readers = 10
		const ops = 200

		var wg sync.WaitGroup
		wg.Add(writers + readers)

		for i := range writers {
			go func(id int) {
				defer wg.Done()
				for j := range ops {
					c.Set(fmt.Sprintf("k-%d-%d", id, j), "t", time.Minute)
				}
			}(i)
		}

		for i := range readers {
			go func(id int) {
				defer wg.Done()
				for j := range ops {
					c.Get(fmt.Sprintf("k-%d-%d", id, j))
				}
			}(i)
		}

		wg.Wait()
		assert.LessOrEqual(t, c.order.Len(), 5, "should not exceed max size")
	})

	t.Run("no deadlock under contention", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		const goroutines = 100

		done := make(chan struct{})
		go func() {
			var wg sync.WaitGroup
			wg.Add(goroutines)
			for i := range goroutines {
				go func(id int) {
					defer wg.Done()
					for j := range 1000 {
						key := fmt.Sprintf("key-%d", j%20)
						if j%2 == 0 {
							c.Set(key, fmt.Sprintf("t-%d", id), time.Minute)
						} else {
							c.Get(key)
						}
					}
				}(i)
			}
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("deadlock detected: test did not complete within 10 seconds")
		}
	})

	t.Run("concurrent get during expiry cleanup", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](100))
		const goroutines = 20
		const ops = 200

		var wg1 sync.WaitGroup
		var wg2 sync.WaitGroup
		wg1.Add(goroutines)
		wg2.Add(goroutines)

		for i := range goroutines {
			go func(id int) {
				defer wg1.Done()
				for j := range ops {
					key := fmt.Sprintf("exp-%d-%d", id, j)
					c.Set(key, "short-lived", time.Millisecond)
				}
			}(i)
		}
		wg1.Wait()
		time.Sleep(2 * time.Millisecond)

		for i := range goroutines {
			go func(id int) {
				defer wg2.Done()
				for j := range ops {
					key := fmt.Sprintf("exp-%d-%d", id, j)
					v, found := c.Get(key)
					assert.False(t, found, "entry should be expired")
					assert.Equal(t, "", v)
				}
			}(i)
		}
		wg2.Wait()
	})
}

func TestLRUWithTTL_Cleanup(t *testing.T) {
	t.Run("removes expired keeps valid", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("expired1", "t1", time.Millisecond)
		c.Set("expired2", "t2", time.Millisecond)
		c.Set("valid", "t3", time.Hour)
		time.Sleep(5 * time.Millisecond)

		c.cleanup()

		assertCacheState(t, c, []string{"valid"})
	})

	t.Run("noop when nothing expired", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("a", "t1", time.Hour)
		c.Set("b", "t2", time.Hour)

		c.cleanup()

		assertCacheState(t, c, []string{"a", "b"})
	})

	t.Run("noop on empty cache", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.cleanup()
		assertCacheState(t, c, []string{})
	})

	t.Run("removes nil elements", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.store["bad"] = nil
		c.Set("good", "t1", time.Hour)

		c.cleanup()

		assertCacheState(t, c, []string{"good"})
	})

	t.Run("respects expiry buffer", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](
			WithSize[string, string](10),
			WithExpiryBuffer[string, string](time.Hour),
		)
		c.Set("buffered", "t1", 30*time.Minute)

		c.cleanup()

		assertCacheState(t, c, []string{})
	})

	t.Run("all expired empties cache", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		for i := range 5 {
			c.Set(fmt.Sprintf("k%d", i), "t", time.Millisecond)
		}
		time.Sleep(5 * time.Millisecond)

		c.cleanup()

		assertCacheState(t, c, []string{})
	})

	t.Run("periodic removes expired entries", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		c.Set("short1", "t1", 5*time.Millisecond)
		c.Set("short2", "t2", 5*time.Millisecond)
		c.Set("long", "t3", time.Hour)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go c.Cleanup(ctx, 10*time.Millisecond)

		time.Sleep(50 * time.Millisecond)
		assertCacheState(t, c, []string{"long"})
	})

	t.Run("periodic stops on context cancel", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](10))
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			c.Cleanup(ctx, 10*time.Millisecond)
			close(done)
		}()

		time.Sleep(50 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Cleanup goroutine did not exit after context cancel")
		}
	})

	t.Run("periodic concurrent with get set", func(t *testing.T) {
		c := NewLRUWithTTL[string, string](WithSize[string, string](100))
		const goroutines = 10
		const ops = 200

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go c.Cleanup(ctx, time.Millisecond)

		var wg sync.WaitGroup
		wg.Add(goroutines * 2)

		for i := range goroutines {
			go func(id int) {
				defer wg.Done()
				for j := range ops {
					c.Set(fmt.Sprintf("k-%d-%d", id, j), "t", time.Millisecond)
				}
			}(i)
		}

		for i := range goroutines {
			go func(id int) {
				defer wg.Done()
				for j := range ops {
					c.Get(fmt.Sprintf("k-%d-%d", id, j))
				}
			}(i)
		}

		wg.Wait()
	})
}

func TestLRUWithTTL_GenericTypes(t *testing.T) {
	t.Parallel()

	type customKey struct {
		Tenant string
		Scope  string
	}
	type customVal struct {
		Token string
		TTL   int
	}

	c := NewLRUWithTTL[customKey, customVal](WithSize[customKey, customVal](10))
	k := customKey{Tenant: "t1", Scope: "s1"}
	c.Set(k, customVal{Token: "tok", TTL: 60}, time.Minute)

	v, ok := c.Get(k)
	require.True(t, ok)
	assert.Equal(t, "tok", v.Token)
}

// ── Test helpers ──

func assertCacheState(t *testing.T, c *LRUWithTTL[string, string], expectedKeys []string) {
	t.Helper()
	c.mu.RLock()
	defer c.mu.RUnlock()

	assert.Equal(t, len(expectedKeys), len(c.store), "map size mismatch")
	assert.Equal(t, len(expectedKeys), c.order.Len(), "list size mismatch")
	for _, key := range expectedKeys {
		assert.Contains(t, c.store, key)
	}
}
