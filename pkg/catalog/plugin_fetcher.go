// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
)

// PlatformAwareCatalog extends Catalog with multi-platform image index support.
// Catalogs that store multi-platform artifacts (e.g. LocalCatalog) implement
// this interface to allow transparent platform-specific fetching.
type PlatformAwareCatalog interface {
	Catalog

	// FetchByPlatform fetches a plugin binary for the given platform,
	// transparently handling both single-platform manifests and
	// multi-platform image indexes.
	FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error)

	// ListPlatforms returns the platforms available for a multi-platform artifact.
	// Returns nil if the artifact is single-platform.
	ListPlatforms(ctx context.Context, ref Reference) ([]string, error)

	// ResolveContentDigest resolves the content-layer digest for a
	// platform-specific binary without downloading the blob. The mediaType
	// parameter selects which layer to read the digest from (e.g.
	// MediaTypeProviderBinary). For a multi-platform image index it selects
	// the platform-specific manifest; for a single-platform manifest the
	// platform argument is ignored.
	ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error)
}

// Option configures a PluginFetcher call.
type Option func(*fetcherOptions)

type fetcherOptions struct {
	catalog string
}

// WithCatalog restricts the operation to a single named catalog (explicit,
// no fallback). Empty is the default implicit/chain behavior.
func WithCatalog(name string) Option {
	return func(o *fetcherOptions) { o.catalog = name }
}

func applyOptions(opts []Option) fetcherOptions {
	var o fetcherOptions
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// PluginSource is the subset of catalog behavior plugin fetching needs. Find
// resolves exact versions, semver constraints and "latest" alike, so there is
// no separate Resolve.
type PluginSource interface {
	Name() string
	FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error)
	ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error)
	Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error)
}

// Compile-time interface assertions.
var (
	_ PluginSource = (*LocalCatalog)(nil)
	_ PluginSource = (*CachedRemoteCatalog)(nil)
	_ PluginSource = (*localPlugin)(nil)
	_ PluginSource = (*pluginSourceChain)(nil)
	_ PluginSource = (*allowlistPluginSource)(nil)
)

// wrappedPluginSource is implemented by PluginSource decorators so callers can
// reach the underlying catalog (e.g. to derive its identity).
type wrappedPluginSource interface {
	unwrapPluginSource() PluginSource
}

// PluginFetcher resolves and fetches plugin artifacts from a strict, ordered
// chain of plugin sources.
type PluginFetcher struct {
	chain     *pluginSourceChain // the ordered chain of every configured source
	byCatalog map[string]PluginSource
	logger    logr.Logger
}

// NewPluginFetcher creates a PluginFetcher over an ordered set of plugin
// sources: local (optional; built artifacts only) first, then remotes in
// priority order. Calls that name no catalog walk that chain; WithCatalog
// scopes a call to the one source with that name. Source names must be unique.
func NewPluginFetcher(logger logr.Logger, local *LocalCatalog, remotes ...PluginSource) (*PluginFetcher, error) {
	logger = logger.WithName("plugin-fetcher")
	chain := newPluginSourceChain(logger, newLocalPlugin(local), remotes...)

	byCatalog := make(map[string]PluginSource, len(chain.sources))
	for _, src := range chain.sources {
		name := src.Name()
		if _, dup := byCatalog[name]; dup {
			return nil, fmt.Errorf("duplicate plugin source %q", name)
		}
		byCatalog[name] = src
	}

	return &PluginFetcher{
		chain:     chain,
		byCatalog: byCatalog,
		logger:    logger,
	}, nil
}

// Catalogs returns the catalogs behind the configured sources, in priority
// order, with any plugin-source decorators removed. Sources that are not
// backed by a Catalog are omitted.
func (f *PluginFetcher) Catalogs() []Catalog {
	cats := make([]Catalog, 0, len(f.chain.sources))
	for _, src := range f.chain.sources {
		for {
			w, ok := src.(wrappedPluginSource)
			if !ok {
				break
			}
			src = w.unwrapPluginSource()
		}
		if cat, ok := src.(Catalog); ok {
			cats = append(cats, cat)
		}
	}
	return cats
}

// newPluginSourceChain orders local (when non-nil) ahead of remotes.
func newPluginSourceChain(logger logr.Logger, local *localPlugin, remotes ...PluginSource) *pluginSourceChain {
	sources := make([]PluginSource, 0, len(remotes)+1)
	if local != nil {
		sources = append(sources, local)
	}
	sources = append(sources, remotes...)
	return &pluginSourceChain{sources: sources, logger: logger.WithName("source-chain")}
}

