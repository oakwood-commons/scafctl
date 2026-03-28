// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── BuildBundle DryRun tests ──────────────────────────────────────────────────

func TestBuildBundle_DryRun_NoFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
	}

	result, err := BuildBundle(context.Background(), sol, []byte("test"), tmpDir, BuildBundleOptions{
		BundleMaxSize: "50MB",
		DryRun:        true,
		NoVendor:      true,
		NoCache:       true,
		Logger:        logr.Discard(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, result.Discovery)
}

func TestBuildBundle_DryRun_WithFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a template file for discovery
	tmplDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.MkdirAll(tmplDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmplDir, "test.yaml"), []byte("hello: world"), 0o644))

	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Bundle: solution.Bundle{
			Include: []string{"templates/**"},
		},
	}

	result, err := BuildBundle(context.Background(), sol, []byte("test"), tmpDir, BuildBundleOptions{
		BundleMaxSize: "50MB",
		DryRun:        true,
		NoVendor:      true,
		NoCache:       true,
		Logger:        logr.Discard(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, result.Discovery)
}

func TestBuildBundle_InvalidMaxSize(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{}
	_, err := BuildBundle(context.Background(), sol, nil, t.TempDir(), BuildBundleOptions{
		BundleMaxSize: "invalid-size",
		Logger:        logr.Discard(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid bundle max size")
}

func TestBuildBundle_NoFilesReturnsNil(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
	}

	result, err := BuildBundle(context.Background(), sol, []byte("test"), tmpDir, BuildBundleOptions{
		BundleMaxSize: "50MB",
		NoVendor:      true,
		NoCache:       true,
		Logger:        logr.Discard(),
	})
	// When no files, BuildBundle returns nil, nil
	require.NoError(t, err)
	assert.Nil(t, result)
}

// ── BuildBundleOptions tests ──────────────────────────────────────────────────

func TestBuildBundleOptions_Defaults(t *testing.T) {
	t.Parallel()
	opts := BuildBundleOptions{}
	assert.False(t, opts.NoVendor)
	assert.False(t, opts.NoCache)
	assert.False(t, opts.DryRun)
	assert.False(t, opts.Dedupe)
	assert.Empty(t, opts.BundleMaxSize)
	assert.Empty(t, opts.DedupeThreshold)
}

// ── BuildResult tests ─────────────────────────────────────────────────────────

func TestBuildResult_ZeroValue(t *testing.T) {
	t.Parallel()
	r := &BuildResult{}
	assert.False(t, r.CacheHit)
	assert.Nil(t, r.TarData)
	assert.Nil(t, r.Dedup)
	assert.Nil(t, r.CacheEntry)
	assert.Empty(t, r.BuildFingerprint)
	assert.Empty(t, r.Messages)
	assert.Empty(t, r.ResolvedPlugins)
}

// ── ParseByteSize additional edge cases ───────────────────────────────────────

func TestParseByteSize_Whitespace(t *testing.T) {
	t.Parallel()
	result, err := ParseByteSize("  50MB  ")
	require.NoError(t, err)
	assert.Equal(t, int64(50*1024*1024), result)
}

func TestParseByteSize_CaseInsensitive(t *testing.T) {
	t.Parallel()
	result, err := ParseByteSize("10kb")
	require.NoError(t, err)
	assert.Equal(t, int64(10*1024), result)
}

func TestParseByteSize_Zero(t *testing.T) {
	t.Parallel()
	result, err := ParseByteSize("0")
	require.NoError(t, err)
	assert.Equal(t, int64(0), result)
}

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkParseByteSize(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = ParseByteSize("50MB")
	}
}

func BenchmarkBuildBundle_DryRun(b *testing.B) {
	tmpDir := b.TempDir()
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
	}
	opts := BuildBundleOptions{
		BundleMaxSize: "50MB",
		DryRun:        true,
		NoVendor:      true,
		NoCache:       true,
		Logger:        logr.Discard(),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = BuildBundle(context.Background(), sol, []byte("test"), tmpDir, opts)
	}
}
