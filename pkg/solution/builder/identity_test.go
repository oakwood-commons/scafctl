// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// fakeFinder is an ArtifactFinder returning the latest built version per
// name, as LocalCatalog.Find does for an open-version SourceBuilt query.
type fakeFinder struct {
	latest  map[string]string
	err     error
	queries []catalog.ArtifactQuery
}

func (f *fakeFinder) Find(_ context.Context, q catalog.ArtifactQuery) (catalog.ArtifactInfo, error) {
	f.queries = append(f.queries, q)
	if f.err != nil {
		return catalog.ArtifactInfo{}, f.err
	}
	v, ok := f.latest[q.Name]
	if !ok {
		return catalog.ArtifactInfo{}, &catalog.ArtifactNotFoundError{Reference: catalog.Reference{Kind: q.Kind, Name: q.Name}}
	}
	return catalog.ArtifactInfo{Reference: catalog.Reference{
		Kind: q.Kind, Name: q.Name, Version: semver.MustParse(v), Built: true,
	}}, nil
}

// identitySource loads a "demo" solution whose metadata.version line is
// versionLine (empty for no version).
func identitySource(t testing.TB, versionLine string) *SolutionSource {
	t.Helper()
	doc := fmt.Sprintf("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: demo\n%sspec:\n  resolvers: {}\n", versionLine)
	path := writeSourceFile(t, t.TempDir(), "solution.yaml", doc)
	src, err := LoadSolutionSource(t.Context(), path, "", nil)
	require.NoError(t, err)
	return src
}

