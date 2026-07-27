// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/cespare/xxhash/v2"
	"github.com/oakwood-commons/scafctl/pkg/cache/diskcache"
	"github.com/oakwood-commons/scafctl/pkg/cache/versionedcache"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// registryHashPattern matches a 16-character lowercase hex string used as
// the registry hash directory level. Used to disambiguate registry-hash
// subdirs from version subdirs when scanning <name>/ contents.
var registryHashPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Cache manages a local content-addressed cache of plugin binaries.
//
// Cache layout (with optional registry hash):
//
//	<cacheDir>/<name>/[<registryHash>/]<version>/<os>-<arch>/<name>[.exe]
//
// The registry hash directory level is present only when a plugin is fetched
// from a catalog. Plugins installed without a catalog omit it.
// On Windows platforms, the binary has a ".exe" extension.
//
// Example:
//
//	~/.cache/scafctl/plugins/aws-provider/a1b2c3d4e5f67890/1.5.3/darwin-arm64/aws-provider
//	~/.cache/scafctl/plugins/my-local-plugin/1.0.0/darwin-arm64/my-local-plugin
type Cache struct {
	// dir is the root directory for cached plugin binaries.
	dir string
	// managed is the bounded LRU cache with pinning (API server mode).
	// When nil, the cache operates in unbounded CLI mode.
	managed *versionedcache.Cache
}

// CacheOption configures cache operations.
type CacheOption func(*cacheOpts)

type cacheOpts struct {
	registryHash string
}

func applyCacheOpts(opts []CacheOption) cacheOpts {
	var o cacheOpts
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithRegistryHash sets the registry hash directory level for cache lookups.
// When the hash is empty, this option is a no-op (no registry level is used).
func WithRegistryHash(hash string) CacheOption {
	return func(o *cacheOpts) {
		o.registryHash = hash
	}
}

// NewCache creates a new Cache. If cacheDir is empty,
// the default XDG cache directory (paths.PluginCacheDir()) is used.
func NewCache(cacheDir string) *Cache {
	if cacheDir == "" {
		cacheDir = paths.PluginCacheDir()
	}
	return &Cache{dir: cacheDir}
}

// NewManagedCache creates a Cache backed by a bounded LRU with pinning.
// This is intended for API server mode where concurrent access and eviction
// control are required. maxSize is the total byte budget for cached binaries.
// cacheDir should be derived from settings.PluginCacheDirFor(binaryName) to
// ensure embedders with custom binary names get isolated cache directories.
func NewManagedCache(cacheDir string, maxSize int64) (*Cache, error) {
	if cacheDir == "" {
		cacheDir = settings.PluginCacheDirFor(settings.CliBinaryName)
	}
	vc, err := versionedcache.New(cacheDir, maxSize,
		diskcache.WithFileMode(0o755),
		diskcache.WithHashFunc(xxhashSum),
	)
	if err != nil {
		return nil, fmt.Errorf("creating managed plugin cache: %w", err)
	}
	return &Cache{dir: cacheDir, managed: vc}, nil
}

// Dir returns the root cache directory.
func (c *Cache) Dir() string {
	return c.dir
}

// Pin marks a cached plugin binary as in-use and returns its path.
// The entry cannot be evicted until the returned release function is called.
// In CLI mode (unbounded), Pin stats the file and returns a no-op release.
func (c *Cache) Pin(name, version, platform string, opts ...CacheOption) (path string, release func(), ok bool) {
	o := applyCacheOpts(opts)
	if c.managed != nil {
		mn := managedName(name, o.registryHash)
		return c.managed.Pin(mn, version, platform)
	}
	p := c.binaryPath(name, version, platform, o.registryHash)
	if _, err := os.Stat(p); err != nil {
		return "", nil, false
	}
	return p, func() {}, true
}

// WarmUp scans the cache directory and populates the LRU and version index.
// No-op in CLI mode (unbounded cache).
func (c *Cache) WarmUp() error {
	if c.managed != nil {
		return c.managed.WarmUp()
	}
	return nil
}

// Get retrieves the path to a cached plugin binary. Returns the path and
// true if the binary exists and (optionally) matches the expected digest.
// If expectedDigest is empty, no digest verification is performed.
func (c *Cache) Get(name, version, platform, expectedDigest string, opts ...CacheOption) (string, bool) {
	o := applyCacheOpts(opts)
	if c.managed != nil {
		return c.getManaged(name, version, platform, expectedDigest, o.registryHash)
	}
	p, ok := c.statCachedBinary(name, version, platform, o.registryHash)
	if !ok {
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

// versionDir identifies a cached version directory and the registry-hash
// layout it belongs to. registryHash is empty for the flat (no-registry)
// layout and non-empty for catalog-installed plugins nested under a hash.
type versionDir struct {
	version      string
	registryHash string
}

// enumerateVersionDirs returns every cached version directory for a plugin
// name in an unmanaged cache. When registryHash is non-empty, only that
// registry's versions are returned. When empty, both the flat layout and every
// registry-hash layout are scanned. A directory whose name matches
// registryHashPattern is treated as a registry-hash layer; anything else is a
// direct version directory.
func (c *Cache) enumerateVersionDirs(name, registryHash string) []versionDir {
	if registryHash != "" {
		entries, err := os.ReadDir(filepath.Join(c.dir, name, registryHash))
		if err != nil {
			return nil
		}
		var dirs []versionDir
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, versionDir{version: e.Name(), registryHash: registryHash})
			}
		}
		return dirs
	}

	entries, err := os.ReadDir(filepath.Join(c.dir, name))
	if err != nil {
		return nil
	}
	var dirs []versionDir
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		if registryHashPattern.MatchString(dirName) {
			// Registry-hash layer — scan its version subdirectories.
			subEntries, err := os.ReadDir(filepath.Join(c.dir, name, dirName))
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() {
					dirs = append(dirs, versionDir{version: se.Name(), registryHash: dirName})
				}
			}
			continue
		}
		// Direct version directory (flat, no-registry layout).
		dirs = append(dirs, versionDir{version: dirName, registryHash: ""})
	}
	return dirs
}

