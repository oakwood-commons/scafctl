// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"slices"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// signatureHelpTriggerCharacters (re)trigger signature help: "(" opens a CEL
// call's argument list, " " separates template function arguments.
var signatureHelpTriggerCharacters = []string{"(", " "}

// signatureHelpFeature registers textDocument/signatureHelp and advertises the
// provider with its trigger characters.
func signatureHelpFeature() feature {
	return feature{
		name: "signatureHelp",
		wire: func(h *protocol.Handler, s *Server) {
			h.TextDocumentSignatureHelp = s.signatureHelp
		},
		advertise: func(c *protocol.ServerCapabilities) {
			c.SignatureHelpProvider = &protocol.SignatureHelpOptions{
				TriggerCharacters: signatureHelpTriggerCharacters,
			}
		},
	}
}

// signatureHelp answers textDocument/signatureHelp by locating the call the
// cursor is inside and describing its parameters. It returns nil (no help) when
// the cursor is not inside a recognized call and never panics.
func (s *Server) signatureHelp(_ *glsp.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	return SignatureHelp(entry, params.Position), nil
}

// SignatureHelp computes signature help for the cursor position against a cached
// document snapshot. It recognizes three call contexts: a Go-template function
// invocation ({{ fn ... }}), a CEL function call (fn(...)), and a call's args:
// block. Returns nil when none applies.
func SignatureHelp(entry *DocEntry, pos protocol.Position) *protocol.SignatureHelp {
	if entry == nil {
		return nil
	}
	line, ok := lineAt(entry.Raw, int(pos.Line))
	if !ok {
		return nil
	}
	runes := []rune(line)
	cursor := int(pos.Character)
	if cursor > len(runes) {
		cursor = len(runes)
	}

	// Template action: {{ fn arg1 arg2 ... }}.
	if action, inAction := enclosingAction(line, cursor); inAction {
		if sh := templateSignature(entry, action); sh != nil {
			return sh
		}
	}
	// CEL function call: fn(arg1, arg2, ...).
	if sh := celSignature(runes, cursor); sh != nil {
		return sh
	}
	// A call's args: block.
	if sh := callArgsSignature(entry, pos, runes); sh != nil {
		return sh
	}
	return nil
}

// templateSignature builds signature help for a Go-template function invocation.
// action is the text from just after "{{" up to the cursor. The first token is
// the function name; subsequent space-separated tokens are its positional
// arguments. Author-defined functions (spec.functions) contribute declared
// parameters; a built-in template function contributes its name and description.
func templateSignature(entry *DocEntry, action string) *protocol.SignatureHelp {
	// Strip a leading pipe segment: in "{{ .x | fn a", the active call is "fn a".
	// (The piped value binds fn's trailing positional slot, which is not the
	// argument the user is typing, so it is not the highlighted one.)
	if i := strings.LastIndex(action, "|"); i >= 0 {
		action = action[i+1:]
	}
	trailingSpace := strings.HasSuffix(action, " ")
	tokens := strings.Fields(action)
	if len(tokens) == 0 {
		return nil
	}
	fn := tokens[0]
	active := len(tokens) - 1 // args typed so far
	if !trailingSpace && active > 0 {
		active-- // the last token is the arg currently being typed
	}

	// Author-defined function: positional params.
	if entry != nil && entry.Sol != nil {
		if f, ok := entry.Sol.Spec.Functions[fn]; ok && f != nil {
			return paramDefSignature(fn, f.Description, f.Params, active)
		}
	}
	// Built-in template function: no declared params, show name + description.
	if fi, ok := LookupFunc(fn); ok && !fi.CEL {
		info := protocol.SignatureInformation{Label: fn}
		if doc := funcDocMarkdown(fi); doc != "" {
			info.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: doc}
		}
		return &protocol.SignatureHelp{Signatures: []protocol.SignatureInformation{info}}
	}
	return nil
}

