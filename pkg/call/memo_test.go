// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package call

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemo_Do_ComputesOncePerKey(t *testing.T) {
	m := NewMemo()

	var calls int32
	fn := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return "value", nil
	}

	for i := 0; i < 5; i++ {
		got, err := m.Do("key-a", fn)
		require.NoError(t, err)
		assert.Equal(t, "value", got)
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "fn should run exactly once per key")
}

func TestMemo_Do_DistinctKeysComputeIndependently(t *testing.T) {
	m := NewMemo()

	var calls int32
	makeFn := func(v string) func() (any, error) {
		return func() (any, error) {
			atomic.AddInt32(&calls, 1)
			return v, nil
		}
	}

	a, err := m.Do("a", makeFn("A"))
	require.NoError(t, err)
	b, err := m.Do("b", makeFn("B"))
	require.NoError(t, err)

	assert.Equal(t, "A", a)
	assert.Equal(t, "B", b)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestMemo_Do_MemoizesError(t *testing.T) {
	m := NewMemo()

	sentinel := errors.New("boom")
	var calls int32
	fn := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return nil, sentinel
	}

	_, err1 := m.Do("k", fn)
	_, err2 := m.Do("k", fn)

	assert.ErrorIs(t, err1, sentinel)
	assert.ErrorIs(t, err2, sentinel)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "errors are cached like values")
}

func TestMemo_Do_ConcurrentSameKey(t *testing.T) {
	m := NewMemo()

	var calls int32
	fn := func() (any, error) { //nolint:unparam // Do requires a (any, error) signature
		atomic.AddInt32(&calls, 1)
		return 42, nil
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	results := make([]any, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			v, err := m.Do("shared", fn)
			assert.NoError(t, err)
			results[idx] = v
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "shared key computes once under concurrency")
	for _, r := range results {
		assert.Equal(t, 42, r)
	}
}
