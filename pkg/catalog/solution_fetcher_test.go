// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
)

// pullFunc adapts a function to remotePuller.
type pullFunc func(ctx context.Context, ref Reference, dst oras.Target, dstTag string) (ocispec.Descriptor, error)

func (f pullFunc) pull(ctx context.Context, ref Reference, dst oras.Target, dstTag string) (ocispec.Descriptor, error) {
	return f(ctx, ref, dst, dstTag)
}

// ingestFunc adapts a function to localWriter.
type ingestFunc func(ctx context.Context, src oras.ReadOnlyTarget, srcTag string, localRef Reference, copyOpts oras.CopyOptions) (ArtifactInfo, error)

func (f ingestFunc) IngestVerbatim(ctx context.Context, src oras.ReadOnlyTarget, srcTag string, localRef Reference, copyOpts oras.CopyOptions) (ArtifactInfo, error) {
	return f(ctx, src, srcTag, localRef, copyOpts)
}

// findFunc adapts a function to Finder.
type findFunc func(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error)

func (f findFunc) Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	return f(ctx, query)
}

// fakeRemote is a SolutionRemote assembled from function fakes.
type fakeRemote struct {
	pullFunc
	findFunc
	id string
}

func (r fakeRemote) CanonicalID() string { return r.id }

// errUnexpectedFind is returned by remotes whose Find must not be called.
var errUnexpectedFind = errors.New("unexpected find")

// latestIs returns a findFunc reporting version as the newest release.
func latestIs(version string) findFunc {
	return func(_ context.Context, query ArtifactQuery) (ArtifactInfo, error) {
		return ArtifactInfo{Reference: solutionRef(query.Name, version)}, nil
	}
}

// failFind returns a findFunc that always fails with err.
func failFind(err error) findFunc {
	return func(context.Context, ArtifactQuery) (ArtifactInfo, error) {
		return ArtifactInfo{}, err
	}
}

// servingRemote returns a remote that serves one artifact built from layers
// (whatever ref is asked for), plus a log of the refs it was asked to pull.
// Its Find fails with errUnexpectedFind; set findFunc to override.
func servingRemote(t *testing.T, layers ...Layer) (fakeRemote, *[]Reference) {
	t.Helper()
	const srcTag = "src"
	src, root := pushToMemory(t, layerManifest(layers...))
	require.NoError(t, src.Tag(t.Context(), root, srcTag))

	var pulls []Reference
	return fakeRemote{
		pullFunc: func(ctx context.Context, ref Reference, dst oras.Target, dstTag string) (ocispec.Descriptor, error) {
			pulls = append(pulls, ref)
			return oras.Copy(ctx, src, srcTag, dst, dstTag, oras.DefaultCopyOptions)
		},
		findFunc: failFind(errUnexpectedFind),
	}, &pulls
}

// seedLocal ingests a content-only solution into local under ref, which must
// carry a version.
func seedLocal(t *testing.T, local *LocalCatalog, ref Reference, content string) {
	t.Helper()
	ctx := t.Context()
	const srcTag = "src"
	builder := NewManifestBuilder(solutionRef(ref.Name, ref.Version.String())).Layers(contentLayer(content))
	store, root := pushToMemory(t, builder)
	require.NoError(t, store.Tag(ctx, root, srcTag))
	_, err := local.IngestVerbatim(ctx, store, srcTag, ref, oras.DefaultCopyOptions)
	require.NoError(t, err)
}

func TestLocalSolutionStore_FetchSolution(t *testing.T) {
	t.Parallel()

	const remoteID = "registry.example.com/myorg"

	tests := []struct {
		name       string
		layers     []Layer
		wantBundle []byte
		wantLock   []byte
	}{
		{
			name:       "all parts",
			layers:     []Layer{contentLayer("solution: demo"), bundleLayer(), lockLayer("lock bytes")},
			wantBundle: []byte("tar bytes"),
			wantLock:   []byte("lock bytes"),
		},
		{
			name:   "content only",
			layers: []Layer{contentLayer("solution: demo")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			local := newTestCatalog(t)
			ref := solutionRef("demo", "1.0.0")
			ref.Remote = remoteID

			const srcTag = "src"
			store, root := pushToMemory(t, layerManifest(tt.layers...))
			require.NoError(t, store.Tag(ctx, root, srcTag))
			_, err := local.IngestVerbatim(ctx, store, srcTag, ref, oras.DefaultCopyOptions)
			require.NoError(t, err)

			s := NewLocalSolutionStore(local)
			parts, err := s.fetchSolution(ctx, ref)
			require.NoError(t, err)
			assert.Equal(t, "solution: demo", string(parts.Content))
			assert.Equal(t, tt.wantBundle, parts.Bundle)
			assert.Equal(t, tt.wantLock, parts.Lock)

			wantRef := ref
			wantRef.Digest = root.Digest.String()
			assert.Equal(t, wantRef.String(), parts.Ref.String())
			assert.Equal(t, remoteID, parts.Ref.Remote)
			assert.False(t, parts.Ref.Built)
			assert.False(t, parts.Pulled, "served from the local catalog")
		})
	}
}

