package action

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContext(t *testing.T) {
	ctx := NewContext()
	require.NotNil(t, ctx)
	assert.NotNil(t, ctx.actions)
	assert.NotNil(t, ctx.iterations)
	assert.Empty(t, ctx.actions)
	assert.Empty(t, ctx.iterations)
}

func TestContext_SetResult_GetResult(t *testing.T) {
	ctx := NewContext()
	now := time.Now()

	result := &ActionResult{
		Inputs:    map[string]any{"cmd": "echo hello"},
		Results:   map[string]any{"exitCode": 0, "stdout": "hello"},
		Status:    StatusSucceeded,
		StartTime: &now,
		EndTime:   &now,
	}

	ctx.SetResult("build", result)

	retrieved, ok := ctx.GetResult("build")
	assert.True(t, ok)
	assert.Equal(t, result, retrieved)

	// Non-existent result
	_, ok = ctx.GetResult("nonexistent")
	assert.False(t, ok)
}

func TestContext_HasResult(t *testing.T) {
	ctx := NewContext()

	assert.False(t, ctx.HasResult("build"))

	ctx.SetResult("build", &ActionResult{Status: StatusSucceeded})

	assert.True(t, ctx.HasResult("build"))
	assert.False(t, ctx.HasResult("deploy"))
}

func TestContext_GetNamespace(t *testing.T) {
	ctx := NewContext()
	now := time.Now()

	ctx.SetResult("build", &ActionResult{
		Inputs:    map[string]any{"cmd": "go build"},
		Results:   map[string]any{"exitCode": 0},
		Status:    StatusSucceeded,
		StartTime: &now,
		EndTime:   &now,
	})

	ctx.SetResult("deploy", &ActionResult{
		Inputs:     map[string]any{"target": "prod"},
		Results:    nil,
		Status:     StatusSkipped,
		SkipReason: SkipReasonCondition,
		StartTime:  &now,
		EndTime:    &now,
	})

	namespace := ctx.GetNamespace()
	require.Len(t, namespace, 2)

	// Check build action
	build, ok := namespace["build"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"exitCode": 0}, build["results"])
	assert.Equal(t, "succeeded", build["status"])

	// Check deploy action
	deploy, ok := namespace["deploy"].(map[string]any)
	require.True(t, ok)
	assert.Nil(t, deploy["results"])
	assert.Equal(t, "skipped", deploy["status"])
	assert.Equal(t, "condition", deploy["skipReason"])
}

func TestContext_GetNamespace_WithError(t *testing.T) {
	ctx := NewContext()
	now := time.Now()

	ctx.SetResult("build", &ActionResult{
		Inputs:    map[string]any{"cmd": "go build"},
		Results:   map[string]any{"exitCode": 1},
		Status:    StatusFailed,
		StartTime: &now,
		EndTime:   &now,
		Error:     "build failed: exit code 1",
	})

	namespace := ctx.GetNamespace()
	build := namespace["build"].(map[string]any)
	assert.Equal(t, "failed", build["status"])
	assert.Equal(t, "build failed: exit code 1", build["error"])
}

func TestContext_AddIteration_GetIterations(t *testing.T) {
	ctx := NewContext()
	now := time.Now()

	iter1 := &ForEachIterationResult{
		Index:     0,
		Name:      "build[0]",
		Results:   map[string]any{"output": "file1.go"},
		Status:    StatusSucceeded,
		StartTime: &now,
		EndTime:   &now,
	}

	iter2 := &ForEachIterationResult{
		Index:     1,
		Name:      "build[1]",
		Results:   map[string]any{"output": "file2.go"},
		Status:    StatusSucceeded,
		StartTime: &now,
		EndTime:   &now,
	}

	ctx.AddIteration("build", iter1)
	ctx.AddIteration("build", iter2)

	iterations := ctx.GetIterations("build")
	require.Len(t, iterations, 2)
	assert.Equal(t, iter1, iterations[0])
	assert.Equal(t, iter2, iterations[1])

	// Non-existent
	noIter := ctx.GetIterations("nonexistent")
	assert.Nil(t, noIter)
}

func TestContext_FinalizeForEach(t *testing.T) {
	ctx := NewContext()
	start1 := time.Now()
	end1 := start1.Add(time.Second)
	start2 := start1.Add(2 * time.Second)
	end2 := start2.Add(time.Second)

	ctx.AddIteration("build", &ForEachIterationResult{
		Index:     0,
		Name:      "build[0]",
		Results:   map[string]any{"file": "a.go"},
		Status:    StatusSucceeded,
		StartTime: &start1,
		EndTime:   &end1,
	})

	ctx.AddIteration("build", &ForEachIterationResult{
		Index:     1,
		Name:      "build[1]",
		Results:   map[string]any{"file": "b.go"},
		Status:    StatusSucceeded,
		StartTime: &start2,
		EndTime:   &end2,
	})

	inputs := map[string]any{"pattern": "*.go"}
	result := ctx.FinalizeForEach("build", inputs)

	require.NotNil(t, result)
	assert.Equal(t, inputs, result.Inputs)
	assert.Equal(t, StatusSucceeded, result.Status)

	// Results should be array of iteration results
	results, ok := result.Results.([]any)
	require.True(t, ok)
	require.Len(t, results, 2)

	// Start time should be earliest
	assert.Equal(t, start1, *result.StartTime)
	// End time should be latest
	assert.Equal(t, end2, *result.EndTime)

	// Should be stored in actions map
	stored, ok := ctx.GetResult("build")
	assert.True(t, ok)
	assert.Equal(t, result, stored)
}

