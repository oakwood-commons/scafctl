// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

var testDigest = "sha256:" + strings.Repeat("a", 64)

func testCatalogIndex() *catalogindex.Index {
	return catalogindex.FromConfig(&config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: config.CatalogNameOfficial, URL: "oci://ghcr.io/oakwood-commons"},
			{Name: "Corp", URL: "https://reg.example.com/team"},
			{Name: "port", URL: "oci://localhost:5000"},
			{Name: "files", Path: "/tmp/catalog"},
		},
	})
}

func TestRemoteReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		raw         string
		wantRemote  string
		wantName    string
		wantVersion string // "" means nil
		wantDigest  string
		wantErrText string
	}{
		{
			name:        "registry repository name version",
			raw:         "ghcr.io/org/app@1.2.3",
			wantRemote:  "ghcr.io/org",
			wantName:    "app",
			wantVersion: "1.2.3",
		},
		{
			name:        "oci prefix with solutions segment",
			raw:         "oci://ghcr.io/org/scafctl/solutions/app@1.0.0",
			wantRemote:  "ghcr.io/org/scafctl",
			wantName:    "app",
			wantVersion: "1.0.0",
		},
		{
			name:       "no repository and no version",
			raw:        "ghcr.io/app",
			wantRemote: "ghcr.io",
			wantName:   "app",
		},
		{
			name:       "registry with port and latest",
			raw:        "localhost:5000/org/app@latest",
			wantRemote: "localhost:5000/org",
			wantName:   "app",
		},
		{
			name:       "digest",
			raw:        "ghcr.io/org/app@" + testDigest,
			wantRemote: "ghcr.io/org",
			wantName:   "app",
			wantDigest: testDigest,
		},
		{
			name:        "docker style tag",
			raw:         "ghcr.io/org/app:1.2.3",
			wantRemote:  "ghcr.io/org",
			wantName:    "app",
			wantVersion: "1.2.3",
		},
		{
			name:        "missing repository",
			raw:         "app",
			wantErrText: "registry and repository",
		},
		{
			name:        "provider reference",
			raw:         "ghcr.io/org/providers/plug@1.0.0",
			wantErrText: "not a solution",
		},
		{
			name:        "invalid digest",
			raw:         "ghcr.io/org/app@sha256:abc",
			wantErrText: "invalid digest",
		},
		{
			name:        "invalid version",
			raw:         "ghcr.io/org/app@not-a-version",
			wantErrText: "invalid version",
		},
		{
			name:        "invalid name",
			raw:         "ghcr.io/org/App@1.0.0",
			wantErrText: "lowercase alphanumeric",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := remoteReference(tt.raw)
			if tt.wantErrText != "" {
				require.ErrorIs(t, err, source.ErrInvalidRef)
				assert.Contains(t, err.Error(), tt.wantErrText)
				assert.Contains(t, err.Error(), tt.raw)
				assert.Equal(t, catalog.Reference{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, catalog.ArtifactKindSolution, got.Kind)
			assert.Equal(t, tt.wantRemote, got.Remote)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, tt.wantDigest, got.Digest)
			assert.False(t, got.Built)
			if tt.wantVersion == "" {
				assert.Nil(t, got.Version)
			} else {
				require.NotNil(t, got.Version)
				assert.Equal(t, tt.wantVersion, got.Version.String())
			}
		})
	}
}

func TestBareNameReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		raw         string
		wantName    string
		wantVersion string // "" means nil
		wantDigest  string
		wantErrText string
	}{
		{name: "name only", raw: "app", wantName: "app"},
		{name: "name with version", raw: "app@1.2.3", wantName: "app", wantVersion: "1.2.3"},
		{name: "name with latest", raw: "my-app@latest", wantName: "my-app"},
		{name: "name with digest", raw: "app@" + testDigest, wantName: "app", wantDigest: testDigest},
		{name: "surrounding whitespace", raw: "  app@1.0.0  ", wantName: "app", wantVersion: "1.0.0"},
		{name: "empty", raw: "", wantErrText: "cannot be empty"},
		{name: "uppercase name", raw: "App@1.0.0", wantErrText: "lowercase alphanumeric"},
		{name: "empty version", raw: "app@", wantErrText: "cannot be empty after @"},
		{name: "invalid digest", raw: "app@sha256:abc", wantErrText: "invalid digest"},
		{name: "invalid version", raw: "app@not-a-version", wantErrText: "invalid version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := bareNameReference(tt.raw)
			if tt.wantErrText != "" {
				require.ErrorIs(t, err, source.ErrInvalidRef)
				assert.Contains(t, err.Error(), tt.wantErrText)
				assert.Equal(t, catalog.Reference{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, catalog.ArtifactKindSolution, got.Kind)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Empty(t, got.Remote)
			assert.Equal(t, tt.wantDigest, got.Digest)
			assert.False(t, got.Built)
			if tt.wantVersion == "" {
				assert.Nil(t, got.Version)
			} else {
				require.NotNil(t, got.Version)
				assert.Equal(t, tt.wantVersion, got.Version.String())
			}
		})
	}
}

func TestCatalogQualifiedReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		catalogs    CatalogAliases
		alias       string
		raw         string
		wantRemote  string
		wantName    string
		wantVersion string // "" means nil
		wantDigest  string
		wantBuilt   bool
		wantErr     error
		wantErrText string
	}{
		{
			name:        "official alias with version",
			catalogs:    testCatalogIndex(),
			alias:       "official",
			raw:         "github@1.2.3",
			wantRemote:  "ghcr.io/oakwood-commons",
			wantName:    "github",
			wantVersion: "1.2.3",
		},
		{
			name:       "alias is case-insensitive",
			catalogs:   testCatalogIndex(),
			alias:      "corp",
			raw:        "app",
			wantRemote: "reg.example.com/team",
			wantName:   "app",
		},
		{
			name:       "alias to registry without repository with latest",
			catalogs:   testCatalogIndex(),
			alias:      "port",
			raw:        "app@latest",
			wantRemote: "localhost:5000",
			wantName:   "app",
		},
		{
			name:       "alias with digest",
			catalogs:   testCatalogIndex(),
			alias:      "official",
			raw:        "app@" + testDigest,
			wantRemote: "ghcr.io/oakwood-commons",
			wantName:   "app",
			wantDigest: testDigest,
		},
		{
			name:        "local alias sets built",
			catalogs:    testCatalogIndex(),
			alias:       "LOCAL",
			raw:         "app@1.0.0",
			wantName:    "app",
			wantVersion: "1.0.0",
			wantBuilt:   true,
		},
		{
			name:      "local alias without index",
			alias:     "local",
			raw:       "app",
			wantName:  "app",
			wantBuilt: true,
		},
		{
			name:        "unknown alias",
			catalogs:    testCatalogIndex(),
			alias:       "nope",
			raw:         "app",
			wantErr:     catalogindex.ErrUnknownCatalog,
			wantErrText: `"nope"`,
		},
		{
			name:     "filesystem alias is not remote",
			catalogs: testCatalogIndex(),
			alias:    "files",
			raw:      "app",
			wantErr:  catalogindex.ErrUnknownCatalog,
		},
		{
			name:    "no index",
			alias:   "official",
			raw:     "app",
			wantErr: catalogindex.ErrUnknownCatalog,
		},
		{
			name:     "typed nil index",
			catalogs: (*catalogindex.Index)(nil),
			alias:    "official",
			raw:      "app",
			wantErr:  catalogindex.ErrUnknownCatalog,
		},
		{
			name:        "invalid name",
			catalogs:    testCatalogIndex(),
			alias:       "official",
			raw:         "App",
			wantErr:     source.ErrInvalidRef,
			wantErrText: "lowercase alphanumeric",
		},
		{
			name:        "invalid version",
			catalogs:    testCatalogIndex(),
			alias:       "official",
			raw:         "app@bad",
			wantErr:     source.ErrInvalidRef,
			wantErrText: "invalid version",
		},
		{
			name:        "empty alias",
			catalogs:    testCatalogIndex(),
			raw:         "app",
			wantErr:     source.ErrInvalidRef,
			wantErrText: "requires a catalog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewCatalogLoader(tt.catalogs, nil).catalogQualifiedReference(tt.alias, tt.raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), tt.wantErrText)
				assert.Equal(t, catalog.Reference{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, catalog.ArtifactKindSolution, got.Kind)
			assert.Equal(t, tt.wantRemote, got.Remote)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, tt.wantDigest, got.Digest)
			assert.Equal(t, tt.wantBuilt, got.Built)
			if tt.wantVersion == "" {
				assert.Nil(t, got.Version)
			} else {
				require.NotNil(t, got.Version)
				assert.Equal(t, tt.wantVersion, got.Version.String())
			}
		})
	}
}

