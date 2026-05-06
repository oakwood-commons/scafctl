// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/paths"
)

const (
	// DefaultFetchCooldown is the default duration to suppress retry attempts
	// after a plugin auto-fetch failure.
	DefaultFetchCooldown = 5 * time.Minute

	// fetchFailedPrefix is the prefix for cooldown marker files.
	fetchFailedPrefix = ".fetch-failed-"
)

// FetchCooldown manages cooldown state for failed plugin auto-fetch attempts.
// It uses file-based markers in the plugin cache directory to persist cooldown
// state across CLI invocations.
type FetchCooldown struct {
	dir      string
	cooldown time.Duration
	now      func() time.Time
}

// NewFetchCooldown creates a FetchCooldown with the given cooldown duration.
// If cacheDir is empty, the default plugin cache directory is used.
// If cooldown is zero, DefaultFetchCooldown is used.
func NewFetchCooldown(cacheDir string, cooldown time.Duration) *FetchCooldown {
	if cacheDir == "" {
		cacheDir = paths.PluginCacheDir()
	}
	if cooldown <= 0 {
		cooldown = DefaultFetchCooldown
	}
	return &FetchCooldown{
		dir:      cacheDir,
		cooldown: cooldown,
		now:      time.Now,
	}
}

// OnCooldown reports whether a fetch attempt for the named plugin should be
// suppressed due to a recent failure. Returns true if a cooldown marker exists
// and has not expired.
func (fc *FetchCooldown) OnCooldown(name string) bool {
	markerPath := fc.markerPath(name)
	info, err := os.Stat(markerPath)
	if err != nil {
		return false
	}
	return fc.now().Sub(info.ModTime()) < fc.cooldown
}

// RecordFailure writes a cooldown marker for the named plugin, indicating that
// a fetch attempt failed at the current time.
func (fc *FetchCooldown) RecordFailure(name string) error {
	if err := os.MkdirAll(fc.dir, 0o755); err != nil {
		return fmt.Errorf("creating cooldown directory: %w", err)
	}
	markerPath := fc.markerPath(name)
	f, err := os.Create(markerPath)
	if err != nil {
		return fmt.Errorf("writing cooldown marker: %w", err)
	}
	return f.Close()
}

// Clear removes the cooldown marker for the named plugin. This is called on
// explicit install (e.g., `scafctl plugin install`) or with --force-fetch.
func (fc *FetchCooldown) Clear(name string) error {
	markerPath := fc.markerPath(name)
	err := os.Remove(markerPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// markerPath returns the filesystem path for a plugin's cooldown marker.
func (fc *FetchCooldown) markerPath(name string) string {
	return filepath.Join(fc.dir, fetchFailedPrefix+name)
}
