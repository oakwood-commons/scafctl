// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package format provides human-readable formatting utilities for bytes and durations.
// These functions are used across CLI output, progress reporting, and metrics display.
package format

import (
	"fmt"
	"time"
)

// Bytes formats a byte count as a human-readable string (e.g., "1.5 KB", "3.2 MB").
// Uses binary units (1 KB = 1024 bytes).
func Bytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Duration formats a time.Duration as a human-readable string.
// Returns microseconds for sub-millisecond, milliseconds for sub-second,
// and seconds (with 2 decimal places) for longer durations.
func Duration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
