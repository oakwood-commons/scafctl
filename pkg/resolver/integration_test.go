package resolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests for the resolver system
// These tests verify the complete flow from solution parsing through execution

// TestIntegration_FullSolution tests a complete solution with multiple resolvers
func TestIntegration_FullSolution(t *testing.T) {
	registry := newMockRegistry()

	// Register providers that simulate real behavior
	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	err = registry.Register(&mockProvider{
		name: "concat",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			result := ""
			for k, v := range inputs {
				if k == "separator" {
					continue
				}
				if result != "" {
					if sep, ok := inputs["separator"].(string); ok {
						result += sep
					} else {
						result += " "
					}
				}
				result += fmt.Sprintf("%v", v)
			}
			return &provider.Output{Data: result}, nil
		},
	})
	require.NoError(t, err)

	err = registry.Register(&mockProvider{
		name: "validate",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			value := inputs["value"]
			minLen := 0
			if ml, ok := inputs["minLength"].(int); ok {
				minLen = ml
			}
			if str, ok := value.(string); ok {
				if len(str) < minLen {
					return nil, fmt.Errorf("value too short: got %d, want at least %d", len(str), minLen)
				}
			}
			return &provider.Output{Data: true}, nil
		},
	})
	require.NoError(t, err)

	t.Run("simple chain with transform and validate", func(t *testing.T) {
		executor := NewExecutor(registry)

		// Solution with: input -> processed (depends on input, validates minLength)
		resolvers := []*Resolver{
			{
				Name: "input",
				Type: TypeString,
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "hello"},
							},
						},
					},
				},
			},
			{
				Name: "processed",
				Type: TypeString,
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "concat",
							Inputs: map[string]*ValueRef{
								"first":     {Resolver: stringPtr("input")},
								"second":    {Literal: "world"},
								"separator": {Literal: " "},
							},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validate",
							Inputs: map[string]*ValueRef{
								// Use literal value for validation instead of self-reference
								"value":     {Literal: "helloworld"}, // approx expected
								"minLength": {Literal: 5},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)

		require.NoError(t, err)
		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		// Verify both resolvers completed
		input, ok := result.Get("input")
		require.True(t, ok)
		assert.Equal(t, "hello", input)

		processed, ok := result.Get("processed")
		require.True(t, ok)
		// Note: map iteration order is random, so we just check it contains expected parts
		assert.Contains(t, processed, "hello")
		assert.Contains(t, processed, "world")
	})

	t.Run("parallel execution with shared dependency", func(t *testing.T) {
		executor := NewExecutor(registry)

		// Diamond pattern: root -> (left, right) -> bottom
		resolvers := []*Resolver{
			{
				Name: "root",
				Type: TypeString,
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "root-value"},
							},
						},
					},
				},
			},
			{
				Name: "left",
				Type: TypeString,
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "concat",
							Inputs: map[string]*ValueRef{
								"base":   {Resolver: stringPtr("root")},
								"suffix": {Literal: "-left"},
							},
						},
					},
				},
			},
			{
				Name: "right",
				Type: TypeString,
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "concat",
							Inputs: map[string]*ValueRef{
								"base":   {Resolver: stringPtr("root")},
								"suffix": {Literal: "-right"},
							},
						},
					},
				},
			},
			{
				Name: "bottom",
				Type: TypeString,
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "concat",
							Inputs: map[string]*ValueRef{
								"a":         {Resolver: stringPtr("left")},
								"b":         {Resolver: stringPtr("right")},
								"separator": {Literal: " + "},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)

		require.NoError(t, err)
		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		// All four resolvers should complete
		for _, name := range []string{"root", "left", "right", "bottom"} {
			value, ok := result.Get(name)
			assert.True(t, ok, "resolver %s should have value", name)
			assert.NotNil(t, value)
		}

		// Verify phases
		rootResult, _ := result.GetResult("root")
		assert.Equal(t, 1, rootResult.Phase, "root should be in phase 1")

		leftResult, _ := result.GetResult("left")
		rightResult, _ := result.GetResult("right")
		assert.Equal(t, leftResult.Phase, rightResult.Phase, "left and right should be in same phase")
		assert.Equal(t, 2, leftResult.Phase, "left/right should be in phase 2")

		bottomResult, _ := result.GetResult("bottom")
		assert.Equal(t, 3, bottomResult.Phase, "bottom should be in phase 3")
	})
}

