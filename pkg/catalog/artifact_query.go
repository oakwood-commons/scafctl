// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"
	"sort"

	"github.com/Masterminds/semver/v3"
)

// sortByVersionDesc orders artifacts newest first. Artifacts without a version
// sort last, since they cannot win a latest-version comparison.
func sortByVersionDesc(artifacts []ArtifactInfo) {
	sort.Slice(artifacts, func(i, j int) bool {
		vi := artifacts[i].Reference.Version
		vj := artifacts[j].Reference.Version
		if vi == nil {
			return false
		}
		if vj == nil {
			return true
		}
		return vi.GreaterThan(vj)
	})
}

// checkAmbiguity returns an error if matches span multiple provenances (built and remote)
// or multiple remote origins. It assumes all matches share name and kind.
func checkAmbiguity(query ArtifactQuery, artifacts []ArtifactInfo) error {
	seenRemotes := make(map[string]bool)
	seenBuilt := false
	for _, a := range artifacts {
		if a.Reference.Built {
			seenBuilt = true
		} else {
			seenRemotes[a.Reference.Remote] = true
		}
	}

	if seenBuilt && len(seenRemotes) > 0 {
		return &AmbiguousArtifactError{Query: query, Matches: artifacts}
	}
	if len(seenRemotes) > 1 {
		return &AmbiguousArtifactError{Query: query, Matches: artifacts}
	}

	return nil
}

// SourceKind selects which provenance an ArtifactQuery accepts. Provenance is
// derived from annotations, not from the OCI tag, because tags are mutable
// storage labels while annotations carry scafctl identity.
type SourceKind int

const (
	// SourceAny matches artifacts from any provenance.
	SourceAny SourceKind = iota

	// SourceBuilt matches artifacts built locally (no AnnotationSourceCanonical).
	SourceBuilt

	// SourceRemote matches artifacts copied from a remote catalog. The
	// selector's Remote field names which one.
	SourceRemote
)

// String implements fmt.Stringer.
func (k SourceKind) String() string {
	switch k {
	case SourceAny:
		return "any"
	case SourceBuilt:
		return "built"
	case SourceRemote:
		return "remote"
	default:
		return fmt.Sprintf("SourceKind(%d)", int(k))
	}
}

// SourceSelector narrows an ArtifactQuery by provenance. The zero value
// (SourceAny) matches everything, so "any source" is explicit rather than
// implied by an empty origin string.
type SourceSelector struct {
	// Kind is the provenance class to match.
	Kind SourceKind `json:"kind" yaml:"kind" doc:"Provenance class to match"`

	// Remote is the canonical remote catalog identity (e.g. "ghcr.io/org/plugins").
	// Required when Kind is SourceRemote, and must be empty otherwise.
	Remote string `json:"remote,omitempty" yaml:"remote,omitempty" doc:"Canonical remote catalog identity"`
}

// String implements fmt.Stringer.
func (s SourceSelector) String() string {
	if s.Kind == SourceRemote && s.Remote != "" {
		return "remote:" + s.Remote
	}
	return s.Kind.String()
}

// matches reports whether annotations satisfy the selector.
//
// Provenance is binary: an artifact either came from a remote catalog or was
// built here. Absence of a canonical source is therefore what defines "built",
// rather than the presence of AnnotationBuild, so artifacts stored before that
// annotation existed still classify correctly.
func (s SourceSelector) matches(annotations map[string]string) bool {
	remote := annotations[AnnotationSourceCanonical]

	switch s.Kind {
	case SourceAny:
		return true
	case SourceBuilt:
		return remote == ""
	case SourceRemote:
		return remote == s.Remote
	default:
		return false
	}
}

// describeSource renders an artifact's provenance for diagnostics.
func describeSource(annotations map[string]string) string {
	if remote := annotations[AnnotationSourceCanonical]; remote != "" {
		return "remote:" + remote
	}
	return SourceBuilt.String()
}

// ArtifactQuery looks up artifacts by their annotation identity:
// name ^ kind ^ (version) ^ (remote origin | built).
//
// Name and Kind are the minimum required selectors; they intentionally may
// match several artifacts. Version and Source are optional narrowing filters:
//
//	Name + Kind                          all versions from all sources
//	Name + Kind + Version                that version from any source
//	Name + Kind + Source(remote)         any version from that origin
//
// Name + Kind + Version or Digest + Source(built) that built version
// Name + Kind + Version or Digest + Source(remote) that version from that origin
type ArtifactQuery struct {
	// Name is the artifact name. Required.
	Name string `json:"name" yaml:"name" doc:"Artifact name"`

	// Kind is the artifact type. Required.
	Kind ArtifactKind `json:"kind" yaml:"kind" doc:"Artifact type"`

	// Version is the exact version to match. Empty matches any version.
	Version string `json:"version,omitempty" yaml:"version,omitempty" doc:"Exact version to match"`

	// Digest is the content digest to match. Empty matches any digest.
	Digest string `json:"digest,omitempty" yaml:"digest,omitempty" doc:"Content digest to match"`

	// Source narrows by provenance. The zero value matches any source.
	Source SourceSelector `json:"source,omitzero" yaml:"source,omitempty" doc:"Provenance filter"`
}

