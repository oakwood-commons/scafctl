// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"fmt"
	"strings"
	"text/template/parse"
)

// RefKind classifies a positioned root-level template reference.
type RefKind int

const (
	// RefKindField is a bare data-context accessor ({{ .name }}). Whether it is
	// a resolver dependency is context-dependent (data-input keys, forEach
	// aliases) and is decided by the caller, not here.
	RefKindField RefKind = iota
	// RefKindExplicitResolver is an explicit resolver reference ({{ ._.name }}
	// or {{ ._name }}), which always names a resolver.
	RefKindExplicitResolver
	// RefKindExplicitAction is an explicit action reference
	// ({{ .__actions.name }}), which always names a workflow action.
	RefKindExplicitAction
	// RefKindFunctionCall is the identifier at the head of a command
	// ({{ name ... }}), i.e. a function invocation. It may name a built-in,
	// an extension function, or an author-defined function; callers filter.
	RefKindFunctionCall
)

// PositionedRef is a single root-level reference occurrence in a Go template,
// with the byte range of the referenced NAME within the template source.
//
// One PositionedRef is emitted per textual occurrence (no de-duplication) so
// tools can locate, highlight, or rewrite each individual use. Special
// variables ({{ .__self }}) and the bare resolver context ({{ ._ }}) are not
// emitted; scoped references (inside {{ with }}/{{ range }} bodies) are emitted
// with Scoped set true so callers can filter them.
type PositionedRef struct {
	// Name is the referenced name with any resolver prefix ("_." / "_") removed.
	Name string

	// Offset is the 0-based BYTE offset of Name within the template source.
	Offset int

	// Len is the byte length of Name. Offset+Len is the exclusive end.
	Len int

	// Kind classifies the reference syntax.
	Kind RefKind

	// Scoped reports that the reference sits inside a {{ with }}/{{ range }}
	// body where dot has been rebound, so it refers to the narrowed context
	// rather than the template root.
	Scoped bool
}

// End returns the exclusive end byte offset of the reference (Offset + Len).
func (r PositionedRef) End() int { return r.Offset + r.Len }

// GetPositionedReferences parses a Go template and returns every root-level
// field reference with the byte range of its root name within the template
// source, in source order. It is the positioned counterpart of GetReferences:
// GetReferences returns deduplicated dotted paths for dependency inference,
// whereas this returns one located occurrence per use for navigation/rewriting.
//
// Chained ({{ (index .a .b) }} handles its args) and nested-pipe references are
// traversed. Variable-rooted references ({{ $.name }}) and chain nodes are not
// yet positioned; callers needing exhaustive coverage should cross-check
// against GetReferences and fail safe when a name lacks a located range.
func (s *Service) GetPositionedReferences(opts TemplateOptions) ([]PositionedRef, error) {
	if opts.Content == "" {
		return nil, fmt.Errorf("template content cannot be empty")
	}

	leftDelim := opts.LeftDelim
	rightDelim := opts.RightDelim
	if leftDelim == "" {
		leftDelim = DefaultLeftDelim
	}
	if rightDelim == "" {
		rightDelim = DefaultRightDelim
	}

	name := opts.Name
	if name == "" {
		name = "unnamed-template"
	}

	trees, err := parse.Parse(name, opts.Content, leftDelim, rightDelim, s.templateFuncNamesWith(opts.DeclaredFuncs))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var out []PositionedRef
	for _, tree := range trees {
		if tree.Root != nil {
			walkPositioned(tree.Root, opts.Content, 0, fieldRootRange, &out)
		}
	}
	return out, nil
}

// GetPositionedActionReferences extracts positioned action references
// ({{ .__actions.name... }}) from a Go template, in source order. The name is
// the action referenced (the segment after __actions). Scoped references
// (inside {{ with }}/{{ range }}) are returned with Scoped set.
func (s *Service) GetPositionedActionReferences(opts TemplateOptions) ([]PositionedRef, error) {
	leftDelim := opts.LeftDelim
	rightDelim := opts.RightDelim
	if leftDelim == "" {
		leftDelim = DefaultLeftDelim
	}
	if rightDelim == "" {
		rightDelim = DefaultRightDelim
	}
	name := opts.Name
	if name == "" {
		name = "unnamed-template"
	}
	if opts.Content == "" {
		return nil, fmt.Errorf("template content cannot be empty")
	}

	trees, err := parse.Parse(name, opts.Content, leftDelim, rightDelim, s.templateFuncNamesWith(opts.DeclaredFuncs))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var out []PositionedRef
	for _, tree := range trees {
		if tree.Root != nil {
			walkPositioned(tree.Root, opts.Content, 0, fieldActionRange, &out)
		}
	}
	return out, nil
}