// targetCatalog selects the source an operation should run against. An empty
// name uses the default source; a non-empty name selects that single catalog
// explicitly, and an unconfigured name is a hard error with no fallback.
func (f *PluginFetcher) targetCatalog(name string) (PluginSource, error) {
	if name == "" {
		return f.chain, nil
	}
	src, ok := f.byCatalog[name]
	if !ok {
		return nil, fmt.Errorf("catalog %q not configured", name)
	}
	return src, nil
}

// Find resolves query to a single plugin artifact, respecting the WithCatalog
// option to scope to a single configured catalog.
func (f *PluginFetcher) Find(ctx context.Context, query ArtifactQuery, opts ...Option) (ArtifactInfo, error) {
	o := applyOptions(opts)
	target, err := f.targetCatalog(o.catalog)
	if err != nil {
		return ArtifactInfo{}, err
	}
	info, err := target.Find(ctx, query)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("finding plugin %s: %w", query.String(), err)
	}
	return info, nil
}

// ResolvePlugin resolves a plugin by name, kind, and version selector (an
// exact version, a semver constraint, or ""/"latest"), returning the newest
// matching artifact.
func (f *PluginFetcher) ResolvePlugin(ctx context.Context, name string, kind ArtifactKind, versionConstraint string, opts ...Option) (ArtifactInfo, error) {
	o := applyOptions(opts)
	target, err := f.targetCatalog(o.catalog)
	if err != nil {
		return ArtifactInfo{}, err
	}

	info, err := target.Find(ctx, ArtifactQuery{Name: name, Kind: kind, Version: versionConstraint})
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("resolving plugin %s (%s): %w", name, kind, err)
	}

	f.logger.V(1).Info("resolved plugin",
		"name", name,
		"kind", kind,
		"version", info.Reference.Version,
		"catalog", info.Catalog)

	return info, nil
}

// FetchPlugin fetches a plugin binary for the given platform via the OCI image
// index. A platform-not-found error is returned as-is: the artifact is known to
// be multi-platform and the requested platform is unavailable.
func (f *PluginFetcher) FetchPlugin(ctx context.Context, name string, kind ArtifactKind, version, platform string, opts ...Option) ([]byte, ArtifactInfo, error) {
	o := applyOptions(opts)
	target, err := f.targetCatalog(o.catalog)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	ref, err := buildPluginRef(name, kind, version)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	data, info, err := target.FetchByPlatform(ctx, ref, platform)
	if err != nil {
		if IsPlatformNotFound(err) {
			return nil, ArtifactInfo{}, err
		}
		return nil, ArtifactInfo{}, fmt.Errorf("fetching plugin %s@%s (%s): %w", name, version, platform, err)
	}

	f.logger.V(1).Info("fetched plugin via image index",
		"name", name,
		"version", version,
		"platform", platform,
		"catalog", info.Catalog)

	return data, info, nil
}

// ResolveContentDigest resolves the content-layer digest for a plugin binary
// without downloading the blob.
func (f *PluginFetcher) ResolveContentDigest(ctx context.Context, name string, kind ArtifactKind, version, platform, mediaType string, opts ...Option) (ContentDigestInfo, error) {
	o := applyOptions(opts)
	target, err := f.targetCatalog(o.catalog)
	if err != nil {
		return ContentDigestInfo{}, err
	}

	ref, err := buildPluginRef(name, kind, version)
	if err != nil {
		return ContentDigestInfo{}, err
	}

	info, err := target.ResolveContentDigest(ctx, ref, platform, mediaType)
	if err != nil {
		return ContentDigestInfo{}, fmt.Errorf("resolving content digest for %s (%s): %w", name, kind, err)
	}

	f.logger.V(1).Info("resolved content digest",
		"name", name,
		"kind", kind,
		"version", version,
		"platform", platform,
		"contentDigest", info.ContentDigest)

	return info, nil
}

// buildPluginRef constructs a Reference from name, kind, and version string.
func buildPluginRef(name string, kind ArtifactKind, version string) (Reference, error) {
	refStr := name
	if version != "" {
		refStr = name + "@" + version
	}
	ref, err := ParseReference(kind, refStr)
	if err != nil {
		return Reference{}, fmt.Errorf("invalid plugin reference %q: %w", refStr, err)
	}
	return ref, nil
}

