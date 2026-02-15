// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultDebounceDuration is the default debounce interval for file changes.
	// Rapid successive writes (e.g. editor save-all) are collapsed into a single re-run.
	DefaultDebounceDuration = 300 * time.Millisecond
)

// WatchOptions configures the watch mode behaviour.
type WatchOptions struct {
	// DebounceDuration controls how long to wait after the last file change
	// before triggering a re-run. Defaults to DefaultDebounceDuration.
	DebounceDuration time.Duration

	// OnRunStart is called just before each test re-run.
	// The string argument identifies the triggering file (relative path when possible).
	OnRunStart func(triggerFile string)

	// OnRunComplete is called after each test re-run with the results.
	OnRunComplete func(results []TestResult, elapsed time.Duration, err error)
}

// WatchResult captures the outcome of a single watch-triggered test run.
type WatchResult struct {
	// TriggerFile is the file change that caused the re-run.
	TriggerFile string
	// Results contains the test outcomes.
	Results []TestResult
	// Elapsed is the wall-clock duration of the run.
	Elapsed time.Duration
	// Err is set when the run fails for infrastructure reasons.
	Err error
}

// Watcher monitors solution files for changes and re-runs affected tests.
type Watcher struct {
	// Runner is the test runner to use for re-runs.
	Runner *Runner

	// TestsPath is the path to the solution file or directory being tested.
	TestsPath string

	// Options configures watch behaviour.
	Options WatchOptions

	// mu protects watchedFiles and solutionFileMap.
	mu              sync.Mutex
	watchedFiles    map[string]bool          // set of files currently monitored
	solutionFileMap map[string]*SolutionInfo // watched file → owning solution info
}

// SolutionInfo tracks the files associated with a single solution.
type SolutionInfo struct {
	// SolutionPath is the absolute path to the main solution file.
	SolutionPath string
	// ComposeFiles are the absolute paths of the compose files referenced by
	// the solution's compose field.
	ComposeFiles []string
}

// Watch starts monitoring files and re-running tests on changes. It blocks
// until ctx is cancelled (typically via Ctrl-C). The initial test run is
// executed immediately before entering the watch loop.
func (w *Watcher) Watch(ctx context.Context) error {
	debounce := w.Options.DebounceDuration
	if debounce == 0 {
		debounce = DefaultDebounceDuration
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating file watcher: %w", err)
	}
	defer watcher.Close()

	// Initial discovery and first run.
	if err := w.discoverAndWatch(watcher); err != nil {
		return fmt.Errorf("initial discovery: %w", err)
	}

	// Run tests once immediately.
	w.triggerRun(ctx, "(initial run)")

	// Watch loop with debounce.
	var (
		debounceTimer *time.Timer
		triggerFile   string
	)

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Only react to writes, creates, and renames (which look like creates).
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) {
				continue
			}

			// Only care about YAML files.
			ext := strings.ToLower(filepath.Ext(event.Name))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}

			triggerFile = event.Name

			// On Create or Rename, try to add the new file to the watcher.
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				_ = watcher.Add(event.Name)
			}

			// Reset the debounce timer.
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.NewTimer(debounce)

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			// Log watcher errors but don't abort — the filesystem watcher
			// can recover from transient errors.
			if w.Options.OnRunComplete != nil {
				w.Options.OnRunComplete(nil, 0, fmt.Errorf("file watcher error: %w", err))
			}

		case <-func() <-chan time.Time {
			if debounceTimer != nil {
				return debounceTimer.C
			}
			// Never fires if no timer is set.
			return make(chan time.Time)
		}():
			debounceTimer = nil

			// Re-discover files (picks up new compose files, renamed solutions).
			if discoverErr := w.discoverAndWatch(watcher); discoverErr != nil {
				if w.Options.OnRunComplete != nil {
					w.Options.OnRunComplete(nil, 0, fmt.Errorf("re-discovery failed: %w", discoverErr))
				}
				continue
			}

			w.triggerRun(ctx, triggerFile)
		}
	}
}

