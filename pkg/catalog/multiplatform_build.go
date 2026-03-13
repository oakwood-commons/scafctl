// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ValidatePluginKind validates that the given kind string is a valid plugin kind
// (provider or auth-handler). Returns the parsed ArtifactKind or an error.
func ValidatePluginKind(kindStr string) (ArtifactKind, error) {
	kind, ok := ParseArtifactKind(kindStr)
	if !ok || (kind != ArtifactKindProvider && kind != ArtifactKindAuthHandler) {
		return "", fmt.Errorf("invalid kind %q for plugin build; must be 'provider' or 'auth-handler'", kindStr)
	}
	return kind, nil
}

// ReadPlatformBinaries validates platform names, resolves file paths to absolute paths,
// stats each file (rejecting directories), reads binary data, and returns PlatformBinary
// entries ready for storage.
//
// platformPaths maps platform strings (e.g., "linux/amd64") to file paths.
// Returns an error if any platform is unsupported, path doesn't exist,
// path is a directory, or file data is empty or unreadable.
func ReadPlatformBinaries(ctx context.Context, platformPaths map[string]string) ([]PlatformBinary, error) {
	if len(platformPaths) == 0 {
		return nil, fmt.Errorf("no platform binaries provided")
	}

	binaries := make([]PlatformBinary, 0, len(platformPaths))

	for platform, binPath := range platformPaths {
		if binPath == "" {
			return nil, fmt.Errorf("platform %q: binary path must be a non-empty string", platform)
		}

		if !IsSupportedPlatform(platform) {
			return nil, fmt.Errorf("unsupported platform %q; supported: %s", platform, strings.Join(SupportedPluginPlatforms, ", "))
		}

		absPath, err := provider.AbsFromContext(ctx, binPath)
		if err != nil {
			return nil, fmt.Errorf("platform %q: invalid path %q: %w", platform, binPath, err)
		}

		fi, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("platform %q: binary not found at %q: %w", platform, absPath, err)
		}
		if fi.IsDir() {
			return nil, fmt.Errorf("platform %q: path %q is a directory, not a binary", platform, absPath)
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("platform %q: failed to read binary: %w", platform, err)
		}

		if len(data) == 0 {
			return nil, fmt.Errorf("platform %q: binary at %q is empty", platform, absPath)
		}

		binaries = append(binaries, PlatformBinary{
			Platform: platform,
			Data:     data,
		})
	}

	return binaries, nil
}
