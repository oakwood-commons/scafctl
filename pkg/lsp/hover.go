// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/concepts"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// hoverFeature registers textDocument/hover. glsp derives the HoverProvider
// capability from the wired handler, so no advertise func is needed.
func hoverFeature() feature {
	return feature{
		name: "hover",
		wire: func(h *protocol.Handler, s *Server) {
			h.TextDocumentHover = s.hover
		},
	}
}

// hover answers textDocument/hover by classifying the cursor and rendering
// markdown from existing sources (symbol descriptions, provider descriptors,
// function metadata, schema field docs). It returns nil (no hover) for unknown
// targets and never panics on a parse-error/unknown document.
func (s *Server) hover(_ *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	cc := ResolveCursor(entry, params.Position)
	md := s.hoverMarkdown(entry, cc, params.Position)
	if md == "" {
		return nil, nil
	}
	hov := &protocol.Hover{
		Contents: protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: md},
	}
	if cc.Kind == CursorSymbolRef && cc.Ref != nil {
		rng := toLSPRange(cc.Ref.Range)
		hov.Range = &rng
	}
	return hov, nil
}

// hoverMarkdown renders the hover body for a resolved cursor context, or "" when
// there is nothing useful to show.
func (s *Server) hoverMarkdown(entry *DocEntry, cc CursorContext, pos protocol.Position) string {
	switch cc.Kind {
	case CursorSymbolRef:
		return symbolHover(entry, cc.Ref)
	case CursorProviderName:
		return s.providerHover(cc)
	case CursorCEL, CursorTemplate:
		// Only bare function-name tokens (no reference prefix) have function
		// metadata; a prefixed token is a data/resolver reference.
		if cc.ExprPrefix != "" {
			return ""
		}
		return funcHover(fullIdentAt(entry.Raw, pos))
	case CursorYAMLKey:
		return keyHover(cc.Path)
	case CursorNone, CursorEnumValue:
		return ""
	default:
		return ""
	}
}

// symbolHover renders a symbol reference's kind, name, and description (from the
// parsed solution).
func symbolHover(entry *DocEntry, ref *refindex.Reference) string {
	if ref == nil {
		return ""
	}
	kind := ref.Symbol.Kind
	name := ref.Symbol.Name
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** `%s`", kind.String(), name)
	if desc := symbolDescription(entry.Sol, kind, name); desc != "" {
		b.WriteString("\n\n")
		b.WriteString(desc)
	}
	return b.String()
}

// symbolDescription looks up the description of a (kind, name) symbol in the
// parsed solution. Returns "" when the solution is unavailable or the symbol has
// no description.
func symbolDescription(sol *solution.Solution, kind refindex.SymbolKind, name string) string {
	if sol == nil {
		return ""
	}
	switch kind {
	case refindex.SymbolResolver:
		if r, ok := sol.Spec.Resolvers[name]; ok && r != nil {
			return r.Description
		}
	case refindex.SymbolCall:
		if c, ok := sol.Spec.Calls[name]; ok && c != nil {
			return c.Description
		}
	case refindex.SymbolFunction:
		if f, ok := sol.Spec.Functions[name]; ok && f != nil {
			return f.Description
		}
	case refindex.SymbolAction:
		if sol.Spec.Workflow != nil {
			if a, ok := sol.Spec.Workflow.Actions[name]; ok && a != nil {
				return a.Description
			}
			if a, ok := sol.Spec.Workflow.Finally[name]; ok && a != nil {
				return a.Description
			}
		}
	}
	return ""
}

// providerHover renders a provider descriptor's identity, description, and input
// schema summary. Returns "" when the provider is unknown or the registry is
// unavailable.
func (s *Server) providerHover(cc CursorContext) string {
	if s.registry == nil || cc.Node == nil {
		return ""
	}
	name := cc.Node.Value
	p, ok := s.registry.Get(name)
	if !ok {
		return ""
	}
	d := p.Descriptor()
	var b strings.Builder
	title := d.Name
	if d.DisplayName != "" {
		title = d.DisplayName
	}
	fmt.Fprintf(&b, "**provider** `%s`", title)
	if d.Description != "" {
		b.WriteString("\n\n")
		b.WriteString(d.Description)
	}
	if summary := providerInputSummary(d); summary != "" {
		b.WriteString("\n\n**Inputs:**\n")
		b.WriteString(summary)
	}
	return b.String()
}