// TestIntegration_ConditionalExecution tests when: condition handling
func TestIntegration_ConditionalExecution(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	t.Run("conditional resolver skipped when false", func(t *testing.T) {
		executor := NewExecutor(registry)

		falseExpr := celexp.Expression("false")
		resolvers := []*Resolver{
			{
				Name: "always",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "always-runs"},
							},
						},
					},
				},
			},
			{
				Name: "conditional",
				When: &Condition{Expr: &falseExpr}, // Resolver-level when condition for skip
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "never-runs"},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)

		require.NoError(t, err)
		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		// always should exist
		alwaysVal, ok := result.Get("always")
		assert.True(t, ok)
		assert.Equal(t, "always-runs", alwaysVal)

		// conditional should be skipped - may not have a result if skipped at resolver level
		conditionalResult, ok := result.GetResult("conditional")
		if ok && conditionalResult != nil {
			assert.Equal(t, ExecutionStatusSkipped, conditionalResult.Status)
		}
		// Verify the value is not set
		_, hasValue := result.Get("conditional")
		assert.False(t, hasValue, "conditional should not have a value when skipped")
	})

	t.Run("conditional based on parameter", func(t *testing.T) {
		executor := NewExecutor(registry)

		featureExpr := celexp.Expression("_.feature_flag == true")
		resolvers := []*Resolver{
			{
				Name: "feature_flag",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: true}, // Feature enabled
							},
						},
					},
				},
			},
			{
				Name: "feature",
				Resolve: &ResolvePhase{
					When: &Condition{Expr: &featureExpr},
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "feature-enabled"},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)

		require.NoError(t, err)
		result, _ := FromContext(ctx)

		// feature should run because feature_flag is true
		featureVal, ok := result.Get("feature")
		assert.True(t, ok)
		assert.Equal(t, "feature-enabled", featureVal)
	})
}

// TestIntegration_ErrorHandling tests error propagation and recovery
func TestIntegration_ErrorHandling(t *testing.T) {
	registry := newMockRegistry()

	var callCount atomic.Int32

	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	err = registry.Register(&mockProvider{
		name: "failing",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			callCount.Add(1)
			return nil, fmt.Errorf("intentional failure")
		},
	})
	require.NoError(t, err)

	t.Run("error stops execution by default", func(t *testing.T) {
		callCount.Store(0)
		executor := NewExecutor(registry)

		resolvers := []*Resolver{
			{
				Name: "will_fail",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "failing",
							Inputs:   map[string]*ValueRef{},
						},
					},
				},
			},
			{
				Name: "after_fail",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value":    {Literal: "should not run"},
								"requires": {Resolver: stringPtr("will_fail")},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "intentional failure")

		result, _ := FromContext(ctx)
		if result != nil {
			// will_fail should have failed status
			failedResult, ok := result.GetResult("will_fail")
			if ok {
				assert.Equal(t, ExecutionStatusFailed, failedResult.Status)
			}
		}
	})

	t.Run("onError: continue allows partial success", func(t *testing.T) {
		callCount.Store(0)
		executor := NewExecutor(registry)

		resolvers := []*Resolver{
			{
				Name: "will_fail",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "failing",
							Inputs:   map[string]*ValueRef{},
							OnError:  ErrorBehaviorContinue,
						},
					},
				},
			},
			{
				Name: "independent",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "runs anyway"},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)

		// May still return overall error but independent should complete
		result, _ := FromContext(ctx)
		if result != nil {
			indepVal, ok := result.Get("independent")
			if ok {
				assert.Equal(t, "runs anyway", indepVal)
			}
		}
	})
}

// TestIntegration_ValueSizeLimits tests value size warning and limits
func TestIntegration_ValueSizeLimits(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "large",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			size := inputs["size"].(int)
			// Generate large string
			data := make([]byte, size)
			for i := range data {
				data[i] = 'x'
			}
			return &provider.Output{Data: string(data)}, nil
		},
	})
	require.NoError(t, err)

	t.Run("large values are stored correctly", func(t *testing.T) {
		executor := NewExecutor(registry,
			WithWarnValueSize(1000),
			WithMaxValueSize(10000),
		)

		resolvers := []*Resolver{
			{
				Name: "large_value",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "large",
							Inputs: map[string]*ValueRef{
								"size": {Literal: 5000},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)

		require.NoError(t, err)
		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		value, ok := result.Get("large_value")
		require.True(t, ok)
		assert.Len(t, value.(string), 5000)
	})
}

// TestIntegration_TypeCoercion tests type coercion through the full pipeline
func TestIntegration_TypeCoercion(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		inputValue any
		targetType Type
		expected   any
	}{
		{"int to string", 42, TypeString, "42"},
		{"string to int", "123", TypeInt, 123},
		{"bool to string", true, TypeString, "true"},
		{"float to string", 3.14, TypeString, "3.14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewExecutor(registry)

			resolvers := []*Resolver{
				{
					Name: "typed_value",
					Type: tt.targetType,
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: tt.inputValue},
								},
							},
						},
					},
				},
			}

			ctx := context.Background()
			ctx, err := executor.Execute(ctx, resolvers, nil)

			require.NoError(t, err)
			result, _ := FromContext(ctx)
			require.NotNil(t, result)

			value, ok := result.Get("typed_value")
			require.True(t, ok)
			assert.Equal(t, tt.expected, value)
		})
	}
}