// celSignature builds signature help for a CEL function call fn(...) whose open
// parenthesis encloses the cursor. The active parameter is the number of
// top-level commas between the "(" and the cursor.
func celSignature(runes []rune, cursor int) *protocol.SignatureHelp {
	open, ok := enclosingCallParen(runes, cursor)
	if !ok {
		return nil
	}
	fn := identifierBefore(runes, open)
	if fn == "" {
		return nil
	}
	fi, ok := LookupFunc(fn)
	if !ok || !fi.CEL {
		return nil
	}
	active := topLevelCommas(runes, open+1, cursor)
	params := signatureParams(fi.Signature)
	label := fi.Signature
	if label == "" {
		label = fn + "()"
	}
	info := protocol.SignatureInformation{
		Label:      label,
		Parameters: make([]protocol.ParameterInformation, 0, len(params)),
	}
	for _, p := range params {
		info.Parameters = append(info.Parameters, protocol.ParameterInformation{Label: p})
	}
	if doc := funcDocMarkdown(fi); doc != "" {
		info.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: doc}
	}
	setActiveParameter(&info, active)
	return &protocol.SignatureHelp{Signatures: []protocol.SignatureInformation{info}}
}

// callArgsSignature builds signature help when the cursor is inside the args:
// block of a call invocation, listing the invoked call's declared arguments.
func callArgsSignature(entry *DocEntry, pos protocol.Position, runes []rune) *protocol.SignatureHelp {
	if entry == nil || entry.Sol == nil {
		return nil
	}
	// If the cursor is on the "call:" invocation line (or the "args:" key line)
	// itself, it is not inside an args value, so it must not borrow a sibling
	// call's arguments.
	if key, _, _, _, _ := parseKeyLine(runes); key == "call" || key == "args" {
		return nil
	}
	name, ok := enclosingCallName(entry.Raw, int(pos.Line))
	if !ok {
		return nil
	}
	call, ok := entry.Sol.Spec.Calls[name]
	if !ok || call == nil || len(call.Args) == 0 {
		return nil
	}

	names := sortedKeys(call.Args)
	info := protocol.SignatureInformation{
		Label:      fmt.Sprintf("%s(%s)", name, strings.Join(argLabels(call.Args, names), ", ")),
		Parameters: make([]protocol.ParameterInformation, 0, len(names)),
	}
	for _, n := range names {
		p := protocol.ParameterInformation{Label: argLabel(n, call.Args[n])}
		if call.Args[n] != nil && call.Args[n].Description != "" {
			p.Documentation = call.Args[n].Description
		}
		info.Parameters = append(info.Parameters, p)
	}
	if call.Description != "" {
		info.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: call.Description}
	}
	// Highlight the argument key currently on the cursor's line, if any.
	if key, _, _, _, _ := parseKeyLine(runes); key != "" {
		for i, n := range names {
			if n == key {
				//nolint:gosec // i is a small slice index
				setActiveParameter(&info, uint32(i))
				break
			}
		}
	}
	return &protocol.SignatureHelp{Signatures: []protocol.SignatureInformation{info}}
}

// paramDefSignature builds a signature from ordered positional parameter
// declarations (author functions).
func paramDefSignature(fn, description string, params []*spec.ParamDef, active int) *protocol.SignatureHelp {
	labels := make([]string, 0, len(params))
	for _, p := range params {
		if p != nil {
			labels = append(labels, paramDefLabel(p))
		}
	}
	info := protocol.SignatureInformation{
		Label:      fmt.Sprintf("%s(%s)", fn, strings.Join(labels, ", ")),
		Parameters: make([]protocol.ParameterInformation, 0, len(params)),
	}
	for _, p := range params {
		if p == nil {
			continue
		}
		pi := protocol.ParameterInformation{Label: paramDefLabel(p)}
		if p.Description != "" {
			pi.Documentation = p.Description
		}
		info.Parameters = append(info.Parameters, pi)
	}
	if description != "" {
		info.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: description}
	}
	if active >= 0 && active < len(info.Parameters) {
		//nolint:gosec // active is a small, bounded index
		setActiveParameter(&info, uint32(active))
	}
	return &protocol.SignatureHelp{Signatures: []protocol.SignatureInformation{info}}
}

// paramDefLabel renders a parameter as "name: type" (or "name" when untyped),
// marking required parameters.
func paramDefLabel(p *spec.ParamDef) string {
	label := p.Name
	if t := string(p.Type); t != "" {
		label += ": " + t
	}
	if p.Required {
		label += "!"
	}
	return label
}

// argLabel renders a call argument as "name: type", marking required arguments.
func argLabel(name string, a *spec.ArgDef) string {
	label := name
	if a != nil {
		if t := string(a.Type); t != "" {
			label += ": " + t
		}
		if a.Required {
			label += "!"
		}
	}
	return label
}

func argLabels(args map[string]*spec.ArgDef, order []string) []string {
	out := make([]string, 0, len(order))
	for _, n := range order {
		out = append(out, argLabel(n, args[n]))
	}
	return out
}

