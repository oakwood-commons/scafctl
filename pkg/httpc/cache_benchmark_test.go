package httpc

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"ivan.dev/httpcache"
)

// BenchmarkMemoryCacheSet benchmarks memory cache Set operations
func BenchmarkMemoryCacheSet(b *testing.B) {
	ctx := context.Background()
	cache := newMetricsMemoryCache(httpcache.MemoryCache(1000, 10*time.Minute))
	data := []byte("test data for benchmarking")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "benchmark-key"
		_ = cache.Set(ctx, key, data, 10*time.Minute)
	}
}

// BenchmarkMemoryCacheGet benchmarks memory cache Get operations (hits)
func BenchmarkMemoryCacheGet(b *testing.B) {
	ctx := context.Background()
	cache := newMetricsMemoryCache(httpcache.MemoryCache(1000, 10*time.Minute))
	data := []byte("test data for benchmarking")
	key := "benchmark-key"

	// Pre-populate cache
	_ = cache.Set(ctx, key, data, 10*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, key)
	}
}

// BenchmarkMemoryCacheGetMiss benchmarks memory cache Get operations (misses)
func BenchmarkMemoryCacheGetMiss(b *testing.B) {
	ctx := context.Background()
	cache := newMetricsMemoryCache(httpcache.MemoryCache(1000, 10*time.Minute))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, "nonexistent-key")
	}
}

// BenchmarkFileCacheSet benchmarks file cache Set operations
func BenchmarkFileCacheSet(b *testing.B) {
	ctx := context.Background()
	tmpDir := b.TempDir()

	cache, err := NewFileCache(&FileCacheConfig{
		Dir:       tmpDir,
		TTL:       10 * time.Minute,
		KeyPrefix: "bench:",
		MaxSize:   10 * 1024 * 1024,
		Logger:    logr.Discard(),
	})
	if err != nil {
		b.Fatalf("Failed to create cache: %v", err)
	}

	data := []byte("test data for benchmarking")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "benchmark-key"
		_ = cache.Set(ctx, key, data, 10*time.Minute)
	}
}

// BenchmarkFileCacheGet benchmarks file cache Get operations (hits)
func BenchmarkFileCacheGet(b *testing.B) {
	ctx := context.Background()
	tmpDir := b.TempDir()

	cache, err := NewFileCache(&FileCacheConfig{
		Dir:       tmpDir,
		TTL:       10 * time.Minute,
		KeyPrefix: "bench:",
		MaxSize:   10 * 1024 * 1024,
		Logger:    logr.Discard(),
	})
	if err != nil {
		b.Fatalf("Failed to create cache: %v", err)
	}

	data := []byte("test data for benchmarking")
	key := "benchmark-key"

	// Pre-populate cache
	_ = cache.Set(ctx, key, data, 10*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, key)
	}
}

// BenchmarkFileCacheGetMiss benchmarks file cache Get operations (misses)
func BenchmarkFileCacheGetMiss(b *testing.B) {
	ctx := context.Background()
	tmpDir := b.TempDir()

	cache, err := NewFileCache(&FileCacheConfig{
		Dir:       tmpDir,
		TTL:       10 * time.Minute,
		KeyPrefix: "bench:",
		MaxSize:   10 * 1024 * 1024,
		Logger:    logr.Discard(),
	})
	if err != nil {
		b.Fatalf("Failed to create cache: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, "nonexistent-key")
	}
}

// BenchmarkFileCacheDel benchmarks file cache Del operations
func BenchmarkFileCacheDel(b *testing.B) {
	ctx := context.Background()
	tmpDir := b.TempDir()

	cache, err := NewFileCache(&FileCacheConfig{
		Dir:       tmpDir,
		TTL:       10 * time.Minute,
		KeyPrefix: "bench:",
		MaxSize:   10 * 1024 * 1024,
		Logger:    logr.Discard(),
	})
	if err != nil {
		b.Fatalf("Failed to create cache: %v", err)
	}

	data := []byte("test data for benchmarking")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		key := "benchmark-key"
		_ = cache.Set(ctx, key, data, 10*time.Minute)
		b.StartTimer()

		_ = cache.Del(ctx, key)
	}
}

// BenchmarkFileCacheSetDifferentSizes benchmarks file cache Set with different data sizes
func BenchmarkFileCacheSetDifferentSizes(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			ctx := context.Background()
			tmpDir := b.TempDir()

			cache, err := NewFileCache(&FileCacheConfig{
				Dir:       tmpDir,
				TTL:       10 * time.Minute,
				KeyPrefix: "bench:",
				MaxSize:   10 * 1024 * 1024,
				Logger:    logr.Discard(),
			})
			if err != nil {
				b.Fatalf("Failed to create cache: %v", err)
			}

			data := make([]byte, size.size)
			for i := range data {
				data[i] = byte(i % 256)
			}

			b.ResetTimer()
			b.SetBytes(int64(size.size))
			for i := 0; i < b.N; i++ {
				key := "benchmark-key"
				_ = cache.Set(ctx, key, data, 10*time.Minute)
			}
		})
	}
}

// BenchmarkMemoryCacheParallel benchmarks memory cache operations under concurrent load
func BenchmarkMemoryCacheParallel(b *testing.B) {
	ctx := context.Background()
	cache := newMetricsMemoryCache(httpcache.MemoryCache(1000, 10*time.Minute))
	data := []byte("test data for benchmarking")

	// Pre-populate with some data
	for i := 0; i < 100; i++ {
		_ = cache.Set(ctx, filepath.Join("key", string(rune(i))), data, 10*time.Minute)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := filepath.Join("key", string(rune(i%100)))
			if i%2 == 0 {
				_, _ = cache.Get(ctx, key)
			} else {
				_ = cache.Set(ctx, key, data, 10*time.Minute)
			}
			i++
		}
	})
}

// BenchmarkFileCacheParallel benchmarks file cache operations under concurrent load
func BenchmarkFileCacheParallel(b *testing.B) {
	ctx := context.Background()
	tmpDir := b.TempDir()

	cache, err := NewFileCache(&FileCacheConfig{
		Dir:       tmpDir,
		TTL:       10 * time.Minute,
		KeyPrefix: "bench:",
		MaxSize:   10 * 1024 * 1024,
		Logger:    logr.Discard(),
	})
	if err != nil {
		b.Fatalf("Failed to create cache: %v", err)
	}

	data := []byte("test data for benchmarking")

	// Pre-populate with some data
	for i := 0; i < 100; i++ {
		_ = cache.Set(ctx, filepath.Join("key", string(rune(i))), data, 10*time.Minute)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := filepath.Join("key", string(rune(i%100)))
			if i%2 == 0 {
				_, _ = cache.Get(ctx, key)
			} else {
				_ = cache.Set(ctx, key, data, 10*time.Minute)
			}
			i++
		}
	})
}
