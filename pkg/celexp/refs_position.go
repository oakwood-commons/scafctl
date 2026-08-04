// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/cel-go/cel"
)

// VariableRef is a single textual occurrence of a "_"-scoped reference within a
// CEL expression, located by its byte range inside the expression source.
//
// Unlike GetUnderscoreVariables (which returns a deduplicated, sorted set of
// names), a VariableRef is emitted once per occurrence and carries the exact
// span of the referenced name, so tools can highlight, navigate to, or rewrite
// each individual use (e.g. renaming a resolver and every reference to it).
type VariableRef struct {
	// Name is the referenced name with the "_." (or "_[\"...\"]") prefix removed.
	Name string

	// Offset is the 0-based BYTE offset of Name within the expression source, or
	// -1 when the occurrence could not be positioned (its exact bytes could not
	// be located, e.g. a bracket key whose decoded value differs from the source
	// due to escapes). A -1 offset signals to consumers that the occurrence
	// exists but must be treated as unpositioned rather than silently dropped.
	Offset int

	// Len is the byte length of Name (== len(Name)). Offset+Len is the exclusive
	// end of the occurrence.
	Len int

	// Optional reports whether the reference was reached through optional-access
	// syntax (_.?name or _[?"name"]).
	Optional bool
}

// End returns the exclusive end byte offset of the reference (Offset + Len).
func (r VariableRef) End() int { return r.Offset + r.Len }

// UnderscoreVariableRefs parses the expression and returns every "_"-scoped
// reference occurrence with its byte range within the expression source, in
// source order (by offset).
//
// Every occurrence is returned: one that could not be located byte-exact is
// reported with Offset == -1 (unpositioned) rather than dropped, so consumers
// such as rename can fail safe instead of silently missing a reference.
//
// It uses the CEL AST to decide WHICH names are genuine "_"-scoped references
// (so a name that only appears inside a string literal is not matched), then an
// AST-anchored, word-boundaried text scan to pin down each occurrence's exact
// range. This hybrid is robust to the fact that a CEL SelectExpr's recorded
// position points at the "." operator rather than the field, and to the same
// name appearing multiple times.
//
// Example:
//
//	refs, _ := celexp.Expression(`_.a + _.b`).UnderscoreVariableRefs(ctx)
//	// refs == [{Name:"a", Offset:2, Len:1}, {Name:"b", Offset:8, Len:1}]
func (e Expression) UnderscoreVariableRefs(ctx context.Context) ([]VariableRef, error) {
	return e.PrefixedVariableRefs(ctx, "_.")
}

// PrefixedVariableRefs is the prefix-parameterized form of UnderscoreVariableRefs.
// It returns every occurrence of a reference under the given prefix (e.g. "_." for
// resolver data, "__actions." for action results) with its byte range within the
// expression source. The prefix must end in "." (select-style). Semantics --
// including the Offset == -1 "unpositioned" sentinel -- match UnderscoreVariableRefs.
func (e Expression) PrefixedVariableRefs(ctx context.Context, prefix string) ([]VariableRef, error) {
	env, err := NewParseEnv(ctx)
	if err != nil {
		return nil, err
	}

	parsed, issues := env.Parse(string(e))
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to parse CEL expression: %w", issues.Err())
	}

	parsedExpr, err := cel.AstToParsedExpr(parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to convert AST: %w", err)
	}

	// SourceInfo.Positions maps AST node id -> RUNE offset in the source. The
	// offset anchors near (at or just before) the reference; we scan for the
	// exact identifier from there.
	positions := parsedExpr.GetSourceInfo().GetPositions()
	exprRunes := []rune(string(e))

	var refs []VariableRef
	walkVariablesWithPrefix(parsedExpr.GetExpr(), prefix, func(name string, id int64, optional bool) {
		anchor := int(positions[id])
		runeOff := findIdentNear(exprRunes, []rune(name), anchor)
		if runeOff < 0 {
			// The occurrence exists but its exact bytes could not be located
			// (e.g. a bracket key with escapes). Report it as unpositioned
			// (Offset -1) instead of dropping it, so a rename fails safe rather
			// than silently missing this reference.
			refs = append(refs, VariableRef{Name: name, Offset: -1, Len: len(name), Optional: optional})
			return
		}
		byteOff := len(string(exprRunes[:runeOff]))
		refs = append(refs, VariableRef{
			Name:     name,
			Offset:   byteOff,
			Len:      len(name),
			Optional: optional,
		})
	})

	sort.Slice(refs, func(i, j int) bool { return refs[i].Offset < refs[j].Offset })
	return refs, nil
}

// findIdentNear returns the rune offset of target in runes, choosing the
// word-boundaried occurrence nearest to anchor. It returns -1 if target does
// not appear with identifier boundaries on both sides. Boundaries prevent a
// short name (e.g. "app") from matching inside a longer identifier
// (e.g. "appName").
func findIdentNear(runes, target []rune, anchor int) int {
	if len(target) == 0 {
		return -1
	}
	best := -1
	bestDist := int(^uint(0) >> 1) // max int
	for i := 0; i+len(target) <= len(runes); i++ {
		if !runesMatchAt(runes, target, i) {
			continue
		}
		// Left boundary: the preceding rune must not be an identifier rune.
		if i > 0 && isIdentRune(runes[i-1]) {
			continue
		}
		// Right boundary: the following rune must not be an identifier rune.
		if i+len(target) < len(runes) && isIdentRune(runes[i+len(target)]) {
			continue
		}
		d := i - anchor
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}

// runesMatchAt reports whether target matches runes starting at index i.
func runesMatchAt(runes, target []rune, i int) bool {
	for j := range target {
		if runes[i+j] != target[j] {
			return false
		}
	}
	return true
}

// isIdentRune reports whether r can be part of a CEL/identifier token. Non-ASCII
// runes are treated as identifier runes so multibyte content forms a boundary
// consistently.
func isIdentRune(r rune) bool {
	switch {
	case r == '_':
		return true
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r >= 0x80:
		return true
	default:
		return false
	}
}
