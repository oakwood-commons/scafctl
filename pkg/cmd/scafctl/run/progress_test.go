// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/terminal/format"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadElapsed reads a duration from the barElapsed sync.Map.
func loadElapsed(pr *ProgressReporter, name string) (time.Duration, bool) {
	v, ok := pr.barElapsed.Load(name)
	if !ok {
		return 0, false
	}
	d, isD := v.(time.Duration)
	return d, isD
}

// loadFailed reads a bool from the barFailed sync.Map.
func loadFailed(pr *ProgressReporter, name string) bool {
	v, ok := pr.barFailed.Load(name)
	if !ok {
		return false
	}
	b, isBool := v.(bool)
	return isBool && b
}

func TestNewProgressReporter(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 5)
	require.NotNil(t, pr)
	assert.Equal(t, 5, pr.total)
	assert.NotNil(t, pr.bars)
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

	elapsed, ok := loadElapsed(pr, "resolver-a")
	assert.True(t, ok)
	assert.Equal(t, 150*time.Millisecond, elapsed)
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

	assert.True(t, loadFailed(pr, "resolver-a"))
}

func TestProgressReporter_Skipped(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	pr.StartPhase(1, []string{"resolver-a"})

	pr.Skipped("resolver-a")

	// Skipped bars are aborted but NOT marked as failed
	assert.False(t, loadFailed(pr, "resolver-a"))
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
	assert.True(t, loadFailed(pr, "nonexistent"))
}

func TestProgressReporter_Wait(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	pr.startTime = time.Now().Add(-time.Millisecond)
	pr.StartPhase(1, []string{"resolver-a"})
	pr.Complete("resolver-a", 50*time.Millisecond)

	dur := pr.Wait()
	assert.Greater(t, dur, time.Duration(0))
}

func TestProgressReporter_TotalDuration(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	pr.startTime = time.Now().Add(-time.Millisecond) // Ensure non-zero elapsed time.

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
	elapsed, ok := loadElapsed(pr, "a")
	assert.True(t, ok)
	assert.Equal(t, 100*time.Millisecond, elapsed)

	cb.OnResolverFailed("b", errors.New("fail"))
	assert.True(t, loadFailed(pr, "b"))
}

func TestProgressCallback_OnResolverSkipped(t *testing.T) {
	t.Parallel()
	pr := NewProgressReporter(io.Discard, 1)
	cb := NewProgressCallback(pr)

	cb.OnPhaseStart(1, []string{"skippable"})
	cb.OnResolverSkipped("skippable")

	assert.False(t, loadFailed(pr, "skippable"))
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

	elapsedA, okA := loadElapsed(pr, "a")
	assert.True(t, okA)
	assert.Equal(t, 50*time.Millisecond, elapsedA)
	assert.True(t, loadFailed(pr, "b"))
	elapsedC, okC := loadElapsed(pr, "c")
	assert.True(t, okC)
	assert.Equal(t, 75*time.Millisecond, elapsedC)
	assert.False(t, loadFailed(pr, "d"))
	assert.Equal(t, 1, pr.barPhases["a"])
	assert.Equal(t, 2, pr.barPhases["c"])
}

// TestProgressReporter_MultiPhase_NoConcurrentDeadlock is a regression test
// for #474: cancelled bars leaking in mpb's heap caused width-sync deadlocks
// when subsequent phases started new bars. This test exercises the exact
// scenario (many phases, mix of complete/fail/skip) with a tight deadline.
func TestProgressReporter_MultiPhase_NoConcurrentDeadlock(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	go func() {
		defer close(done)

		pr := NewProgressReporter(io.Discard, 20)

		// Simulate 8 phases with mixed outcomes
		for phase := 1; phase <= 8; phase++ {
			names := make([]string, 3)
			for i := range names {
				names[i] = fmt.Sprintf("p%d-r%d", phase, i)
			}
			pr.StartPhase(phase, names)

			pr.Complete(names[0], time.Duration(phase)*time.Millisecond)
			pr.Failed(names[1], errors.New("err"))
			pr.Skipped(names[2])
		}

		pr.Wait()
	}()

	select {
	case <-done:
		// Success — no deadlock.
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: ProgressReporter.Wait() did not return within 10s")
	}
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
	elapsed, ok := loadElapsed(pr, "resolver-x")
	assert.True(t, ok)
	assert.Equal(t, 200*time.Millisecond, elapsed)
	assert.False(t, loadFailed(pr, "resolver-x"))
}

