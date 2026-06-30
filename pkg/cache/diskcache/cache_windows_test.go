//go:build windows

package diskcache

import "testing"

// These tests exercise behavior when the cache base directory is made
// read-only via POSIX permission bits (os.Chmod 0o555). Windows does not honor
// those bits the same way, so the permission-error paths cannot be reproduced
// reliably. The helpers are provided as skipping stubs so the shared tests in
// cache_test.go compile and run on Windows.

func testRemoveEntryPermissionError(t *testing.T) {
	t.Helper()
	t.Skip("permission-error path relies on POSIX mode bits; not reproducible on Windows")
}

func testEvictFromBackPermissionError(t *testing.T) {
	t.Helper()
	t.Skip("permission-error path relies on POSIX mode bits; not reproducible on Windows")
}

func testOnEvictedNotCalledOnPermissionError(t *testing.T) {
	t.Helper()
	t.Skip("permission-error path relies on POSIX mode bits; not reproducible on Windows")
}

func testDeletePermissionError(t *testing.T) {
	t.Helper()
	t.Skip("permission-error path relies on POSIX mode bits; not reproducible on Windows")
}
