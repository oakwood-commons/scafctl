// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPluginResolver implements PluginResolver for testing.
type mockPluginResolver struct {
	plugins map[string]catalog.ArtifactInfo
}

func (m *mockPluginResolver) ResolvePlugin(_ context.Context, name string, kind catalog.ArtifactKind, _ string) (catalog.ArtifactInfo, error) {
	key := name + ":" + string(kind)
	info, ok := m.plugins[key]
	if !ok {
		return catalog.ArtifactInfo{}, &catalog.ArtifactNotFoundError{
			Reference: catalog.Reference{Kind: kind, Name: name},
			Catalog:   "mock",
		}
	}
	return info, nil
}

func TestVendorPlugins_ResolvesFromCatalog(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"azure-provider:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "azure-provider",
					Version: semver.MustParse("1.2.3"),
				},
				Digest:  "sha256:abc123",
				Catalog: "test-catalog",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
		},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, "azure-provider", result.ResolvedPlugins[0].Name)
	assert.Equal(t, "provider", result.ResolvedPlugins[0].Kind)
	assert.Equal(t, "1.2.3", result.ResolvedPlugins[0].Version)
	assert.Equal(t, "sha256:abc123", result.ResolvedPlugins[0].Digest)
	assert.Equal(t, "test-catalog", result.ResolvedPlugins[0].ResolvedFrom)
}

func TestVendorPlugins_ReplaysFromLockFile(t *testing.T) {
	ctx := testContext()

	// Resolver should NOT be called if lock entry is valid
	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
		},
	}

	existingLock := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			{
				Name:         "azure-provider",
				Kind:         "provider",
				Version:      "1.2.3",
				Digest:       "sha256:abc123",
				ResolvedFrom: "cached-catalog",
			},
		},
	}

	result, err := VendorPlugins(ctx, plugins, existingLock, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	// Should come from the lock file, not the resolver
	assert.Equal(t, "cached-catalog", result.ResolvedPlugins[0].ResolvedFrom)
	assert.Equal(t, "1.2.3", result.ResolvedPlugins[0].Version)
}

func TestVendorPlugins_StaleLockEntryReResolves(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"azure-provider:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "azure-provider",
					Version: semver.MustParse("2.0.0"),
				},
				Digest:  "sha256:newdigest",
				Catalog: "fresh-catalog",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^2.0.0",
		},
	}

	// Lock file has version 1.2.3 which doesn't satisfy ^2.0.0
	existingLock := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			{
				Name:         "azure-provider",
				Kind:         "provider",
				Version:      "1.2.3",
				Digest:       "sha256:olddigest",
				ResolvedFrom: "old-catalog",
			},
		},
	}

	result, err := VendorPlugins(ctx, plugins, existingLock, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	// Should be re-resolved from catalog, not the stale lock
	assert.Equal(t, "fresh-catalog", result.ResolvedPlugins[0].ResolvedFrom)
	assert.Equal(t, "2.0.0", result.ResolvedPlugins[0].Version)
	assert.Equal(t, "sha256:newdigest", result.ResolvedPlugins[0].Digest)
}

func TestVendorPlugins_NilResolver(t *testing.T) {
	ctx := testContext()

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
		},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: nil,
	})
	require.NoError(t, err)
	assert.Empty(t, result.ResolvedPlugins)
}

func TestVendorPlugins_MultiplePlugins(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"azure-provider:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "azure-provider",
					Version: semver.MustParse("1.5.0"),
				},
				Digest:  "sha256:prov123",
				Catalog: "catalog-a",
			},
			"entra-auth:auth-handler": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindAuthHandler,
					Name:    "entra-auth",
					Version: semver.MustParse("2.1.0"),
				},
				Digest:  "sha256:auth456",
				Catalog: "catalog-b",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
		},
		{
			Name:    "entra-auth",
			Kind:    solution.PluginKindAuthHandler,
			Version: "^2.0.0",
		},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 2)

	assert.Equal(t, "azure-provider", result.ResolvedPlugins[0].Name)
	assert.Equal(t, "provider", result.ResolvedPlugins[0].Kind)
	assert.Equal(t, "entra-auth", result.ResolvedPlugins[1].Name)
	assert.Equal(t, "auth-handler", result.ResolvedPlugins[1].Kind)
}

func TestVendorPlugins_VersionConstraintViolation(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"azure-provider:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "azure-provider",
					Version: semver.MustParse("3.0.0"),
				},
				Digest:  "sha256:wrongversion",
				Catalog: "catalog",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0", // Wants 1.x, but catalog has 3.0.0
		},
	}

	_, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not satisfy constraint")
}

func TestVendorPlugins_WithSignatureVerification(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"signed-plugin:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "signed-plugin",
					Version: semver.MustParse("2.0.0"),
				},
				Digest:   "sha256:def456",
				ImageRef: "ghcr.io/org/plugins/providers/signed-plugin@sha256:manifest123",
				Catalog:  "remote-catalog",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{Name: "signed-plugin", Kind: solution.PluginKindProvider, Version: "^2.0.0"},
	}

	t.Run("records signature metadata", func(t *testing.T) {
		result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
			PluginResolver: resolver,
			VerifySignature: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
				assert.Equal(t, "ghcr.io/org/plugins/providers/signed-plugin@sha256:manifest123", imageRef)
				return &LockPluginSignature{
					Issuer:   "https://token.actions.githubusercontent.com",
					Identity: "https://github.com/org/plugin/.github/workflows/release.yaml@refs/tags/v2.0.0",
					SignedAt: "2025-06-01T10:00:00Z",
				}, nil
			},
		})
		require.NoError(t, err)
		require.Len(t, result.ResolvedPlugins, 1)
		require.NotNil(t, result.ResolvedPlugins[0].Signature)
		assert.Equal(t, "https://token.actions.githubusercontent.com", result.ResolvedPlugins[0].Signature.Issuer)
		assert.Equal(t, "2025-06-01T10:00:00Z", result.ResolvedPlugins[0].Signature.SignedAt)
	})

	t.Run("nil signature on warn-mode failure", func(t *testing.T) {
		result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
			PluginResolver: resolver,
			VerifySignature: func(_ context.Context, _ string) (*LockPluginSignature, error) {
				// Warn mode: caller returns nil (no error to propagate)
				return nil, nil
			},
		})
		require.NoError(t, err)
		require.Len(t, result.ResolvedPlugins, 1)
		assert.Nil(t, result.ResolvedPlugins[0].Signature)
	})

	t.Run("error on enforce-mode failure", func(t *testing.T) {
		_, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
			PluginResolver: resolver,
			VerifySignature: func(_ context.Context, _ string) (*LockPluginSignature, error) {
				return nil, assert.AnError
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "signature verification failed during lock")
	})
}
