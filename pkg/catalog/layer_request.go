// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// LayerRequest selects layers from an artifact's manifest(s) and is generic
// over the shape of its result. It is implemented by ManifestLayers (a single
// image manifest, yielding FetchedLayers). The interface is sealed: only this
// package defines requests.
//
// TODO: reimplement an image-index request (multi-platform, e.g. provider
// binaries) when a caller needs it.
//
// Fetch reads the manifest (and index) behind root and returns lazy handles for
// the matching layers. Layer content is only transferred when a handle is
// opened, so selecting a layer from a large artifact costs a manifest read.
type LayerRequest[R any] interface {
	Fetch(ctx context.Context, fetcher content.Fetcher, root ocispec.Descriptor) (R, error)
	isLayerRequest()
}

var _ LayerRequest[FetchedLayers] = (*ManifestLayers)(nil)

// FetchLayers runs req against root. It is a function rather than a method
// because Go methods cannot be generic; the result type follows from the
// request type:
//
//	layers, err := FetchLayers(ctx, fetcher, root, NewManifestLayers(MediaTypeSolutionContent))
func FetchLayers[R any](ctx context.Context, fetcher content.Fetcher, root ocispec.Descriptor, req LayerRequest[R]) (R, error) {
	var zero R
	if req == nil {
		return zero, errors.New("fetching layers: nil request")
	}

	result, err := req.Fetch(ctx, fetcher, root)
	if err != nil {
		return zero, err
	}
	return result, nil
}

// FetchedLayer is one selected layer of one manifest.
type FetchedLayer struct {
	// Descriptor identifies the layer blob.
	Descriptor ocispec.Descriptor

	open func(context.Context) (io.ReadCloser, error)
}

// Open returns a reader for the layer content. The advertised size is bounded
// before any bytes are requested. The caller must close the reader.
func (l FetchedLayer) Open(ctx context.Context) (io.ReadCloser, error) {
	if l.open == nil {
		return nil, fmt.Errorf("opening layer %s: no content source", l.Descriptor.Digest)
	}
	return l.open(ctx)
}

// Bytes reads the whole layer into memory, verifying size and digest against
// the descriptor. Prefer Open for layers that are not known to be small.
func (l FetchedLayer) Bytes(ctx context.Context) ([]byte, error) {
	rc, err := l.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return readVerifiedLayer(rc, l.Descriptor)
}

// FetchedLayers is the selected layers of a single manifest, in manifest order,
// together with the manifest they came from. It is the whole result of a
// ManifestLayers request.
//
// The manifest is read during selection anyway, so carrying it is free. It
// matters because a request selects layers by media type up front, while some
// artifacts -- a deduplicated bundle, whose file index records absolute layer
// positions -- only decide which layers to read after their content has been
// read. Those callers need the unselected layers too, so a handle is built for
// every layer of the manifest and the request records only which of them
// matched. Retaining the content source instead would let a FetchedLayers mint
// a handle for any descriptor at all, including one belonging to a different
// artifact.
//
// A FetchedLayers is immutable once returned and is safe to share across
// goroutines. Opening a handle produces an independent reader per call.
type FetchedLayers struct {
	manifest ocispec.Manifest

	// allLayers holds a handle for every layer of the manifest, in manifest
	// order. selectedLayers is the subset the request asked for.
	allLayers      []FetchedLayer
	selectedLayers []FetchedLayer
}

// Manifest returns the image manifest the layers were selected from, including
// the layers the request did not select.
func (l FetchedLayers) Manifest() ocispec.Manifest {
	return l.manifest
}

// All returns the selected layers in manifest order.
func (l FetchedLayers) All() []FetchedLayer {
	return l.selectedLayers
}

// Len reports how many layers were selected.
func (l FetchedLayers) Len() int {
	return len(l.selectedLayers)
}

// ByMediaType groups the selected layers by media type. The groups are plain
// slices: they all come from the manifest this FetchedLayers already carries,
// so there is nothing extra to attach to them.
func (l FetchedLayers) ByMediaType() map[string][]FetchedLayer {
	grouped := make(map[string][]FetchedLayer)
	for _, layer := range l.selectedLayers {
		grouped[layer.Descriptor.MediaType] = append(grouped[layer.Descriptor.MediaType], layer)
	}
	return grouped
}

// Find returns the selected layer carrying mediaType, reporting whether one was
// found. It addresses scafctl's first-class artifact parts -- the solution
// file, the bundle, the lock, a provider binary -- which an artifact carries at
// most once each, so a media type names exactly one layer. That is a scafctl
// invariant, not an OCI one: a manifest's layer list is an ordered array and
// may repeat a media type freely, which is why the deduplicated per-file bundle
// blobs are not addressable this way. They are reached through the bundle
// manifest's index, by absolute position.
func (l FetchedLayers) Find(mediaType string) (FetchedLayer, bool) {
	for _, layer := range l.selectedLayers {
		if layer.Descriptor.MediaType == mediaType {
			return layer, true
		}
	}
	return FetchedLayer{}, false
}

