// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/Masterminds/semver/v3"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// ErrAmbiguousArtifact is returned when a push selector matches more than one
// distinct local artifact (by kind or by content digest).
var ErrAmbiguousArtifact = errors.New("ambiguous artifact reference")

// pushDestinationPlaceholder stands in for the --catalog value in hints.
const pushDestinationPlaceholder = "<destination>"

// shortDigestLen is the number of hex characters shown for digests in messages.
const shortDigestLen = 12

// OriginBuilt is the --origin flag value selecting locally built artifacts
// (artifacts with an empty source canonical). normalizeOriginFlag maps it to
// a separate Built selector field rather than a canonical string, so a
// registry literally named "built" can never be mistaken for it.
const OriginBuilt = "built"

// ociScheme is the optional scheme prefix accepted on remote references.
const ociScheme = "oci://"

// PushSelector describes which local artifact a push should publish. It is
// produced by ParsePushSelector from the positional reference and the
// --kind/--origin flags.
type PushSelector struct {
	// Kind filters candidates by artifact kind. Empty means "infer"; resolution
	// errors if candidates span more than one kind.
	Kind ArtifactKind

	// Name is the artifact name. For the FQN form it is the remote name.
	Name string

	// Version selects an exact version. Nil means latest stable per origin.
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

	// MatchRemoteName is set for the FQN form: candidates are matched on their
	// remote identity (source name) rather than their local name, so renamed
	// copies never match.
	MatchRemoteName bool
}

// ParsePushSelector parses a push source reference and its --kind/--origin
// flags into a PushSelector.
//
// Accepted input forms:
//   - "name", "name@version", "name@sha256:..." - a local artifact name.
//   - "registry/repo/<kinds>/name[@version|:version|@sha256:...]" - the local
//     mirror of that remote path. Shorthand for --origin registry/repo
//     --kind <kind>. When the path has no kinds segment, kindFlag is required.
//
// Errors (all wrap ErrInvalidReference):
//   - empty input, invalid name, version, digest, kind, or origin.
//   - a slash-containing input that is not a remote reference.
//   - originFlag combined with an FQN.
//   - kindFlag contradicting the FQN kinds segment.
//   - an FQN with no kinds segment and no kindFlag.
func ParsePushSelector(input, kindFlag, originFlag string) (PushSelector, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return PushSelector{}, &InvalidReferenceError{Input: input, Message: "reference cannot be empty"}
	}

	kind, err := parseKindFlag(kindFlag)
	if err != nil {
		return PushSelector{}, err
	}

	if LooksLikeRemoteReference(input) {
		if strings.TrimSpace(originFlag) != "" {
			return PushSelector{}, &InvalidReferenceError{
				Input:   input,
				Message: "--origin cannot be combined with a remote reference; the origin is taken from the reference",
			}
		}
		return parsePushFQN(input, kind)
	}

	if strings.Contains(input, "/") {
		return PushSelector{}, &InvalidReferenceError{
			Input:   input,
			Message: "expected an artifact name (name[@version]) or a full remote reference (registry/repo/<kinds>/name[@version])",
		}
	}

	origin, built, err := normalizeOriginFlag(originFlag)
	if err != nil {
		return PushSelector{}, err
	}

	ref, err := ParseReference(kind, input)
	if err != nil {
		return PushSelector{}, err
	}

	return PushSelector{
		Kind:    kind,
		Name:    ref.Name,
		Version: ref.Version,
		Digest:  ref.Digest,
		Origin:  origin,
		Built:   built,
	}, nil
}

