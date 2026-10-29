// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// deleteReferenceBuild captures the parsed delete target: which artifact to
// delete and which local origin it must come from.
type deleteReferenceBuild struct {
	Reference catalog.Reference
	Source    catalog.SourceSelector

	// Registry and Repository locate the remote catalog to delete from. They are
	// set only when --catalog is given (see resolveCatalogLocation).
	Registry   string
	Repository string
}

// deleteReferenceBuilder incrementally derives the delete target from the
// command options. Each step mutates the builder and returns it so callers can
// compose the parse flow as a chain; the first error short-circuits later steps.
type deleteReferenceBuilder struct {
	ctx   context.Context
	opts  *DeleteOptions
	build deleteReferenceBuild
	err   error
}

func newDeleteReferenceBuilder(ctx context.Context, opts *DeleteOptions) *deleteReferenceBuilder {
	return &deleteReferenceBuilder{ctx: ctx, opts: opts}
}

func (b *deleteReferenceBuilder) parseReference() *deleteReferenceBuilder {
	if b.err != nil {
		return b
	}

	var kind catalog.ArtifactKind
	if b.opts.Kind != "" {
		kind, _ = catalog.ParseArtifactKind(b.opts.Kind)
	}

	ref, err := catalog.ParseReference(kind, b.opts.Reference)
	if err != nil {
		b.err = fmt.Errorf("invalid reference: %w", err)
		return b
	}
	// Deletion needs an exact version: "latest" and digest-only references
	// parse without a semver version and would not identify one artifact.
	if ref.Version == nil {
		b.err = errors.New(deleteVersionRequiredMsg)
		return b
	}
	b.build.Reference = ref

	return b
}

func (b *deleteReferenceBuilder) parseOrigin() *deleteReferenceBuilder {
	if b.err != nil {
		return b
	}

	source, err := catalog.ParseSourceSelector(b.opts.Origin)
	if err != nil {
		b.err = fmt.Errorf("invalid --origin: %w", err)
		return b
	}
	b.build.Source = source

	// An explicit origin fully specifies the reference. With no origin the
	// reference stays open until it is resolved against the catalog.
	switch source.Kind {
	case catalog.SourceAny:
		// no-op
	case catalog.SourceBuilt:
		b.build.Reference.Built = true
	case catalog.SourceRemote:
		b.build.Reference.Remote = source.Remote
	}

	return b
}

// resolveCatalogLocation resolves --catalog (a URL or configured catalog name)
// to the registry and repository to delete from. It is a no-op for local deletes.
func (b *deleteReferenceBuilder) resolveCatalogLocation() *deleteReferenceBuilder {
	if b.err != nil || b.opts.Catalog == "" {
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
	}

	return b
}

func (b *deleteReferenceBuilder) Build() (deleteReferenceBuild, error) {
	if b.err != nil {
		return deleteReferenceBuild{}, b.err
	}
	return b.build, nil
}
