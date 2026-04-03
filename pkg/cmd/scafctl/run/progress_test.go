// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProgressReporter(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 5)
	require.NotNil(t, pr)
	assert.Equal(t, 5, pr.total)
	assert.NotNil(t, pr.bars)
	assert.NotNil(t, pr.barFailed)
}

func TestProgressReporter_StartPhase(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 3)

	pr.StartPhase(1, []string{"resolver-a", "resolver-b"})

	assert.Contains(t, pr.bars, "resolver-a")
	assert.Contains(t, pr.bars, "resolver-b")
	assert.Equal(t, 1, pr.barPhases["resolver-a"])
}

func TestProgressReporter_Complete(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	pr.StartPhase(1, []string{"resolver-a"})

	pr.Complete("resolver-a", 150*time.Millisecond)

	assert.Contains(t, pr.barElapsed, "resolver-a")
	assert.Equal(t, 150*time.Millisecond, pr.barElapsed["resolver-a"])
}

func TestProgressReporter_Complete_UnknownResolver(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)

	// Should not panic on unknown resolver
	pr.Complete("nonexistent", 10*time.Millisecond)
}

func TestProgressReporter_Failed(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	pr.StartPhase(1, []string{"resolver-a"})

	pr.Failed("resolver-a", errors.New("boom"))

	assert.True(t, pr.barFailed["resolver-a"])
}

func TestProgressReporter_Skipped(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	pr.StartPhase(1, []string{"resolver-a"})

	pr.Skipped("resolver-a")

	// Skipped bars are aborted but NOT marked as failed
	assert.False(t, pr.barFailed["resolver-a"])
}

func TestProgressReporter_Skipped_UnknownResolver(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)

	// Should not panic on unknown resolver
	pr.Skipped("nonexistent")
}

func TestProgressReporter_Failed_UnknownResolver(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)

	// Should not panic on unknown resolver
	pr.Failed("nonexistent", errors.New("boom"))
	assert.True(t, pr.barFailed["nonexistent"])
}

func TestProgressReporter_Wait(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	pr.StartPhase(1, []string{"resolver-a"})
	pr.Complete("resolver-a", 50*time.Millisecond)

	dur := pr.Wait()
	assert.Greater(t, dur, time.Duration(0))
}

func TestProgressReporter_TotalDuration(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	time.Sleep(1 * time.Millisecond) // Ensure non-zero elapsed time.

	dur := pr.TotalDuration()
	assert.Greater(t, dur, time.Duration(0))
}

func TestProgressCallback(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 3)
	cb := NewProgressCallback(pr)
	require.NotNil(t, cb)

	cb.OnPhaseStart(1, []string{"a", "b"})
	assert.Contains(t, pr.bars, "a")
	assert.Contains(t, pr.bars, "b")

	cb.OnResolverComplete("a", 100*time.Millisecond)
	assert.Contains(t, pr.barElapsed, "a")
	assert.Equal(t, 100*time.Millisecond, pr.barElapsed["a"])

	cb.OnResolverFailed("b", errors.New("fail"))
	assert.True(t, pr.barFailed["b"])
}

func TestProgressCallback_OnResolverSkipped(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	cb := NewProgressCallback(pr)

	cb.OnPhaseStart(1, []string{"skippable"})
	cb.OnResolverSkipped("skippable")

	assert.False(t, pr.barFailed["skippable"])
}

func TestProgressReporter_MultiplePhases(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 4)

	pr.StartPhase(1, []string{"a", "b"})
	pr.Complete("a", 50*time.Millisecond)
	pr.Failed("b", errors.New("err"))

	pr.StartPhase(2, []string{"c", "d"})
	pr.Complete("c", 75*time.Millisecond)
	pr.Skipped("d")

	assert.Contains(t, pr.barElapsed, "a")
	assert.Equal(t, 50*time.Millisecond, pr.barElapsed["a"])
	assert.True(t, pr.barFailed["b"])
	assert.Contains(t, pr.barElapsed, "c")
	assert.Equal(t, 75*time.Millisecond, pr.barElapsed["c"])
	assert.False(t, pr.barFailed["d"])
	assert.Equal(t, 1, pr.barPhases["a"])
	assert.Equal(t, 2, pr.barPhases["c"])
}

func BenchmarkProgressReporter_Lifecycle(b *testing.B) {
	for b.Loop() {
		pr := NewProgressReporter(io.Discard, 3)
		pr.StartPhase(1, []string{"a", "b", "c"})
		pr.Complete("a", 50*time.Millisecond)
		pr.Failed("b", errors.New("err"))
		pr.Skipped("c")
		pr.Wait()
	}
}

// ── Decorator state verification tests ────────────────────────────────────────

func TestProgressReporter_StartPhase_Complete_RendersWithoutPanic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pr := NewProgressReporter(&buf, 1)
	pr.StartPhase(1, []string{"resolver-x"})
	pr.Complete("resolver-x", 200*time.Millisecond)
	pr.Wait()

	// Verify internal state for decorator correctness
	assert.Equal(t, 200*time.Millisecond, pr.barElapsed["resolver-x"])
	assert.False(t, pr.barFailed["resolver-x"])
}

func TestProgressReporter_StartPhase_Failed_RendersWithoutPanic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pr := NewProgressReporter(&buf, 1)
	pr.StartPhase(1, []string{"resolver-y"})
	pr.Failed("resolver-y", errors.New("boom"))
	pr.Wait()

	assert.True(t, pr.barFailed["resolver-y"])
}

func TestProgressReporter_StartPhase_Skipped_RendersWithoutPanic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pr := NewProgressReporter(&buf, 1)
	pr.StartPhase(1, []string{"resolver-z"})
	pr.Skipped("resolver-z")
	pr.Wait()

	assert.False(t, pr.barFailed["resolver-z"])
}

func TestRunSpinnerFrames_PackageLevel(t *testing.T) {
	t.Parallel()
	assert.Len(t, runSpinnerFrames, 10, "runSpinnerFrames should have 10 frames")
	assert.Equal(t, "⠋", runSpinnerFrames[0])
}
