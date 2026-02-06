package catalog

// OCI media types for scafctl artifacts.
const (
	// MediaTypeSolutionManifest is the manifest media type for solution artifacts.
	MediaTypeSolutionManifest = "application/vnd.oci.image.manifest.v1+json"

	// MediaTypeSolutionContent is the content layer media type for solution YAML.
	MediaTypeSolutionContent = "application/vnd.scafctl.solution.v1+yaml"

	// MediaTypeSolutionConfig is the config blob media type for solution metadata.
	MediaTypeSolutionConfig = "application/vnd.scafctl.solution.config.v1+json"

	// MediaTypePluginManifest is the manifest media type for plugin artifacts.
	MediaTypePluginManifest = "application/vnd.oci.image.manifest.v1+json"

	// MediaTypePluginBinary is the content layer media type for plugin binaries.
	MediaTypePluginBinary = "application/vnd.scafctl.plugin.v1+binary"

	// MediaTypePluginConfig is the config blob media type for plugin metadata.
	MediaTypePluginConfig = "application/vnd.scafctl.plugin.config.v1+json"
)

// MediaTypeForKind returns the content media type for an artifact kind.
func MediaTypeForKind(kind ArtifactKind) string {
	switch kind {
	case ArtifactKindSolution:
		return MediaTypeSolutionContent
	case ArtifactKindPlugin:
		return MediaTypePluginBinary
	default:
		return "application/octet-stream"
	}
}

// ConfigMediaTypeForKind returns the config media type for an artifact kind.
func ConfigMediaTypeForKind(kind ArtifactKind) string {
	switch kind {
	case ArtifactKindSolution:
		return MediaTypeSolutionConfig
	case ArtifactKindPlugin:
		return MediaTypePluginConfig
	default:
		return "application/vnd.oci.image.config.v1+json"
	}
}