// localPlugin restricts a local catalog to built artifacts, so cached copies
// of remote artifacts are served only through their CachedRemoteCatalog. The
// source is a named field, not embedded, so every pluginSource method must be
// wrapped explicitly and none can bypass the built-only filter. A ref or query
// that asks for a remote's artifact is not-found here rather than narrowed to
// built, so a local build never stands in for a requested remote.
type localPlugin struct {
	source PluginSource
}

// newLocalPlugin wraps local; a nil local yields a nil *localPlugin, which
// NewPluginFetcher treats as "no local source".
func newLocalPlugin(local *LocalCatalog) *localPlugin {
	if local == nil {
		return nil
	}
	return &localPlugin{source: local}
}

func (l *localPlugin) Name() string {
	return l.source.Name()
}

func (l *localPlugin) unwrapPluginSource() PluginSource {
	return l.source
}

func (l *localPlugin) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	if ref.Remote != "" {
		return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: l.Name()}
	}
	return l.source.FetchByPlatform(ctx, sanitizeRefForBuilt(ref), platform)
}

func (l *localPlugin) ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error) {
	if ref.Remote != "" {
		return ContentDigestInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: l.Name()}
	}
	return l.source.ResolveContentDigest(ctx, sanitizeRefForBuilt(ref), platform, mediaType)
}

func (l *localPlugin) Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	if query.Source.Kind == SourceRemote {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: Reference{Kind: query.Kind, Name: query.Name}, Catalog: l.Name()}
	}
	query.Source = SourceSelector{Kind: SourceBuilt}
	return l.source.Find(ctx, query)
}

// pluginSourceChain tries each source in priority order. It is always strict
// (see ChainModeStrict): it moves past a source only when that source
// definitively reports not-found, so a broken or unreachable higher-priority
// source is never silently substituted by a lower-priority one. A
// PlatformNotFoundError halts the chain immediately and is returned as-is.
type pluginSourceChain struct {
	sources []PluginSource
	logger  logr.Logger
}

func (c *pluginSourceChain) Name() string {
	return "chain"
}

func (c *pluginSourceChain) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	type fetchResult struct {
		data []byte
		info ArtifactInfo
	}
	subject := fmt.Sprintf("%s (%s)", ref.String(), platform)
	r, err := walkPluginSources(c, subject, func(src PluginSource) (fetchResult, error) {
		data, info, err := src.FetchByPlatform(ctx, ref, platform)
		return fetchResult{data: data, info: info}, err
	})
	return r.data, r.info, err
}

func (c *pluginSourceChain) ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error) {
	subject := fmt.Sprintf("%s (%s)", ref.String(), platform)
	return walkPluginSources(c, subject, func(src PluginSource) (ContentDigestInfo, error) {
		return src.ResolveContentDigest(ctx, ref, platform, mediaType)
	})
}

func (c *pluginSourceChain) Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	return walkPluginSources(c, query.String(), func(src PluginSource) (ArtifactInfo, error) {
		return src.Find(ctx, query)
	})
}

// walkPluginSources runs action against each source in order and returns the
// first success. A PlatformNotFoundError, or any other error that is not a
// definitive not-found, halts the chain; the former is returned wrapped with
// the subject and source. The final error
// is built by aggregateChainError, so an all-miss chain still satisfies
// errors.Is(err, ErrArtifactNotFound) and an unreachable source is reported
// as such.
func walkPluginSources[T any](c *pluginSourceChain, subject string, action func(PluginSource) (T, error)) (T, error) {
	var zero T
	outcomes := make([]catalogOutcome, 0, len(c.sources))
	for _, src := range c.sources {
		result, err := action(src)
		if err == nil {
			c.logger.V(1).Info("plugin source resolved", "source", src.Name(), "subject", subject)
			return result, nil
		}
		if IsPlatformNotFound(err) {
			return zero, fmt.Errorf("%s from %s: %w", subject, src.Name(), err)
		}
		outcomes = append(outcomes, catalogOutcome{catalog: src.Name(), err: err})
		if !errors.Is(err, ErrArtifactNotFound) {
			c.logger.V(1).Info("plugin source error (non-404), halting", "source", src.Name(), "subject", subject, "error", err)
			return zero, aggregateChainError(subject, outcomes)
		}
	}
	return zero, aggregateChainError(subject, outcomes)
}