// GetGoTemplatePositionedActionReferences is a convenience wrapper. declaredFuncs
// registers additional (author-defined) function names so a template invoking
// them parses successfully.
func GetGoTemplatePositionedActionReferences(templateContent, leftDelim, rightDelim string, declaredFuncs []string) ([]PositionedRef, error) {
	return NewService(nil).GetPositionedActionReferences(TemplateOptions{
		Content:       templateContent,
		LeftDelim:     leftDelim,
		RightDelim:    rightDelim,
		DeclaredFuncs: declaredFuncs,
	})
}

// GetGoTemplatePositionedReferences is a convenience wrapper that extracts
// positioned references without constructing a Service. declaredFuncs registers
// additional (author-defined) function names so a template invoking them parses
// successfully.
func GetGoTemplatePositionedReferences(templateContent, leftDelim, rightDelim string, declaredFuncs []string) ([]PositionedRef, error) {
	return NewService(nil).GetPositionedReferences(TemplateOptions{
		Content:       templateContent,
		LeftDelim:     leftDelim,
		RightDelim:    rightDelim,
		DeclaredFuncs: declaredFuncs,
	})
}

// templateFuncNames returns the set of function names known to templates parsed
// by this service (built-ins plus registered custom funcs). parse.Parse only
// checks existence (not the value), so placeholders suffice.
func (s *Service) templateFuncNames() map[string]any {
	funcNames := make(map[string]any, len(s.defaultFuncs)+len(goTemplateBuiltins))
	for _, name := range goTemplateBuiltins {
		funcNames[name] = true
	}
	for k := range s.defaultFuncs {
		funcNames[k] = true
	}
	return funcNames
}

// templateFuncNamesWith returns templateFuncNames plus the given extra names
// (e.g. author-defined spec.functions helpers), so a template invoking them
// parses without an "unknown function" error during reference extraction.
func (s *Service) templateFuncNamesWith(extra []string) map[string]any {
	funcNames := s.templateFuncNames()
	for _, name := range extra {
		funcNames[name] = true
	}
	return funcNames
}

// fieldExtractor computes a PositionedRef from a FieldNode, or reports ok=false
// when the field is not a reference of the kind being collected.
type fieldExtractor func(src string, fn *parse.FieldNode, scoped bool) (PositionedRef, bool)

// walkPositioned recursively walks the parse tree, emitting a PositionedRef for
// each root-level FieldNode that extract accepts. scopeDepth tracks
// {{ with }}/{{ range }} bodies where dot is rebound (matching walkNodes in
// refs.go).
func walkPositioned(node parse.Node, src string, scopeDepth int, extract fieldExtractor, out *[]PositionedRef) {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, child := range n.Nodes {
			walkPositioned(child, src, scopeDepth, extract, out)
		}

	case *parse.ActionNode:
		walkPositionedPipe(n.Pipe, src, scopeDepth, extract, out)

	case *parse.IfNode:
		walkPositionedPipe(n.Pipe, src, scopeDepth, extract, out)
		walkPositioned(n.List, src, scopeDepth, extract, out)
		walkPositioned(n.ElseList, src, scopeDepth, extract, out)

	case *parse.RangeNode:
		// The pipe is evaluated in the outer scope; the body rebinds dot.
		walkPositionedPipe(n.Pipe, src, scopeDepth, extract, out)
		walkPositioned(n.List, src, scopeDepth+1, extract, out)
		walkPositioned(n.ElseList, src, scopeDepth, extract, out)

	case *parse.WithNode:
		walkPositionedPipe(n.Pipe, src, scopeDepth, extract, out)
		walkPositioned(n.List, src, scopeDepth+1, extract, out)
		walkPositioned(n.ElseList, src, scopeDepth, extract, out)

	case *parse.TemplateNode:
		walkPositionedPipe(n.Pipe, src, scopeDepth, extract, out)
	}
}

func walkPositionedPipe(p *parse.PipeNode, src string, scopeDepth int, extract fieldExtractor, out *[]PositionedRef) {
	if p == nil {
		return
	}
	for _, cmd := range p.Cmds {
		for _, arg := range cmd.Args {
			switch a := arg.(type) {
			case *parse.FieldNode:
				if ref, ok := extract(src, a, scopeDepth > 0); ok {
					*out = append(*out, ref)
				}
			case *parse.PipeNode:
				walkPositionedPipe(a, src, scopeDepth, extract, out)
			}
		}
	}
}

