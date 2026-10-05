// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
)

// stagingTag tags the artifact in the per-fetch in-memory store. The store
// holds a single artifact, so any fixed tag works.
const stagingTag = "staged"

// SolutionFetcher fetches solution artifacts from the local catalog, falling
// back to a remote catalog and caching what it pulls locally.
type SolutionFetcher struct {
	local          localSource
	remotes        map[string]remoteSource // keyed by remote CanonicalID
	defaultCatalog string
	newRemote      RemoteFactory // nil: unconfigured remotes are an error
}

// remoteSource is a remote catalog the fetcher can resolve versions in and
// pull solution artifacts from.
type remoteSource interface {
	remotePuller
	Finder
}

// SolutionRemote is a remote that can be registered with a SolutionFetcher.
// Its pull method is unexported, so only catalog types (e.g. *RemoteCatalog)
// satisfy it. CanonicalID is only needed to key the remote.
type SolutionRemote interface {
	remoteSource
	CanonicalID() string
}

// RemoteFactory builds a remote for a canonical "registry[/repository]" origin
// that is not among the fetcher's configured remotes.
type RemoteFactory func(ctx context.Context, remoteID string) (SolutionRemote, error)

// SolutionFetcherOption configures a SolutionFetcher.
type SolutionFetcherOption func(*SolutionFetcher)

// WithRemoteFactory lets the fetcher reach remotes that are not configured.
// Configured remotes always take precedence over the factory.
func WithRemoteFactory(fn RemoteFactory) SolutionFetcherOption {
	return func(f *SolutionFetcher) { f.newRemote = fn }
}

// NewSolutionFetcher returns a fetcher over local and remotes. defaultCatalog
// is the CanonicalID of the remote used when a reference names no origin.
func NewSolutionFetcher(local localSource, defaultCatalog string, remotes []SolutionRemote, opts ...SolutionFetcherOption) *SolutionFetcher {
	m := make(map[string]remoteSource, len(remotes))
	for _, r := range remotes {
		m[r.CanonicalID()] = r
	}
	f := &SolutionFetcher{local: local, remotes: m, defaultCatalog: defaultCatalog}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// SolutionParts holds a solution artifact's parts fully read into memory, so it
// stays valid after the store it was read from is gone.
type SolutionParts struct {
	Content []byte
	Bundle  []byte // nil when the artifact has no bundle
	Lock    []byte // nil when the artifact has no lock
	// Ref is the reference actually served: Digest is set, Version is set
	// when known, and Remote/Built record its origin.
	Ref Reference
	// Pulled is true when the artifact was pulled from its remote during this
	// fetch, and false when it was served from the local catalog (a build or a
	// cached copy of a remote artifact).
	Pulled bool
}

// localReader reads a solution's parts from the local catalog.
type localReader interface {
	fetchSolution(ctx context.Context, ref Reference) (*SolutionParts, error)
}

// localWriter caches an artifact into the local catalog unchanged.
type localWriter interface {
	IngestVerbatim(ctx context.Context, src oras.ReadOnlyTarget, srcTag string, localRef Reference, copyOpts oras.CopyOptions) (ArtifactInfo, error)
}

type localSource interface {
	localReader
	localWriter
	Finder
}

// remotePuller copies a solution artifact from a remote catalog into dst,
// tagged dstTag, and returns its root descriptor.
type remotePuller interface {
	pull(ctx context.Context, ref Reference, dst oras.Target, dstTag string) (ocispec.Descriptor, error)
}

// Finder resolves a query to a single artifact.
type Finder interface {
	Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error)
}

func sanitizeRefForBuilt(ref Reference) Reference {
	ref.Built = true
	ref.Remote = ""
	return ref
}

func sanitizeRefForRemote(ref Reference, remoteID string) Reference {
	ref.Built = false
	ref.Remote = remoteID
	return ref
}

// FetchOption configures a single FetchSolution call.
type FetchOption func(*fetchOptions)

type fetchOptions struct {
	constraint string
	noCache    bool
}

// WithVersionConstraint selects the newest version satisfying a semver
// constraint (e.g. "^1.2.0", ">=1.0 <3.0") instead of the newest overall. It
// is valid only for a ref without a version or digest. An empty constraint is
// a no-op.
//
// TODO: add a pre-release option (for --pre-release). Constraints exclude
// pre-releases unless they name one; when nothing stable matches and the
// caller opts in, retry allowing pre-releases.
func WithVersionConstraint(constraint string) FetchOption {
	return func(o *fetchOptions) { o.constraint = constraint }
}

