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

func TestBuildBundle_NoFilesReturnsResult(t *testing.T) {
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
	// When no files, BuildBundle returns a non-nil result (carrying LockData
	// and Discovery) with no tar/dedup payload.
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.TarData)
	assert.Nil(t, result.Dedup)
	assert.Equal(t, 0, result.InputFileCount)
	assert.NotNil(t, result.Discovery)
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
	r := &BuildResult{}
	assert.False(t, r.CacheHit)
	assert.Nil(t, r.TarData)
	assert.Nil(t, r.Dedup)
	assert.Nil(t, r.CacheEntry)
	assert.Empty(t, r.BuildFingerprint)
	assert.Empty(t, r.Messages)
	assert.Empty(t, r.ResolvedPlugins)
}

// ── buildSourcedCatalogs tests ────────────────────────────────────────────────

func TestBuildSourcedCatalogs_NilConfig(t *testing.T) {
	t.Parallel()
	got := buildSourcedCatalogs(context.Background(), logr.Discard())
	assert.Nil(t, got)
}

func TestBuildSourcedCatalogs_NoRemoteCatalogs(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "local", Type: config.CatalogTypeFilesystem, Path: "./local"},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)
	got := buildSourcedCatalogs(ctx, logr.Discard())
	assert.Nil(t, got, "filesystem catalogs have no registry origin and must be skipped")
}

func TestBuildSourcedCatalogs_BuildsKeyedByLowercasedAlias(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "MyOrg", Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/myorg"},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)
	got := buildSourcedCatalogs(ctx, logr.Discard())
	require.Len(t, got, 1)
	cat, ok := got["myorg"]
	require.True(t, ok, "catalog must be keyed by the lowercased alias")
	assert.NotNil(t, cat)
}

func TestBuildSourcedCatalogs_FirstAliasWins(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "dup", Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/first"},
			{Name: "DUP", Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/second"},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)
	got := buildSourcedCatalogs(ctx, logr.Discard())
	require.Len(t, got, 1, "duplicate lowercased aliases collapse to one entry")
	_, ok := got["dup"]
	assert.True(t, ok)
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

// ── validateProviderCoverage tests ───────────────────────────────────────────

func TestValidateProviderCoverage_AllDeclared(t *testing.T) {
	t.Parallel()

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

	err := validateProviderCoverage(sol, nil, nil)
	assert.NoError(t, err)
}

func TestValidateProviderCoverage_BuiltinExempt(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myhttp": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "http"}},
					},
				},
			},
		},
	}

	err := validateProviderCoverage(sol, []string{"http", "file", "cel"}, nil)
	assert.NoError(t, err)
}

func TestValidateProviderCoverage_MissingProviderErrors(t *testing.T) {
	t.Parallel()

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

	err := validateProviderCoverage(sol, []string{"http"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "my-custom-provider")
	assert.Contains(t, err.Error(), "bundle.plugins")
}

func TestValidateProviderCoverage_OfficialProviderSuggestion(t *testing.T) {
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

	err := validateProviderCoverage(sol, []string{"http"}, officialReg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env")
	assert.Contains(t, err.Error(), "latest")
	assert.Contains(t, err.Error(), "bundle:\n  plugins:")
}

func TestValidateProviderCoverage_MixedOfficialAndCustom(t *testing.T) {
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
				"custom": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "my-custom"}},
					},
				},
			},
		},
	}

	err := validateProviderCoverage(sol, []string{"http"}, officialReg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env")
	assert.Contains(t, err.Error(), "my-custom")
}

func TestValidateProviderCoverage_NoReferences(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{},
		},
	}

	err := validateProviderCoverage(sol, nil, nil)
	assert.NoError(t, err)
}

func TestValidateProviderCoverage_TransformPhase(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"transformed": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "http"}},
					},
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{{Provider: "exec"}},
					},
				},
			},
		},
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "exec", Kind: solution.PluginKindProvider},
			},
		},
	}

	err := validateProviderCoverage(sol, []string{"http"}, nil)
	assert.NoError(t, err)
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
