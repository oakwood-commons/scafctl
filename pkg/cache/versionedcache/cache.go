// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package versionedcache

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/cache/diskcache"
)

// keySep separates name and platform in the version index key.
const keySep = "\x00"

// Cache wraps a diskcache.Cache with a semver version index.
// It maintains a sorted (descending) list of versions per name+platform,
// kept in sync automatically via the diskcache OnEviction callback.
type Cache struct {
	cache    *diskcache.Cache
	mu       sync.RWMutex
	versions map[string][]*semver.Version // indexKey -> sorted desc
}

// New creates a Cache backed by a bounded diskcache.
// The OnEviction callback is wired internally to keep the version index
// consistent when the diskcache evicts entries.
func New(cacheDir string, maxSize int64, opts ...diskcache.CacheOption) (*Cache, error) {
	vc := &Cache{
		versions: make(map[string][]*semver.Version),
	}
	// Append our eviction callback last so it cannot be overridden by caller opts.
	// The version index depends on this callback to stay consistent.
	allOpts := make([]diskcache.CacheOption, 0, len(opts)+1)
	allOpts = append(allOpts, opts...)
	allOpts = append(allOpts, diskcache.WithOnEviction(vc.onEvict))

	cache, err := diskcache.NewCache(cacheDir, maxSize, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating versioned cache: %w", err)
	}
	vc.cache = cache
	return vc, nil
}

// Set writes data into the diskcache and adds the version to the index.
func (vc *Cache) Set(name, version, platform string, data []byte) error {
	sv, err := semver.NewVersion(version)
	if err != nil {
		return fmt.Errorf("invalid semver version %q: %w", version, err)
	}
	key := diskKey(name, version, platform)
	if err := vc.cache.Set(key, data); err != nil {
		return err
	}
	vc.addVersion(name, platform, sv)
	return nil
}

// SetPin writes data, pins the entry atomically, and updates the version index.
// The entry cannot be evicted until the returned release function is called.
func (vc *Cache) SetPin(name, version, platform string, data []byte) (string, func(), error) {
	sv, err := semver.NewVersion(version)
	if err != nil {
		return "", nil, fmt.Errorf("invalid semver version %q: %w", version, err)
	}
	key := diskKey(name, version, platform)
	path, release, err := vc.cache.SetPin(key, data)
	if err != nil {
		return "", nil, err
	}
	vc.addVersion(name, platform, sv)
	return path, release, nil
}

// Pin marks a cache entry as in-use and returns its on-disk path.
// The entry cannot be evicted until the returned release function is called.
// The entry file can however be updated while holding the pin, in which case the path will point to the updated content.
// Returns ok=false if the entry is not in the cache.
func (vc *Cache) Pin(name, version, platform string) (path string, release func(), ok bool) {
	key := diskKey(name, version, platform)
	return vc.cache.Pin(key)
}

// Latest returns the highest cached semver version for a name+platform.
// Returns ok=false if nothing is indexed.
func (vc *Cache) Latest(name, platform string) (version string, ok bool) {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	ik := indexKey(name, platform)
	vs := vc.versions[ik]
	if len(vs) == 0 {
		return "", false
	}
	return vs[0].Original(), true
}

// Delete explicitly removes an entry from the diskcache.
// The version index is updated via the OnEviction callback.
func (vc *Cache) Delete(name, version, platform string) bool {
	key := diskKey(name, version, platform)
	return vc.cache.Delete(key)
}

// Versions returns all indexed versions for a name+platform, sorted descending.
// The returned slice is a copy.
func (vc *Cache) Versions(name, platform string) []string {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	ik := indexKey(name, platform)
	vs := vc.versions[ik]
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Original()
	}
	return out
}

// BaseDir returns the root directory of the underlying diskcache.
func (vc *Cache) BaseDir() string {
	return vc.cache.BaseDir()
}

// WarmUp scans the cache directory for existing plugin binaries and populates
// both the diskcache LRU and the version index without reading file contents.
// Files are adopted into the LRU without triggering eviction — the cache may
// temporarily exceed maxSize. Eviction occurs naturally on the next Set call.
func (vc *Cache) WarmUp() error {
	return vc.cache.WarmUp(func(relativePath string) (diskcache.Key, bool) {
		name, version, platform, ok := parseRelativePath(relativePath)
		if !ok {
			return diskcache.Key{}, false
		}
		if sv, err := semver.NewVersion(version); err == nil {
			vc.addVersion(name, platform, sv)
		}
		return diskKey(name, version, platform), true
	})
}

