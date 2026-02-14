// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// MPBTestProgress displays animated spinner bars per test using mpb.
// Intended for interactive (TTY) terminals.
//
// Lock ordering: mpb's render goroutine calls decorator closures on a timer.
// Decorator closures must NEVER acquire mu because OnTestStart/OnTestComplete
// hold mu while calling mpb methods (AddBar, Increment, Abort), which would
// deadlock with the render goroutine. Instead, decorator-visible data is stored
// in sync.Map fields that are safe for concurrent lock-free reads.
type MPBTestProgress struct {
	progress *mpb.Progress
	// mu protects bars and barStarts only. It must NOT be held when mpb's
	// render goroutine might call decorator closures.
	mu        sync.Mutex
	bars      map[string]*mpb.Bar
	barStarts map[string]time.Time
	// results and barElapsed are read by decorator closures from mpb's render
	// goroutine, so they use sync.Map to avoid lock ordering issues.
	results    sync.Map // key → *soltesting.TestResult
	barElapsed sync.Map // key → time.Duration
}

// NewMPBTestProgress creates an mpb-based progress reporter that writes to w.
func NewMPBTestProgress(w io.Writer) *MPBTestProgress {
	p := mpb.New(
		mpb.WithOutput(w),
		mpb.WithWidth(0),
		mpb.WithRefreshRate(100*time.Millisecond),
	)
	return &MPBTestProgress{
		progress:  p,
		bars:      make(map[string]*mpb.Bar),
		barStarts: make(map[string]time.Time),
	}
}

func (p *MPBTestProgress) barKey(solution, test string) string {
	return solution + " :: " + test
}

// OnTestStart creates a new spinner bar for the test.
func (p *MPBTestProgress) OnTestStart(solution, test string) {
	key := p.barKey(solution, test)

	p.mu.Lock()
	p.barStarts[key] = time.Now()
	p.mu.Unlock()

	// Elapsed time — shown only after completion.
	// Reads from sync.Map; no mutex needed.
	elapsedDecor := decor.Any(func(s decor.Statistics) string {
		if s.Completed || s.Aborted {
			if v, ok := p.barElapsed.Load(key); ok {
				if d, ok := v.(time.Duration); ok {
					return fmtDuration(d)
				}
			}
		}
		return ""
	})

	// Status text — shown only after completion.
	// Reads from sync.Map; no mutex needed.
	statusDecor := decor.Any(func(s decor.Statistics) string {
		if s.Completed || s.Aborted {
			if v, ok := p.results.Load(key); ok {
				if r, ok := v.(*soltesting.TestResult); ok {
					return fmt.Sprintf("%-5s", r.Status)
				}
			}
		}
		return ""
	})

	bar := p.progress.AddBar(1,
		mpb.PrependDecorators(
			decor.OnAbort(
				decor.OnComplete(
					decor.Spinner(spinnerFrames, decor.WCSyncSpace),
					"✓ ",
				),
				"✗ ",
			),
			decor.Name(key, decor.WCSyncSpaceR),
		),
		mpb.AppendDecorators(statusDecor, decor.Name("  "), elapsedDecor),
		mpb.BarFillerClearOnComplete(),
	)

	p.mu.Lock()
	p.bars[key] = bar
	p.mu.Unlock()
}

// OnTestComplete records the result and marks the bar finished.
func (p *MPBTestProgress) OnTestComplete(result soltesting.TestResult) {
	key := p.barKey(result.Solution, result.Test)
	resultCopy := result

	// Store result and elapsed in sync.Map so decorators can read lock-free.
	p.results.Store(key, &resultCopy)

	p.mu.Lock()
	start, hasStart := p.barStarts[key]
	p.mu.Unlock()

	if hasStart {
		p.barElapsed.Store(key, time.Since(start))
	} else {
		p.barElapsed.Store(key, result.Duration)
	}

	p.mu.Lock()
	bar, ok := p.bars[key]
	p.mu.Unlock()

	if !ok {
		// Test completed without OnTestStart (validation error, dry run, etc.)
		// Create a bar and immediately finish it.
		icon := "✓ "
		switch result.Status {
		case soltesting.StatusPass:
			// icon already set to "✓ "
		case soltesting.StatusFail, soltesting.StatusError:
			icon = "✗ "
		case soltesting.StatusSkip:
			icon = "⊘ "
		}

		bar = p.progress.AddBar(1,
			mpb.PrependDecorators(
				decor.Name(icon),
				decor.Name(key, decor.WCSyncSpaceR),
			),
			mpb.AppendDecorators(
				decor.Name(fmt.Sprintf("%-5s  %s", result.Status, fmtDuration(result.Duration))),
			),
			mpb.BarFillerClearOnComplete(),
		)
		bar.Increment()

		p.mu.Lock()
		p.bars[key] = bar
		p.mu.Unlock()
		return
	}

	switch result.Status {
	case soltesting.StatusFail, soltesting.StatusError:
		bar.Abort(false)
	case soltesting.StatusPass, soltesting.StatusSkip:
		bar.Increment()
	}
}

// Wait blocks until all bars finish rendering.
func (p *MPBTestProgress) Wait() {
	p.progress.Wait()
}

// ---------------------------------------------------------------------------
// LineTestProgress — non-TTY fallback
// ---------------------------------------------------------------------------

// LineTestProgress prints one line per completed test.
// Suitable for non-interactive output (CI logs, piped streams).
type LineTestProgress struct {
	w  io.Writer
	mu sync.Mutex
}

// NewLineTestProgress creates a line-based progress reporter.
func NewLineTestProgress(w io.Writer) *LineTestProgress {
	return &LineTestProgress{w: w}
}

// OnTestStart is a no-op for line-based output.
func (r *LineTestProgress) OnTestStart(_, _ string) {}

// OnTestComplete prints a single status line.
func (r *LineTestProgress) OnTestComplete(result soltesting.TestResult) {
	r.mu.Lock()
	defer r.mu.Unlock()

	icon := "✓"
	switch result.Status {
	case soltesting.StatusPass:
		// icon already set to "✓"
	case soltesting.StatusFail:
		icon = "✗"
	case soltesting.StatusError:
		icon = "!"
	case soltesting.StatusSkip:
		icon = "⊘"
	}

	dur := ""
	if result.Duration > 0 {
		dur = fmt.Sprintf("  (%s)", fmtDuration(result.Duration))
	}

	fmt.Fprintf(r.w, "%s %-5s  %s :: %s%s\n", icon, result.Status, result.Solution, result.Test, dur)
}

// Wait is a no-op for line-based output.
func (r *LineTestProgress) Wait() {}

// ---------------------------------------------------------------------------

// fmtDuration formats a duration in compact human-readable form.
func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}