func TestResolveIdentity(t *testing.T) {
	t.Parallel()

	const (
		metaV1  = "  version: 1.0.0\n"
		metaDev = "  version: 0.0.0-dev\n"
	)
	history := map[string]string{"demo": "1.2.3", "other": "5.0.0"}

	tests := []struct {
		name           string
		meta           string
		opts           IdentityOptions
		latest         map[string]string
		wantName       string
		wantVersion    string
		wantSource     VersionSource
		wantOverridden string
		wantFound      bool
		wantErr        string
	}{
		{
			name: "metadata version", meta: metaV1,
			wantName: "demo", wantVersion: "1.0.0", wantSource: VersionFromMetadata,
		},
		{
			name: "auto-increment first version", latest: map[string]string{},
			wantName: "demo", wantVersion: firstAutoVersion, wantSource: VersionFromAutoIncrement, wantFound: true,
		},
		{
			name: "auto-increment over latest built", latest: history,
			wantName: "demo", wantVersion: "1.2.4", wantSource: VersionFromAutoIncrement, wantFound: true,
		},
		{
			name: "placeholder metadata version auto-increments", meta: metaDev, latest: history,
			wantName: "demo", wantVersion: "1.2.4", wantSource: VersionFromAutoIncrement, wantFound: true,
		},
		{
			name: "--version overrides metadata", meta: metaV1, opts: IdentityOptions{Version: "2.0.0"},
			wantName: "demo", wantVersion: "2.0.0", wantSource: VersionFromFlag, wantOverridden: "1.0.0",
		},
		{
			name: "--version equal to metadata", meta: metaV1, opts: IdentityOptions{Version: "1.0.0"},
			wantName: "demo", wantVersion: "1.0.0", wantSource: VersionFromFlag,
		},
		{
			name: "--version on version-less solution overrides nothing", opts: IdentityOptions{Version: "2.0.0"},
			wantName: "demo", wantVersion: "2.0.0", wantSource: VersionFromFlag,
		},
		{
			name: "--name", meta: metaV1, opts: IdentityOptions{Name: "other"},
			wantName: "other", wantVersion: "1.0.0", wantSource: VersionFromMetadata,
		},
		{
			name: "--name scopes auto-increment", opts: IdentityOptions{Name: "other"}, latest: history,
			wantName: "other", wantVersion: "5.0.1", wantSource: VersionFromAutoIncrement, wantFound: true,
		},
		{
			name: "--tag name and version", meta: metaV1, opts: IdentityOptions{Tag: "other@2.0.0"},
			wantName: "other", wantVersion: "2.0.0", wantSource: VersionFromTag, wantOverridden: "1.0.0",
		},
		{
			name: "--tag name only", meta: metaV1, opts: IdentityOptions{Tag: "other"},
			wantName: "other", wantVersion: "1.0.0", wantSource: VersionFromMetadata,
		},
		{
			name: "--tag remote with kind segment", meta: metaV1, opts: IdentityOptions{Tag: "ghcr.io/org/solutions/other@2.0.0"},
			wantName: "other", wantVersion: "2.0.0", wantSource: VersionFromTag, wantOverridden: "1.0.0",
		},
		{
			name: "--tag remote docker style", meta: metaV1, opts: IdentityOptions{Tag: "ghcr.io/org/other:2.0.0"},
			wantName: "other", wantVersion: "2.0.0", wantSource: VersionFromTag, wantOverridden: "1.0.0",
		},
		{
			name: "--tag name only with --bump", opts: IdentityOptions{Tag: "other", Bump: "minor"}, latest: history,
			wantName: "other", wantVersion: "5.1.0", wantSource: VersionFromBump, wantFound: true,
		},
		{
			name: "--bump patch", meta: metaV1, opts: IdentityOptions{Bump: "patch"}, latest: history,
			wantName: "demo", wantVersion: "1.2.4", wantSource: VersionFromBump, wantFound: true,
		},
		{
			name: "--bump minor", opts: IdentityOptions{Bump: "MINOR"}, latest: history,
			wantName: "demo", wantVersion: "1.3.0", wantSource: VersionFromBump, wantFound: true,
		},
		{
			name: "--bump major", opts: IdentityOptions{Bump: "major"}, latest: history,
			wantName: "demo", wantVersion: "2.0.0", wantSource: VersionFromBump, wantFound: true,
		},
		{
			name: "--bump without built versions", opts: IdentityOptions{Bump: "patch"}, latest: map[string]string{},
			wantFound: true, wantErr: `no existing versions for "demo"`,
		},
		{name: "invalid --bump", opts: IdentityOptions{Bump: "huge"}, wantErr: "invalid bump level"},
		{name: "--bump with --version", opts: IdentityOptions{Bump: "patch", Version: "1.0.0"}, wantErr: "--bump cannot be used together"},
		{name: "--bump with versioned --tag", opts: IdentityOptions{Bump: "patch", Tag: "demo@1.0.0"}, wantErr: "--bump cannot be used together"},
		{name: "invalid --version", opts: IdentityOptions{Version: "one"}, wantErr: `invalid version "one"`},
		{name: "invalid --name", opts: IdentityOptions{Name: "Bad_Name"}, wantErr: `invalid name "Bad_Name"`},
		{name: "--tag with --name", opts: IdentityOptions{Tag: "demo", Name: "demo"}, wantErr: "--tag cannot be used together"},
		{name: "--tag with --version", opts: IdentityOptions{Tag: "demo", Version: "1.0.0"}, wantErr: "--tag cannot be used together"},
		{name: "--tag latest", opts: IdentityOptions{Tag: "demo@latest"}, wantErr: `"latest" is not a valid version`},
		{name: "--tag remote latest", opts: IdentityOptions{Tag: "ghcr.io/org/solutions/demo:Latest"}, wantErr: `"latest" is not a valid version`},
		{name: "--tag digest", opts: IdentityOptions{Tag: "demo@sha256:" + strings.Repeat("a", 64)}, wantErr: "a digest is not a version"},
		{name: "--tag remote digest", opts: IdentityOptions{Tag: "ghcr.io/org/solutions/demo@sha256:" + strings.Repeat("a", 64)}, wantErr: "a digest is not a version"},
		{name: "--tag empty version", opts: IdentityOptions{Tag: "demo@"}, wantErr: "version cannot be empty"},
		{name: "--tag empty name", opts: IdentityOptions{Tag: "@1.0.0"}, wantErr: "name cannot be empty"},
		{name: "--tag other kind", opts: IdentityOptions{Tag: "ghcr.io/org/providers/demo@1.0.0"}, wantErr: `references kind "provider"`},
		{name: "--tag invalid version", opts: IdentityOptions{Tag: "demo@one"}, wantErr: `invalid version "one"`},
		{name: "--version dev placeholder", meta: metaV1, opts: IdentityOptions{Version: "0.0.0-dev"}, wantErr: ErrDevVersion.Error()},
		{name: "--tag dev placeholder", opts: IdentityOptions{Tag: "demo@0.0.0-dev"}, wantErr: ErrDevVersion.Error()},
		{
			name: "--version dev placeholder allowed", meta: metaV1, opts: IdentityOptions{Version: "0.0.0-dev", AllowDevVersion: true},
			wantName: "demo", wantVersion: "0.0.0-dev", wantSource: VersionFromFlag, wantOverridden: "1.0.0",
		},
		{
			name: "--allow-dev-version does not stop auto-increment", opts: IdentityOptions{AllowDevVersion: true},
			wantName: "demo", wantVersion: firstAutoVersion, wantSource: VersionFromAutoIncrement, wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			finder := &fakeFinder{latest: tt.latest}

			id, err := ResolveIdentity(t.Context(), finder, identitySource(t, tt.meta), tt.opts)

			assert.Equal(t, tt.wantFound, len(finder.queries) > 0, "finder queried")
			for _, q := range finder.queries {
				assert.Equal(t, catalog.ArtifactKindSolution, q.Kind)
				assert.Equal(t, catalog.SourceSelector{Kind: catalog.SourceBuilt}, q.Source)
				assert.Empty(t, q.Version, "open version selects the latest")
			}
			if tt.wantErr != "" {
				require.ErrorIs(t, err, ErrInvalidIdentity)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, catalog.ArtifactKindSolution, id.Ref.Kind)
			assert.True(t, id.Ref.Built)
			assert.Equal(t, tt.wantName, id.Ref.Name)
			assert.Equal(t, tt.wantVersion, id.Ref.Version.String())
			assert.Equal(t, tt.wantSource, id.VersionSource)
			if tt.wantOverridden == "" {
				assert.Nil(t, id.OverriddenVersion)
			} else {
				require.NotNil(t, id.OverriddenVersion)
				assert.Equal(t, tt.wantOverridden, id.OverriddenVersion.String())
			}
		})
	}
}

