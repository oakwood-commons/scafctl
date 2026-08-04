// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package refactor performs source-preserving refactorings on solution files.
//
// Refactorings are expressed as a set of TextEdits (a Range plus replacement
// text). Applying edits replaces only the exact bytes of each occurrence, so
// comments, key order, and formatting are preserved verbatim -- there is no YAML
// round-trip. The same edits drive a CLI rewrite (Apply to bytes) and an LSP
// WorkspaceEdit (map each TextEdit to an LSP edit).
//
// Correctness is guarded, not best-effort: RenameResolver refuses to produce
// edits when the reference index is incomplete (refindex.Unresolved() > 0),
// because a missed reference would silently break the solution. It is safer to
// abort a rename than to perform a partial one.
package refactor

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
)

// ResolverNamePattern is the canonical pattern a resolver name must match. It
// mirrors the pattern enforced on resolver names and rslvr references in the
// spec struct tags.
const ResolverNamePattern = `^[a-zA-Z_][a-zA-Z0-9_-]*$`

var resolverNameRe = regexp.MustCompile(ResolverNamePattern)

// TextEdit is a single replacement of a source Range with NewText.
type TextEdit struct {
	// Range is the half-open span of source to replace.
	Range sourcepos.Range
	// NewText is the replacement text.
	NewText string
}

// RenameResult is the outcome of a rename: the names involved and the edits
// that carry it out.
type RenameResult struct {
	OldName string
	NewName string
	Edits   []TextEdit
}

// Apply applies the result's edits to raw and returns the rewritten bytes.
func (r *RenameResult) Apply(raw []byte) ([]byte, error) {
	return Apply(raw, r.Edits)
}

// RenameResolver computes the edits to rename a resolver and every reference to
// it (dependsOn entries, rslvr values, CEL _.name uses, explicit template
// ._.name uses, and the definition key). It is RenameSymbol for resolvers; see
// RenameSymbol for the refusal conditions.
func RenameResolver(sol *solution.Solution, oldName, newName string) (*RenameResult, error) {
	return RenameSymbol(sol, refindex.SymbolResolver, oldName, newName)
}

// RenameAction computes the edits to rename an action and every reference to it
// (dependsOn entries, __actions.name CEL uses, and .__actions.name template
// uses, plus the definition). It refuses (producing no edits) under the same
// conditions as RenameResolver.
func RenameAction(sol *solution.Solution, oldName, newName string) (*RenameResult, error) {
	return RenameSymbol(sol, refindex.SymbolAction, oldName, newName)
}

// RenameSymbol computes the edits to rename a symbol of the given kind and every
// reference to it. It returns an error, producing NO edits, when:
//   - newName is not a valid name;
//   - newName equals oldName;
//   - oldName is not defined in the solution;
//   - newName already names a symbol of the same kind (collision); or
//   - the reference index is incomplete for oldName (some references could not be
//     located), which would make a rename partial and unsafe.
func RenameSymbol(sol *solution.Solution, kind refindex.SymbolKind, oldName, newName string) (*RenameResult, error) {
	if sol == nil {
		return nil, fmt.Errorf("rename %s: nil solution", kind)
	}
	if !resolverNameRe.MatchString(newName) {
		return nil, fmt.Errorf("rename %s: %q is not a valid %s name (must match %s)", kind, newName, kind, ResolverNamePattern)
	}
	if oldName == newName {
		return nil, fmt.Errorf("rename %s: new name equals old name %q", kind, oldName)
	}

	idx, err := refindex.Build(sol)
	if err != nil {
		return nil, fmt.Errorf("rename %s: %w", kind, err)
	}

	if _, ok := idx.Definition(kind, oldName); !ok {
		return nil, fmt.Errorf("rename %s: %s %q is not defined", kind, kind, oldName)
	}
	if _, ok := idx.Definition(kind, newName); ok {
		return nil, fmt.Errorf("rename %s: %s %q already exists", kind, kind, newName)
	}

	// Refuse a partial rewrite: if any reference to oldName could not be located
	// byte-exact, a rename might miss it and silently break the solution. This is
	// symbol-scoped -- unlocatable references to OTHER symbols do not block this
	// rename, but references whose target could not be determined at all do.
	if n := idx.UnresolvedFor(kind, oldName); n > 0 {
		return nil, fmt.Errorf("rename %s: %d reference(s) to %q could not be located; aborting to avoid a partial rename", kind, n, oldName)
	}

	occ := idx.Occurrences(kind, oldName)
	edits := make([]TextEdit, 0, len(occ))
	for _, r := range occ {
		edits = append(edits, TextEdit{Range: r.Range, NewText: newName})
	}

	return &RenameResult{OldName: oldName, NewName: newName, Edits: edits}, nil
}

// Apply applies edits to raw and returns the rewritten bytes. Edits are applied
// left-to-right against the original byte offsets (resolved via a LineIndex);
// overlapping edits are rejected. The input is not mutated.
func Apply(raw []byte, edits []TextEdit) ([]byte, error) {
	if len(edits) == 0 {
		return append([]byte(nil), raw...), nil
	}

	li := sourcepos.NewLineIndex(raw, "")

	type span struct {
		start, end int
		text       string
	}
	spans := make([]span, 0, len(edits))
	for _, e := range edits {
		start := li.Offset(e.Range.Start)
		end := li.Offset(e.Range.End)
		if start < 0 || end > len(raw) || start > end {
			return nil, fmt.Errorf("apply: edit range %s is out of bounds", e.Range)
		}
		spans = append(spans, span{start: start, end: end, text: e.NewText})
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end < spans[j].end
	})

	var buf bytes.Buffer
	buf.Grow(len(raw))
	last := 0
	for _, sp := range spans {
		if sp.start < last {
			return nil, fmt.Errorf("apply: overlapping edits at byte %d", sp.start)
		}
		buf.Write(raw[last:sp.start])
		buf.WriteString(sp.text)
		last = sp.end
	}
	buf.Write(raw[last:])
	return buf.Bytes(), nil
}