// parsePushFQN parses the FQN form. kind is the already-validated --kind value
// (may be empty).
func parsePushFQN(input string, kind ArtifactKind) (PushSelector, error) {
	rr, err := ParseRemoteReference(input)
	if err != nil {
		return PushSelector{}, err
	}

	if rr.Kind != "" {
		// ParseRemoteReference takes the first kinds segment and the segment
		// right after it as the name, silently dropping anything that follows.
		// Require the kinds segment to be the second-to-last path element.
		path := strings.TrimPrefix(input, ociScheme)
		if rr.Tag != "" {
			path = path[:len(path)-len(rr.Tag)-1]
		}
		if !strings.HasSuffix(path, "/"+rr.Kind.Plural()+"/"+rr.Name) {
			return PushSelector{}, &InvalidReferenceError{
				Input:   input,
				Message: "remote reference must end in /<kinds>/<name>",
			}
		}
		if kind != "" && kind != rr.Kind {
			return PushSelector{}, &InvalidReferenceError{
				Input:   input,
				Message: "--kind " + kind.String() + " conflicts with kind " + rr.Kind.String() + " in the reference path",
			}
		}
		kind = rr.Kind
	}

	if kind == "" {
		return PushSelector{}, &InvalidReferenceError{
			Input:   input,
			Message: "cannot determine artifact kind: the reference has no /<kinds>/ segment; specify --kind",
		}
	}
	rr.Kind = kind

	if err := ValidateName(rr.Name); err != nil {
		return PushSelector{}, err
	}

	ref, err := rr.ToReference()
	if err != nil {
		return PushSelector{}, err
	}

	return PushSelector{
		Kind:            kind,
		Name:            ref.Name,
		Version:         ref.Version,
		Digest:          ref.Digest,
		Origin:          ref.Origin,
		MatchRemoteName: true,
	}, nil
}

// parseKindFlag validates the --kind flag. Empty means "infer".
func parseKindFlag(kindFlag string) (ArtifactKind, error) {
	kindFlag = strings.TrimSpace(kindFlag)
	if kindFlag == "" {
		return "", nil
	}
	kind, ok := ParseArtifactKind(kindFlag)
	if !ok {
		return "", &InvalidReferenceError{
			Input:   kindFlag,
			Message: "invalid kind (expected one of: " + ArtifactKindSolution.String() + ", " + ArtifactKindProvider.String() + ", " + ArtifactKindAuthHandler.String() + ")",
		}
	}
	return kind, nil
}

// normalizeOriginFlag validates and normalizes the --origin flag, returning
// the canonical to filter by and whether OriginBuilt (the locally-built
// selector) was given. The two are mutually exclusive: when built is true,
// the returned origin is always "". This separation (rather than overloading
// the canonical string with the OriginBuilt sentinel) ensures a registry
// literally named "built" can never be mistaken for the locally-built
// selector. Empty input means "any origin" (origin "", built false).
func normalizeOriginFlag(originFlag string) (origin string, built bool, err error) {
	origin = strings.TrimSpace(originFlag)
	if origin == "" {
		return "", false, nil
	}
	if origin == OriginBuilt {
		return "", true, nil
	}

	origin = strings.TrimRight(strings.TrimPrefix(origin, ociScheme), "/")
	invalid := origin == "" ||
		strings.Contains(origin, "://") ||
		strings.Contains(origin, "@") ||
		strings.Contains(origin, "//") ||
		strings.IndexFunc(origin, unicode.IsSpace) != -1
	if invalid {
		return "", false, &InvalidReferenceError{
			Input:   originFlag,
			Message: "invalid --origin (expected \"" + OriginBuilt + "\" or a canonical registry/repository, e.g. ghcr.io/myorg)",
		}
	}
	return origin, false, nil
}