func TestIdentityOptions_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    IdentityOptions
		wantErr string
	}{
		{name: "empty"},
		{name: "all compatible", opts: IdentityOptions{Tag: "demo", Bump: "minor", AllowDevVersion: true}},
		{name: "invalid --bump", opts: IdentityOptions{Bump: "huge"}, wantErr: "invalid bump level"},
		{name: "--tag with --name", opts: IdentityOptions{Tag: "demo", Name: "demo"}, wantErr: "--tag cannot be used together"},
		{name: "--bump with --version", opts: IdentityOptions{Bump: "patch", Version: "1.0.0"}, wantErr: "--bump cannot be used together"},
		{name: "--tag latest", opts: IdentityOptions{Tag: "demo@latest"}, wantErr: `"latest" is not a valid version`},
		{name: "invalid --version", opts: IdentityOptions{Version: "one"}, wantErr: `invalid version "one"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.opts.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalidIdentity)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestResolveIdentity_NoVersionsError(t *testing.T) {
	t.Parallel()

	_, err := ResolveIdentity(t.Context(), &fakeFinder{}, identitySource(t, ""), IdentityOptions{Bump: "patch"})

	var noVersions *solution.ErrNoVersions
	require.ErrorAs(t, err, &noVersions)
	assert.Equal(t, "demo", noVersions.Name)
}

func TestResolveIdentity_FinderErrors(t *testing.T) {
	t.Parallel()

	findErr := errors.New("catalog unavailable")
	tests := []struct {
		name    string
		finder  ArtifactFinder
		opts    IdentityOptions
		wantErr error
	}{
		{name: "auto-increment propagates find error", finder: &fakeFinder{err: findErr}, wantErr: findErr},
		{name: "bump propagates find error", finder: &fakeFinder{err: findErr}, opts: IdentityOptions{Bump: "patch"}, wantErr: findErr},
		{name: "auto-increment needs a finder", wantErr: ErrNilCatalog},
		{name: "bump needs a finder", opts: IdentityOptions{Bump: "patch"}, wantErr: ErrNilCatalog},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ResolveIdentity(t.Context(), tt.finder, identitySource(t, ""), tt.opts)
			require.ErrorIs(t, err, tt.wantErr)
			assert.NotErrorIs(t, err, ErrInvalidIdentity, "catalog failures are not input errors")
		})
	}
}

func TestResolveIdentity_NilFinderUnusedWithExplicitVersion(t *testing.T) {
	t.Parallel()

	id, err := ResolveIdentity(t.Context(), nil, identitySource(t, ""), IdentityOptions{Version: "1.0.0"})
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", id.Ref.Version.String())
}

func TestResolveIdentity_LocalCatalog(t *testing.T) {
	t.Parallel()

	cat, err := catalog.NewLocalCatalogAt(filepath.Join(t.TempDir(), "catalog"), logr.Discard())
	require.NoError(t, err)
	built := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.4.0"), Built: true}
	_, err = cat.Store(t.Context(), built, []byte(sourceDoc), nil, nil, false)
	require.NoError(t, err)
	pulled := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "demo", Version: semver.MustParse("3.0.0"), Remote: "ghcr.io/org"}
	_, err = cat.Store(t.Context(), pulled, []byte(sourceDoc), nil, map[string]string{catalog.AnnotationSourceCanonical: "ghcr.io/org"}, false)
	require.NoError(t, err)

	id, err := ResolveIdentity(t.Context(), cat, identitySource(t, ""), IdentityOptions{Bump: "minor"})
	require.NoError(t, err)
	assert.Equal(t, "1.5.0", id.Ref.Version.String())
}

func BenchmarkResolveIdentity(b *testing.B) {
	src := identitySource(b, "")
	finder := &fakeFinder{latest: map[string]string{"demo": "1.2.3"}}
	ctx := context.Background()

	for b.Loop() {
		finder.queries = finder.queries[:0]
		if _, err := ResolveIdentity(ctx, finder, src, IdentityOptions{Bump: "minor"}); err != nil {
			b.Fatal(err)
		}
	}
}