// WithNoCache bypasses the local catalog's remote-artifact cache for this fetch.
// A ref that resolves to a remote is pulled directly from that remote: a cached
// copy is never served in its place, the pulled artifact is not cached locally,
// and the unreachable-remote fallback to a cached version is disabled. Built
// refs (and local builds that win over a remote) are unaffected, since a build
// is not a cache.
func WithNoCache() FetchOption {
	return func(o *fetchOptions) { o.noCache = true }
}

// validate checks the options against ref.
func (o fetchOptions) validate(ref Reference) error {
	if o.constraint == "" {
		return nil
	}
	if isPinned(ref) {
		return &InvalidReferenceError{
			Input:   ref.String(),
			Message: fmt.Sprintf("version constraint %q cannot be combined with a pinned version or digest", o.constraint),
		}
	}
	if _, err := semver.NewConstraint(o.constraint); err != nil {
		return &InvalidReferenceError{
			Input:   ref.String(),
			Message: fmt.Sprintf("invalid version constraint %q: %v", o.constraint, err),
		}
	}
	return nil
}

// FetchSolution returns the parts of the solution ref points at. Built refs
// are served from the local catalog only; refs naming a remote are served
// from the local cache of that remote, then the remote itself. Refs naming
// neither try a local build first, then the default remote. A ref that is
// both built and names a remote is contradictory and rejected.
//
// WithVersionConstraint narrows an unversioned ref to the newest version
// satisfying the constraint, in whichever origin the ref resolves to.
// WithNoCache disables the local cache of remote artifacts for this fetch.
func (f *SolutionFetcher) FetchSolution(ctx context.Context, ref Reference, opts ...FetchOption) (*SolutionParts, error) {
	var o fetchOptions
	for _, opt := range opts {
		opt(&o)
	}
	if err := o.validate(ref); err != nil {
		return nil, err
	}

	switch {
	case ref.Built && ref.Remote != "":
		return nil, &InvalidReferenceError{
			Input:   ref.String(),
			Message: fmt.Sprintf("reference cannot be both built and from remote %q", ref.Remote),
		}
	case ref.Built: // fetch from the local catalog only
		return f.fetchBuilt(ctx, sanitizeRefForBuilt(ref), o.constraint)
	case ref.Remote != "": // fetch from the named remote, caching locally
		return f.fetchRemote(ctx, sanitizeRefForRemote(ref, ref.Remote), o.constraint, o.noCache)
	default:
		return f.fetchBare(ctx, ref, o.constraint, o.noCache)
	}
}

// fetchBuilt serves a built ref from the local catalog. A constraint is first
// resolved to the newest matching local build.
func (f *SolutionFetcher) fetchBuilt(ctx context.Context, ref Reference, constraint string) (*SolutionParts, error) {
	if constraint != "" {
		info, err := f.local.Find(ctx, ArtifactQuery{
			Name:    ref.Name,
			Kind:    ref.Kind,
			Version: constraint,
			Source:  SourceSelector{Kind: SourceBuilt},
		})
		if err != nil {
			return nil, fmt.Errorf("resolving version %q of %s in local catalog: %w", constraint, ref, err)
		}
		ref.Version = info.Reference.Version
	}
	return f.local.fetchSolution(ctx, ref)
}

// fetchBare serves a ref naming no origin. A local build wins outright, even
// when the default remote offers a newer version; the default remote is only
// consulted when no local build exists (or, with a constraint, none matches).
// Any other local failure (corrupt store, unreadable blob) is returned rather
// than masked by a remote copy that may differ from the build. With no default
// remote the lookup stops there rather than searching other remotes.
//
// TODO: decide whether a matching local build should still win when the
// default remote offers a newer version that also satisfies the constraint
// (today: local wins, matching the unconstrained behavior).
func (f *SolutionFetcher) fetchBare(ctx context.Context, ref Reference, constraint string, noCache bool) (*SolutionParts, error) {
	parts, err := f.fetchBuilt(ctx, sanitizeRefForBuilt(ref), constraint)
	if err == nil {
		return parts, nil
	}

	if !IsNotFound(err) {
		return nil, err
	}

	if f.defaultCatalog == "" {
		return nil, fmt.Errorf("solution %s is not built locally and no default catalog is configured: %w", ref, err)
	}
	return f.fetchRemote(ctx, sanitizeRefForRemote(ref, f.defaultCatalog), constraint, noCache)
}