func TestLocalSolutionStore_Find(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		remoteA = "registry.example.com/a"
		remoteB = "registry.example.com/b"
	)
	local := newTestCatalog(t)
	for _, seed := range []struct{ remote, version string }{
		{remoteA, "1.0.0"},
		{remoteA, "1.1.0"},
		{remoteB, "2.0.0"},
	} {
		seedLocal(t, local, sanitizeRefForRemote(solutionRef("demo", seed.version), seed.remote), "solution: demo")
	}

	s := NewLocalSolutionStore(local)
	info, err := s.Find(ctx, ArtifactQuery{
		Name:   "demo",
		Kind:   ArtifactKindSolution,
		Source: SourceSelector{Kind: SourceRemote, Remote: remoteA},
	})
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", info.Reference.Version.String())

	_, err = s.Find(ctx, ArtifactQuery{Name: "absent", Kind: ArtifactKindSolution})
	assert.True(t, IsNotFound(err), "want not-found, got %v", err)
}

func TestLocalSolutionStore_FetchSolution_NotFound(t *testing.T) {
	t.Parallel()

	s := &LocalSolutionStore{local: newTestCatalog(t)}
	ref := solutionRef("demo", "1.0.0")
	ref.Remote = "registry.example.com/myorg"

	parts, err := s.fetchSolution(t.Context(), ref)
	require.Error(t, err)
	assert.Nil(t, parts)
	assert.True(t, IsNotFound(err), "want not-found, got %v", err)
}

func TestCacheLocally(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const remoteID = "registry.example.com/myorg"

	local := newTestCatalog(t)
	ref := sanitizeRefForRemote(solutionRef("demo", "1.0.0"), remoteID)

	staging, root := pushToMemory(t, layerManifest(contentLayer("solution: demo")))
	require.NoError(t, staging.Tag(ctx, root, stagingTag))

	info, err := cacheLocally(ctx, &LocalSolutionStore{local: local}, ref, staging)
	require.NoError(t, err)

	assert.Equal(t, root.Digest.String(), info.Digest)
	assert.Equal(t, remoteID, info.Reference.Remote)
	assert.False(t, info.Reference.Built)
	assert.Equal(t, remoteID, info.Annotations[AnnotationSourceCanonical])

	// Stored under the remote-origin local tag and resolvable as a pulled artifact.
	resolved, err := local.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, root.Digest.String(), resolved.Digest)
}

func TestCacheLocally_MissingTag(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// The staging store does not hold stagingTag.
	staging, root := pushToMemory(t, layerManifest(contentLayer("solution: demo")))
	require.NoError(t, staging.Tag(ctx, root, "other"))
	ref := sanitizeRefForRemote(solutionRef("demo", "1.0.0"), "registry.example.com/myorg")

	_, err := cacheLocally(ctx, &LocalSolutionStore{local: newTestCatalog(t)}, ref, staging)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copying artifact")
}

func TestGetSolutionFromStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		layers     []Layer
		wantBundle []byte
		wantLock   []byte
		wantErr    string
	}{
		{
			name:       "all parts",
			layers:     []Layer{contentLayer("solution: demo"), bundleLayer(), lockLayer("lock bytes")},
			wantBundle: []byte("tar bytes"),
			wantLock:   []byte("lock bytes"),
		},
		{
			name:   "content only",
			layers: []Layer{contentLayer("solution: demo")},
		},
		{
			name:    "missing content",
			layers:  []Layer{lockLayer("lock bytes")},
			wantErr: MediaTypeSolutionContent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store, root := pushToMemory(t, layerManifest(tt.layers...))

			parts, err := getSolutionFromStorage(ctx, store, root)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "solution: demo", string(parts.Content))
			assert.Equal(t, tt.wantBundle, parts.Bundle)
			assert.Equal(t, tt.wantLock, parts.Lock)
		})
	}
}

func TestPullRemote_Caches(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	local := newTestCatalog(t)
	remote, pulls := servingRemote(t, contentLayer("solution: demo"), lockLayer("lock bytes"))
	ref := sanitizeRefForRemote(solutionRef("demo", "1.0.0"), "registry.example.com/myorg")

	parts, err := pullRemote(ctx, remote, &LocalSolutionStore{local: local}, ref, true)
	require.NoError(t, err)
	assert.Equal(t, "solution: demo", string(parts.Content))
	assert.Equal(t, []byte("lock bytes"), parts.Lock)
	assert.Equal(t, []Reference{ref}, *pulls)

	// Ref is the pulled ref, pinned to the digest actually served.
	assert.Equal(t, ref.Remote, parts.Ref.Remote)
	assert.Equal(t, ref.Version, parts.Ref.Version)
	assert.True(t, IsValidDigest(parts.Ref.Digest), "digest %q", parts.Ref.Digest)
	assert.True(t, parts.Pulled)

	// The pulled artifact is now cached locally under its remote origin.
	info, err := local.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, info.Digest, parts.Ref.Digest)
}

