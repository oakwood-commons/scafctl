// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"gopkg.in/yaml.v3"
)

// editContext bundles the three byte-level views a source-preserving edit needs:
// a path->value-node map (refindex.NodeMap), a path->key-position source map
// (sourcepos.SourceMap), and a byte<->position line index. All three use the
// same logical-path scheme (dotted keys, [i] sequence elements, spec.-prefixed
// roots), so a single path resolves consistently across them. It is built once
// per edit from the raw bytes; the helpers below do not mutate the input.
type editContext struct {
	nodes map[string]*yaml.Node
	sm    *sourcepos.SourceMap
	li    *sourcepos.LineIndex
}

// newEditContext parses raw and builds the value-node map, source map, and line
// index used by the edit builders. It returns an error when the YAML cannot be
// parsed, so callers never operate on a partial view.
func newEditContext(raw []byte) (*editContext, error) {
	nodes, err := refindex.NodeMap(raw)
	if err != nil {
		return nil, fmt.Errorf("build node map: %w", err)
	}
	sm, err := sourcepos.BuildSourceMap(raw, "")
	if err != nil {
		return nil, fmt.Errorf("build source map: %w", err)
	}
	return &editContext{
		nodes: nodes,
		sm:    sm,
		li:    sourcepos.NewLineIndex(raw, ""),
	}, nil
}

// RemoveMappingEntry computes the TextEdit that deletes the whole `key: <value>`
// mapping entry located at path (a NodeMap path that points at the entry's VALUE
// node). The key token itself is located via the source map, which records the
// key position for the same path.
//
// The removed span runs from the start of the key line (consuming the entry's
// leading indentation) through the trailing newline of the entry's last line, so
// no blank line is left behind and sibling entries are untouched. The value may
// be an inline scalar, a nested block mapping, or a sequence: the block's full
// extent is found by indentation scanning (yaml.v3 reports only a node's start),
// identical to the technique in ExtractCall.
//
// It returns an error (and no edit) when path does not resolve to both a key
// position and a value node, so a caller never applies a guessed edit.
func RemoveMappingEntry(raw []byte, path string) (TextEdit, error) {
	ec, err := newEditContext(raw)
	if err != nil {
		return TextEdit{}, fmt.Errorf("remove mapping entry: %w", err)
	}
	keyPos, ok := ec.sm.Get(path)
	if !ok {
		return TextEdit{}, fmt.Errorf("remove mapping entry: path %q has no key position", path)
	}
	if _, ok := ec.nodes[path]; !ok {
		return TextEdit{}, fmt.Errorf("remove mapping entry: path %q does not resolve to a value node", path)
	}

	keyIndent := keyPos.Column - 1
	endLine := scanBlockEndLine(raw, ec.li, keyPos.Line, keyIndent)

	startByte := ec.li.Offset(sourcepos.Position{Line: keyPos.Line, Column: 1})
	// End at the first byte of the line after the block's last line, which
	// consumes the entry's trailing newline (clamps to EOF for the last entry).
	endByte := ec.li.Offset(sourcepos.Position{Line: endLine + 1, Column: 1})

	return TextEdit{Range: ec.li.Range(startByte, endByte), NewText: ""}, nil
}

// RemoveSequenceElement computes the TextEdit that deletes a single `- item`
// element at a sequence-element path (e.g. "spec.resolvers.x.dependsOn[2]"). The
// element's line plus its trailing newline are consumed, leaving the other
// elements intact.
//
// Callers must decide the empty-sequence case themselves: if removing this
// element would leave the sequence empty, prefer RemoveMappingEntry on the
// owning key instead, so the result is not a `key:` with an empty list (which is
// a different, still-present value rather than a removal). This helper does not
// detect that on its own -- it only ever removes the one element at path.
//
// It returns an error (and no edit) when path does not resolve to a node, or the
// element's line does not begin with a `- ` sequence marker (only block-style
// sequences are supported).
func RemoveSequenceElement(raw []byte, path string) (TextEdit, error) {
	ec, err := newEditContext(raw)
	if err != nil {
		return TextEdit{}, fmt.Errorf("remove sequence element: %w", err)
	}
	node, ok := ec.nodes[path]
	if !ok || node == nil {
		return TextEdit{}, fmt.Errorf("remove sequence element: path %q does not resolve to a node", path)
	}

	lineStart := ec.li.Offset(sourcepos.Position{Line: node.Line, Column: 1})
	lineEnd := ec.li.Offset(sourcepos.Position{Line: node.Line, Column: maxColumn})
	lineBytes := raw[lineStart:lineEnd]
	indent := leadingSpaces(lineBytes)
	if indent >= len(lineBytes) || lineBytes[indent] != '-' {
		return TextEdit{}, fmt.Errorf("remove sequence element: line %d does not begin with a %q sequence marker", node.Line, "- ")
	}

	endLine := scanBlockEndLine(raw, ec.li, node.Line, indent)
	endByte := ec.li.Offset(sourcepos.Position{Line: endLine + 1, Column: 1})

	return TextEdit{Range: ec.li.Range(lineStart, endByte), NewText: ""}, nil
}

// ReplaceMappingKeyAndValue computes the TextEdit that rewrites the `key: value`
// scalar entry at path (a NodeMap path pointing at the scalar VALUE node) as
// `newKey: newValue`. The line's leading indentation is preserved (the edit
// starts at the key token, not the line start), and any trailing inline comment
// after the value is preserved because the replaced span ends at the value token
// (not the end of line). Whitespace between the key colon and the value is
// normalized to a single space.
//
// It is used for the deprecated onError -> continueOnError fix. It returns an
// error (and no edit) when path does not resolve to both a key position and a
// scalar value node, so a caller never applies a guessed edit.
func ReplaceMappingKeyAndValue(raw []byte, path, newKey, newValue string) (TextEdit, error) {
	ec, err := newEditContext(raw)
	if err != nil {
		return TextEdit{}, fmt.Errorf("replace mapping key and value: %w", err)
	}
	keyPos, ok := ec.sm.Get(path)
	if !ok {
		return TextEdit{}, fmt.Errorf("replace mapping key and value: path %q has no key position", path)
	}
	valNode, ok := ec.nodes[path]
	if !ok || valNode == nil {
		return TextEdit{}, fmt.Errorf("replace mapping key and value: path %q does not resolve to a value node", path)
	}
	if valNode.Kind != yaml.ScalarNode {
		return TextEdit{}, fmt.Errorf("replace mapping key and value: path %q is not a scalar value", path)
	}

	keyStart := ec.li.Offset(sourcepos.Position{Line: keyPos.Line, Column: keyPos.Column})
	valStart := ec.li.Offset(sourcepos.Position{Line: valNode.Line, Column: valNode.Column})
	valEnd := valStart + scalarRawLen(valNode)
	if keyStart < 0 || valEnd > len(raw) || keyStart >= valEnd {
		return TextEdit{}, fmt.Errorf("replace mapping key and value: path %q resolves to an out-of-bounds span", path)
	}

	return TextEdit{Range: ec.li.Range(keyStart, valEnd), NewText: newKey + ": " + newValue}, nil
}

// scalarRawLen returns the byte length the scalar occupies in the source,
// accounting for surrounding quotes on quoted styles. It is exact for plain and
// simple single/double-quoted scalars without escapes (the onError values
// "continue"/"fail" are always in this class); block/folded scalars are not a
// target of the replace helper and fall back to the decoded length.
func scalarRawLen(n *yaml.Node) int {
	l := len(n.Value)
	if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
		l += 2 // opening and closing quote
	}
	return l
}
