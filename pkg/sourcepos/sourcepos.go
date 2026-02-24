// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package sourcepos provides source-location-aware parsing for YAML files.
// It maps logical paths (e.g., "spec.resolvers.appName.resolve.with[0].inputs.value")
// to source positions (line, column, file) in the original YAML document.
//
// This enables tools like lint, validation, and MCP diagnostics to report
// precise source locations alongside logical paths.
//
// Usage:
//
//	sm, err := sourcepos.BuildSourceMap(yamlBytes, "solution.yaml")
//	if err != nil { ... }
//	if pos, ok := sm.Get("spec.resolvers.appName.resolve.with[0]"); ok {
//	    fmt.Printf("line %d, column %d\n", pos.Line, pos.Column)
//	}
package sourcepos

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Position represents a location in a YAML source file.
type Position struct {
	// Line is the 1-based line number in the source file.
	Line int `json:"line" yaml:"line" doc:"1-based line number in source file"`

	// Column is the 1-based column number in the source file.
	Column int `json:"column" yaml:"column" doc:"1-based column number in source file"`

	// File is the source file path. This is important for compose scenarios
	// where multiple files contribute to a single solution.
	File string `json:"file,omitempty" yaml:"file,omitempty" doc:"Source file path (important for compose)"`
}

// String returns a human-readable representation of the position.
func (p Position) String() string {
	if p.File != "" {
		return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// IsZero returns true if the position has no meaningful location.
func (p Position) IsZero() bool {
	return p.Line == 0 && p.Column == 0
}

// SourceMap maps logical YAML paths to source positions.
// It is built by walking a yaml.Node tree and recording each node's Line/Column.
type SourceMap struct {
	positions map[string]Position
}

// NewSourceMap creates a new empty SourceMap.
func NewSourceMap() *SourceMap {
	return &SourceMap{
		positions: make(map[string]Position),
	}
}

// Get returns the Position for the given logical path, if known.
func (sm *SourceMap) Get(path string) (Position, bool) {
	if sm == nil {
		return Position{}, false
	}
	pos, ok := sm.positions[path]
	return pos, ok
}

// Set records a position for the given logical path.
func (sm *SourceMap) Set(path string, pos Position) {
	if sm == nil {
		return
	}
	sm.positions[path] = pos
}

// Len returns the number of recorded positions.
func (sm *SourceMap) Len() int {
	if sm == nil {
		return 0
	}
	return len(sm.positions)
}

// Paths returns all recorded logical paths.
func (sm *SourceMap) Paths() []string {
	if sm == nil {
		return nil
	}
	paths := make([]string, 0, len(sm.positions))
	for p := range sm.positions {
		paths = append(paths, p)
	}
	return paths
}

// Merge incorporates all positions from another SourceMap.
// If both maps have the same key, the other map's position wins.
func (sm *SourceMap) Merge(other *SourceMap) {
	if sm == nil || other == nil {
		return
	}
	for path, pos := range other.positions {
		sm.positions[path] = pos
	}
}

// BuildSourceMap parses the YAML data and builds a SourceMap by walking the
// yaml.Node tree. The file parameter is recorded in each Position for
// multi-file (compose) scenarios.
func BuildSourceMap(data []byte, file string) (*SourceMap, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse YAML for source map: %w", err)
	}

	sm := NewSourceMap()
	walkNode(&doc, "", file, sm)
	return sm, nil
}

// walkNode recursively walks a yaml.Node tree, recording positions in the SourceMap.
func walkNode(node *yaml.Node, path, file string, sm *SourceMap) {
	if node == nil {
		return
	}

	switch node.Kind {
	case yaml.DocumentNode:
		// Document nodes contain a single child — the root mapping.
		for _, child := range node.Content {
			walkNode(child, path, file, sm)
		}

	case yaml.MappingNode:
		// Record the mapping node itself only if the path hasn't been set yet
		// (i.e. this is a root or stand-alone mapping, not one reached via a key).
		if path != "" {
			if _, exists := sm.positions[path]; !exists {
				sm.Set(path, Position{Line: node.Line, Column: node.Column, File: file})
			}
		}

		// Content is alternating key, value, key, value...
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]

			key := keyNode.Value
			var childPath string
			if path == "" {
				childPath = key
			} else {
				childPath = path + "." + key
			}

			// Record the key position (points to where the key starts).
			sm.Set(childPath, Position{Line: keyNode.Line, Column: keyNode.Column, File: file})

			// Recurse into the value.
			walkNode(valNode, childPath, file, sm)
		}

	case yaml.SequenceNode:
		// Record the sequence node itself only if the path hasn't been set yet.
		if path != "" {
			if _, exists := sm.positions[path]; !exists {
				sm.Set(path, Position{Line: node.Line, Column: node.Column, File: file})
			}
		}

		for i, child := range node.Content {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			sm.Set(childPath, Position{Line: child.Line, Column: child.Column, File: file})
			walkNode(child, childPath, file, sm)
		}

	case yaml.ScalarNode:
		// Scalars are recorded at their parent's path (already set by the mapping handler).
		// No children to recurse into.

	case yaml.AliasNode:
		// Follow the alias to its anchor.
		if node.Alias != nil {
			walkNode(node.Alias, path, file, sm)
		}
	}
}