func TestPullRemote_Errors(t *testing.T) {
	t.Parallel()

	errPull := errors.New("registry down")
	ref := sanitizeRefForRemote(solutionRef("demo", "1.0.0"), "registry.example.com/myorg")

	tests := []struct {
		name      string
		remote    func(t *testing.T) remotePuller
		wantErr   string
		wantErrIs error
	}{
		{
			name: "pull fails",
			remote: func(*testing.T) remotePuller {
				return pullFunc(func(context.Context, Reference, oras.Target, string) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{}, errPull
				})
			},
			wantErr:   "pulling solution",
			wantErrIs: errPull,
		},
		{
			name: "artifact has no content layer",
			remote: func(t *testing.T) remotePuller {
				r, _ := servingRemote(t, lockLayer("lock bytes"))
				return r
			},
			wantErr: MediaTypeSolutionContent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			local := newTestCatalog(t)

			parts, err := pullRemote(ctx, tt.remote(t), &LocalSolutionStore{local: local}, ref, true)
			require.Error(t, err)
			assert.Nil(t, parts)
			assert.Contains(t, err.Error(), tt.wantErr)
			if tt.wantErrIs != nil {
				assert.ErrorIs(t, err, tt.wantErrIs)
			}

			// Nothing is cached when the fetch fails.
			_, err = local.Resolve(ctx, ref)
			assert.True(t, IsNotFound(err), "want not-found, got %v", err)
		})
	}
}

func TestPullRemote_CacheFailureIsBestEffort(t *testing.T) {
	t.Parallel()

	remote, _ := servingRemote(t, contentLayer("solution: demo"))
	ingested := 0
	failingWriter := ingestFunc(func(context.Context, oras.ReadOnlyTarget, string, Reference, oras.CopyOptions) (ArtifactInfo, error) {
		ingested++
		return ArtifactInfo{}, errors.New("disk full")
	})
	ref := sanitizeRefForRemote(solutionRef("demo", "1.0.0"), "registry.example.com/myorg")

	parts, err := pullRemote(t.Context(), remote, failingWriter, ref, true)
	require.NoError(t, err)
	assert.Equal(t, "solution: demo", string(parts.Content))
	assert.Equal(t, 1, ingested)
}

func TestPullRemote_DigestOnlyIsNotCached(t *testing.T) {
	t.Parallel()

	remote, pulls := servingRemote(t, contentLayer("solution: demo"))
	ingested := 0
	writer := ingestFunc(func(context.Context, oras.ReadOnlyTarget, string, Reference, oras.CopyOptions) (ArtifactInfo, error) {
		ingested++
		return ArtifactInfo{}, nil
	})
	ref := Reference{
		Kind:   ArtifactKindSolution,
		Name:   "demo",
		Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Remote: "registry.example.com/myorg",
	}

	parts, err := pullRemote(t.Context(), remote, writer, ref, true)
	require.NoError(t, err)
	assert.Equal(t, "solution: demo", string(parts.Content))
	assert.Nil(t, parts.Ref.Version)
	assert.Len(t, *pulls, 1)
	assert.Zero(t, ingested, "digest-only pulls must not be cached")
}

// brokenLocal is a localSource whose reads fail with err.
type brokenLocal struct {
	localSource
	err error
}

func (b brokenLocal) fetchSolution(context.Context, Reference) (*SolutionParts, error) {
	return nil, b.err
}

func TestSolutionFetcher_FetchSolution_BareLocalFailureIsNotMasked(t *testing.T) {
	t.Parallel()

	const remoteA = "registry.example.com/a"
	errCorrupt := errors.New("corrupt blob")
	remote, pulls := servingRemote(t, contentLayer("solution: from-a"))
	remote.findFunc = latestIs("1.0.0")

	f := &SolutionFetcher{
		local:          brokenLocal{err: errCorrupt},
		remotes:        map[string]remoteSource{remoteA: remote},
		defaultCatalog: remoteA,
	}

	parts, err := f.FetchSolution(t.Context(), solutionRef("demo", "1.0.0"))
	require.ErrorIs(t, err, errCorrupt)
	assert.Nil(t, parts)
	assert.Empty(t, *pulls, "a local failure other than not-found must not fall through to the remote")
}

