// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// cacheDirName is the subdirectory under the XDG cache dir holding resolved
// hostname inventories.
const cacheDirName = "hostname"

// cacheFilePerm and cacheDirPerm restrict cached inventories to the owner.
const (
	cacheFilePerm = 0o600
	cacheDirPerm  = 0o700
)

// cacheKey derives a stable, filesystem-safe cache key for a handler's
// resolver. It incorporates every input that affects the fetched inventory --
// the handler name, source URL, transform, auth provider/scope, and any static
// request headers -- so a change to any of them invalidates the entry and a
// same-URL inventory selected by a header or scope is never served stale.
func cacheKey(handler string, rc *config.HostnameResolverConfig) string {
	h := sha256.New()
	write := func(s string) {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	write(handler)
	write(rc.Source.URL)
	write(rc.Transform)
	write(rc.Source.AuthProvider)
	write(rc.Source.AuthScope)

	// Headers in sorted key order so the digest is independent of map iteration.
	headerKeys := make([]string, 0, len(rc.Source.Headers))
	for k := range rc.Source.Headers {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)
	for _, k := range headerKeys {
		write(k)
		write(rc.Source.Headers[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// diskCache is an on-disk InventoryCache honoring the resolver TTL across CLI
// invocations. A single-shot CLI cannot use an in-memory cache, so resolved
// inventories persist under the XDG cache dir.
type diskCache struct {
	baseDir string
	now     func() time.Time
}

// newDiskCache returns the default on-disk inventory cache.
func newDiskCache() *diskCache {
	return &diskCache{
		baseDir: filepath.Join(paths.CacheDir(), cacheDirName),
		now:     time.Now,
	}
}

// cacheFile is the on-disk representation of a cached inventory.
type cacheFile struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Entries   []Entry   `json:"entries"`
}

func (c *diskCache) path(key string) string {
	return filepath.Join(c.baseDir, key+".json")
}

// Get returns the cached entries when present and unexpired. Missing, corrupt,
// or expired files are treated as a miss (fail-open to re-fetch).
func (c *diskCache) Get(_ context.Context, key string) ([]Entry, bool) {
	data, err := os.ReadFile(c.path(key)) //nolint:gosec // key is a sha256 hex digest, path is fixed
	if err != nil {
		return nil, false
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, false
	}
	if !c.now().Before(cf.ExpiresAt) {
		_ = os.Remove(c.path(key))
		return nil, false
	}
	return cf.Entries, true
}

// peekAll returns the union of entries from every inventory cache file on disk,
// ignoring TTL expiry and without evicting anything. It backs display-only
// reverse-mapping (URL -> short selector) where the caller does not know which
// resolver produced the cache -- e.g. `auth status` labeling a cluster that was
// resolved via `kube login` under a different cache key. Missing dir or corrupt
// files are skipped.
func (c *diskCache) peekAll() []Entry {
	matches, err := filepath.Glob(filepath.Join(c.baseDir, "*.json"))
	if err != nil {
		return nil
	}
	var all []Entry
	for _, p := range matches {
		data, err := os.ReadFile(p) //nolint:gosec // p is a glob match under the fixed cache dir
		if err != nil {
			continue
		}
		var cf cacheFile
		if err := json.Unmarshal(data, &cf); err != nil {
			continue
		}
		all = append(all, cf.Entries...)
	}
	return all
}

// Set writes the entries with an expiry of now+ttl. Write failures are silent:
// caching is best-effort and must not break resolution.
func (c *diskCache) Set(_ context.Context, key string, entries []Entry, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	if err := os.MkdirAll(c.baseDir, cacheDirPerm); err != nil {
		return
	}
	cf := cacheFile{
		ExpiresAt: c.now().Add(ttl),
		Entries:   entries,
	}
	data, err := json.Marshal(cf)
	if err != nil {
		return
	}
	_ = os.WriteFile(c.path(key), data, cacheFilePerm)
}