func sortedKeys(args map[string]*spec.ArgDef) []string {
	out := make([]string, 0, len(args))
	for k := range args {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// setActiveParameter sets the 0-based active parameter index on a signature.
func setActiveParameter(info *protocol.SignatureInformation, idx uint32) {
	v := idx
	info.ActiveParameter = &v
}

// enclosingCallParen returns the rune index of the innermost unmatched "(" that
// encloses the cursor, ignoring parentheses inside quoted strings. ok is false
// when the cursor is not inside a parenthesized call.
func enclosingCallParen(runes []rune, cursor int) (int, bool) {
	var stack []int
	inStr := rune(0)
	for i := 0; i < cursor && i < len(runes); i++ {
		r := runes[i]
		switch {
		case inStr != 0:
			if r == inStr {
				inStr = 0
			}
		case r == '"' || r == '\'':
			inStr = r
		case r == '(':
			stack = append(stack, i)
		case r == ')':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(stack) == 0 {
		return 0, false
	}
	return stack[len(stack)-1], true
}

// identifierBefore returns the CEL identifier (including dotted namespaces like
// "arrays.groupBy") ending immediately before index i.
func identifierBefore(runes []rune, i int) string {
	end := i
	start := end
	for start > 0 {
		r := runes[start-1]
		if isIdentRune(r) || r == '.' {
			start--
			continue
		}
		break
	}
	return strings.Trim(string(runes[start:end]), ".")
}

// topLevelCommas counts commas between [from, to) that are not nested inside
// parentheses, brackets, braces, or angle brackets, and not inside strings.
func topLevelCommas(runes []rune, from, to int) uint32 {
	if to > len(runes) {
		to = len(runes)
	}
	var count, depth uint32
	inStr := rune(0)
	for i := from; i < to; i++ {
		r := runes[i]
		switch {
		case inStr != 0:
			if r == inStr {
				inStr = 0
			}
		case r == '"' || r == '\'':
			inStr = r
		case r == '(' || r == '[' || r == '{' || r == '<':
			depth++
		case r == ')' || r == ']' || r == '}' || r == '>':
			if depth > 0 {
				depth--
			}
		case r == ',' && depth == 0:
			count++
		}
	}
	return count
}

// signatureParams extracts the parameter type list from a CEL signature such as
// "arrays.groupBy(list<map<string,dyn>>, string) -> map<...>": the substring
// between the first "(" and its matching ")", split on top-level commas.
func signatureParams(signature string) []string {
	open := strings.IndexByte(signature, '(')
	if open < 0 {
		return nil
	}
	var depth int
	var params []string
	var cur strings.Builder
	inStr := rune(0)
	for _, r := range signature[open+1:] {
		switch {
		case inStr != 0:
			if r == inStr {
				inStr = 0
			}
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			inStr = r
			cur.WriteRune(r)
		case r == '(' || r == '[' || r == '{' || r == '<':
			depth++
			cur.WriteRune(r)
		case r == ')':
			if depth == 0 {
				params = appendParam(params, cur.String())
				return params
			}
			depth--
			cur.WriteRune(r)
		case r == ']' || r == '}' || r == '>':
			if depth > 0 {
				depth--
			}
			cur.WriteRune(r)
		case r == ',' && depth == 0:
			params = appendParam(params, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return params
}

func appendParam(params []string, s string) []string {
	if t := strings.TrimSpace(s); t != "" {
		return append(params, t)
	}
	return params
}

// enclosingCallName scans upward from the given 0-based line to find the call
// name of an enclosing "args:" block: the nearest "call:" mapping (seen after an
// "args:" key on the way up) whose value names the invoked call. ok is false when
// the cursor is not inside an args block. Callers should first exclude the case
// where the cursor is on the "call:"/"args:" key line itself.
func enclosingCallName(raw []byte, line int) (string, bool) {
	// Require an enclosing "args:" ancestor before the "call:" sibling.
	sawArgs := false
	for l := line - 1; l >= 0; l-- {
		text, ok := lineAt(raw, l)
		if !ok {
			continue
		}
		lr := []rune(text)
		key, _, _, valStart, hasVal := parseKeyLine(lr)
		if key == "" {
			continue
		}
		if key == "args" {
			sawArgs = true
			continue
		}
		if key == "call" && sawArgs && hasVal {
			name := strings.TrimSpace(string(lr[valStart:]))
			name = strings.Trim(name, `"'`)
			if name != "" {
				return name, true
			}
		}
	}
	return "", false
}
