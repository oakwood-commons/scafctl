// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"100B", 100, false},
		{"1KB", 1024, false},
		{"50MB", 50 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"100", 100, false},
		{"50mb", 50 * 1024 * 1024, false},
		{"invalid", 0, true},
		{"MB", 0, true},
		{"xGB", 0, true}, // invalid: non-numeric prefix
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseByteSize(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFormatByteSize(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{50 * 1024 * 1024, "50.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, format.Bytes(tt.input))
		})
	}
}

func newTestCatalogRegistry(t *testing.T) (*catalog.Registry, *catalog.LocalCatalog) {
	t.Helper()
	tmpDir := t.TempDir()
	local, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)
	reg := catalog.NewRegistryWithLocal(local, logr.Discard())
	return reg, local
}

func TestRegistryFetcherAdapter_FetchSolution_InvalidRef(t *testing.T) {
	reg, _ := newTestCatalogRegistry(t)
	adapter := &RegistryFetcherAdapter{Registry: reg}

	_, _, err := adapter.FetchSolution(context.Background(), "")
	require.Error(t, err)
}

func TestRegistryFetcherAdapter_FetchSolution_NotFound(t *testing.T) {
	reg, _ := newTestCatalogRegistry(t)
	adapter := &RegistryFetcherAdapter{Registry: reg}

	_, _, err := adapter.FetchSolution(context.Background(), "nonexistent@1.0.0")
	require.Error(t, err)
}

func TestRegistryFetcherAdapter_FetchSolution_Found(t *testing.T) {
	ctx := context.Background()
	reg, local := newTestCatalogRegistry(t)

	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    "test-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := local.Store(ctx, ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	adapter := &RegistryFetcherAdapter{Registry: reg}
	content, info, err := adapter.FetchSolution(ctx, "test-sol@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte("content"), content)
	assert.Equal(t, "test-sol", info.Reference.Name)
}

func TestRegistryFetcherAdapter_ListSolutions(t *testing.T) {
	ctx := context.Background()
	reg, local := newTestCatalogRegistry(t)

	ref1 := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "list-sol", Version: semver.MustParse("1.0.0")}
	ref2 := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "list-sol", Version: semver.MustParse("2.0.0")}
	_, err := local.Store(ctx, ref1, []byte("v1"), nil, nil, false)
	require.NoError(t, err)
	_, err = local.Store(ctx, ref2, []byte("v2"), nil, nil, false)
	require.NoError(t, err)

	adapter := &RegistryFetcherAdapter{Registry: reg}
	results, err := adapter.ListSolutions(ctx, "list-sol")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}
