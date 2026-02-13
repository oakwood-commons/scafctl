// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRetryExecutor(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		executor := NewRetryExecutor(nil)
		assert.NotNil(t, executor)
		assert.Equal(t, 1, executor.MaxAttempts())
	})

	t.Run("with config", func(t *testing.T) {
		config := &RetryConfig{
			MaxAttempts: 3,
			Backoff:     BackoffExponential,
		}
		executor := NewRetryExecutor(config)
		assert.NotNil(t, executor)
		assert.Equal(t, 3, executor.MaxAttempts())
	})
}

func TestRetryExecutor_MaxAttempts(t *testing.T) {
	tests := []struct {
		name     string
		config   *RetryConfig
		expected int
	}{
		{
			name:     "nil config returns 1",
			config:   nil,
			expected: 1,
		},
		{
			name:     "zero maxAttempts returns 1",
			config:   &RetryConfig{MaxAttempts: 0},
			expected: 1,
		},
		{
			name:     "negative maxAttempts returns 1",
			config:   &RetryConfig{MaxAttempts: -1},
			expected: 1,
		},
		{
			name:     "valid maxAttempts",
			config:   &RetryConfig{MaxAttempts: 5},
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewRetryExecutor(tt.config)
			assert.Equal(t, tt.expected, executor.MaxAttempts())
		})
	}
}

func TestRetryExecutor_CalculateDelay(t *testing.T) {
	second := duration.New(time.Second)
	fiveSeconds := duration.New(5 * time.Second)
	thirtySeconds := duration.New(30 * time.Second)

	tests := []struct {
		name     string
		config   *RetryConfig
		attempt  int
		expected time.Duration
	}{
		// Nil config
		{
			name:     "nil config returns 0",
			config:   nil,
			attempt:  2,
			expected: 0,
		},
		// First attempt
		{
			name:     "first attempt returns 0",
			config:   &RetryConfig{MaxAttempts: 3},
			attempt:  1,
			expected: 0,
		},
		// Fixed backoff
		{
			name: "fixed backoff attempt 2",
			config: &RetryConfig{
				MaxAttempts:  3,
				Backoff:      BackoffFixed,
				InitialDelay: &second,
			},
			attempt:  2,
			expected: time.Second,
		},
		{
			name: "fixed backoff attempt 3",
			config: &RetryConfig{
				MaxAttempts:  5,
				Backoff:      BackoffFixed,
				InitialDelay: &fiveSeconds,
			},
			attempt:  3,
			expected: 5 * time.Second,
		},
		// Linear backoff
		{
			name: "linear backoff attempt 2",
			config: &RetryConfig{
				MaxAttempts:  5,
				Backoff:      BackoffLinear,
				InitialDelay: &second,
			},
			attempt:  2,
			expected: time.Second, // 1s * 1
		},
		{
			name: "linear backoff attempt 3",
			config: &RetryConfig{
				MaxAttempts:  5,
				Backoff:      BackoffLinear,
				InitialDelay: &second,
			},
			attempt:  3,
			expected: 2 * time.Second, // 1s * 2
		},
		{
			name: "linear backoff attempt 5",
			config: &RetryConfig{
				MaxAttempts:  5,
				Backoff:      BackoffLinear,
				InitialDelay: &second,
			},
			attempt:  5,
			expected: 4 * time.Second, // 1s * 4
		},
		// Exponential backoff
		{
			name: "exponential backoff attempt 2",
			config: &RetryConfig{
				MaxAttempts:  5,
				Backoff:      BackoffExponential,
				InitialDelay: &second,
			},
			attempt:  2,
			expected: time.Second, // 1s * 2^0
		},
		{
			name: "exponential backoff attempt 3",
			config: &RetryConfig{
				MaxAttempts:  5,
				Backoff:      BackoffExponential,
				InitialDelay: &second,
			},
			attempt:  3,
			expected: 2 * time.Second, // 1s * 2^1
		},
		{
			name: "exponential backoff attempt 4",
			config: &RetryConfig{
				MaxAttempts:  5,
				Backoff:      BackoffExponential,
				InitialDelay: &second,
			},
			attempt:  4,
			expected: 4 * time.Second, // 1s * 2^2
		},
		{
			name: "exponential backoff attempt 5",
			config: &RetryConfig{
				MaxAttempts:  5,
				Backoff:      BackoffExponential,
				InitialDelay: &second,
			},
			attempt:  5,
			expected: 8 * time.Second, // 1s * 2^3
		},
		// Max delay cap
		{
			name: "max delay caps linear",
			config: &RetryConfig{
				MaxAttempts:  10,
				Backoff:      BackoffLinear,
				InitialDelay: &fiveSeconds,
				MaxDelay:     &thirtySeconds,
			},
			attempt:  10,
			expected: 30 * time.Second, // Would be 45s but capped at 30s
		},
		{
			name: "max delay caps exponential",
			config: &RetryConfig{
				MaxAttempts:  10,
				Backoff:      BackoffExponential,
				InitialDelay: &second,
				MaxDelay:     &thirtySeconds,
			},
			attempt:  10,
			expected: 30 * time.Second, // Would be 256s but capped at 30s
		},
		// Empty backoff defaults to fixed
		{
			name: "empty backoff defaults to fixed",
			config: &RetryConfig{
				MaxAttempts:  3,
				Backoff:      "",
				InitialDelay: &second,
			},
			attempt:  2,
			expected: time.Second,
		},
		// No initial delay uses 1s default
		{
			name: "no initial delay uses default",
			config: &RetryConfig{
				MaxAttempts: 3,
				Backoff:     BackoffFixed,
			},
			attempt:  2,
			expected: time.Second, // Default 1s
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewRetryExecutor(tt.config)
			delay := executor.CalculateDelay(tt.attempt)
			assert.Equal(t, tt.expected, delay)
		})
	}
}