// handle returns the handle for any layer of the manifest, including layers the
// request did not select. It exists for content that addresses layers by
// position rather than by media type, such as a deduplicated bundle's file
// index, and is unexported because such positions are only meaningful to the
// format that recorded them.
//
// It is a lookup, not a constructor: desc must name a layer of this manifest,
// so a FetchedLayers can only ever reach the layers it was selected from.
func (l FetchedLayers) handle(desc ocispec.Descriptor) (FetchedLayer, error) {
	for _, layer := range l.allLayers {
		if layer.Descriptor.Digest == desc.Digest {
			return layer, nil
		}
	}
	return FetchedLayer{}, fmt.Errorf("layer %s is not in the manifest", desc.Digest)
}

// ManifestLayers selects layers from a single image manifest:
//
//	layers, err := FetchLayers(ctx, fetcher, root, NewManifestLayers(MediaTypeSolutionContent))
type ManifestLayers struct {
	required   []string
	optional   []string
	onProgress func(ocispec.Descriptor)
}

// NewManifestLayers starts a request for layers having any of the required media types.
func NewManifestLayers(required ...string) *ManifestLayers {
	return &ManifestLayers{required: slices.Clone(required)}
}

// Optional adds media types that will be selected if present in the manifest,
// without causing Fetch to fail if they are missing.
func (r *ManifestLayers) Optional(mediaTypes ...string) *ManifestLayers {
	r.optional = append(r.optional, mediaTypes...)
	return r
}

func (r *ManifestLayers) allMediaTypes() []string {
	all := append([]string(nil), r.required...)
	for _, mt := range r.optional {
		if !slices.Contains(all, mt) {
			all = append(all, mt)
		}
	}
	return all
}

// OnProgress sets a callback invoked as each selected layer is opened.
func (r *ManifestLayers) OnProgress(fn func(ocispec.Descriptor)) *ManifestLayers {
	r.onProgress = fn
	return r
}

func (*ManifestLayers) isLayerRequest() {}

// Fetch implements LayerRequest. root must be an image manifest.
func (r *ManifestLayers) Fetch(ctx context.Context, fetcher content.Fetcher, root ocispec.Descriptor) (FetchedLayers, error) {
	allMediaTypes := r.allMediaTypes()
	if err := validateMediaTypes(allMediaTypes); err != nil {
		return FetchedLayers{}, err
	}
	if root.MediaType != ocispec.MediaTypeImageManifest {
		return FetchedLayers{}, fmt.Errorf("fetching layers: artifact %s is %q, expected %q", root.Digest, root.MediaType, ocispec.MediaTypeImageManifest)
	}

	layers, err := selectManifestLayers(ctx, fetcher, root, allMediaTypes, r.onProgress)
	if err != nil {
		return FetchedLayers{}, err
	}
	if err := requireMediaTypes(r.required, layers); err != nil {
		return FetchedLayers{}, err
	}
	return layers, nil
}

// selectManifestLayers reads one manifest and returns lazy handles for the
// layers whose media type is requested.
func selectManifestLayers(
	ctx context.Context,
	fetcher content.Fetcher,
	manifestDesc ocispec.Descriptor,
	mediaTypes []string,
	onProgress func(ocispec.Descriptor),
) (FetchedLayers, error) {
	var manifest ocispec.Manifest
	if err := fetchJSON(ctx, fetcher, manifestDesc, &manifest); err != nil {
		return FetchedLayers{}, err
	}

	layers := FetchedLayers{manifest: manifest}
	for _, layerDesc := range manifest.Layers {
		layer := FetchedLayer{
			Descriptor: layerDesc,
			open:       openLayer(fetcher, layerDesc, onProgress),
		}

		layers.allLayers = append(layers.allLayers, layer)
		if slices.Contains(mediaTypes, layerDesc.MediaType) {
			layers.selectedLayers = append(layers.selectedLayers, layer)
		}
	}

	return layers, nil
}

func validateMediaTypes(mediaTypes []string) error {
	if len(mediaTypes) == 0 {
		return errors.New("fetching layers: at least one media type is required")
	}
	if slices.Contains(mediaTypes, "") {
		return errors.New("fetching layers: media type must not be empty")
	}
	return nil
}

// requireMediaTypes fails when a requested media type matched no layer of the
// manifest.
func requireMediaTypes(requested []string, layers FetchedLayers) error {
	found := layers.ByMediaType()
	for _, mediaType := range requested {
		if len(found[mediaType]) == 0 {
			return fmt.Errorf("fetching layers: no layer with media type %q", mediaType)
		}
	}
	return nil
}

