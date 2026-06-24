// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0
package versionedcache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cache/diskcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCache(t *testing.T, maxSize int64, opts ...diskcache.CacheOption) *Cache {
	t.Helper()
	vc, err := New(t.TempDir(), maxSize, opts...)
	require.NoError(t, err)
	return vc
}

func TestNew(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		vc := newTestCache(t, 1024)
		assert.NotNil(t, vc.cache)
		assert.NotNil(t, vc.versions)
	})

	t.Run("invalid max size", func(t *testing.T) {
		_, err := New(t.TempDir(), -1)
		assert.Error(t, err)
	})
}

func TestCache_SetAndPin(t *testing.T) {
	t.Run("set then pin returns path", func(t *testing.T) {
		vc := newTestCache(t, 1024)
		data := []byte("binary-data")
		require.NoError(t, vc.Set("myplugin", "1.2.3", "darwin/arm64", data))

		path, release, ok := vc.Pin("myplugin", "1.2.3", "darwin/arm64")
		require.True(t, ok)
		defer release()

		assert.Contains(t, path, "myplugin/1.2.3/darwin-arm64/myplugin")
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, data, content)
	})

	t.Run("pin missing entry returns false", func(t *testing.T) {
		vc := newTestCache(t, 1024)
		_, _, ok := vc.Pin("missing", "1.0.0", "linux/amd64")
		assert.False(t, ok)
	})
}