func TestSolutionFetcher_FetchSolution_NoDefaultCatalog(t *testing.T) {
	t.Parallel()

	const remoteA = "registry.example.com/a"
	bare := solutionRef("demo", "1.0.0")

	tests := []struct {
		name       string
		seedBuilt  bool
		seedCached bool
		want       string
	}{
		{name: "local build is still served", seedBuilt: true, want: "solution: built"},
		{name: "nothing built errors"},
		{name: "another remote's cache is not searched", seedCached: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			local := newTestCatalog(t)
			if tt.seedBuilt {
				seedLocal(t, local, sanitizeRefForBuilt(bare), "solution: built")
			}
			if tt.seedCached {
				seedLocal(t, local, sanitizeRefForRemote(bare, remoteA), "solution: cached-a")
			}
			remote, pulls := servingRemote(t, contentLayer("solution: from-a"))
			built := 0
			factory := func(context.Context, string) (SolutionRemote, error) {
				built++
				return remote, nil
			}

			f := NewSolutionFetcher(NewLocalSolutionStore(local), "",
				[]SolutionRemote{remote}, WithRemoteFactory(factory))

			parts, err := f.FetchSolution(ctx, bare)
			if tt.want != "" {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(parts.Content))
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no default catalog is configured")
				assert.ErrorIs(t, err, ErrArtifactNotFound)
			}
			assert.Empty(t, *pulls)
			assert.Zero(t, built, "factory must not be called without a remote")
		})
	}
}

func TestSolutionFetcher_FetchSolution(t *testing.T) {
	t.Parallel()

	const (
		remoteA = "registry.example.com/a"
		remoteB = "registry.example.com/b"
	)
	base := solutionRef("demo", "1.0.0")
	withRemote := func(id string) Reference {
		r := base
		r.Remote = id
		return r
	}
	built := base
	built.Built = true
	builtAndRemote := built
	builtAndRemote.Remote = remoteB

	tests := []struct {
		name        string
		seedBuilt   bool
		seedCachedB bool
		ref         Reference
		want        string
		wantErr     string
		wantPullsA  int
		wantPullsB  int
		wantRemote  string
		wantBuilt   bool
	}{
		{name: "built ref served locally", seedBuilt: true, ref: built, want: "solution: built", wantBuilt: true},
		{name: "built ref missing never hits a remote", ref: built, wantErr: "local catalog"},
		{name: "remote ref served from local cache", seedCachedB: true, ref: withRemote(remoteB), want: "solution: cached-b", wantRemote: remoteB},
		{name: "remote ref pulled from its remote", ref: withRemote(remoteB), want: "solution: from-b", wantPullsB: 1, wantRemote: remoteB},
		{name: "unknown remote", ref: withRemote("registry.example.com/nope"), wantErr: "no catalog configured"},
		{name: "no origin prefers local build", seedBuilt: true, ref: base, want: "solution: built", wantBuilt: true},
		{name: "no origin falls back to default remote", ref: base, want: "solution: from-a", wantPullsA: 1, wantRemote: remoteA},
		{name: "built ref naming a remote is rejected", ref: builtAndRemote, wantErr: "both built and from remote"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			local := newTestCatalog(t)
			if tt.seedBuilt {
				seedLocal(t, local, sanitizeRefForBuilt(base), "solution: built")
			}
			if tt.seedCachedB {
				seedLocal(t, local, withRemote(remoteB), "solution: cached-b")
			}
			// Pinned refs never resolve a version: the fakes' Find fails if called.
			pullerA, pullsA := servingRemote(t, contentLayer("solution: from-a"))
			pullerB, pullsB := servingRemote(t, contentLayer("solution: from-b"))

			f := &SolutionFetcher{
				local:          &LocalSolutionStore{local: local},
				remotes:        map[string]remoteSource{remoteA: pullerA, remoteB: pullerB},
				defaultCatalog: remoteA,
			}

			parts, err := f.FetchSolution(ctx, tt.ref)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(parts.Content))
				assert.Equal(t, tt.ref.Version, parts.Ref.Version)
				assert.Equal(t, tt.wantRemote, parts.Ref.Remote)
				assert.Equal(t, tt.wantBuilt, parts.Ref.Built)
				assert.True(t, IsValidDigest(parts.Ref.Digest), "digest %q", parts.Ref.Digest)
			}
			assert.Len(t, *pullsA, tt.wantPullsA, "pulls from %s", remoteA)
			assert.Len(t, *pullsB, tt.wantPullsB, "pulls from %s", remoteB)
		})
	}
}

