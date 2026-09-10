// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"os"
	"path/filepath"
	"runtime"
)

// TestDataRoot is the absolute path to e2e/testdata, resolved once at
// package-init time via the caller's own source file location (not the
// process's working directory, which `go test` sets to the calling
// package's directory and so varies by which suite is running). Mirrors
// ORAS's e2e TestDataRoot convention (test/e2e/internal/utils/init.go).
var TestDataRoot = mustTestDataRoot()

func mustTestDataRoot() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("utils.TestDataRoot: runtime.Caller failed to resolve source location")
	}
	// thisFile is .../e2e/internal/utils/fixtures.go; testdata lives at
	// .../e2e/testdata.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata")
	root, err := filepath.Abs(root)
	if err != nil {
		panic("utils.TestDataRoot: " + err.Error())
	}
	if fi, statErr := os.Stat(root); statErr != nil || !fi.IsDir() {
		panic("utils.TestDataRoot: failed to find testdata directory at " + root)
	}
	return root
}

// SolutionFixture returns the absolute path to a named fixture directory
// under e2e/testdata/solutions/.
func SolutionFixture(name string) string {
	return filepath.Join(TestDataRoot, "solutions", name)
}