// TestCatalogQualifiedReference_MatchesRemoteReference guards that an alias
// resolves to the same Reference as the equivalent full registry reference.
func TestCatalogQualifiedReference_MatchesRemoteReference(t *testing.T) {
	t.Parallel()

	viaAlias, err := NewCatalogLoader(testCatalogIndex(), nil).catalogQualifiedReference("official", "app@1.2.3")
	require.NoError(t, err)
	viaRegistry, err := remoteReference("ghcr.io/oakwood-commons/solutions/app@1.2.3")
	require.NoError(t, err)

	assert.Equal(t, viaRegistry, viaAlias)
}

// fetchFunc adapts a function to SolutionFetcher, ignoring options.
type fetchFunc func(ctx context.Context, ref catalog.Reference) (*catalog.SolutionParts, error)

func (f fetchFunc) FetchSolution(ctx context.Context, ref catalog.Reference, _ ...catalog.FetchOption) (*catalog.SolutionParts, error) {
	return f(ctx, ref)
}

func TestCatalogLoader_Load(t *testing.T) {
	t.Parallel()

	version := semver.MustParse("1.0.0")
	built := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "app", Version: version, Digest: testDigest, Built: true}
	fromRemote := func(remote string) catalog.Reference {
		r := built
		r.Built = false
		r.Remote = remote
		return r
	}

	tests := []struct {
		name       string
		c          source.Classification
		served     catalog.Reference
		pulled     bool
		wantRemote string
		wantBuilt  bool
		wantPath   string
	}{
		{
			name:       "pulled remote ref keeps the raw ref",
			c:          source.Classification{Kind: source.KindRemoteRef, Ref: "ghcr.io/org/app@1.0.0"},
			served:     fromRemote("ghcr.io/org"),
			pulled:     true,
			wantRemote: "ghcr.io/org",
			wantPath:   "remote:ghcr.io/org/app@1.0.0",
		},
		{
			name:       "cached remote ref is labelled local",
			c:          source.Classification{Kind: source.KindRemoteRef, Ref: "oci://ghcr.io/org/app:1.0.0"},
			served:     fromRemote("ghcr.io/org"),
			wantRemote: "ghcr.io/org",
			wantPath:   "catalog:local:app@1.0.0",
		},
		{
			name:       "pulled catalog alias names the alias",
			c:          source.Classification{Kind: source.KindCatalogQualified, Ref: "app@1.0.0", Catalog: "official"},
			served:     fromRemote("ghcr.io/oakwood-commons"),
			pulled:     true,
			wantRemote: "ghcr.io/oakwood-commons",
			wantPath:   "catalog:official:app@1.0.0",
		},
		{
			name:       "cached catalog alias is labelled local",
			c:          source.Classification{Kind: source.KindCatalogQualified, Ref: "app@1.0.0", Catalog: "official"},
			served:     fromRemote("ghcr.io/oakwood-commons"),
			wantRemote: "ghcr.io/oakwood-commons",
			wantPath:   "catalog:local:app@1.0.0",
		},
		{
			name:      "local alias fetches a build",
			c:         source.Classification{Kind: source.KindCatalogQualified, Ref: "app@1.0.0", Catalog: "local"},
			served:    built,
			wantBuilt: true,
			wantPath:  "catalog:local:app@1.0.0",
		},
		{
			name:     "bare name served by a local build",
			c:        source.Classification{Kind: source.KindBareName, Ref: "app@1.0.0"},
			served:   built,
			wantPath: "catalog:local:app@1.0.0",
		},
		{
			name:     "bare name pulled from the default remote names its alias",
			c:        source.Classification{Kind: source.KindBareName, Ref: "app@1.0.0"},
			served:   fromRemote("reg.example.com/team"),
			pulled:   true,
			wantPath: "catalog:Corp:app@1.0.0",
		},
		{
			name:     "bare name pulled from an unaliased remote names the origin",
			c:        source.Classification{Kind: source.KindBareName, Ref: "app@1.0.0"},
			served:   fromRemote("other.example.com/org"),
			pulled:   true,
			wantPath: "catalog:other.example.com/org:app@1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parts := &catalog.SolutionParts{
				Content: []byte(validSolutionYAML),
				Bundle:  []byte("bundle bytes"),
				Lock:    []byte("lock bytes"),
				Ref:     tt.served,
				Pulled:  tt.pulled,
			}
			var fetched []catalog.Reference
			fetcher := fetchFunc(func(_ context.Context, ref catalog.Reference) (*catalog.SolutionParts, error) {
				fetched = append(fetched, ref)
				return parts, nil
			})

			got, err := NewCatalogLoader(testCatalogIndex(), fetcher).Load(t.Context(), tt.c)
			require.NoError(t, err)

			require.Len(t, fetched, 1)
			assert.Equal(t, catalog.ArtifactKindSolution, fetched[0].Kind)
			assert.Equal(t, "app", fetched[0].Name)
			assert.Equal(t, "1.0.0", fetched[0].Version.String())
			assert.Equal(t, tt.wantRemote, fetched[0].Remote)
			assert.Equal(t, tt.wantBuilt, fetched[0].Built)

			assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
			assert.Equal(t, tt.wantPath, got.Solution.GetPath())
			assert.Equal(t, parts.Bundle, got.Bundle)
			assert.Equal(t, parts.Lock, got.Lock)
			assert.Equal(t, tt.c, got.Source)
			require.NotNil(t, got.Served)
			assert.Equal(t, tt.served, *got.Served)
			assert.Equal(t, tt.pulled, got.Pulled)
		})
	}
}

