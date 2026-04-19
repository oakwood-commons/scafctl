// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"time"

	"github.com/Masterminds/semver/v3"
)

// contextKey is a private type for catalog context keys.
type contextKey int

const (
	// includePreReleaseKey controls whether pre-release versions are considered
	// when resolving "latest" (no explicit version). By default, pre-release
	// versions are excluded.
	includePreReleaseKey contextKey = iota
)

// WithIncludePreRelease returns a context that includes pre-release versions
// when resolving the latest version from a catalog.
func WithIncludePreRelease(ctx context.Context) context.Context {
	return context.WithValue(ctx, includePreReleaseKey, true)
}

// IncludePreReleaseFromContext returns true if pre-release versions should be
// included when resolving the latest version.
func IncludePreReleaseFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(includePreReleaseKey).(bool)
	return v
}

// IsPreRelease returns true if the semver version has a pre-release suffix
// (e.g., 1.0.0-beta.1, 2.0.0-alpha).
func IsPreRelease(v *semver.Version) bool {
	return v != nil && v.Prerelease() != ""
}

// ArtifactKind represents the type of artifact stored in the catalog.
type ArtifactKind string

const (
	// ArtifactKindSolution represents a solution artifact.
	ArtifactKindSolution ArtifactKind = "solution"

	// ArtifactKindProvider represents a provider artifact (go-plugin binary exposing providers).
	ArtifactKindProvider ArtifactKind = "provider"

	// ArtifactKindAuthHandler represents an auth handler artifact (go-plugin binary exposing auth handlers).
	ArtifactKindAuthHandler ArtifactKind = "auth-handler"
)

// String returns the string representation of the artifact kind.
func (k ArtifactKind) String() string {
	return string(k)
}

// IsValid returns true if the artifact kind is valid.
func (k ArtifactKind) IsValid() bool {
	switch k {
	case ArtifactKindSolution, ArtifactKindProvider, ArtifactKindAuthHandler:
		return true
	default:
		return false
	}
}

// Plural returns the pluralized form of the artifact kind (for repository paths).
func (k ArtifactKind) Plural() string {
	switch k {
	case ArtifactKindSolution:
		return "solutions"
	case ArtifactKindProvider:
		return "providers"
	case ArtifactKindAuthHandler:
		return "auth-handlers"
	default:
		return string(k) + "s"
	}
}

// ParseArtifactKind parses a string into an ArtifactKind.
// Returns empty string and false if the input is not a valid kind.
func ParseArtifactKind(s string) (ArtifactKind, bool) {
	kind := ArtifactKind(s)
	return kind, kind.IsValid()
}

// ParseArtifactKindFromPlural parses a pluralized path segment into an ArtifactKind.
// E.g., "solutions" -> ArtifactKindSolution, "providers" -> ArtifactKindProvider
func ParseArtifactKindFromPlural(s string) (ArtifactKind, bool) {
	switch s {
	case "solutions":
		return ArtifactKindSolution, true
	case "providers":
		return ArtifactKindProvider, true
	case "auth-handlers":
		return ArtifactKindAuthHandler, true
	default:
		return "", false
	}
}

// Reference uniquely identifies an artifact in the catalog.
type Reference struct {
	// Kind is the type of artifact (solution, provider, or auth-handler).
	Kind ArtifactKind `json:"kind" yaml:"kind" doc:"Artifact type"`

	// Name is the artifact identifier (e.g., "my-solution").
	Name string `json:"name" yaml:"name" doc:"Artifact name"`

	// Version is the semantic version (e.g., 1.2.3). Nil means "latest".
	Version *semver.Version `json:"version,omitempty" yaml:"version,omitempty" doc:"Semantic version"`

	// Digest is the content digest for pinning (e.g., "sha256:abc123...").
	// If set, takes precedence over Version for resolution.
	Digest string `json:"digest,omitempty" yaml:"digest,omitempty" doc:"Content digest for pinning"`
}

// String returns the canonical reference string (e.g., "my-solution@1.2.3").
func (r Reference) String() string {
	if r.Digest != "" {
		return r.Name + "@" + r.Digest
	}
	if r.Version != nil {
		return r.Name + "@" + r.Version.String()
	}
	return r.Name
}

// HasVersion returns true if the reference has a version specified.
func (r Reference) HasVersion() bool {
	return r.Version != nil
}

// HasDigest returns true if the reference has a digest specified.
func (r Reference) HasDigest() bool {
	return r.Digest != ""
}

// VersionOrDigest returns a display string for the reference identifier.
// Prefers Version.String() when set, falls back to Digest, then "unknown".
func (r Reference) VersionOrDigest() string {
	if r.Version != nil {
		return r.Version.String()
	}
	if r.Digest != "" {
		return r.Digest
	}
	return "unknown"
}

// ArtifactInfo contains metadata about a stored artifact.
type ArtifactInfo struct {
	// Reference is the artifact identifier.
	Reference Reference `json:"reference" yaml:"reference" doc:"Artifact reference"`

	// Tag is the OCI tag label (e.g. "1.0.0", "stable", "latest").
	// For version tags this matches Reference.Version; for aliases it holds
	// the alias string.
	Tag string `json:"tag" yaml:"tag" doc:"OCI tag label"`

	// Digest is the content digest (sha256:...).
	Digest string `json:"digest" yaml:"digest" doc:"Content digest"`

	// CreatedAt is when the artifact was stored.
	CreatedAt time.Time `json:"createdAt" yaml:"createdAt" doc:"Storage timestamp"`

	// Size is the artifact size in bytes.
	Size int64 `json:"size" yaml:"size" doc:"Size in bytes"`

	// Annotations are OCI annotations from the manifest.
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty" doc:"OCI annotations"`

	// Catalog is the name of the catalog this artifact came from.
	Catalog string `json:"catalog" yaml:"catalog" doc:"Source catalog name"`
}

// Catalog defines the interface for artifact storage.
// Both local and remote catalogs implement this interface.
type Catalog interface {
	// Name returns the catalog identifier (e.g., "local", "company-registry").
	Name() string

	// Store saves an artifact to the catalog.
	// For solutions with bundled files, bundleData contains the tar archive.
	// If bundleData is nil, only the primary content layer is stored.
	// Returns ErrArtifactExists if the version already exists (use force to overwrite).
	Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool) (ArtifactInfo, error)

	// Fetch retrieves an artifact's primary content from the catalog.
	// Returns ErrArtifactNotFound if the artifact doesn't exist.
	Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error)

	// FetchWithBundle retrieves an artifact's primary content and bundle layer.
	// The bundle layer contains bundled files as a tar archive.
	// If the artifact has no bundle layer, bundleData is nil.
	// Returns ErrArtifactNotFound if the artifact doesn't exist.
	FetchWithBundle(ctx context.Context, ref Reference) (content, bundleData []byte, info ArtifactInfo, err error)

	// Resolve finds the best matching version for a reference.
	// If no version is specified, returns the highest semver version.
	// Returns ErrArtifactNotFound if no matching artifact exists.
	Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error)

	// List returns all artifacts matching the criteria.
	// If name is empty, returns all artifacts of the specified kind.
	List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error)

	// Exists checks if an artifact exists in the catalog.
	Exists(ctx context.Context, ref Reference) (bool, error)

	// Delete removes an artifact from the catalog.
	// Returns ErrArtifactNotFound if the artifact doesn't exist.
	Delete(ctx context.Context, ref Reference) error
}
