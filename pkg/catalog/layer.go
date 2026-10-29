// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func (l *Layer) Descriptor() ocispec.Descriptor {
	return ocispec.Descriptor{
		MediaType: l.MediaType,
		Digest:    digest.FromBytes(l.Data),
		Size:      int64(len(l.Data)),
	}
}
