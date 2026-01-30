package resolver

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutionStatus_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    ExecutionStatus
		expected string
	}{
		{"success status", ExecutionStatusSuccess, "success"},
		{"failed status", ExecutionStatusFailed, "failed"},
		{"skipped status", ExecutionStatusSkipped, "skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.value))
		})
	}
}

func TestNewContext(t *testing.T) {
	ctx := NewContext()
	require.NotNil(t, ctx)
	require.NotNil(t, ctx.data)
	require.NotNil(t, ctx.results)
}

func TestContext_Set_Get(t *testing.T) {
	ctx := NewContext()

	// Test setting and getting a simple value
	ctx.Set("env", "production")
	val, ok := ctx.Get("env")
	require.True(t, ok)
	assert.Equal(t, "production", val)

	// Test getting non-existent value
	_, ok = ctx.Get("nonexistent")
	assert.False(t, ok)
}

func TestContext_SetResult_GetResult(t *testing.T) {
	ctx := NewContext()

	result := &ExecutionResult{
		Value:             "production",
		Status:            ExecutionStatusSuccess,
		Phase:             1,
		TotalDuration:     100 * time.Millisecond,
		ProviderCallCount: 2,
		ValueSizeBytes:    10,
		DependencyCount:   3,
	}

	ctx.SetResult("env", result)

	// Verify we can get the full result
	retrieved, ok := ctx.GetResult("env")
	require.True(t, ok)
	assert.Equal(t, "production", retrieved.Value)
	assert.Equal(t, ExecutionStatusSuccess, retrieved.Status)
	assert.Equal(t, 1, retrieved.Phase)
	assert.Equal(t, 100*time.Millisecond, retrieved.TotalDuration)
	assert.Equal(t, 2, retrieved.ProviderCallCount)
	assert.Equal(t, int64(10), retrieved.ValueSizeBytes)
	assert.Equal(t, 3, retrieved.DependencyCount)

	// Verify we can also get just the value
	val, ok := ctx.Get("env")
	require.True(t, ok)
	assert.Equal(t, "production", val)
}

func TestContext_Has(t *testing.T) {
	ctx := NewContext()

	assert.False(t, ctx.Has("env"))

	ctx.Set("env", "production")
	assert.True(t, ctx.Has("env"))
}

func TestContext_ToMap(t *testing.T) {
	ctx := NewContext()

	ctx.Set("env", "production")
	ctx.Set("region", "us-west-2")
	ctx.Set("count", 5)

	dataMap := ctx.ToMap()
	assert.Len(t, dataMap, 3)
	assert.Equal(t, "production", dataMap["env"])
	assert.Equal(t, "us-west-2", dataMap["region"])
	assert.Equal(t, 5, dataMap["count"])
}

func TestContext_GetAllResults(t *testing.T) {
	ctx := NewContext()

	result1 := &ExecutionResult{
		Value:  "production",
		Status: ExecutionStatusSuccess,
		Phase:  1,
	}
	result2 := &ExecutionResult{
		Value:  "us-west-2",
		Status: ExecutionStatusSuccess,
		Phase:  2,
	}

	ctx.SetResult("env", result1)
	ctx.SetResult("region", result2)

	allResults := ctx.GetAllResults()
	assert.Len(t, allResults, 2)
	assert.Equal(t, "production", allResults["env"].Value)
	assert.Equal(t, "us-west-2", allResults["region"].Value)
	assert.Equal(t, 1, allResults["env"].Phase)
	assert.Equal(t, 2, allResults["region"].Phase)
}

func TestContext_ThreadSafety(t *testing.T) {
	ctx := NewContext()
	const goroutines = 100
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // Writers and readers

	// Writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				result := &ExecutionResult{
					Value:  j,
					Status: ExecutionStatusSuccess,
					Phase:  id,
				}
				ctx.SetResult("key", result)
			}
		}(i)
	}

	// Readers
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ctx.Get("key")
				ctx.GetResult("key")
				ctx.Has("key")
				ctx.ToMap()
				ctx.GetAllResults()
			}
		}()
	}

	wg.Wait()

	// Verify context is still valid
	assert.True(t, ctx.Has("key"))
}

func TestContext_WithContext_FromContext(t *testing.T) {
	resolverCtx := NewContext()
	resolverCtx.Set("env", "production")

	// Add to Go context
	goCtx := WithContext(context.Background(), resolverCtx)

	// Retrieve from Go context
	retrieved, ok := FromContext(goCtx)
	require.True(t, ok)
	require.NotNil(t, retrieved)

	// Verify it's the same context
	val, ok := retrieved.Get("env")
	require.True(t, ok)
	assert.Equal(t, "production", val)
}

func TestContext_FromContext_NotPresent(t *testing.T) {
	ctx := context.Background()

	retrieved, ok := FromContext(ctx)
	assert.False(t, ok)
	assert.Nil(t, retrieved)
}

