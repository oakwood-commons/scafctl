// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
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
	kind, partial, ok := symbolCompletionTarget(entry, pos, cc)
	if !ok {
		return nil
	}
	return symbolNameItems(entry.Index, kind, partial)
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
			return dependsOnKind(base), partial, true
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

// dependsOnKind returns the symbol kind a dependsOn entry references, from the
// owning path: an action's dependsOn lists action names; a resolver's lists
// resolver names.
func dependsOnKind(path string) refindex.SymbolKind {
	if strings.Contains(path, ".workflow.actions.") || strings.Contains(path, ".workflow.finally.") {
		return refindex.SymbolAction
	}
	return refindex.SymbolResolver
}

// symbolNameItems builds completion items for every DEFINED name of kind in idx
// whose name starts with partial (case-insensitive; an empty partial offers
// all). Only definitions are offered -- idx.Names also includes reference names
// (e.g. the partial token the user is currently typing, or a dangling ref), so
// filtering to names that resolve to a definition keeps the suggestions to
// symbols that actually exist.
func symbolNameItems(idx *refindex.Index, kind refindex.SymbolKind, partial string) []protocol.CompletionItem {
	names := idx.Names(kind)
	lower := strings.ToLower(strings.TrimSpace(partial))
	items := make([]protocol.CompletionItem, 0, len(names))
	itemKind := symbolItemKind(kind)
	detail := kind.String()
	for _, name := range names {
		if _, defined := idx.Definition(kind, name); !defined {
			continue
		}
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
