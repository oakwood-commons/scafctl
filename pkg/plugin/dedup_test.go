// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
)

func TestDedupeRequest_HappyPath(t *testing.T) {
	t.Parallel()
	var g singleflight.Group
	ctx := context.Background()

	result, err := dedupeRequest(ctx, &g, "key1", 5*time.Second, func(_ context.Context) (string, error) {
		return "hello", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestDedupeRequest_ErrorPropagation(t *testing.T) {
	t.Parallel()
	var g singleflight.Group
	ctx := context.Background()
	expectedErr := errors.New("something failed")

	result, err := dedupeRequest(ctx, &g, "key1", 5*time.Second, func(_ context.Context) (string, error) {
		return "", expectedErr
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
	assert.Empty(t, result)
}

func TestDedupeRequest_Deduplication(t *testing.T) {
	t.Parallel()
	var g singleflight.Group
	ctx := context.Background()
	n := 3
	var callCount atomic.Int32
	started := make(chan struct{})

	fn := func(_ context.Context) (int, error) { //nolint:unparam // error is intentionally always nil to test the happy path
		time.Sleep(50 * time.Millisecond)
		callCount.Add(1)
		return 42, nil
	}

	var wg sync.WaitGroup
	results := make([]int, n)
	errs := make([]error, n)
	var atomicCount atomic.Int32
	// Wait for fn to start executing, then launch 2 more concurrent callers.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if atomicCount.Add(1) == int32(n) {
				close(started)
			} else {
				<-started
			}
			results[idx], errs[idx] = dedupeRequest(ctx, &g, "same-key", 5*time.Second, fn)
		}(i)
	}

	wg.Wait()

	// fn executed exactly once.
	assert.Equal(t, int32(1), callCount.Load())
	// All callers got the same result.
	for i := range results {
		assert.NoError(t, errs[i])
		assert.Equal(t, 42, results[i])
	}
}

func TestDedupeRequest_DifferentKeysExecuteIndependently(t *testing.T) {
	t.Parallel()
	var g singleflight.Group
	ctx := context.Background()

	var callCount atomic.Int32

	fn := func(_ context.Context) (string, error) { //nolint:unparam // error is intentionally always nil to test independent execution
		callCount.Add(1)
		return "val", nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := "key-" + string(rune('a'+idx))
			_, _ = dedupeRequest(ctx, &g, key, 5*time.Second, fn)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int32(3), callCount.Load())
}

func TestDedupeRequest_CallerContextCancelled(t *testing.T) {
	t.Parallel()
	var g singleflight.Group
	ctx, cancel := context.WithCancel(context.Background())

	fn := func(_ context.Context) (string, error) {
		// Simulate slow work — outlasts caller context.
		time.Sleep(200 * time.Millisecond)
		return "done", nil
	}

	// Cancel the caller's context after a short delay.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	result, err := dedupeRequest(ctx, &g, "key1", 5*time.Second, func(sfCtx context.Context) (string, error) {
		return fn(sfCtx)
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, result)
}

func TestDedupeRequest_InternalTimeout(t *testing.T) {
	t.Parallel()
	var g singleflight.Group

	result, err := dedupeRequest(context.Background(), &g, "timeout-key", 50*time.Millisecond, func(sfCtx context.Context) (string, error) {
		// Block until the singleflight-internal timeout (50ms) fires.
		<-sfCtx.Done()
		return "", sfCtx.Err()
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDedupTimeout)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, result)
}

func TestDedupeRequest_TimeoutForgetsKey(t *testing.T) {
	t.Parallel()
	var g singleflight.Group
	ctx := context.Background()

	var callCount atomic.Int32

	// First call: times out.
	_, err := dedupeRequest(ctx, &g, "retry-key", 30*time.Millisecond, func(ctx context.Context) (string, error) {
		callCount.Add(1)
		<-ctx.Done()
		return "", ctx.Err()
	})
	require.ErrorIs(t, err, ErrDedupTimeout)

	// Second call with same key: should execute fn again (key was forgotten).
	result, err := dedupeRequest(ctx, &g, "retry-key", 5*time.Second, func(_ context.Context) (string, error) {
		callCount.Add(1)
		return "retried", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "retried", result)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestDedupeRequest_StructType(t *testing.T) {
	t.Parallel()
	var g singleflight.Group
	ctx := context.Background()

	type myResult struct {
		Name  string
		Count int
	}

	expected := myResult{Name: "test-plugin", Count: 7}

	result, err := dedupeRequest(ctx, &g, "struct-key", 5*time.Second, func(_ context.Context) (myResult, error) {
		return expected, nil
	})

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestDedupeRequest_SharedResultOnError(t *testing.T) {
	t.Parallel()
	var g singleflight.Group
	ctx := context.Background()
	n := 3

	expectedErr := errors.New("shared failure")
	started := make(chan struct{})
	fn := func(_ context.Context) (int, error) {
		time.Sleep(50 * time.Millisecond)
		return 0, expectedErr
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	var atomicCount atomic.Int32

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if atomicCount.Add(1) == int32(n) {
				close(started)
			} else {
				<-started
			}
			_, errs[idx] = dedupeRequest(ctx, &g, "err-key", 5*time.Second, fn)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		assert.ErrorIs(t, errs[i], expectedErr)
	}
}
