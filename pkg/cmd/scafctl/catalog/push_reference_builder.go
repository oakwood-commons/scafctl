// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// pushDigestUnsupportedMsg is reported when a push reference pins a digest.
// A digest names content, not a publishable version, so there is no version
// tag to create in the destination.
const pushDigestUnsupportedMsg = "digest references cannot be pushed: use 'name@version'"

// pushReferenceBuild captures the parsed push request: which local artifact to
// push, and the remote catalog to push it to.
type pushReferenceBuild struct {
	// Reference identifies the local artifact: kind, name, optional version and,
	// when --origin is given, its origin. With a version and an origin the
	// identity is complete and the local catalog answers by tag lookup; otherwise
	// it is resolved by search (latest version, ambiguity across origins).
	Reference catalog.Reference

	// Target is the artifact's identity in the destination when --as renames it
	// (name and version). Its Name is empty when there is no rename.
	Target catalog.Reference

	// Registry and Repository locate the destination catalog.
	Registry   string
	Repository string
}

// remoteRef returns the artifact as it is named in the destination: the --as
// target when renamed, otherwise the local reference unchanged.
func (p pushReferenceBuild) remoteRef(local catalog.Reference) catalog.Reference {
	if p.Target.Name != "" {
		return p.Target
	}
	return local
}

// pushReferenceBuilder incrementally derives the push request from the command
// options. Each step mutates the builder and returns it so callers can compose
// the parse flow as a chain; the first error short-circuits later steps.
type pushReferenceBuilder struct {
	ctx   context.Context
	opts  *pushKindOptions
	build pushReferenceBuild
	err   error
}

func newPushReferenceBuilder(ctx context.Context, opts *pushKindOptions) *pushReferenceBuilder {
	return &pushReferenceBuilder{ctx: ctx, opts: opts}
}

// parseShortReference parses a short reference (kind subcommands). The kind
// comes from the subcommand; the version is optional and defaults to the
// latest local version.
func (b *pushReferenceBuilder) parseShortReference() *pushReferenceBuilder {
	if b.err != nil {
		return b
	}

	var kind catalog.ArtifactKind
	if b.opts.Kind != "" {
		kind, _ = catalog.ParseArtifactKind(b.opts.Kind)
	}

	name, version := catalog.ParseNameVersion(b.opts.Reference)
	ref := catalog.Reference{Kind: kind, Name: name}
	if version != "" {
		var err error
		ref, err = catalog.ParseReference(kind, b.opts.Reference)
		if err != nil {
			b.err = fmt.Errorf("invalid reference: %w", err)
			return b
		}
	}
	b.build.Reference = ref

	return b
}

// parseRemoteReference splits a full remote reference (root push) into the
// local artifact identity (kind, name, optional version) and the destination
// (registry and repository). The kind comes from the reference path.
func (b *pushReferenceBuilder) parseRemoteReference() *pushReferenceBuilder {
	if b.err != nil {
		return b
	}

	remoteRef, err := catalog.ParseRemoteReference(b.opts.Reference)
	if err != nil {
		b.err = fmt.Errorf("invalid reference: %w", err)
		return b
	}

	ref, err := remoteRef.ToReference()
	if err != nil {
		b.err = fmt.Errorf("invalid reference: %w", err)
		return b
	}

	// ToReference stamps Remote with the destination. On push, Reference.Remote
	// means "the origin of the local copy", which only --origin may set.
	ref.Remote = ""

	b.build.Reference = ref
	b.build.Registry = remoteRef.Registry
	b.build.Repository = remoteRef.Repository

	return b
}

// rejectDigest refuses digest-pinned references.
func (b *pushReferenceBuilder) rejectDigest() *pushReferenceBuilder {
	if b.err != nil {
		return b
	}
	if b.build.Reference.HasDigest() {
		b.err = errors.New(pushDigestUnsupportedMsg)
	}
	return b
}

// parseOrigin parses --origin and marks the reference as built or as pulled
// from that registry, picking among the local copies of the artifact. With no
// --origin the reference stays open and is resolved by search.
func (b *pushReferenceBuilder) parseOrigin() *pushReferenceBuilder {
	if b.err != nil {
		return b
	}

	source, err := catalog.ParseSourceSelector(b.opts.Origin)
	if err != nil {
		b.err = fmt.Errorf("invalid --origin: %w", err)
		return b
	}

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

// parseTarget validates --as. A rename must be a concrete name@version because
// the published manifest is rebuilt with that identity.
func (b *pushReferenceBuilder) parseTarget() *pushReferenceBuilder {
	if b.err != nil || b.opts.TargetName == "" {
		return b
	}

	target, err := catalog.ResolveLocalTarget(b.build.Reference, b.opts.TargetName)
	if err != nil {
		b.err = err
		return b
	}
	b.build.Target = target

	return b
}

// resolveCatalogLocation resolves --catalog (a URL or configured catalog name,
// else the default catalog) to the destination registry and repository.
func (b *pushReferenceBuilder) resolveCatalogLocation() *pushReferenceBuilder {
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
	}

	return b
}

func (b *pushReferenceBuilder) Build() (pushReferenceBuild, error) {
	if b.err != nil {
		return pushReferenceBuild{}, b.err
	}
	return b.build, nil
}