func TestPhaseMetrics_Structure(t *testing.T) {
	started := time.Now()
	ended := started.Add(100 * time.Millisecond)

	metrics := PhaseMetrics{
		Phase:    "resolve",
		Duration: 100 * time.Millisecond,
		Started:  started,
		Ended:    ended,
	}

	assert.Equal(t, "resolve", metrics.Phase)
	assert.Equal(t, 100*time.Millisecond, metrics.Duration)
	assert.Equal(t, started, metrics.Started)
	assert.Equal(t, ended, metrics.Ended)
}

func TestResolverExecutionResult_CompleteStructure(t *testing.T) {
	started := time.Now()
	ended := started.Add(250 * time.Millisecond)

	result := ExecutionResult{
		Value:             "production",
		Status:            ExecutionStatusSuccess,
		Phase:             1,
		TotalDuration:     250 * time.Millisecond,
		StartTime:         started,
		EndTime:           ended,
		Error:             nil,
		ProviderCallCount: 3,
		ValueSizeBytes:    128,
		DependencyCount:   2,
		PhaseMetrics: []PhaseMetrics{
			{
				Phase:    "resolve",
				Duration: 100 * time.Millisecond,
				Started:  started,
				Ended:    started.Add(100 * time.Millisecond),
			},
			{
				Phase:    "transform",
				Duration: 75 * time.Millisecond,
				Started:  started.Add(100 * time.Millisecond),
				Ended:    started.Add(175 * time.Millisecond),
			},
			{
				Phase:    "validate",
				Duration: 75 * time.Millisecond,
				Started:  started.Add(175 * time.Millisecond),
				Ended:    ended,
			},
		},
	}

	assert.Equal(t, "production", result.Value)
	assert.Equal(t, ExecutionStatusSuccess, result.Status)
	assert.Equal(t, 1, result.Phase)
	assert.Equal(t, 250*time.Millisecond, result.TotalDuration)
	assert.Equal(t, started, result.StartTime)
	assert.Equal(t, ended, result.EndTime)
	assert.Nil(t, result.Error)
	assert.Equal(t, 3, result.ProviderCallCount)
	assert.Equal(t, int64(128), result.ValueSizeBytes)
	assert.Equal(t, 2, result.DependencyCount)
	assert.Len(t, result.PhaseMetrics, 3)

	// Verify phase metrics
	assert.Equal(t, "resolve", result.PhaseMetrics[0].Phase)
	assert.Equal(t, "transform", result.PhaseMetrics[1].Phase)
	assert.Equal(t, "validate", result.PhaseMetrics[2].Phase)
}

func TestContext_Set_CreatesMinimalResult(t *testing.T) {
	ctx := NewContext()
	ctx.Set("env", "production")

	// Verify result was created with minimal metadata
	result, ok := ctx.GetResult("env")
	require.True(t, ok)
	assert.Equal(t, "production", result.Value)
	assert.Equal(t, ExecutionStatusSuccess, result.Status)
	assert.Equal(t, 0, result.Phase)
	assert.Equal(t, time.Duration(0), result.TotalDuration)
}

func TestContext_MultipleValues(t *testing.T) {
	ctx := NewContext()

	// Add multiple values with different types
	ctx.Set("string", "hello")
	ctx.Set("int", 42)
	ctx.Set("bool", true)
	ctx.Set("slice", []string{"a", "b", "c"})
	ctx.Set("map", map[string]int{"x": 1, "y": 2})

	// Verify all values
	val, ok := ctx.Get("string")
	require.True(t, ok)
	assert.Equal(t, "hello", val)

	val, ok = ctx.Get("int")
	require.True(t, ok)
	assert.Equal(t, 42, val)

	val, ok = ctx.Get("bool")
	require.True(t, ok)
	assert.Equal(t, true, val)

	val, ok = ctx.Get("slice")
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, val)

	val, ok = ctx.Get("map")
	require.True(t, ok)
	assert.Equal(t, map[string]int{"x": 1, "y": 2}, val)

	// Verify ToMap contains all values
	dataMap := ctx.ToMap()
	assert.Len(t, dataMap, 5)
}

func TestContext_GetResult_NotFound(t *testing.T) {
	ctx := NewContext()

	result, ok := ctx.GetResult("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestContext_GetResult_WithError(t *testing.T) {
	ctx := NewContext()

	testErr := assert.AnError
	result := &ExecutionResult{
		Value:  nil,
		Status: ExecutionStatusFailed,
		Phase:  1,
		Error:  testErr,
	}

	ctx.SetResult("failed", result)

	retrieved, ok := ctx.GetResult("failed")
	require.True(t, ok)
	assert.Equal(t, ExecutionStatusFailed, retrieved.Status)
	assert.Equal(t, testErr, retrieved.Error)
	assert.Nil(t, retrieved.Value)
}
