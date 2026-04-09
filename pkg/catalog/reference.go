// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// ParseReference parses a reference string into a Reference struct.
// Supported formats:
//   - "name" - artifact name only (version resolved to latest)
//   - "name@1.2.3" - artifact with specific version
//   - "name@sha256:abc..." - artifact with specific digest
func ParseReference(kind ArtifactKind, input string) (Reference, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Reference{}, &InvalidReferenceError{
			Input:   input,
			Message: "reference cannot be empty",
		}
	}

	ref := Reference{Kind: kind}

	// Check for @ separator
	atIdx := strings.LastIndex(input, "@")
	if atIdx == -1 {
		// No version specified
		if err := validateName(input); err != nil {
			return Reference{}, err
		}
		ref.Name = input
		return ref, nil
	}

	// Split into name and version/digest
	name := input[:atIdx]
	versionOrDigest := input[atIdx+1:]

	if err := validateName(name); err != nil {
		return Reference{}, err
	}
	ref.Name = name

	if versionOrDigest == "" {
		return Reference{}, &InvalidReferenceError{
			Input:   input,
			Message: "version or digest cannot be empty after @",
		}
	}

	// Check if it's a digest (starts with algorithm:)
	if strings.Contains(versionOrDigest, ":") {
		if !IsValidDigest(versionOrDigest) {
			return Reference{}, &InvalidReferenceError{
				Input:   input,
				Message: "invalid digest format (expected sha256:...)",
			}
		}
		ref.Digest = versionOrDigest
		return ref, nil
	}

	// "latest" is a virtual alias that resolves to the highest semver version.
	// Treat it as no version specified so the resolver picks the newest.
	if strings.EqualFold(versionOrDigest, "latest") {
		return ref, nil
	}

	// Parse as semver
	v, err := semver.NewVersion(versionOrDigest)
	if err != nil {
		return Reference{}, &InvalidReferenceError{
			Input:   input,
			Message: "invalid version: " + err.Error(),
		}
	}
	ref.Version = v

	return ref, nil
}

// validateName checks if an artifact name is valid.
func validateName(name string) error {
	if name == "" {
		return &InvalidReferenceError{
			Input:   name,
			Message: "name cannot be empty",
		}
	}

	// Name pattern: lowercase alphanumeric with hyphens, must start with letter
	if !IsValidName(name) {
		return &InvalidReferenceError{
			Input:   name,
			Message: "name must be lowercase alphanumeric with hyphens (e.g., 'my-solution')",
		}
	}

	return nil
}

// IsValidName checks if a name follows the naming convention.
// Names must be lowercase alphanumeric with hyphens, starting with a letter.
func IsValidName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}

	// Must start with a letter
	first := name[0]
	if first < 'a' || first > 'z' {
		return false
	}

	// Must end with alphanumeric
	last := name[len(name)-1]
	isLastValid := (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')
	if !isLastValid {
		return false
	}

	// All characters must be lowercase alphanumeric or hyphen
	for _, c := range name {
		isValid := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
		if !isValid {
			return false
		}
	}

	// No double hyphens
	if strings.Contains(name, "--") {
		return false
	}

	return true
}

// IsValidDigest checks if a string is a valid OCI digest.
func IsValidDigest(digest string) bool {
	// Format: algorithm:hex
	parts := strings.SplitN(digest, ":", 2)
	if len(parts) != 2 {
		return false
	}

	algorithm := parts[0]
	hex := parts[1]

	// Currently only sha256 is supported
	if algorithm != "sha256" {
		return false
	}

	// SHA256 is 64 hex characters
	if len(hex) != 64 {
		return false
	}

	// All characters must be hex
	for _, c := range hex {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHex {
			return false
		}
	}

	return true
}

// RemoteReference represents a parsed remote registry reference.
type RemoteReference struct {
	// Registry is the registry host (e.g., "ghcr.io", "docker.io")
	Registry string

	// Repository is the repository path (e.g., "myorg/scafctl")
	Repository string

	// Kind is the artifact kind (solution, provider, or auth-handler)
	Kind ArtifactKind

	// Name is the artifact name
	Name string

	// Tag is the version tag or digest
	Tag string
}

