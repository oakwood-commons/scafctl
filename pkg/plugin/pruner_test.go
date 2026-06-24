// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedCache(t *testing.T, cacheDir, name, version string) {
	t.Helper()
	platform := CurrentPlatform()
	platformKey := PlatformCacheKey(platform)
	dir := filepath.Join(cacheDir, name, version, platformKey)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	binName := name
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, binName), []byte("binary-"+version), 0o755))
}

func TestPrune_RemovesOldVersions(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "1.1.0")
	seedCache(t, cacheDir, "github", "1.2.0")

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{Keep: 1}, false)
	require.NoError(t, err)

	assert.Len(t, summary.Removed, 2)
	assert.Greater(t, summary.TotalFreed, int64(0))

	// Only latest should remain.
	remaining, err := cache.List()
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, "1.2.0", remaining[0].Version)
}

func TestPrune_KeepsMultipleVersions(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "exec", "0.3.0")
	seedCache(t, cacheDir, "exec", "0.4.0")
	seedCache(t, cacheDir, "exec", "0.5.0")

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{Keep: 2}, false)
	require.NoError(t, err)

	assert.Len(t, summary.Removed, 1)
	assert.Equal(t, "0.3.0", summary.Removed[0].Version)

	remaining, err := cache.List()
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
}

func TestPrune_DryRun_DoesNotDelete(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "2.0.0")

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{Keep: 1}, true)
	require.NoError(t, err)

	assert.Len(t, summary.Removed, 1)
	assert.Equal(t, "1.0.0", summary.Removed[0].Version)

	// Files should still exist.
	remaining, err := cache.List()
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
}

func TestPrune_FiltersByName(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "2.0.0")
	seedCache(t, cacheDir, "exec", "0.1.0")
	seedCache(t, cacheDir, "exec", "0.2.0")

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{Keep: 1, Names: []string{"github"}}, false)
	require.NoError(t, err)

	assert.Len(t, summary.Removed, 1)
	assert.Equal(t, "github", summary.Removed[0].Name)

	// exec should still have both versions.
	remaining, err := cache.List()
	require.NoError(t, err)
	execCount := 0
	for _, r := range remaining {
		if r.Name == "exec" {
			execCount++
		}
	}
	assert.Equal(t, 2, execCount)
}

func TestPrune_All_RequiresForce(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")

	cache := NewCache(cacheDir)
	_, err := cache.Prune(PruneOptions{All: true, Force: false}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all requires --force")
}

func TestPrune_All_WithForce(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "exec", "0.5.0")

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{All: true, Force: true}, false)
	require.NoError(t, err)

	assert.Len(t, summary.Removed, 2)

	remaining, err := cache.List()
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

func TestPrune_EmptyCache(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{Keep: 1}, false)
	require.NoError(t, err)

	assert.Empty(t, summary.Removed)
	assert.Equal(t, int64(0), summary.TotalFreed)
}

func TestPrune_NothingToRemove(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{Keep: 1}, false)
	require.NoError(t, err)

	assert.Empty(t, summary.Removed)
}

func TestSelectPruneTargets_SemverOrder(t *testing.T) {
	t.Parallel()

	plugins := []CachedPlugin{
		{Name: "github", Version: "1.0.0", Platform: "linux/amd64", Size: 100},
		{Name: "github", Version: "1.10.0", Platform: "linux/amd64", Size: 100},
		{Name: "github", Version: "1.2.0", Platform: "linux/amd64", Size: 100},
	}

	targets := selectPruneTargets(plugins, 1)
	assert.Len(t, targets, 2)
	// Should keep 1.10.0 (newest), remove 1.0.0 and 1.2.0.
	for _, r := range targets {
		assert.NotEqual(t, "1.10.0", r.Version)
	}
}

func TestSelectPruneTargets_KeepAllWhenUnderLimit(t *testing.T) {
	t.Parallel()

	plugins := []CachedPlugin{
		{Name: "exec", Version: "0.1.0", Platform: "linux/amd64", Size: 50},
	}

	targets := selectPruneTargets(plugins, 2)
	assert.Nil(t, targets)
}