// ResolveForPush selects exactly one local artifact to publish. Unlike
// Resolve, it never picks silently among distinct artifacts:
//
//  1. Candidates: by local name, or (MatchRemoteName) by remote identity --
//     source canonical plus AnnotationSourceName, falling back to the local
//     name when the source name is absent.
//  2. Digest: when set, only that content is kept; kind/version ambiguity rules
//     are skipped.
//  3. Origin: filtered by sel.Built (locally built only) or sel.Origin ("" any,
//     otherwise a canonical). For the FQN form this is the reference's
//     registry/repository.
//  4. Kind: without sel.Kind, remaining candidates spanning kinds ->
//     AmbiguousKindError.
//  5. Version: exact version keeps all matches; no version keeps the latest
//     stable version per origin (pre-releases only when opted in via context
//     or when an origin has no stable version).
//  6. Remaining entries are grouped by digest (alias tags collapse).
//
// Zero groups returns an error wrapping ErrArtifactNotFound, one group returns
// its representative (preferring the version tag), and more returns
// AmbiguousArtifactError.
func (c *LocalCatalog) ResolveForPush(ctx context.Context, sel PushSelector) (ArtifactInfo, error) {
	if sel.Name == "" {
		return ArtifactInfo{}, &InvalidReferenceError{Input: sel.Name, Message: "name cannot be empty"}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	matches, err := c.pushCandidatesLocked(ctx, sel)
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
		if sel.Version != nil {
			matches = filterInfos(matches, func(a ArtifactInfo) bool { return versionEqual(a.Reference.Version, sel.Version) })
		} else {
			matches = latestPerOrigin(matches, IncludePreReleaseFromContext(ctx))
		}
		matches = preferVersionTags(matches)
	}

	groups := groupByDigest(matches)
	switch len(groups) {
	case 0:
		return ArtifactInfo{}, pushNotFoundError(sel, beforeOrigin)
	case 1:
		return groups[0][0], nil
	default:
		return ArtifactInfo{}, newAmbiguousArtifactError(sel, groups)
	}
}

// pushCandidatesLocked lists the entries matching the selector's kind and
// local name, or remote name for the FQN form (origin is filtered later so
// not-found errors can report other origins). Caller must hold c.mu.
func (c *LocalCatalog) pushCandidatesLocked(ctx context.Context, sel PushSelector) ([]ArtifactInfo, error) {
	if !sel.MatchRemoteName {
		return c.listLocked(ctx, sel.Kind, sel.Name)
	}
	all, err := c.listLocked(ctx, sel.Kind, "")
	if err != nil {
		return nil, err
	}
	return filterInfos(all, func(a ArtifactInfo) bool { return remoteName(a) == sel.Name }), nil
}

// remoteName is the artifact's name at its source catalog.
func remoteName(a ArtifactInfo) string {
	if n := a.Annotations[AnnotationSourceName]; n != "" {
		return n
	}
	return a.Reference.Name
}

// originMatches reports whether a source canonical satisfies an origin filter.
func originMatches(canonical, origin string, built bool) bool {
	if built {
		return canonical == ""
	}
	if origin == "" {
		return true
	}
	return canonical == origin
}

func versionEqual(a, b *semver.Version) bool {
	return a != nil && b != nil && a.Equal(b)
}

func filterInfos(infos []ArtifactInfo, keep func(ArtifactInfo) bool) []ArtifactInfo {
	out := make([]ArtifactInfo, 0, len(infos))
	for _, a := range infos {
		if keep(a) {
			out = append(out, a)
		}
	}
	return out
}

func distinctKinds(infos []ArtifactInfo) []ArtifactKind {
	var kinds []ArtifactKind
	for _, a := range infos {
		if !slices.Contains(kinds, a.Reference.Kind) {
			kinds = append(kinds, a.Reference.Kind)
		}
	}
	slices.Sort(kinds)
	return kinds
}

// latestPerOrigin keeps, for each source canonical, every entry at that
// origin's highest version. Stable versions are preferred unless includePre
// is set or the origin has only pre-releases. Entries without a version are
// kept only when no entry of that origin has one.
func latestPerOrigin(infos []ArtifactInfo, includePre bool) []ArtifactInfo {
	byOrigin := make(map[string][]ArtifactInfo)
	for _, a := range infos {
		byOrigin[a.Canonical] = append(byOrigin[a.Canonical], a)
	}

	var out []ArtifactInfo
	for _, group := range byOrigin {
		if !includePre {
			if stable := filterInfos(group, func(a ArtifactInfo) bool { return !IsPreRelease(a.Reference.Version) }); len(stable) > 0 {
				group = stable
			}
		}
		var best *semver.Version
		for _, a := range group {
			if v := a.Reference.Version; v != nil && (best == nil || v.GreaterThan(best)) {
				best = v
			}
		}
		for _, a := range group {
			if (best == nil && a.Reference.Version == nil) || versionEqual(a.Reference.Version, best) {
				out = append(out, a)
			}
		}
	}
	return out
}

