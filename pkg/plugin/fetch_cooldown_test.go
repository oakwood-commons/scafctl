// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchCooldown_OnCooldown_NoMarker(t *testing.T) {
	t.Parallel()
	fc := NewFetchCooldown(t.TempDir(), DefaultFetchCooldown)
	assert.False(t, fc.OnCooldown("github"))
}

func TestFetchCooldown_OnCooldown_RecentMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fc := NewFetchCooldown(dir, DefaultFetchCooldown)

	require.NoError(t, fc.RecordFailure("github"))
	assert.True(t, fc.OnCooldown("github"))
}

func TestFetchCooldown_OnCooldown_ExpiredMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fc := NewFetchCooldown(dir, DefaultFetchCooldown)
	// Advance time past cooldown
	fc.now = func() time.Time {
		return time.Now().Add(10 * time.Minute)
	}

	// Write marker at current real time
	markerPath := filepath.Join(dir, fetchFailedPrefix+"github")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	f, err := os.Create(markerPath)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	assert.False(t, fc.OnCooldown("github"))
}

func TestFetchCooldown_RecordFailure_CreatesMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fc := NewFetchCooldown(dir, DefaultFetchCooldown)

	require.NoError(t, fc.RecordFailure("entra"))

	markerPath := filepath.Join(dir, fetchFailedPrefix+"entra")
	_, err := os.Stat(markerPath)
	assert.NoError(t, err)
}

func TestFetchCooldown_RecordFailure_CreatesDirectory(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	fc := NewFetchCooldown(dir, DefaultFetchCooldown)

	require.NoError(t, fc.RecordFailure("gcp"))

	markerPath := filepath.Join(dir, fetchFailedPrefix+"gcp")
	_, err := os.Stat(markerPath)
	assert.NoError(t, err)
}

func TestFetchCooldown_Clear_RemovesMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fc := NewFetchCooldown(dir, DefaultFetchCooldown)

	require.NoError(t, fc.RecordFailure("github"))
	assert.True(t, fc.OnCooldown("github"))

	require.NoError(t, fc.Clear("github"))
	assert.False(t, fc.OnCooldown("github"))
}

func TestFetchCooldown_Clear_NonExistent(t *testing.T) {
	t.Parallel()
	fc := NewFetchCooldown(t.TempDir(), DefaultFetchCooldown)
	assert.NoError(t, fc.Clear("nonexistent"))
}

func TestFetchCooldown_DefaultCooldown(t *testing.T) {
	t.Parallel()
	fc := NewFetchCooldown(t.TempDir(), 0)
	assert.Equal(t, DefaultFetchCooldown, fc.cooldown)
}

func TestFetchCooldown_IndependentPlugins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fc := NewFetchCooldown(dir, DefaultFetchCooldown)

	require.NoError(t, fc.RecordFailure("github"))
	assert.True(t, fc.OnCooldown("github"))
	assert.False(t, fc.OnCooldown("entra"))
}

func TestFetchCooldown_CustomCooldownDuration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fc := NewFetchCooldown(dir, 30*time.Second)

	require.NoError(t, fc.RecordFailure("github"))
	assert.True(t, fc.OnCooldown("github"))

	// Simulate 31 seconds passing
	fc.now = func() time.Time {
		return time.Now().Add(31 * time.Second)
	}
	assert.False(t, fc.OnCooldown("github"))
}
