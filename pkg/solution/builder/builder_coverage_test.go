// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
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
	if _, err := ParseByteSize("50MB"); err != nil {
		b.Fatal(err)
	}
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
	if _, err := BuildBundle(context.Background(), sol, []byte("test"), tmpDir, opts); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = BuildBundle(context.Background(), sol, []byte("test"), tmpDir, opts)
	}
}

// ── autoInjectOfficialPlugins tests ──────────────────────────────────────────

func TestAutoInjectOfficialPlugins_InjectsMissingProviders(t *testing.T) {
	t.Parallel()

	officialReg := official.NewRegistry()
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
				"mydir": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "directory"}},
					},
				},
			},
		},
	}

	injected := autoInjectOfficialPlugins(sol, officialReg, false, logr.Discard())

	assert.Len(t, injected, 2)
	assert.Contains(t, injected, "env")
	assert.Contains(t, injected, "directory")
	assert.Len(t, sol.Bundle.Plugins, 2)
}

func TestAutoInjectOfficialPlugins_SkipsAlreadyDeclared(t *testing.T) {
	t.Parallel()

	officialReg := official.NewRegistry()
	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "env", Kind: solution.PluginKindProvider, Version: ">=1.0.0"},
			},
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	injected := autoInjectOfficialPlugins(sol, officialReg, false, logr.Discard())

	assert.Empty(t, injected)
	assert.Len(t, sol.Bundle.Plugins, 1, "existing declaration should be untouched")
}

func TestAutoInjectOfficialPlugins_SkipsNonOfficialProviders(t *testing.T) {
	t.Parallel()

	officialReg := official.NewRegistry()
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"custom": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "my-custom-provider"}},
					},
				},
			},
		},
	}

	injected := autoInjectOfficialPlugins(sol, officialReg, false, logr.Discard())

	assert.Empty(t, injected)
	assert.Empty(t, sol.Bundle.Plugins)
}

func TestAutoInjectOfficialPlugins_StrictMode(t *testing.T) {
	t.Parallel()

	officialReg := official.NewRegistry()
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	injected := autoInjectOfficialPlugins(sol, officialReg, true, logr.Discard())

	assert.Len(t, injected, 1)
	assert.Contains(t, injected, "env")
	assert.Empty(t, sol.Bundle.Plugins, "strict mode should not mutate solution")
}

func TestAutoInjectOfficialPlugins_TransformPhase(t *testing.T) {
	t.Parallel()

	officialReg := official.NewRegistry()
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"transformed": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{{Provider: "exec"}},
					},
				},
			},
		},
	}

	injected := autoInjectOfficialPlugins(sol, officialReg, false, logr.Discard())

	assert.Len(t, injected, 2)
	assert.Contains(t, injected, "env")
	assert.Contains(t, injected, "exec")
}

func TestAutoInjectOfficialPlugins_DeduplicatesProviders(t *testing.T) {
	t.Parallel()

	officialReg := official.NewRegistry()
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"first": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
				"second": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	injected := autoInjectOfficialPlugins(sol, officialReg, false, logr.Discard())

	assert.Len(t, injected, 1, "same provider used twice should only be injected once")
	assert.Len(t, sol.Bundle.Plugins, 1)
}
