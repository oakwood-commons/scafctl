// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

// symbolCompletions offers scafctl-specific symbol-name completions -- the
// suggestions no generic YAML tooling can provide -- from the document's
// reference index:
//
//   - after "_." (CEL) or "._." (Go template)          -> resolver names
//   - after "__actions." (CEL) or ".__actions." (tmpl) -> action names
//   - a "call:" value                                  -> call names
//   - a "rslvr:" value                                 -> resolver names
//   - a "dependsOn:" list item (resolver)              -> resolver names
//   - a "dependsOn:" list item (action)                -> action names
//
// It is the SymbolRef branch #773's dispatch reserves and #775's function
// completion defers to (funcCompletions returns nothing for a reference-prefixed
// token). It composes as a fallback in the completion handler: it runs only when
// the structural/enum/function sources produced nothing, so it never competes
// with them.
//
// Names come from refindex (idx.Names), filtered by what the user has typed. It
// returns nil (no completion) when the document has no index (a parse error
// mid-edit) or the cursor is not on a symbol-reference position, so it never
// panics and degrades gracefully.
func symbolCompletions(entry *DocEntry, pos protocol.Position, cc CursorContext) []protocol.CompletionItem {
	if entry == nil || entry.Index == nil {
		return nil
	}
	// An action's dependsOn is SECTION-SCOPED: a dependsOn entry may only
	// reference actions in its own workflow section (actions vs finally), the same
	// rule validateDependsOn enforces. Offering the unified action index here would
	// suggest cross-section names the validator immediately rejects, so scope the
	// candidates to the owning section (single source of truth: Workflow.SectionActions).
	// This runs before the generic index path, which handles every other symbol.
	if names, partial, ok := actionDependsOnCandidates(entry, cc); ok {
		return completionItemsFromNames(names, refindex.SymbolAction, partial)
	}
	kind, partial, ok := symbolCompletionTarget(entry, pos, cc)
	if !ok {
		return nil
	}
	return symbolNameItems(entry.Index, kind, partial)
}

// actionDependsOnCandidates reports whether the cursor is on an action's
// dependsOn entry and, if so, returns the same-section action names it may
// reference (from the shared Workflow.SectionActions source of truth) plus the
// partial token typed so far. It handles both the typing case (a CursorNone
// dependsOn item, classified from cc.Path) and the swap case (a CursorSymbolRef
// on a complete action name, classified from the reference's position). Resolver
// dependsOn and non-dependsOn action references are left to the generic path.
func actionDependsOnCandidates(entry *DocEntry, cc CursorContext) ([]string, string, bool) {
	base, partial, ok := actionDependsOnBase(entry, cc)
	if !ok {
		return nil, "", false
	}
	section, ok := workflowSectionFromPath(base)
	if !ok {
		return nil, "", false
	}
	if entry.Sol == nil {
		return nil, "", false
	}
	names := make([]string, 0, len(entry.Sol.Spec.Workflow.SectionActions(section)))
	for name := range entry.Sol.Spec.Workflow.SectionActions(section) {
		names = append(names, name)
	}
	return names, partial, true
}

// actionDependsOnBase returns the owning dependsOn path (e.g.
// "spec.workflow.finally.notify.dependsOn") and the partial token when the cursor
// is on an ACTION dependsOn entry. It reports ok=false for a resolver dependsOn,
// a non-dependsOn position, or when the path cannot be determined.
func actionDependsOnBase(entry *DocEntry, cc CursorContext) (string, string, bool) {
	// Typing case: a partial dependsOn item is CursorNone with the item path.
	if base := stripIndex(cc.Path); base != "" && lastSegment(base) == "dependsOn" && isActionDependsOnPath(base) {
		return base, nodeValue(cc.Node), true
	}
	// Swap case: parked on a complete action name that the index located as a
	// reference. Derive the owning dependsOn path from the reference position.
	if cc.Kind == CursorSymbolRef && cc.Ref != nil && cc.Ref.Symbol.Kind == refindex.SymbolAction {
		if base, ok := dependsOnPathAt(entry, cc.Ref.Range.Start); ok {
			// Empty partial for a defined name (offer all in-section to swap);
			// otherwise keep it as the filter prefix.
			if _, defined := entry.Index.Definition(refindex.SymbolAction, cc.Ref.Symbol.Name); defined {
				return base, "", true
			}
			return base, cc.Ref.Symbol.Name, true
		}
	}
	return "", "", false
}

