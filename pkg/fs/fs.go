// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fs

import "os"

// StatFunc defines a function type that takes a file path as input and returns
// the file's os.FileInfo and an error if the operation fails. It is typically
// used to abstract file stat operations for testing or customization.
type StatFunc func(path string) (os.FileInfo, error)

// ReadFileFunc defines a function type for reading the contents of a file.
// It takes a filename as input and returns the file's contents as a byte slice,
// along with an error if the operation fails.
type ReadFileFunc func(filename string) ([]byte, error)
