// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// completionTriggerCharacters are the characters that (re)trigger completion:
// "." inside expressions/templates, ":" after a key, and " " after a list dash
// or key. Downstream branches (#774 symbols, #775 functions) rely on the same
// triggers.
var completionTriggerCharacters = []string{".", ":", " "}

// completionFeature registers textDocument/completion and advertises the
// completion provider with its trigger characters. This PR owns the dispatch
// core; #774 and #775 extend it in their own files (completion_symbols.go /
// completion_funcs.go) with one added dispatch branch each.
func completionFeature() feature {
	return feature{
		name: "completion",
		wire: func(h *protocol.Handler, s *Server) {
			h.TextDocumentCompletion = s.completion
		},
		advertise: func(c *protocol.ServerCapabilities) {
			c.CompletionProvider = &protocol.CompletionOptions{
				TriggerCharacters: completionTriggerCharacters,
			}
		},
	}
}

// completion answers textDocument/completion by classifying the cursor and
// dispatching to a per-class completion source. It returns nil (no completion)
// for classes without a source and never panics on a parse-error/unknown
// document.
func (s *Server) completion(_ *glsp.Context, params *protocol.CompletionParams) (any, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	cc := ResolveCursor(entry, params.Position)
	items := completionItems(entry, cc)
	if len(items) == 0 {
		// SymbolRef branch (#774): scafctl symbol-name completion (resolver/call/
		// action names after _./._./call:/rslvr:/dependsOn). Runs only when the
		// structural/enum/function sources produced nothing, so it never competes.
		items = symbolCompletions(entry, params.Position, cc)
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items, nil
}

// completionItems dispatches a resolved cursor to its completion source. The
// structural (schema key) and enum-value branches live in this file; the CEL and
// Go-template function branch lives in completion_funcs.go, and the SymbolRef
// prefix branch (#774) is added by that PR -- each a single case, so the
// completion PRs never edit each other's sources beyond this switch.
func completionItems(entry *DocEntry, cc CursorContext) []protocol.CompletionItem {
	switch cc.Kind {
	case CursorYAMLKey:
		return keyCompletions(cc)
	case CursorEnumValue:
		return enumCompletions(cc)
	case CursorCEL, CursorTemplate:
		return funcCompletions(entry, cc)
	case CursorNone, CursorSymbolRef, CursorProviderName:
		return nil
	default:
		return nil
	}
}

// keyCompletions offers the valid child keys at the cursor's container, from the
// solution schema, filtered by what the user has typed so far. It returns
// nothing when the cursor is at a dynamic map-key position (naming a
// resolver/action/call/etc.) rather than a schema field position.
func keyCompletions(cc CursorContext) []protocol.CompletionItem {
	parent := parentPath(cc.Path)
	// A key directly under a map container (resolvers/calls/functions/actions/
	// finally/inputs/args) is a user-chosen name, not a schema field, so there
	// is nothing structural to suggest.
	if _, isMapKeyPosition := mapContainerKeys[lastSegment(parent)]; isMapKeyPosition {
		return nil
	}
	fields, ok := schema.FieldsAtPath((*solution.Solution)(nil), schemaPath(parent))
	if !ok {
		return nil
	}

	prefix := strings.ToLower(cc.PartialToken)
	items := make([]protocol.CompletionItem, 0, len(fields))
	for _, f := range fields {
		if f.Name == "" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(f.Name), prefix) {
			continue
		}
		items = append(items, newCompletionItem(f.Name, protocol.CompletionItemKindField, f.Type, f.Description))
	}
	sortCompletionItems(items)
	return items
}

// enumCompletions offers the allowed values of an enum-valued field, filtered by
// what the user has typed so far.
func enumCompletions(cc CursorContext) []protocol.CompletionItem {
	values, ok := enumValueKeys[lastSegment(cc.Path)]
	if !ok {
		return nil
	}
	prefix := strings.ToLower(cc.PartialToken)
	items := make([]protocol.CompletionItem, 0, len(values))
	for _, v := range values {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(v), prefix) {
			continue
		}
		items = append(items, newCompletionItem(v, protocol.CompletionItemKindEnumMember, "", ""))
	}
	sortCompletionItems(items)
	return items
}

// newCompletionItem builds a completion item, attaching a detail (type) and a
// markdown documentation body when provided.
func newCompletionItem(label string, kind protocol.CompletionItemKind, detail, doc string) protocol.CompletionItem {
	k := kind
	item := protocol.CompletionItem{Label: label, Kind: &k}
	if detail != "" {
		d := detail
		item.Detail = &d
	}
	if doc != "" {
		item.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: doc}
	}
	return item
}

// sortCompletionItems orders items alphabetically by label for stable output.
func sortCompletionItems(items []protocol.CompletionItem) {
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
}

// parentPath returns the logical path of the container holding the last segment
// of p (everything before the final "."). It returns "" when p has no parent
// (a top-level key), which FieldsAtPath maps to the root type's fields.
func parentPath(p string) string {
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[:i]
	}
	return ""
}
