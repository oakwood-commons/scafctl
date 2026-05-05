// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// PluginCache manages a local content-addressed cache of plugin binaries.
//
// Cache layout:
//
//	<cacheDir>/<name>/<version>/<os>-<arch>/<name>[.exe]
//
// On Windows platforms, the binary has a ".exe" extension.
//
// Example:
//
//	~/.cache/scafctl/plugins/aws-provider/1.5.3/darwin-arm64/aws-provider
//	~/.cache/scafctl/plugins/aws-provider/1.5.3/windows-amd64/aws-provider.exe
type Cache struct {
	// dir is the root directory for cached plugin binaries.
	dir string
}

// NewCache creates a new Cache. If cacheDir is empty,
// the default XDG cache directory (paths.PluginCacheDir()) is used.
func NewCache(cacheDir string) *Cache {
	if cacheDir == "" {
		cacheDir = paths.PluginCacheDir()
	}
	return &Cache{dir: cacheDir}
}

// Dir returns the root cache directory.
func (c *Cache) Dir() string {
	return c.dir
}

// Get retrieves the path to a cached plugin binary. Returns the path and
// true if the binary exists and (optionally) matches the expected digest.
// If expectedDigest is empty, no digest verification is performed.
func (c *Cache) Get(name, version, platform, expectedDigest string) (string, bool) {
	p := c.binaryPath(name, version, platform)

	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		// Migration fallback: check legacy path without .exe for existing Windows caches.
		// Only attempt migration when the new path genuinely doesn't exist (not for
		// permission errors or other transient failures).
		if platformIsWindows(platform) && os.IsNotExist(err) {
			legacy := c.legacyBinaryPath(name, version, platform)
			if li, lerr := os.Stat(legacy); lerr == nil && !li.IsDir() {
				// Rename legacy binary to new .exe path for future lookups.
				if mkErr := os.MkdirAll(filepath.Dir(p), 0o755); mkErr != nil {
					return "", false
				}
				if os.Rename(legacy, p) == nil {
					info = li
				} else {
					return "", false
				}
			} else {
				return "", false
			}
		} else {
			return "", false
		}
	}

	// Verify executable permission (skip on Windows where permission bits are not meaningful)
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return "", false
	}

	// Verify digest if provided
	if expectedDigest != "" {
		actual, err := fileDigest(p)
		if err != nil {
			return "", false
		}
		if actual != expectedDigest {
			return "", false
		}
	}

	return p, true
}

// GetLatestCached returns the path to the newest cached binary for the given
// name and platform, regardless of version. Returns empty string and false if
// nothing is cached. This is used as a fallback when catalog version resolution
// fails (e.g., when running offline).
func (c *Cache) GetLatestCached(name, platform string) (string, string, bool) {
	entries, err := os.ReadDir(filepath.Join(c.dir, name))
	if err != nil {
		return "", "", false
	}

	var bestSemver *semver.Version
	var bestVersion string
	var bestPath string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		v := entry.Name()
		p := c.binaryPath(name, v, platform)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			// Migration fallback: check legacy path without .exe for older Windows caches.
			if platformIsWindows(platform) && os.IsNotExist(err) {
				legacy := c.legacyBinaryPath(name, v, platform)
				if li, lerr := os.Stat(legacy); lerr == nil && !li.IsDir() {
					if mkErr := os.MkdirAll(filepath.Dir(p), 0o755); mkErr == nil {
						if os.Rename(legacy, p) == nil {
							info = li
						} else {
							continue
						}
					} else {
						continue
					}
				} else {
					continue
				}
			} else {
				continue
			}
		}
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			continue
		}
		parsed, parseErr := semver.NewVersion(v)
		if parseErr != nil {
			// Not a valid semver directory — use lexicographic fallback.
			if bestSemver == nil && (bestVersion == "" || v > bestVersion) {
				bestVersion = v
				bestPath = p
			}
			continue
		}
		if bestSemver == nil || parsed.GreaterThan(bestSemver) {
			bestSemver = parsed
			bestVersion = v
			bestPath = p
		}
	}

	if bestPath == "" {
		return "", "", false
	}
	return bestPath, bestVersion, true
}