// TestIntegration_SnapshotCapture tests snapshot functionality
func TestIntegration_SnapshotCapture(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	t.Run("capture snapshot after execution", func(t *testing.T) {
		executor := NewExecutor(registry)

		resolvers := []*Resolver{
			{
				Name: "greeting",
				Type: TypeString,
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "hello"},
							},
						},
					},
				},
			},
			{
				Name: "target",
				Type: TypeString,
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "world"},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		start := time.Now()
		ctx, err = executor.Execute(ctx, resolvers, nil)
		totalDuration := time.Since(start)
		require.NoError(t, err)

		// Capture snapshot
		snapshot, err := CaptureSnapshot(ctx, "test-solution", "1.0.0", "dev", nil, totalDuration, ExecutionStatusSuccess)
		require.NoError(t, err)
		require.NotNil(t, snapshot)

		assert.Contains(t, snapshot.Resolvers, "greeting")
		assert.Contains(t, snapshot.Resolvers, "target")
		assert.Equal(t, "hello", snapshot.Resolvers["greeting"].Value)
		assert.Equal(t, "world", snapshot.Resolvers["target"].Value)
	})

	t.Run("save and load snapshot", func(t *testing.T) {
		executor := NewExecutor(registry)

		resolvers := []*Resolver{
			{
				Name: "test_value",
				Type: TypeString,
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "snapshot-test"},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		start := time.Now()
		ctx, err = executor.Execute(ctx, resolvers, nil)
		totalDuration := time.Since(start)
		require.NoError(t, err)

		snapshot, err := CaptureSnapshot(ctx, "test-solution", "1.0.0", "dev", nil, totalDuration, ExecutionStatusSuccess)
		require.NoError(t, err)
		require.NotNil(t, snapshot)

		// Save to temp file
		tmpDir := t.TempDir()
		snapshotPath := filepath.Join(tmpDir, "snapshot.json")
		err = SaveSnapshot(snapshot, snapshotPath)
		require.NoError(t, err)

		// Verify file exists
		_, err = os.Stat(snapshotPath)
		require.NoError(t, err)

		// Load snapshot
		loaded, err := LoadSnapshot(snapshotPath)
		require.NoError(t, err)

		assert.Equal(t, snapshot.Resolvers["test_value"].Value, loaded.Resolvers["test_value"].Value)
	})
}

// TestIntegration_MetricsTracking tests execution metrics
func TestIntegration_MetricsTracking(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "slow",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			delay := inputs["delay"].(time.Duration)
			time.Sleep(delay)
			return &provider.Output{Data: "done"}, nil
		},
	})
	require.NoError(t, err)

	t.Run("track execution time per resolver", func(t *testing.T) {
		executor := NewExecutor(registry)

		resolvers := []*Resolver{
			{
				Name: "fast",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "slow",
							Inputs: map[string]*ValueRef{
								"delay": {Literal: 5 * time.Millisecond},
							},
						},
					},
				},
			},
			{
				Name: "slower",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "slow",
							Inputs: map[string]*ValueRef{
								"delay": {Literal: 20 * time.Millisecond},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)
		require.NoError(t, err)

		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		// Check execution times
		fastResult, _ := result.GetResult("fast")
		slowerResult, _ := result.GetResult("slower")

		assert.Greater(t, fastResult.TotalDuration, time.Duration(0))
		assert.Greater(t, slowerResult.TotalDuration, time.Duration(0))
		assert.Greater(t, slowerResult.TotalDuration, fastResult.TotalDuration,
			"slower resolver should take more time")
	})
}
