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

// TestSeeAlso_NoDanglingLinks asserts that every SeeAlso target resolves to a
// registered concept, so cross-references never point at a typo or a removed
// concept.
func TestSeeAlso_NoDanglingLinks(t *testing.T) {
	for _, c := range List() {
		for _, target := range c.SeeAlso {
			_, ok := Get(target)
			assert.True(t, ok, "concept %q has dangling SeeAlso target %q", c.Name, target)
		}
	}
}

// TestContextCategory_Present asserts the new context category exists and
// contains exactly the expected authoring/runtime concepts.
func TestContextCategory_Present(t *testing.T) {
	assert.Contains(t, Categories(), "context")

	items := ByCategory("context")
	names := make([]string, 0, len(items))
	for _, c := range items {
		assert.Equal(t, "context", c.Category)
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{
		"context-variables",
		"phase-execution",
		"cel-cost-model",
		"template-dependency-inference",
		"snapshot-masking",
		"authoring-workflow",
	}, names)
}

// TestContextConcepts_WellFormed asserts each new concept is complete.
func TestContextConcepts_WellFormed(t *testing.T) {
	for _, name := range []string{
		"context-variables",
		"phase-execution",
		"cel-cost-model",
		"template-dependency-inference",
		"snapshot-masking",
		"authoring-workflow",
	} {
		c, ok := Get(name)
		require.True(t, ok, "concept %q must be registered", name)
		assert.Equal(t, "context", c.Category)
		assert.NotEmpty(t, c.Summary, "%q summary", name)
		assert.NotEmpty(t, c.Explanation, "%q explanation", name)
		assert.NotEmpty(t, c.SeeAlso, "%q seeAlso", name)
	}
}

// TestContextConcepts_PointToLiveTools guards against drift: concepts must
// reference the live tools rather than mirroring their output.
func TestContextConcepts_PointToLiveTools(t *testing.T) {
	cv, ok := Get("context-variables")
	require.True(t, ok)
	assert.Contains(t, cv.Explanation, "list_context_variables")

	tdi, ok := Get("template-dependency-inference")
	require.True(t, ok)
	assert.Contains(t, tdi.Explanation, "extract_resolver_refs")

	aw, ok := Get("authoring-workflow")
	require.True(t, ok)
	assert.Contains(t, aw.Explanation, "get_provider_schema")
}
