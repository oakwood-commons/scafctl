// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
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

func TestBuildLockSignatureVerifier_NilPolicy(t *testing.T) {
	ctx := context.Background()
	fn := buildLockSignatureVerifier(ctx, logr.Discard())
	assert.Nil(t, fn, "should return nil when no policy in context or config")
}

func TestBuildLockSignatureVerifier_ConfigFallback(t *testing.T) {
	// No policy in context, but config has enforce mode — should derive from config.
	appCfg := &config.Config{}
	appCfg.Plugins.Signatures.Mode = "enforce"
	appCfg.Plugins.Signatures.TrustedIssuers = []string{"https://issuer.example.com"}
	appCfg.Plugins.Signatures.TrustedIdentities = []string{"https://github.com/org/*"}
	ctx := config.WithConfig(context.Background(), appCfg)

	fn := buildLockSignatureVerifier(ctx, logr.Discard())
	require.NotNil(t, fn, "should derive policy from config when context has no policy")
}

func TestBuildLockSignatureVerifier_ConfigFallback_Off(t *testing.T) {
	// Config has mode=off — should return nil.
	appCfg := &config.Config{}
	appCfg.Plugins.Signatures.Mode = "off"
	ctx := config.WithConfig(context.Background(), appCfg)

	fn := buildLockSignatureVerifier(ctx, logr.Discard())
	assert.Nil(t, fn, "should return nil when config mode is off")
}

func TestBuildLockSignatureVerifier_ContextOverridesConfig(t *testing.T) {
	// Context policy is off, config is enforce — context wins.
	appCfg := &config.Config{}
	appCfg.Plugins.Signatures.Mode = "enforce"
	appCfg.Plugins.Signatures.TrustedIssuers = []string{"https://issuer.example.com"}
	appCfg.Plugins.Signatures.TrustedIdentities = []string{"https://github.com/org/*"}
	ctx := config.WithConfig(context.Background(), appCfg)
	ctx = plugin.WithSignaturePolicy(ctx, &plugin.SignaturePolicy{Mode: plugin.SignatureModeOff})

	fn := buildLockSignatureVerifier(ctx, logr.Discard())
	assert.Nil(t, fn, "context policy should take precedence over config")
}

func TestBuildLockSignatureVerifier_OffPolicy(t *testing.T) {
	ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
		Mode: plugin.SignatureModeOff,
	})
	fn := buildLockSignatureVerifier(ctx, logr.Discard())
	assert.Nil(t, fn, "should return nil when policy mode is off")
}

func TestBuildLockSignatureVerifier_EnforceReturnsError(t *testing.T) {
	ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
		Mode:              plugin.SignatureModeEnforce,
		TrustedIssuers:    []string{"https://issuer.example.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	})
	fn := buildLockSignatureVerifier(ctx, logr.Discard())
	require.NotNil(t, fn, "should return a callback when policy is active")

	// The stub verifier returns ErrCosignNotAvailable, which should be
	// propagated as an error in enforce mode.
	result, err := fn(ctx, "ghcr.io/org/plugin@sha256:abc123")
	assert.Nil(t, result)
	require.Error(t, err)
}

func TestBuildLockSignatureVerifier_WarnReturnsNil(t *testing.T) {
	ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
		Mode:              plugin.SignatureModeWarn,
		TrustedIssuers:    []string{"https://issuer.example.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	})
	fn := buildLockSignatureVerifier(ctx, logr.Discard())
	require.NotNil(t, fn, "should return a callback when policy is active")

	// The stub verifier returns ErrCosignNotAvailable; in warn mode,
	// the callback logs and returns nil.
	result, err := fn(ctx, "ghcr.io/org/plugin@sha256:abc123")
	assert.Nil(t, result)
	assert.NoError(t, err)
}

func TestSignaturePolicyFromConfig_NilConfig(t *testing.T) {
	policy := signaturePolicyFromAppConfig(nil, logr.Discard())
	assert.Nil(t, policy)
}

func TestSignaturePolicyFromConfig_OffMode(t *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.Signatures.Mode = "off"
	policy := signaturePolicyFromAppConfig(cfg, logr.Discard())
	assert.Nil(t, policy)
}

func TestSignaturePolicyFromConfig_EmptyMode(t *testing.T) {
	cfg := &config.Config{}
	policy := signaturePolicyFromAppConfig(cfg, logr.Discard())
	assert.Nil(t, policy)
}

func TestSignaturePolicyFromConfig_InvalidMode(t *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.Signatures.Mode = "bogus"
	policy := signaturePolicyFromAppConfig(cfg, logr.Discard())
	assert.Nil(t, policy, "invalid mode should return nil")
}

func TestSignaturePolicyFromConfig_EnforceMode(t *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.Signatures.Mode = "enforce"
	cfg.Plugins.Signatures.TrustedIssuers = []string{"https://issuer.example.com"}
	cfg.Plugins.Signatures.TrustedIdentities = []string{"https://github.com/org/*"}
	policy := signaturePolicyFromAppConfig(cfg, logr.Discard())
	require.NotNil(t, policy)
	assert.Equal(t, plugin.SignatureModeEnforce, policy.Mode)
	assert.Equal(t, []string{"https://issuer.example.com"}, policy.TrustedIssuers)
	assert.Equal(t, []string{"https://github.com/org/*"}, policy.TrustedIdentities)
}

func TestSignaturePolicyFromConfig_WarnMode(t *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.Signatures.Mode = "warn"
	policy := signaturePolicyFromAppConfig(cfg, logr.Discard())
	require.NotNil(t, policy)
	assert.Equal(t, plugin.SignatureModeWarn, policy.Mode)
}