// groupByDigest groups entries by content digest. Within a group the
// representative (index 0) is the entry whose tag is its version, then by
// canonical and tag. Groups are ordered by representative canonical, version,
// and digest so output is deterministic.
func groupByDigest(infos []ArtifactInfo) [][]ArtifactInfo {
	return groupInfos(infos, func(a ArtifactInfo) string { return a.Digest })
}

// groupByOriginDigest is groupByDigest keyed by (source canonical, digest):
// identical content from different origins stays in separate groups, since
// each origin is a distinct local identity.
func groupByOriginDigest(infos []ArtifactInfo) [][]ArtifactInfo {
	return groupInfos(infos, func(a ArtifactInfo) string { return a.Canonical + "\x00" + a.Digest })
}

func groupInfos(infos []ArtifactInfo, key func(ArtifactInfo) string) [][]ArtifactInfo {
	byKey := make(map[string][]ArtifactInfo)
	for _, a := range infos {
		k := key(a)
		byKey[k] = append(byKey[k], a)
	}

	groups := make([][]ArtifactInfo, 0, len(byKey))
	for _, group := range byKey {
		slices.SortFunc(group, func(a, b ArtifactInfo) int {
			if av, bv := isVersionTag(a), isVersionTag(b); av != bv {
				if av {
					return -1
				}
				return 1
			}
			if c := strings.Compare(a.Canonical, b.Canonical); c != 0 {
				return c
			}
			return strings.Compare(a.Tag, b.Tag)
		})
		groups = append(groups, group)
	}
	slices.SortFunc(groups, func(a, b []ArtifactInfo) int {
		if c := strings.Compare(a[0].Canonical, b[0].Canonical); c != 0 {
			return c
		}
		if c := strings.Compare(a[0].Reference.VersionOrDigest(), b[0].Reference.VersionOrDigest()); c != 0 {
			return c
		}
		return strings.Compare(a[0].Digest, b[0].Digest)
	})
	return groups
}

func isVersionTag(a ArtifactInfo) bool {
	return a.Reference.Version != nil && a.Tag == a.Reference.Version.String()
}

// preferVersionTags drops, per source canonical, alias entries whose digest
// is not held by any of that origin's version tags. Aliases of a version-tagged
// copy are kept (so they still collapse into its candidate), and an origin
// with no version tag keeps all its entries. An alias carries its target's
// version annotation, so without this a stale alias left behind by a
// same-version rebuild (--force) would look like a second artifact at that
// version and make a plain name@version ambiguous.
func preferVersionTags(infos []ArtifactInfo) []ArtifactInfo {
	hasVersionTag := make(map[string]bool)
	versionDigests := make(map[string]bool)
	digestKey := func(a ArtifactInfo) string {
		return a.Canonical + "\x00" + string(a.Reference.Kind) + "\x00" + a.Digest
	}
	for _, a := range infos {
		if isVersionTag(a) {
			hasVersionTag[a.Canonical] = true
			versionDigests[digestKey(a)] = true
		}
	}
	return filterInfos(infos, func(a ArtifactInfo) bool {
		return !hasVersionTag[a.Canonical] || isVersionTag(a) || versionDigests[digestKey(a)]
	})
}

