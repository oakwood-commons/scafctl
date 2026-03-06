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

func newTestLocalCatalog(t *testing.T) *LocalCatalog {
	t.Helper()
	dir := t.TempDir()
	cat, err := NewLocalCatalogAt(dir, logr.Discard())
	require.NoError(t, err)
	return cat
}

func testPluginRef(name, version string) Reference {
	v, err := semver.NewVersion(version)
	if err != nil {
		panic("bad test version: " + err.Error())
	}
	return Reference{
		Kind:    ArtifactKindProvider,
		Name:    name,
		Version: v,
	}
}

func TestLocalCatalog_StoreMultiPlatform_RoundTrip(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("multi-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-amd64-binary")},
		{Platform: "darwin/arm64", Data: []byte("darwin-arm64-binary")},
	}

	info, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)
	assert.Equal(t, "multi-provider", info.Reference.Name)
	assert.Equal(t, "1.0.0", info.Reference.Version.String())
	assert.NotEmpty(t, info.Digest)

	// Fetch linux/amd64
	data, fetchInfo, err := cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("linux-amd64-binary"), data)
	assert.Equal(t, "linux/amd64", fetchInfo.Annotations[AnnotationPlatform])

	// Fetch darwin/arm64
	data, fetchInfo, err = cat.FetchByPlatform(ctx, ref, "darwin/arm64")
	require.NoError(t, err)
	assert.Equal(t, []byte("darwin-arm64-binary"), data)
	assert.Equal(t, "darwin/arm64", fetchInfo.Annotations[AnnotationPlatform])
}

func TestLocalCatalog_StoreMultiPlatform_PlatformNotFound(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("single-plat", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-binary")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	// Try to fetch a platform that doesn't exist
	_, _, err = cat.FetchByPlatform(ctx, ref, "windows/amd64")
	require.Error(t, err)
	assert.True(t, IsPlatformNotFound(err))
}

func TestLocalCatalog_StoreMultiPlatform_AlreadyExists(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("dup-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("binary")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	// Second store should fail (no force)
	_, err = cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.Error(t, err)
	assert.True(t, IsExists(err))
}

func TestLocalCatalog_StoreMultiPlatform_Force(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("force-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("binary-v1")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	// Force overwrite
	binaries[0].Data = []byte("binary-v2")
	_, err = cat.StoreMultiPlatform(ctx, ref, binaries, nil, true)
	require.NoError(t, err)

	// Verify v2 data
	data, _, err := cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("binary-v2"), data)
}

func TestLocalCatalog_StoreMultiPlatform_EmptyBinaries(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("empty-provider", "1.0.0")

	_, err := cat.StoreMultiPlatform(ctx, ref, nil, nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one platform binary")
}

func TestLocalCatalog_StoreMultiPlatform_InvalidPlatform(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("bad-plat-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "invalid", Data: []byte("binary")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid platform")
}

func TestLocalCatalog_StoreMultiPlatform_FiveplatformRoundTrip(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("all-plats-provider", "2.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-amd64")},
		{Platform: "linux/arm64", Data: []byte("linux-arm64")},
		{Platform: "darwin/amd64", Data: []byte("darwin-amd64")},
		{Platform: "darwin/arm64", Data: []byte("darwin-arm64")},
		{Platform: "windows/amd64", Data: []byte("windows-amd64")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	// Verify each platform
	for _, pb := range binaries {
		data, info, err := cat.FetchByPlatform(ctx, ref, pb.Platform)
		require.NoError(t, err, "platform %s", pb.Platform)
		assert.Equal(t, pb.Data, data, "platform %s", pb.Platform)
		assert.Equal(t, pb.Platform, info.Annotations[AnnotationPlatform])
	}
}

func TestLocalCatalog_ListPlatforms(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("list-plats", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux")},
		{Platform: "darwin/arm64", Data: []byte("darwin")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	platforms, err := cat.ListPlatforms(ctx, ref)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"linux/amd64", "darwin/arm64"}, platforms)
}

func TestLocalCatalog_ListPlatforms_SinglePlatform(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "single-provider",
		Version: mustVersion("1.0.0"),
	}

	// Store a normal single-platform artifact
	_, err := cat.Store(ctx, ref, []byte("binary"), nil, nil, false)
	require.NoError(t, err)

	// ListPlatforms should return nil for single-platform
	platforms, err := cat.ListPlatforms(ctx, ref)
	require.NoError(t, err)
	assert.Nil(t, platforms)
}

func TestLocalCatalog_FetchByPlatform_SinglePlatformFallback(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "single-provider",
		Version: mustVersion("1.0.0"),
	}

	// Store a normal single-platform artifact
	_, err := cat.Store(ctx, ref, []byte("single-binary"), nil, nil, false)
	require.NoError(t, err)

	// FetchByPlatform should return the content for any platform since
	// it's a single-platform manifest (no image index)
	data, _, err := cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("single-binary"), data)
}

func TestLocalCatalog_StoreMultiPlatform_WithAnnotations(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("annotated-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("binary")},
	}

	annotations := map[string]string{
		AnnotationDescription: "A test provider",
		AnnotationAuthors:     "test-author",
	}

	info, err := cat.StoreMultiPlatform(ctx, ref, binaries, annotations, false)
	require.NoError(t, err)
	assert.Equal(t, "A test provider", info.Annotations[AnnotationDescription])
	assert.Equal(t, "test-author", info.Annotations[AnnotationAuthors])
}

func TestLocalCatalog_StoreMultiPlatform_AuthHandler(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := Reference{
		Kind:    ArtifactKindAuthHandler,
		Name:    "multi-auth",
		Version: mustVersion("1.0.0"),
	}

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("auth-linux")},
		{Platform: "darwin/arm64", Data: []byte("auth-darwin")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	data, _, err := cat.FetchByPlatform(ctx, ref, "darwin/arm64")
	require.NoError(t, err)
	assert.Equal(t, []byte("auth-darwin"), data)
}
