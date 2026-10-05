// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

func TestRouter_Select(t *testing.T) {
	t.Parallel()

	noFetch := fetchFunc(func(context.Context, catalog.Reference) (*catalog.SolutionParts, error) {
		t.Error("Select must not fetch")
		return nil, assert.AnError
	})
	withCatalog := []RouterOption{WithCatalog(nil, noFetch)}

	tests := []struct {
		name       string
		routerOpts []RouterOption
		files      map[string]string
		explicit   string
		args       []string
		policy     ArgPolicy
		opts       []source.Option
		want       Selection
		// wantDiscovered are discovered paths relative to the working directory.
		wantDiscovered []string
		wantErr        error
		wantAnyErr     bool
	}{
		{
			name:       "explicit file keeps all args",
			routerOpts: withCatalog,
			explicit:   "./app.yaml",
			args:       []string{"my-app@1.0.0", "greeting"},
			want: Selection{
				Source: source.Classification{Kind: source.KindLocalFile, Ref: "./app.yaml"},
				Args:   []string{"my-app@1.0.0", "greeting"},
			},
		},
		{
			name:       "explicit catalog name is scoped by opts",
			routerOpts: withCatalog,
			explicit:   "my-app",
			opts:       []source.Option{source.WithVersion("^1.0.0")},
			want: Selection{
				Source: source.Classification{Kind: source.KindBareName, Ref: "my-app", Version: "^1.0.0"},
			},
		},
		{
			name:       "explicit file rejects a version",
			routerOpts: withCatalog,
			explicit:   "./app.yaml",
			opts:       []source.Option{source.WithVersion("^1.0.0")},
			wantAnyErr: true,
		},
		{
			name:  "versioned name arg wins over discovery",
			files: map[string]string{"solution.yaml": ""},
			args:  []string{"my-app@1.0.0", "greeting"},
			want: Selection{
				Source: source.Classification{Kind: source.KindBareName, Ref: "my-app@1.0.0"},
				Args:   []string{"greeting"},
			},
		},
		{
			name:  "URL arg wins over discovery",
			files: map[string]string{"solution.yaml": ""},
			args:  []string{"https://example.com/solution.yaml"},
			want: Selection{
				Source: source.Classification{Kind: source.KindURL, Ref: "https://example.com/solution.yaml"},
				Args:   []string{},
			},
		},
		{
			name: "registry ref arg is selected without discovery",
			args: []string{"ghcr.io/org/app@1.0.0"},
			want: Selection{
				Source: source.Classification{Kind: source.KindRemoteRef, Ref: "ghcr.io/org/app@1.0.0"},
				Args:   []string{},
			},
		},
		{
			name:           "unambiguous ref after the first arg is a name",
			files:          map[string]string{"solution.yaml": ""},
			args:           []string{"greeting", "my-app@1.0.0"},
			want:           Selection{Source: source.Classification{Kind: source.KindAutoDiscover}, Args: []string{"greeting", "my-app@1.0.0"}},
			wantDiscovered: []string{"solution.yaml"},
		},
		{
			name:       "unambiguous arg rejects a conflicting version",
			routerOpts: withCatalog,
			args:       []string{"my-app@1.0.0"},
			opts:       []source.Option{source.WithVersion("^1.0.0")},
			wantAnyErr: true,
		},
		{
			name:           "discovered solution keeps all args",
			routerOpts:     withCatalog,
			files:          map[string]string{"solution.yaml": ""},
			args:           []string{"my-app"},
			policy:         ArgsAreNamesOrCatalog,
			want:           Selection{Source: source.Classification{Kind: source.KindAutoDiscover}, Args: []string{"my-app"}},
			wantDiscovered: []string{"solution.yaml"},
		},
		{
			name:           "discovered matches are all returned",
			files:          map[string]string{"solution.yaml": "", "scafctl/solution.yaml": ""},
			want:           Selection{Source: source.Classification{Kind: source.KindAutoDiscover}},
			wantDiscovered: []string{"scafctl/solution.yaml", "solution.yaml"},
		},
		{
			name:       "discovered solution rejects a version",
			routerOpts: withCatalog,
			files:      map[string]string{"solution.yaml": ""},
			args:       []string{"my-app"},
			opts:       []source.Option{source.WithVersion("^1.0.0")},
			wantAnyErr: true,
		},
		{
			name:       "no solution falls back to a bare name arg",
			routerOpts: withCatalog,
			args:       []string{"my-app", "greeting"},
			policy:     ArgsAreNamesOrCatalog,
			opts:       []source.Option{source.WithVersion("^1.0.0")},
			want: Selection{
				Source: source.Classification{Kind: source.KindBareName, Ref: "my-app", Version: "^1.0.0"},
				Args:   []string{"greeting"},
			},
		},
		{
			name:       "no solution falls back to a catalog-scoped name",
			routerOpts: withCatalog,
			args:       []string{"my-app"},
			policy:     ArgsAreNamesOrCatalog,
			opts:       []source.Option{source.WithCatalog("official")},
			want: Selection{
				Source: source.Classification{Kind: source.KindCatalogQualified, Ref: "my-app", Catalog: "official"},
				Args:   []string{},
			},
		},
		{
			name:       "go-task taskfile does not block the fallback",
			routerOpts: withCatalog,
			files:      map[string]string{"taskfile.yaml": goTaskfile},
			args:       []string{"my-app"},
			policy:     ArgsAreNamesOrCatalog,
			want: Selection{
				Source: source.Classification{Kind: source.KindBareName, Ref: "my-app"},
				Args:   []string{},
			},
		},
		{
			name:       "no fallback with ArgsAreNames",
			routerOpts: withCatalog,
			args:       []string{"my-app"},
			wantErr:    ErrNoSolutionFound,
		},
		{
			name:    "no fallback when catalog is disabled",
			args:    []string{"my-app"},
			policy:  ArgsAreNamesOrCatalog,
			wantErr: ErrNoSolutionFound,
		},
		{
			name:       "no fallback without args",
			routerOpts: withCatalog,
			policy:     ArgsAreNamesOrCatalog,
			wantErr:    ErrNoSolutionFound,
		},
		{
			name:       "no fallback for a local path arg",
			routerOpts: withCatalog,
			args:       []string{"sub/dir"},
			policy:     ArgsAreNamesOrCatalog,
			wantErr:    ErrNoSolutionFound,
		},
		{
			name:       "without local, discovery is not enabled",
			routerOpts: []RouterOption{WithCatalog(nil, noFetch), WithoutLocal()},
			files:      map[string]string{"solution.yaml": ""},
			args:       []string{"my-app"},
			policy:     ArgsAreNamesOrCatalog,
			wantErr:    ErrSourceNotEnabled,
		},
		{
			name:       "without local, an explicit source is still selected",
			routerOpts: []RouterOption{WithoutLocal()},
			explicit:   "./app.yaml",
			want:       Selection{Source: source.Classification{Kind: source.KindLocalFile, Ref: "./app.yaml"}},
		},
		{
			name:       "without local, an unambiguous arg is still selected",
			routerOpts: []RouterOption{WithoutLocal()},
			args:       []string{"ghcr.io/org/app@1.0.0"},
			want: Selection{
				Source: source.Classification{Kind: source.KindRemoteRef, Ref: "ghcr.io/org/app@1.0.0"},
				Args:   []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, tt.files)
			ctx := provider.WithWorkingDirectory(t.Context(), dir)

			got, err := NewRouter("scafctl", tt.routerOpts...).Select(ctx, tt.explicit, tt.args, tt.policy, tt.opts...)
			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, Selection{}, got)
			case tt.wantAnyErr:
				require.Error(t, err)
				assert.Equal(t, Selection{}, got)
			default:
				require.NoError(t, err)
				var wantPaths []string
				for _, p := range tt.wantDiscovered {
					wantPaths = append(wantPaths, filepath.Join(dir, p))
				}
				var gotPaths []string
				if got.Discovered != nil {
					gotPaths = discoveryPaths(got.Discovered)
				}
				assert.Equal(t, wantPaths, gotPaths)
				got.Discovered = nil
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestRouter_SelectionLoader(t *testing.T) {
	t.Parallel()

	t.Run("loads the discovered file without searching again", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"solution.yaml": validSolutionYAML})
		ctx := provider.WithWorkingDirectory(t.Context(), dir)
		r := NewRouter("scafctl")

		sel, err := r.Select(ctx, "", nil, ArgsAreNames)
		require.NoError(t, err)

		// A higher-priority file added after Select is not picked up: the
		// selection is reused rather than searched again.
		writeTree(t, dir, map[string]string{"scafctl/solution.yaml": "not: [valid"})

		m, err := r.SelectionLoader(sel).Load(ctx, sel.Source)
		require.NoError(t, err)
		assert.Equal(t, "test-solution", m.Solution.Metadata.Name)
		require.NotNil(t, m.Discovery)
		assert.Equal(t, filepath.Join(dir, "solution.yaml"), m.Discovery.Path)
		assert.Empty(t, m.OtherMatches)
	})

	t.Run("other sources go to the router", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"app.yaml": validSolutionYAML})
		ctx := provider.WithWorkingDirectory(t.Context(), dir)
		r := NewRouter("scafctl")

		sel, err := r.Select(ctx, filepath.Join(dir, "app.yaml"), nil, ArgsAreNames)
		require.NoError(t, err)

		m, err := r.SelectionLoader(sel).Load(ctx, sel.Source)
		require.NoError(t, err)
		assert.Equal(t, "test-solution", m.Solution.Metadata.Name)
		assert.Nil(t, m.Discovery)
	})

	t.Run("high-risk ambiguity is still enforced", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"solution.yaml": "", "scafctl/solution.yaml": ""})
		ctx := provider.WithWorkingDirectory(t.Context(), dir)
		r := NewRouter("scafctl", WithDiscoverOptions(WithDiscoveryRisk(DiscoveryRiskHigh)))

		sel, err := r.Select(ctx, "", nil, ArgsAreNames)
		require.NoError(t, err)

		_, err = r.SelectionLoader(sel).Load(ctx, sel.Source)
		require.ErrorIs(t, err, ErrAmbiguousSolution)
	})
}

