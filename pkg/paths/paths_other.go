// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package paths

// applyDefaults is a no-op on non-darwin platforms.
// On darwin, this overrides the adrg/xdg library defaults to use
// CLI tool conventions instead of GUI conventions.
func applyDefaults() {}
