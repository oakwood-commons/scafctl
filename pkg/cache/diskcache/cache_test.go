package diskcache

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCache(t *testing.T, maxSize int64, opts ...CacheOption) *Cache {
	t.Helper()
	cache, err := NewCache(t.TempDir(), maxSize, opts...)
	require.NoError(t, err)
	return cache
}

func mustSet(t *testing.T, c *Cache, key Key, data []byte) {
	t.Helper()
	require.NoError(t, c.Set(key, data))
}

func k(name string) Key {
	return Key{Key: name, Path: name + ".txt"}
}

func TestNewCache(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cache, err := NewCache(t.TempDir(), 1024)
		require.NoError(t, err)
		assert.Equal(t, int64(1024), cache.maxSize)
		assert.Equal(t, int64(0), cache.currentSize)
	})

	t.Run("invalid max size", func(t *testing.T) {
		cache, err := NewCache(t.TempDir(), -1)
		assert.Error(t, err)
		assert.Nil(t, cache)
	})
}

func TestCache_Set(t *testing.T) {
	t.Run("multiple keys", func(t *testing.T) {
		c := newTestCache(t, 1024)
		mustSet(t, c, k("key1"), []byte("value1"))
		mustSet(t, c, k("key2"), []byte("value2"))

		assert.Equal(t, int64(12), c.currentSize)
		assert.Equal(t, 2, c.order.Len())

		content, err := os.ReadFile(filepath.Join(c.baseDir, "key1.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("value1"), content)

		content, err = os.ReadFile(filepath.Join(c.baseDir, "key2.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("value2"), content)
	})

	t.Run("update existing", func(t *testing.T) {
		c := newTestCache(t, 1024)
		mustSet(t, c, k("key1"), []byte("value1"))

		mustSet(t, c, k("key1"), []byte("value1-updated"))

		assert.Equal(t, int64(len("value1-updated")), c.currentSize)
		content, err := os.ReadFile(filepath.Join(c.baseDir, "key1.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("value1-updated"), content)
	})
}

func TestCache_Get(t *testing.T) {
	t.Run("found and not found", func(t *testing.T) {
		c := newTestCache(t, 1024)
		mustSet(t, c, k("key1"), []byte("value1"))

		found, val := c.Get(k("key1"))
		assert.True(t, found)
		assert.Equal(t, []byte("value1"), val)

		found, val = c.Get(k("nonexistent"))
		assert.False(t, found)
		assert.Nil(t, val)
	})

	t.Run("evicts invalid entry", func(t *testing.T) {
		c := newTestCache(t, 1024)
		key := k("key1")
		err := os.WriteFile(filepath.Join(c.baseDir, key.Path), []byte("value1"), 0o644)
		require.NoError(t, err)

		e := &entry{key: key, valid: false}
		c.order.PushFront(e)
		c.items[key] = c.order.Front()

		found, val := c.Get(key)
		assert.False(t, found)
		assert.Nil(t, val)
		assert.NotContains(t, c.items, key)
		assert.Equal(t, 0, c.order.Len())
	})
}

func TestSkipWrite(t *testing.T) {
	t.Run("same hash — skip", func(t *testing.T) {
		c := newTestCache(t, 1024)
		c.hashFunc = func([]byte) (uint64, error) { return 123, nil }
		assert.True(t, c.skipWrite([]byte("x"), 123))
	})

	t.Run("different hash — no skip", func(t *testing.T) {
		c := newTestCache(t, 1024)
		c.hashFunc = func([]byte) (uint64, error) { return 123, nil }
		assert.False(t, c.skipWrite([]byte("x"), 456))
	})

	t.Run("nil hashFunc — no skip", func(t *testing.T) {
		c := newTestCache(t, 1024)
		assert.False(t, c.skipWrite([]byte("x"), 0))
	})

	t.Run("hash error — no skip", func(t *testing.T) {
		c := newTestCache(t, 1024)
		c.hashFunc = func([]byte) (uint64, error) { return 0, assert.AnError }
		assert.False(t, c.skipWrite([]byte("x"), 0))
	})
}

func TestCache_MaxEntrySize(t *testing.T) {
	t.Run("reject add over limit", func(t *testing.T) {
		c := newTestCache(t, 1024)
		c.maxEntrySize = 5
		err := c.Set(k("key1"), []byte("too large"))
		assert.ErrorIs(t, err, ErrEntryTooLarge)
		assert.NotContains(t, c.items, k("key1"))
	})

	t.Run("accept add under limit", func(t *testing.T) {
		c := newTestCache(t, 1024)
		c.maxEntrySize = 20
		mustSet(t, c, k("key1"), []byte("small"))
		assert.Contains(t, c.items, k("key1"))
	})

	t.Run("reject update over limit, removes entry", func(t *testing.T) {
		c := newTestCache(t, 1024)
		c.maxEntrySize = 10
		mustSet(t, c, k("key1"), []byte("small"))

		err := c.Set(k("key1"), []byte("too largeeee"))
		assert.ErrorIs(t, err, ErrEntryTooLarge)
		assert.NotContains(t, c.items, k("key1"))
	})
}

func TestRemoveEntry(t *testing.T) {
	t.Run("permission error marks invalid", func(t *testing.T) {
		testRemoveEntryPermissionError(t)
	})
}

func TestCache_EvictFromBack(t *testing.T) {
	t.Run("evicts LRU until under budget", func(t *testing.T) {
		c := newTestCache(t, 20)
		for i := 4; i >= 0; i-- {
			key := Key{Key: fmt.Sprintf("key%d", i), Path: fmt.Sprintf("file%d.txt", i)}
			e := &entry{key: key, size: 10, valid: true}
			c.items[key] = c.order.PushFront(e)
			c.currentSize += 10
			os.WriteFile(filepath.Join(c.baseDir, key.Path), []byte("0123456789"), 0o644) //nolint:errcheck
		}

		c.evictFromBack(c.maxSize, nil, nil)

		assert.LessOrEqual(t, c.currentSize, c.maxSize)
		assert.Equal(t, 2, c.order.Len())
		assert.Contains(t, c.items, Key{Key: "key0", Path: "file0.txt"})
		assert.Contains(t, c.items, Key{Key: "key1", Path: "file1.txt"})
	})

	t.Run("stops at protected element", func(t *testing.T) {
		c := newTestCache(t, 10)

		frontKey := Key{Key: "front", Path: "front.txt"}
		frontElem := c.order.PushFront(&entry{key: frontKey, size: 15, valid: true})
		c.items[frontKey] = frontElem
		c.currentSize += 15
		os.WriteFile(filepath.Join(c.baseDir, "front.txt"), []byte("0123456789abcde"), 0o644) //nolint:errcheck

		backKey := Key{Key: "back", Path: "back.txt"}
		c.items[backKey] = c.order.PushBack(&entry{key: backKey, size: 10, valid: true})
		c.currentSize += 10
		os.WriteFile(filepath.Join(c.baseDir, "back.txt"), []byte("0123456789"), 0o644) //nolint:errcheck

		c.evictFromBack(c.maxSize, frontElem, nil)

		assert.NotContains(t, c.items, backKey)
		assert.Contains(t, c.items, frontKey)
		assert.Equal(t, int64(15), c.currentSize)
	})

	t.Run("skips invalid entries", func(t *testing.T) {
		c := newTestCache(t, 10)

		validKey := k("valid")
		validElem := c.order.PushFront(&entry{key: validKey, size: 5, valid: true})
		c.items[validKey] = validElem
		c.currentSize += 5
		os.WriteFile(filepath.Join(c.baseDir, validKey.Path), []byte("12345"), 0o644) //nolint:errcheck

		invalidKey := k("invalid")
		c.items[invalidKey] = c.order.PushBack(&entry{key: invalidKey, size: 10, valid: false})
		c.currentSize += 10

		backKey := k("back")
		c.items[backKey] = c.order.PushBack(&entry{key: backKey, size: 5, valid: true})
		c.currentSize += 5
		os.WriteFile(filepath.Join(c.baseDir, backKey.Path), []byte("12345"), 0o644) //nolint:errcheck

		c.evictFromBack(c.maxSize, validElem, nil)

		assert.NotContains(t, c.items, backKey)
		assert.Contains(t, c.items, invalidKey)
	})

	t.Run("all invalid — no progress", func(t *testing.T) {
		c := newTestCache(t, 10)
		for i := 0; i < 3; i++ {
			key := Key{Key: fmt.Sprintf("key%d", i), Path: fmt.Sprintf("file%d.txt", i)}
			c.items[key] = c.order.PushBack(&entry{key: key, size: 10, valid: false})
			c.currentSize += 10
		}

		c.evictFromBack(c.maxSize, nil, nil)

		assert.Equal(t, int64(30), c.currentSize)
		assert.Equal(t, 3, c.order.Len())
	})

	t.Run("permission error marks invalid", func(t *testing.T) {
		testEvictFromBackPermissionError(t)
	})

	t.Run("skips pinned entry and sets pendingEvict", func(t *testing.T) {
		c := newTestCache(t, 10)

		pinnedKey := k("pinned")
		pinnedEntry := &entry{key: pinnedKey, size: 15, valid: true, refCount: 1}
		c.items[pinnedKey] = c.order.PushBack(pinnedEntry)
		c.currentSize += 15
		os.WriteFile(filepath.Join(c.baseDir, pinnedKey.Path), []byte("123456789012345"), 0o644) //nolint:errcheck

		c.evictFromBack(c.maxSize, nil, nil)

		// Entry not removed — still in map and list
		assert.Contains(t, c.items, pinnedKey)
		assert.Equal(t, 1, c.order.Len())
		assert.Equal(t, int64(15), c.currentSize)
		// File still on disk
		_, err := os.Stat(filepath.Join(c.baseDir, pinnedKey.Path))
		assert.NoError(t, err)
		// But marked for deferred eviction
		assert.True(t, pinnedEntry.pendingEvict)
	})
}

func TestCache_Concurrent(t *testing.T) {
	t.Run("parallel sets evict correctly", func(t *testing.T) {
		c := newTestCache(t, 10)
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				c.Set(Key{Key: fmt.Sprintf("key_%d", i), Path: fmt.Sprintf("file_%d.txt", i)}, []byte("valuevalue")) //nolint:errcheck
			}(i)
		}
		wg.Wait()

		foundCount := 0
		for i := 0; i < 10; i++ {
			if found, _ := c.Get(Key{Key: fmt.Sprintf("key_%d", i), Path: fmt.Sprintf("file_%d.txt", i)}); found {
				foundCount++
			}
		}
		assert.Equal(t, 1, foundCount)
	})

	t.Run("parallel sets and gets", func(t *testing.T) {
		c := newTestCache(t, 1024)
		var wg sync.WaitGroup

		// Writers
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := Key{Key: fmt.Sprintf("k%d", i), Path: fmt.Sprintf("f%d.txt", i)}
				c.Set(key, []byte(fmt.Sprintf("val%d", i))) //nolint:errcheck
			}(i)
		}

		// Readers (concurrent with writers)
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := Key{Key: fmt.Sprintf("k%d", i), Path: fmt.Sprintf("f%d.txt", i)}
				c.Get(key)
			}(i)
		}
		wg.Wait()

		assert.LessOrEqual(t, c.currentSize, c.maxSize)
	})

	t.Run("parallel updates same key", func(t *testing.T) {
		c := newTestCache(t, 1024)
		mustSet(t, c, k("shared"), []byte("initial"))

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				c.Set(k("shared"), []byte(fmt.Sprintf("update-%03d", i))) //nolint:errcheck
			}(i)
		}
		wg.Wait()

		found, val := c.Get(k("shared"))
		assert.True(t, found)
		assert.NotEmpty(t, val)
		assert.Equal(t, 1, c.order.Len())
	})

	t.Run("parallel sets under pressure", func(t *testing.T) {
		c := newTestCache(t, 50)
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := Key{Key: fmt.Sprintf("k%d", i), Path: fmt.Sprintf("f%d.txt", i)}
				c.Set(key, []byte(fmt.Sprintf("data-%04d", i))) //nolint:errcheck
			}(i)
		}
		wg.Wait()

		assert.LessOrEqual(t, c.currentSize, c.maxSize)
		// Verify all remaining entries are readable
		c.mu.Lock()
		for key := range c.items {
			elem := c.items[key]
			e := elem.Value.(*entry)
			if e.valid {
				path := filepath.Join(c.baseDir, key.Path)
				_, err := os.Stat(path)
				assert.NoError(t, err, "valid entry %s missing from disk", key.Key)
			}
		}
		c.mu.Unlock()
	})

	t.Run("mixed set get and update with eviction", func(t *testing.T) {
		c := newTestCache(t, 100)
		var wg sync.WaitGroup

		// Seed some entries
		for i := 0; i < 10; i++ {
			mustSet(t, c, Key{Key: fmt.Sprintf("seed%d", i), Path: fmt.Sprintf("seed%d.txt", i)}, []byte("seeddata!"))
		}

		// Hammer with sets, gets, and updates concurrently
		for i := 0; i < 50; i++ {
			wg.Add(3)
			go func(i int) {
				defer wg.Done()
				key := Key{Key: fmt.Sprintf("new%d", i), Path: fmt.Sprintf("new%d.txt", i)}
				c.Set(key, []byte(fmt.Sprintf("newval%04d", i))) //nolint:errcheck
			}(i)
			go func(i int) {
				defer wg.Done()
				key := Key{Key: fmt.Sprintf("seed%d", i%10), Path: fmt.Sprintf("seed%d.txt", i%10)}
				c.Get(key) //nolint:errcheck
			}(i)
			go func(i int) {
				defer wg.Done()
				key := Key{Key: fmt.Sprintf("seed%d", i%10), Path: fmt.Sprintf("seed%d.txt", i%10)}
				c.Set(key, []byte(fmt.Sprintf("updated%04d", i))) //nolint:errcheck
			}(i)
		}
		wg.Wait()

		assert.LessOrEqual(t, c.currentSize, c.maxSize)
	})
}