func TestSolutionFetcher_FetchSolution_NoCache(t *testing.T) {
	t.Parallel()

	const remoteA = "registry.example.com/a"
	pinned := solutionRef("demo", "1.0.0")
	pinned.Remote = remoteA
	unversioned := Reference{Kind: ArtifactKindSolution, Name: "demo", Remote: remoteA}
	unreachable := &UnreachableError{Catalog: remoteA, Cause: errors.New("dial tcp: connection refused")}

	tests := []struct {
		name          string
		ref           Reference
		seedCached    bool
		seedBuilt     bool
		find          findFunc
		want          string
		wantBuilt     bool
		wantPulls     []string
		wantNotCached bool
		wantErrIs     error
		wantErr       string
	}{
		{
			name:       "pinned ref ignores the cached copy",
			ref:        pinned,
			seedCached: true,
			find:       failFind(errUnexpectedFind),
			want:       "solution: from-a",
			wantPulls:  []string{"1.0.0"},
		},
		{
			name:          "pulled artifact is not cached locally",
			ref:           pinned,
			find:          failFind(errUnexpectedFind),
			want:          "solution: from-a",
			wantPulls:     []string{"1.0.0"},
			wantNotCached: true,
		},
		{
			name:       "unversioned ref ignores the cached resolved version",
			ref:        unversioned,
			seedCached: true,
			find:       latestIs("1.0.0"),
			want:       "solution: from-a",
			wantPulls:  []string{"1.0.0"},
		},
		{
			name:       "unreachable remote does not fall back to the cache",
			ref:        unversioned,
			seedCached: true,
			find:       failFind(unreachable),
			wantErrIs:  ErrCatalogUnreachable,
			wantErr:    "resolving latest version",
		},
		{
			name:      "local build still wins for a bare ref",
			ref:       solutionRef("demo", "1.0.0"),
			seedBuilt: true,
			find:      failFind(errUnexpectedFind),
			want:      "solution: built",
			wantBuilt: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			local := newTestCatalog(t)
			if tt.seedCached {
				seedLocal(t, local, sanitizeRefForRemote(solutionRef("demo", "1.0.0"), remoteA), "cached 1.0.0")
			}
			if tt.seedBuilt {
				seedLocal(t, local, sanitizeRefForBuilt(solutionRef("demo", "1.0.0")), "solution: built")
			}
			remote, pulls := servingRemote(t, contentLayer("solution: from-a"))
			remote.findFunc = tt.find
			remote.id = remoteA

			f := NewSolutionFetcher(NewLocalSolutionStore(local), remoteA, []SolutionRemote{remote})

			parts, err := f.FetchSolution(ctx, tt.ref, WithNoCache())
			if tt.wantErrIs != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErrIs)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Empty(t, *pulls)
				assert.Nil(t, parts)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(parts.Content))
			assert.Equal(t, tt.wantBuilt, parts.Ref.Built)

			pulled := make([]string, 0, len(*pulls))
			for _, p := range *pulls {
				pulled = append(pulled, p.Version.String())
			}
			if tt.wantPulls == nil {
				tt.wantPulls = []string{}
			}
			assert.Equal(t, tt.wantPulls, pulled)

			if tt.wantNotCached {
				_, resolveErr := local.Resolve(ctx, pinned)
				assert.True(t, IsNotFound(resolveErr), "want not cached, got %v", resolveErr)
			}
		})
	}
}