// statCachedBinary resolves and validates the on-disk binary for an exact
// (name, version, platform, registryHash) tuple in an unmanaged cache. It
// performs the Windows legacy-path migration and verifies the executable
// permission bit, but does not perform digest verification. It returns the
// resolved path and true when a usable binary exists.
func (c *Cache) statCachedBinary(name, version, platform, registryHash string) (string, bool) {
	p := c.binaryPath(name, version, platform, registryHash)

	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		// Migration fallback: check legacy path without .exe for existing Windows caches.
		// Only attempt migration when the new path genuinely doesn't exist (not for
		// permission errors or other transient failures).
		if !platformIsWindows(platform) || !os.IsNotExist(err) {
			return "", false
		}
		legacy := c.legacyBinaryPath(name, version, platform, registryHash)
		li, lerr := os.Stat(legacy)
		if lerr != nil || li.IsDir() {
			return "", false
		}
		// Rename legacy binary to new .exe path for future lookups.
		if mkErr := os.MkdirAll(filepath.Dir(p), 0o755); mkErr != nil {
			return "", false
		}
		if os.Rename(legacy, p) != nil {
			return "", false
		}
		info = li
	}

	// Verify executable permission. Skip when the target platform is Windows
	// (its binaries carry no Unix executable bit) or when the host OS is
	// Windows (permission bits are not meaningful there). Using the target
	// platform — not just the host — keeps cross-platform cache inspection
	// (e.g. validating a windows/amd64 entry on a non-Windows host) correct.
	if !platformIsWindows(platform) && runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return "", false
	}

	return p, true
}