func TestCache_UpdateOverBudget(t *testing.T) {
	t.Run("grows entry, evicts others to make room", func(t *testing.T) {
		c := newTestCache(t, 20)
		mustSet(t, c, k("key1"), []byte("0123456789")) // 10 bytes
		mustSet(t, c, k("key2"), []byte("01234"))      // 5 bytes, total=15

		mustSet(t, c, k("key1"), []byte("012345678901234567")) // 18 bytes

		assert.Contains(t, c.items, k("key1"))
		assert.NotContains(t, c.items, k("key2"))
		assert.Equal(t, int64(18), c.currentSize)
	})

	t.Run("grows entry, eviction fails, returns ErrCacheFull", func(t *testing.T) {
		c := newTestCache(t, 10)
		mustSet(t, c, k("key1"), []byte("12345")) // 5 bytes

		bk := k("blocker")
		c.items[bk] = c.order.PushBack(&entry{key: bk, size: 8, valid: false})
		c.currentSize += 8 // total=13

		err := c.Set(k("key1"), []byte("123456789")) // sizeDiff=4
		assert.ErrorIs(t, err, ErrCacheFull)
		assert.NotContains(t, c.items, k("key1"))
	})

	t.Run("grows entry, evicts others behind it", func(t *testing.T) {
		c := newTestCache(t, 20)
		mustSet(t, c, k("key2"), []byte("bbbbbbbbbb")) // 10
		mustSet(t, c, k("key1"), []byte("aa"))         // 2, total=12

		// key1 grows to 19: sizeDiff=17, 12+17=29>20, target=20-17=3
		// evictFromBack(3, elem): key2 evicted, currentSize=2<=3, done.
		mustSet(t, c, k("key1"), []byte("aaaaaaaaaaaaaaaaaaa")) // 19

		assert.Contains(t, c.items, k("key1"))
		assert.NotContains(t, c.items, k("key2"))
		found, content := c.Get(k("key1"))
		assert.True(t, found)
		assert.Equal(t, []byte("aaaaaaaaaaaaaaaaaaa"), content)
		assert.LessOrEqual(t, c.currentSize, c.maxSize)
	})

	t.Run("shrink when over budget — allowed", func(t *testing.T) {
		c := newTestCache(t, 10)
		mustSet(t, c, k("key1"), []byte("12345678")) // 8 bytes
		c.currentSize = 15

		mustSet(t, c, k("key1"), []byte("abc")) // shrink to 3

		assert.Contains(t, c.items, k("key1"))
		assert.Equal(t, int64(10), c.currentSize)
	})

	t.Run("same size when over budget — allowed", func(t *testing.T) {
		c := newTestCache(t, 10)
		mustSet(t, c, k("key1"), []byte("12345")) // 5 bytes
		c.currentSize = 12

		err := c.Set(k("key1"), []byte("abcde"))
		require.NoError(t, err)
		assert.Contains(t, c.items, k("key1"))
	})
}