// pushNotFoundError builds a not-found error. When an origin filter removed
// every match, it lists the origins that do have the artifact.
func pushNotFoundError(sel PushSelector, beforeOrigin []ArtifactInfo) error {
	base := &ArtifactNotFoundError{
		Reference: Reference{Kind: sel.Kind, Name: sel.Name, Version: sel.Version, Digest: sel.Digest},
		Catalog:   LocalCatalogName,
	}
	if sel.Origin == "" && !sel.Built {
		return base
	}

	var available []string
	for _, a := range beforeOrigin {
		if sel.Version != nil && sel.Digest == "" && !versionEqual(a.Reference.Version, sel.Version) {
			continue
		}
		if label := originLabel(a.Canonical); !slices.Contains(available, label) {
			available = append(available, label)
		}
	}
	slices.Sort(available)

	if len(available) == 0 {
		return fmt.Errorf("no copy from %s: %w", originLabel(originCanonicalFor(sel.Origin, sel.Built)), base)
	}
	return fmt.Errorf("no copy from %s (available from: %s): %w",
		originLabel(originCanonicalFor(sel.Origin, sel.Built)), strings.Join(available, ", "), base)
}

// originCanonicalFor maps an origin filter to the canonical it matches.
func originCanonicalFor(origin string, built bool) string {
	if built {
		return ""
	}
	return origin
}

// originLabel renders a source canonical for humans.
func originLabel(canonical string) string {
	if canonical == "" {
		return "built locally"
	}
	return canonical
}

// originFlagValue renders a source canonical as an --origin flag value.
func originFlagValue(canonical string) string {
	if canonical == "" {
		return OriginBuilt
	}
	return canonical
}

func shortDigest(d string) string {
	const prefix = "sha256:"
	if strings.HasPrefix(d, prefix) && len(d) > len(prefix)+shortDigestLen {
		return d[:len(prefix)+shortDigestLen]
	}
	return d
}

// PushCandidate is one distinct artifact (content digest) matching an
// ambiguous push selector.
type PushCandidate struct {
	// Name is the local artifact name.
	Name string
	// Origin is the source canonical; empty for locally built artifacts.
	Origin string
	// Version is the artifact version; empty when unversioned.
	Version string
	// Digest is the manifest digest.
	Digest string
	// Tags are the tag labels (versions/aliases) pointing at this digest.
	Tags []string
}

// AmbiguousArtifactError is returned by ResolveForPush when more than one
// distinct artifact matches.
type AmbiguousArtifactError struct {
	Kind       ArtifactKind
	Name       string
	Version    string // empty when the latest version was requested
	Candidates []PushCandidate
}

func newAmbiguousArtifactError(sel PushSelector, groups [][]ArtifactInfo) *AmbiguousArtifactError {
	e := &AmbiguousArtifactError{Kind: sel.Kind, Name: sel.Name}
	if e.Kind == "" {
		e.Kind = groups[0][0].Reference.Kind
	}
	if sel.Version != nil {
		e.Version = sel.Version.String()
	}
	for _, group := range groups {
		rep := group[0]
		pc := PushCandidate{Name: rep.Reference.Name, Origin: rep.Canonical, Digest: rep.Digest}
		if rep.Reference.Version != nil {
			pc.Version = rep.Reference.Version.String()
		}
		for _, a := range group {
			if a.Tag != "" && !slices.Contains(pc.Tags, a.Tag) {
				pc.Tags = append(pc.Tags, a.Tag)
			}
		}
		e.Candidates = append(e.Candidates, pc)
	}
	return e
}

