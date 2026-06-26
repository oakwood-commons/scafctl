// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// DefaultMaxLayerSize bounds how large a content layer scafctl will read into
// memory when fetching a plugin/solution blob. It guards against a hostile or
// misconfigured registry advertising an enormous blob. Plugin binaries are
// typically tens of MiB, so 1 GiB is a generous ceiling that still prevents
// unbounded allocation.
//
// Note: oras-go's content.FetchAll / content.ReadAll impose their own 32 MiB
// cap, which is appropriate for small JSON manifests/configs but far too small
// for binary layers. fetchLayerContent deliberately bypasses that cap (while
// preserving size + digest verification) for content layers.
const DefaultMaxLayerSize int64 = 1 << 30 // 1 GiB

// maxLayerSize is the active upper bound enforced by fetchLayerContent. It
// defaults to DefaultMaxLayerSize and is a package var (not a const) so tests
// can lower it to exercise the size-limit path without allocating a real
// gigabyte-scale buffer. A value <= 0 disables the bound.
var maxLayerSize = DefaultMaxLayerSize

// fetchLayerContent fetches a content layer (a binary or bundle blob) from the
// fetcher, verifying it against the descriptor's size and digest.
//
// Unlike oras-go's content.FetchAll, it does not impose the 32 MiB ReadAll cap,
// so large plugin binaries can be fetched. The active bound is maxLayerSize.
func fetchLayerContent(
	ctx context.Context,
	fetcher content.Fetcher,
	desc ocispec.Descriptor,
) ([]byte, error) {
	if desc.Size < 0 {
		return nil, fmt.Errorf("invalid layer size %d for digest %s", desc.Size, desc.Digest)
	}
	if maxLayerSize > 0 && desc.Size > maxLayerSize {
		return nil, fmt.Errorf("layer size %d exceeds maximum allowed size %d for digest %s", desc.Size, maxLayerSize, desc.Digest)
	}

	rc, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	// Stream through a VerifyReader so size and digest are checked as the
	// content flows, without oras-go's in-memory ReadAll size cap.
	vr := content.NewVerifyReader(rc, desc)
	buf := make([]byte, desc.Size)
	if _, err := io.ReadFull(vr, buf); err != nil {
		return nil, fmt.Errorf("reading layer content for digest %s: %w", desc.Digest, err)
	}
	if err := vr.Verify(); err != nil {
		return nil, fmt.Errorf("verifying layer content for digest %s: %w", desc.Digest, err)
	}
	return buf, nil
}
