// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/terminal/format"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var runSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ProgressReporter outputs execution progress using mpb.
// It provides visual feedback during resolver execution by displaying
// progress bars for each resolver in the current phase.
type ProgressReporter struct {
	progress   *mpb.Progress
	bars       map[string]*mpb.Bar
	barPhases  map[string]int
	barElapsed map[string]time.Duration // Store calculated elapsed time on completion
	barFailed  map[string]bool          // Track whether an aborted bar was a failure (vs skip)
	total      int
	startTime  time.Time
	writer     io.Writer
	mu         sync.Mutex
}

// NewProgressReporter creates a new progress reporter writing to the given output.
// The total parameter indicates the total number of resolvers to be executed.
func NewProgressReporter(writer io.Writer, total int) *ProgressReporter {
	p := mpb.New(
		mpb.WithOutput(writer),
		mpb.WithWidth(40),
		mpb.WithRefreshRate(100*time.Millisecond),
		mpb.PopCompletedMode(),
	)
	return &ProgressReporter{
		progress:   p,
		bars:       make(map[string]*mpb.Bar),
		barPhases:  make(map[string]int),
		barElapsed: make(map[string]time.Duration),
		barFailed:  make(map[string]bool),
		total:      total,
		startTime:  time.Now(),
		writer:     writer,
	}
}

// StartPhase creates progress bars for all resolvers in a phase.
// This should be called at the beginning of each execution phase.
func (p *ProgressReporter) StartPhase(phaseNum int, resolverNames []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, name := range resolverNames {
		p.barPhases[name] = phaseNum

		// Create a decorator that shows elapsed time on completion (reads from barElapsed map)
		resolverName := name // Capture for closure
		elapsedOnComplete := decor.Any(func(s decor.Statistics) string {
			if s.Completed {
				p.mu.Lock()
				elapsed, ok := p.barElapsed[resolverName]
				p.mu.Unlock()
				if ok {
					return format.Duration(elapsed)
				}
			}
			return "" // Show nothing while in progress
		})

		// Build a status decorator that distinguishes completion, failure, and skip.
		// Uses an atomic counter for spinner frame selection instead of wall-clock time.
		resolverStatus := name // Capture for closure
		var spinCount atomic.Uint64
		statusDecorator := decor.Any(func(s decor.Statistics) string {
			if s.Completed {
				return "✓ "
			}
			if s.Aborted {
				p.mu.Lock()
				failed := p.barFailed[resolverStatus]
				p.mu.Unlock()
				if failed {
					return "✗ "
				}
				return "⊘ "
			}
			// In-progress spinner
			idx := spinCount.Add(1) - 1
			return runSpinnerFrames[idx%uint64(len(runSpinnerFrames))] + " "
		}, decor.WCSyncSpace)

		bar := p.progress.AddBar(1,
			mpb.PrependDecorators(
				decor.Name(fmt.Sprintf("[%d] %s", phaseNum, name), decor.WCSyncSpaceR),
				statusDecorator,
			),
			mpb.AppendDecorators(elapsedOnComplete),
			mpb.BarFillerClearOnComplete(),
		)
		p.bars[name] = bar
	}
}

// Complete marks a resolver as successfully completed.
// elapsed is the pure execution time measured by the caller.
func (p *ProgressReporter) Complete(resolverName string, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.barElapsed[resolverName] = elapsed

	if bar, ok := p.bars[resolverName]; ok {
		bar.Increment()
	}
}

// Failed marks a resolver as failed with the given error.
func (p *ProgressReporter) Failed(resolverName string, _ error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.barFailed[resolverName] = true
	if bar, ok := p.bars[resolverName]; ok {
		bar.Abort(false)
	}
}

// Skipped marks a resolver as skipped (e.g., due to when condition).
func (p *ProgressReporter) Skipped(resolverName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if bar, ok := p.bars[resolverName]; ok {
		bar.Abort(false)
	}
}

// Wait waits for all progress bars to complete and returns the total duration.
func (p *ProgressReporter) Wait() time.Duration {
	p.progress.Wait()
	return time.Since(p.startTime)
}

// TotalDuration returns the elapsed time since the reporter was created.
func (p *ProgressReporter) TotalDuration() time.Duration {
	return time.Since(p.startTime)
}

// ProgressCallback provides a callback interface that can be used with the executor.
// This allows the progress reporter to be notified of execution events.
type ProgressCallback struct {
	reporter *ProgressReporter
}

// NewProgressCallback creates a new progress callback wrapping the given reporter.
func NewProgressCallback(reporter *ProgressReporter) *ProgressCallback {
	return &ProgressCallback{reporter: reporter}
}

// OnPhaseStart is called when a new execution phase begins.
func (c *ProgressCallback) OnPhaseStart(phaseNum int, resolverNames []string) {
	c.reporter.StartPhase(phaseNum, resolverNames)
}

// OnResolverComplete is called when a resolver completes successfully.
func (c *ProgressCallback) OnResolverComplete(resolverName string, elapsed time.Duration) {
	c.reporter.Complete(resolverName, elapsed)
}

// OnResolverFailed is called when a resolver fails.
func (c *ProgressCallback) OnResolverFailed(resolverName string, err error) {
	c.reporter.Failed(resolverName, err)
}

// OnResolverSkipped is called when a resolver is skipped.
func (c *ProgressCallback) OnResolverSkipped(resolverName string) {
	c.reporter.Skipped(resolverName)
}
