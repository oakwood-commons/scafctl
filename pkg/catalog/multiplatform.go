// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// SupportedPluginPlatforms is the list of platforms supported for plugin artifacts.
var SupportedPluginPlatforms = []string{
	"linux/amd64",
	"linux/arm64",
	"darwin/amd64",
	"darwin/arm64",
	"windows/amd64",
}

// PlatformBinary pairs a platform string (e.g. "linux/amd64") with the
// raw plugin binary for that platform.
type PlatformBinary struct {
	// Platform in OCI format, e.g. "linux/amd64".
	Platform string `json:"platform" yaml:"platform" doc:"Target platform in os/arch format"`

	// Data is the raw binary content.
	Data []byte `json:"-" yaml:"-"`
}

// PlatformToOCI converts a platform string like "linux/amd64" to an OCI Platform struct.
func PlatformToOCI(platform string) (*ocispec.Platform, error) {
	os, arch, err := parsePlatformParts(platform)
	if err != nil {
		return nil, err
	}
	return &ocispec.Platform{
		Architecture: arch,
		OS:           os,
	}, nil
}

// OCIPlatformString converts an OCI Platform struct to a "os/arch" string.
func OCIPlatformString(p *ocispec.Platform) string {
	if p == nil {
		return ""
	}
	return p.OS + "/" + p.Architecture
}

// MatchPlatform selects the manifest descriptor from an OCI image index
// that matches the requested platform. Returns an error if no match is found.
func MatchPlatform(index *ocispec.Index, platform string) (*ocispec.Descriptor, error) {
	if index == nil {
		return nil, fmt.Errorf("nil index")
	}

	targetOS, targetArch, err := parsePlatformParts(platform)
	if err != nil {
		return nil, err
	}

	for i := range index.Manifests {
		desc := &index.Manifests[i]
		if desc.Platform == nil {
			continue
		}
		if desc.Platform.OS == targetOS && desc.Platform.Architecture == targetArch {
			return desc, nil
		}
	}

	available := make([]string, 0, len(index.Manifests))
	for _, desc := range index.Manifests {
		if desc.Platform != nil {
			available = append(available, OCIPlatformString(desc.Platform))
		}
	}

	return nil, &PlatformNotFoundError{
		Platform:  platform,
		Available: available,
	}
}

// IndexPlatforms returns the list of platforms in an OCI image index.
func IndexPlatforms(index *ocispec.Index) []string {
	if index == nil {
		return nil
	}
	platforms := make([]string, 0, len(index.Manifests))
	for _, desc := range index.Manifests {
		if desc.Platform != nil {
			platforms = append(platforms, OCIPlatformString(desc.Platform))
		}
	}
	return platforms
}

// IsImageIndex returns true if the descriptor has image index media type.
func IsImageIndex(desc ocispec.Descriptor) bool {
	return desc.MediaType == ocispec.MediaTypeImageIndex
}

// parsePlatformParts splits "os/arch" and validates the format.
func parsePlatformParts(platform string) (os, arch string, err error) {
	// Reuse the same logic as plugin.ParsePlatform, but keep catalog
	// package self-contained to avoid circular imports.
	parts := splitPlatform(platform)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid platform format %q: expected os/arch (e.g., linux/amd64)", platform)
	}
	return parts[0], parts[1], nil
}

func splitPlatform(platform string) []string {
	parts := make([]string, 0, 2)
	idx := -1
	for i, c := range platform {
		if c == '/' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []string{platform}
	}
	parts = append(parts, platform[:idx], platform[idx+1:])
	return parts
}

// IsSupportedPlatform returns true if the platform string is in the supported list.
func IsSupportedPlatform(platform string) bool {
	for _, p := range SupportedPluginPlatforms {
		if p == platform {
			return true
		}
	}
	return false
}
