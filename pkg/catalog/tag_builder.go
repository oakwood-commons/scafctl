// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"io"
	"maps"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type Pusher interface {
	Push(ctx context.Context, desc ocispec.Descriptor, reader io.Reader) error
}

// Tagger assigns a tag to a pushed descriptor. oras stores and repositories
// both satisfy it.
type Tagger interface {
	Tag(ctx context.Context, desc ocispec.Descriptor, tag string) error
}

// TargetWriter combines Pusher and Tagger for destinations that receive pushed blobs and tags.
type TargetWriter interface {
	Pusher
	Tagger
}

// TagBuilder tags the root descriptor (manifest or index) returned by
// ManifestBuilder.Push or IndexBuilder.Push. Remote pushes use a plain tag:
//
//	desc, err := NewManifestBuilder(ref)....Push(ctx, repo)
//	desc, err = NewTagBuilder(version).Build(ctx, desc, repo)
//
// The local catalog derives both the tag and the provenance from the ref:
//
//	tagger, err := NewLocalTagBuilder(ref)
//	desc, err = tagger.Build(ctx, desc, store)
//
// Annotations set here live on the tagged descriptor only (index.json for a
// local store), not in the manifest blob, so descriptor-only keys such as
// AnnotationSourceCanonical never affect the digest. The descriptor's
// existing annotations pass through the transformer first, so keys can be
// dropped as well as added.
type TagBuilder struct {
	tag         string
	ref         *Reference
	annotations map[string]string
	transform   AnnotationTransformer
}

// NewTagBuilder starts a tag chain for the given tag. No provenance is
// recorded; use it for destinations that format their own tags (remote
// repositories).
func NewTagBuilder(tag string) *TagBuilder {
	return &TagBuilder{
		tag:         tag,
		annotations: make(map[string]string),
		transform:   copyAllAnnotations,
	}
}

// NewLocalTagBuilder starts a tag chain for the local catalog. The tag is
// derived from ref, and Build stamps ref's identity and provenance (canonical
// set for a remote ref, removed for a built one) onto the descriptor, after
// any caller annotations, so they cannot be overridden.
func NewLocalTagBuilder(ref Reference) (*TagBuilder, error) {
	tag, err := tagForLocalRef(ref)
	if err != nil {
		return nil, err
	}
	b := NewTagBuilder(tag)
	b.ref = &ref
	return b, nil
}

// Tag returns the tag Build writes.
func (b *TagBuilder) Tag() string {
	return b.tag
}

// Annotation sets a single descriptor annotation. Empty values are skipped.
func (b *TagBuilder) Annotation(key, value string) *TagBuilder {
	if value != "" {
		b.annotations[key] = value
	}
	return b
}

// Annotations merges descriptor annotations. Empty values are skipped.
func (b *TagBuilder) Annotations(annotations map[string]string) *TagBuilder {
	for k, v := range annotations {
		b.Annotation(k, v)
	}
	return b
}

// AnnotationTransformer configures how the descriptor's existing annotations
// are filtered before the builder's annotations are merged over them. A nil
// transformer restores the default copy-all behavior.
func (b *TagBuilder) AnnotationTransformer(transformer AnnotationTransformer) *TagBuilder {
	if transformer == nil {
		b.transform = copyAllAnnotations
	} else {
		b.transform = transformer
	}
	return b
}

// Build tags desc with the configured tag and returns the descriptor as
// tagged, annotations included, so callers need not resolve it again.
func (b *TagBuilder) Build(ctx context.Context, desc ocispec.Descriptor, tagger Tagger) (ocispec.Descriptor, error) {
	if b.tag == "" {
		return ocispec.Descriptor{}, &TagError{Reason: "tag is empty"}
	}
	if tagger == nil {
		return ocispec.Descriptor{}, &TagError{Reason: "no tagger"}
	}

	anns := b.transform(copyAnnotations(desc.Annotations))
	if anns == nil {
		anns = make(map[string]string, len(b.annotations))
	}
	maps.Copy(anns, b.annotations)
	if b.ref != nil {
		anns = localProvenance(*b.ref, anns)
	}
	desc.Annotations = anns

	if err := tagger.Tag(ctx, desc, b.tag); err != nil {
		return ocispec.Descriptor{}, &TagError{Tag: b.tag, Cause: err}
	}
	return desc, nil
}
