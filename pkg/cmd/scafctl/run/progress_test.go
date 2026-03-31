// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
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
	assert.NotNil(t, pr.barStarts)
	assert.NotNil(t, pr.barFailed)
}

func TestProgressReporter_StartPhase(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 3)

	pr.StartPhase(1, []string{"resolver-a", "resolver-b"})

	assert.Contains(t, pr.bars, "resolver-a")
	assert.Contains(t, pr.bars, "resolver-b")
	assert.Contains(t, pr.barStarts, "resolver-a")
	assert.Equal(t, 1, pr.barPhases["resolver-a"])
}

func TestProgressReporter_Complete(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	pr.StartPhase(1, []string{"resolver-a"})

	pr.Complete("resolver-a")

	assert.Contains(t, pr.barElapsed, "resolver-a")
	assert.Greater(t, pr.barElapsed["resolver-a"], time.Duration(0))
}

func TestProgressReporter_Complete_UnknownResolver(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)

	// Should not panic on unknown resolver
	pr.Complete("nonexistent")
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
	pr.Complete("resolver-a")

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

	cb.OnResolverComplete("a")
	assert.Contains(t, pr.barElapsed, "a")

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
	pr.Complete("a")
	pr.Failed("b", errors.New("err"))

	pr.StartPhase(2, []string{"c", "d"})
	pr.Complete("c")
	pr.Skipped("d")

	assert.Contains(t, pr.barElapsed, "a")
	assert.True(t, pr.barFailed["b"])
	assert.Contains(t, pr.barElapsed, "c")
	assert.False(t, pr.barFailed["d"])
	assert.Equal(t, 1, pr.barPhases["a"])
	assert.Equal(t, 2, pr.barPhases["c"])
}

func BenchmarkProgressReporter_Lifecycle(b *testing.B) {
	for b.Loop() {
		pr := NewProgressReporter(io.Discard, 3)
		pr.StartPhase(1, []string{"a", "b", "c"})
		pr.Complete("a")
		pr.Failed("b", errors.New("err"))
		pr.Skipped("c")
		pr.Wait()
	}
}
