package authdelegation

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkTokenCache_Get_Hit(b *testing.B) {
	b.ReportAllocs()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := NewTokenCache[string, TokenResult](ctx, 1024, 0, time.Hour)
	token := TokenResult{AccessToken: "bench-token", ExpiresIn: 3600}
	tc.Set(ctx, "bench-key", token, 10*time.Minute)

	b.ResetTimer()
	for range b.N {
		_, _ = tc.Get(ctx, "bench-key")
	}
}

func BenchmarkTokenCache_Get_Miss(b *testing.B) {
	b.ReportAllocs()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := NewTokenCache[string, TokenResult](ctx, 1024, 0, time.Hour)

	b.ResetTimer()
	for range b.N {
		_, _ = tc.Get(ctx, "nonexistent")
	}
}

func BenchmarkTokenCache_Set(b *testing.B) {
	b.ReportAllocs()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := NewTokenCache[string, TokenResult](ctx, 1024, 0, time.Hour)
	token := TokenResult{AccessToken: "bench-token", ExpiresIn: 3600}

	b.ResetTimer()
	for i := range b.N {
		tc.Set(ctx, fmt.Sprintf("key-%d", i), token, 10*time.Minute)
	}
}

func BenchmarkTokenCache_Get_Parallel(b *testing.B) {
	b.ReportAllocs()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := NewTokenCache[string, TokenResult](ctx, 1024, 0, time.Hour)
	token := TokenResult{AccessToken: "bench-token", ExpiresIn: 3600}

	// Pre-populate with entries to simulate warm cache
	for i := range 100 {
		tc.Set(ctx, fmt.Sprintf("key-%d", i), token, 10*time.Minute)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = tc.Get(ctx, fmt.Sprintf("key-%d", i%100))
			i++
		}
	})
}