func TestCatalogLoader_LegacyPath_UnservedKinds(t *testing.T) {
	t.Parallel()

	l := NewCatalogLoader(testCatalogIndex(), nil)
	parts := &catalog.SolutionParts{Pulled: true, Ref: catalog.Reference{Remote: "ghcr.io/org"}}
	for _, kind := range []source.Kind{source.KindUnknown, source.KindAutoDiscover, source.KindStdin, source.KindURL, source.KindLocalFile} {
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, l.legacyPath(source.Classification{Kind: kind, Ref: "ref"}, catalog.Reference{}, parts))
		})
	}
}

func TestCatalogLoader_ServingCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		catalogs CatalogAliases
		parts    catalog.SolutionParts
		want     string
	}{
		{name: "not pulled", catalogs: testCatalogIndex(), parts: catalog.SolutionParts{Ref: catalog.Reference{Remote: "ghcr.io/oakwood-commons"}}, want: "local"},
		{name: "pulled without origin", catalogs: testCatalogIndex(), parts: catalog.SolutionParts{Pulled: true}, want: "local"},
		{name: "pulled from aliased remote", catalogs: testCatalogIndex(), parts: catalog.SolutionParts{Pulled: true, Ref: catalog.Reference{Remote: "ghcr.io/oakwood-commons"}}, want: config.CatalogNameOfficial},
		{name: "pulled from unaliased remote", catalogs: testCatalogIndex(), parts: catalog.SolutionParts{Pulled: true, Ref: catalog.Reference{Remote: "x.example.com"}}, want: "x.example.com"},
		{name: "no index", parts: catalog.SolutionParts{Pulled: true, Ref: catalog.Reference{Remote: "ghcr.io/oakwood-commons"}}, want: "ghcr.io/oakwood-commons"},
		{name: "typed nil index", catalogs: (*catalogindex.Index)(nil), parts: catalog.SolutionParts{Pulled: true, Ref: catalog.Reference{Remote: "ghcr.io/oakwood-commons"}}, want: "ghcr.io/oakwood-commons"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, NewCatalogLoader(tt.catalogs, nil).servingCatalog(&tt.parts))
		})
	}
}