func TestRetryExecutor_WithJitter(t *testing.T) {
	second := duration.New(time.Second)
	config := &RetryConfig{
		MaxAttempts:  3,
		Backoff:      BackoffFixed,
		InitialDelay: &second,
	}

	executor := NewRetryExecutor(config).WithJitter(func(d time.Duration) time.Duration {
		return d + 500*time.Millisecond
	})

	delay := executor.CalculateDelay(2)
	assert.Equal(t, 1500*time.Millisecond, delay)
}

func TestRetryExecutor_ShouldRetry(t *testing.T) {
	config := &RetryConfig{
		MaxAttempts: 3,
	}

	t.Run("nil error returns false", func(t *testing.T) {
		executor := NewRetryExecutor(config)
		shouldRetry, err := executor.ShouldRetry(context.Background(), nil, 1)
		require.NoError(t, err)
		assert.False(t, shouldRetry)
	})

	t.Run("nil config returns false", func(t *testing.T) {
		executor := NewRetryExecutor(nil)
		shouldRetry, err := executor.ShouldRetry(context.Background(), errors.New("error"), 1)
		require.NoError(t, err)
		assert.False(t, shouldRetry)
	})

	t.Run("max attempts reached returns false", func(t *testing.T) {
		executor := NewRetryExecutor(config)
		shouldRetry, err := executor.ShouldRetry(context.Background(), errors.New("error"), 3)
		require.NoError(t, err)
		assert.False(t, shouldRetry)
	})

	t.Run("cancelled context returns false", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		executor := NewRetryExecutor(config)
		shouldRetry, err := executor.ShouldRetry(ctx, errors.New("error"), 1)
		require.NoError(t, err)
		assert.False(t, shouldRetry)
	})

	t.Run("should retry on error before max attempts", func(t *testing.T) {
		executor := NewRetryExecutor(config)
		shouldRetry1, err := executor.ShouldRetry(context.Background(), errors.New("error"), 1)
		require.NoError(t, err)
		assert.True(t, shouldRetry1)
		shouldRetry2, err := executor.ShouldRetry(context.Background(), errors.New("error"), 2)
		require.NoError(t, err)
		assert.True(t, shouldRetry2)
	})
}

