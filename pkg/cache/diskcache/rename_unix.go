// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package diskcache

import "os"

// atomicRename atomically replaces dst with src.
// On Unix, os.Rename is atomic and overwrites the target.
func atomicRename(src, dst string) error {
	return os.Rename(src, dst)
}