type VersionOrConstraintType int

const (
	LatestVersionConstraint VersionOrConstraintType = iota
	IsVersion
	IsConstraint
	InvalidVersionOrConstraint
)

type VersionOrConstraint struct {
	version    *semver.Version
	constraint *semver.Constraints
	semVarType VersionOrConstraintType
}

func (v *VersionOrConstraint) Type() VersionOrConstraintType {
	return v.semVarType
}

func (v *VersionOrConstraint) Set(value string) {
	if value == "" || value == "latest" {
		v.semVarType = LatestVersionConstraint
		return
	}

	// TODO: short versions ("1", "1.2") parse as exact versions (1.0.0, 1.2.0)
	// here, whereas as constraints they mean ranges (1.x, 1.2.x). Consider
	// treating only full major.minor.patch values as exact and everything else
	// as a constraint, so "--version 1" selects the newest 1.x.
	if ver, err := semver.NewVersion(value); err == nil {
		v.version = ver
		v.semVarType = IsVersion
		return
	}
	if constraint, err := semver.NewConstraint(value); err == nil && constraint != nil {
		v.constraint = constraint
		v.semVarType = IsConstraint
		return
	}

	v.semVarType = InvalidVersionOrConstraint
}

func (v *VersionOrConstraint) Satisfies(version string) bool {
	if v.semVarType == LatestVersionConstraint {
		return true
	}
	ver, err := semver.NewVersion(version)
	if err != nil {
		return false
	}
	return v.satisfiesVersion(ver)
}

// satisfiesVersion is satisfies for an already-parsed version, so callers
// holding semver values (e.g. a registry tag list) avoid a re-parse.
func (v *VersionOrConstraint) satisfiesVersion(ver *semver.Version) bool {
	switch v.semVarType {
	case LatestVersionConstraint:
		return true
	case IsVersion:
		return v.version != nil && ver != nil && v.version.Equal(ver)
	case IsConstraint:
		return v.constraint != nil && ver != nil && v.constraint.Check(ver)
	case InvalidVersionOrConstraint:
		return false
	}
	return false
}

func (v *VersionOrConstraint) IsValid() bool {
	return v.semVarType != InvalidVersionOrConstraint
}

func (v *VersionOrConstraint) String() string {
	switch v.semVarType {
	case IsVersion:
		if v.version != nil {
			return v.version.String()
		}
	case IsConstraint:
		if v.constraint != nil {
			return v.constraint.String()
		}
	case LatestVersionConstraint:
		return "latest"
	case InvalidVersionOrConstraint:
		return "invalid"
	}
	return "invalid"
}

// String implements fmt.Stringer.
func (q ArtifactQuery) String() string {
	versionOrDigest := q.Version
	separator := ":"
	if q.Digest != "" {
		versionOrDigest = q.Digest
		separator = "@"
	}
	if versionOrDigest == "" {
		versionOrDigest = "*"
	}
	return fmt.Sprintf("%s/%s%s%s (source=%s)", q.Kind, q.Name, separator, versionOrDigest, q.Source)
}

// Validate reports whether the query carries the minimum identity
// (name + kind) and a coherent source selector.
func (q ArtifactQuery) Validate() error {
	if q.Name == "" {
		return &InvalidQueryError{Query: q, Message: "name is required"}
	}
	if q.Kind == "" {
		return &InvalidQueryError{Query: q, Message: "kind is required"}
	}
	if q.Source.Kind == SourceRemote && q.Source.Remote == "" {
		return &InvalidQueryError{Query: q, Message: "source kind remote requires a remote origin"}
	}
	if q.Source.Kind != SourceRemote && q.Source.Remote != "" {
		return &InvalidQueryError{
			Query:   q,
			Message: fmt.Sprintf("remote origin is only valid with source kind remote, got %s", q.Source.Kind),
		}
	}
	if q.Digest != "" && !IsValidDigest(q.Digest) {
		return &InvalidQueryError{Query: q, Message: fmt.Sprintf("digest %q is not a valid OCI digest", q.Digest)}
	}
	if q.Version == "" || q.Version == "latest" {
		return nil
	}

	if _, err := semver.NewVersion(q.Version); err == nil {
		return nil
	}
	if _, err := semver.NewConstraint(q.Version); err == nil {
		return nil
	}

	return &InvalidQueryError{Query: q, Message: fmt.Sprintf("version %q is not a valid semantic version or constraint", q.Version)}
}

// matches reports whether an artifact satisfies the query.
func (q ArtifactQuery) matches(info ArtifactInfo) bool {
	if info.Reference.Name != q.Name {
		return false
	}
	if info.Reference.Kind != q.Kind {
		return false
	}
	if q.Source != (SourceSelector{}) && !q.Source.matches(info.Annotations) {
		return false
	}
	if q.Digest != "" && info.Digest != q.Digest {
		return false
	}
	var versionMatcher VersionOrConstraint
	versionMatcher.Set(q.Version)
	return versionMatcher.Satisfies(info.Annotations[AnnotationVersion])
}
