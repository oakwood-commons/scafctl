package run

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// ProgressReporter outputs execution progress using mpb.
// It provides visual feedback during resolver execution by displaying
// progress bars for each resolver in the current phase.
type ProgressReporter struct {
	progress   *mpb.Progress
	bars       map[string]*mpb.Bar
	barStarts  map[string]time.Time
	barPhases  map[string]int
	barElapsed map[string]time.Duration // Store calculated elapsed time on completion
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
	)
	return &ProgressReporter{
		progress:   p,
		bars:       make(map[string]*mpb.Bar),
		barStarts:  make(map[string]time.Time),
		barPhases:  make(map[string]int),
		barElapsed: make(map[string]time.Duration),
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
		p.barStarts[name] = time.Now()
		p.barPhases[name] = phaseNum

		// Create a decorator that shows elapsed time on completion (reads from barElapsed map)
		resolverName := name // Capture for closure
		elapsedOnComplete := decor.Any(func(s decor.Statistics) string {
			if s.Completed {
				if elapsed, ok := p.barElapsed[resolverName]; ok {
					return formatDuration(elapsed)
				}
			}
			return "" // Show nothing while in progress
		})

		bar := p.progress.AddBar(1,
			mpb.PrependDecorators(
				decor.Name(fmt.Sprintf("[%d] %s", phaseNum, name), decor.WCSyncSpaceR),
				decor.OnComplete(decor.Spinner([]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}, decor.WCSyncSpace), "✓ "),
			),
			mpb.AppendDecorators(elapsedOnComplete),
			mpb.BarFillerClearOnComplete(),
		)
		p.bars[name] = bar
	}
}

// formatDuration formats a duration showing milliseconds for sub-second durations
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	// Show seconds and milliseconds for longer durations
	secs := d / time.Second
	ms := (d % time.Second) / time.Millisecond
	return fmt.Sprintf("%ds %dms", secs, ms)
}

// Complete marks a resolver as successfully completed.
func (p *ProgressReporter) Complete(resolverName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Calculate and store elapsed time at the moment of completion
	if startTime, ok := p.barStarts[resolverName]; ok {
		p.barElapsed[resolverName] = time.Since(startTime)
	}

	if bar, ok := p.bars[resolverName]; ok {
		bar.Increment()
	}
}

// Failed marks a resolver as failed with the given error.
func (p *ProgressReporter) Failed(resolverName string, _ error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if bar, ok := p.bars[resolverName]; ok {
		bar.Abort(false)
	}
}

// Skipped marks a resolver as skipped (e.g., due to when condition).
func (p *ProgressReporter) Skipped(resolverName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if bar, ok := p.bars[resolverName]; ok {
		bar.SetTotal(0, true)
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
func (c *ProgressCallback) OnResolverComplete(resolverName string) {
	c.reporter.Complete(resolverName)
}

// OnResolverFailed is called when a resolver fails.
func (c *ProgressCallback) OnResolverFailed(resolverName string, err error) {
	c.reporter.Failed(resolverName, err)
}

// OnResolverSkipped is called when a resolver is skipped.
func (c *ProgressCallback) OnResolverSkipped(resolverName string) {
	c.reporter.Skipped(resolverName)
}