// Put writes a plugin binary to the cache. It creates the directory
// structure, writes the data, sets executable permissions, and returns
// the path to the cached binary.
func (c *Cache) Put(name, version, platform string, data []byte) (string, error) {
	p := c.binaryPath(name, version, platform)

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating plugin cache directory: %w", err)
	}

	// Check if already cached (another process may have finished first).
	// On Windows, permission bits are not meaningful so skip the execute check.
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		if runtime.GOOS == "windows" || info.Mode()&0o111 != 0 {
			return p, nil
		}
	}

	// Write to a uniquely-named temp file then rename for atomicity.
	// Using os.CreateTemp avoids collisions when multiple processes
	// cache the same plugin concurrently.
	tmpFile, err := os.CreateTemp(dir, filepath.Base(p)+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("creating temp file for plugin binary: %w", err)
	}
	tmp := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("writing plugin binary to cache: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("closing temp plugin binary: %w", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("setting plugin binary permissions: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("moving plugin binary into cache: %w", err)
	}

	return p, nil
}

// Digest computes the sha256 digest of a cached plugin binary.
// Returns the digest in "sha256:<hex>" format.
func (c *Cache) Digest(name, version, platform string) (string, error) {
	return fileDigest(c.binaryPath(name, version, platform))
}

// Remove deletes a cached plugin binary.
func (c *Cache) Remove(name, version, platform string) error {
	return os.RemoveAll(filepath.Dir(c.binaryPath(name, version, platform)))
}

// List returns all cached (name, version, platform) triples.
func (c *Cache) List() ([]CachedPlugin, error) {
	var results []CachedPlugin

	names, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading plugin cache: %w", err)
	}

	for _, nameEntry := range names {
		if !nameEntry.IsDir() {
			continue
		}
		versions, err := os.ReadDir(filepath.Join(c.dir, nameEntry.Name()))
		if err != nil {
			continue
		}
		for _, versionEntry := range versions {
			if !versionEntry.IsDir() {
				continue
			}
			platforms, err := os.ReadDir(filepath.Join(c.dir, nameEntry.Name(), versionEntry.Name()))
			if err != nil {
				continue
			}
			for _, platformEntry := range platforms {
				if !platformEntry.IsDir() {
					continue
				}
				platform := strings.ReplaceAll(platformEntry.Name(), "-", "/")
				bin := nameEntry.Name()
				if platformIsWindows(platform) {
					bin += ".exe"
				}
				binaryPath := filepath.Join(c.dir, nameEntry.Name(), versionEntry.Name(), platformEntry.Name(), bin)
				if info, err := os.Stat(binaryPath); err == nil && !info.IsDir() {
					results = append(results, CachedPlugin{
						Name:     nameEntry.Name(),
						Version:  versionEntry.Name(),
						Platform: platform,
						Path:     binaryPath,
						Size:     info.Size(),
					})
				}
			}
		}
	}

	return results, nil
}

// CachedPlugin describes a cached plugin binary.
type CachedPlugin struct {
	Name     string `json:"name" yaml:"name" doc:"Plugin name"`
	Version  string `json:"version" yaml:"version" doc:"Plugin version"`
	Platform string `json:"platform" yaml:"platform" doc:"Target platform (os/arch)"`
	Path     string `json:"path" yaml:"path" doc:"Absolute path to cached binary"`
	Size     int64  `json:"size" yaml:"size" doc:"Binary size in bytes"`
}

// binaryPath returns the expected path for a cached plugin binary.
// On Windows platforms, the binary name gets a ".exe" extension.
func (c *Cache) binaryPath(name, version, platform string) string {
	bin := name
	if platformIsWindows(platform) {
		bin += ".exe"
	}
	return filepath.Join(c.dir, name, version, PlatformCacheKey(platform), bin)
}

// legacyBinaryPath returns the pre-fix path without .exe extension (used for migration).
func (c *Cache) legacyBinaryPath(name, version, platform string) string {
	return filepath.Join(c.dir, name, version, PlatformCacheKey(platform), name)
}

// platformIsWindows returns true if the platform string targets Windows.
func platformIsWindows(platform string) bool {
	return strings.HasPrefix(platform, "windows/") || strings.HasPrefix(platform, "windows-")
}

// fileDigest computes the sha256 digest of a file.
func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file for digest: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum), nil
}