func TestSolutionFetcher_FetchSolution_Unversioned(t *testing.T) {
	t.Parallel()

	const (
		remoteA = "registry.example.com/a"
		remoteB = "registry.example.com/b"
	)
	unversioned := Reference{Kind: ArtifactKindSolution, Name: "demo"}
	fromB := unversioned
	fromB.Remote = remoteB
	errDial := errors.New("dial tcp: connection refused")
	unreachable := &UnreachableError{Catalog: remoteB, Cause: errDial}

	type seed struct{ remote, version string }
	tests := []struct {
		name        string
		ref         Reference
		seeds       []seed
		find        findFunc
		want        string
		wantVersion string
		wantPulls   []string // versions pulled
		wantErrIs   error
		wantErr     string
	}{
		{
			name:        "resolves newest then pulls it",
			ref:         fromB,
			find:        latestIs("2.0.0"),
			want:        "solution: from-b",
			wantVersion: "2.0.0",
			wantPulls:   []string{"2.0.0"},
		},
		{
			name:        "resolved version served from local cache",
			ref:         fromB,
			seeds:       []seed{{remoteB, "1.0.0"}, {remoteB, "2.0.0"}},
			find:        latestIs("1.0.0"),
			want:        "cached b 1.0.0",
			wantVersion: "1.0.0",
		},
		{
			name:        "no origin resolves in the default remote",
			ref:         unversioned,
			find:        latestIs("3.0.0"),
			want:        "solution: from-b",
			wantVersion: "3.0.0",
			wantPulls:   []string{"3.0.0"},
		},
		{
			name:        "unreachable falls back to newest cached from the same remote",
			ref:         fromB,
			seeds:       []seed{{remoteB, "1.0.0"}, {remoteB, "1.2.0"}, {remoteA, "9.0.0"}},
			find:        failFind(unreachable),
			want:        "cached b 1.2.0",
			wantVersion: "1.2.0",
		},
		{
			name:      "unreachable with nothing cached",
			ref:       fromB,
			find:      failFind(unreachable),
			wantErrIs: ErrCatalogUnreachable,
			wantErr:   "resolving latest version",
		},
		{
			name:      "unreachable ignores other remotes' cache",
			ref:       fromB,
			seeds:     []seed{{remoteA, "1.0.0"}},
			find:      failFind(unreachable),
			wantErrIs: ErrCatalogUnreachable,
		},
		{
			name:      "not found does not fall back to cache",
			ref:       fromB,
			seeds:     []seed{{remoteB, "1.0.0"}},
			find:      failFind(&ArtifactNotFoundError{Reference: fromB, Catalog: remoteB}),
			wantErrIs: ErrArtifactNotFound,
		},
		{
			name:      "other errors do not fall back to cache",
			ref:       fromB,
			seeds:     []seed{{remoteB, "1.0.0"}},
			find:      failFind(errUnexpectedFind),
			wantErrIs: errUnexpectedFind,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			local := newTestCatalog(t)
			for _, s := range tt.seeds {
				ref := sanitizeRefForRemote(solutionRef("demo", s.version), s.remote)
				seedLocal(t, local, ref, "cached "+s.remote[len(s.remote)-1:]+" "+s.version)
			}
			remote, pulls := servingRemote(t, contentLayer("solution: from-b"))
			remote.findFunc = tt.find

			f := &SolutionFetcher{
				local:          NewLocalSolutionStore(local),
				remotes:        map[string]remoteSource{remoteB: remote},
				defaultCatalog: remoteB,
			}

			parts, err := f.FetchSolution(ctx, tt.ref)
			if tt.wantErrIs != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErrIs)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Empty(t, *pulls)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(parts.Content))
			assert.Equal(t, tt.wantVersion, parts.Ref.Version.String())
			assert.Equal(t, remoteB, parts.Ref.Remote)
			assert.True(t, IsValidDigest(parts.Ref.Digest), "digest %q", parts.Ref.Digest)

			pulled := make([]string, 0, len(*pulls))
			for _, p := range *pulls {
				assert.Equal(t, remoteB, p.Remote)
				pulled = append(pulled, p.Version.String())
			}
			if tt.wantPulls == nil {
				tt.wantPulls = []string{}
			}
			assert.Equal(t, tt.wantPulls, pulled)
		})
	}
}

// errConstraintMismatch is returned by findConstrained when the query does not
// carry the expected version constraint.
var errConstraintMismatch = errors.New("unexpected version constraint")

// findConstrained returns a findFunc that reports version as the newest match,
// failing unless the query carries constraint.
func findConstrained(constraint, version string) findFunc {
	return func(_ context.Context, query ArtifactQuery) (ArtifactInfo, error) {
		if query.Version != constraint {
			return ArtifactInfo{}, fmt.Errorf("%w: got %q, want %q", errConstraintMismatch, query.Version, constraint)
		}
		return ArtifactInfo{Reference: solutionRef(query.Name, version)}, nil
	}
}