// fieldActionRange computes the byte range of an action reference in a FieldNode
// of the form {{ .__actions.name... }}. The name is the segment after __actions.
func fieldActionRange(src string, fn *parse.FieldNode, scoped bool) (PositionedRef, bool) {
	if len(fn.Ident) < 2 || fn.Ident[0] != actionNamespace {
		return PositionedRef{}, false
	}
	fieldStart, ok := fieldStartOffset(src, fn)
	if !ok {
		return PositionedRef{}, false
	}
	name := fn.Ident[1]
	if name == "" {
		return PositionedRef{}, false
	}
	// Segment 1 starts after the leading '.', "__actions", and its trailing '.'.
	offset := fieldStart + 1 + len(fn.Ident[0]) + 1
	return PositionedRef{Name: name, Offset: offset, Len: len(name), Kind: RefKindExplicitAction, Scoped: scoped}, true
}

// fieldRootRange computes the byte range of the resolver-relevant root name of a
// FieldNode, classifying the syntax. It returns ok=false for special variables
// ({{ .__x }}) and the bare resolver context ({{ ._ }}), which never name a
// resolver.
func fieldRootRange(src string, fn *parse.FieldNode, scoped bool) (PositionedRef, bool) {
	if len(fn.Ident) == 0 {
		return PositionedRef{}, false
	}

	fieldStart, ok := fieldStartOffset(src, fn)
	if !ok {
		return PositionedRef{}, false
	}

	// segStart returns the byte offset of segment i within src.
	segStart := func(i int) int {
		off := fieldStart + 1 // skip leading '.'
		for j := 0; j < i; j++ {
			off += len(fn.Ident[j]) + 1 // segment + its trailing '.'
		}
		return off
	}

	id0 := fn.Ident[0]
	switch {
	case len(fn.Ident) >= 2 && id0 == "_":
		// {{ ._.name ... }} -- explicit resolver; the name is the second segment.
		name := fn.Ident[1]
		if name == "" {
			return PositionedRef{}, false
		}
		return PositionedRef{Name: name, Offset: segStart(1), Len: len(name), Kind: RefKindExplicitResolver, Scoped: scoped}, true

	case strings.HasPrefix(id0, "__"), id0 == "_":
		// Special variable ({{ .__self }}) or bare resolver context ({{ ._ }}).
		return PositionedRef{}, false

	case strings.HasPrefix(id0, "_"):
		// {{ ._name ... }} -- explicit resolver; the name is id0 without the
		// leading underscore, so the source range starts one byte in.
		name := id0[1:]
		return PositionedRef{Name: name, Offset: segStart(0) + 1, Len: len(name), Kind: RefKindExplicitResolver, Scoped: scoped}, true

	default:
		// {{ .name ... }} -- bare data-context accessor; the root is id0.
		return PositionedRef{Name: id0, Offset: segStart(0), Len: len(id0), Kind: RefKindField, Scoped: scoped}, true
	}
}

// fieldStartOffset returns the byte offset of the leading '.' of a FieldNode.
//
// text/template's FieldNode.Pos points at the dot BEFORE the FINAL identifier
// (not the leading dot), so the leading position is reconstructed from the full
// field text length. The result is validated against src; if it does not match
// (delimiter/whitespace edge cases), it falls back to locating the field text
// nearest to Pos.
func fieldStartOffset(src string, fn *parse.FieldNode) (int, bool) {
	full := "." + strings.Join(fn.Ident, ".")
	last := fn.Ident[len(fn.Ident)-1]
	start := int(fn.Pos) + 1 + len(last) - len(full)

	if start >= 0 && start+len(full) <= len(src) && src[start:start+len(full)] == full {
		return start, true
	}
	if idx := indexNearest(src, full, int(fn.Pos)); idx >= 0 {
		return idx, true
	}
	return 0, false
}

// indexNearest returns the start index of the occurrence of sub in src whose
// start is nearest to pos, or -1 if sub does not occur.
func indexNearest(src, sub string, pos int) int {
	best := -1
	bestDist := int(^uint(0) >> 1)
	for from := 0; ; {
		i := strings.Index(src[from:], sub)
		if i < 0 {
			break
		}
		at := from + i
		d := at - pos
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			bestDist = d
			best = at
		}
		from = at + 1
	}
	return best
}
