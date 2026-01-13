package sleepprovider

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSleepProvider(t *testing.T) {
	p := NewSleepProvider()
	require.NotNil(t, p)
	assert.Equal(t, "sleep", p.Descriptor().Name)
	assert.Equal(t, "Sleep Provider", p.Descriptor().DisplayName)
}

func TestSleepProvider_Execute_Success(t *testing.T) {
	p := NewSleepProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))

	inputs := map[string]any{
		"duration": "100ms",
	}

	start := time.Now()
	output, err := p.Execute(ctx, inputs)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.True(t, data["success"].(bool))
	assert.Equal(t, "100ms", data["duration"])

	// Verify actual sleep occurred (should be at least 100ms)
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
}

func TestSleepProvider_Execute_InvalidDuration(t *testing.T) {
	p := NewSleepProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))

	inputs := map[string]any{
		"duration": "invalid",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	require.Nil(t, output)
	assert.Contains(t, err.Error(), "invalid duration format")
}

func TestSleepProvider_Execute_NegativeDuration(t *testing.T) {
	p := NewSleepProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))

	inputs := map[string]any{
		"duration": "-5s",
	}

	output, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	require.Nil(t, output)
	assert.Contains(t, err.Error(), "duration cannot be negative")
}

func TestSleepProvider_Execute_ContextCancellation(t *testing.T) {
	p := NewSleepProvider()
	ctx, cancel := context.WithCancel(logger.WithLogger(context.Background(), logger.Get(0)))

	inputs := map[string]any{
		"duration": "5s",
	}

	// Cancel after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	output, err := p.Execute(ctx, inputs)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Nil(t, output)
	assert.Contains(t, err.Error(), "sleep interrupted")

	// Verify sleep was interrupted (should be less than 5s)
	assert.Less(t, elapsed, 5*time.Second)
}

func TestSleepProvider_Execute_DryRun(t *testing.T) {
	p := NewSleepProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = provider.WithDryRun(ctx, true)

	inputs := map[string]any{
		"duration": "5s",
	}

	start := time.Now()
	output, err := p.Execute(ctx, inputs)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.True(t, data["_dryRun"].(bool))
	assert.Equal(t, "5s", data["duration"])

	// Verify no actual sleep occurred (should be instant)
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func TestSleepProvider_Execute_VariousDurations(t *testing.T) {
	tests := []struct {
		name     string
		duration string
		minTime  time.Duration
	}{
		{"Milliseconds", "50ms", 50 * time.Millisecond},
		{"Seconds", "1s", 1 * time.Second},
		{"Complex", "500ms", 500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewSleepProvider()
			ctx := logger.WithLogger(context.Background(), logger.Get(0))

			inputs := map[string]any{
				"duration": tt.duration,
			}

			start := time.Now()
			output, err := p.Execute(ctx, inputs)
			elapsed := time.Since(start)

			require.NoError(t, err)
			require.NotNil(t, output)

			data, ok := output.Data.(map[string]any)
			require.True(t, ok)
			assert.True(t, data["success"].(bool))
			assert.GreaterOrEqual(t, elapsed, tt.minTime)
		})
	}
}