// TestCatalogLoader_Load_VersionConstraint drives a real fetcher, since
// FetchOption is opaque: the errors below can only arise if the
// classification's constraint reached the fetcher.
func TestCatalogLoader_Load_VersionConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		c           source.Classification
		wantErr     error
		wantErrText string
	}{
		{
			name:        "constraint narrows the local lookup",
			c:           source.Classification{Kind: source.KindBareName, Ref: "app", Version: "^1.0.0"},
			wantErr:     catalog.ErrArtifactNotFound,
			wantErrText: `resolving version "^1.0.0"`,
		},
		{
			name:        "constraint on a pinned ref is rejected",
			c:           source.Classification{Kind: source.KindBareName, Ref: "app@1.0.0", Version: "^1.0.0"},
			wantErr:     catalog.ErrInvalidReference,
			wantErrText: "cannot be combined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			local, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
			require.NoError(t, err)
			fetcher := catalog.NewSolutionFetcher(catalog.NewLocalSolutionStore(local), "", nil)

			got, err := NewCatalogLoader(testCatalogIndex(), fetcher).Load(t.Context(), tt.c)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Contains(t, err.Error(), tt.wantErrText)
			assert.Nil(t, got)
		})
	}
}

// cachedRemote is the registry cachedRemoteFetcher's local copy is cached from.
const cachedRemote = "ghcr.io/org"

// cachedRemoteFetcher returns a real fetcher whose local catalog holds
// app@1.0.0 cached from cachedRemote, and which cannot reach any remote. A
// fetch of that ref therefore succeeds only when served from the cache.
func cachedRemoteFetcher(t *testing.T) *catalog.SolutionFetcher {
	t.Helper()
	local, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, "app@1.0.0")
	require.NoError(t, err)
	ref.Remote = cachedRemote
	_, err = local.Store(t.Context(), ref, []byte(validSolutionYAML), nil,
		map[string]string{catalog.AnnotationSourceCanonical: cachedRemote}, false)
	require.NoError(t, err)

	return catalog.NewSolutionFetcher(catalog.NewLocalSolutionStore(local), "", nil)
}

// TestCatalogLoader_Load_NoCache drives a real fetcher, since FetchOption is
// opaque: the cached copy is served unless no-cache reached the fetcher, in
// which case it goes to the (unreachable) remote instead.
func TestCatalogLoader_Load_NoCache(t *testing.T) {
	t.Parallel()

	const remote = cachedRemote
	c := source.Classification{Kind: source.KindRemoteRef, Ref: remote + "/app@1.0.0"}

	t.Run("default serves the cached copy", func(t *testing.T) {
		t.Parallel()
		got, err := NewCatalogLoader(testCatalogIndex(), cachedRemoteFetcher(t)).Load(t.Context(), c)
		require.NoError(t, err)
		assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
		assert.False(t, got.Pulled)
		require.NotNil(t, got.Served)
		assert.Equal(t, remote, got.Served.Remote)
		assert.True(t, catalog.IsValidDigest(got.Served.Digest), "digest %q", got.Served.Digest)
		assert.Equal(t, "catalog:local:app@1.0.0", got.Solution.GetPath())
	})

	t.Run("no-cache bypasses the cached copy", func(t *testing.T) {
		t.Parallel()
		got, err := NewCatalogLoader(testCatalogIndex(), cachedRemoteFetcher(t), WithNoCache()).Load(t.Context(), c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `no catalog configured for remote "`+remote+`"`)
		assert.Nil(t, got)
	})
}

