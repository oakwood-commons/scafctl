// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandKindSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		want     []ArtifactKind
		wantOK   bool
	}{
		{
			name:     "empty selector means all kinds",
			selector: "",
			want:     nil,
			wantOK:   true,
		},
		{
			name:     "plugin expands to provider and auth-handler",
			selector: KindSelectorPlugin,
			want:     []ArtifactKind{ArtifactKindProvider, ArtifactKindAuthHandler},
			wantOK:   true,
		},
		{
			name:     "solution passes through",
			selector: "solution",
			want:     []ArtifactKind{ArtifactKindSolution},
			wantOK:   true,
		},
		{
			name:     "provider passes through",
			selector: "provider",
			want:     []ArtifactKind{ArtifactKindProvider},
			wantOK:   true,
		},
		{
			name:     "auth-handler passes through",
			selector: "auth-handler",
			want:     []ArtifactKind{ArtifactKindAuthHandler},
			wantOK:   true,
		},
		{
			name:     "unknown selector is rejected",
			selector: "bogus",
			want:     nil,
			wantOK:   false,
		},
		{
			name:     "plugin is not a valid stored ArtifactKind",
			selector: KindSelectorPlugin,
			// sanity: even though it expands, it must not be a stored kind
			want:   []ArtifactKind{ArtifactKindProvider, ArtifactKindAuthHandler},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExpandKindSelector(tt.selector)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestKindSelectorPlugin_IsNotValidArtifactKind(t *testing.T) {
	// The "plugin" selector must never be treated as a stored artifact kind.
	assert.False(t, ArtifactKind(KindSelectorPlugin).IsValid(),
		"plugin must be a selector alias, not a stored ArtifactKind")
}

func TestPluginKinds(t *testing.T) {
	assert.Equal(t, []ArtifactKind{ArtifactKindProvider, ArtifactKindAuthHandler}, PluginKinds())
}

func TestListAcrossKinds_EmptyListsAll(t *testing.T) {
	ctx := context.Background()
	cat := newMockCatalog("test")

	var gotKind ArtifactKind
	var called int
	cat.listFunc = func(_ context.Context, kind ArtifactKind, _ string) ([]ArtifactInfo, error) {
		called++
		gotKind = kind
		return []ArtifactInfo{{Reference: Reference{Kind: ArtifactKindSolution, Name: "a"}}}, nil
	}

	got, err := ListAcrossKinds(ctx, cat, nil, "", logr.Discard())
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, 1, called, "empty kinds should call List exactly once")
	assert.Equal(t, ArtifactKind(""), gotKind, "empty kinds should pass empty kind to List (all)")
}

func TestListAcrossKinds_SingleKindErrorIsStrict(t *testing.T) {
	ctx := context.Background()
	cat := newMockCatalog("test")

	sentinel := errors.New("boom")
	cat.listFunc = func(_ context.Context, _ ArtifactKind, _ string) ([]ArtifactInfo, error) {
		return nil, sentinel
	}

	// A single-kind request must surface the error directly (no tolerance).
	_, err := ListAcrossKinds(ctx, cat, []ArtifactKind{ArtifactKindProvider}, "x", logr.Discard())
	require.ErrorIs(t, err, sentinel)
}

func TestListAcrossKinds_MultipleKindsConcatenated(t *testing.T) {
	ctx := context.Background()
	cat := newMockCatalog("test")

	var seen []ArtifactKind
	cat.listFunc = func(_ context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
		seen = append(seen, kind)
		return []ArtifactInfo{{Reference: Reference{Kind: kind, Name: name + "-" + kind.String()}}}, nil
	}

	got, err := ListAcrossKinds(ctx, cat, PluginKinds(), "gh", logr.Discard())
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Order preserved: provider first, then auth-handler.
	assert.Equal(t, []ArtifactKind{ArtifactKindProvider, ArtifactKindAuthHandler}, seen)
	assert.Equal(t, ArtifactKindProvider, got[0].Reference.Kind)
	assert.Equal(t, ArtifactKindAuthHandler, got[1].Reference.Kind)
}

func TestListAcrossKinds_MultiKindToleratesAbsentKind(t *testing.T) {
	ctx := context.Background()
	cat := newMockCatalog("test")

	// A plugin present only under providers/<name>: the auth-handler repo does
	// not exist, so List returns an error for that kind. The provider result
	// must still be returned (this is the #482 P1 fix).
	notFound := errors.New("failed to list tags: name unknown")
	cat.listFunc = func(_ context.Context, kind ArtifactKind, _ string) ([]ArtifactInfo, error) {
		if kind == ArtifactKindAuthHandler {
			return nil, notFound
		}
		return []ArtifactInfo{{Reference: Reference{Kind: kind, Name: "github"}}}, nil
	}

	got, err := ListAcrossKinds(ctx, cat, PluginKinds(), "github", logr.Discard())
	require.NoError(t, err, "one absent kind must not fail the whole multi-kind listing")
	require.Len(t, got, 1)
	assert.Equal(t, ArtifactKindProvider, got[0].Reference.Kind)
}

func TestListAcrossKinds_MultiKindAllFailSurfacesError(t *testing.T) {
	ctx := context.Background()
	cat := newMockCatalog("test")

	// When EVERY kind fails (e.g. the whole catalog is unreachable), the error
	// must surface rather than masquerading as an empty result.
	sentinel := errors.New("catalog unreachable")
	cat.listFunc = func(_ context.Context, _ ArtifactKind, _ string) ([]ArtifactInfo, error) {
		return nil, sentinel
	}

	got, err := ListAcrossKinds(ctx, cat, PluginKinds(), "", logr.Discard())
	require.ErrorIs(t, err, sentinel)
	assert.Nil(t, got, "no partial success means the error is returned")
}
