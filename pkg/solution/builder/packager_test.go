// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPackager(t *testing.T) {
	t.Parallel()

	t.Run("binds the catalog handle", func(t *testing.T) {
		t.Parallel()
		cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
		require.NoError(t, err)

		p, err := NewPackager(cat)
		require.NoError(t, err)
		assert.Same(t, cat, p.Catalog())
	})

	t.Run("rejects a nil catalog", func(t *testing.T) {
		t.Parallel()
		p, err := NewPackager(nil)
		require.ErrorIs(t, err, ErrNilCatalog)
		assert.Nil(t, p)
	})

	t.Run("defaults bundle limits", func(t *testing.T) {
		t.Parallel()
		p := newTestPackager(t)
		assert.Equal(t, BundleLimits{MaxSize: bundler.DefaultMaxBundleSize, DedupeThreshold: bundler.DefaultDedupeThreshold}, p.archive.Limits)
		assert.False(t, p.archive.Dedupe)
	})
}

func newTestPackager(t *testing.T) *Packager {
	t.Helper()
	cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	p, err := NewPackager(cat)
	require.NoError(t, err)
	return p
}

func TestPackager_Chain(t *testing.T) {
	t.Parallel()

	p := newTestPackager(t)
	src := identitySource(t, "")
	ref := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0"), Built: true}
	pf := PreflightOptions{SkipLint: true}
	archive := ArchiveOptions{Limits: BundleLimits{MaxSize: 10, DedupeThreshold: 1}, Dedupe: true}
	verify := VerifyOptions{Skip: true, Strict: true}

	got := p.Solution(src).
		As(ref).
		Preflight(pf).
		Vendor(VendorOptions{Skip: true}).
		Cache(CacheOptions{Skip: true}).
		Archive(archive).
		ComposedOut("composed.yaml").
		DryRun(true).
		Store(true).
		Verify(verify)

	assert.Same(t, p, got)
	assert.Same(t, src, p.src)
	assert.Equal(t, ref, p.ref)
	require.NotNil(t, p.preflight)
	assert.Equal(t, pf, *p.preflight)
	assert.True(t, p.vendor.Skip)
	assert.True(t, p.cache.Skip)
	assert.Equal(t, archive, p.archive)
	assert.Equal(t, "composed.yaml", p.composedOut)
	assert.True(t, p.dryRun)
	assert.True(t, p.force)
	assert.Equal(t, verify, p.verify)
}

func TestPackager_Do(t *testing.T) {
	t.Parallel()

	built := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0"), Built: true}
	withRef := func(mut func(*catalog.Reference)) catalog.Reference {
		r := built
		mut(&r)
		return r
	}

	tests := []struct {
		name    string
		noSrc   bool
		ref     catalog.Reference
		wantErr error
	}{
		{name: "missing source", noSrc: true, ref: built, wantErr: ErrNoSource},
		{name: "missing ref", ref: catalog.Reference{}, wantErr: ErrInvalidRef},
		{name: "not built", ref: withRef(func(r *catalog.Reference) { r.Built = false }), wantErr: ErrInvalidRef},
		{name: "wrong kind", ref: withRef(func(r *catalog.Reference) { r.Kind = catalog.ArtifactKindProvider }), wantErr: ErrInvalidRef},
		{name: "no name", ref: withRef(func(r *catalog.Reference) { r.Name = "" }), wantErr: ErrInvalidRef},
		{name: "no version", ref: withRef(func(r *catalog.Reference) { r.Version = nil }), wantErr: ErrInvalidRef},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := newTestPackager(t)
			if !tt.noSrc {
				p.Solution(identitySource(t, ""))
			}
			res, err := p.As(tt.ref).Do(t.Context())
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, res)
		})
	}
}