func TestCache_Latest(t *testing.T) {
	t.Run("returns highest version", func(t *testing.T) {
		vc := newTestCache(t, 4096)
		require.NoError(t, vc.Set("plugin", "1.0.0", "linux/amd64", []byte("v1")))
		require.NoError(t, vc.Set("plugin", "2.0.0", "linux/amd64", []byte("v2")))
		require.NoError(t, vc.Set("plugin", "1.5.0", "linux/amd64", []byte("v15")))

		ver, ok := vc.Latest("plugin", "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "2.0.0", ver)
	})

	t.Run("different platforms are independent", func(t *testing.T) {
		vc := newTestCache(t, 4096)
		require.NoError(t, vc.Set("plugin", "3.0.0", "darwin/arm64", []byte("d3")))
		require.NoError(t, vc.Set("plugin", "1.0.0", "linux/amd64", []byte("l1")))

		ver, ok := vc.Latest("plugin", "darwin/arm64")
		require.True(t, ok)
		assert.Equal(t, "3.0.0", ver)

		ver, ok = vc.Latest("plugin", "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "1.0.0", ver)
	})

	t.Run("no entries returns false", func(t *testing.T) {
		vc := newTestCache(t, 1024)
		_, ok := vc.Latest("missing", "linux/amd64")
		assert.False(t, ok)
	})
}

func TestCache_Remove(t *testing.T) {
	t.Run("removes entry and updates index", func(t *testing.T) {
		vc := newTestCache(t, 4096)
		require.NoError(t, vc.Set("plugin", "1.0.0", "linux/amd64", []byte("v1")))
		require.NoError(t, vc.Set("plugin", "2.0.0", "linux/amd64", []byte("v2")))

		removed := vc.Delete("plugin", "2.0.0", "linux/amd64")
		assert.True(t, removed)

		ver, ok := vc.Latest("plugin", "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "1.0.0", ver)
	})

	t.Run("remove last version cleans up index", func(t *testing.T) {
		vc := newTestCache(t, 1024)
		require.NoError(t, vc.Set("plugin", "1.0.0", "linux/amd64", []byte("v1")))

		removed := vc.Delete("plugin", "1.0.0", "linux/amd64")
		assert.True(t, removed)

		_, ok := vc.Latest("plugin", "linux/amd64")
		assert.False(t, ok)

		vc.mu.RLock()
		_, exists := vc.versions[indexKey("plugin", "linux/amd64")]
		vc.mu.RUnlock()
		assert.False(t, exists)
	})

	t.Run("remove nonexistent returns false", func(t *testing.T) {
		vc := newTestCache(t, 1024)
		removed := vc.Delete("missing", "1.0.0", "linux/amd64")
		assert.False(t, removed)
	})
}

func TestCache_EvictionUpdatesIndex(t *testing.T) {
	t.Run("LRU eviction removes version from index", func(t *testing.T) {
		// maxSize=15 bytes; two entries of 10 bytes each forces eviction.
		vc := newTestCache(t, 15)
		require.NoError(t, vc.Set("plugin", "1.0.0", "linux/amd64", []byte("0123456789")))
		require.NoError(t, vc.Set("plugin", "2.0.0", "linux/amd64", []byte("0123456789")))

		// v1 should have been evicted to make room for v2.
		_, _, ok := vc.Pin("plugin", "1.0.0", "linux/amd64")
		assert.False(t, ok)

		ver, ok := vc.Latest("plugin", "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "2.0.0", ver)

		versions := vc.Versions("plugin", "linux/amd64")
		assert.Equal(t, []string{"2.0.0"}, versions)
	})
}

func TestCache_Versions(t *testing.T) {
	t.Run("returns sorted descending copy", func(t *testing.T) {
		vc := newTestCache(t, 4096)
		require.NoError(t, vc.Set("p", "1.0.0", "linux/amd64", []byte("a")))
		require.NoError(t, vc.Set("p", "3.0.0", "linux/amd64", []byte("c")))
		require.NoError(t, vc.Set("p", "2.0.0", "linux/amd64", []byte("b")))

		versions := vc.Versions("p", "linux/amd64")
		assert.Equal(t, []string{"3.0.0", "2.0.0", "1.0.0"}, versions)
	})

	t.Run("empty for unknown plugin", func(t *testing.T) {
		vc := newTestCache(t, 1024)
		versions := vc.Versions("missing", "linux/amd64")
		assert.Empty(t, versions)
	})
}

func TestCache_SetDuplicateVersion(t *testing.T) {
	vc := newTestCache(t, 4096)
	require.NoError(t, vc.Set("p", "1.0.0", "linux/amd64", []byte("first")))
	require.NoError(t, vc.Set("p", "1.0.0", "linux/amd64", []byte("second")))

	versions := vc.Versions("p", "linux/amd64")
	assert.Equal(t, []string{"1.0.0"}, versions)
}

func TestCache_SetInvalidSemver(t *testing.T) {
	vc := newTestCache(t, 1024)
	err := vc.Set("p", "not-a-version", "linux/amd64", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid semver version")

	// Nothing written to disk.
	_, _, ok := vc.Pin("p", "not-a-version", "linux/amd64")
	assert.False(t, ok)
}

func TestCache_BaseDir(t *testing.T) {
	dir := t.TempDir()
	vc, err := New(dir, 1024)
	require.NoError(t, err)
	assert.Equal(t, dir, vc.BaseDir())
}

func TestCache_WithFileMode(t *testing.T) {
	vc := newTestCache(t, 1024, diskcache.WithFileMode(0o755))
	require.NoError(t, vc.Set("p", "1.0.0", "linux/amd64", []byte("exec")))

	path, release, ok := vc.Pin("p", "1.0.0", "linux/amd64")
	require.True(t, ok)
	defer release()

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o111, "file should be executable")
}

func TestDiskKey(t *testing.T) {
	t.Run("plain name", func(t *testing.T) {
		key := diskKey("myplugin", "1.2.3", "darwin/arm64")
		assert.Equal(t, "myplugin\x001.2.3\x00darwin/arm64", key.Key)
		assert.Equal(t, "myplugin/1.2.3/darwin-arm64/myplugin", key.Path)
	})

	t.Run("composite name with registry hash", func(t *testing.T) {
		key := diskKey("myplugin/a1b2c3d4e5f67890", "1.2.3", "darwin/arm64")
		assert.Equal(t, "myplugin/a1b2c3d4e5f67890\x001.2.3\x00darwin/arm64", key.Key)
		assert.Equal(t, "myplugin/a1b2c3d4e5f67890/1.2.3/darwin-arm64/myplugin", key.Path)
	})
}

func TestParseDiskKey(t *testing.T) {
	t.Run("valid key", func(t *testing.T) {
		key := diskcache.Key{Key: "name\x001.0.0\x00linux/amd64"}
		name, version, platform, ok := parseDiskKey(key)
		assert.True(t, ok)
		assert.Equal(t, "name", name)
		assert.Equal(t, "1.0.0", version)
		assert.Equal(t, "linux/amd64", platform)
	})

	t.Run("malformed key", func(t *testing.T) {
		key := diskcache.Key{Key: "no-separators"}
		_, _, _, ok := parseDiskKey(key)
		assert.False(t, ok)
	})
}

func TestCache_FileOnDisk(t *testing.T) {
	vc := newTestCache(t, 4096)
	data := []byte("plugin-binary-content")
	require.NoError(t, vc.Set("auth-handler", "0.5.0", "darwin/arm64", data))

	expected := filepath.Join(vc.BaseDir(), "auth-handler", "0.5.0", "darwin-arm64", "auth-handler")
	content, err := os.ReadFile(expected)
	require.NoError(t, err)
	assert.Equal(t, data, content)
}

func TestParseRelativePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantName string
		wantVer  string
		wantPlat string
		wantOk   bool
	}{
		{"valid linux", "myplugin/1.2.3/linux-amd64/myplugin", "myplugin", "1.2.3", "linux/amd64", true},
		{"valid darwin", "aws-provider/2.0.0/darwin-arm64/aws-provider", "aws-provider", "2.0.0", "darwin/arm64", true},
		{"valid windows exe", "myplugin/1.0.0/windows-amd64/myplugin.exe", "myplugin", "1.0.0", "windows/amd64", true},
		{"too few segments", "myplugin/1.0.0/linux-amd64", "", "", "", false},
		{"too many segments", "a/b/c/d/e/f", "", "", "", false},
		{"binary mismatch", "myplugin/1.0.0/linux-amd64/other", "", "", "", false},
		{"no dash in platform", "myplugin/1.0.0/linuxamd64/myplugin", "", "", "", false},
		{"dash at start", "myplugin/1.0.0/-amd64/myplugin", "", "", "", false},
		{"dash at end", "myplugin/1.0.0/linux-/myplugin", "", "", "", false},
		// 5-segment paths with registry hash.
		{"valid with registry hash", "aws-provider/a1b2c3d4e5f67890/1.5.3/darwin-arm64/aws-provider", "aws-provider/a1b2c3d4e5f67890", "1.5.3", "darwin/arm64", true},
		{"valid with registry hash windows", "myplugin/deadbeef01234567/2.0.0/windows-amd64/myplugin.exe", "myplugin/deadbeef01234567", "2.0.0", "windows/amd64", true},
		{"5-segment binary mismatch", "myplugin/hash123/1.0.0/linux-amd64/other", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, ver, plat, ok := parseRelativePath(tt.path)
			assert.Equal(t, tt.wantOk, ok)
			if ok {
				assert.Equal(t, tt.wantName, name)
				assert.Equal(t, tt.wantVer, ver)
				assert.Equal(t, tt.wantPlat, plat)
			}
		})
	}
}

func TestCache_WarmUp(t *testing.T) {
	t.Run("populates index from existing files", func(t *testing.T) {
		dir := t.TempDir()

		// Pre-create plugin files on disk matching the expected layout.
		files := map[string][]byte{
			"plugin-a/1.0.0/linux-amd64/plugin-a":                  []byte("binary-a-1.0"),
			"plugin-a/2.0.0/linux-amd64/plugin-a":                  []byte("binary-a-2.0"),
			"plugin-b/0.5.0/darwin-arm64/plugin-b":                 []byte("binary-b-0.5"),
			"plugin-c/a1b2c3d4e5f67890/1.0.0/linux-amd64/plugin-c": []byte("binary-c-1.0-reg"),
			"plugin-c/a1b2c3d4e5f67890/2.0.0/linux-amd64/plugin-c": []byte("binary-c-2.0-reg"),
		}
		for path, data := range files {
			full := filepath.Join(dir, filepath.FromSlash(path))
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
			require.NoError(t, os.WriteFile(full, data, 0o755))
		}

		// Create cache and warm up.
		vc, err := New(dir, 4096)
		require.NoError(t, err)
		require.NoError(t, vc.WarmUp())

		// Version index should be populated.
		ver, ok := vc.Latest("plugin-a", "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "2.0.0", ver)

		ver, ok = vc.Latest("plugin-b", "darwin/arm64")
		require.True(t, ok)
		assert.Equal(t, "0.5.0", ver)

		versions := vc.Versions("plugin-a", "linux/amd64")
		assert.Equal(t, []string{"2.0.0", "1.0.0"}, versions)

		// Pin should work (file adopted into diskcache).
		path, release, ok := vc.Pin("plugin-a", "2.0.0", "linux/amd64")
		require.True(t, ok)
		defer release()
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, []byte("binary-a-2.0"), content)

		// Registry-hash composite name should also be indexed.
		ver, ok = vc.Latest("plugin-c/a1b2c3d4e5f67890", "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "2.0.0", ver)

		regVersions := vc.Versions("plugin-c/a1b2c3d4e5f67890", "linux/amd64")
		assert.Equal(t, []string{"2.0.0", "1.0.0"}, regVersions)
	})

	t.Run("skips non-matching files", func(t *testing.T) {
		dir := t.TempDir()

		// Create a file that doesn't match the plugin layout.
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "random"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "random", "file.txt"), []byte("junk"), 0o644))

		vc, err := New(dir, 1024)
		require.NoError(t, err)
		require.NoError(t, vc.WarmUp())

		// No versions indexed.
		_, ok := vc.Latest("random", "linux/amd64")
		assert.False(t, ok)
	})

	t.Run("empty directory", func(t *testing.T) {
		vc := newTestCache(t, 1024)
		require.NoError(t, vc.WarmUp())
		_, ok := vc.Latest("anything", "linux/amd64")
		assert.False(t, ok)
	})

	t.Run("over budget after warmup converges on next Set", func(t *testing.T) {
		dir := t.TempDir()

		// Create 20 bytes of plugins with maxSize=15.
		files := map[string][]byte{
			"p/1.0.0/linux-amd64/p": []byte("0123456789"), // 10 bytes
			"p/2.0.0/linux-amd64/p": []byte("0123456789"), // 10 bytes
		}
		for path, data := range files {
			full := filepath.Join(dir, filepath.FromSlash(path))
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
			require.NoError(t, os.WriteFile(full, data, 0o755))
		}

		vc, err := New(dir, 15)
		require.NoError(t, err)
		require.NoError(t, vc.WarmUp())

		// Both versions are tracked (temporarily over budget).
		versions := vc.Versions("p", "linux/amd64")
		assert.Equal(t, []string{"2.0.0", "1.0.0"}, versions)

		// A new Set triggers eviction of the oldest.
		require.NoError(t, vc.Set("p", "3.0.0", "linux/amd64", []byte("new")))

		// v1 should be evicted, v2 and v3 remain.
		versions = vc.Versions("p", "linux/amd64")
		assert.Contains(t, versions, "3.0.0")
		assert.Contains(t, versions, "2.0.0")
		assert.NotContains(t, versions, "1.0.0")
	})
}