// providerInputSummary lists a provider's input properties (name, type, required
// marker, description) from its JSON schema.
func providerInputSummary(d *provider.Descriptor) string {
	if d.Schema == nil || len(d.Schema.Properties) == 0 {
		return ""
	}
	required := make(map[string]bool, len(d.Schema.Required))
	for _, r := range d.Schema.Required {
		required[r] = true
	}
	names := make([]string, 0, len(d.Schema.Properties))
	for n := range d.Schema.Properties {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, n := range names {
		prop := d.Schema.Properties[n]
		typ := ""
		if prop != nil && prop.Type != "" {
			typ = " (" + prop.Type + ")"
		}
		req := ""
		if required[n] {
			req = " *required*"
		}
		fmt.Fprintf(&b, "- `%s`%s%s", n, typ, req)
		if prop != nil && prop.Description != "" {
			fmt.Fprintf(&b, " -- %s", firstLine(prop.Description))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// funcHover renders a CEL/template function's signature, description, and first
// example. Returns "" when name is not a known function.
func funcHover(name string) string {
	if name == "" {
		return ""
	}
	fi, ok := LookupFunc(name)
	if !ok {
		return ""
	}
	lang := "template"
	if fi.CEL {
		lang = "CEL"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%s function** `%s`", lang, fi.Name)
	if fi.Signature != "" {
		fmt.Fprintf(&b, "\n\n```\n%s\n```", fi.Signature)
	}
	if fi.Description != "" {
		b.WriteString("\n\n")
		b.WriteString(fi.Description)
	}
	if len(fi.Examples) > 0 {
		fmt.Fprintf(&b, "\n\n_Example:_\n\n```\n%s\n```", fi.Examples[0])
	}
	return b.String()
}

// keyHover renders the schema documentation for a mapping key at a logical path,
// plus a related concept summary when one matches the key name.
func keyHover(path string) string {
	var b strings.Builder
	// Derive the displayed name and concept key from the NORMALIZED path so the
	// label matches the field actually introspected. fieldInfoForPath normalizes
	// via schemaPath, which strips dynamic map-key segments (e.g. a resolver or
	// input name under a mapContainerKeys parent). Labeling with the raw last
	// segment would show the container field's doc under the wrong (instance)
	// name -- e.g. hovering "myInput" under inputs: would display the inputs
	// field doc labeled "myInput".
	label := lastSegment(schemaPath(path))
	if fi, ok := fieldInfoForPath(path); ok && fi.Description != "" {
		fmt.Fprintf(&b, "**field** `%s`", label)
		if fi.Type != "" {
			fmt.Fprintf(&b, " (%s)", fi.Type)
		}
		b.WriteString("\n\n")
		b.WriteString(fi.Description)
	}
	if c, ok := concepts.Get(label); ok {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "**concept: %s** -- %s", c.Title, c.Summary)
	}
	return b.String()
}

// fieldInfoForPath introspects the solution schema for the field at a logical
// path. The path is normalized first: sequence indices ([i]) and dynamic
// map-key segments (resolver/action/call/function/input names) are removed
// because the schema introspector navigates struct/field and map-value types,
// not concrete keys.
func fieldInfoForPath(path string) (*schema.FieldInfo, bool) {
	if path == "" {
		return nil, false
	}
	fi, err := schema.IntrospectField((*solution.Solution)(nil), schemaPath(path))
	if err != nil || fi == nil {
		return nil, false
	}
	return fi, true
}

// mapContainerKeys are the fields whose values are keyed by a dynamic name; the
// segment immediately after one of these is a concrete key, not a schema field,
// so it is dropped when normalizing a logical path for schema introspection.
var mapContainerKeys = map[string]struct{}{
	"resolvers": {},
	"calls":     {},
	"functions": {},
	"actions":   {},
	"finally":   {},
	"inputs":    {},
	"args":      {},
}

// schemaPath normalizes a logical YAML path into the field path expected by
// schema.IntrospectField: it strips [i] indices and the dynamic map-key segment
// that follows any mapContainerKeys entry.
func schemaPath(path string) string {
	segs := strings.Split(stripIndices(path), ".")
	out := make([]string, 0, len(segs))
	skipNext := false
	for _, seg := range segs {
		if skipNext {
			skipNext = false
			continue
		}
		out = append(out, seg)
		if _, ok := mapContainerKeys[seg]; ok {
			skipNext = true
		}
	}
	return strings.Join(out, ".")
}

// stripIndices removes [i] sequence subscripts from a logical path so it matches
// the struct-field navigation used by schema.IntrospectField.
func stripIndices(path string) string {
	var b strings.Builder
	depth := 0
	for _, r := range path {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// fullIdentAt returns the complete function identifier under pos, scanning both
// backward and forward over identifier runes (and '.', so namespaced names like
// "arrays.groupBy" are captured whole) on the cursor's line. Used to recover a
// function name from a cursor that may sit mid-token.
func fullIdentAt(raw []byte, pos protocol.Position) string {
	line, ok := lineAt(raw, int(pos.Line))
	if !ok {
		return ""
	}
	runes := []rune(line)
	col := int(pos.Character)
	if col > len(runes) {
		col = len(runes)
	}
	isTok := func(r rune) bool { return isIdentRune(r) || r == '.' }
	start := col
	for start > 0 && isTok(runes[start-1]) {
		start--
	}
	end := col
	for end < len(runes) && isTok(runes[end]) {
		end++
	}
	return strings.Trim(string(runes[start:end]), ".")
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}
