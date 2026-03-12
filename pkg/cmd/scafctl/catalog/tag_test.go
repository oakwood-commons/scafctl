// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
)

func TestIsValidTagChar(t *testing.T) {
	valid := []rune{'a', 'z', 'A', 'Z', '0', '9', '_', '.', '-'}
	for _, ch := range valid {
		assert.True(t, catalogpkg.IsValidTagChar(ch), "expected %q to be valid", string(ch))
	}

	invalid := []rune{'/', ':', ' ', '@', '#', '!', '(', ')'}
	for _, ch := range invalid {
		assert.False(t, catalogpkg.IsValidTagChar(ch), "expected %q to be invalid", string(ch))
	}
}
