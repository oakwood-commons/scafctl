// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package metadataprovider

// parentProcessName is a no-op on non-Windows platforms.
// On Unix, $SHELL is the authoritative source for the user's shell.
func parentProcessName() string {
	return ""
}