func TestRetryExecutor_ExecuteWithRetry(t *testing.T) {
	t.Run("success on first attempt", func(t *testing.T) {
		executor := NewRetryExecutor(&RetryConfig{MaxAttempts: 3})

		calls := 0
		output, err := executor.ExecuteWithRetry(
			context.Background(),
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				calls++
				return &provider.Output{Data: "success"}, nil
			},
			nil,
		)

		require.NoError(t, err)
		assert.Equal(t, "success", output.Data)
		assert.Equal(t, 1, calls)
	})

	t.Run("success on second attempt", func(t *testing.T) {
		tenMillis := duration.New(10 * time.Millisecond)
		executor := NewRetryExecutor(&RetryConfig{
			MaxAttempts:  3,
			InitialDelay: &tenMillis,
		})

		calls := 0
		output, err := executor.ExecuteWithRetry(
			context.Background(),
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				calls++
				if calls == 1 {
					return nil, errors.New("transient error")
				}
				return &provider.Output{Data: "success"}, nil
			},
			nil,
		)

		require.NoError(t, err)
		assert.Equal(t, "success", output.Data)
		assert.Equal(t, 2, calls)
	})

	t.Run("all attempts fail", func(t *testing.T) {
		tenMillis := duration.New(10 * time.Millisecond)
		executor := NewRetryExecutor(&RetryConfig{
			MaxAttempts:  3,
			InitialDelay: &tenMillis,
		})

		calls := 0
		output, err := executor.ExecuteWithRetry(
			context.Background(),
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				calls++
				return nil, errors.New("persistent error")
			},
			nil,
		)

		require.Error(t, err)
		assert.Nil(t, output)
		assert.Equal(t, 3, calls)
		assert.Contains(t, err.Error(), "failed after 3 attempt(s)")
		assert.Contains(t, err.Error(), "persistent error")
	})

	t.Run("no retries with nil config", func(t *testing.T) {
		executor := NewRetryExecutor(nil)

		calls := 0
		output, err := executor.ExecuteWithRetry(
			context.Background(),
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				calls++
				return nil, errors.New("error")
			},
			nil,
		)

		require.Error(t, err)
		assert.Nil(t, output)
		assert.Equal(t, 1, calls)
	})

	t.Run("cancelled during retry delay", func(t *testing.T) {
		hundredMillis := duration.New(100 * time.Millisecond)
		executor := NewRetryExecutor(&RetryConfig{
			MaxAttempts:  3,
			InitialDelay: &hundredMillis,
		})

		ctx, cancel := context.WithCancel(context.Background())
		calls := 0

		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		output, err := executor.ExecuteWithRetry(
			ctx,
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				calls++
				return nil, errors.New("error")
			},
			nil,
		)

		require.Error(t, err)
		assert.Nil(t, output)
		assert.Equal(t, 1, calls) // Only first attempt, cancelled during delay
		assert.Contains(t, err.Error(), "cancelled")
	})

	t.Run("cancelled before first attempt", func(t *testing.T) {
		executor := NewRetryExecutor(&RetryConfig{MaxAttempts: 3})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		output, err := executor.ExecuteWithRetry(
			ctx,
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				return &provider.Output{Data: "success"}, nil
			},
			nil,
		)

		require.Error(t, err)
		assert.Nil(t, output)
		assert.Contains(t, err.Error(), "cancelled")
	})

	t.Run("callback called on retry", func(t *testing.T) {
		tenMillis := duration.New(10 * time.Millisecond)
		executor := NewRetryExecutor(&RetryConfig{
			MaxAttempts:  3,
			InitialDelay: &tenMillis,
		})

		var callbacks []struct {
			name        string
			attempt     int
			maxAttempts int
		}

		callback := &mockRetryCallback{
			onRetryAttempt: func(name string, attempt, maxAttempts int, _ error) {
				callbacks = append(callbacks, struct {
					name        string
					attempt     int
					maxAttempts int
				}{name, attempt, maxAttempts})
			},
		}

		calls := 0
		_, _ = executor.ExecuteWithRetry(
			context.Background(),
			"my-action",
			func(_ context.Context) (*provider.Output, error) {
				calls++
				if calls < 3 {
					return nil, errors.New("error")
				}
				return &provider.Output{Data: "success"}, nil
			},
			callback,
		)

		require.Len(t, callbacks, 2)
		assert.Equal(t, "my-action", callbacks[0].name)
		assert.Equal(t, 2, callbacks[0].attempt)
		assert.Equal(t, 3, callbacks[0].maxAttempts)
		assert.Equal(t, 3, callbacks[1].attempt)
	})
}

