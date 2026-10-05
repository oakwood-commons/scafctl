// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// pullReferenceBuild captures the candidate remote reference computed from a
// short pull input before registry-side resolution fills in the concrete
// version or digest to fetch.
type pullReferenceBuild struct {
	Reference  catalog.Reference
	Registry   string
	Repository string
	Tag        string
}

// pullReferenceBuilder incrementally derives the candidate reference for the
// short-name pull path. Each step mutates the builder and returns it so callers
// can compose the parse/resolution flow as a chain.
type pullReferenceBuilder struct {
	ctx   context.Context
	opts  *PullOptions
	build pullReferenceBuild
	err   error
}

func newPullReferenceBuilder(ctx context.Context, opts *PullOptions) *pullReferenceBuilder {
	return &pullReferenceBuilder{ctx: ctx, opts: opts}
}

func (b *pullReferenceBuilder) parseShortReference() *pullReferenceBuilder {
	if b.err != nil {
		return b
	}

	name, version := catalog.ParseNameVersion(b.opts.Reference)
	b.build.Tag = version

	var artifactKind catalog.ArtifactKind
	if b.opts.Kind != "" {
		artifactKind, _ = catalog.ParseArtifactKind(b.opts.Kind)
	}

	b.build.Reference = catalog.Reference{
		Kind: artifactKind,
		Name: name,
	}

	if version == "" {
		return b
	}

	parsedRef, err := catalog.ParseReference(b.build.Reference.Kind, b.opts.Reference)
	if err != nil {
		b.err = fmt.Errorf("invalid reference: %w", err)
		return b
	}
	b.build.Reference = parsedRef
	return b
}

func (b *pullReferenceBuilder) parseRemoteReference() *pullReferenceBuilder {
	if b.err != nil {
		return b
	}

	remoteRef, err := catalog.ParseRemoteReference(b.opts.Reference)
	if err != nil {
		b.err = fmt.Errorf("invalid reference: %w", err)
		return b
	}

	if b.opts.Catalog != "" {
		b.err = ErrCatalogWithRemoteReference
		return b
	}

	ref, err := remoteRef.ToReference()
	if err != nil {
		b.err = fmt.Errorf("invalid reference: %w", err)
		return b
	}

	b.build.Reference = ref
	b.build.Registry = remoteRef.Registry
	b.build.Repository = remoteRef.Repository
	b.build.Tag = remoteRef.Tag

	return b
}

func (b *pullReferenceBuilder) resolveCatalogLocation() *pullReferenceBuilder {
	if b.err != nil {
		return b
	}

	catalogURL, err := catalog.ResolveCatalogURL(b.ctx, b.opts.Catalog)
	if err != nil {
		b.err = err
		return b
	}

	b.build.Registry, b.build.Repository = catalog.ParseCatalogURL(catalogURL)
	if b.build.Registry == "" {
		b.err = fmt.Errorf("invalid catalog URL: %s", catalogURL)
		return b
	}
	b.build.Reference.Remote = catalog.CanonicalRemoteCatalogID(b.build.Registry, b.build.Repository)

	return b
}

func (b *pullReferenceBuilder) Build() (pullReferenceBuild, error) {
	if b.err != nil {
		return pullReferenceBuild{}, b.err
	}
	return b.build, nil
}
