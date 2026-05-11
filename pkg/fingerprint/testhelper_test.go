// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"os"
	"path/filepath"
	"testing"
)

// testWriteFile creates a file at the relative path within baseDir.
func testWriteFile(tb testing.TB, base, relPath, content string) {
	tb.Helper()
	abs := filepath.Join(base, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		tb.Fatal(err)
	}
}