// Error implements the error interface.
func (e *AmbiguousArtifactError) Error() string {
	target := e.Name
	if e.Version != "" {
		target += "@" + e.Version
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %q matches %d different local artifacts:", e.Kind, target, len(e.Candidates))
	for _, c := range e.Candidates {
		fmt.Fprintf(&sb, "\n  %s  %s  %s  [%s]", originLabel(c.Origin), c.Version, shortDigest(c.Digest), strings.Join(c.Tags, ", "))
	}
	return sb.String()
}

// Unwrap returns ErrAmbiguousArtifact for errors.Is support.
func (e *AmbiguousArtifactError) Unwrap() error {
	return ErrAmbiguousArtifact
}

// Hints returns copy-pasteable commands selecting each candidate. A candidate
// whose origin is unique is selected with --origin; otherwise by digest.
func (e *AmbiguousArtifactError) Hints(binaryName string) []string {
	binaryName = hintBinaryName(binaryName)
	hints := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		if c.Version != "" && e.originCount(c.Origin) == 1 {
			hints = append(hints, fmt.Sprintf("%s catalog push %s@%s --kind %s --origin %s --catalog %s",
				binaryName, c.Name, c.Version, e.Kind, originFlagValue(c.Origin), pushDestinationPlaceholder))
			continue
		}
		hints = append(hints, fmt.Sprintf("%s catalog push %s@%s --kind %s --catalog %s",
			binaryName, c.Name, c.Digest, e.Kind, pushDestinationPlaceholder))
	}
	return hints
}

func (e *AmbiguousArtifactError) originCount(origin string) int {
	n := 0
	for _, c := range e.Candidates {
		if c.Origin == origin {
			n++
		}
	}
	return n
}

// DeleteHints returns copy-pasteable "catalog delete" commands selecting each
// candidate. See exactHints for the selection rules.
func (e *AmbiguousArtifactError) DeleteHints(binaryName string) []string {
	return e.exactHints(binaryName, "delete", "")
}

// TagHints returns copy-pasteable "catalog tag" commands selecting each
// candidate for alias. See exactHints for the selection rules.
func (e *AmbiguousArtifactError) TagHints(binaryName, alias string) []string {
	return e.exactHints(binaryName, "tag", " "+alias)
}

// exactHints renders one "catalog <verb>" command per candidate of an
// ResolveExact ambiguity. Every hint carries --origin, because identical
// content can exist under several origins (separate local identities). The
// candidate is selected by version when that is unique within its origin,
// otherwise by digest. args is appended after the reference.
func (e *AmbiguousArtifactError) exactHints(binaryName, verb, args string) []string {
	binaryName = hintBinaryName(binaryName)
	hints := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		selector := c.Digest
		if c.Version != "" && e.originCount(c.Origin) == 1 {
			selector = c.Version
		}
		hints = append(hints, fmt.Sprintf("%s catalog %s %s@%s%s --kind %s --origin %s",
			binaryName, verb, c.Name, selector, args, e.Kind, originFlagValue(c.Origin)))
	}
	return hints
}

// AmbiguousKindError is returned by ResolveForPush when the name exists under
// more than one artifact kind and no kind was given.
type AmbiguousKindError struct {
	Name  string
	Kinds []ArtifactKind
	// Selector is the original "@version" or "@digest" suffix from the input
	// reference, if any. It is preserved so generated hints remain directly
	// runnable instead of requiring the version/digest to be re-specified.
	Selector string
}

// selectorSuffix formats a version/digest pair as the "@..." suffix to append
// to a bare artifact name in a generated command hint. Digest takes
// precedence when both are set; returns "" when neither is set.
func selectorSuffix(version *semver.Version, digest string) string {
	if digest != "" {
		return "@" + digest
	}
	if version != nil {
		return "@" + version.String()
	}
	return ""
}

// Error implements the error interface.
func (e *AmbiguousKindError) Error() string {
	kinds := make([]string, len(e.Kinds))
	for i, k := range e.Kinds {
		kinds[i] = k.String()
	}
	return fmt.Sprintf("%q exists as multiple kinds (%s); specify --kind", e.Name+e.Selector, strings.Join(kinds, ", "))
}

// Unwrap returns ErrAmbiguousArtifact for errors.Is support.
func (e *AmbiguousKindError) Unwrap() error {
	return ErrAmbiguousArtifact
}

