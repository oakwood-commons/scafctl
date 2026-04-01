// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterItems_NoFilter(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	result, err := FilterItems(context.Background(), items, "")
	assert.NoError(t, err)
	assert.Equal(t, items, result)
}

func BenchmarkFilterItems_NoFilter(b *testing.B) {
	items := make([]string, 100)
	for i := range items {
		items[i] = "test"
	}
	ctx := context.Background()
	for b.Loop() {
		_, _ = FilterItems(ctx, items, "")
	}
}