// isActionDependsOnPath reports whether a dependsOn path belongs to a workflow
// action (either section) rather than a resolver.
func isActionDependsOnPath(path string) bool {
	return strings.Contains(path, ".workflow.actions.") || strings.Contains(path, ".workflow.finally.")
}

// workflowSectionFromPath returns the workflow section ("actions" or "finally")
// a path lies within, or ok=false when it is in neither.
func workflowSectionFromPath(path string) (string, bool) {
	switch {
	case strings.Contains(path, ".workflow.finally."):
		return action.SectionFinally, true
	case strings.Contains(path, ".workflow.actions."):
		return action.SectionActions, true
	default:
		return "", false
	}
}

// refContentColumn returns the column at which the refindex records a
// whole-scalar reference for value node n -- the scalar's CONTENT start, which
// for a quoted scalar is one column past the opening quote (mirroring
// refindex's contentStart). It is why dependsOnPathAt must not compare against
// n.Column directly: a quoted dependsOn entry like `- "alphaMain"` has its
// reference at n.Column+1, so an exact-column match on n.Column would miss it
// and the swap case would fall back to the cross-section index. dependsOn values
// are always plain or quoted flow scalars, so only the quote shift applies;
// block (literal/folded) styles do not occur for an action-name reference.
func refContentColumn(n *yaml.Node) int {
	if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
		return n.Column + 1
	}
	return n.Column
}

// dependsOnPathAt returns the logical path of the action dependsOn item whose
// value node starts at pos, or ok=false when no such item is there. It maps a
// located reference's position back to its dependsOn path so the swap case can be
// section-scoped like the typing case.
func dependsOnPathAt(entry *DocEntry, pos sourcepos.Position) (string, bool) {
	if entry.Nodes == nil {
		return "", false
	}
	for path, n := range entry.Nodes {
		if n == nil || n.Line != pos.Line || refContentColumn(n) != pos.Column {
			continue
		}
		base := stripIndex(path)
		if lastSegment(base) == "dependsOn" && isActionDependsOnPath(base) {
			return base, true
		}
	}
	return "", false
}

// symbolCompletionTarget classifies the cursor into the symbol kind to complete
// and the partial token typed so far. It reports ok=false when the cursor is not
// on a symbol-reference position this branch handles.
func symbolCompletionTarget(entry *DocEntry, pos protocol.Position, cc CursorContext) (refindex.SymbolKind, string, bool) {
	// 1. CEL / Go-template reference prefixes. ResolveCursor has already isolated
	// the prefix (_. / ._. / __actions. / .__actions.) and the partial token.
	switch cc.Kind { //nolint:exhaustive // only the three reference-bearing kinds are handled here; all others fall through to value/list classification
	case CursorCEL:
		switch cc.ExprPrefix {
		case celResolverPrefix:
			return refindex.SymbolResolver, cc.PartialToken, true
		case celActionPrefix:
			return refindex.SymbolAction, cc.PartialToken, true
		}
	case CursorTemplate:
		switch cc.ExprPrefix {
		case tmplResolverPrefix:
			return refindex.SymbolResolver, cc.PartialToken, true
		case tmplActionPrefix:
			return refindex.SymbolAction, cc.PartialToken, true
		}
	case CursorSymbolRef:
		// The cursor sits on a located reference. The refindex records the token
		// under the cursor as a reference whether it is a complete symbol name or
		// a partial still being typed, so distinguish the two:
		//   - a complete, DEFINED name -> the user is parked on an existing
		//     reference; offer ALL same-kind names (empty partial) so it can be
		//     swapped to another symbol.
		//   - a partial/dangling token -> keep it as the filter prefix so the
		//     suggestion list narrows as the user types (matches the CEL/value
		//     branches' server-side filtering).
		// entry.Index is non-nil here: ResolveCursor only classifies a cursor as
		// CursorSymbolRef when the index is present.
		if cc.Ref != nil {
			kind, name := cc.Ref.Symbol.Kind, cc.Ref.Symbol.Name
			if _, defined := entry.Index.Definition(kind, name); defined {
				return kind, "", true
			}
			return kind, name, true
		}
	}

	// 2. call:/rslvr: values and dependsOn: list items resolve to CursorNone from
	// the completion core (their leaf key is not a CEL/template/enum/provider
	// field), so classify them from the cursor's path and line.
	return valueRefCompletionTarget(entry, pos, cc)
}

