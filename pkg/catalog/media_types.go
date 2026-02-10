// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

// OCI media types for scafctl artifacts.
const (
	// MediaTypeSolutionManifest is the manifest media type for solution artifacts.
	MediaTypeSolutionManifest = "application/vnd.oci.image.manifest.v1+json"

	// MediaTypeSolutionContent is the content layer media type for solution YAML.
	MediaTypeSolutionContent = "application/vnd.scafctl.solution.v1+yaml"

	// MediaTypeSolutionConfig is the config blob media type for solution metadata.
	MediaTypeSolutionConfig = "application/vnd.scafctl.solution.config.v1+json"

	// MediaTypeSolutionBundle is the content layer media type for solution bundle tar archives.
	MediaTypeSolutionBundle = "application/vnd.scafctl.solution.bundle.v1+tar"

	// MediaTypeSolutionBundleManifest is the media type for deduplicated bundle manifests (v2).
	MediaTypeSolutionBundleManifest = "application/vnd.scafctl.solution.bundle-manifest.v2+json"

	// MediaTypeSolutionBundleBlob is the media type for individual file blobs in deduplicated bundles (v2).
	MediaTypeSolutionBundleBlob = "application/vnd.scafctl.solution.bundle-blob.v2+octet-stream"

	// MediaTypeSolutionBundleSmallTar is the media type for grouped small files in deduplicated bundles (v2).
	MediaTypeSolutionBundleSmallTar = "application/vnd.scafctl.solution.bundle-small.v2+tar"

	// MediaTypeProviderManifest is the manifest media type for provider artifacts.
	MediaTypeProviderManifest = "application/vnd.oci.image.manifest.v1+json"

	// MediaTypeProviderBinary is the content layer media type for provider binaries.
	MediaTypeProviderBinary = "application/vnd.scafctl.provider.v1+binary"

	// MediaTypeProviderConfig is the config blob media type for provider metadata.
	MediaTypeProviderConfig = "application/vnd.scafctl.provider.config.v1+json"

	// MediaTypeAuthHandlerManifest is the manifest media type for auth handler artifacts.
	MediaTypeAuthHandlerManifest = "application/vnd.oci.image.manifest.v1+json"

	// MediaTypeAuthHandlerBinary is the content layer media type for auth handler binaries.
	MediaTypeAuthHandlerBinary = "application/vnd.scafctl.auth-handler.v1+binary"

	// MediaTypeAuthHandlerConfig is the config blob media type for auth handler metadata.
	MediaTypeAuthHandlerConfig = "application/vnd.scafctl.auth-handler.config.v1+json"
)

// MediaTypeForKind returns the content media type for an artifact kind.
func MediaTypeForKind(kind ArtifactKind) string {
	switch kind {
	case ArtifactKindSolution:
		return MediaTypeSolutionContent
	case ArtifactKindProvider:
		return MediaTypeProviderBinary
	case ArtifactKindAuthHandler:
		return MediaTypeAuthHandlerBinary
	default:
		return "application/octet-stream"
	}
}

// ConfigMediaTypeForKind returns the config media type for an artifact kind.
func ConfigMediaTypeForKind(kind ArtifactKind) string {
	switch kind {
	case ArtifactKindSolution:
		return MediaTypeSolutionConfig
	case ArtifactKindProvider:
		return MediaTypeProviderConfig
	case ArtifactKindAuthHandler:
		return MediaTypeAuthHandlerConfig
	default:
		return "application/vnd.oci.image.config.v1+json"
	}
}
