// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustVersion(s string) *semver.Version {
	v, err := semver.NewVersion(s)
	if err != nil {
		panic("bad test version: " + err.Error())
	}
	return v
}

func TestPluginFetcher_ResolvePlugin(t *testing.T) {
	cat := newMockCatalog("test")
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "aws-provider",
		Version: mustVersion("1.5.0"),
	}
	cat.addArtifact(ref, nil, nil)

	pf := NewPluginFetcher(cat, logr.Discard())

	info, err := pf.ResolvePlugin(context.Background(), "aws-provider", ArtifactKindProvider, "1.5.0")
	require.NoError(t, err)
	assert.Equal(t, "aws-provider", info.Reference.Name)
	assert.Equal(t, "1.5.0", info.Reference.Version.String())
	assert.Equal(t, "test", info.Catalog)
}

func TestPluginFetcher_ResolvePlugin_NotFound(t *testing.T) {
	cat := newMockCatalog("test")
	pf := NewPluginFetcher(cat, logr.Discard())

	_, err := pf.ResolvePlugin(context.Background(), "missing", ArtifactKindProvider, "1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving plugin")
}

func TestPluginFetcher_FetchPlugin_DirectFetch(t *testing.T) {
	cat := newMockCatalog("test")
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "simple-plugin",
		Version: mustVersion("1.0.0"),
	}
	cat.addArtifact(ref, []byte("plugin-binary"), nil)

	pf := NewPluginFetcher(cat, logr.Discard())

	data, info, err := pf.FetchPlugin(context.Background(), "simple-plugin", ArtifactKindProvider, "1.0.0", "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("plugin-binary"), data)
	assert.Equal(t, "test", info.Catalog)
}

func TestPluginFetcher_FetchPlugin_PlatformMatch(t *testing.T) {
	cat := newMockCatalog("test")

	// Add a platform-specific artifact
	refLinux := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "multi-plugin",
		Version: mustVersion("2.0.0"),
	}
	cat.addArtifact(refLinux, []byte("linux-binary"), map[string]string{
		AnnotationPlatform: "linux/amd64",
	})

	refDarwin := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "multi-plugin",
		Version: mustVersion("2.0.0"),
	}
	// The mock uses ref.String() as key, so we differentiate via digest in annotations
	// For better testing, we need a different approach - let's use a custom list func
	cat.listFunc = func(_ context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
		return []ArtifactInfo{
			{
				Reference: refLinux,
				Catalog:   "test",
				Annotations: map[string]string{
					AnnotationPlatform: "linux/amd64",
				},
			},
			{
				Reference: refDarwin,
				Catalog:   "test",
				Annotations: map[string]string{
					AnnotationPlatform: "darwin/arm64",
				},
			},
		}, nil
	}

	pf := NewPluginFetcher(cat, logr.Discard())

	// Fetch for linux/amd64
	data, _, err := pf.FetchPlugin(context.Background(), "multi-plugin", ArtifactKindProvider, "2.0.0", "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("linux-binary"), data)
}

func TestPluginFetcher_FetchPlugin_FallbackToDirectOnListError(t *testing.T) {
	cat := newMockCatalog("test")
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "fallback-plugin",
		Version: mustVersion("1.0.0"),
	}
	cat.addArtifact(ref, []byte("fallback-binary"), nil)

	// Make list fail
	cat.listFunc = func(_ context.Context, _ ArtifactKind, _ string) ([]ArtifactInfo, error) {
		return nil, assert.AnError
	}

	pf := NewPluginFetcher(cat, logr.Discard())

	data, _, err := pf.FetchPlugin(context.Background(), "fallback-plugin", ArtifactKindProvider, "1.0.0", "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("fallback-binary"), data)
}

func TestPluginFetcher_FetchPlugin_NoPlatformMatch_FallsBackToDirectFetch(t *testing.T) {
	cat := newMockCatalog("test")
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "single-plat",
		Version: mustVersion("1.0.0"),
	}
	cat.addArtifact(ref, []byte("single-binary"), nil)

	// List returns artifacts but none match the requested platform
	cat.listFunc = func(_ context.Context, _ ArtifactKind, _ string) ([]ArtifactInfo, error) {
		return []ArtifactInfo{
			{
				Reference: ref,
				Catalog:   "test",
				Annotations: map[string]string{
					AnnotationPlatform: "windows/amd64",
				},
			},
		}, nil
	}

	pf := NewPluginFetcher(cat, logr.Discard())

	// Request linux/amd64, which doesn't match — should fall back to direct
	data, _, err := pf.FetchPlugin(context.Background(), "single-plat", ArtifactKindProvider, "1.0.0", "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("single-binary"), data)
}

func TestPluginFetcher_ResolvePlugin_NoVersion(t *testing.T) {
	cat := newMockCatalog("test")
	ref := Reference{
		Kind: ArtifactKindProvider,
		Name: "latest-plugin",
	}
	cat.addArtifact(ref, nil, nil)

	pf := NewPluginFetcher(cat, logr.Discard())

	info, err := pf.ResolvePlugin(context.Background(), "latest-plugin", ArtifactKindProvider, "")
	require.NoError(t, err)
	assert.Equal(t, "latest-plugin", info.Reference.Name)
}
