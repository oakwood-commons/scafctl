// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import "strings"

// OCI annotation keys.
const (
	// Standard OCI annotations
	// See: https://github.com/opencontainers/image-spec/blob/main/annotations.md

	// AnnotationTitle is the human-readable title.
	AnnotationTitle = "org.opencontainers.image.title"

	// AnnotationDescription is a longer description.
	AnnotationDescription = "org.opencontainers.image.description"

	// AnnotationVersion is the version (semver).
	AnnotationVersion = "org.opencontainers.image.version"

	// AnnotationCreated is the creation timestamp (RFC 3339).
	AnnotationCreated = "org.opencontainers.image.created"

	// AnnotationAuthors is the contact for the image.
	AnnotationAuthors = "org.opencontainers.image.authors"

	// AnnotationSource is the URL to get source code.
	AnnotationSource = "org.opencontainers.image.source"

	// AnnotationDocumentation is the URL for documentation.
	AnnotationDocumentation = "org.opencontainers.image.documentation"

	// AnnotationVendor is the vendor name.
	AnnotationVendor = "org.opencontainers.image.vendor"

	// scafctl-specific annotations

	// AnnotationArtifactType is the scafctl artifact type ("solution", "provider", or "auth-handler").
	AnnotationArtifactType = "dev.scafctl.artifact.type"

	// AnnotationArtifactName is the artifact name.
	AnnotationArtifactName = "dev.scafctl.artifact.name"

	// AnnotationCategory is the solution category.
	AnnotationCategory = "dev.scafctl.solution.category"

	// AnnotationTags is a comma-separated list of tags.
	AnnotationTags = "dev.scafctl.solution.tags"

	// AnnotationMaintainers is a JSON array of maintainer objects.
	AnnotationMaintainers = "dev.scafctl.solution.maintainers"

	// AnnotationRequires is the dependency specifications (future).
	AnnotationRequires = "dev.scafctl.solution.requires"

	// AnnotationDisplayName is the human-friendly display name.
	AnnotationDisplayName = "dev.scafctl.solution.displayName"

	// AnnotationProviders is a comma-separated list of provider names (provider artifacts only).
	AnnotationProviders = "dev.scafctl.plugin.providers"

	// AnnotationPlatform is the target platform (provider/auth-handler artifacts only, e.g., "linux/amd64").
	AnnotationPlatform = "dev.scafctl.plugin.platform"

	// AnnotationOrigin records how a local artifact was obtained.
	// Values: "built", "pulled from <catalog>", "auto-cached from <catalog>".
	// This key carries provenance metadata only. Its exact storage location
	// depends on the catalog implementation, so it must not be assumed to be
	// descriptor-only or digest-stable.
	AnnotationOrigin = "dev.scafctl.artifact.origin"
)

// AnnotationBuilder helps construct annotation maps.
type AnnotationBuilder struct {
	annotations map[string]string
}

// NewAnnotationBuilder creates a new annotation builder.
func NewAnnotationBuilder() *AnnotationBuilder {
	return &AnnotationBuilder{
		annotations: make(map[string]string),
	}
}

// Set adds an annotation if the value is non-empty.
func (b *AnnotationBuilder) Set(key, value string) *AnnotationBuilder {
	if value != "" {
		b.annotations[key] = value
	}
	return b
}

// SetTags adds tags as a comma-separated annotation.
func (b *AnnotationBuilder) SetTags(tags []string) *AnnotationBuilder {
	if len(tags) > 0 {
		b.annotations[AnnotationTags] = strings.Join(tags, ",")
	}
	return b
}

// Build returns the annotation map.
func (b *AnnotationBuilder) Build() map[string]string {
	return b.annotations
}

// GetTags parses the tags annotation into a slice.
func GetTags(annotations map[string]string) []string {
	tags := annotations[AnnotationTags]
	if tags == "" {
		return nil
	}
	return strings.Split(tags, ",")
}
