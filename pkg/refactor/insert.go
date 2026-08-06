// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"gopkg.in/yaml.v3"
)

// InsertMappingEntry computes the zero-width TextEdit that inserts entryYAML as a
// new child entry under the mapping at containerPath (a NodeMap/SourceMap path
// such as "spec.resolvers"). It is the source-preserving counterpart to
// ExtractCall's private callsInsertion: rather than a YAML round-trip, it splices
// re-indented text so the file's comments, key order, and formatting are
// untouched.
//
// entryYAML is a keyed block given at ZERO indentation, e.g.
//
//	myRes:
//	  resolve:
//	    with:
//	      - provider: static
//	        inputs:
//	          value: ""
//
// Every line is shifted uniformly so the entry's top-level key lands at the
// container's child indent -- which, along with the container's own indent when
// it is created, is derived from the file's existing indentation unit (mirroring
// extract.go's callIndents) so the placement matches the surrounding style. The
// block's OWN interior indentation (the nesting within entryYAML) is preserved
// as-is, so entryYAML should already be written with the indent step you want
// for the inserted block's children.
//
// Two cases:
//
//   - The container key EXISTS: the entry is appended after the container's last
//     existing child (or, for an empty container, as its first child).
//   - The container key is ABSENT: the container key is created as the first child
//     of its parent, holding the entry (e.g. a new "resolvers:" under "spec").
//
// It returns an error (and NO edit) when containerPath's parent cannot be located
// or is not a block-style mapping, or when an existing container is written in
// flow style -- cases where a spliced block insertion would corrupt the document.
func InsertMappingEntry(raw []byte, containerPath, entryYAML string) (TextEdit, error) {
	ec, err := newEditContext(raw)
	if err != nil {
		return TextEdit{}, fmt.Errorf("insert mapping entry: %w", err)
	}

	parentPath, containerKey, ok := splitLastKey(containerPath)
	if !ok {
		return TextEdit{}, fmt.Errorf("insert mapping entry: %q has no parent mapping to insert into", containerPath)
	}
	parentPos, ok := ec.sm.Get(parentPath)
	if !ok {
		return TextEdit{}, fmt.Errorf("insert mapping entry: parent path %q could not be located", parentPath)
	}
	parentNode, ok := ec.nodes[parentPath]
	if !ok || parentNode == nil || parentNode.Kind != yaml.MappingNode {
		return TextEdit{}, fmt.Errorf("insert mapping entry: parent path %q is not a mapping", parentPath)
	}
	if parentNode.Style&yaml.FlowStyle != 0 {
		return TextEdit{}, fmt.Errorf("insert mapping entry: parent %q is written in flow style; rewrite it in block style", parentPath)
	}

	parentIndent := parentPos.Column - 1
	unit := mappingIndentUnit(parentNode, parentPos.Column)

	// Refuse to append an entry whose key already exists under the container: a
	// duplicate mapping key produces invalid YAML. This makes the primitive
	// all-or-nothing (mirroring ExtractCall's "already exists" guard) rather than
	// corrupting the document for a caller that did not pre-check.
	if key := entryTopKey(entryYAML); key != "" {
		if _, exists := ec.nodes[containerPath+"."+key]; exists {
			return TextEdit{}, fmt.Errorf("insert mapping entry: %q already exists under %q", key, containerPath)
		}
	}

	// Container exists: append the entry after its last child (or as its first
	// child when the container is currently empty).
	if containerPos, present := ec.sm.Get(containerPath); present {
		containerNode := ec.nodes[containerPath]
		if containerNode != nil && containerNode.Kind == yaml.MappingNode && containerNode.Style&yaml.FlowStyle != 0 {
			return TextEdit{}, fmt.Errorf("insert mapping entry: container %q is written in flow style; rewrite it in block style", containerPath)
		}
		containerIndent := containerPos.Column - 1
		childIndent := containerIndent + unit
		if containerNode != nil && containerNode.Kind == yaml.MappingNode && len(containerNode.Content) > 0 {
			childIndent = containerNode.Content[0].Column - 1
		}
		endLine := scanBlockEndLine(raw, ec.li, containerPos.Line, containerIndent)
		insertByte := ec.li.Offset(sourcepos.Position{Line: endLine, Column: maxColumn})
		pos := ec.li.Position(insertByte)
		return TextEdit{
			Range:   sourcepos.Range{Start: pos, End: pos},
			NewText: "\n" + reindentEntry(entryYAML, childIndent),
		}, nil
	}

	// Container absent: create it as the first child of the parent, immediately
	// after the parent key line -- a deterministic, always-valid insertion point
	// (mirrors callsInsertion's new-block branch).
	containerIndent := parentIndent + unit
	childIndent := containerIndent + unit
	insertByte := ec.li.Offset(sourcepos.Position{Line: parentPos.Line + 1, Column: 1})
	pos := ec.li.Position(insertByte)
	block := strings.Repeat(" ", containerIndent) + containerKey + ":\n" + reindentEntry(entryYAML, childIndent) + "\n"
	return TextEdit{
		Range:   sourcepos.Range{Start: pos, End: pos},
		NewText: block,
	}, nil
}

// entryTopKey returns the top-level mapping key of a zero-indented keyed block
// (the text before the first ":" on its first non-blank line), or "" when the
// block does not begin with a key. Used to detect a collision with an existing
// entry before appending.
func entryTopKey(entryYAML string) string {
	for _, line := range strings.Split(entryYAML, "\n") {
		if isBlank([]byte(line)) {
			continue
		}
		trimmed := strings.TrimLeft(line, " ")
		if i := strings.IndexByte(trimmed, ':'); i > 0 {
			return trimmed[:i]
		}
		return ""
	}
	return ""
}

// splitLastKey splits a dotted logical path into its parent path and final key.
// It reports ok=false when there is no parent segment (a bare root key), which a
// child insertion cannot use.
func splitLastKey(path string) (parent, key string, ok bool) {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return "", "", false
	}
	return path[:i], path[i+1:], true
}

// mappingIndentUnit returns the indentation step (in spaces) between a mapping
// key and its children, measured from the mapping's first existing child. It
// falls back to 2 when the mapping has no children to measure. parentKeyColumn
// is the 1-based column of the mapping's own key.
func mappingIndentUnit(mappingValueNode *yaml.Node, parentKeyColumn int) int {
	if mappingValueNode != nil && mappingValueNode.Kind == yaml.MappingNode && len(mappingValueNode.Content) > 0 {
		if u := mappingValueNode.Content[0].Column - parentKeyColumn; u > 0 {
			return u
		}
	}
	return 2
}

// reindentEntry shifts every non-blank line of a zero-indented keyed block so its
// top-level key lands at indent spaces, preserving the block's own relative
// indentation. Blank lines are emitted empty. Any trailing newlines on the input
// are dropped so the caller controls the surrounding whitespace.
func reindentEntry(entryYAML string, indent int) string {
	lines := strings.Split(strings.TrimRight(entryYAML, "\n"), "\n")
	pad := strings.Repeat(" ", indent)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if isBlank([]byte(line)) {
			out = append(out, "")
			continue
		}
		out = append(out, pad+line)
	}
	return strings.Join(out, "\n")
}