// ParseRemoteReference parses a full remote reference URL.
// Supported formats:
//   - "ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0"
//   - "oci://ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0"
//   - "docker.io/myorg/my-solution:1.0.0" (Docker Hub style)
//
// Returns the registry, repository, name, and version/tag.
func ParseRemoteReference(input string) (*RemoteReference, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, &InvalidReferenceError{
			Input:   input,
			Message: "remote reference cannot be empty",
		}
	}

	// Remove oci:// scheme if present
	input = strings.TrimPrefix(input, "oci://")

	// Split by @ or : to separate tag/digest
	var tag string
	atIdx := strings.LastIndex(input, "@")
	colonIdx := strings.LastIndex(input, ":")

	if atIdx != -1 {
		// @ separator (digest or version)
		tag = input[atIdx+1:]
		input = input[:atIdx]
	} else if colonIdx != -1 {
		// Check if colon is for port or tag
		// If it's after the first /, it's a tag
		slashIdx := strings.Index(input, "/")
		if slashIdx != -1 && colonIdx > slashIdx {
			tag = input[colonIdx+1:]
			input = input[:colonIdx]
		}
	}

	// Split by / to get parts
	parts := strings.Split(input, "/")
	if len(parts) < 2 {
		return nil, &InvalidReferenceError{
			Input:   input,
			Message: "remote reference must include registry and repository",
		}
	}

	ref := &RemoteReference{
		Registry: parts[0],
		Tag:      tag,
	}

	// Detect artifact kind from path
	// Format: registry/[repo/...]/solutions/name or registry/[repo/...]/providers/name
	for i := 1; i < len(parts)-1; i++ {
		if kind, ok := ParseArtifactKindFromPlural(parts[i]); ok {
			ref.Kind = kind
			ref.Repository = strings.Join(parts[1:i], "/")
			ref.Name = parts[i+1]
			return ref, nil
		}
	}

	// No explicit kind in path - treat last part as name, rest as repository
	ref.Name = parts[len(parts)-1]
	ref.Repository = strings.Join(parts[1:len(parts)-1], "/")

	return ref, nil
}

// ToReference converts a RemoteReference to a Reference.
func (r *RemoteReference) ToReference() (Reference, error) {
	ref := Reference{
		Kind: r.Kind,
		Name: r.Name,
	}

	if r.Tag == "" || strings.EqualFold(r.Tag, "latest") {
		return ref, nil
	}

	// Check if tag is a digest
	if strings.HasPrefix(r.Tag, "sha256:") {
		if !IsValidDigest(r.Tag) {
			return Reference{}, &InvalidReferenceError{
				Input:   r.Tag,
				Message: "invalid digest format",
			}
		}
		ref.Digest = r.Tag
		return ref, nil
	}

	// Parse as semver
	v, err := semver.NewVersion(r.Tag)
	if err != nil {
		return Reference{}, &InvalidReferenceError{
			Input:   r.Tag,
			Message: "invalid version: " + err.Error(),
		}
	}
	ref.Version = v

	return ref, nil
}

// String returns the full remote reference string.
func (r *RemoteReference) String() string {
	var sb strings.Builder
	sb.WriteString(r.Registry)
	if r.Repository != "" {
		sb.WriteString("/")
		sb.WriteString(r.Repository)
	}
	if r.Kind != "" {
		sb.WriteString("/")
		sb.WriteString(r.Kind.Plural())
	}
	sb.WriteString("/")
	sb.WriteString(r.Name)
	if r.Tag != "" {
		sb.WriteString("@")
		sb.WriteString(r.Tag)
	}
	return sb.String()
}

// NormalizeRegistryHost normalizes common registry hostnames.
func NormalizeRegistryHost(host string) string {
	switch host {
	case "docker.io", "index.docker.io", "registry-1.docker.io":
		return "docker.io"
	default:
		return host
	}
}

// ValidateAlias checks that an alias tag is valid for use as a non-version tag.
// It must not be empty, must not be a valid semver version, and must only contain
// characters valid in OCI tags ([a-zA-Z0-9_.-]).
func ValidateAlias(alias string) error {
	if alias == "" {
		return fmt.Errorf("alias tag cannot be empty")
	}

	// "latest" is a reserved virtual alias (auto-resolves to highest semver)
	if strings.EqualFold(alias, "latest") {
		return fmt.Errorf("alias %q is reserved; it automatically resolves to the highest semver version", alias)
	}

	// Must not be purely numeric (confusing with versions)
	if isNumericOnly(alias) {
		return fmt.Errorf("alias %q is purely numeric and could be confused with a version; use a descriptive name like 'v%s' instead", alias, alias)
	}

	// Must not be a valid semver version (those should be created via build)
	if _, err := ParseReference(ArtifactKindSolution, "x@"+alias); err == nil {
		return fmt.Errorf("alias %q looks like a semver version; use '%s build' to create versioned artifacts", alias, paths.AppName())
	}

	// OCI tag constraints: must match [a-zA-Z0-9_.-]+
	for _, ch := range alias {
		if !IsValidTagChar(ch) {
			return fmt.Errorf("alias %q contains invalid character %q; valid characters: letters, digits, '.', '-', '_'", alias, string(ch))
		}
	}

	// Must not start with a dot or hyphen
	if strings.HasPrefix(alias, ".") || strings.HasPrefix(alias, "-") {
		return fmt.Errorf("alias %q must start with a letter, digit, or underscore", alias)
	}

	return nil
}

// IsValidTagChar returns true if the rune is valid for an OCI tag.
func IsValidTagChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_' || ch == '.' || ch == '-'
}

// isNumericOnly returns true if the string contains only digits.
func isNumericOnly(s string) bool {
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
