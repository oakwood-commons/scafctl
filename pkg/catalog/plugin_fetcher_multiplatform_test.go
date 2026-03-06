// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPlatformAwareCatalog extends mockCatalog with PlatformAwareCatalog support.
type mockPlatformAwareCatalog struct {
	mockCatalog
	fetchByPlatformFunc func(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error)
	listPlatformsFunc   func(ctx context.Context, ref Reference) ([]string, error)
}

func newMockPlatformAwareCatalog(name string) *mockPlatformAwareCatalog {
	return &mockPlatformAwareCatalog{
		mockCatalog: *newMockCatalog(name),
	}
}

func (m *mockPlatformAwareCatalog) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	if m.fetchByPlatformFunc != nil {
		return m.fetchByPlatformFunc(ctx, ref, platform)
	}
	return nil, ArtifactInfo{}, ErrArtifactNotFound
}

func (m *mockPlatformAwareCatalog) ListPlatforms(ctx context.Context, ref Reference) ([]string, error) {
	if m.listPlatformsFunc != nil {
		return m.listPlatformsFunc(ctx, ref)
	}
	return nil, nil
}

func TestPluginFetcher_FetchPlugin_ImageIndex(t *testing.T) {
	cat := newMockPlatformAwareCatalog("test")

	ref := testPluginRef("indexed-plugin", "3.0.0")
	cat.addArtifact(ref, nil, nil)

	cat.fetchByPlatformFunc = func(_ context.Context, r Reference, platform string) ([]byte, ArtifactInfo, error) {
		if r.Name == "indexed-plugin" && platform == "linux/amd64" {
			return []byte("linux-binary"), ArtifactInfo{
				Reference: r,
				Catalog:   "test",
				Annotations: map[string]string{
					AnnotationPlatform: "linux/amd64",
				},
			}, nil
		}
		return nil, ArtifactInfo{}, &PlatformNotFoundError{
			Platform:  platform,
			Available: []string{"linux/amd64"},
		}
	}

	pf := NewPluginFetcher(cat, logr.Discard())

	// Should use image index path
	data, info, err := pf.FetchPlugin(context.Background(), "indexed-plugin", ArtifactKindProvider, "3.0.0", "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("linux-binary"), data)
	assert.Equal(t, "linux/amd64", info.Annotations[AnnotationPlatform])
}

func TestPluginFetcher_FetchPlugin_ImageIndex_PlatformNotFound(t *testing.T) {
	cat := newMockPlatformAwareCatalog("test")

	ref := testPluginRef("indexed-plugin", "3.0.0")
	cat.addArtifact(ref, nil, nil)

	cat.fetchByPlatformFunc = func(_ context.Context, _ Reference, platform string) ([]byte, ArtifactInfo, error) {
		return nil, ArtifactInfo{}, &PlatformNotFoundError{
			Platform:  platform,
			Available: []string{"linux/amd64"},
		}
	}

	pf := NewPluginFetcher(cat, logr.Discard())

	// PlatformNotFoundError should NOT fall back — it's an explicit multi-platform artifact
	_, _, err := pf.FetchPlugin(context.Background(), "indexed-plugin", ArtifactKindProvider, "3.0.0", "darwin/arm64")
	require.Error(t, err)
	assert.True(t, IsPlatformNotFound(err))
}

func TestPluginFetcher_FetchPlugin_ImageIndex_FallsBackToAnnotation(t *testing.T) {
	cat := newMockPlatformAwareCatalog("test")

	ref := testPluginRef("fallback-plugin", "1.0.0")
	cat.addArtifact(ref, []byte("fallback-binary"), map[string]string{
		AnnotationPlatform: "linux/amd64",
	})

	// FetchByPlatform returns a non-platform error (artifact is single-platform)
	cat.fetchByPlatformFunc = func(_ context.Context, _ Reference, _ string) ([]byte, ArtifactInfo, error) {
		return nil, ArtifactInfo{}, ErrArtifactNotFound
	}

	// List returns the artifact with annotation matching
	cat.listFunc = func(_ context.Context, _ ArtifactKind, _ string) ([]ArtifactInfo, error) {
		return []ArtifactInfo{
			{
				Reference: ref,
				Catalog:   "test",
				Annotations: map[string]string{
					AnnotationPlatform: "linux/amd64",
				},
			},
		}, nil
	}

	pf := NewPluginFetcher(cat, logr.Discard())

	data, _, err := pf.FetchPlugin(context.Background(), "fallback-plugin", ArtifactKindProvider, "1.0.0", "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("fallback-binary"), data)
}

func TestPluginFetcher_FetchPlugin_NonPlatformAwareCatalog(t *testing.T) {
	// Use a regular mockCatalog (not PlatformAwareCatalog)
	cat := newMockCatalog("test")

	ref := testPluginRef("basic-plugin", "1.0.0")
	cat.addArtifact(ref, []byte("basic-binary"), nil)

	pf := NewPluginFetcher(cat, logr.Discard())

	// Should fall through to direct fetch (no image index support)
	data, _, err := pf.FetchPlugin(context.Background(), "basic-plugin", ArtifactKindProvider, "1.0.0", "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("basic-binary"), data)
}

// Verify that LocalCatalog implements PlatformAwareCatalog.
func TestLocalCatalog_ImplementsPlatformAwareCatalog(t *testing.T) {
	var _ PlatformAwareCatalog = (*LocalCatalog)(nil)
}
