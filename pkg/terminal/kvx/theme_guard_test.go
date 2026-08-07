// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kvx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/stretchr/testify/assert"
)

// kvxThemeRaceSubprocessEnv marks the re-exec'd child process spawned by
// TestRenderMu_ConcurrentRenders, so it runs the actual stress workload
// instead of re-spawning itself again.
const kvxThemeRaceSubprocessEnv = "KVX_THEME_RACE_SUBPROCESS=1"

// TestRenderMu_ConcurrentRenders stress-tests concurrent calls into the
// exported render entry points that all share renderMu (RenderTable,
// RenderList, View). Run with -race: without the mutex serializing calls
// into the upstream kvx tui package, this reliably trips the data race on
// tui.CurrentTheme()'s unsynchronized package-level theme global. It passing
// under -race is the regression guard for the fix in this file.
//
// This only exercises the upstream race while tui's package-level theme is
// still unset (see theme_guard.go). To avoid being order-dependent on
// whichever other test in this package happens to run first and lazily
// initializes the theme, this re-execs itself as a fresh subprocess (via
// os.Args[0], the compiled test binary) so the race check always starts
// from a guaranteed-unset theme global, regardless of test execution order.
func TestRenderMu_ConcurrentRenders(t *testing.T) {
	if os.Getenv("KVX_THEME_RACE_SUBPROCESS") == "1" {
		renderMuConcurrentRendersWorkload(t)
		return
	}

	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestRenderMu_ConcurrentRenders$", "-test.v")
	cmd.Env = append(os.Environ(), kvxThemeRaceSubprocessEnv)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess race check failed: %v\n%s", err, out)
	}
}

// renderMuConcurrentRendersWorkload is the actual concurrent-render stress
// workload, run inside the re-exec'd subprocess so it starts from a fresh,
// unset theme global every time.
func renderMuConcurrentRendersWorkload(t *testing.T) {
	data := []map[string]any{
		{"name": "a", "value": 1},
		{"name": "b", "value": 2},
	}

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := RenderTable(data, tui.TableOptions{})
			assert.NoError(t, err)
		}()
		go func() {
			defer wg.Done()
			_, err := RenderList(data, tui.ListOptions{})
			assert.NoError(t, err)
		}()
	}

	wg.Wait()
}

// TestWithRenderLock_RunsFnAndPropagatesError verifies WithRenderLock both
// executes the wrapped function while holding renderMu, and propagates its
// error unchanged, since external callers (e.g. pkg/auth/loginui) rely on
// the returned error to detect failures/cancellation.
func TestWithRenderLock_RunsFnAndPropagatesError(t *testing.T) {
	called := false
	err := WithRenderLock(func() error {
		called = true
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)

	sentinel := errors.New("boom")
	err = WithRenderLock(func() error {
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel)
}

// TestWithRenderLock_SerializesConcurrentCallers stress-tests WithRenderLock
// itself: concurrent callers must never execute fn simultaneously. This
// guards the external-caller entry point the same way
// TestRenderMu_ConcurrentRenders guards the in-package render call sites.
//
// The critical section does a small, non-optimizable amount of work (a busy
// loop over an atomic counter) between the increment and decrement, rather
// than just incrementing/decrementing back-to-back. An empty critical
// section can pass even if the mutex is accidentally removed, because two
// goroutines are unlikely to race into two bare instructions at the exact
// same instant; widening the window makes an actual concurrency violation
// far more likely to be observed, independent of running with -race.
func TestWithRenderLock_SerializesConcurrentCallers(t *testing.T) {
	const goroutines = 16
	const busyWorkIterations = 10_000
	var active, maxActive int
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			err := WithRenderLock(func() error {
				mu.Lock()
				active++
				if active > maxActive {
					maxActive = active
				}
				mu.Unlock()

				var busy uint64
				for j := 0; j < busyWorkIterations; j++ {
					busy += uint64(j)
				}
				runtime.KeepAlive(busy)

				mu.Lock()
				active--
				mu.Unlock()
				return nil
			})
			assert.NoError(t, err)
		}()
	}

	wg.Wait()
	assert.Equal(t, 1, maxActive, "WithRenderLock must never run fn concurrently")
}