// parseRelativePath parses a relative path into its components.
// Supports two formats:
//   - 4 segments: "name/version/os-arch/binary" (no registry hash)
//   - 5 segments: "name/registryHash/version/os-arch/binary" (with registry hash)
//
// When a registry hash is present, the returned name is "name/registryHash"
// (a composite name). Platform is returned as "os/arch".
func parseRelativePath(relativePath string) (name, version, platform string, ok bool) {
	// Normalize to forward slashes for cross-platform consistency.
	normalized := filepath.ToSlash(relativePath)
	parts := strings.Split(normalized, "/")

	switch len(parts) {
	case 4:
		// name/version/os-arch/binary
		name = parts[0]
		version = parts[1]
		fsPlatform := parts[2]
		binary := parts[3]

		if strings.TrimSuffix(binary, ".exe") != name {
			return "", "", "", false
		}

		dashIdx := strings.Index(fsPlatform, "-")
		if dashIdx <= 0 || dashIdx == len(fsPlatform)-1 {
			return "", "", "", false
		}
		platform = fsPlatform[:dashIdx] + "/" + fsPlatform[dashIdx+1:]
		return name, version, platform, true

	case 5:
		// name/registryHash/version/os-arch/binary
		name = parts[0] + "/" + parts[1] // composite name
		version = parts[2]
		fsPlatform := parts[3]
		binary := parts[4]

		if strings.TrimSuffix(binary, ".exe") != parts[0] {
			return "", "", "", false
		}

		dashIdx := strings.Index(fsPlatform, "-")
		if dashIdx <= 0 || dashIdx == len(fsPlatform)-1 {
			return "", "", "", false
		}
		platform = fsPlatform[:dashIdx] + "/" + fsPlatform[dashIdx+1:]
		return name, version, platform, true

	default:
		return "", "", "", false
	}
}

// onEvict is the diskcache OnEviction callback. It parses the evicted key
// and removes the corresponding version from the index.
func (vc *Cache) onEvict(key diskcache.Key) {
	name, version, platform, ok := parseDiskKey(key)
	if !ok {
		return
	}
	sv, err := semver.NewVersion(version)
	if err != nil {
		return
	}
	vc.removeVersion(name, platform, sv)
}

// addVersion inserts a semver version into the index in sorted (descending) order.
// Duplicates are ignored.
func (vc *Cache) addVersion(name, platform string, v *semver.Version) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	ik := indexKey(name, platform)
	vs := vc.versions[ik]
	// Binary search for insertion point (sorted descending: greater versions first).
	idx := sort.Search(len(vs), func(i int) bool {
		return !vs[i].GreaterThan(v) // vs[i] <= v
	})
	// Duplicate check.
	if idx < len(vs) && vs[idx].Equal(v) {
		return
	}
	vs = append(vs, nil)
	copy(vs[idx+1:], vs[idx:])
	vs[idx] = v
	vc.versions[ik] = vs
}

// removeVersion removes a semver version from the index.
func (vc *Cache) removeVersion(name, platform string, v *semver.Version) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	ik := indexKey(name, platform)
	vs := vc.versions[ik]
	idx := sort.Search(len(vs), func(i int) bool {
		return !vs[i].GreaterThan(v)
	})
	if idx < len(vs) && vs[idx].Equal(v) {
		vc.versions[ik] = append(vs[:idx], vs[idx+1:]...)
		if len(vc.versions[ik]) == 0 {
			delete(vc.versions, ik)
		}
	}
}

// indexKey builds the version index map key.
func indexKey(name, platform string) string {
	return name + keySep + platform
}

// diskKey builds a diskcache.Key from name, version, and platform.
// The Key field encodes all components for round-tripping via parseDiskKey.
// The Path field matches the plugin cache layout.
//
// When name is a composite "pluginName/registryHash", the path includes the
// registry hash directory level and the binary name uses just the plugin name:
//
//	pluginName/registryHash/version/os-arch/pluginName
//
// When name is a plain string, the path is the original 4-segment layout:
//
//	name/version/os-arch/name
func diskKey(name, version, platform string) diskcache.Key {
	// Platform uses "os/arch" but filesystem uses "os-arch".
	fsPlatform := strings.ReplaceAll(platform, "/", "-")
	// Extract the bare plugin name for the binary filename.
	bareName := name
	if idx := strings.Index(name, "/"); idx >= 0 {
		bareName = name[:idx]
	}
	return diskcache.Key{
		Key:  name + keySep + version + keySep + platform,
		Path: fmt.Sprintf("%s/%s/%s/%s", name, version, fsPlatform, bareName),
	}
}

// parseDiskKey extracts name, version, and platform from a diskcache.Key.
func parseDiskKey(key diskcache.Key) (name, version, platform string, ok bool) {
	parts := strings.SplitN(key.Key, keySep, 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}