// ResolveVersion returns the on-disk path to a cached plugin binary for an
// exact version, searching across both the flat (no-registry) cache layout and
// every registry-hash layout. This lets callers that do not know a plugin's
// catalog registry hash still resolve catalog-installed plugins, which live
// under <name>/<registryHash>/<version>/... rather than the flat
// <name>/<version>/... layout.
//
// When the same version resolves under multiple registry layouts with
// differing binary contents, the result is ambiguous and an error is returned
// rather than silently choosing one. Identical binaries (matching digests)
// resolve to a single match.
func (c *Cache) ResolveVersion(name, version, platform string) (string, bool, error) {
	if c.managed != nil {
		// The managed (API server) cache is keyed per registry namespace and
		// does not support cross-registry enumeration; fall back to the flat
		// lookup to preserve existing behavior.
		path, ok := c.getManaged(name, version, platform, "", "")
		return path, ok, nil
	}

	var matches []string
	// Probe the flat (no-registry) layout directly plus every registry-hash
	// layer present for this plugin. The flat layout is always probed so a
	// version that happens to look like a registry hash (16 lowercase hex) is
	// never misclassified as a registry layer and lost.
	registryHashes := []string{""}
	if entries, err := os.ReadDir(filepath.Join(c.dir, name)); err == nil {
		for _, e := range entries {
			if e.IsDir() && registryHashPattern.MatchString(e.Name()) {
				registryHashes = append(registryHashes, e.Name())
			}
		}
	}

	seen := make(map[string]bool)
	for _, rh := range registryHashes {
		if p, ok := c.statCachedBinary(name, version, platform, rh); ok && !seen[p] {
			seen[p] = true
			matches = append(matches, p)
		}
	}

	switch len(matches) {
	case 0:
		return "", false, nil
	case 1:
		return matches[0], true, nil
	default:
		identical, err := identicalDigests(matches)
		if err != nil {
			return "", false, err
		}
		if identical {
			return matches[0], true, nil
		}
		return "", false, fmt.Errorf(
			"cached plugin %q version %q resolves to %d differing binaries across catalog registries; remove the duplicate cache entries or reinstall from a single catalog",
			name, version, len(matches),
		)
	}
}

// identicalDigests reports whether all files at the given paths share the same
// sha256 digest. Fewer than two paths are trivially identical.
func identicalDigests(paths []string) (bool, error) {
	if len(paths) < 2 {
		return true, nil
	}
	first, err := fileDigest(paths[0])
	if err != nil {
		return false, fmt.Errorf("digesting %s: %w", paths[0], err)
	}
	for _, p := range paths[1:] {
		d, err := fileDigest(p)
		if err != nil {
			return false, fmt.Errorf("digesting %s: %w", p, err)
		}
		if d != first {
			return false, nil
		}
	}
	return true, nil
}

