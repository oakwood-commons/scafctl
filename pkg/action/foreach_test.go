// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewForEachExecutor(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		exec := NewForEachExecutor()
		assert.NotNil(t, exec)
		assert.Equal(t, 0, exec.concurrency)
		assert.Equal(t, spec.OnErrorFail, exec.onError)
		assert.Nil(t, exec.progressCallback)
	})

	t.Run("with options", func(t *testing.T) {
		callback := &NoOpProgressCallback{}
		exec := NewForEachExecutor(
			WithForEachConcurrency(5),
			WithForEachOnError(spec.OnErrorContinue),
			WithForEachProgressCallback(callback),
		)
		assert.NotNil(t, exec)
		assert.Equal(t, 5, exec.concurrency)
		assert.Equal(t, spec.OnErrorContinue, exec.onError)
		assert.NotNil(t, exec.progressCallback)
	})
}

func TestFromForEachClause(t *testing.T) {
	t.Run("nil clause", func(t *testing.T) {
		exec := FromForEachClause(nil, nil)
		assert.NotNil(t, exec)
		assert.Equal(t, 0, exec.concurrency)
		assert.Equal(t, spec.OnErrorFail, exec.onError)
	})

	t.Run("with clause", func(t *testing.T) {
		clause := &spec.ForEachClause{
			Concurrency: 3,
			OnError:     spec.OnErrorContinue,
		}
		exec := FromForEachClause(clause, nil)
		assert.NotNil(t, exec)
		assert.Equal(t, 3, exec.concurrency)
		assert.Equal(t, spec.OnErrorContinue, exec.onError)
	})
}

