// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverWatchableFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()
	solutionFile := filepath.Join(dir, "solution.yaml")
	err := os.WriteFile(solutionFile, []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
spec:
  tests:
    basic-test:
      command: [render, solution]
      assertions:
        - contains: "hello"
`), 0o644)
	require.NoError(t, err)

	infos, err := discoverWatchableFiles(solutionFile)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, solutionFile, infos[0].SolutionPath)
	assert.Empty(t, infos[0].ComposeFiles)
}

func TestDiscoverWatchableFiles_WithCompose(t *testing.T) {
	dir := t.TempDir()

	composeFile := filepath.Join(dir, "tests.yaml")
	err := os.WriteFile(composeFile, []byte(`
spec:
  tests:
    extra-test:
      command: [render, solution]
      assertions:
        - contains: "world"
`), 0o644)
	require.NoError(t, err)

	solutionFile := filepath.Join(dir, "solution.yaml")
	err = os.WriteFile(solutionFile, []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: compose-solution
compose:
  - tests.yaml
spec:
  tests:
    basic-test:
      command: [render, solution]
      assertions:
        - contains: "hello"
`), 0o644)
	require.NoError(t, err)

	infos, err := discoverWatchableFiles(solutionFile)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, solutionFile, infos[0].SolutionPath)
	require.Len(t, infos[0].ComposeFiles, 1)
	assert.Equal(t, composeFile, infos[0].ComposeFiles[0])
}

func TestDiscoverWatchableFiles_Directory(t *testing.T) {
	dir := t.TempDir()

	sol1 := filepath.Join(dir, "sol1.yaml")
	err := os.WriteFile(sol1, []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: solution-one
spec:
  tests:
    test-one:
      command: [render, solution]
      assertions:
        - contains: "one"
`), 0o644)
	require.NoError(t, err)

	sol2 := filepath.Join(dir, "sol2.yaml")
	err = os.WriteFile(sol2, []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: solution-two
spec:
  tests:
    test-two:
      command: [render, solution]
      assertions:
        - contains: "two"
`), 0o644)
	require.NoError(t, err)

	// Non-solution file should be ignored
	err = os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Not a solution"), 0o644)
	require.NoError(t, err)

	infos, err := discoverWatchableFiles(dir)
	require.NoError(t, err)
	assert.Len(t, infos, 2)
}

func TestDiscoverWatchableFiles_SkipsNonSolution(t *testing.T) {
	dir := t.TempDir()

	// File with no tests and no Solution kind
	nonsol := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(nonsol, []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: something
`), 0o644)
	require.NoError(t, err)

	infos, err := discoverWatchableFiles(dir)
	require.NoError(t, err)
	assert.Empty(t, infos)
}

func TestDiscoverWatchableFiles_NonexistentPath(t *testing.T) {
	_, err := discoverWatchableFiles("/nonexistent/path/solution.yaml")
	assert.Error(t, err)
}

func TestWatcher_DebounceCollapses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping debounce test in short mode")
	}

	dir := t.TempDir()
	solutionFile := filepath.Join(dir, "solution.yaml")
	writeSolution := func() {
		err := os.WriteFile(solutionFile, []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: debounce-test
spec:
  tests:
    basic:
      command: [render, solution]
      assertions:
        - contains: "hello"
`), 0o644)
		require.NoError(t, err)
	}
	writeSolution()

	var mu sync.Mutex
	runStartCount := 0

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	w := &Watcher{
		Runner: &Runner{
			Concurrency: 1,
			DryRun:      true, // don't actually execute commands
		},
		TestsPath: solutionFile,
		Options: WatchOptions{
			DebounceDuration: 200 * time.Millisecond,
			OnRunStart: func(_ string) {
				mu.Lock()
				runStartCount++
				mu.Unlock()
			},
			OnRunComplete: func(_ []TestResult, _ time.Duration, _ error) {
				mu.Lock()
				count := runStartCount
				mu.Unlock()
				// After initial run + 1 debounced run = 2, cancel
				if count >= 2 {
					cancel()
				}
			},
		},
	}

	// Start watching in background
	done := make(chan error, 1)
	go func() {
		done <- w.Watch(ctx)
	}()

	// Wait for initial run to complete
	time.Sleep(500 * time.Millisecond)

	// Rapid successive writes — should be debounced into one run
	for i := 0; i < 5; i++ {
		writeSolution()
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for completion or timeout
	select {
	case err := <-done:
		// context.Canceled is expected
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(6 * time.Second):
		t.Fatal("watcher did not complete within timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	// Initial run = 1, debounced run = 1, total = 2
	assert.Equal(t, 2, runStartCount, "expected exactly 2 runs: initial + 1 debounced")
}

func TestWatcher_CancelStopsCleanly(t *testing.T) {
	dir := t.TempDir()
	solutionFile := filepath.Join(dir, "solution.yaml")
	err := os.WriteFile(solutionFile, []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cancel-test
spec:
  tests:
    basic:
      command: [render, solution]
      assertions:
        - contains: "hello"
`), 0o644)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	w := &Watcher{
		Runner: &Runner{
			Concurrency: 1,
			DryRun:      true,
		},
		TestsPath: solutionFile,
		Options: WatchOptions{
			DebounceDuration: 100 * time.Millisecond,
			OnRunComplete: func(_ []TestResult, _ time.Duration, _ error) {
				// Cancel after initial run
				cancel()
			},
		},
	}

	err = w.Watch(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWatcher_FileCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping file creation test in short mode")
	}

	dir := t.TempDir()
	solutionFile := filepath.Join(dir, "solution.yaml")
	err := os.WriteFile(solutionFile, []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: creation-test
spec:
  tests:
    basic:
      command: [render, solution]
      assertions:
        - contains: "hello"
`), 0o644)
	require.NoError(t, err)

	var mu sync.Mutex
	runCount := 0

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	w := &Watcher{
		Runner: &Runner{
			Concurrency: 1,
			DryRun:      true,
		},
		TestsPath: dir, // watch directory
		Options: WatchOptions{
			DebounceDuration: 200 * time.Millisecond,
			OnRunStart: func(_ string) {
				mu.Lock()
				runCount++
				mu.Unlock()
			},
			OnRunComplete: func(_ []TestResult, _ time.Duration, _ error) {
				mu.Lock()
				count := runCount
				mu.Unlock()
				if count >= 2 {
					cancel()
				}
			},
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- w.Watch(ctx)
	}()

	// Wait for initial run
	time.Sleep(500 * time.Millisecond)

	// Create a new solution file in the watched directory
	newSol := filepath.Join(dir, "new-solution.yaml")
	err = os.WriteFile(newSol, []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: new-solution
spec:
  tests:
    new-test:
      command: [render, solution]
      assertions:
        - contains: "new"
`), 0o644)
	require.NoError(t, err)

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(6 * time.Second):
		t.Fatal("watcher did not react to new file within timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.GreaterOrEqual(t, runCount, 2, "expected at least 2 runs (initial + file creation)")
}

func TestDiscoverSolutionFiles_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "bad.yaml")
	err := os.WriteFile(badFile, []byte(`{{{not valid yaml`), 0o644)
	require.NoError(t, err)

	si, err := discoverSolutionFiles(badFile)
	assert.NoError(t, err) // should not error, just return nil
	assert.Nil(t, si)
}
