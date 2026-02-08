// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
)

// FileCacheConfig holds configuration for filesystem cache
type FileCacheConfig struct {
	// Dir is the directory to use for cache storage
	Dir string
	// TTL is the time-to-live for cached entries
	TTL time.Duration
	// KeyPrefix is a prefix added to all cache keys to prevent collisions
	KeyPrefix string
	// MaxSize is the maximum size in bytes for a single cached file (0 = no limit)
	MaxSize int64
	// Logger is used for logging cache operations
	Logger logr.Logger
}

// FileCache is a filesystem-based cache implementation.
//
// Thread-Safety: FileCache is safe for concurrent use by multiple goroutines.
// Get, Set, and Del operations can be called concurrently. Hit/miss statistics
// are tracked using atomic operations. File operations are performed atomically
// where possible (e.g., write-then-rename for Set). However, due to filesystem
// limitations, there may be race conditions if multiple processes (not goroutines)
// access the same cache directory simultaneously.
type FileCache struct {
	dir       string
	ttl       time.Duration
	keyPrefix string
	maxSize   int64
	logger    logr.Logger
	hits      uint64
	misses    uint64
}

// NewFileCache creates a new filesystem cache in the specified directory
func NewFileCache(config *FileCacheConfig) (*FileCache, error) {
	if config == nil {
		return nil, errors.New("config cannot be nil")
	}

	dir := config.Dir
	if dir == "" {
		return nil, errors.New("cache directory cannot be empty")
	}

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

	logger := config.Logger
	if logger.GetSink() == nil {
		logger = logr.Discard()
	}

	fc := &FileCache{
		dir:       dir,
		ttl:       config.TTL,
		keyPrefix: config.KeyPrefix,
		maxSize:   config.MaxSize,
		logger:    logger,
	}

	// Update cache size metric on initialization
	go fc.updateCacheSizeMetric()

	return fc, nil
}

// Set stores data in the cache with the given key
// The ttl parameter is required by the httpcache.Cache interface but is not used.
// This implementation uses the cache's default TTL (fc.ttl) for all entries.
func (fc *FileCache) Set(ctx context.Context, key string, data []byte, _ time.Duration) error {
	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		return err
	}

	// Check size limit
	if fc.maxSize > 0 && int64(len(data)) > fc.maxSize {
		fc.logger.V(1).Info("cache entry exceeds size limit",
			"key", key,
			"size", len(data),
			"maxSize", fc.maxSize,
		)
		return fmt.Errorf("%w: entry size %d exceeds limit %d", ErrCacheSizeLimitExceeded, len(data), fc.maxSize)
	}

	filename := fc.keyToFilename(key)
	tmpFile := filename + ".tmp"

	// Create a context with timeout for file operations (5 seconds)
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Write to temporary file first using a goroutine to respect context
	errChan := make(chan error, 1)
	go func() {
		errChan <- os.WriteFile(tmpFile, data, 0o600)
	}()

	// Wait for write to complete or context to cancel
	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("failed to write cache file: %w", err)
		}
	case <-writeCtx.Done():
		os.Remove(tmpFile) // Clean up temp file
		return fmt.Errorf("cache write timeout: %w", writeCtx.Err())
	}

	// Check context again before rename
	if err := ctx.Err(); err != nil {
		os.Remove(tmpFile) // Clean up temp file
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile) // Clean up temp file
		return fmt.Errorf("failed to rename cache file: %w", err)
	}

	fc.logger.V(2).Info("cached entry", "key", key, "size", len(data))

	// Update cache size metric asynchronously
	go fc.updateCacheSizeMetric()

	return nil
}

