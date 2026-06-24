//go:build !windows

package diskcache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRemoveEntryPermissionError(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("requires non-root")
	}
	c := newTestCache(t, 1024)
	mustSet(t, c, k("key1"), []byte("data"))

	err := os.Chmod(c.baseDir, 0o555)
	require.NoError(t, err)
	t.Cleanup(func() { os.Chmod(c.baseDir, 0o755) }) //nolint:errcheck

	e := c.items[k("key1")].Value.(*entry)
	c.removeEntry(e, nil)

	assert.False(t, e.valid)
	assert.Contains(t, c.items, k("key1"))
}

func testEvictFromBackPermissionError(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("requires non-root")
	}
	c := newTestCache(t, 10)
	key := k("key1")
	e := &entry{key: key, size: 20, valid: true}
	c.items[key] = c.order.PushBack(e)
	c.currentSize += 20
	os.WriteFile(filepath.Join(c.baseDir, key.Path), []byte("data"), 0o644) //nolint:errcheck

	os.Chmod(c.baseDir, 0o555)                       //nolint:errcheck
	t.Cleanup(func() { os.Chmod(c.baseDir, 0o755) }) //nolint:errcheck

	c.evictFromBack(c.maxSize, nil, nil)

	assert.Contains(t, c.items, key)
	assert.False(t, e.valid)
	assert.Equal(t, int64(20), c.currentSize)
}

func testOnEvictedNotCalledOnPermissionError(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("requires non-root")
	}
	var evictedKeys []Key
	c := newTestCache(t, 1024, WithOnEviction(func(key Key) {
		evictedKeys = append(evictedKeys, key)
	}))
	key := k("key1")
	mustSet(t, c, key, []byte("data"))

	os.Chmod(c.baseDir, 0o555)                       //nolint:errcheck
	t.Cleanup(func() { os.Chmod(c.baseDir, 0o755) }) //nolint:errcheck

	c.mu.Lock()
	e := c.items[key].Value.(*entry)
	c.removeEntry(e, nil)
	c.mu.Unlock()

	assert.Empty(t, evictedKeys, "callback should not fire when os.Remove fails")
	assert.False(t, e.valid)
}

func testDeletePermissionError(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("requires non-root")
	}
	c := newTestCache(t, 1024)
	key := k("key1")
	mustSet(t, c, key, []byte("value1"))

	os.Chmod(c.baseDir, 0o555)                       //nolint:errcheck
	t.Cleanup(func() { os.Chmod(c.baseDir, 0o755) }) //nolint:errcheck
}