func TestPrune_FiltersByPlatform(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	// Seed current platform.
	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "2.0.0")

	cache := NewCache(cacheDir)
	// Filter by a non-matching platform — should find nothing to prune.
	summary, err := cache.Prune(PruneOptions{Keep: 1, Platform: "fakeos/fakearch"}, false)
	require.NoError(t, err)
	assert.Empty(t, summary.Removed)
}

func TestPrune_AllWithForce_DryRun(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "exec", "0.5.0")

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{All: true, Force: true}, true)
	require.NoError(t, err)

	// Should list everything as removed.
	assert.Len(t, summary.Removed, 2)
	assert.Greater(t, summary.TotalFreed, int64(0))

	// But files should still exist.
	remaining, err := cache.List()
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
}

func TestPrune_DefaultKeepZero(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "2.0.0")

	cache := NewCache(cacheDir)
	// Keep=0 should default to 1.
	summary, err := cache.Prune(PruneOptions{Keep: 0}, false)
	require.NoError(t, err)
	assert.Len(t, summary.Removed, 1)
	assert.Equal(t, "1.0.0", summary.Removed[0].Version)
}

func TestPrune_MultiplePlugins(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "2.0.0")
	seedCache(t, cacheDir, "github", "3.0.0")
	seedCache(t, cacheDir, "exec", "0.1.0")
	seedCache(t, cacheDir, "exec", "0.2.0")

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{Keep: 1}, false)
	require.NoError(t, err)
	// github: remove 1.0.0 + 2.0.0, exec: remove 0.1.0.
	assert.Len(t, summary.Removed, 3)

	remaining, err := cache.List()
	require.NoError(t, err)
	assert.Len(t, remaining, 2) // github@3.0.0 + exec@0.2.0
}

func TestSelectPruneTargets_NonSemverFallback(t *testing.T) {
	t.Parallel()

	plugins := []CachedPlugin{
		{Name: "custom", Version: "abc", Platform: "linux/amd64", Size: 100},
		{Name: "custom", Version: "xyz", Platform: "linux/amd64", Size: 100},
		{Name: "custom", Version: "mno", Platform: "linux/amd64", Size: 100},
	}

	targets := selectPruneTargets(plugins, 1)
	assert.Len(t, targets, 2)
	// Lexicographic descending: xyz > mno > abc → keep xyz.
	for _, r := range targets {
		assert.NotEqual(t, "xyz", r.Version)
	}
}

func TestPrune_KeepZero_WithForceAndName(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "2.0.0")
	seedCache(t, cacheDir, "exec", "0.5.0")

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{Keep: 0, Force: true, Names: []string{"github"}}, false)
	require.NoError(t, err)

	// All github versions removed, exec untouched.
	assert.Len(t, summary.Removed, 2)
	for _, r := range summary.Removed {
		assert.Equal(t, "github", r.Name)
	}

	remaining, err := cache.List()
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, "exec", remaining[0].Name)
}

func TestPrune_KeepZero_WithoutForce_DefaultsToOne(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "2.0.0")

	cache := NewCache(cacheDir)
	// Keep=0 without force should default to 1.
	summary, err := cache.Prune(PruneOptions{Keep: 0, Names: []string{"github"}}, false)
	require.NoError(t, err)
	assert.Len(t, summary.Removed, 1)
	assert.Equal(t, "1.0.0", summary.Removed[0].Version)
}

func TestPrune_KeepZero_WithoutNames_DefaultsToOne(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "2.0.0")

	cache := NewCache(cacheDir)
	// Keep=0 with force but no names should default to 1.
	summary, err := cache.Prune(PruneOptions{Keep: 0, Force: true}, false)
	require.NoError(t, err)
	assert.Len(t, summary.Removed, 1)
	assert.Equal(t, "1.0.0", summary.Removed[0].Version)
}