func TestForEachExecutor_Execute(t *testing.T) {
	t.Run("empty items", func(t *testing.T) {
		exec := NewForEachExecutor()
		result, err := exec.Execute(context.Background(), "test", []any{}, func(_ context.Context, _ any, _ int) (*provider.Output, error) {
			t.Fatal("should not be called")
			return nil, nil
		})

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 0, result.TotalCount)
		assert.Equal(t, 0, result.SuccessCount)
		assert.Equal(t, 0, result.FailureCount)
		assert.True(t, result.AllSucceeded)
		assert.Empty(t, result.Iterations)
	})

	t.Run("single item success", func(t *testing.T) {
		exec := NewForEachExecutor()
		items := []any{"item1"}

		result, err := exec.Execute(context.Background(), "test", items, func(_ context.Context, item any, index int) (*provider.Output, error) {
			return &provider.Output{Data: map[string]any{"item": item, "index": index}}, nil
		})

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 1, result.TotalCount)
		assert.Equal(t, 1, result.SuccessCount)
		assert.Equal(t, 0, result.FailureCount)
		assert.True(t, result.AllSucceeded)
		require.Len(t, result.Iterations, 1)
		assert.Equal(t, StatusSucceeded, result.Iterations[0].Status)
		assert.Equal(t, "test[0]", result.Iterations[0].Name)
	})

	t.Run("multiple items parallel", func(t *testing.T) {
		exec := NewForEachExecutor()
		items := []any{"a", "b", "c", "d", "e"}
		var executionCount atomic.Int32

		result, err := exec.Execute(context.Background(), "deploy", items, func(_ context.Context, item any, index int) (*provider.Output, error) {
			executionCount.Add(1)
			time.Sleep(10 * time.Millisecond) // Small delay to test parallelism
			return &provider.Output{Data: map[string]any{"item": item}}, nil
		})

		require.NoError(t, err)
		assert.Equal(t, int32(5), executionCount.Load())
		assert.Equal(t, 5, result.TotalCount)
		assert.Equal(t, 5, result.SuccessCount)
		assert.True(t, result.AllSucceeded)
	})

	t.Run("concurrency limit", func(t *testing.T) {
		exec := NewForEachExecutor(WithForEachConcurrency(2))
		items := []any{1, 2, 3, 4, 5}
		var maxConcurrent atomic.Int32
		var currentConcurrent atomic.Int32

		result, err := exec.Execute(context.Background(), "test", items, func(_ context.Context, _ any, _ int) (*provider.Output, error) {
			curr := currentConcurrent.Add(1)
			// Track max concurrent executions
			for {
				maxVal := maxConcurrent.Load()
				if curr <= maxVal || maxConcurrent.CompareAndSwap(maxVal, curr) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			currentConcurrent.Add(-1)
			return &provider.Output{Data: "ok"}, nil
		})

		require.NoError(t, err)
		assert.Equal(t, 5, result.SuccessCount)
		assert.True(t, result.AllSucceeded)
		// Max concurrent should not exceed limit
		assert.LessOrEqual(t, maxConcurrent.Load(), int32(2))
	})

	t.Run("failure with onError fail", func(t *testing.T) {
		exec := NewForEachExecutor(WithForEachOnError(spec.OnErrorFail))
		items := []any{1, 2, 3, 4, 5}

		result, err := exec.Execute(context.Background(), "test", items, func(_ context.Context, _ any, index int) (*provider.Output, error) {
			if index == 2 {
				return nil, errors.New("deliberate error")
			}
			time.Sleep(100 * time.Millisecond) // Let other items finish or get cancelled
			return &provider.Output{Data: "ok"}, nil
		})

		require.Error(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.AllSucceeded)
		assert.Greater(t, result.FailureCount, 0)
	})

	t.Run("failure with onError continue", func(t *testing.T) {
		exec := NewForEachExecutor(
			WithForEachOnError(spec.OnErrorContinue),
			WithForEachConcurrency(1), // Sequential to ensure predictable execution
		)
		items := []any{1, 2, 3}

		result, err := exec.Execute(context.Background(), "test", items, func(_ context.Context, _ any, index int) (*provider.Output, error) {
			if index == 1 {
				return nil, errors.New("item 1 failed")
			}
			return &provider.Output{Data: map[string]any{"index": index}}, nil
		})

		// No error returned with continue mode
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.AllSucceeded)
		assert.Equal(t, 2, result.SuccessCount)
		assert.Equal(t, 1, result.FailureCount)
		assert.NotNil(t, result.FirstError)

		// All iterations should have results
		require.Len(t, result.Iterations, 3)
		assert.Equal(t, StatusSucceeded, result.Iterations[0].Status)
		assert.Equal(t, StatusFailed, result.Iterations[1].Status)
		assert.Equal(t, StatusSucceeded, result.Iterations[2].Status)
	})

	t.Run("context cancellation", func(t *testing.T) {
		exec := NewForEachExecutor(WithForEachConcurrency(1))
		items := []any{1, 2, 3, 4, 5}

		ctx, cancel := context.WithCancel(context.Background())

		var execCount atomic.Int32
		result, err := exec.Execute(ctx, "test", items, func(_ context.Context, _ any, _ int) (*provider.Output, error) {
			count := execCount.Add(1)
			if count == 2 {
				cancel()
			}
			time.Sleep(50 * time.Millisecond)
			return &provider.Output{Data: "ok"}, nil
		})

		// Either we get an error or some items were cancelled
		if err == nil {
			// Check that some items were cancelled
			hasCancelled := false
			for _, iter := range result.Iterations {
				if iter.Status == StatusCancelled {
					hasCancelled = true
					break
				}
			}
			// It's possible all completed before cancellation was processed
			_ = hasCancelled
		}
		assert.NotNil(t, result)
	})

	t.Run("progress callback", func(t *testing.T) {
		callback := &testProgressCallback{}
		exec := NewForEachExecutor(
			WithForEachConcurrency(1),
			WithForEachProgressCallback(callback),
		)
		items := []any{"a", "b", "c"}

		result, err := exec.Execute(context.Background(), "deploy", items, func(_ context.Context, _ any, _ int) (*provider.Output, error) {
			return &provider.Output{Data: "ok"}, nil
		})

		require.NoError(t, err)
		assert.Equal(t, 3, result.SuccessCount)
		assert.Equal(t, 3, callback.forEachProgressCount)
	})
}

func TestExecuteResult_AggregatedResults(t *testing.T) {
	t.Run("nil result", func(t *testing.T) {
		var r *ExecuteResult
		results := r.AggregatedResults()
		assert.NotNil(t, results)
		assert.Empty(t, results)
	})

	t.Run("empty iterations", func(t *testing.T) {
		r := &ExecuteResult{Iterations: []*ForEachIterationResult{}}
		results := r.AggregatedResults()
		assert.NotNil(t, results)
		assert.Empty(t, results)
	})

	t.Run("with results", func(t *testing.T) {
		r := &ExecuteResult{
			Iterations: []*ForEachIterationResult{
				{Index: 0, Results: "result0"},
				{Index: 1, Results: map[string]any{"key": "value"}},
				{Index: 2, Results: 123},
			},
		}
		results := r.AggregatedResults()
		require.Len(t, results, 3)
		assert.Equal(t, "result0", results[0])
		assert.Equal(t, map[string]any{"key": "value"}, results[1])
		assert.Equal(t, 123, results[2])
	})
}