// triggerRun performs a single test run cycle.
func (w *Watcher) triggerRun(ctx context.Context, triggerFile string) {
	if w.Options.OnRunStart != nil {
		w.Options.OnRunStart(triggerFile)
	}

	solutions, err := DiscoverSolutions(w.TestsPath)
	if err != nil {
		if w.Options.OnRunComplete != nil {
			w.Options.OnRunComplete(nil, 0, fmt.Errorf("discovery failed: %w", err))
		}
		return
	}

	if len(solutions) == 0 {
		if w.Options.OnRunComplete != nil {
			w.Options.OnRunComplete(nil, 0, nil)
		}
		return
	}

	start := time.Now()
	results, runErr := w.Runner.Run(ctx, solutions)
	elapsed := time.Since(start)

	// Wait for progress output to flush.
	if w.Runner.Progress != nil {
		w.Runner.Progress.Wait()
	}

	if w.Options.OnRunComplete != nil {
		w.Options.OnRunComplete(results, elapsed, runErr)
	}
}

// discoverAndWatch discovers all solution and compose files, adds them to the
// fsnotify watcher, and updates the internal tracking maps.
func (w *Watcher) discoverAndWatch(watcher *fsnotify.Watcher) error {
	infos, err := discoverWatchableFiles(w.TestsPath)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	newFiles := make(map[string]bool)
	newMap := make(map[string]*SolutionInfo)

	for _, info := range infos {
		// Watch the solution file itself.
		newFiles[info.SolutionPath] = true
		newMap[info.SolutionPath] = info

		// Watch compose files.
		for _, cf := range info.ComposeFiles {
			newFiles[cf] = true
			newMap[cf] = info
		}

		// Watch the directory containing the solution so we catch new files.
		dir := filepath.Dir(info.SolutionPath)
		newFiles[dir] = true
	}

	// Remove watches for files no longer relevant.
	for f := range w.watchedFiles {
		if !newFiles[f] {
			_ = watcher.Remove(f)
		}
	}

	// Add watches for new files.
	for f := range newFiles {
		if !w.watchedFiles[f] {
			if addErr := watcher.Add(f); addErr != nil {
				// Skip files/dirs that no longer exist.
				if !os.IsNotExist(addErr) {
					return fmt.Errorf("watching %q: %w", f, addErr)
				}
			}
		}
	}

	w.watchedFiles = newFiles
	w.solutionFileMap = newMap
	return nil
}

// discoverWatchableFiles returns the set of files to watch for the given path.
func discoverWatchableFiles(testsPath string) ([]*SolutionInfo, error) {
	info, err := os.Stat(testsPath)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", testsPath, err)
	}

	if !info.IsDir() {
		si, err := discoverSolutionFiles(testsPath)
		if err != nil {
			return nil, err
		}
		if si == nil {
			return nil, nil
		}
		return []*SolutionInfo{si}, nil
	}

	var results []*SolutionInfo
	err = filepath.Walk(testsPath, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		si, parseErr := discoverSolutionFiles(path)
		if parseErr != nil || si == nil {
			return nil //nolint:nilerr // skip non-solution files
		}
		results = append(results, si)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %q: %w", testsPath, err)
	}

	return results, nil
}

// discoverSolutionFiles parses a single solution file to extract its path and
// any compose file paths for watching.
func discoverSolutionFiles(filePath string) (*SolutionInfo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", filePath, err)
	}

	var doc struct {
		APIVersion string   `yaml:"apiVersion"`
		Kind       string   `yaml:"kind"`
		Compose    []string `yaml:"compose"`
		Spec       struct {
			Tests map[string]any `yaml:"tests"`
		} `yaml:"spec"`
	}

	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil //nolint:nilerr // not a valid YAML file for our purposes
	}

	// Only watch solution files (must be a Solution kind or have tests).
	if doc.Kind != "Solution" && len(doc.Spec.Tests) == 0 {
		return nil, nil
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	si := &SolutionInfo{
		SolutionPath: absPath,
	}

	// Resolve compose file globs.
	if len(doc.Compose) > 0 {
		solutionDir := filepath.Dir(absPath)
		for _, pattern := range doc.Compose {
			absPattern := pattern
			if !filepath.IsAbs(pattern) {
				absPattern = filepath.Join(solutionDir, pattern)
			}
			matches, globErr := doublestar.FilepathGlob(absPattern)
			if globErr != nil {
				continue // skip invalid globs
			}
			si.ComposeFiles = append(si.ComposeFiles, matches...)
		}
	}

	return si, nil
}