func TestProgressReporter_StartPhase_Failed_RendersWithoutPanic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pr := NewProgressReporter(&buf, 1)
	pr.StartPhase(1, []string{"resolver-y"})
	pr.Failed("resolver-y", errors.New("boom"))
	pr.Wait()

	assert.True(t, loadFailed(pr, "resolver-y"))
}

func TestProgressReporter_StartPhase_Skipped_RendersWithoutPanic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pr := NewProgressReporter(&buf, 1)
	pr.StartPhase(1, []string{"resolver-z"})
	pr.Skipped("resolver-z")
	pr.Wait()

	assert.False(t, loadFailed(pr, "resolver-z"))
}

func TestRunSpinnerFrames_PackageLevel(t *testing.T) {
	t.Parallel()
	assert.Len(t, runSpinnerFrames, 10, "runSpinnerFrames should have 10 frames")
	assert.Equal(t, "⠋", runSpinnerFrames[0])
}

// TestProgressReporter_DecoratorRendering exercises decorator closures by
// allowing render cycles to fire before completing/failing/skipping bars.
// This improves coverage of the StartPhase decorator callbacks.
func TestProgressReporter_DecoratorRendering(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pr := NewProgressReporter(&buf, 3)
	pr.StartPhase(1, []string{"running", "failing", "skipping"})

	// Allow at least two render cycles (refresh rate is 100ms) to
	// exercise the spinner decorator while bars are in-progress.
	time.Sleep(250 * time.Millisecond)

	pr.Complete("running", 100*time.Millisecond)
	pr.Failed("failing", errors.New("err"))
	pr.Skipped("skipping")
	pr.Wait()

	// Verify internal state is correct after decorators rendered.
	// (BarRemoveOnComplete clears terminal output, so we verify state instead.)
	elapsed, ok := loadElapsed(pr, "running")
	assert.True(t, ok)
	assert.Equal(t, 100*time.Millisecond, elapsed)
	assert.True(t, loadFailed(pr, "failing"))
	assert.False(t, loadFailed(pr, "skipping"))
}

// ── Extracted decorator function tests ────────────────────────────────────────

func TestElapsedText(t *testing.T) {
	t.Parallel()
	var m sync.Map

	// Not completed — always empty
	assert.Equal(t, "", elapsedText(&m, "r1", false))

	// Completed but no value stored
	assert.Equal(t, "", elapsedText(&m, "r1", true))

	// Completed with stored duration
	m.Store("r1", 150*time.Millisecond)
	assert.Equal(t, format.Duration(150*time.Millisecond), elapsedText(&m, "r1", true))

	// Wrong type stored — returns empty
	m.Store("r2", "not-a-duration")
	assert.Equal(t, "", elapsedText(&m, "r2", true))
}

func TestStatusText(t *testing.T) {
	t.Parallel()
	var m sync.Map

	// Completed
	assert.Equal(t, "✓ ", statusText(&m, "r1", true, false, 0))

	// Aborted + failed
	m.Store("r1", true)
	assert.Equal(t, "✗ ", statusText(&m, "r1", false, true, 0))

	// Aborted + not failed (skipped)
	assert.Equal(t, "⊘ ", statusText(&m, "r2", false, true, 0))

	// Aborted + wrong type stored — treated as skipped
	m.Store("r3", "not-a-bool")
	assert.Equal(t, "⊘ ", statusText(&m, "r3", false, true, 0))

	// In-progress — spinner frames
	assert.Equal(t, "⠋ ", statusText(&m, "r1", false, false, 0))
	assert.Equal(t, "⠙ ", statusText(&m, "r1", false, false, 1))
	assert.Equal(t, "⠋ ", statusText(&m, "r1", false, false, 10)) // wraps
}