func TestCatalogLoader_Load_Errors(t *testing.T) {
	t.Parallel()

	errFetch := errors.New("registry down")
	errUnexpectedFetch := errors.New("unexpected fetch")
	mustNotFetch := fetchFunc(func(context.Context, catalog.Reference) (*catalog.SolutionParts, error) {
		return nil, errUnexpectedFetch
	})
	serve := func(content string) SolutionFetcher {
		return fetchFunc(func(context.Context, catalog.Reference) (*catalog.SolutionParts, error) {
			return &catalog.SolutionParts{Content: []byte(content)}, nil
		})
	}
	validRemote := source.Classification{Kind: source.KindRemoteRef, Ref: "ghcr.io/org/app@1.0.0"}

	tests := []struct {
		name        string
		c           source.Classification
		fetcher     SolutionFetcher
		wantErr     error
		wantErrText string
	}{
		{
			name:    "invalid remote ref fails before fetch",
			c:       source.Classification{Kind: source.KindRemoteRef, Ref: "ghcr.io/org/App@1.0.0"},
			fetcher: mustNotFetch,
			wantErr: source.ErrInvalidRef,
		},
		{
			name:    "unknown catalog fails before fetch",
			c:       source.Classification{Kind: source.KindCatalogQualified, Ref: "app@1.0.0", Catalog: "nope"},
			fetcher: mustNotFetch,
			wantErr: catalogindex.ErrUnknownCatalog,
		},
		{
			name:    "invalid bare name fails before fetch",
			c:       source.Classification{Kind: source.KindBareName, Ref: "app@not-a-version"},
			fetcher: mustNotFetch,
			wantErr: source.ErrInvalidRef,
		},
		{
			name:        "no fetcher configured",
			c:           validRemote,
			wantErrText: "no solution fetcher configured",
		},
		{
			name: "fetch error is wrapped",
			c:    validRemote,
			fetcher: fetchFunc(func(context.Context, catalog.Reference) (*catalog.SolutionParts, error) {
				return nil, errFetch
			}),
			wantErr:     errFetch,
			wantErrText: "fetching solution",
		},
		{
			name:        "invalid content fails to parse",
			c:           validRemote,
			fetcher:     serve(undefinedDepJSON),
			wantErrText: "parsing solution",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewCatalogLoader(testCatalogIndex(), tt.fetcher).Load(t.Context(), tt.c)
			require.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			assert.Contains(t, err.Error(), tt.wantErrText)
			assert.Contains(t, err.Error(), tt.c.Ref)
			assert.NotErrorIs(t, err, errUnexpectedFetch)
			assert.NotErrorIs(t, err, ErrUnsupportedKind)
			assert.Nil(t, got)
		})
	}
}

func TestCatalogLoader_Load_Lenient(t *testing.T) {
	t.Parallel()

	fetcher := fetchFunc(func(context.Context, catalog.Reference) (*catalog.SolutionParts, error) {
		return &catalog.SolutionParts{Content: []byte(undefinedDepJSON)}, nil
	})
	c := source.Classification{Kind: source.KindRemoteRef, Ref: "ghcr.io/org/app@1.0.0"}

	t.Run("strict rejects invalid spec", func(t *testing.T) {
		t.Parallel()
		got, err := NewCatalogLoader(testCatalogIndex(), fetcher).Load(t.Context(), c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing solution")
		assert.Nil(t, got)
	})

	t.Run("lenient loads invalid spec", func(t *testing.T) {
		t.Parallel()
		got, err := NewCatalogLoader(testCatalogIndex(), fetcher, WithLenient()).Load(t.Context(), c)
		require.NoError(t, err)
		assert.Equal(t, "lenient", got.Solution.Metadata.Name)
	})

	t.Run("lenient still rejects malformed content", func(t *testing.T) {
		t.Parallel()
		malformed := fetchFunc(func(context.Context, catalog.Reference) (*catalog.SolutionParts, error) {
			return &catalog.SolutionParts{Content: []byte("not: [valid")}, nil
		})
		got, err := NewCatalogLoader(testCatalogIndex(), malformed, WithLenient()).Load(t.Context(), c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing solution")
		assert.Nil(t, got)
	})
}

func TestCatalogLoader_Load_UnsupportedKinds(t *testing.T) {
	t.Parallel()

	supported := map[source.Kind]bool{
		source.KindRemoteRef:        true,
		source.KindCatalogQualified: true,
		source.KindBareName:         true,
	}
	for _, kind := range allKinds {
		if supported[kind] {
			continue
		}
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			got, err := NewCatalogLoader(nil, nil).Load(t.Context(), source.Classification{Kind: kind, Ref: "ref"})
			require.ErrorIs(t, err, ErrUnsupportedKind)
			assert.Contains(t, err.Error(), kind.String())
			assert.Nil(t, got)
		})
	}
}

func BenchmarkRemoteReference(b *testing.B) {
	raw := "oci://ghcr.io/org/scafctl/solutions/app@1.2.3"

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := remoteReference(raw); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBareNameReference(b *testing.B) {
	raw := "my-app@1.2.3"

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := bareNameReference(raw); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCatalogQualifiedReference(b *testing.B) {
	l := NewCatalogLoader(testCatalogIndex(), nil)
	raw := "my-app@1.2.3"

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := l.catalogQualifiedReference("official", raw); err != nil {
			b.Fatal(err)
		}
	}
}