// fetchRemote serves ref (whose Remote names its origin) from the local cache
// when present, otherwise pulls it from its remote. An unversioned ref is first
// pinned to the newest version its remote offers (that satisfies constraint,
// when set). A pinned ref (version or digest) found in the cache is served
// without looking up the remote at all. When noCache is set, the local cache is
// neither read nor written and an unreachable remote does not fall back to it.
func (f *SolutionFetcher) fetchRemote(ctx context.Context, ref Reference, constraint string, noCache bool) (*SolutionParts, error) {
	if isPinned(ref) {
		return f.fetchPinned(ctx, ref, noCache)
	}
	return f.fetchUnpinned(ctx, ref, constraint, noCache)
}

// fetchPinned serves a ref pinned by version and/or digest. When both are set
// they are trusted to name the same artifact: the remote is pulled by digest
// and the served Ref echoes the caller's version unverified. Enforcing that
// pairing belongs to the build layer, not the fetcher.
func (f *SolutionFetcher) fetchPinned(ctx context.Context, ref Reference, noCache bool) (*SolutionParts, error) {
	if !noCache {
		if parts, err := f.local.fetchSolution(ctx, ref); err == nil {
			return parts, nil
		}
	}
	var remote remoteSource
	var err error

	if remote, err = f.remoteFor(ctx, ref.Remote); err != nil {
		return nil, err
	}

	return pullRemote(ctx, remote, f.local, ref, !noCache)
}

func (f *SolutionFetcher) fetchUnpinned(ctx context.Context, ref Reference, constraint string, noCache bool) (*SolutionParts, error) {
	remote, err := f.remoteFor(ctx, ref.Remote)
	if err != nil {
		return nil, err
	}

	pinned, err := f.resolveVersion(ctx, remote, ref, constraint, noCache)
	if err != nil {
		return nil, err
	}
	if !noCache {
		if parts, err := f.local.fetchSolution(ctx, pinned); err == nil {
			return parts, nil
		}
	}

	return pullRemote(ctx, remote, f.local, pinned, !noCache)
}

// isPinned reports whether ref names one exact artifact (version or digest).
func isPinned(ref Reference) bool {
	return ref.HasVersion() || ref.HasDigest()
}

// remoteFor returns the remote serving remoteID: a configured remote first,
// otherwise one built by the remote factory.
func (f *SolutionFetcher) remoteFor(ctx context.Context, remoteID string) (remoteSource, error) {
	if remote, ok := f.remotes[remoteID]; ok {
		return remote, nil
	}
	if f.newRemote == nil {
		return nil, fmt.Errorf("no catalog configured for remote %q", remoteID)
	}
	remote, err := f.newRemote(ctx, remoteID)
	if err != nil {
		return nil, fmt.Errorf("creating catalog for remote %q: %w", remoteID, err)
	}
	return remote, nil
}

// resolveVersion pins an unversioned ref to the newest version remote offers
// that satisfies constraint (any version when constraint is empty). When
// remote is unreachable, the newest matching version cached locally from the
// same remote is used instead; any other failure (not found, auth) is returned.
// With noCache the cached fallback is disabled.
func (f *SolutionFetcher) resolveVersion(ctx context.Context, remote remoteSource, ref Reference, constraint string, noCache bool) (Reference, error) {
	query := ArtifactQuery{
		Name:    ref.Name,
		Kind:    ref.Kind,
		Version: constraint,
		Source:  SourceSelector{Kind: SourceRemote, Remote: ref.Remote},
	}
	wanted := "latest version"
	if constraint != "" {
		wanted = fmt.Sprintf("version %q", constraint)
	}

	info, err := remote.Find(ctx, query)
	if err != nil {
		if noCache {
			return Reference{}, fmt.Errorf("resolving %s of %s: %w", wanted, ref, err)
		}
		if _, unreachable := IsCatalogUnreachable(err); !unreachable {
			return Reference{}, fmt.Errorf("resolving %s of %s: %w", wanted, ref, err)
		}
		cached, cacheErr := f.local.Find(ctx, query)
		if cacheErr != nil {
			return Reference{}, fmt.Errorf("resolving %s of %s: %w", wanted, ref, err)
		}
		logger.FromContext(ctx).Info("remote unreachable; using newest locally cached version",
			"ref", ref.String(), "version", cached.Reference.Version.String(), "error", err.Error())
		info = cached
	}

	ref.Version = info.Reference.Version
	return ref, nil
}