func TestSelection_DiscoveredPath(t *testing.T) {
	t.Parallel()

	one := []Discovery{{Path: "/work/solution.yaml"}}
	two := []Discovery{{Path: "/work/solution.yaml"}, {Path: "/work/scafctl.yaml"}}

	tests := []struct {
		name       string
		discovered []Discovery
		risk       DiscoveryRisk
		want       string
		wantErr    error
	}{
		{name: "not discovered", risk: DiscoveryRiskHigh},
		{name: "single match high risk", discovered: one, risk: DiscoveryRiskHigh, want: "/work/solution.yaml"},
		{name: "several matches low risk uses first", discovered: two, risk: DiscoveryRiskLow, want: "/work/solution.yaml"},
		{name: "several matches high risk is ambiguous", discovered: two, risk: DiscoveryRiskHigh, wantErr: ErrAmbiguousSolution},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Selection{Discovered: tt.discovered}.DiscoveredPath(tt.risk)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), "/work/solution.yaml, /work/scafctl.yaml")
				assert.Contains(t, err.Error(), "use -f/--file")
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkRouter_Select(b *testing.B) {
	dir := b.TempDir()
	writeTree(b, dir, map[string]string{"solution.yaml": ""})
	ctx := provider.WithWorkingDirectory(b.Context(), dir)
	r := NewRouter("scafctl")
	args := []string{"greeting"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.Select(ctx, "", args, ArgsAreNamesOrCatalog); err != nil {
			b.Fatal(err)
		}
	}
}
