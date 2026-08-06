// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// funcCompletions offers function-name completions inside a CEL expression or a
// Go template. CEL context lists CEL functions; template context lists Go-template
// functions plus the solution's author-defined helpers (spec.functions). It
// reuses the shared funcref.go registry view (no duplicate registry walking).
//
// It fires only for a bare function token (ExprPrefix == ""); a reference-prefixed
// token ("_.", "._.", "__actions.", ".") is a data/symbol reference handled by
// the SymbolRef completion branch (#774), not a function.
func funcCompletions(entry *DocEntry, cc CursorContext) []protocol.CompletionItem {
	if cc.ExprPrefix != "" {
		return nil
	}
	prefix := strings.ToLower(cc.PartialToken)
	wantCEL := cc.Kind == CursorCEL

	// AllFuncs() de-duplicates by name (CEL wins on a collision), so the
	// fn.CEL==wantCEL split is exact only while the CEL and template namespaces
	// don't collide -- true today. A future collision would hide the template
	// entry; funcref's index is the single place to reconcile that if it arises.
	var items []protocol.CompletionItem
	for _, fn := range AllFuncs() {
		if fn.CEL != wantCEL {
			continue
		}
		if !matchesPrefix(fn.Name, prefix) {
			continue
		}
		items = append(items, funcCompletionItem(fn))
	}

	// Template context also completes author-defined helper functions declared in
	// spec.functions.
	if cc.Kind == CursorTemplate && entry != nil && entry.Sol != nil {
		for name, fn := range entry.Sol.Spec.Functions {
			if !matchesPrefix(name, prefix) {
				continue
			}
			doc := ""
			if fn != nil {
				doc = fn.Description
			}
			items = append(items, authorFuncCompletionItem(name, doc))
		}
	}

	sortCompletionItems(items)
	return items
}

// matchesPrefix reports whether name starts with the (already lowercased) prefix.
// An empty prefix matches everything.
func matchesPrefix(name, lowerPrefix string) bool {
	if lowerPrefix == "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(name), lowerPrefix)
}

// funcCompletionItem builds a completion item for a built-in CEL/template
// function, rendering its signature as detail and inserting a call snippet that
// places the cursor between the parentheses.
func funcCompletionItem(fn FuncInfo) protocol.CompletionItem {
	kind := protocol.CompletionItemKindFunction
	item := protocol.CompletionItem{Label: fn.Name, Kind: &kind}

	detail := fn.Signature
	if detail == "" {
		if fn.CEL {
			detail = "CEL function"
		} else {
			detail = "template function"
		}
	}
	item.Detail = &detail

	if doc := funcDocMarkdown(fn); doc != "" {
		item.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: doc}
	}

	// Insert "name($0)" as a snippet so the cursor lands between the parens.
	snippet := fmt.Sprintf("%s($0)", fn.Name)
	format := protocol.InsertTextFormatSnippet
	item.InsertText = &snippet
	item.InsertTextFormat = &format
	return item
}

// authorFuncCompletionItem builds a completion item for a solution author-defined
// template helper function.
func authorFuncCompletionItem(name, description string) protocol.CompletionItem {
	kind := protocol.CompletionItemKindFunction
	detail := "author function"
	item := protocol.CompletionItem{Label: name, Kind: &kind, Detail: &detail}
	if description != "" {
		item.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: description}
	}
	snippet := fmt.Sprintf("%s $0", name)
	format := protocol.InsertTextFormatSnippet
	item.InsertText = &snippet
	item.InsertTextFormat = &format
	return item
}

// funcDocMarkdown renders a function's description and first example as markdown
// for the completion documentation popup.
func funcDocMarkdown(fn FuncInfo) string {
	var b strings.Builder
	if fn.Description != "" {
		b.WriteString(fn.Description)
	}
	if len(fn.Examples) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "_Example:_\n\n```\n%s\n```", fn.Examples[0])
	}
	return b.String()
}