func TestCache_Get_PendingEvict(t *testing.T) {
	t.Run("removes entry when sole reader finishes and pendingEvict", func(t *testing.T) {
		c := newTestCache(t, 1024)
		key := k("key1")
		mustSet(t, c, key, []byte("value1"))

		// Mark pendingEvict with no other readers — Get will be the sole ref
		e := c.items[key].Value.(*entry)
		e.pendingEvict = true

		// Get: refCount 0→1 (read), then 1→0 (done) → triggers removeEntry
		found, val := c.Get(key)
		assert.True(t, found)
		assert.Equal(t, []byte("value1"), val)

		assert.NotContains(t, c.items, key)
		assert.Equal(t, int64(0), c.currentSize)
		_, err := os.Stat(filepath.Join(c.baseDir, key.Path))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("does not remove when other readers still hold ref", func(t *testing.T) {
		c := newTestCache(t, 1024)
		key := k("key1")
		mustSet(t, c, key, []byte("value1"))

		// Simulate another reader already holding a ref
		e := c.items[key].Value.(*entry)
		e.pendingEvict = true
		e.refCount = 1

		// Get: refCount 1→2 (read), then 2→1 (done) → refCount>0, NOT removed
		found, val := c.Get(key)
		assert.True(t, found)
		assert.Equal(t, []byte("value1"), val)

		assert.Contains(t, c.items, key, "entry should still exist, another reader holds it")
		assert.Equal(t, int64(6), c.currentSize)

		// Simulate last reader finishing
		c.mu.Lock()
		e.refCount--
		if e.pendingEvict && e.refCount == 0 {
			c.removeEntry(e, nil)
		}
		c.mu.Unlock()

		assert.NotContains(t, c.items, key)
		assert.Equal(t, int64(0), c.currentSize)
		_, err := os.Stat(filepath.Join(c.baseDir, key.Path))
		assert.True(t, os.IsNotExist(err))
	})
}

func TestPin(t *testing.T) {
	t.Run("key doesn't exits", func(t *testing.T) {
		c := newTestCache(t, 10)
		path, release, ok := c.Pin(Key{Key: "nonexistent", Path: "nonexistent.txt"})
		assert.False(t, ok)
		assert.Empty(t, path)
		assert.Nil(t, release)
	})

	t.Run("key exists", func(t *testing.T) {
		c := newTestCache(t, 10)
		key := k("key1")
		mustSet(t, c, key, []byte("value1"))

		path, release, ok := c.Pin(key)
		assert.True(t, ok)
		assert.Equal(t, filepath.Join(c.baseDir, key.Path), path)
		assert.NotNil(t, release)

		// Verify refCount incremented
		e := c.items[key].Value.(*entry)
		assert.Equal(t, 1, e.refCount)

		release()

		// Verify refCount decremented
		assert.Equal(t, 0, e.refCount)
	})

	t.Run("don't evict if entry pinned", func(t *testing.T) {
		bytes := []byte("value1")
		c := newTestCache(t, int64(len(bytes)))
		key := k("key1")
		mustSet(t, c, key, bytes)

		path, release, ok := c.Pin(key)
		assert.Equal(t, filepath.Join(c.baseDir, key.Path), path)
		require.True(t, ok)
		require.NotNil(t, release)
		e := c.items[key].Value.(*entry)
		assert.Equal(t, 1, e.refCount)
		assert.Contains(t, c.items, key)
		// Mark entry as pendingEvict and try to evict it — should skip and set pendingEvict

		c.evictFromBack(c.maxSize-1, nil, nil)
		assert.True(t, e.pendingEvict)
		release()
		assert.NotContains(t, c.items, key)
	})
}

func TestOnEvictedCallBack(t *testing.T) {
	t.Run("called on eviction", func(t *testing.T) {
		var evictedKeys []Key
		c := newTestCache(t, 10, WithOnEviction(func(key Key) {
			evictedKeys = append(evictedKeys, key)
		}))

		mustSet(t, c, k("key1"), []byte("value1"))
		mustSet(t, c, k("key2"), []byte("value2")) // should evict key1

		assert.Contains(t, evictedKeys, k("key1"))
		assert.NotContains(t, evictedKeys, k("key2"))
	})

	t.Run("called on eviction from back", func(t *testing.T) {
		var evictedKeys []Key
		c := newTestCache(t, 10, WithOnEviction(func(key Key) {
			evictedKeys = append(evictedKeys, key)
		}))

		for i := 0; i < 5; i++ {
			key := Key{Key: fmt.Sprintf("key%d", i), Path: fmt.Sprintf("file%d.txt", i)}
			e := &entry{key: key, size: 10, valid: true}
			c.currentSize += 10
			c.items[key] = c.order.PushFront(e)
		}
		c.evictFromBack(c.maxSize-1, nil, &evictedKeys)
		assert.Len(t, evictedKeys, 5)
	})

	t.Run("test eviction from get", func(t *testing.T) {
		var evictedKeys []Key
		c := newTestCache(t, 10, WithOnEviction(func(key Key) {
			evictedKeys = append(evictedKeys, key)
		}))

		key := k("key1")
		mustSet(t, c, key, []byte("value1"))

		// Mark entry as invalid to trigger eviction on Get
		e := c.items[key].Value.(*entry)
		e.valid = false

		found, _ := c.Get(key)
		assert.False(t, found)
		assert.Contains(t, evictedKeys, key)
	})

	t.Run("called on update with ErrEntryTooLarge", func(t *testing.T) {
		var evictedKeys []Key
		c := newTestCache(t, 1024, WithOnEviction(func(key Key) {
			evictedKeys = append(evictedKeys, key)
		}))
		c.maxEntrySize = 10
		mustSet(t, c, k("key1"), []byte("small"))

		err := c.Set(k("key1"), []byte("way too large!!"))
		assert.ErrorIs(t, err, ErrEntryTooLarge)
		assert.Contains(t, evictedKeys, k("key1"))
	})

	t.Run("called on update with ErrCacheFull", func(t *testing.T) {
		var evictedKeys []Key
		c := newTestCache(t, 10, WithOnEviction(func(key Key) {
			evictedKeys = append(evictedKeys, key)
		}))
		mustSet(t, c, k("key1"), []byte("12345")) // 5 bytes

		// Add invalid blocker that can't be evicted
		bk := k("blocker")
		c.items[bk] = c.order.PushBack(&entry{key: bk, size: 8, valid: false})
		c.currentSize += 8

		err := c.Set(k("key1"), []byte("123456789")) // sizeDiff=4, can't fit
		assert.ErrorIs(t, err, ErrCacheFull)
		assert.Contains(t, evictedKeys, k("key1"))
	})

	t.Run("called on pin release with pendingEvict", func(t *testing.T) {
		var evictedKeys []Key
		c := newTestCache(t, 10, WithOnEviction(func(key Key) {
			evictedKeys = append(evictedKeys, key)
		}))
		key := k("key1")
		mustSet(t, c, key, []byte("value1"))

		_, release, ok := c.Pin(key)
		require.True(t, ok)

		// Mark pendingEvict while pinned
		c.mu.Lock()
		e := c.items[key].Value.(*entry)
		e.pendingEvict = true
		c.mu.Unlock()

		assert.Empty(t, evictedKeys, "callback should not fire while pinned")

		release()

		assert.Contains(t, evictedKeys, key, "callback should fire after release")
	})

	t.Run("not called on permission error", func(t *testing.T) {
		testOnEvictedNotCalledOnPermissionError(t)
	})

	t.Run("called for evicted entries on update path, not the updated one", func(t *testing.T) {
		var evictedKeys []Key
		c := newTestCache(t, 20, WithOnEviction(func(key Key) {
			evictedKeys = append(evictedKeys, key)
		}))
		mustSet(t, c, k("key1"), []byte("0123456789")) // 10
		mustSet(t, c, k("key2"), []byte("01234"))      // 5, total=15

		// key1 grows to 18 → evicts key2
		mustSet(t, c, k("key1"), []byte("012345678901234567"))

		assert.Contains(t, evictedKeys, k("key2"))
		assert.NotContains(t, evictedKeys, k("key1"))
	})
}

func TestDelete(t *testing.T) {
	t.Run("removes existing key", func(t *testing.T) {
		c := newTestCache(t, 1024)
		key := k("key1")
		mustSet(t, c, key, []byte("value1"))

		removed := c.Delete(key)
		assert.True(t, removed)
		assert.NotContains(t, c.items, key)
		_, err := os.Stat(filepath.Join(c.baseDir, key.Path))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("returns false for non-existent key", func(t *testing.T) {
		c := newTestCache(t, 1024)
		removed := c.Delete(k("nonexistent"))
		assert.False(t, removed)
	})

	t.Run("handles permission error gracefully", func(t *testing.T) {
		testDeletePermissionError(t)
	})

	t.Run("removes pinned entry and sets pendingEvict", func(t *testing.T) {
		c := newTestCache(t, 1024)
		key := k("key1")
		mustSet(t, c, key, []byte("value1"))

		path, release, ok := c.Pin(key)
		require.True(t, ok)
		require.NotNil(t, release)
		assert.Equal(t, filepath.Join(c.baseDir, key.Path), path)

		removed := c.Delete(key)
		assert.True(t, removed)

		e := c.items[key].Value.(*entry)
		assert.True(t, e.pendingEvict)

		release()

		assert.NotContains(t, c.items, key)
	})
}

func TestAdopt(t *testing.T) {
	t.Run("adopts existing cache directory", func(t *testing.T) {
		baseDir := t.TempDir()
		cache1, err := NewCache(baseDir, 1024)
		require.NoError(t, err)

		mustSet(t, cache1, k("key1"), []byte("value1"))
		mustSet(t, cache1, k("key2"), []byte("value2"))

		cache2, err := NewCache(baseDir, 1024)
		// assert items and order empty before adopt
		assert.Empty(t, cache2.items)
		assert.Equal(t, 0, cache2.order.Len())

		key1 := Key{Key: "key1", Path: "key1.txt"}
		cache2.Adopt(key1)
		require.NoError(t, err)
		found, val := cache2.Get(k("key1"))
		assert.True(t, found)
		assert.Equal(t, []byte("value1"), val)

		key2 := Key{Key: "key2", Path: "key2.txt"}
		cache2.Adopt(key2)
		found, val = cache2.Get(k("key2"))
		assert.True(t, found)
		assert.Equal(t, []byte("value2"), val)
	})

	t.Run("validates entry if entry is invalid", func(t *testing.T) {
		baseDir := t.TempDir()
		cache, err := NewCache(baseDir, 1024)
		require.NoError(t, err)

		mustSet(t, cache, k("key1"), []byte("value1"))

		cache.mu.Lock()
		e := cache.items[k("key1")].Value.(*entry)
		e.valid = false
		cache.mu.Unlock()

		cache.adopt(k("key1"), int64(6))
		found, _ := cache.Get(k("key1"))
		assert.True(t, found)
		// assert entry is now valid
		assert.True(t, e.valid)
	})
}

func TestWarmUp(t *testing.T) {
	t.Run("loads existing entries into cache, and does not apply sizing limits", func(t *testing.T) {
		baseDir := t.TempDir()
		cache, err := NewCache(baseDir, 15)
		require.NoError(t, err)
		Key1 := k("key1")
		Key2 := k("key2")
		mustSet(t, cache, Key1, []byte("value1"))
		mustSet(t, cache, Key2, []byte("value2"))

		// Create new cache instance and warm up
		cache2, err := NewCache(baseDir, 10)
		require.NoError(t, err)
		err = cache2.WarmUp(func(path string) (key Key, ok bool) {
			name := path[:len(path)-len(filepath.Ext(path))]
			return k(name), true
		})
		require.NoError(t, err)

		found, val := cache2.Get(Key1)
		assert.True(t, found)
		assert.Equal(t, []byte("value1"), val)

		found, val = cache2.Get(Key2)
		assert.True(t, found)
		assert.Equal(t, []byte("value2"), val)
	})
}

func TestSetPin(t *testing.T) {
	t.Run("new entry", func(t *testing.T) {
		c := newTestCache(t, 1024)
		key := k("key1")
		data := []byte("value1")

		path, release, err := c.SetPin(key, data)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(c.baseDir, key.Path), path)
		assert.NotNil(t, release)

		// Verify written to disk
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, data, content)

		// Verify pinned (refCount incremented)
		e := c.items[key].Value.(*entry)
		assert.Equal(t, 1, e.refCount)
		assert.Equal(t, int64(len(data)), c.currentSize)

		release()
		assert.Equal(t, 0, e.refCount)
	})

	t.Run("update existing entry", func(t *testing.T) {
		c := newTestCache(t, 1024)
		key := k("key1")
		mustSet(t, c, key, []byte("old"))

		path, release, err := c.SetPin(key, []byte("new-value"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(c.baseDir, key.Path), path)

		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, []byte("new-value"), content)

		assert.Equal(t, int64(len("new-value")), c.currentSize)

		e := c.items[key].Value.(*entry)
		assert.Equal(t, 1, e.refCount)

		release()
		assert.Equal(t, 0, e.refCount)
	})

	t.Run("entry too large", func(t *testing.T) {
		c := newTestCache(t, 1024, WithMaxEntrySize(5))
		key := k("key1")

		path, release, err := c.SetPin(key, []byte("too-large-data"))
		assert.ErrorIs(t, err, ErrEntryTooLarge)
		assert.Empty(t, path)
		assert.Nil(t, release)
	})

	t.Run("cache full", func(t *testing.T) {
		c := newTestCache(t, 10)
		// Fill cache with a pinned entry so eviction can't free space
		key1 := k("key1")
		_, release1, err := c.SetPin(key1, []byte("1234567890"))
		require.NoError(t, err)
		defer release1()

		key2 := k("key2")
		path, release, err := c.SetPin(key2, []byte("overflow"))
		assert.ErrorIs(t, err, ErrCacheFull)
		assert.Empty(t, path)
		assert.Nil(t, release)
	})

	t.Run("evicts LRU to make room", func(t *testing.T) {
		c := newTestCache(t, 30)
		mustSet(t, c, k("old1"), []byte("1234567890"))
		mustSet(t, c, k("old2"), []byte("1234567890"))

		key := k("new")
		path, release, err := c.SetPin(key, []byte("1234567890-new"))
		require.NoError(t, err)
		assert.NotEmpty(t, path)
		defer release()

		// At least one old entry should have been evicted
		assert.True(t, c.currentSize <= c.maxSize)
	})

	t.Run("pinned entry not evicted", func(t *testing.T) {
		data := []byte("value1")
		c := newTestCache(t, int64(len(data)))
		key := k("key1")

		_, release, err := c.SetPin(key, data)
		require.NoError(t, err)

		// Try to evict — should be blocked by pin
		c.mu.Lock()
		c.evictFromBack(0, nil, nil)
		c.mu.Unlock()

		e := c.items[key].Value.(*entry)
		assert.True(t, e.pendingEvict)
		assert.Contains(t, c.items, key)

		// Release the pin — now eviction completes
		release()
		assert.NotContains(t, c.items, key)
	})

	t.Run("release is idempotent", func(t *testing.T) {
		c := newTestCache(t, 1024)
		key := k("key1")

		_, release, err := c.SetPin(key, []byte("value1"))
		require.NoError(t, err)

		release()
		release() // second call should be no-op
		release() // third call should be no-op

		e := c.items[key].Value.(*entry)
		assert.Equal(t, 0, e.refCount)
	})

	t.Run("concurrent SetPin same key", func(t *testing.T) {
		c := newTestCache(t, 1024)
		key := k("key1")

		var wg sync.WaitGroup
		releases := make([]func(), 10)
		errs := make([]error, 10)

		for i := range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, rel, err := c.SetPin(key, []byte(fmt.Sprintf("value-%d", i)))
				releases[i] = rel
				errs[i] = err
			}()
		}
		wg.Wait()

		for i := range 10 {
			assert.NoError(t, errs[i])
			if releases[i] != nil {
				releases[i]()
			}
		}

		// Entry should still exist with refCount 0
		e := c.items[key].Value.(*entry)
		assert.Equal(t, 0, e.refCount)
	})

	t.Run("eviction callback fires", func(t *testing.T) {
		var evictedKeys []Key
		c := newTestCache(t, 20, WithOnEviction(func(key Key) {
			evictedKeys = append(evictedKeys, key)
		}))
		mustSet(t, c, k("old"), []byte("1234567890"))

		_, release, err := c.SetPin(k("new"), []byte("1234567890-n"))
		require.NoError(t, err)
		defer release()

		assert.Contains(t, evictedKeys, k("old"))
	})
}