func TestPrune_SkipsLockedFiles(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "github", "2.0.0")

	// Make the v1.0.0 platform directory non-removable via chmod.
	cache := NewCache(cacheDir)
	binPath := cache.binaryPath("github", "1.0.0", CurrentPlatform(), "")
	platformDir := filepath.Dir(binPath)
	if err := os.Chmod(platformDir, 0o555); err != nil {
		t.Skip("cannot restrict permissions on this platform")
	}
	t.Cleanup(func() { os.Chmod(platformDir, 0o755) })

	summary, err := cache.Prune(PruneOptions{Keep: 0, Force: true, Names: []string{"github"}}, false)
	require.NoError(t, err)

	// At least one entry should be skipped due to permission error.
	if len(summary.Skipped) == 0 {
		t.Skip("OS allowed removal despite chmod -- platform does not enforce")
	}
	assert.NotEmpty(t, summary.Skipped)
	assert.NotEmpty(t, summary.Skipped[0].Reason)
}

func TestPruneAll_SkipsLockedEntry_RecalculatesRemoved(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "exec", "0.5.0")

	// Lock the github directory so pruneAll can't remove it.
	githubDir := filepath.Join(cacheDir, "github")
	if err := os.Chmod(githubDir, 0o555); err != nil {
		t.Skip("cannot restrict permissions on this platform")
	}
	t.Cleanup(func() { os.Chmod(githubDir, 0o755) })

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{All: true, Force: true}, false)
	require.NoError(t, err)

	if len(summary.Skipped) == 0 {
		t.Skip("OS allowed removal despite chmod -- platform does not enforce")
	}

	// github should be skipped, exec should be removed.
	assert.Len(t, summary.Skipped, 1)
	assert.Equal(t, "github", summary.Skipped[0].Name)
	assert.Empty(t, summary.Skipped[0].Version, "pruneAll skips use name only, no version")

	// Only exec should be in Removed (github was recalculated out).
	for _, r := range summary.Removed {
		assert.Equal(t, "exec", r.Name, "github entries should have been recalculated out of Removed")
	}
}

func TestPruneAll_SkipsLockedEntry_DryRunUnaffected(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")
	seedCache(t, cacheDir, "exec", "0.5.0")

	// Lock github -- but dry run should not attempt removal.
	githubDir := filepath.Join(cacheDir, "github")
	if err := os.Chmod(githubDir, 0o555); err != nil {
		t.Skip("cannot restrict permissions on this platform")
	}
	t.Cleanup(func() { os.Chmod(githubDir, 0o755) })

	cache := NewCache(cacheDir)
	summary, err := cache.Prune(PruneOptions{All: true, Force: true}, true)
	require.NoError(t, err)

	// Dry run lists everything as removed; skipped is empty.
	assert.Len(t, summary.Removed, 2)
	assert.Empty(t, summary.Skipped)
}

func TestPrune_KeepZero_CleansNameDirectory(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	seedCache(t, cacheDir, "github", "1.0.0")

	cache := NewCache(cacheDir)
	_, err := cache.Prune(PruneOptions{Keep: 0, Force: true, Names: []string{"github"}}, false)
	require.NoError(t, err)

	// The name directory should be cleaned up when all versions are removed.
	_, statErr := os.Stat(filepath.Join(cacheDir, "github"))
	assert.True(t, os.IsNotExist(statErr), "name directory should be removed when empty")
}

func TestPruneSkipped_OmitsEmptyFields_JSON(t *testing.T) {
	t.Parallel()

	// Verify omitempty works: Version/Platform should not appear when empty.
	s := PruneSkipped{Name: "test", Reason: "locked"}
	data, err := json.Marshal(s)
	require.NoError(t, err)

	assert.NotContains(t, string(data), `"version"`)
	assert.NotContains(t, string(data), `"platform"`)

	// With values set, they should appear.
	s2 := PruneSkipped{Name: "test", Version: "1.0.0", Platform: "linux/amd64", Reason: "locked"}
	data2, err := json.Marshal(s2)
	require.NoError(t, err)

	assert.Contains(t, string(data2), `"version"`)
	assert.Contains(t, string(data2), `"platform"`)
}