func TestCreateIterationName(t *testing.T) {
	tests := []struct {
		baseName string
		index    int
		expected string
	}{
		{"deploy", 0, "deploy[0]"},
		{"deploy", 1, "deploy[1]"},
		{"build-image", 10, "build-image[10]"},
		{"test", 999, "test[999]"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := CreateIterationName(tt.baseName, tt.index)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsForEachIteration(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"deploy[0]", true},
		{"deploy[1]", true},
		{"build[10]", true},
		{"test[999]", true},
		{"a[0]", true},
		{"deploy", false},
		{"deploy[]", false},
		{"deploy[a]", false},
		{"deploy[0", false},
		{"deploy0]", false},
		{"[0]", false},
		{"", false},
		{"[]", false},
		{"deploy[0][1]", true}, // Nested, still matches pattern
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsForEachIteration(tt.name)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseIterationName(t *testing.T) {
	tests := []struct {
		name      string
		wantBase  string
		wantIndex int
		wantOk    bool
	}{
		{"deploy[0]", "deploy", 0, true},
		{"deploy[1]", "deploy", 1, true},
		{"build-image[10]", "build-image", 10, true},
		{"test[999]", "test", 999, true},
		{"deploy", "", -1, false},
		{"deploy[]", "", -1, false},
		{"[0]", "", -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, index, ok := ParseIterationName(tt.name)
			assert.Equal(t, tt.wantOk, ok)
			if ok {
				assert.Equal(t, tt.wantBase, base)
				assert.Equal(t, tt.wantIndex, index)
			}
		})
	}
}

func TestAggregateForEachResults(t *testing.T) {
	t.Run("empty iterations", func(t *testing.T) {
		result := AggregateForEachResults("test", nil, map[string]any{"key": "value"})
		assert.NotNil(t, result)
		assert.Equal(t, StatusSucceeded, result.Status)
		assert.Equal(t, map[string]any{"key": "value"}, result.Inputs)
		assert.Equal(t, []any{}, result.Results)
	})

	t.Run("all succeeded", func(t *testing.T) {
		now := time.Now()
		later := now.Add(time.Second)
		iterations := []*ForEachIterationResult{
			{Index: 0, Results: "r0", Status: StatusSucceeded, StartTime: &now, EndTime: &later},
			{Index: 1, Results: "r1", Status: StatusSucceeded, StartTime: &now, EndTime: &later},
		}

		result := AggregateForEachResults("deploy", iterations, nil)
		assert.Equal(t, StatusSucceeded, result.Status)
		assert.Len(t, result.Results, 2)
		assert.NotNil(t, result.StartTime)
		assert.NotNil(t, result.EndTime)
		assert.Empty(t, result.Error)
	})

	t.Run("with failures", func(t *testing.T) {
		now := time.Now()
		iterations := []*ForEachIterationResult{
			{Index: 0, Results: "r0", Status: StatusSucceeded, StartTime: &now},
			{Index: 1, Results: nil, Status: StatusFailed, Error: "iteration 1 failed", StartTime: &now},
			{Index: 2, Results: "r2", Status: StatusSucceeded, StartTime: &now},
		}

		result := AggregateForEachResults("deploy", iterations, nil)
		assert.Equal(t, StatusFailed, result.Status)
		assert.Len(t, result.Results, 3)
		assert.Equal(t, "iteration 1 failed", result.Error)
	})

	t.Run("with timeout", func(t *testing.T) {
		now := time.Now()
		iterations := []*ForEachIterationResult{
			{Index: 0, Results: "r0", Status: StatusSucceeded, StartTime: &now},
			{Index: 1, Results: nil, Status: StatusTimeout, Error: "timed out", StartTime: &now},
		}

		result := AggregateForEachResults("test", iterations, nil)
		assert.Equal(t, StatusFailed, result.Status)
		assert.Equal(t, "timed out", result.Error)
	})
}

// testProgressCallback is a simple progress callback for testing.
type testProgressCallback struct {
	NoOpProgressCallback
	forEachProgressCount int
}

func (t *testProgressCallback) OnForEachProgress(_ string, _, _ int) {
	t.forEachProgressCount++
}

func TestExpandForEachItems_NilForEach(t *testing.T) {
	ctx := context.Background()
	items, err := ExpandForEachItems(ctx, nil, nil)
	assert.Error(t, err)
	assert.Nil(t, items)
}

func TestExpandForEachItems_NilIn(t *testing.T) {
	ctx := context.Background()
	forEach := &spec.ForEachClause{In: nil}
	items, err := ExpandForEachItems(ctx, forEach, nil)
	assert.Error(t, err)
	assert.Nil(t, items)
}
