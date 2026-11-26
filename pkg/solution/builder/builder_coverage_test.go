// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── BuildResult tests ─────────────────────────────────────────────────────────

func TestBuildResult_ZeroValue(t *testing.T) {
	r := &BuildResult{}
	assert.False(t, r.CacheHit)
	assert.Nil(t, r.TarData)
	assert.Nil(t, r.Dedup)
	assert.Nil(t, r.CacheEntry)
	assert.Empty(t, r.BuildFingerprint)
	assert.Nil(t, r.LockData)
	assert.Nil(t, r.Discovery)
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
	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	require.NoError(t, err)
	assert.Nil(t, fn, "should return nil when no policy in context or config")
}

func TestBuildLockSignatureVerifier_ConfigFallback(t *testing.T) {
	// No policy in context, but config has enforce mode — should derive from config.
	appCfg := &config.Config{}
	appCfg.Plugins.Signatures.Mode = "enforce"
	appCfg.Plugins.Signatures.TrustedIssuers = []string{"https://issuer.example.com"}
	appCfg.Plugins.Signatures.TrustedIdentities = []string{"https://github.com/org/*"}
	ctx := config.WithConfig(context.Background(), appCfg)

	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, fn, "should derive policy from config when context has no policy")
}

func TestBuildLockSignatureVerifier_InvalidPolicyErrors(t *testing.T) {
	t.Parallel()
	appCfg := &config.Config{}
	appCfg.Plugins.Signatures.Mode = "enforce"
	// TrustedIssuers and TrustedIdentities intentionally omitted.
	ctx := config.WithConfig(context.Background(), appCfg)

	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	assert.Nil(t, fn)
	require.ErrorContains(t, err, "plugin signature policy")
}

func TestBuildLockSignatureVerifier_ConfigFallback_Off(t *testing.T) {
	// Config has mode=off — should return nil.
	appCfg := &config.Config{}
	appCfg.Plugins.Signatures.Mode = "off"
	ctx := config.WithConfig(context.Background(), appCfg)

	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	require.NoError(t, err)
	assert.Nil(t, fn, "should return nil when config mode is off")
}

func TestBuildLockSignatureVerifier_ConfigLoadErrorFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := config.WithConfig(context.Background(), &config.Config{})
	ctx = config.WithLoadError(ctx, fmt.Errorf("invalid plugins.signatures: mode \"enforce\" requires at least one trustedIssuers"))

	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	assert.Nil(t, fn)
	require.ErrorContains(t, err, "plugin signature policy: configuration failed to load")
}

func TestBuildLockSignatureVerifier_ConfigFallback_DisabledRejectsMalformedPattern(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"", "off"} {
		appCfg := &config.Config{}
		appCfg.Plugins.Signatures.Mode = mode
		appCfg.Plugins.Signatures.TrustedIdentities = []string{"https://github.com/my-org/{repo}/*"}
		ctx := config.WithConfig(context.Background(), appCfg)

		fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
		assert.Nil(t, fn, "mode=%q", mode)
		require.ErrorIs(t, err, plugin.ErrInvalidIdentityPattern, "mode=%q", mode)
	}
}

func TestBuildLockSignatureVerifier_ContextOverridesConfig(t *testing.T) {
	// Context policy is off, config is enforce — context wins.
	appCfg := &config.Config{}
	appCfg.Plugins.Signatures.Mode = "enforce"
	appCfg.Plugins.Signatures.TrustedIssuers = []string{"https://issuer.example.com"}
	appCfg.Plugins.Signatures.TrustedIdentities = []string{"https://github.com/org/*"}
	ctx := config.WithConfig(context.Background(), appCfg)
	ctx = plugin.WithSignaturePolicy(ctx, &plugin.SignaturePolicy{Mode: plugin.SignatureModeOff})

	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	require.NoError(t, err)
	assert.Nil(t, fn, "context policy should take precedence over config")
}

func TestBuildLockSignatureVerifier_OffPolicy(t *testing.T) {
	ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
		Mode: plugin.SignatureModeOff,
	})
	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	require.NoError(t, err)
	assert.Nil(t, fn, "should return nil when policy mode is off")
}

func TestBuildLockSignatureVerifier_EnforceReturnsError(t *testing.T) {
	ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
		Mode:              plugin.SignatureModeEnforce,
		TrustedIssuers:    []string{"https://issuer.example.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	})
	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, fn, "should return a callback when policy is active")

	// The stub verifier returns ErrCosignNotAvailable, which should be
	// propagated as an error in enforce mode.
	result, err := fn(ctx, catalog.ArtifactKindProvider, "exec", "ghcr.io/org/plugin@sha256:abc123")
	assert.Nil(t, result)
	require.Error(t, err)
}

func TestBuildLockSignatureVerifier_WarnReturnsNil(t *testing.T) {
	ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
		Mode:              plugin.SignatureModeWarn,
		TrustedIssuers:    []string{"https://issuer.example.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	})
	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, fn, "should return a callback when policy is active")

	// The stub verifier returns ErrCosignNotAvailable; in warn mode,
	// the callback logs and returns nil.
	result, err := fn(ctx, catalog.ArtifactKindProvider, "exec", "ghcr.io/org/plugin@sha256:abc123")
	assert.Nil(t, result)
	assert.NoError(t, err)
}