func TestSolutionFetcher_FetchSolution_VersionConstraint(t *testing.T) {
	t.Parallel()

	const (
		remoteB    = "registry.example.com/b"
		constraint = "^1.0.0"
	)
	unversioned := Reference{Kind: ArtifactKindSolution, Name: "demo"}
	fromB := unversioned
	fromB.Remote = remoteB
	built := unversioned
	built.Built = true
	unreachable := &UnreachableError{Catalog: remoteB, Cause: errors.New("dial tcp: connection refused")}

	tests := []struct {
		name        string
		ref         Reference
		constraint  string
		built       []string // versions built locally
		cached      []string // versions cached from remoteB
		find        findFunc
		want        string
		wantVersion string
		wantBuilt   bool
		wantPulls   []string
		wantErrIs   error
		wantErr     string
	}{
		{
			name:        "remote ref resolves the newest match",
			ref:         fromB,
			constraint:  constraint,
			find:        findConstrained(constraint, "1.5.0"),
			want:        "solution: from-b",
			wantVersion: "1.5.0",
			wantPulls:   []string{"1.5.0"},
		},
		{
			name:        "empty constraint resolves the newest version",
			ref:         fromB,
			find:        findConstrained("", "2.0.0"),
			want:        "solution: from-b",
			wantVersion: "2.0.0",
			wantPulls:   []string{"2.0.0"},
		},
		{
			name:        "built ref resolves the newest matching build",
			ref:         built,
			constraint:  constraint,
			built:       []string{"1.0.0", "1.4.0", "2.0.0"},
			find:        failFind(errUnexpectedFind),
			want:        "built 1.4.0",
			wantVersion: "1.4.0",
			wantBuilt:   true,
		},
		{
			name:       "built ref with no matching build never hits a remote",
			ref:        built,
			constraint: constraint,
			built:      []string{"2.0.0"},
			find:       failFind(errUnexpectedFind),
			wantErrIs:  ErrArtifactNotFound,
			wantErr:    `resolving version "^1.0.0"`,
		},
		{
			name:        "bare name prefers a matching local build",
			ref:         unversioned,
			constraint:  constraint,
			built:       []string{"1.0.0", "1.4.0", "2.0.0"},
			find:        failFind(errUnexpectedFind),
			want:        "built 1.4.0",
			wantVersion: "1.4.0",
			wantBuilt:   true,
		},
		{
			name:        "bare name falls to the default remote when no build matches",
			ref:         unversioned,
			constraint:  constraint,
			built:       []string{"2.0.0"},
			find:        findConstrained(constraint, "1.5.0"),
			want:        "solution: from-b",
			wantVersion: "1.5.0",
			wantPulls:   []string{"1.5.0"},
		},
		{
			name:        "unreachable falls back to the newest matching cached version",
			ref:         fromB,
			constraint:  constraint,
			cached:      []string{"1.0.0", "1.2.0", "2.0.0"},
			find:        failFind(unreachable),
			want:        "cached 1.2.0",
			wantVersion: "1.2.0",
		},
		{
			name:       "unreachable with no matching cached version",
			ref:        fromB,
			constraint: constraint,
			cached:     []string{"2.0.0"},
			find:       failFind(unreachable),
			wantErrIs:  ErrCatalogUnreachable,
			wantErr:    `resolving version "^1.0.0"`,
		},
		{
			name:       "pinned ref is rejected",
			ref:        solutionRef("demo", "1.0.0"),
			constraint: constraint,
			find:       failFind(errUnexpectedFind),
			wantErrIs:  ErrInvalidReference,
			wantErr:    "cannot be combined with a pinned version",
		},
		{
			name:       "digest ref is rejected",
			ref:        Reference{Kind: ArtifactKindSolution, Name: "demo", Remote: remoteB, Digest: "sha256:" + strings.Repeat("a", 64)},
			constraint: constraint,
			find:       failFind(errUnexpectedFind),
			wantErrIs:  ErrInvalidReference,
			wantErr:    "cannot be combined with a pinned version",
		},
		{
			name:       "invalid constraint is rejected",
			ref:        fromB,
			constraint: "not a constraint!",
			find:       failFind(errUnexpectedFind),
			wantErrIs:  ErrInvalidReference,
			wantErr:    `invalid version constraint "not a constraint!"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			local := newTestCatalog(t)
			for _, v := range tt.built {
				seedLocal(t, local, sanitizeRefForBuilt(solutionRef("demo", v)), "built "+v)
			}
			for _, v := range tt.cached {
				seedLocal(t, local, sanitizeRefForRemote(solutionRef("demo", v), remoteB), "cached "+v)
			}
			remote, pulls := servingRemote(t, contentLayer("solution: from-b"))
			remote.findFunc = tt.find

			f := &SolutionFetcher{
				local:          NewLocalSolutionStore(local),
				remotes:        map[string]remoteSource{remoteB: remote},
				defaultCatalog: remoteB,
			}

			parts, err := f.FetchSolution(ctx, tt.ref, WithVersionConstraint(tt.constraint))
			if tt.wantErrIs != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErrIs)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, parts)
				assert.Empty(t, *pulls)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(parts.Content))
			assert.Equal(t, tt.wantVersion, parts.Ref.Version.String())
			assert.Equal(t, tt.wantBuilt, parts.Ref.Built)

			pulled := make([]string, 0, len(*pulls))
			for _, p := range *pulls {
				pulled = append(pulled, p.Version.String())
			}
			if tt.wantPulls == nil {
				tt.wantPulls = []string{}
			}
			assert.Equal(t, tt.wantPulls, pulled)
		})
	}
}

func TestSolutionFetcher_RemoteFactory(t *testing.T) {
	t.Parallel()

	const (
		configured   = "registry.example.com/configured"
		unconfigured = "registry.example.com/adhoc"
	)
	errFactory := errors.New("no credentials")
	pinned := func(remote string) Reference {
		return sanitizeRefForRemote(solutionRef("demo", "1.0.0"), remote)
	}
	unpinned := func(remote string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: "demo", Remote: remote}
	}

	tests := []struct {
		name       string
		ref        Reference
		seed       bool // cache ref locally first
		factoryErr error
		want       string
		wantErr    string
		wantBuilt  []string // remote IDs the factory was asked for
	}{
		{name: "configured remote wins", ref: pinned(configured), want: "solution: configured"},
		{name: "factory builds an unconfigured remote", ref: pinned(unconfigured), want: "solution: adhoc", wantBuilt: []string{unconfigured}},
		{name: "unversioned ref builds the remote once", ref: unpinned(unconfigured), want: "solution: adhoc", wantBuilt: []string{unconfigured}},
		{name: "cache hit never builds a remote", ref: pinned(unconfigured), seed: true, want: "cached"},
		{name: "factory error is wrapped", ref: pinned(unconfigured), factoryErr: errFactory, wantErr: "creating catalog for remote", wantBuilt: []string{unconfigured}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			local := newTestCatalog(t)
			if tt.seed {
				seedLocal(t, local, tt.ref, "cached")
			}
			configuredRemote, _ := servingRemote(t, contentLayer("solution: configured"))
			configuredRemote.id = configured
			adhocRemote, _ := servingRemote(t, contentLayer("solution: adhoc"))
			adhocRemote.findFunc = latestIs("1.0.0")

			var built []string
			factory := func(_ context.Context, remoteID string) (SolutionRemote, error) {
				built = append(built, remoteID)
				if tt.factoryErr != nil {
					return nil, tt.factoryErr
				}
				return adhocRemote, nil
			}

			f := NewSolutionFetcher(NewLocalSolutionStore(local), configured,
				[]SolutionRemote{configuredRemote}, WithRemoteFactory(factory))

			parts, err := f.FetchSolution(ctx, tt.ref)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.ErrorIs(t, err, tt.factoryErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(parts.Content))
			}
			assert.Equal(t, tt.wantBuilt, built)
		})
	}
}

func TestSolutionFetcher_FetchSolution_CachesUnderPulledRemote(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		remoteA = "registry.example.com/a"
		remoteB = "registry.example.com/b"
	)
	local := newTestCatalog(t)
	pullerA, pullsA := servingRemote(t, contentLayer("solution: from-a"))
	pullerB, pullsB := servingRemote(t, contentLayer("solution: from-b"))
	f := &SolutionFetcher{
		local:          &LocalSolutionStore{local: local},
		remotes:        map[string]remoteSource{remoteA: pullerA, remoteB: pullerB},
		defaultCatalog: remoteA,
	}

	ref := solutionRef("demo", "1.0.0")
	ref.Remote = remoteB
	for range 2 {
		parts, err := f.FetchSolution(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "solution: from-b", string(parts.Content))
	}

	// The second fetch is served from the local cache.
	assert.Len(t, *pullsB, 1)
	assert.Empty(t, *pullsA)

	// The cached copy is stamped with the remote it was pulled from, not the default.
	info, err := local.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, remoteB, info.Annotations[AnnotationSourceCanonical])
}

func TestNewSolutionFetcher(t *testing.T) {
	t.Parallel()

	local := NewLocalSolutionStore(newTestCatalog(t))
	remotes := []SolutionRemote{
		fakeRemote{id: "registry.example.com/a"},
		fakeRemote{id: "registry.example.com/b"},
	}

	f := NewSolutionFetcher(local, "registry.example.com/a", remotes)
	assert.Same(t, local, f.local)
	assert.Equal(t, "registry.example.com/a", f.defaultCatalog)
	assert.Len(t, f.remotes, 2)
	assert.Contains(t, f.remotes, "registry.example.com/a")
	assert.Contains(t, f.remotes, "registry.example.com/b")
	assert.Nil(t, f.newRemote)

	factory := func(context.Context, string) (SolutionRemote, error) { return nil, errors.New("unused") }
	f = NewSolutionFetcher(local, "registry.example.com/a", nil, WithRemoteFactory(factory))
	assert.Empty(t, f.remotes)
	assert.NotNil(t, f.newRemote)
}

func BenchmarkSolutionFetcher_FetchSolution(b *testing.B) {
	const remoteID = "registry.example.com/myorg"
	ctx := b.Context()

	local, err := NewLocalCatalogAt(b.TempDir(), logr.Discard())
	require.NoError(b, err)
	pinned := sanitizeRefForRemote(solutionRef("demo", "1.0.0"), remoteID)

	const srcTag = "src"
	store := memory.New()
	root, err := layerManifest(contentLayer("solution: demo")).Push(ctx, store)
	require.NoError(b, err)
	require.NoError(b, store.Tag(ctx, root, srcTag))
	_, err = local.IngestVerbatim(ctx, store, srcTag, pinned, oras.DefaultCopyOptions)
	require.NoError(b, err)

	remote := fakeRemote{id: remoteID, findFunc: latestIs("1.0.0")}
	f := NewSolutionFetcher(NewLocalSolutionStore(local), remoteID, []SolutionRemote{remote})

	benchmarks := []struct {
		name string
		ref  Reference
	}{
		{name: "pinned cache hit", ref: pinned},
		{name: "unversioned cache hit", ref: Reference{Kind: ArtifactKindSolution, Name: "demo", Remote: remoteID}},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := f.FetchSolution(ctx, bm.ref); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
