// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalogindex

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// catalogAliasSeparator separates a catalog alias from the artifact name in
// "<alias>/<name>[@version]" references.
const catalogAliasSeparator = "/"

// ErrUnknownCatalog is returned when an "<alias>/<name>" reference names no
// configured remote catalog.
var ErrUnknownCatalog = errors.New("unknown catalog")

// ParseReference parses a CLI artifact reference into a catalog.Reference
// whose origin is explicit:
//
//   - "<registry>/.../name[@v]" -> Remote set to the canonical origin
//   - "name[@v]"                -> origin left open (resolved later)
//   - "local/name[@v]"          -> Built set (never resolved over the network)
//   - "<alias>/name[@v]"        -> alias translated to its registry via the
//     index, then parsed as a remote reference
//
// URLs and file paths are not handled; callers must route those first.
// Safe on a nil receiver: every alias lookup misses.
func (idx *Index) ParseReference(kind catalog.ArtifactKind, input string) (catalog.Reference, error) {
	input = strings.TrimSpace(input)

	if catalog.LooksLikeRemoteReference(input) {
		return parseRemoteReference(kind, input)
	}

	alias, rest, qualified := strings.Cut(input, catalogAliasSeparator)
	if !qualified {
		return catalog.ParseReference(kind, input)
	}
	if alias == "" || strings.Contains(rest, catalogAliasSeparator) {
		return catalog.Reference{}, &catalog.InvalidReferenceError{
			Input:   input,
			Message: "expected <catalog>/<name>[@version]",
		}
	}

	if strings.EqualFold(alias, config.CatalogNameLocal) {
		ref, err := catalog.ParseReference(kind, rest)
		if err != nil {
			return catalog.Reference{}, err
		}
		ref.Built = true
		return ref, nil
	}

	registry, ok := idx.RegistryForAlias(alias)
	if !ok {
		return catalog.Reference{}, fmt.Errorf("%w %q in reference %q", ErrUnknownCatalog, alias, input)
	}
	return parseRemoteReference(kind, registry+catalogAliasSeparator+rest)
}

// parseRemoteReference parses a registry reference, defaulting its kind to the
// requested kind and rejecting a kind mismatch or an invalid artifact name.
func parseRemoteReference(kind catalog.ArtifactKind, input string) (catalog.Reference, error) {
	rr, err := catalog.ParseRemoteReference(input)
	if err != nil {
		return catalog.Reference{}, err
	}
	switch {
	case rr.Kind == "":
		rr.Kind = kind
	case rr.Kind != kind:
		return catalog.Reference{}, &catalog.InvalidReferenceError{
			Input:   input,
			Message: fmt.Sprintf("reference is a %s, not a %s", rr.Kind, kind),
		}
	}
	ref, err := rr.ToReference()
	if err != nil {
		return catalog.Reference{}, err
	}
	if !catalog.IsValidName(ref.Name) {
		return catalog.Reference{}, &catalog.InvalidReferenceError{
			Input:   input,
			Message: "name must be lowercase alphanumeric with hyphens (e.g., 'my-solution')",
		}
	}
	return ref, nil
}
