// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmplprovider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValueRefShapedData_Credentials(t *testing.T) {
	t.Parallel()

	assert.True(t, isValueRefShapedData(map[string]any{"scrt": "my-secret"}))
	assert.True(t, isValueRefShapedData(map[string]any{"tkn": "github"}))
	assert.False(t, isValueRefShapedData(map[string]any{"scrt": "my-secret", "extra": "literal"}))
	assert.False(t, isValueRefShapedData(map[string]any{"secret": "literal"}))
	assert.False(t, isValueRefShapedData(map[string]any{"appName": "demo"}))
}