// Get retrieves data from the cache for the given key
// Returns (nil, nil) for cache misses - this is not an error, it's standard cache behavior
func (fc *FileCache) Get(ctx context.Context, key string) ([]byte, error) {
	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	filename := fc.keyToFilename(key)

	// Create a context with timeout for file operations (5 seconds)
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Check if file exists and is not expired (with timeout)
	type statResult struct {
		info os.FileInfo
		err  error
	}
	statChan := make(chan statResult, 1)
	go func() {
		info, err := os.Stat(filename)
		statChan <- statResult{info, err}
	}()

	var info os.FileInfo
	select {
	case result := <-statChan:
		if result.err != nil {
			if errors.Is(result.err, fs.ErrNotExist) {
				// Cache miss
				atomic.AddUint64(&fc.misses, 1)
				metrics.HTTPClientCacheMisses.Inc()
				// Return nil, nil for httpcache compatibility
				return nil, nil
			}
			return nil, fmt.Errorf("failed to stat cache file: %w", result.err)
		}
		info = result.info
	case <-readCtx.Done():
		return nil, fmt.Errorf("cache stat timeout: %w", readCtx.Err())
	}

	// Check if cache entry is expired
	if fc.ttl > 0 && time.Since(info.ModTime()) > fc.ttl {
		// Clean up expired file
		if err := os.Remove(filename); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fc.logger.V(1).Info("failed to remove expired cache file", "key", key, "error", err)
		}
		// Cache miss (expired)
		atomic.AddUint64(&fc.misses, 1)
		metrics.HTTPClientCacheMisses.Inc()
		// Return nil, nil for httpcache compatibility
		return nil, nil
	}

	// Read the cached data with timeout
	type readResult struct {
		data []byte
		err  error
	}
	readChan := make(chan readResult, 1)
	go func() {
		data, err := os.ReadFile(filename)
		readChan <- readResult{data, err}
	}()

	select {
	case result := <-readChan:
		if result.err != nil {
			if errors.Is(result.err, fs.ErrNotExist) {
				// File was deleted between stat and read
				atomic.AddUint64(&fc.misses, 1)
				metrics.HTTPClientCacheMisses.Inc()
				// Return nil, nil for httpcache compatibility
				return nil, nil
			}
			return nil, fmt.Errorf("failed to read cache file: %w", result.err)
		}
		// Cache hit
		atomic.AddUint64(&fc.hits, 1)
		metrics.HTTPClientCacheHits.Inc()
		fc.logger.V(2).Info("cache hit", "key", key, "size", len(result.data))
		return result.data, nil
	case <-readCtx.Done():
		return nil, fmt.Errorf("cache read timeout: %w", readCtx.Err())
	}
}

// Del removes data from the cache for the given key
func (fc *FileCache) Del(ctx context.Context, key string) error {
	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		return err
	}

	filename := fc.keyToFilename(key)

	if err := os.Remove(filename); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to delete cache file: %w", err)
	}

	fc.logger.V(2).Info("deleted cache entry", "key", key)
	return nil
}

// Clear removes all cached files
func (fc *FileCache) Clear() error {
	entries, err := os.ReadDir(fc.dir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			filePath := filepath.Join(fc.dir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				errs = append(errs, fmt.Errorf("failed to remove %s: %w", filePath, err))
				fc.logger.Error(err, "failed to remove cache file", "path", filePath)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("encountered %d errors while clearing cache", len(errs))
	}

	fc.logger.Info("cleared all cache entries", "directory", fc.dir)
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
	removedCount := 0
	var errs []error

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(fc.dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			fc.logger.V(1).Info("failed to get file info during cleanup", "path", filePath, "error", err)
			continue
		}

		if now.Sub(info.ModTime()) > fc.ttl {
			if err := os.Remove(filePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
				fc.logger.V(1).Info("failed to remove expired cache file", "path", filePath, "error", err)
			} else {
				removedCount++
			}
		}
	}

	fc.logger.V(1).Info("cleaned expired cache entries", "removed", removedCount, "errors", len(errs))

	if len(errs) > 0 {
		return fmt.Errorf("encountered %d errors while cleaning expired entries", len(errs))
	}

	return nil
}

// keyToFilename converts a cache key to a safe filename using SHA-256 hash
func (fc *FileCache) keyToFilename(key string) string {
	// Add prefix to key before hashing
	prefixedKey := fc.keyPrefix + key
	hash := sha256.Sum256([]byte(prefixedKey))
	filename := hex.EncodeToString(hash[:])
	return filepath.Join(fc.dir, filename)
}

// Stats returns the cache hit and miss statistics
func (fc *FileCache) Stats() (hits, misses uint64) {
	return atomic.LoadUint64(&fc.hits), atomic.LoadUint64(&fc.misses)
}

// Close performs cleanup and releases resources
// This method cleans expired entries as a final housekeeping step
func (fc *FileCache) Close() error {
	fc.logger.V(1).Info("closing file cache", "directory", fc.dir)
	// Clean up expired entries on close
	if err := fc.CleanExpired(); err != nil {
		fc.logger.V(1).Info("error cleaning expired entries on close", "error", err)
		return err
	}
	return nil
}

// updateCacheSizeMetric calculates the total size of cached files and updates the metric
// This should be called periodically or after significant cache operations
func (fc *FileCache) updateCacheSizeMetric() {
	entries, err := os.ReadDir(fc.dir)
	if err != nil {
		fc.logger.V(1).Info("failed to read cache directory for size metric", "error", err)
		return
	}

	var totalSize int64
	for _, entry := range entries {
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			totalSize += info.Size()
		}
	}

	metrics.HTTPClientCacheSizeBytes.Set(float64(totalSize))
	fc.logger.V(2).Info("updated cache size metric", "bytes", totalSize)
}