// pullRemote pulls ref from remote into a throwaway in-memory store and reads
// its parts. When cache is true the artifact is also cached in local; caching is
// best-effort, so a failure is logged and does not fail the fetch. When cache is
// false the local catalog is not written at all.
func pullRemote(ctx context.Context, remote remotePuller, local localWriter, ref Reference, cache bool) (*SolutionParts, error) {
	staging := memory.New()
	desc, err := remote.pull(ctx, ref, staging, stagingTag)
	if err != nil {
		return nil, fmt.Errorf("pulling solution %s: %w", ref, err)
	}

	parts, err := getSolutionFromStorage(ctx, staging, desc)
	if err != nil {
		return nil, err
	}
	parts.Ref = ref
	parts.Ref.Digest = desc.Digest.String()
	parts.Pulled = true

	if !cache {
		return parts, nil
	}

	// TODO: decide how to cache digest-only pulls. Without a version the entry
	// would be tagged by digest with no version annotation, so later
	// version-pinned fetches would miss it. One option is to read the version
	// from the pulled manifest's annotations. Until then they are not cached.
	if !ref.HasVersion() {
		logger.FromContext(ctx).V(1).Info("not caching digest-only pull", "ref", ref.String())
		return parts, nil
	}

	if _, err := cacheLocally(ctx, local, ref, staging); err != nil {
		logger.FromContext(ctx).V(1).Info("caching pulled solution locally failed",
			"ref", ref.String(), "error", err.Error())
	}
	return parts, nil
}

// getSolutionFromStorage reads the solution rooted at desc out of store (the
// in-memory copy of a remote artifact). Parts are read eagerly because
// SolutionLayers only fetches when an accessor is called.
func getSolutionFromStorage(ctx context.Context, store oras.ReadOnlyTarget, desc ocispec.Descriptor) (*SolutionParts, error) {
	sol, err := FetchSolutionLayers(ctx, store, desc)
	if err != nil {
		return nil, fmt.Errorf("reading solution %s: %w", desc.Digest, err)
	}

	contentData, err := sol.Content(ctx)
	if err != nil {
		return nil, err
	}
	bundle, _, err := sol.Bundle(ctx)
	if err != nil {
		return nil, err
	}
	lock, _, err := sol.Lock(ctx)
	if err != nil {
		return nil, err
	}

	return &SolutionParts{Content: contentData, Bundle: bundle, Lock: lock}, nil
}

// cacheLocally copies the artifact staged under stagingTag into the local
// catalog. ref must already carry its origin (Remote set, Built false); the
// artifact is copied byte for byte and IngestVerbatim stamps the identity and
// origin annotations onto the local descriptor.
func cacheLocally(ctx context.Context, local localWriter, ref Reference, staging oras.ReadOnlyTarget) (ArtifactInfo, error) {
	return local.IngestVerbatim(ctx, staging, stagingTag, ref, oras.DefaultCopyOptions)
}

// LocalSolutionStore adapts a LocalCatalog to the SolutionFetcher's local
// store: it reads cached and built solutions and caches pulled ones.
type LocalSolutionStore struct {
	local *LocalCatalog
}

var _ localSource = (*LocalSolutionStore)(nil)

// NewLocalSolutionStore returns a solution store backed by local.
func NewLocalSolutionStore(local *LocalCatalog) *LocalSolutionStore {
	return &LocalSolutionStore{local: local}
}

// Find resolves a single artifact in the local catalog.
func (s *LocalSolutionStore) Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	return s.local.Find(ctx, query)
}

func (s *LocalSolutionStore) IngestVerbatim(ctx context.Context, src oras.ReadOnlyTarget, srcTag string, localRef Reference, copyOpts oras.CopyOptions) (ArtifactInfo, error) {
	return s.local.IngestVerbatim(ctx, src, srcTag, localRef, copyOpts)
}

func (s *LocalSolutionStore) fetchSolution(ctx context.Context, ref Reference) (*SolutionParts, error) {
	// Content is always FetchWithLayer's first return; only aux layers are requested.
	content, layers, info, err := s.local.FetchWithLayer(ctx, ref, MediaTypeSolutionBundle, MediaTypeSolutionLock)
	if err != nil {
		return nil, fmt.Errorf("fetching solution %s from local catalog: %w", ref, err)
	}
	served := ref
	if info.Reference.Version != nil {
		served.Version = info.Reference.Version
	}
	served.Digest = info.Digest
	return &SolutionParts{
		Content: content,
		Bundle:  layers[MediaTypeSolutionBundle],
		Lock:    layers[MediaTypeSolutionLock],
		Ref:     served,
	}, nil
}