func TestContext_FinalizeForEach_WithFailure(t *testing.T) {
	ctx := NewContext()
	now := time.Now()

	ctx.AddIteration("build", &ForEachIterationResult{
		Index:     0,
		Name:      "build[0]",
		Results:   map[string]any{"file": "a.go"},
		Status:    StatusSucceeded,
		StartTime: &now,
		EndTime:   &now,
	})

	ctx.AddIteration("build", &ForEachIterationResult{
		Index:     1,
		Name:      "build[1]",
		Results:   nil,
		Status:    StatusFailed,
		StartTime: &now,
		EndTime:   &now,
		Error:     "compilation error",
	})

	result := ctx.FinalizeForEach("build", nil)

	assert.Equal(t, StatusFailed, result.Status)
	assert.Equal(t, "compilation error", result.Error)
}

func TestContext_FinalizeForEach_Empty(t *testing.T) {
	ctx := NewContext()

	result := ctx.FinalizeForEach("empty", nil)

	assert.Equal(t, StatusSucceeded, result.Status)
	results, ok := result.Results.([]any)
	require.True(t, ok)
	assert.Empty(t, results)
}

func TestContext_MarkRunning(t *testing.T) {
	ctx := NewContext()
	inputs := map[string]any{"cmd": "go build"}

	ctx.MarkRunning("build", inputs)

	result, ok := ctx.GetResult("build")
	require.True(t, ok)
	assert.Equal(t, StatusRunning, result.Status)
	assert.Equal(t, inputs, result.Inputs)
	assert.NotNil(t, result.StartTime)
	assert.Nil(t, result.EndTime)
}

func TestContext_MarkSucceeded(t *testing.T) {
	ctx := NewContext()
	inputs := map[string]any{"cmd": "go build"}
	ctx.MarkRunning("build", inputs)

	results := map[string]any{"exitCode": 0}
	ctx.MarkSucceeded("build", results)

	result, ok := ctx.GetResult("build")
	require.True(t, ok)
	assert.Equal(t, StatusSucceeded, result.Status)
	assert.Equal(t, results, result.Results)
	assert.NotNil(t, result.EndTime)
}

func TestContext_MarkSucceeded_WithoutPrior(t *testing.T) {
	ctx := NewContext()

	results := map[string]any{"exitCode": 0}
	ctx.MarkSucceeded("build", results)

	result, ok := ctx.GetResult("build")
	require.True(t, ok)
	assert.Equal(t, StatusSucceeded, result.Status)
	assert.NotNil(t, result.StartTime)
	assert.NotNil(t, result.EndTime)
}

func TestContext_MarkFailed(t *testing.T) {
	ctx := NewContext()
	ctx.MarkRunning("build", nil)

	ctx.MarkFailed("build", "compilation error")

	result, ok := ctx.GetResult("build")
	require.True(t, ok)
	assert.Equal(t, StatusFailed, result.Status)
	assert.Equal(t, "compilation error", result.Error)
	assert.NotNil(t, result.EndTime)
}

func TestContext_MarkSkipped(t *testing.T) {
	ctx := NewContext()

	ctx.MarkSkipped("deploy", SkipReasonCondition)

	result, ok := ctx.GetResult("deploy")
	require.True(t, ok)
	assert.Equal(t, StatusSkipped, result.Status)
	assert.Equal(t, SkipReasonCondition, result.SkipReason)
}

func TestContext_MarkTimeout(t *testing.T) {
	ctx := NewContext()
	ctx.MarkRunning("longTask", nil)

	ctx.MarkTimeout("longTask")

	result, ok := ctx.GetResult("longTask")
	require.True(t, ok)
	assert.Equal(t, StatusTimeout, result.Status)
	assert.Equal(t, "action timed out", result.Error)
}

func TestContext_MarkCancelled(t *testing.T) {
	ctx := NewContext()
	ctx.MarkRunning("build", nil)

	ctx.MarkCancelled("build")

	result, ok := ctx.GetResult("build")
	require.True(t, ok)
	assert.Equal(t, StatusCancelled, result.Status)
}

