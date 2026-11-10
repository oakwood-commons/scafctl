// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"
)

// mockPlatformAwareCatalog extends mockCatalog with PlatformAwareCatalog support.
type mockPlatformAwareCatalog struct {
	mockCatalog
	fetchByPlatformFunc      func(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error)
	listPlatformsFunc        func(ctx context.Context, ref Reference) ([]string, error)
	resolveContentDigestFunc func(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error)
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

func (m *mockPlatformAwareCatalog) ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error) {
	if m.resolveContentDigestFunc != nil {
		return m.resolveContentDigestFunc(ctx, ref, platform, mediaType)
	}
	return ContentDigestInfo{}, ErrArtifactNotFound
}

// Verify that LocalCatalog implements PlatformAwareCatalog.
func TestLocalCatalog_ImplementsPlatformAwareCatalog(t *testing.T) {
	var _ PlatformAwareCatalog = (*LocalCatalog)(nil)
}
