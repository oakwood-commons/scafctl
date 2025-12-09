package httpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileCache is a filesystem-based cache implementation
type FileCache struct {
	dir string
	ttl time.Duration
}

// NewFileCache creates a new filesystem cache in the specified directory
func NewFileCache(dir string, ttl time.Duration) (*FileCache, error) {
	// Expand home directory if present
	if len(dir) > 0 && dir[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dir = filepath.Join(homeDir, dir[1:])
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &FileCache{
		dir: dir,
		ttl: ttl,
	}, nil
}

// Set stores data in the cache with the given key
// Note: The ttl parameter is required by the httpcache.Cache interface but is not used.
// This implementation uses the cache's default TTL (fc.ttl) for all entries, which is
// checked during Get() based on the file's modification time.
func (fc *FileCache) Set(_ context.Context, key string, data []byte, _ time.Duration) error {
	filename := fc.keyToFilename(key)
	tmpFile := filename + ".tmp"

	// Write to temporary file first
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile) // Clean up temp file
		return fmt.Errorf("failed to rename cache file: %w", err)
	}

	return nil
}

// Get retrieves data from the cache for the given key
func (fc *FileCache) Get(_ context.Context, key string) ([]byte, error) {
	filename := fc.keyToFilename(key)

	// Check if file exists and is not expired
	info, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// Return nil, nil for cache miss (not an error condition)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to stat cache file: %w", err)
	}

	// Check if cache entry is expired
	if fc.ttl > 0 && time.Since(info.ModTime()) > fc.ttl {
		// Clean up expired file
		os.Remove(filename)
		// Return nil, nil for expired cache (not an error condition)
		return nil, nil
	}

	// Read the cached data
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	return data, nil
}

// Del removes data from the cache for the given key
func (fc *FileCache) Del(_ context.Context, key string) error {
	filename := fc.keyToFilename(key)

	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete cache file: %w", err)
	}

	return nil
}

// Clear removes all cached files
func (fc *FileCache) Clear() error {
	entries, err := os.ReadDir(fc.dir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			filepath := filepath.Join(fc.dir, entry.Name())
			if err := os.Remove(filepath); err != nil {
				return fmt.Errorf("failed to remove cache file %s: %w", filepath, err)
			}
		}
	}

	return nil
}

// CleanExpired removes expired cache entries
func (fc *FileCache) CleanExpired() error {
	if fc.ttl == 0 {
		return nil // No TTL, nothing to clean
	}

	entries, err := os.ReadDir(fc.dir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filepath := filepath.Join(fc.dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if now.Sub(info.ModTime()) > fc.ttl {
			os.Remove(filepath) // Ignore errors, best effort cleanup
		}
	}

	return nil
}

// keyToFilename converts a cache key to a safe filename using SHA-256 hash
func (fc *FileCache) keyToFilename(key string) string {
	hash := sha256.Sum256([]byte(key))
	filename := hex.EncodeToString(hash[:])
	return filepath.Join(fc.dir, filename)
}