// GetLatestCached returns the path to the newest cached binary for the given
// name and platform, regardless of version. When WithRegistryHash is provided,
// only that registry's versions are searched. Otherwise, all versions across
// all registry hashes (and the no-registry path) are considered.
// Returns empty string and false if nothing is cached.
func (c *Cache) GetLatestCached(name, platform string, opts ...CacheOption) (string, string, bool) {
	o := applyCacheOpts(opts)
	if c.managed != nil {
		return c.getLatestCachedManaged(name, platform, o.registryHash)
	}

	versionDirs := c.enumerateVersionDirs(name, o.registryHash)

	var bestSemver *semver.Version
	var bestVersion string
	var bestPath string
	for _, vd := range versionDirs {
		p, ok := c.statCachedBinary(name, vd.version, platform, vd.registryHash)
		if !ok {
			continue
		}
		parsed, parseErr := semver.NewVersion(vd.version)
		if parseErr != nil {
			// Not a valid semver directory — use lexicographic fallback.
			if bestSemver == nil && (bestVersion == "" || vd.version > bestVersion) {
				bestVersion = vd.version
				bestPath = p
			}
			continue
		}
		if bestSemver == nil || parsed.GreaterThan(bestSemver) {
			bestSemver = parsed
			bestVersion = vd.version
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
func (c *Cache) Put(name, version, platform string, data []byte, opts ...CacheOption) (string, error) {
	o := applyCacheOpts(opts)
	if c.managed != nil {
		return c.putManaged(name, version, platform, o.registryHash, data)
	}
	p := c.binaryPath(name, version, platform, o.registryHash)

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating plugin cache directory: %w", err)
	}

	// Check if already cached (another process may have finished first).
	// If the existing file matches exactly, reuse it. Otherwise, overwrite it
	// below to recover from stale or corrupted cache entries.
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		existing, readErr := os.ReadFile(p)
		if readErr == nil && bytes.Equal(existing, data) {
			if runtime.GOOS == "windows" || info.Mode()&0o111 != 0 {
				return p, nil
			}
		}
		if err := os.Remove(p); err != nil {
			return "", fmt.Errorf("removing stale cached plugin binary: %w", err)
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
		// Windows does not allow rename-over-existing. Handle races where
		// another process created the destination between our remove and rename.
		if _, statErr := os.Stat(p); statErr == nil {
			if rmErr := os.Remove(p); rmErr == nil {
				if retryErr := os.Rename(tmp, p); retryErr == nil {
					return p, nil
				}
			}
		}
		os.Remove(tmp)
		return "", fmt.Errorf("moving plugin binary into cache: %w", err)
	}

	return p, nil
}

// Digest computes the sha256 digest of a cached plugin binary.
// Returns the digest in \"sha256:<hex>\" format.
func (c *Cache) Digest(name, version, platform string, opts ...CacheOption) (string, error) {
	o := applyCacheOpts(opts)
	return fileDigest(c.binaryPath(name, version, platform, o.registryHash))
}

// Remove deletes a cached plugin binary.
func (c *Cache) Remove(name, version, platform string, opts ...CacheOption) error {
	o := applyCacheOpts(opts)
	if c.managed != nil {
		mn := managedName(name, o.registryHash)
		c.managed.Delete(mn, version, platform)
		return nil
	}
	return os.RemoveAll(filepath.Dir(c.binaryPath(name, version, platform, o.registryHash)))
}

// List returns all cached (name, version, platform) triples.
// It handles both layouts: with and without a registry hash directory level.
func (c *Cache) List() ([]CachedPlugin, error) {
	var results []CachedPlugin

	info, statErr := os.Stat(c.dir)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading plugin cache: %w", statErr)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("reading plugin cache: not a directory: %s", c.dir)
	}

	names, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, fmt.Errorf("reading plugin cache: %w", err)
	}

	for _, nameEntry := range names {
		if !nameEntry.IsDir() {
			continue
		}
		pluginName := nameEntry.Name()
		subEntries, err := os.ReadDir(filepath.Join(c.dir, pluginName))
		if err != nil {
			continue
		}

		for _, subEntry := range subEntries {
			if !subEntry.IsDir() {
				continue
			}

			if registryHashPattern.MatchString(subEntry.Name()) {
				// Registry hash dir — versions are one level deeper.
				regHash := subEntry.Name()
				versionEntries, err := os.ReadDir(filepath.Join(c.dir, pluginName, regHash))
				if err != nil {
					continue
				}
				for _, vEntry := range versionEntries {
					if !vEntry.IsDir() {
						continue
					}
					results = append(results, c.listPlatforms(pluginName, vEntry.Name(), regHash)...)
				}
			} else {
				// No registry hash — subEntry is a version dir directly.
				results = append(results, c.listPlatforms(pluginName, subEntry.Name(), "")...)
			}
		}
	}

	return results, nil
}

// listPlatforms scans platform subdirs under a version directory and returns matching CachedPlugin entries.
func (c *Cache) listPlatforms(name, version, registryHash string) []CachedPlugin {
	var baseDir string
	if registryHash != "" {
		baseDir = filepath.Join(c.dir, name, registryHash, version)
	} else {
		baseDir = filepath.Join(c.dir, name, version)
	}

	platforms, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}

	var results []CachedPlugin
	for _, platformEntry := range platforms {
		if !platformEntry.IsDir() {
			continue
		}
		platform := strings.ReplaceAll(platformEntry.Name(), "-", "/")
		bin := name
		if platformIsWindows(platform) {
			bin += ".exe"
		}
		binaryPath := filepath.Join(baseDir, platformEntry.Name(), bin)
		if info, err := os.Stat(binaryPath); err == nil && !info.IsDir() {
			results = append(results, CachedPlugin{
				Name:         name,
				Version:      version,
				Platform:     platform,
				Path:         binaryPath,
				Size:         info.Size(),
				RegistryHash: registryHash,
			})
		}
	}
	return results
}

// CachedPlugin describes a cached plugin binary.
type CachedPlugin struct {
	Name         string `json:"name" yaml:"name" doc:"Plugin name"`
	Version      string `json:"version" yaml:"version" doc:"Plugin version"`
	Platform     string `json:"platform" yaml:"platform" doc:"Target platform (os/arch)"`
	Path         string `json:"path" yaml:"path" doc:"Absolute path to cached binary"`
	Size         int64  `json:"size" yaml:"size" doc:"Binary size in bytes"`
	RegistryHash string `json:"registryHash,omitempty" yaml:"registryHash,omitempty" doc:"Registry hash directory (empty for local installs)"`
}