func TestRetryExecutor_ExecuteWithRetryDetailed(t *testing.T) {
	t.Run("successful result", func(t *testing.T) {
		executor := NewRetryExecutor(&RetryConfig{MaxAttempts: 3})

		result := executor.ExecuteWithRetryDetailed(
			context.Background(),
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				return &provider.Output{Data: "success"}, nil
			},
			nil,
		)

		require.NotNil(t, result)
		assert.NotNil(t, result.Output)
		assert.Equal(t, "success", result.Output.Data)
		assert.Equal(t, 1, result.Attempts)
		assert.Nil(t, result.FinalError)
		assert.Empty(t, result.AttemptErrors)
		assert.Greater(t, result.TotalDuration, time.Duration(0))
	})

	t.Run("failed result with retries", func(t *testing.T) {
		tenMillis := duration.New(10 * time.Millisecond)
		executor := NewRetryExecutor(&RetryConfig{
			MaxAttempts:  3,
			InitialDelay: &tenMillis,
		})

		calls := int32(0)
		result := executor.ExecuteWithRetryDetailed(
			context.Background(),
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				atomic.AddInt32(&calls, 1)
				return nil, errors.New("error")
			},
			nil,
		)

		require.NotNil(t, result)
		assert.Nil(t, result.Output)
		assert.Equal(t, 3, result.Attempts)
		assert.NotNil(t, result.FinalError)
		assert.Len(t, result.AttemptErrors, 3)
	})

	t.Run("success after retries", func(t *testing.T) {
		tenMillis := duration.New(10 * time.Millisecond)
		executor := NewRetryExecutor(&RetryConfig{
			MaxAttempts:  3,
			InitialDelay: &tenMillis,
		})

		calls := 0
		result := executor.ExecuteWithRetryDetailed(
			context.Background(),
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				calls++
				if calls < 3 {
					return nil, errors.New("error")
				}
				return &provider.Output{Data: "success"}, nil
			},
			nil,
		)

		require.NotNil(t, result)
		assert.NotNil(t, result.Output)
		assert.Equal(t, 3, result.Attempts)
		assert.Nil(t, result.FinalError)
		assert.Len(t, result.AttemptErrors, 2) // Two failures before success
	})
}

// mockRetryCallback is a mock implementation of RetryCallback for testing.
type mockRetryCallback struct {
	onRetryAttempt func(actionName string, attempt, maxAttempts int, err error)
}

func (m *mockRetryCallback) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	if m.onRetryAttempt != nil {
		m.onRetryAttempt(actionName, attempt, maxAttempts, err)
	}
}

func TestRetryExecutor_BackoffStrategies(t *testing.T) {
	// Verify the mathematical properties of backoff strategies

	t.Run("fixed backoff is constant", func(t *testing.T) {
		second := duration.New(time.Second)
		executor := NewRetryExecutor(&RetryConfig{
			MaxAttempts:  5,
			Backoff:      BackoffFixed,
			InitialDelay: &second,
		})

		for attempt := 2; attempt <= 5; attempt++ {
			assert.Equal(t, time.Second, executor.CalculateDelay(attempt))
		}
	})

	t.Run("linear backoff increases linearly", func(t *testing.T) {
		second := duration.New(time.Second)
		executor := NewRetryExecutor(&RetryConfig{
			MaxAttempts:  5,
			Backoff:      BackoffLinear,
			InitialDelay: &second,
		})

		// attempt 2 -> delay = 1s * 1 = 1s
		// attempt 3 -> delay = 1s * 2 = 2s
		// attempt 4 -> delay = 1s * 3 = 3s
		// attempt 5 -> delay = 1s * 4 = 4s
		assert.Equal(t, 1*time.Second, executor.CalculateDelay(2))
		assert.Equal(t, 2*time.Second, executor.CalculateDelay(3))
		assert.Equal(t, 3*time.Second, executor.CalculateDelay(4))
		assert.Equal(t, 4*time.Second, executor.CalculateDelay(5))
	})

	t.Run("exponential backoff doubles", func(t *testing.T) {
		second := duration.New(time.Second)
		executor := NewRetryExecutor(&RetryConfig{
			MaxAttempts:  6,
			Backoff:      BackoffExponential,
			InitialDelay: &second,
		})

		// attempt 2 -> delay = 1s * 2^0 = 1s
		// attempt 3 -> delay = 1s * 2^1 = 2s
		// attempt 4 -> delay = 1s * 2^2 = 4s
		// attempt 5 -> delay = 1s * 2^3 = 8s
		// attempt 6 -> delay = 1s * 2^4 = 16s
		assert.Equal(t, 1*time.Second, executor.CalculateDelay(2))
		assert.Equal(t, 2*time.Second, executor.CalculateDelay(3))
		assert.Equal(t, 4*time.Second, executor.CalculateDelay(4))
		assert.Equal(t, 8*time.Second, executor.CalculateDelay(5))
		assert.Equal(t, 16*time.Second, executor.CalculateDelay(6))
	})
}

