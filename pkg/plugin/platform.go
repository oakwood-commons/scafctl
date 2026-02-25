// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"fmt"
	"runtime"
	"strings"
)

// CurrentPlatform returns the current OS/architecture in OCI platform format
// (e.g., "linux/amd64", "darwin/arm64").
func CurrentPlatform() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}

// ParsePlatform splits an OCI platform string into OS and architecture.
// Returns an error if the format is invalid.
func ParsePlatform(platform string) (os, arch string, err error) {
	parts := strings.SplitN(platform, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid platform format %q: expected os/arch (e.g., linux/amd64)", platform)
	}
	return parts[0], parts[1], nil
}

// PlatformCacheKey returns a filesystem-safe key for the platform
// (e.g., "linux-amd64" from "linux/amd64").
func PlatformCacheKey(platform string) string {
	return strings.ReplaceAll(platform, "/", "-")
}