// GetLatestBinary returns the path to the newest cached binary for the given
// name on the current platform. Returns the path, version, and true if found.
func (c *Cache) GetLatestBinary(name string) (string, string, bool) {
	return c.GetLatestCached(name, CurrentPlatform())
}

// SortAndDedupeLatest sorts a slice of CachedPlugin by name, then version
// descending (real semver comparison so e.g. 0.10.0 sorts above 0.9.0), and --
// unless allVersions is true -- collapses it to only the latest cached
// version per name+platform. This is the shared dedup-to-latest logic used
// by both `plugins list` and the MCP list_plugins tool, mirroring the
// `catalog list` behavior for consistency across cached-plugin listings.
//
// A version that fails to parse as semver always sorts below one that does
// (matching GetLatestCached's "valid semver always wins" precedent); only
// when BOTH sides fail to parse is a lexical fallback used. This avoids an
// invalid strict-weak-ordering that could otherwise pick an arbitrary
// "latest" when comparing valid and invalid versions pairwise.
//
// The input slice is sorted and filtered in place; the returned slice shares
// the input's backing array.
func SortAndDedupeLatest(cached []CachedPlugin, allVersions bool) []CachedPlugin {
	sort.Slice(cached, func(i, j int) bool {
		if cached[i].Name != cached[j].Name {
			return cached[i].Name < cached[j].Name
		}
		vi, iErr := semver.NewVersion(cached[i].Version)
		vj, jErr := semver.NewVersion(cached[j].Version)
		switch {
		case iErr == nil && jErr == nil:
			return vi.GreaterThan(vj)
		case iErr == nil:
			return true // valid semver always sorts above invalid
		case jErr == nil:
			return false
		default:
			return cached[i].Version > cached[j].Version
		}
	})

	if allVersions {
		return cached
	}

	seen := make(map[string]bool)
	filtered := cached[:0]
	for _, p := range cached {
		key := p.Name + "/" + p.Platform
		if !seen[key] {
			seen[key] = true
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// ListCurrentPlatform returns cached plugins that match the current platform.
// When multiple versions of the same plugin are cached, only the latest
// (by semver) is included.
func (c *Cache) ListCurrentPlatform() ([]CachedPlugin, error) {
	all, err := c.List()
	if err != nil {
		return nil, err
	}
	platform := CurrentPlatform()

	// Collect unique plugin names that have binaries for the current platform.
	names := make(map[string]bool)
	for _, p := range all {
		if p.Platform == platform {
			names[p.Name] = true
		}
	}

	// For each unique name, resolve the latest version via semver comparison.
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	result := make([]CachedPlugin, 0, len(sorted))
	for _, name := range sorted {
		path, version, ok := c.GetLatestCached(name, platform)
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		var size int64
		if err == nil {
			size = info.Size()
		}
		result = append(result, CachedPlugin{
			Name:     name,
			Version:  version,
			Platform: platform,
			Path:     path,
			Size:     size,
		})
	}
	return result, nil
}

// binaryPath returns the expected path for a cached plugin binary.
// When registryHash is non-empty, it is inserted as a directory level between name and version.
// On Windows platforms, the binary name gets a ".exe" extension.
func (c *Cache) binaryPath(name, version, platform, registryHash string) string {
	bin := name
	if platformIsWindows(platform) {
		bin += ".exe"
	}
	if registryHash != "" {
		return filepath.Join(c.dir, name, registryHash, version, PlatformCacheKey(platform), bin)
	}
	return filepath.Join(c.dir, name, version, PlatformCacheKey(platform), bin)
}

// legacyBinaryPath returns the pre-fix path without .exe extension (used for migration).
func (c *Cache) legacyBinaryPath(name, version, platform, registryHash string) string {
	if registryHash != "" {
		return filepath.Join(c.dir, name, registryHash, version, PlatformCacheKey(platform), name)
	}
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

// --- Managed mode helpers ---

// xxhashSum computes a non-cryptographic xxHash64 digest for content-dedup.
// Used by the managed cache to skip redundant disk writes when the binary
// content has not changed (e.g., re-fetching the same version in enforce mode).
func xxhashSum(data []byte) (uint64, error) {
	return xxhash.Sum64(data), nil
}

// managedName builds the composite name passed to versionedcache.
// When registryHash is non-empty, the name becomes "name/registryHash"
// so the versionedcache builds a path with the registry hash directory level.
func managedName(name, registryHash string) string {
	if registryHash != "" {
		return name + "/" + registryHash
	}
	return name
}

// getManaged handles Get() in managed (API server) mode.
// It uses Pin to verify the entry exists and checks the digest.
// The pin is released immediately — Get does not hold entries.
func (c *Cache) getManaged(name, version, platform, expectedDigest, registryHash string) (string, bool) {
	mn := managedName(name, registryHash)
	path, release, ok := c.managed.Pin(mn, version, platform)
	if !ok {
		return "", false
	}
	defer release()

	if expectedDigest != "" {
		actual, err := fileDigest(path)
		if err != nil {
			return "", false
		}
		if actual != expectedDigest {
			return "", false
		}
	}
	return path, true
}

// GetPin retrieves a cached plugin binary and pins it to prevent eviction.
// The caller must call release() when done with the binary.
// In CLI mode (unbounded), the release function is a no-op.
// This is the atomic alternative to Get()+Pin() which has a TOCTOU gap.
func (c *Cache) GetPin(name, version, platform, expectedDigest string, opts ...CacheOption) (string, func(), bool) {
	o := applyCacheOpts(opts)
	if c.managed != nil {
		mn := managedName(name, o.registryHash)
		path, release, ok := c.managed.Pin(mn, version, platform)
		if !ok {
			return "", nil, false
		}
		if expectedDigest != "" {
			actual, err := fileDigest(path)
			if err != nil || actual != expectedDigest {
				release()
				return "", nil, false
			}
		}
		return path, release, true
	}
	path, ok := c.Get(name, version, platform, expectedDigest, opts...)
	if !ok {
		return "", nil, false
	}
	return path, func() {}, true
}

// GetLatestCachedPin returns the newest cached binary and pins it.
// The caller must call release() when done with the binary.
// This is the atomic alternative to GetLatestCached()+Pin().
func (c *Cache) GetLatestCachedPin(name, platform string, opts ...CacheOption) (string, string, func(), bool) {
	o := applyCacheOpts(opts)
	if c.managed != nil {
		mn := managedName(name, o.registryHash)
		version, ok := c.managed.Latest(mn, platform)
		if !ok {
			return "", "", nil, false
		}
		path, release, ok := c.managed.Pin(mn, version, platform)
		if !ok {
			return "", "", nil, false
		}
		return path, version, release, true
	}
	path, version, ok := c.GetLatestCached(name, platform, opts...)
	if !ok {
		return "", "", nil, false
	}
	return path, version, func() {}, true
}

// getLatestCachedManaged handles GetLatestCached() in managed mode.
func (c *Cache) getLatestCachedManaged(name, platform, registryHash string) (string, string, bool) {
	mn := managedName(name, registryHash)
	version, ok := c.managed.Latest(mn, platform)
	if !ok {
		return "", "", false
	}
	p := c.binaryPath(name, version, platform, registryHash)
	if _, err := os.Stat(p); err != nil {
		return "", "", false
	}
	return p, version, true
}

// putManaged handles Put() in managed mode.
// Delegates to the versioned cache which handles atomic writes and file mode.
func (c *Cache) putManaged(name, version, platform, registryHash string, data []byte) (string, error) {
	mn := managedName(name, registryHash)
	if err := c.managed.Set(mn, version, platform, data); err != nil {
		return "", err
	}
	return c.binaryPath(name, version, platform, registryHash), nil
}

// SetPin writes a plugin binary and pins it atomically to prevent eviction
// between write and use. In CLI mode (unbounded), behaves like Put with a no-op release.
func (c *Cache) SetPin(name, version, platform string, data []byte, opts ...CacheOption) (string, func(), error) {
	o := applyCacheOpts(opts)
	if c.managed != nil {
		mn := managedName(name, o.registryHash)
		return c.managed.SetPin(mn, version, platform, data)
	}
	path, err := c.Put(name, version, platform, data, opts...)
	if err != nil {
		return "", nil, err
	}
	return path, func() {}, nil
}