func TestRetryExecutor_ShouldRetry_WithRetryIf(t *testing.T) {
	t.Run("retryIf true allows retry", func(t *testing.T) {
		retryIfExpr := celexp.Expression("__error.statusCode == 429")
		config := &RetryConfig{
			MaxAttempts: 3,
			RetryIf:     &retryIfExpr,
		}
		executor := NewRetryExecutor(config)

		// HTTP 429 error should retry
		shouldRetry, err := executor.ShouldRetry(context.Background(), &HTTPError{StatusCode: 429, Message: "rate limited"}, 1)
		require.NoError(t, err)
		assert.True(t, shouldRetry)
	})

	t.Run("retryIf false prevents retry", func(t *testing.T) {
		retryIfExpr := celexp.Expression("__error.statusCode == 429 || __error.statusCode >= 500")
		config := &RetryConfig{
			MaxAttempts: 3,
			RetryIf:     &retryIfExpr,
		}
		executor := NewRetryExecutor(config)

		// HTTP 401 error should not retry
		shouldRetry, err := executor.ShouldRetry(context.Background(), &HTTPError{StatusCode: 401, Message: "unauthorized"}, 1)
		require.NoError(t, err)
		assert.False(t, shouldRetry)
	})

	t.Run("retryIf with exit code", func(t *testing.T) {
		retryIfExpr := celexp.Expression("__error.exitCode == 1")
		config := &RetryConfig{
			MaxAttempts: 3,
			RetryIf:     &retryIfExpr,
		}
		executor := NewRetryExecutor(config)

		// Generic error with exit code 0 should not retry
		shouldRetry, err := executor.ShouldRetry(context.Background(), errors.New("some error"), 1)
		require.NoError(t, err)
		assert.False(t, shouldRetry)
	})

	t.Run("retryIf with error type", func(t *testing.T) {
		retryIfExpr := celexp.Expression("__error.type == \"timeout\"")
		config := &RetryConfig{
			MaxAttempts: 3,
			RetryIf:     &retryIfExpr,
		}
		executor := NewRetryExecutor(config)

		// Timeout error should retry
		shouldRetry, err := executor.ShouldRetry(context.Background(), context.DeadlineExceeded, 1)
		require.NoError(t, err)
		assert.True(t, shouldRetry)

		// Other errors should not retry
		shouldRetry, err = executor.ShouldRetry(context.Background(), errors.New("some error"), 1)
		require.NoError(t, err)
		assert.False(t, shouldRetry)
	})

	t.Run("retryIf with attempt number", func(t *testing.T) {
		// Only retry first 2 attempts
		retryIfExpr := celexp.Expression("__error.attempt <= 2")
		config := &RetryConfig{
			MaxAttempts: 5,
			RetryIf:     &retryIfExpr,
		}
		executor := NewRetryExecutor(config)

		shouldRetry1, err := executor.ShouldRetry(context.Background(), errors.New("error"), 1)
		require.NoError(t, err)
		assert.True(t, shouldRetry1)

		shouldRetry2, err := executor.ShouldRetry(context.Background(), errors.New("error"), 2)
		require.NoError(t, err)
		assert.True(t, shouldRetry2)

		shouldRetry3, err := executor.ShouldRetry(context.Background(), errors.New("error"), 3)
		require.NoError(t, err)
		assert.False(t, shouldRetry3)
	})

	t.Run("retryIf invalid expression returns error", func(t *testing.T) {
		retryIfExpr := celexp.Expression("__error.nonexistentField")
		config := &RetryConfig{
			MaxAttempts: 3,
			RetryIf:     &retryIfExpr,
		}
		executor := NewRetryExecutor(config)

		_, err := executor.ShouldRetry(context.Background(), errors.New("error"), 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "retryIf")
	})

	t.Run("retryIf non-boolean result returns error", func(t *testing.T) {
		retryIfExpr := celexp.Expression("__error.message")
		config := &RetryConfig{
			MaxAttempts: 3,
			RetryIf:     &retryIfExpr,
		}
		executor := NewRetryExecutor(config)

		_, err := executor.ShouldRetry(context.Background(), errors.New("error"), 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "boolean")
	})

	t.Run("retryIf complex condition", func(t *testing.T) {
		// Retry rate limits up to 5 attempts, server errors up to 3 attempts
		retryIfExpr := celexp.Expression("(__error.statusCode == 429 && __error.attempt <= 5) || (__error.statusCode >= 500 && __error.attempt <= 3)")
		config := &RetryConfig{
			MaxAttempts: 10,
			RetryIf:     &retryIfExpr,
		}
		executor := NewRetryExecutor(config)

		// Rate limit on attempt 4 should retry
		shouldRetry, err := executor.ShouldRetry(context.Background(), &HTTPError{StatusCode: 429, Message: "rate limited"}, 4)
		require.NoError(t, err)
		assert.True(t, shouldRetry)

		// Rate limit on attempt 6 should not retry
		shouldRetry, err = executor.ShouldRetry(context.Background(), &HTTPError{StatusCode: 429, Message: "rate limited"}, 6)
		require.NoError(t, err)
		assert.False(t, shouldRetry)

		// Server error on attempt 2 should retry
		shouldRetry, err = executor.ShouldRetry(context.Background(), &HTTPError{StatusCode: 503, Message: "service unavailable"}, 2)
		require.NoError(t, err)
		assert.True(t, shouldRetry)

		// Server error on attempt 4 should not retry
		shouldRetry, err = executor.ShouldRetry(context.Background(), &HTTPError{StatusCode: 503, Message: "service unavailable"}, 4)
		require.NoError(t, err)
		assert.False(t, shouldRetry)
	})
}