func TestBuildLockSignatureVerifier_EmptyImageRef_EnforceFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
		Mode:              plugin.SignatureModeEnforce,
		TrustedIssuers:    []string{"https://issuer.example.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	})
	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, fn)

	result, err := fn(ctx, catalog.ArtifactKindProvider, "exec", "")
	assert.Nil(t, result)
	require.ErrorIs(t, err, plugin.ErrSignatureUnverifiable)
}

func TestBuildLockSignatureVerifier_EmptyImageRef_WarnContinues(t *testing.T) {
	t.Parallel()
	ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
		Mode:              plugin.SignatureModeWarn,
		TrustedIssuers:    []string{"https://issuer.example.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	})
	fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, fn)

	result, err := fn(ctx, catalog.ArtifactKindProvider, "exec", "")
	assert.Nil(t, result)
	assert.NoError(t, err)
}

func TestBuildLockSignatureVerifier_PlaceholdersAndInvalidArtifactIdentity(t *testing.T) {
	t.Parallel()

	t.Run("invalid identity template rejected up front", func(t *testing.T) {
		t.Parallel()
		ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
			Mode:              plugin.SignatureModeEnforce,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/my-org/{repo}/*"},
		})
		fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
		assert.Nil(t, fn)
		require.ErrorIs(t, err, plugin.ErrInvalidIdentityPattern)
	})

	t.Run("invalid artifact name fails closed in enforce mode", func(t *testing.T) {
		t.Parallel()
		ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
			Mode:              plugin.SignatureModeEnforce,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/my-org/{kind}-{name}/.github/workflows/*"},
		})
		fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
		require.NoError(t, err)
		require.NotNil(t, fn)

		res, err := fn(ctx, catalog.ArtifactKindProvider, "../spoof", "ghcr.io/my-org/providers/spoof@sha256:abc")
		assert.Nil(t, res)
		require.ErrorIs(t, err, plugin.ErrInvalidArtifactIdentity)
	})

	t.Run("invalid artifact name logs and continues in warn mode", func(t *testing.T) {
		t.Parallel()
		ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
			Mode:              plugin.SignatureModeWarn,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/my-org/{kind}-{name}/.github/workflows/*"},
		})
		fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
		require.NoError(t, err)
		require.NotNil(t, fn)

		res, err := fn(ctx, catalog.ArtifactKindProvider, "../spoof", "ghcr.io/my-org/providers/spoof@sha256:abc")
		assert.Nil(t, res)
		require.NoError(t, err)
	})

	t.Run("off mode context policy still rejects malformed identity template", func(t *testing.T) {
		t.Parallel()
		ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
			Mode:              plugin.SignatureModeOff,
			TrustedIdentities: []string{"https://github.com/my-org/{repo}/*"},
		})
		fn, err := BuildLockSignatureVerifier(ctx, logr.Discard())
		assert.Nil(t, fn)
		require.ErrorIs(t, err, plugin.ErrInvalidIdentityPattern)
	})

	t.Run("expands {kind} and {name} into effectivePolicy and maps verified result", func(t *testing.T) {
		t.Parallel()
		ctx := plugin.WithSignaturePolicy(context.Background(), &plugin.SignaturePolicy{
			Mode:              plugin.SignatureModeEnforce,
			TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{"https://github.com/my-org/scafctl-{kind}-{name}/.github/workflows/*"},
		})
		fake := &capturingSignatureVerifier{
			result: &plugin.SignatureResult{
				Verified: true,
				Issuer:   "https://token.actions.githubusercontent.com",
				Identity: "https://github.com/my-org/scafctl-provider-github/.github/workflows/release.yml@refs/tags/v1.0.0",
				SignedAt: "2026-03-15T10:30:00Z",
			},
		}
		fn, err := buildLockSignatureVerifierWith(ctx, logr.Discard(), fake)
		require.NoError(t, err)
		require.NotNil(t, fn)

		sig, err := fn(ctx, catalog.ArtifactKindProvider, "github", "ghcr.io/my-org/providers/github@sha256:abc")
		require.NoError(t, err)
		require.NotNil(t, fake.gotPolicy)
		assert.Equal(t, []string{"https://github.com/my-org/scafctl-provider-github/.github/workflows/*"}, fake.gotPolicy.TrustedIdentities)
		assert.Equal(t, &bundler.LockPluginSignature{
			Issuer:   "https://token.actions.githubusercontent.com",
			Identity: "https://github.com/my-org/scafctl-provider-github/.github/workflows/release.yml@refs/tags/v1.0.0",
			SignedAt: "2026-03-15T10:30:00Z",
		}, sig)

		fake.result = &plugin.SignatureResult{Verified: false}
		sig, err = fn(ctx, catalog.ArtifactKindProvider, "github", "ghcr.io/my-org/providers/github@sha256:abc")
		require.NoError(t, err)
		assert.Nil(t, sig)
	})
}

type capturingSignatureVerifier struct {
	gotPolicy *plugin.SignaturePolicy
	result    *plugin.SignatureResult
	err       error
}

func (c *capturingSignatureVerifier) VerifySignature(_ context.Context, _ string, policy *plugin.SignaturePolicy) (*plugin.SignatureResult, error) {
	c.gotPolicy = policy
	return c.result, c.err
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
