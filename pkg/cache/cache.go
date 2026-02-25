// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package cache provides domain logic for cache management operations
// including clearing directories, gathering cache information, and
// formatting byte sizes.
package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Kind represents the type of cache to clear.
type Kind string

const (
	// KindAll clears all caches.
	KindAll Kind = "all"
	// KindHTTP clears the HTTP response cache.
	KindHTTP Kind = "http"
	// KindBuild clears the build cache (incremental build fingerprints).
	KindBuild Kind = "build"
)

// ValidKinds lists all valid cache kinds.
var ValidKinds = []string{string(KindAll), string(KindHTTP), string(KindBuild)}

// ClearOutput represents the result of a cache clear operation.
type ClearOutput struct {
	RemovedFiles int64  `json:"removedFiles" yaml:"removedFiles"`
	RemovedBytes int64  `json:"removedBytes" yaml:"removedBytes"`
	RemovedHuman string `json:"reclaimedHuman" yaml:"reclaimedHuman"`
	Kind         string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
}

// Info represents information about a cache directory.
type Info struct {
	Name        string `json:"name" yaml:"name"`
	Path        string `json:"path" yaml:"path"`
	Size        int64  `json:"size" yaml:"size"`
	SizeHuman   string `json:"sizeHuman" yaml:"sizeHuman"`
	FileCount   int64  `json:"fileCount" yaml:"fileCount"`
	Description string `json:"description" yaml:"description"`
}

// InfoOutput represents aggregated information for multiple cache directories.
type InfoOutput struct {
	Caches     []Info `json:"caches" yaml:"caches"`
	TotalSize  int64  `json:"totalSize" yaml:"totalSize"`
	TotalHuman string `json:"totalHuman" yaml:"totalHuman"`
	TotalFiles int64  `json:"totalFiles" yaml:"totalFiles"`
}

// ClearDirectory removes files from a directory, optionally matching a pattern.
// Returns the number of files removed and total bytes reclaimed.
func ClearDirectory(dir, pattern string) (int64, int64, error) {
	var filesRemoved int64
	var bytesRemoved int64

	// Check if directory exists
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("failed to stat directory: %w", err)
	}
	if !info.IsDir() {
		return 0, 0, fmt.Errorf("path is not a directory: %s", dir)
	}

	// If no pattern and clearing everything, just remove the whole directory
	if pattern == "" {
		// Calculate size first
		_ = filepath.Walk(dir, func(_ string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return nil //nolint:nilerr // Intentionally ignoring walk errors
			}
			bytesRemoved += info.Size()
			filesRemoved++
			return nil
		})

		// Remove the directory contents (but keep the directory itself)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to read directory: %w", err)
		}
		for _, entry := range entries {
			entryPath := filepath.Join(dir, entry.Name())
			if err := os.RemoveAll(entryPath); err != nil {
				return filesRemoved, bytesRemoved, fmt.Errorf("failed to remove %s: %w", entryPath, err)
			}
		}

		return filesRemoved, bytesRemoved, nil
	}

	// With a pattern, only remove matching files
	_ = filepath.Walk(dir, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil //nolint:nilerr // Intentionally ignoring walk errors
		}

		// Check if file matches pattern
		name := filepath.Base(filePath)
		matched, matchErr := filepath.Match(pattern, name)
		if matchErr != nil {
			// Invalid pattern, try as prefix match
			matched = strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))
		}

		if matched {
			bytesRemoved += info.Size()
			_ = os.Remove(filePath) // Ignore individual file removal errors
			filesRemoved++
		}

		return nil
	})

	return filesRemoved, bytesRemoved, nil
}

// GetCacheInfo returns information about a cache directory.
func GetCacheInfo(name, dir, description string) Info {
	info := Info{
		Name:        name,
		Path:        dir,
		Description: description,
	}

	// Check if directory exists
	stat, err := os.Stat(dir)
	if os.IsNotExist(err) || !stat.IsDir() {
		info.SizeHuman = "0 B"
		return info
	}

	// Calculate size and file count
	_ = filepath.Walk(dir, func(_ string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil || fileInfo.IsDir() {
			return nil //nolint:nilerr // Intentionally ignoring walk errors
		}
		info.Size += fileInfo.Size()
		info.FileCount++
		return nil
	})

	info.SizeHuman = FormatBytes(info.Size)
	return info
}

// FormatBytes formats bytes as a human-readable string.
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
