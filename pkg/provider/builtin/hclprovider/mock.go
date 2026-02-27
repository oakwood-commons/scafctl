// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import "fmt"

// MockFileReader is a test double for the FileReader interface.
type MockFileReader struct {
	// ReadFileFunc allows custom behavior per test.
	ReadFileFunc func(path string) ([]byte, error)
	// ReadFileErr causes ReadFile to return an error when true.
	ReadFileErr bool
	// Content is returned by ReadFile when ReadFileFunc is nil and ReadFileErr is false.
	Content []byte
}

// ReadFile reads a file using the mock configuration.
func (m *MockFileReader) ReadFile(path string) ([]byte, error) {
	if m.ReadFileErr {
		return nil, fmt.Errorf("mock read error: %s", path)
	}
	if m.ReadFileFunc != nil {
		return m.ReadFileFunc(path)
	}
	return m.Content, nil
}
