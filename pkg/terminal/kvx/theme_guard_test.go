// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kvx

import (
	"errors"
	"sync"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/stretchr/testify/assert"
)

// TestRenderMu_ConcurrentRenders stress-tests concurrent calls into the
// exported render entry points that all share renderMu (RenderTable,
// RenderList, View). Run with -race: without the mutex serializing calls
// into the upstream kvx tui package, this reliably trips the data race on
// tui.CurrentTheme()'s unsynchronized package-level theme global. It passing
// under -race is the regression guard for the fix in this file.
func TestRenderMu_ConcurrentRenders(t *testing.T) {
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
func TestWithRenderLock_SerializesConcurrentCallers(t *testing.T) {
	const goroutines = 16
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
