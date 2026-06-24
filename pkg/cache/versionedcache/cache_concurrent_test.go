// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0
package versionedcache

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/cache/diskcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestCache_ConcurrentSetGetDelete(t *testing.T) {
	// Large enough for ~30 entries of 10 bytes each to allow eviction churn.
	vc := newTestCache(t, 512, diskcache.WithFileMode(0o755))

	const (
		numWriters  = 10
		numReaders  = 10
		numPinners  = 5
		numDeleters = 5
		opsPerGoro  = 20
		platform    = "linux/amd64"
		pluginName  = "race-plugin"
	)

	data := []byte("0123456789") // 10 bytes per entry

	var g errgroup.Group

	// Writers: each writes a unique set of versions.
	for w := range numWriters {
		g.Go(func() error {
			for i := range opsPerGoro {
				ver := fmt.Sprintf("%d.%d.0", w, i)
				if err := vc.Set(pluginName, ver, platform, data); err != nil {
					return fmt.Errorf("writer %d set %s: %w", w, ver, err)
				}
			}
			return nil
		})
	}

	// Readers: call Latest and Versions in a tight loop.
	for r := range numReaders {
		g.Go(func() error {
			for range opsPerGoro {
				_ = vc.Versions(pluginName, platform)
				_, _ = vc.Latest(pluginName, platform)
			}
			_ = r // suppress unused
			return nil
		})
	}

	// Pinners: pin whatever Latest returns, hold briefly, release.
	for p := range numPinners {
		g.Go(func() error {
			for range opsPerGoro {
				ver, ok := vc.Latest(pluginName, platform)
				if !ok {
					continue
				}
				_, release, pinOk := vc.Pin(pluginName, ver, platform)
				if pinOk {
					// Simulate brief use of the binary.
					_ = vc.Versions(pluginName, platform)
					release()
				}
			}
			_ = p
			return nil
		})
	}

	// Deleters: delete versions they discover via Versions.
	for d := range numDeleters {
		g.Go(func() error {
			for range opsPerGoro {
				versions := vc.Versions(pluginName, platform)
				if len(versions) == 0 {
					continue
				}
				// Delete the oldest (last in the descending list).
				target := versions[len(versions)-1]
				vc.Delete(pluginName, target, platform)
			}
			_ = d
			return nil
		})
	}

	require.NoError(t, g.Wait())

	// Post-condition: Versions must be valid descending semver (no duplicates, sorted).
	versions := vc.Versions(pluginName, platform)
	for i := 1; i < len(versions); i++ {
		prev, _ := semver.NewVersion(versions[i-1])
		curr, _ := semver.NewVersion(versions[i])
		assert.True(t, prev.GreaterThan(curr),
			"versions not sorted descending: %s should be > %s", versions[i-1], versions[i])
	}

	// Latest must match head of Versions (if any remain).
	if len(versions) > 0 {
		latest, ok := vc.Latest(pluginName, platform)
		assert.True(t, ok)
		assert.Equal(t, versions[0], latest)
	}
}

func TestCache_ConcurrentSetPin(t *testing.T) {
	// Multiple goroutines SetPin the same version simultaneously.
	// Only one should write; all should get a valid path + release.
	vc := newTestCache(t, 4096, diskcache.WithFileMode(0o755))

	const (
		concurrency = 20
		platform    = "darwin/arm64"
		pluginName  = "setpin-race"
		version     = "1.0.0"
	)

	data := []byte("concurrent-binary")
	var successCount atomic.Int32

	var g errgroup.Group
	for range concurrency {
		g.Go(func() error {
			path, release, err := vc.SetPin(pluginName, version, platform, data)
			if err != nil {
				return err
			}
			defer release()
			if path != "" {
				successCount.Add(1)
			}
			return nil
		})
	}

	require.NoError(t, g.Wait())
	assert.Equal(t, int32(concurrency), successCount.Load(),
		"all SetPin calls should succeed with a valid path")

	// Exactly one version indexed.
	versions := vc.Versions(pluginName, platform)
	assert.Equal(t, []string{version}, versions)
}

func BenchmarkCache_MixedOps(b *testing.B) {
	b.ReportAllocs()

	dir := b.TempDir()
	vc, err := New(dir, 64*1024, diskcache.WithFileMode(0o755))
	if err != nil {
		b.Fatal(err)
	}

	const platform = "linux/amd64"
	data := []byte("bench-binary-payload-16b")

	// Pre-seed some versions.
	for i := range 10 {
		ver := fmt.Sprintf("0.%d.0", i)
		if err := vc.Set("bench-plugin", ver, platform, data); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ver := fmt.Sprintf("1.%d.%d", i/100, i%100)
			name := "bench-plugin"

			// Set
			_ = vc.Set(name, ver, platform, data)

			// Latest
			_, _ = vc.Latest(name, platform)

			// Pin + release
			if _, release, ok := vc.Pin(name, ver, platform); ok {
				release()
			}

			// Delete (every 3rd iteration)
			if i%3 == 0 {
				vc.Delete(name, ver, platform)
			}

			i++
		}
	})
}
