// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// DescriptorCache persists provider descriptors as JSON files so that
// schemas are available without spawning plugin processes. Each entry
// is keyed by provider name and expires after a configurable TTL.
//
// Cache layout:
//
//	<dir>/<name>.json
type DescriptorCache struct {
	dir string
	ttl time.Duration
}

// descriptorCacheEntry wraps a descriptor with a timestamp for TTL checks.
type descriptorCacheEntry struct {
	CachedAt   time.Time           `json:"cachedAt"`
	Descriptor provider.Descriptor `json:"descriptor"`
}

// NewDescriptorCache creates a cache backed by the given directory.
// If dir is empty, the default XDG path (paths.ProviderSchemaCacheDir()) is used.
// TTL controls how long entries are considered fresh; zero means entries never expire.
func NewDescriptorCache(dir string, ttl time.Duration) *DescriptorCache {
	if dir == "" {
		dir = paths.ProviderSchemaCacheDir()
	}
	return &DescriptorCache{dir: dir, ttl: ttl}
}

// Get retrieves a cached descriptor by provider name.
// Returns nil if the entry does not exist, has expired, or is invalid.
func (c *DescriptorCache) Get(name string) *provider.Descriptor {
	p, err := c.safePath(name)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var entry descriptorCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	if c.ttl > 0 && time.Since(entry.CachedAt) > c.ttl {
		return nil
	}
	// Validate required fields to prevent nil-pointer panics downstream.
	if entry.Descriptor.Name == "" || entry.Descriptor.Version == nil {
		return nil
	}
	return &entry.Descriptor
}

// Put stores a descriptor in the cache.
func (c *DescriptorCache) Put(name string, desc provider.Descriptor) error {
	p, err := c.safePath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	entry := descriptorCacheEntry{
		CachedAt:   time.Now(),
		Descriptor: desc,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// Invalidate removes a cached entry by provider name.
func (c *DescriptorCache) Invalidate(name string) {
	if p, err := c.safePath(name); err == nil {
		os.Remove(p)
	}
}

// InvalidateAll removes all cached entries.
func (c *DescriptorCache) InvalidateAll() {
	os.RemoveAll(c.dir)
}

// Dir returns the cache directory path.
func (c *DescriptorCache) Dir() string {
	return c.dir
}

// safePath returns the filesystem path for a provider name after validating
// that it does not contain path separators or traversal sequences.
func (c *DescriptorCache) safePath(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid provider name for cache key: %q", name)
	}
	p := filepath.Join(c.dir, name+".json")
	// Belt-and-suspenders: verify the cleaned path stays under c.dir.
	if !strings.HasPrefix(p, filepath.Clean(c.dir)+string(filepath.Separator)) {
		return "", fmt.Errorf("resolved cache path escapes cache directory: %q", name)
	}
	return p, nil
}