func TestRetryExecutor_ExecuteWithRetry_WithRetryIf(t *testing.T) {
	t.Run("stops retrying when retryIf returns false", func(t *testing.T) {
		retryIfExpr := celexp.Expression("__error.statusCode >= 500")
		tenMillis := duration.New(10 * time.Millisecond)
		config := &RetryConfig{
			MaxAttempts:  5,
			RetryIf:      &retryIfExpr,
			InitialDelay: &tenMillis,
		}
		executor := NewRetryExecutor(config)

		calls := 0
		_, err := executor.ExecuteWithRetry(
			context.Background(),
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				calls++
				// Return 401 which should not retry
				return nil, &HTTPError{StatusCode: 401, Message: "unauthorized"}
			},
			nil,
		)

		require.Error(t, err)
		assert.Equal(t, 1, calls) // Should only try once, no retries
	})

	t.Run("retries when retryIf returns true then succeeds", func(t *testing.T) {
		retryIfExpr := celexp.Expression("__error.statusCode == 503")
		tenMillis := duration.New(10 * time.Millisecond)
		config := &RetryConfig{
			MaxAttempts:  5,
			RetryIf:      &retryIfExpr,
			InitialDelay: &tenMillis,
		}
		executor := NewRetryExecutor(config)

		calls := 0
		output, err := executor.ExecuteWithRetry(
			context.Background(),
			"test-action",
			func(_ context.Context) (*provider.Output, error) {
				calls++
				if calls < 3 {
					return nil, &HTTPError{StatusCode: 503, Message: "service unavailable"}
				}
				return &provider.Output{Data: "success"}, nil
			},
			nil,
		)

		require.NoError(t, err)
		assert.Equal(t, "success", output.Data)
		assert.Equal(t, 3, calls) // 2 failures + 1 success
	})
}
