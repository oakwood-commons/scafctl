// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithState_FromContext(t *testing.T) {
	data := NewMockData("test-sol", "1.0.0", map[string]*Entry{
		"key1": {Value: "val1", Type: "string"},
	})

	ctx := WithState(context.Background(), data)
	got, ok := FromContext(ctx)

	assert.True(t, ok)
	assert.Same(t, data, got)
	assert.Equal(t, "test-sol", got.Metadata.Solution)
	assert.Len(t, got.Values, 1)
}

func TestFromContext_Missing(t *testing.T) {
	got, ok := FromContext(context.Background())

	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestWithState_OverwritesPrevious(t *testing.T) {
	data1 := NewMockData("sol-1", "1.0.0", nil)
	data2 := NewMockData("sol-2", "2.0.0", nil)

	ctx := WithState(context.Background(), data1)
	ctx = WithState(ctx, data2)

	got, ok := FromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "sol-2", got.Metadata.Solution)
}
