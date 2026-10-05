// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// CatalogAliases translates between configured catalog aliases and their
// canonical "registry[/repository]" origins. *catalogindex.Index satisfies it.
type CatalogAliases interface {
	RegistryForAlias(alias string) (string, bool)
	AliasForRegistry(registry string) (string, bool)
}

// SolutionFetcher fetches a solution artifact's parts. *catalog.SolutionFetcher
// satisfies it.
type SolutionFetcher interface {
	FetchSolution(ctx context.Context, ref catalog.Reference, opts ...catalog.FetchOption) (*catalog.SolutionParts, error)
}

// CatalogLoader loads solutions from a catalog or remote registry.
// Supported kinds: KindRemoteRef, KindCatalogQualified, KindBareName.
type CatalogLoader struct {
	catalogs CatalogAliases
	fetcher  SolutionFetcher
	lenient  bool
	noCache  bool
}

var _ Loader = (*CatalogLoader)(nil)

// CatalogOption configures a CatalogLoader.
type CatalogOption interface {
	applyCatalog(*CatalogLoader)
}

// NoCacheOption makes catalog loads bypass the local cache of remote
// artifacts (see catalog.WithNoCache): remote refs are always pulled from
// their registry and the pulled artifact is not cached. Local builds are
// unaffected. It is accepted by NewCatalogLoader and NewRouter.
type NoCacheOption struct{}

var (
	_ CatalogOption = NoCacheOption{}
	_ RouterOption  = NoCacheOption{}
)

// WithNoCache returns a NoCacheOption.
func WithNoCache() NoCacheOption {
	return NoCacheOption{}
}

func (NoCacheOption) applyCatalog(l *CatalogLoader) { l.noCache = true }

func (NoCacheOption) applyRouter(c *routerConfig) { c.noCache = true }

// NewCatalogLoader returns a CatalogLoader that translates catalog aliases
// with catalogs and fetches solutions with fetcher. A nil catalogs resolves no
// aliases except "local".
func NewCatalogLoader(catalogs CatalogAliases, fetcher SolutionFetcher, opts ...CatalogOption) *CatalogLoader {
	l := &CatalogLoader{catalogs: catalogs, fetcher: fetcher}
	for _, opt := range opts {
		opt.applyCatalog(l)
	}
	return l
}

// Load fetches and parses the solution referenced by c. A version constraint
// in c.Version is passed to the fetcher to select among the ref's versions.
// WithNoCache makes the fetch bypass the local cache of remote artifacts.
func (l *CatalogLoader) Load(ctx context.Context, c source.Classification) (*MaterializedSolution, error) {
	ref, err := l.reference(c)
	if err != nil {
		return nil, err
	}
	if l.fetcher == nil {
		return nil, fmt.Errorf("catalog loader %q: no solution fetcher configured", c.Ref)
	}

	fetchOpts := []catalog.FetchOption{catalog.WithVersionConstraint(c.Version)}
	if l.noCache {
		fetchOpts = append(fetchOpts, catalog.WithNoCache())
	}

	parts, err := l.fetcher.FetchSolution(ctx, ref, fetchOpts...)
	if err != nil {
		return nil, fmt.Errorf("fetching solution %q: %w", c.Ref, err)
	}

	sol := &solution.Solution{}
	if err := parseSolution(sol, parts.Content, l.lenient); err != nil {
		return nil, fmt.Errorf("parsing solution %q: %w", c.Ref, err)
	}
	sol.SetPath(l.legacyPath(c, ref, parts))
	served := parts.Ref
	return &MaterializedSolution{
		Solution: sol,
		Bundle:   parts.Bundle,
		Lock:     parts.Lock,
		Source:   c,
		Served:   &served,
		Pulled:   parts.Pulled,
	}, nil
}

// Legacy Getter path labels. Downstream code (prepare, run, inspect) still
// keys off these prefixes; remove once callers switch to Source.Kind.
const (
	legacyCatalogPathPrefix = "catalog:"
	legacyRemotePathPrefix  = "remote:"
)

// legacyPath reproduces the path label the Getter set for catalog sources,
// based on where the artifact was served from:
//   - a remote ref pulled from its registry: "remote:<ref>";
//   - a remote ref served from the local catalog: "catalog:local:<name>[@version]";
//   - a bare or catalog-qualified name: "catalog:<serving catalog>:<ref>".
//
// requested is the reference that was fetched. Kinds the CatalogLoader does
// not serve yield "".
func (l *CatalogLoader) legacyPath(c source.Classification, requested catalog.Reference, parts *catalog.SolutionParts) string {
	switch c.Kind {
	case source.KindRemoteRef:
		if parts.Pulled {
			return legacyRemotePathPrefix + c.Ref
		}
		// TODO: revisit labelling a cached copy of a remote artifact as
		// "catalog:local:...". It mirrors the getter, but the artifact's origin
		// is still the remote (parts.Ref.Remote), so this may be the wrong way to
		// describe a pulled-and-cached artifact. Consider a distinct label (or
		// drop labels for Source.Kind/Served) once callers stop keying off them.
		return legacyCatalogPath(catalog.LocalCatalogName, requested.String())
	case source.KindCatalogQualified, source.KindBareName:
		return legacyCatalogPath(l.servingCatalog(parts), c.Ref)
	case source.KindUnknown, source.KindAutoDiscover, source.KindStdin, source.KindURL, source.KindLocalFile:
	}
	return ""
}