func TestContext_AllActionNames(t *testing.T) {
	ctx := NewContext()

	ctx.SetResult("build", &ActionResult{Status: StatusSucceeded})
	ctx.SetResult("test", &ActionResult{Status: StatusSucceeded})
	ctx.SetResult("deploy", &ActionResult{Status: StatusSkipped})

	names := ctx.AllActionNames()
	assert.Len(t, names, 3)
	assert.Contains(t, names, "build")
	assert.Contains(t, names, "test")
	assert.Contains(t, names, "deploy")
}

func TestContext_ActionCount(t *testing.T) {
	ctx := NewContext()
	assert.Equal(t, 0, ctx.ActionCount())

	ctx.SetResult("build", &ActionResult{Status: StatusSucceeded})
	assert.Equal(t, 1, ctx.ActionCount())

	ctx.SetResult("test", &ActionResult{Status: StatusSucceeded})
	assert.Equal(t, 2, ctx.ActionCount())
}

func TestContext_Reset(t *testing.T) {
	ctx := NewContext()

	ctx.SetResult("build", &ActionResult{Status: StatusSucceeded})
	ctx.AddIteration("build", &ForEachIterationResult{Index: 0})

	ctx.Reset()

	assert.Equal(t, 0, ctx.ActionCount())
	assert.Nil(t, ctx.GetIterations("build"))
}

func TestContext_Clone(t *testing.T) {
	ctx := NewContext()
	now := time.Now()

	ctx.SetResult("build", &ActionResult{
		Inputs:    map[string]any{"cmd": "go build"},
		Results:   map[string]any{"exitCode": 0},
		Status:    StatusSucceeded,
		StartTime: &now,
		EndTime:   &now,
	})

	ctx.AddIteration("foreach", &ForEachIterationResult{
		Index:     0,
		Name:      "foreach[0]",
		Results:   "result0",
		Status:    StatusSucceeded,
		StartTime: &now,
		EndTime:   &now,
	})

	clone := ctx.Clone()

	// Should be equal
	result, ok := clone.GetResult("build")
	require.True(t, ok)
	assert.Equal(t, StatusSucceeded, result.Status)

	iterations := clone.GetIterations("foreach")
	require.Len(t, iterations, 1)
	assert.Equal(t, 0, iterations[0].Index)

	// Should be independent
	ctx.SetResult("build", &ActionResult{Status: StatusFailed})

	result, _ = clone.GetResult("build")
	assert.Equal(t, StatusSucceeded, result.Status, "clone should not be affected")
}

func TestContext_ThreadSafety(t *testing.T) {
	ctx := NewContext()
	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := "action" + string(rune('A'+idx%26))
			ctx.SetResult(name, &ActionResult{
				Status: StatusSucceeded,
			})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ctx.GetNamespace()
		}()
	}

	// Concurrent iteration adds
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx.AddIteration("foreach", &ForEachIterationResult{
				Index: idx,
			})
		}(i)
	}

	wg.Wait()

	// Should have some actions
	assert.Greater(t, ctx.ActionCount(), 0)

	// Should have all iterations
	iterations := ctx.GetIterations("foreach")
	assert.Len(t, iterations, numGoroutines)
}

func TestActionResultToMap(t *testing.T) {
	tests := []struct {
		name     string
		result   *ActionResult
		expected map[string]any
	}{
		{
			name:     "nil result",
			result:   nil,
			expected: nil,
		},
		{
			name: "success result",
			result: &ActionResult{
				Inputs:  map[string]any{"key": "value"},
				Results: map[string]any{"output": "data"},
				Status:  StatusSucceeded,
			},
			expected: map[string]any{
				"inputs":  map[string]any{"key": "value"},
				"results": map[string]any{"output": "data"},
				"status":  "succeeded",
			},
		},
		{
			name: "skipped result",
			result: &ActionResult{
				Inputs:     nil,
				Results:    nil,
				Status:     StatusSkipped,
				SkipReason: SkipReasonDependencyFailed,
			},
			expected: map[string]any{
				"results":    nil,
				"status":     "skipped",
				"skipReason": "dependency-failed",
			},
		},
		{
			name: "failed result",
			result: &ActionResult{
				Inputs:  nil,
				Results: nil,
				Status:  StatusFailed,
				Error:   "something went wrong",
			},
			expected: map[string]any{
				"results": nil,
				"status":  "failed",
				"error":   "something went wrong",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := actionResultToMap(tt.result)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				for k, v := range tt.expected {
					assert.Equal(t, v, result[k], "key %s mismatch", k)
				}
			}
		})
	}
}

func TestCopyMap(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name:     "nil map",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: map[string]any{},
		},
		{
			name:     "map with values",
			input:    map[string]any{"a": 1, "b": "two"},
			expected: map[string]any{"a": 1, "b": "two"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := copyMap(tt.input)
			assert.Equal(t, tt.expected, result)

			// Verify independence
			if len(tt.input) > 0 {
				result["new"] = "value"
				assert.NotContains(t, tt.input, "new")
			}
		})
	}
}
