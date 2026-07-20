// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ProviderRef is a raw provider reference string as it appears in a solution's
// `provider:` field. It supports three forms:
//
//	echo                                     bare name (alias / chain lookup)
//	echo@^1.0.0                              name + version constraint
//	registry.example.com/myorg/echo@^1.0.0   fully-qualified (registry + name + version)
//
// ProviderRef is only a lexical container. It performs no semver validation and
// no catalog resolution; Parse merely splits the string into its sections.
type ProviderRef string

// ProviderRefParts is the decomposed view of a ProviderRef.
//
// The '@' character separates the name from the version/digest. At most one of
// Version or Digest is set (neither, for a bare reference). Registry is the OCI
// prefix up to and excluding the final path segment; it is empty for an
// unqualified reference.
type ProviderRefParts struct {
	// Registry is the OCI prefix (host[:port] plus optional namespace path),
	// e.g. "registry.example.com:5000/myorg". Empty when the reference is
	// unqualified (bare or name@version).
	Registry string

	// Name is the final path segment, e.g. "echo".
	Name string

	// Version is the raw version or constraint after '@' (e.g. "^1.0.0",
	// "1.2.3", "latest"). Empty when unspecified or when Digest is set.
	Version string

	// Digest is the raw content digest after '@' (e.g. "sha256:abc..."). Empty
	// when unspecified or when Version is set.
	Digest string
}

// IsQualified reports whether the reference names an explicit registry.
func (p ProviderRefParts) IsQualified() bool { return p.Registry != "" }

// String reassembles the canonical reference string. It round-trips the output
// of Parse for any valid input.
func (p ProviderRefParts) String() string {
	var b strings.Builder
	if p.Registry != "" {
		b.WriteString(p.Registry)
		b.WriteByte('/')
	}
	b.WriteString(p.Name)
	switch {
	case p.Digest != "":
		b.WriteByte('@')
		b.WriteString(p.Digest)
	case p.Version != "":
		b.WriteByte('@')
		b.WriteString(p.Version)
	}
	return b.String()
}

// Parse splits the reference into its sections. It returns an error only for
// structurally invalid input (empty string, an empty section around a
// separator, or a '/' whose first segment is not a registry host). It does NOT
// validate that Name matches the artifact naming rules or that Version is valid
// semver -- those are separate, later concerns.
func (r ProviderRef) Parse() (ProviderRefParts, error) {
	s := strings.TrimSpace(string(r))
	if s == "" {
		return ProviderRefParts{}, fmt.Errorf("provider reference cannot be empty")
	}

	var parts ProviderRefParts

	// 1. Peel off the version/digest. '@' only ever separates name from
	//    version/digest: a registry port uses ':' and a digest uses ':', never
	//    '@', so LastIndex is unambiguous.
	if at := strings.LastIndex(s, "@"); at != -1 {
		name := s[:at]
		verOrDigest := s[at+1:]
		if verOrDigest == "" {
			return ProviderRefParts{}, fmt.Errorf("provider reference %q: version or digest is empty after '@'", r)
		}
		if isDigest(verOrDigest) {
			parts.Digest = verOrDigest
		} else {
			parts.Version = verOrDigest
		}
		s = name
		if s == "" {
			return ProviderRefParts{}, fmt.Errorf("provider reference %q: name is empty before '@'", r)
		}
	}

	// 2. Peel off the registry prefix. The reference is registry-qualified only
	//    when the first path segment looks like a host (contains '.' or ':', or
	//    is "localhost"). Otherwise the whole remainder is the name.
	if slash := strings.LastIndex(s, "/"); slash != -1 {
		firstSeg := s[:strings.IndexByte(s, '/')]
		if looksLikeRegistryHost(firstSeg) {
			registry := s[:slash]
			name := s[slash+1:]
			if registry == "" || name == "" {
				return ProviderRefParts{}, fmt.Errorf("provider reference %q: empty registry or name section", r)
			}
			parts.Registry = registry
			parts.Name = name
			return parts, nil
		}
		// A '/' with a non-host first segment is not a valid provider name.
		return ProviderRefParts{}, fmt.Errorf("provider reference %q: %q is not a registry host and names may not contain '/'", r, firstSeg)
	}

	parts.Name = s
	return parts, nil
}

// versionLatest is the virtual version alias that resolves to the highest
// available version. It bypasses semver-constraint validation.
const versionLatest = "latest"

// Validate checks that the parsed sections are well-formed. Parse only splits;
// Validate enforces the section-level rules:
//
//   - Name must be a valid artifact name (lowercase alphanumeric with single
//     hyphens, starting with a letter, ending alphanumeric).
//   - Version, when set and not "latest", must be a valid semver constraint
//     (e.g. "1.2.3", "^1.0.0", ">=2.0.0").
//   - Digest, when set, must be a valid OCI digest ("sha256:" + 64 hex chars).
//   - Registry, when set, must have a host-like first segment.
//
// Validate does not perform any catalog resolution or network I/O.
func (p ProviderRefParts) Validate() error {
	if !isValidArtifactName(p.Name) {
		return fmt.Errorf("invalid provider name %q: must be lowercase alphanumeric with single hyphens, start with a letter, and end with a letter or digit", p.Name)
	}

	if p.Registry != "" {
		firstSeg := p.Registry
		if slash := strings.IndexByte(firstSeg, '/'); slash != -1 {
			firstSeg = firstSeg[:slash]
		}
		if !looksLikeRegistryHost(firstSeg) {
			return fmt.Errorf("invalid registry %q: first segment %q is not a host (needs a '.', ':', or be 'localhost')", p.Registry, firstSeg)
		}
	}

	switch {
	case p.Digest != "":
		if !isValidDigest(p.Digest) {
			return fmt.Errorf("invalid digest %q: expected sha256:<64 hex chars>", p.Digest)
		}
	case p.Version != "" && !strings.EqualFold(p.Version, versionLatest):
		if _, err := semver.NewConstraint(p.Version); err != nil {
			return fmt.Errorf("invalid version constraint %q: %w", p.Version, err)
		}
	}

	return nil
}

// isValidArtifactName reports whether name follows the artifact naming
// convention: lowercase alphanumeric with single hyphens, starting with a
// letter, ending with a letter or digit, at most 128 characters. This mirrors
// catalog.IsValidName without importing the heavier catalog package into spec.
func isValidArtifactName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	if first := name[0]; first < 'a' || first > 'z' {
		return false
	}
	last := name[len(name)-1]
	if !((last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')) {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return !strings.Contains(name, "--")
}

// isValidDigest reports whether digest is a valid OCI digest of the form
// "sha256:<64 lowercase hex chars>". This mirrors catalog.IsValidDigest.
func isValidDigest(digest string) bool {
	const shaPrefix = "sha256:"
	if !strings.HasPrefix(digest, shaPrefix) {
		return false
	}
	hex := digest[len(shaPrefix):]
	if len(hex) != 64 {
		return false
	}
	for _, c := range hex {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// isDigest reports whether s is an OCI digest of the form "algo:hex".
func isDigest(s string) bool {
	i := strings.IndexByte(s, ':')
	return i > 0 && i < len(s)-1
}

// looksLikeRegistryHost mirrors the host-detection heuristic used for catalog
// URLs: a host contains a '.' (domain) or ':' (port), or is exactly
// "localhost".
func looksLikeRegistryHost(seg string) bool {
	return seg == "localhost" || strings.ContainsAny(seg, ".:")
}
