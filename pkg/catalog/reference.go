package catalog

import (
	"strings"

	"github.com/Masterminds/semver/v3"
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

// MustParseReference parses a reference string and panics on error.
// Only use in tests or with known-valid input.
func MustParseReference(kind ArtifactKind, input string) Reference {
	ref, err := ParseReference(kind, input)
	if err != nil {
		panic(err)
	}
	return ref
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