// valueRefCompletionTarget handles the non-expression symbol positions: a
// "call:" or "rslvr:" scalar value and a "dependsOn:" sequence item. It prefers
// the parsed path (cc.Path, populated whenever the surrounding YAML parses --
// the common case, since these values stay valid while typing) and falls back to
// re-reading the line for a mid-edit document that does not parse.
func valueRefCompletionTarget(entry *DocEntry, pos protocol.Position, cc CursorContext) (refindex.SymbolKind, string, bool) {
	// Parsed-path classification.
	if base := stripIndex(cc.Path); base != "" {
		partial := nodeValue(cc.Node)
		switch lastSegment(base) {
		case "call":
			return refindex.SymbolCall, partial, true
		case "rslvr":
			return refindex.SymbolResolver, partial, true
		case "dependsOn":
			// Action dependsOn is section-scoped and handled by
			// actionDependsOnCandidates before this generic path runs, so a
			// dependsOn reaching here is a resolver's (single resolver namespace).
			return refindex.SymbolResolver, partial, true
		}
	}

	// Text fallback (unparsed mid-edit): classify from the current line.
	line, ok := lineAt(entry.Raw, int(pos.Line))
	if !ok {
		return 0, "", false
	}
	runes := []rune(line)
	cursor := int(pos.Character)
	if cursor > len(runes) {
		cursor = len(runes)
	}
	key, _, _, valueStart, hasValue := parseKeyLine(runes)
	if !hasValue || cursor < valueStart {
		return 0, "", false
	}
	switch key {
	case "call":
		return refindex.SymbolCall, backwardProviderIdent(line, cursor), true
	case "rslvr":
		return refindex.SymbolResolver, backwardProviderIdent(line, cursor), true
	}
	return 0, "", false
}

// symbolNameItems builds completion items for every DEFINED name of kind in idx
// whose name starts with partial (case-insensitive; an empty partial offers
// all). Only definitions are offered -- idx.Names also includes reference names
// (e.g. the partial token the user is currently typing, or a dangling ref), so
// filtering to names that resolve to a definition keeps the suggestions to
// symbols that actually exist.
func symbolNameItems(idx *refindex.Index, kind refindex.SymbolKind, partial string) []protocol.CompletionItem {
	names := make([]string, 0)
	for _, name := range idx.Names(kind) {
		if _, defined := idx.Definition(kind, name); defined {
			names = append(names, name)
		}
	}
	return completionItemsFromNames(names, kind, partial)
}

// completionItemsFromNames builds and sorts completion items for the given
// symbol names, filtered by the (case-insensitive) partial prefix -- an empty
// partial offers all. The names are assumed to already be the valid candidate
// set (e.g. defined symbols, or a section's actions), so no further existence
// filtering is applied here.
func completionItemsFromNames(names []string, kind refindex.SymbolKind, partial string) []protocol.CompletionItem {
	lower := strings.ToLower(strings.TrimSpace(partial))
	items := make([]protocol.CompletionItem, 0, len(names))
	itemKind := symbolItemKind(kind)
	detail := kind.String()
	for _, name := range names {
		if lower != "" && !strings.HasPrefix(strings.ToLower(name), lower) {
			continue
		}
		items = append(items, newCompletionItem(name, itemKind, detail, ""))
	}
	sortCompletionItems(items)
	return items
}

// symbolItemKind maps a scafctl symbol kind to the LSP completion item kind that
// renders a fitting editor icon: resolvers as variables (values), calls as
// methods (invocations), actions as functions.
func symbolItemKind(kind refindex.SymbolKind) protocol.CompletionItemKind {
	switch kind { //nolint:exhaustive // resolver and function share the Variable icon via the default
	case refindex.SymbolCall:
		return protocol.CompletionItemKindMethod
	case refindex.SymbolAction:
		return protocol.CompletionItemKindFunction
	default:
		// SymbolResolver and SymbolFunction render as a value/variable.
		return protocol.CompletionItemKindVariable
	}
}

// stripIndex removes a trailing sequence "[i]" from a path, so a dependsOn item
// path ("...dependsOn[0]") reduces to its owning key ("...dependsOn"). Paths
// without a trailing index are returned unchanged.
func stripIndex(path string) string {
	if i := strings.LastIndex(path, "["); i >= 0 && strings.HasSuffix(path, "]") {
		return path[:i]
	}
	return path
}

// nodeValue returns a value node's scalar text, or "" when the node is nil.
func nodeValue(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}