func legacyCatalogPath(catalogName, ref string) string {
	return legacyCatalogPathPrefix + catalogName + ":" + ref
}

// servingCatalog names the catalog parts were served from: "local" unless
// they were pulled, else the alias configured for the served remote (its
// canonical origin when no alias is configured).
func (l *CatalogLoader) servingCatalog(parts *catalog.SolutionParts) string {
	// TODO: same open question as the KindRemoteRef case in legacyPath -- a
	// cached copy of a remote artifact (Remote set, not Pulled) is labelled
	// "local" here too.
	if !parts.Pulled || parts.Ref.Remote == "" {
		return catalog.LocalCatalogName
	}
	if l.catalogs != nil {
		if alias, ok := l.catalogs.AliasForRegistry(parts.Ref.Remote); ok {
			return alias
		}
	}
	return parts.Ref.Remote
}

// reference turns a classified catalog source into a solution reference.
func (l *CatalogLoader) reference(c source.Classification) (catalog.Reference, error) {
	switch c.Kind {
	case source.KindRemoteRef:
		return remoteReference(c.Ref) // sets Remote
	case source.KindBareName:
		return bareNameReference(c.Ref) // no Remote, not Built
	case source.KindCatalogQualified:
		return l.catalogQualifiedReference(c.Catalog, c.Ref) // "local" sets Built; an alias sets Remote
	case source.KindUnknown, source.KindAutoDiscover, source.KindStdin, source.KindURL, source.KindLocalFile:
	}
	return catalog.Reference{}, fmt.Errorf("catalog loader: %w: %s", ErrUnsupportedKind, c.Kind)
}

// remoteReference parses a registry reference such as
// "ghcr.io/org/name@1.0.0" or "oci://ghcr.io/org/solutions/name@sha256:..."
// into a solution catalog.Reference. Remote is the canonical
// "registry/repository" identity; an absent version or "latest" leaves
// Version nil. References to non-solution artifacts are rejected.
func remoteReference(raw string) (catalog.Reference, error) {
	rr, err := catalog.ParseRemoteReference(raw)
	if err != nil {
		return catalog.Reference{}, fmt.Errorf("%w %q: %w", source.ErrInvalidRef, raw, err)
	}
	if rr.Kind != "" && rr.Kind != catalog.ArtifactKindSolution {
		return catalog.Reference{}, fmt.Errorf("%w %q: reference is a %s, not a %s",
			source.ErrInvalidRef, raw, rr.Kind, catalog.ArtifactKindSolution)
	}

	ref, err := rr.ToReference()
	if err != nil {
		return catalog.Reference{}, fmt.Errorf("%w %q: %w", source.ErrInvalidRef, raw, err)
	}
	ref.Kind = catalog.ArtifactKindSolution

	if !catalog.IsValidName(ref.Name) {
		return catalog.Reference{}, fmt.Errorf("%w %q: name %q must be lowercase alphanumeric with hyphens",
			source.ErrInvalidRef, raw, ref.Name)
	}
	return ref, nil
}

// bareNameReference parses an unqualified reference such as "name",
// "name@1.0.0" or "name@sha256:..." into a solution catalog.Reference.
// Remote is left empty so the origin is resolved later; an absent version or
// "latest" leaves Version nil.
func bareNameReference(raw string) (catalog.Reference, error) {
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, raw)
	if err != nil {
		return catalog.Reference{}, fmt.Errorf("%w %q: %w", source.ErrInvalidRef, raw, err)
	}
	return ref, nil
}

// catalogQualifiedReference parses the bare name raw ("<name>[@version]") and
// scopes it to the catalog alias. The alias is translated to its registry
// origin, which becomes Remote; the reserved "local" alias sets Built instead.
// An alias naming no configured remote catalog yields
// catalogindex.ErrUnknownCatalog.
func (l *CatalogLoader) catalogQualifiedReference(alias, raw string) (catalog.Reference, error) {
	if alias == "" {
		return catalog.Reference{}, fmt.Errorf("%w %q: catalog-qualified reference requires a catalog", source.ErrInvalidRef, raw)
	}

	ref, err := bareNameReference(raw)
	if err != nil {
		return catalog.Reference{}, err
	}

	if strings.EqualFold(alias, config.CatalogNameLocal) {
		ref.Built = true
		return ref, nil
	}

	var registry string
	found := false
	if l.catalogs != nil {
		registry, found = l.catalogs.RegistryForAlias(alias)
	}
	if !found {
		return catalog.Reference{}, fmt.Errorf("%w %q for reference %q", catalogindex.ErrUnknownCatalog, alias, raw)
	}
	ref.Remote = registry
	return ref, nil
}
