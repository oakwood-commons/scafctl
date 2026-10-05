// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
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
// Note: oras-go's content.FetchAll / content.ReadAll do not bound the blob
// size; they only cap the initial buffer allocation (32 MiB) and verify size
// and digest. fetchLayerContent adds the maxLayerSize bound (while preserving
// size + digest verification) so content layers fetched from a registry cannot
// force an unbounded allocation.
const DefaultMaxLayerSize int64 = 1 << 30 // 1 GiB

// maxLayerSize is the active upper bound enforced by fetchLayerContent. It
// defaults to DefaultMaxLayerSize and is a package var (not a const) so tests
// can lower it to exercise the size-limit path without allocating a real
// gigabyte-scale buffer. A value <= 0 disables the bound.
var maxLayerSize = DefaultMaxLayerSize

// ErrLayerTooLarge reports a blob whose advertised size exceeds the active
// layer size limit.
var ErrLayerTooLarge = errors.New("layer too large")

// checkLayerSize rejects a descriptor with a negative size or one larger than
// maxLayerSize, so the blob can be refused before any bytes are requested.
func checkLayerSize(desc ocispec.Descriptor) error {
	if desc.Size < 0 {
		return fmt.Errorf("invalid layer size %d for digest %s", desc.Size, desc.Digest)
	}
	if maxLayerSize > 0 && desc.Size > maxLayerSize {
		return fmt.Errorf("%w: layer size %d exceeds maximum allowed size %d for digest %s", ErrLayerTooLarge, desc.Size, maxLayerSize, desc.Digest)
	}
	return nil
}

// fetchLayerContent fetches a content layer (a binary or bundle blob) from the
// fetcher, verifying it against the descriptor's size and digest.
//
// Unlike oras-go's content.FetchAll, it rejects a descriptor larger than
// maxLayerSize before reading.
func fetchLayerContent(
	ctx context.Context,
	fetcher content.Fetcher,
	desc ocispec.Descriptor,
) ([]byte, error) {
	if err := checkLayerSize(desc); err != nil {
		return nil, err
	}

	rc, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return readVerifiedLayer(rc, desc)
}

// readVerifiedLayer reads exactly desc.Size bytes from r, verifying size and
// digest. It streams through a VerifyReader so the checks happen as the content
// flows, without oras-go's in-memory ReadAll size cap.
func readVerifiedLayer(r io.Reader, desc ocispec.Descriptor) ([]byte, error) {
	vr := content.NewVerifyReader(r, desc)
	buf := make([]byte, desc.Size)
	if _, err := io.ReadFull(vr, buf); err != nil {
		return nil, fmt.Errorf("reading layer content for digest %s: %w", desc.Digest, err)
	}
	if err := vr.Verify(); err != nil {
		return nil, fmt.Errorf("verifying layer content for digest %s: %w", desc.Digest, err)
	}
	return buf, nil
}
