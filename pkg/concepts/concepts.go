// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package concepts provides a registry of scafctl domain concepts with
// concise explanations, examples, and cross-references. It is used by
// MCP tools, CLI help, and documentation generation.
package concepts

import "sort"

// Concept describes a single scafctl domain concept.
type Concept struct {
	// Name is the canonical kebab-case concept identifier.
	Name string `json:"name" yaml:"name"`

	// Title is the human-readable display name.
	Title string `json:"title" yaml:"title"`

	// Category groups related concepts (e.g., "resolvers", "testing", "actions").
	Category string `json:"category" yaml:"category"`

	// Summary is a one-sentence description.
	Summary string `json:"summary" yaml:"summary"`

	// Explanation is a multi-paragraph detailed explanation.
	Explanation string `json:"explanation" yaml:"explanation"`

	// Examples provides short YAML or code examples.
	Examples []string `json:"examples,omitempty" yaml:"examples,omitempty"`

	// SeeAlso lists related concept names.
	SeeAlso []string `json:"seeAlso,omitempty" yaml:"seeAlso,omitempty"`
}

// registry is the canonical map of all concepts keyed by name.
//
//nolint:gochecknoinits // Package-level registration of builtin concepts is the simplest approach.
var registry = func() map[string]Concept {
	m := make(map[string]Concept, len(builtinConcepts))
	for _, c := range builtinConcepts {
		m[c.Name] = c
	}
	return m
}()

// Get returns a concept by name. Returns false if not found.
func Get(name string) (Concept, bool) {
	c, ok := registry[name]
	return c, ok
}

// List returns all registered concepts sorted by category then name.
func List() []Concept {
	result := make([]Concept, 0, len(registry))
	for _, c := range registry {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// Categories returns a sorted list of unique category names.
func Categories() []string {
	seen := map[string]bool{}
	for _, c := range registry {
		seen[c.Category] = true
	}
	cats := make([]string, 0, len(seen))
	for c := range seen {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return cats
}

// ByCategory returns all concepts in a given category, sorted by name.
func ByCategory(category string) []Concept {
	var result []Concept
	for _, c := range registry {
		if c.Category == category {
			result = append(result, c)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Search returns concepts whose name, title, or summary contain the query (case-insensitive).
func Search(query string) []Concept {
	if query == "" {
		return List()
	}
	var result []Concept
	lq := toLower(query)
	for _, c := range registry {
		if contains(c.Name, lq) || contains(c.Title, lq) || contains(c.Summary, lq) {
			result = append(result, c)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(toLower(haystack), needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
