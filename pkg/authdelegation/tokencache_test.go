package authdelegation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTokenCache_GetSet_RoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := NewTokenCache[string, TokenResult](ctx, 10, 0, time.Hour)

	token := TokenResult{
		AccessToken: "abc123",
		ExpiresIn:   3600,
	}

	tc.Set(ctx, "key1", token, 5*time.Minute)

	got, ok := tc.Get(ctx, "key1")
	assert.True(t, ok)
	assert.Equal(t, token, got)
}

func TestTokenCache_Get_Miss(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := NewTokenCache[string, TokenResult](ctx, 10, 0, time.Hour)

	got, ok := tc.Get(ctx, "nonexistent")
	assert.False(t, ok)
	assert.Equal(t, TokenResult{}, got)
}

func TestTokenCache_TTLExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := NewTokenCache[string, TokenResult](ctx, 10, 0, 10*time.Millisecond)

	token := TokenResult{
		AccessToken: "expires-soon",
		ExpiresIn:   1,
	}

	tc.Set(ctx, "ephemeral", token, 50*time.Millisecond)

	got, ok := tc.Get(ctx, "ephemeral")
	assert.True(t, ok)
	assert.Equal(t, token, got)

	// Wait for TTL + cleanup interval to pass
	time.Sleep(100 * time.Millisecond)

	got, ok = tc.Get(ctx, "ephemeral")
	assert.False(t, ok)
	assert.Equal(t, TokenResult{}, got)
}

func TestNewTokenCache_CleanupRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	tc := NewTokenCache[string, TokenResult](ctx, 10, 0, 10*time.Millisecond)

	token := TokenResult{AccessToken: "test", ExpiresIn: 1}
	tc.Set(ctx, "k", token, time.Hour)

	// Cancel context — cleanup goroutine should exit without panic or leak
	cancel()

	// Give goroutine time to exit
	time.Sleep(50 * time.Millisecond)

	// Cache still returns the value (cancellation stops cleanup, not reads)
	got, ok := tc.Get(context.Background(), "k")
	assert.True(t, ok)
	assert.Equal(t, token, got)
}