// Hints returns one command per kind.
func (e *AmbiguousKindError) Hints(binaryName string) []string {
	binaryName = hintBinaryName(binaryName)
	hints := make([]string, 0, len(e.Kinds))
	for _, k := range e.Kinds {
		hints = append(hints, fmt.Sprintf("%s catalog push %s%s --kind %s --catalog %s", binaryName, e.Name, e.Selector, k, pushDestinationPlaceholder))
	}
	return hints
}

// DeleteHints returns one "catalog delete" command per kind.
func (e *AmbiguousKindError) DeleteHints(binaryName string) []string {
	return e.verbHints(binaryName, "delete", "")
}

// TagHints returns one "catalog tag" command per kind for alias.
func (e *AmbiguousKindError) TagHints(binaryName, alias string) []string {
	return e.verbHints(binaryName, "tag", " "+alias)
}

func (e *AmbiguousKindError) verbHints(binaryName, verb, args string) []string {
	binaryName = hintBinaryName(binaryName)
	hints := make([]string, 0, len(e.Kinds))
	for _, k := range e.Kinds {
		hints = append(hints, fmt.Sprintf("%s catalog %s %s%s%s --kind %s", binaryName, verb, e.Name, e.Selector, args, k))
	}
	return hints
}

// hintBinaryName is the binary name used in generated command hints, falling
// back to the default CLI name when an embedder leaves it empty.
func hintBinaryName(binaryName string) string {
	if binaryName == "" {
		return settings.CliBinaryName
	}
	return binaryName
}

// IsAmbiguous reports whether err is an ambiguity error from ResolveForPush or
// ResolveExact.
func IsAmbiguous(err error) bool {
	return errors.Is(err, ErrAmbiguousArtifact)
}

// ErrPushToOrigin is returned by ValidatePushDestination when a pulled copy
// would be pushed back to the catalog it was pulled from.
var ErrPushToOrigin = errors.New("cannot push an artifact back to the catalog it was pulled from")

// ErrPushDestinationRequired is returned when push is invoked without a
// destination catalog. Push never falls back to the default catalog: the
// default is a read setting and reusing it as a publish target would make the
// destination implicit and machine-dependent.
var ErrPushDestinationRequired = errors.New(`required flag "--catalog" not set; specify the destination catalog to push to`)

// NewPushDestinationRequiredError wraps ErrPushDestinationRequired with the
// configured OCI catalogs (usable as --catalog values) and an example.
func NewPushDestinationRequiredError(ctx context.Context) error {
	var sb strings.Builder
	if cfg := config.FromContext(ctx); cfg != nil {
		for _, cat := range cfg.Catalogs {
			if cat.Type != config.CatalogTypeOCI {
				continue
			}
			if sb.Len() == 0 {
				sb.WriteString("\n\nConfigured catalogs:")
			}
			fmt.Fprintf(&sb, "\n  %s  %s", cat.Name, cat.URL)
		}
	}
	fmt.Fprintf(&sb, "\n\nExample:\n  %s catalog push <artifact> --catalog <registry-url|catalog-name>", settings.BinaryNameFromContext(ctx))
	return fmt.Errorf("%w%s", ErrPushDestinationRequired, sb.String())
}

// ValidatePushDestination rejects publishing a pulled copy to its own origin
// (destCanonical is the destination's RemoteCatalog.CanonicalID). Such a push
// is at best a no-op and, with --force, can silently revert a version that
// was republished upstream after the pull. Built artifacts (empty Canonical)
// are always allowed. Registry paths are compared case-insensitively.
func ValidatePushDestination(info ArtifactInfo, destCanonical string) error {
	if info.Canonical == "" || !strings.EqualFold(info.Canonical, strings.TrimRight(destCanonical, "/")) {
		return nil
	}
	return fmt.Errorf("%w: %s@%s was pulled from %s; choose a different --catalog or select another copy with --origin",
		ErrPushToOrigin, info.Reference.Name, info.Reference.VersionOrDigest(), info.Canonical)
}
