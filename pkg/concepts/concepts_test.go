// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package concepts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet_ExistingConcept(t *testing.T) {
	c, ok := Get("resolver")
	require.True(t, ok)
	assert.Equal(t, "Resolver", c.Title)
	assert.Equal(t, "resolvers", c.Category)
	assert.NotEmpty(t, c.Summary)
	assert.NotEmpty(t, c.Explanation)
}

func TestGet_NotFound(t *testing.T) {
	_, ok := Get("nonexistent-concept")
	assert.False(t, ok)
}

func TestList_ReturnsAll(t *testing.T) {
	all := List()
	assert.GreaterOrEqual(t, len(all), 10)
	// Verify sorted by category then name
	for i := 1; i < len(all); i++ {
		prev := all[i-1]
		cur := all[i]
		if prev.Category == cur.Category {
			assert.LessOrEqual(t, prev.Name, cur.Name)
		} else {
			assert.Less(t, prev.Category, cur.Category)
		}
	}
}

func TestCategories(t *testing.T) {
	cats := Categories()
	assert.NotEmpty(t, cats)
	for i := 1; i < len(cats); i++ {
		assert.Less(t, cats[i-1], cats[i])
	}
}

func TestByCategory(t *testing.T) {
	items := ByCategory("testing")
	assert.NotEmpty(t, items)
	for _, c := range items {
		assert.Equal(t, "testing", c.Category)
	}
}

func TestSearch(t *testing.T) {
	results := Search("template")
	assert.NotEmpty(t, results)
}

func TestSearch_Empty(t *testing.T) {
	results := Search("")
	assert.Equal(t, len(List()), len(results))
}

func TestSearch_NoMatch(t *testing.T) {
	results := Search("zzzznonexistentzzzz")
	assert.Empty(t, results)
}
