// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ExactSelector describes exactly one local artifact to operate on (e.g. for
// delete or tag). It is produced by ParseExactSelector from the positional
// reference and the --kind/--origin flags.
type ExactSelector struct {
	// Kind filters candidates by artifact kind. Empty means "infer"; resolution
	// errors if candidates span more than one kind.
	Kind ArtifactKind

	// Name is the artifact name.
	Name string

	// Version selects an exact version. A version or digest is always
	// required.
	Version *semver.Version

	// Digest selects exact content and bypasses ambiguity logic.
	Digest string

	// Origin filters by source canonical: "" = any (unless Built is set),
	// otherwise a canonical "registry" or "registry/repository". Empty when
	// Built is true.
	Origin string

	// Built selects only locally-built artifacts (empty source canonical),
	// set from --origin built. Mutually exclusive with a non-empty Origin.
	Built bool
}

// ParseExactSelector parses a reference and its --kind/--origin flags into an
// ExactSelector.
//
// Accepted input forms:
//   - "name@version" - an exact version.
//   - "name@sha256:..." - exact content.
//
// Errors (all wrap ErrInvalidReference): empty input, invalid name, version,
// digest, kind, or origin, or a reference with no version/digest.
func ParseExactSelector(input, kindFlag, originFlag string) (ExactSelector, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return ExactSelector{}, &InvalidReferenceError{Input: input, Message: "reference cannot be empty"}
	}

	kind, err := parseKindFlag(kindFlag)
	if err != nil {
		return ExactSelector{}, err
	}

	origin, built, err := normalizeOriginFlag(originFlag)
	if err != nil {
		return ExactSelector{}, err
	}

	ref, err := ParseReference(kind, input)
	if err != nil {
		return ExactSelector{}, err
	}
	if ref.Version == nil && ref.Digest == "" {
		return ExactSelector{}, &InvalidReferenceError{
			Input:   input,
			Message: "version or digest required: use format 'name@version' (e.g., 'my-solution@1.0.0')",
		}
	}

	return ExactSelector{
		Kind:    kind,
		Name:    ref.Name,
		Version: ref.Version,
		Digest:  ref.Digest,
		Origin:  origin,
		Built:   built,
	}, nil
}

// ResolveExact selects exactly one local artifact (one local identity) to
// operate on, e.g. to delete or tag. Like ResolveForPush, it never picks
// silently among distinct artifacts: candidates are filtered by name, kind,
// version/digest, and origin, then grouped by origin and content digest.
// Unlike push, copies of identical content from different origins are NOT
// collapsed: each origin is a separate local identity, and acting on one must
// not silently leave (or touch) the other. Zero groups is a not-found error,
// one group returns its representative (with Reference.Origin stamped and Tag
// naming its exact stored tag), and more than one is an AmbiguousArtifactError
// naming --origin as the disambiguator.
func (c *LocalCatalog) ResolveExact(ctx context.Context, sel ExactSelector) (ArtifactInfo, error) {
	if sel.Name == "" {
		return ArtifactInfo{}, &InvalidReferenceError{Input: sel.Name, Message: "name cannot be empty"}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	matches, err := c.listLocked(ctx, sel.Kind, sel.Name)
	if err != nil {
		return ArtifactInfo{}, err
	}

	if sel.Digest != "" {
		matches = filterInfos(matches, func(a ArtifactInfo) bool { return a.Digest == sel.Digest })
	}

	beforeOrigin := matches
	matches = filterInfos(matches, func(a ArtifactInfo) bool { return originMatches(a.Canonical, sel.Origin, sel.Built) })

	if sel.Digest == "" && sel.Kind == "" {
		if kinds := distinctKinds(matches); len(kinds) > 1 {
			return ArtifactInfo{}, &AmbiguousKindError{Name: sel.Name, Kinds: kinds, Selector: selectorSuffix(sel.Version, sel.Digest)}
		}
	}

	if sel.Digest == "" {
		matches = filterInfos(matches, func(a ArtifactInfo) bool { return versionEqual(a.Reference.Version, sel.Version) })
		matches = preferVersionTags(matches)
	}

	groups := groupByOriginDigest(matches)
	switch len(groups) {
	case 0:
		return ArtifactInfo{}, pushNotFoundError(PushSelector{
			Kind: sel.Kind, Name: sel.Name, Version: sel.Version, Digest: sel.Digest, Origin: sel.Origin, Built: sel.Built,
		}, beforeOrigin)
	case 1:
		return withResolvedOrigin(groups[0][0]), nil
	default:
		return ArtifactInfo{}, newAmbiguousArtifactError(PushSelector{
			Kind: sel.Kind, Name: sel.Name, Version: sel.Version, Digest: sel.Digest, Origin: sel.Origin, Built: sel.Built,
		}, groups)
	}
}