// Catalog wiring: resolving a Reference and then selecting layers cannot be a
// method, because Go methods cannot be generic and the result type follows from
// the request type. It becomes a package-level function over whatever resolves
// a reference to its root descriptor:
//
// func FetchRefLayers[R any](ctx context.Context, src layerSource, ref Reference, req LayerRequest[R]) (R, error) {
// 	var zero R
//
// 	desc, err := src.resolveRef(ctx, ref)
// 	if err != nil {
// 		return zero, err
// 	}
//
// 	result, err := FetchLayers(ctx, src, desc, req)
// 	if err != nil {
// 		return zero, fmt.Errorf("fetching layers for %q: %w", ref, err)
// 	}
// 	return result, nil
// }
//
// The local store resolves under its read lock, which covers selection only:
// layer content is read lazily when a handle is opened, which can happen after
// the lock is released. Blobs are content-addressed, so a concurrent push
// cannot tear a read, but blobs CAN be removed: LocalCatalog.Prune deletes
// every blob not reachable from a tag. A Delete (untag) followed by a Prune
// between selection and Open makes the handle fail with not-found. c.mu is
// also in-process only, so a prune from another scafctl process is not
// excluded even while the lock is held. The remote repository resolves the
// tag for the reference.
//
// TODO: lazy handles are NOT safe against the LocalCatalog today. Before wiring
// this to the local store, either read layer content within the lock (as
// FetchWithLayer does), re-resolve on not-found, or coordinate Prune with
// readers across processes (e.g. a file lock). Current local reads are eager,
// and FetchSolutionLayers only runs on the in-memory staging store of a pull.

// SolutionLayers is a solution artifact's layers addressed by the part they
// hold rather than by media type. It exists because the generic request API
// deliberately knows nothing about scafctl: a caller that wants the solution
// file should ask for the solution file, not look up a media type string and
// then decide what the bytes mean.
//
// Its accessors read a part in full rather than handing back a handle: every
// solution part is small enough to materialize, and the lazy handles underneath
// only earn their keep for provider binaries and descriptor-only inspection,
// which do not go through this type.
//
// The value itself holds no bytes, though. It fetches when an accessor is
// called, not when it is built, so it must be consumed within whatever critical
// section produced it rather than carried past one.
type SolutionLayers struct {
	layers FetchedLayers
}

// FetchSolutionLayers selects every part a solution artifact can carry. Only
// the solution file is required: a solution may have no bundle and no lock,
// and stores its bundle as either a v1 tar or a v2 deduplicated index.
func FetchSolutionLayers(ctx context.Context, fetcher content.Fetcher, root ocispec.Descriptor) (SolutionLayers, error) {
	layers, err := FetchLayers(ctx, fetcher, root,
		NewManifestLayers(MediaTypeSolutionContent).
			Optional(
				MediaTypeSolutionBundle,
				MediaTypeSolutionBundleManifest,
				MediaTypeSolutionLock,
			))
	if err != nil {
		return SolutionLayers{}, err
	}
	return SolutionLayers{layers: layers}, nil
}

// Layers exposes the underlying selection for callers that need the manifest
// itself rather than a named part.
func (s SolutionLayers) Layers() FetchedLayers {
	return s.layers
}

// Content returns the solution file. Every solution artifact has one, so a
// missing content layer is a malformed artifact rather than a miss.
func (s SolutionLayers) Content(ctx context.Context) ([]byte, error) {
	layer, ok := s.layers.Find(MediaTypeSolutionContent)
	if !ok {
		return nil, fmt.Errorf("solution artifact has no %q layer", MediaTypeSolutionContent)
	}

	data, err := layer.Bytes(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading solution content: %w", err)
	}
	return data, nil
}

// Lock returns the solution lock, reporting whether the artifact carries one.
// A solution published without resolved dependencies has no lock, so absence
// is normal rather than an error.
func (s SolutionLayers) Lock(ctx context.Context) ([]byte, bool, error) {
	layer, ok := s.layers.Find(MediaTypeSolutionLock)
	if !ok {
		return nil, false, nil
	}

	data, err := layer.Bytes(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("reading solution lock: %w", err)
	}
	return data, true, nil
}

// Bundle returns the solution bundle as a v1-compatible tar, reporting whether
// the artifact carries one. A solution with no files to ship has no bundle.
//
// The two on-disk shapes are hidden from the caller. A v1 artifact stores the
// tar as a single layer and is returned verbatim. A v2 artifact stores a
// manifest layer indexing per-file deduplicated blobs by absolute position, so
// it is reassembled into the v1 shape here, reading layers the request never
// selected.
func (s SolutionLayers) Bundle(ctx context.Context) ([]byte, bool, error) {
	if layer, ok := s.layers.Find(MediaTypeSolutionBundle); ok {
		data, err := layer.Bytes(ctx)
		if err != nil {
			return nil, false, fmt.Errorf("reading solution bundle: %w", err)
		}
		return data, true, nil
	}

	if _, ok := s.layers.Find(MediaTypeSolutionBundleManifest); !ok {
		return nil, false, nil
	}

	data, err := reassembleDedupBundle(s.layers.Manifest(), func(desc ocispec.Descriptor) ([]byte, error) {
		layer, err := s.layers.handle(desc)
		if err != nil {
			return nil, err
		}
		return layer.Bytes(ctx)
	})
	if err != nil {
		return nil, false, fmt.Errorf("reassembling deduplicated bundle: %w", err)
	}
	return data, true, nil
}
