// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package refindex builds a positioned reference index over a solution: every
// occurrence of a symbol (currently resolvers) -- its definition and every
// reference to it -- located by an exact source Range.
//
// It is the shared backbone for editor/refactoring features: find-references
// (all uses of a name), go-to-definition (the defining site), and rename
// (rewrite every occurrence). It composes three layers:
//
//   - walk.Walk provides the semantic structure (which ValueRefs, conditions,
//     and dependsOn entries exist, and their logical paths).
//   - a value-node map + sourcepos.LineIndex turn a logical path into the exact
//     bytes of the underlying YAML scalar (style-aware).
//   - celexp.UnderscoreVariableRefs and gotmpl.GetPositionedReferences locate
//     each reference within an expression/template.
//
// Every emitted reference is validated byte-exact against the raw source: a
// candidate range whose bytes do not equal the referenced name is dropped and
// counted in Unresolved(), so consumers (notably rename) can refuse to act when
// coverage is incomplete rather than corrupt unrelated text.
package refindex

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/walk"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"gopkg.in/yaml.v3"
)

// SymbolKind classifies the kind of entity a reference points at.
type SymbolKind int

const (
	// SymbolResolver is a resolver defined under spec.resolvers.
	SymbolResolver SymbolKind = iota
)

// String returns the lowercase kind name.
func (k SymbolKind) String() string {
	switch k {
	case SymbolResolver:
		return "resolver"
	default:
		return "unknown"
	}
}

// Symbol identifies a named entity in a solution.
type Symbol struct {
	Kind SymbolKind
	Name string
}

// Origin describes the syntactic form a reference took.
type Origin int

const (
	// OriginDefinition is the defining site of the symbol (the map key).
	OriginDefinition Origin = iota
	// OriginDependsOn is an explicit dependsOn list entry.
	OriginDependsOn
	// OriginResolverRef is a rslvr: value reference.
	OriginResolverRef
	// OriginCEL is a _.name reference inside a CEL expression or condition.
	OriginCEL
	// OriginTemplate is an explicit ._.name reference inside a Go template.
	OriginTemplate
)

// String returns a short label for the origin.
func (o Origin) String() string {
	switch o {
	case OriginDefinition:
		return "definition"
	case OriginDependsOn:
		return "dependsOn"
	case OriginResolverRef:
		return "rslvr"
	case OriginCEL:
		return "cel"
	case OriginTemplate:
		return "template"
	default:
		return "unknown"
	}
}

// Reference is a single located occurrence of a symbol.
type Reference struct {
	Symbol Symbol
	Range  sourcepos.Range
	Origin Origin
	// IsDef reports whether this occurrence is the symbol's definition.
	IsDef bool
}

// Index is a queryable set of positioned references over one solution.
type Index struct {
	file   string
	refs   []Reference
	byName map[string][]Reference
	// unresolvedByName counts references that were identified semantically but
	// could not be located byte-exact, attributed to the resolver name they
	// target. This makes the fail-safe name-scoped: a rename of X is blocked
	// only by unlocatable references to X, not by unrelated ones.
	unresolvedByName map[string]int
	// unresolvedOther counts unlocatable references whose target name could not
	// be determined (e.g. an expression/template that failed to parse). These
	// conservatively block every rename, since they might reference any name.
	unresolvedOther int
}

// All returns every reference (definitions and uses), sorted by position.
func (i *Index) All() []Reference {
	out := make([]Reference, len(i.refs))
	copy(out, i.refs)
	return out
}

// Occurrences returns every occurrence of name (definition and uses), sorted by
// position. This is the set a rename must rewrite.
func (i *Index) Occurrences(name string) []Reference {
	src := i.byName[name]
	out := make([]Reference, len(src))
	copy(out, src)
	return out
}

// References returns the non-definition uses of name, sorted by position.
func (i *Index) References(name string) []Reference {
	var out []Reference
	for _, r := range i.byName[name] {
		if !r.IsDef {
			out = append(out, r)
		}
	}
	return out
}

// Definition returns the defining occurrence of name, if one exists.
func (i *Index) Definition(name string) (Reference, bool) {
	for _, r := range i.byName[name] {
		if r.IsDef {
			return r, true
		}
	}
	return Reference{}, false
}

// Names returns the sorted set of symbol names known to the index.
func (i *Index) Names() []string {
	out := make([]string, 0, len(i.byName))
	for n := range i.byName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Unresolved returns the total number of references that were identified
// semantically but could not be located byte-exact in the source. A non-zero
// value means the index is not a complete picture of every reference.
func (i *Index) Unresolved() int {
	total := i.unresolvedOther
	for _, n := range i.unresolvedByName {
		total += n
	}
	return total
}

// UnresolvedFor returns the number of unlocatable references that could affect a
// rename of name: those attributed to name plus any whose target could not be
// determined. A rename of name must refuse when this is non-zero, or it risks a
// partial (solution-breaking) rewrite.
func (i *Index) UnresolvedFor(name string) int {
	return i.unresolvedByName[name] + i.unresolvedOther
}

// Build constructs a positioned reference index for the solution. It returns an
// empty (non-nil) index when sol is nil.
func Build(sol *solution.Solution) (*Index, error) {
	idx := &Index{byName: map[string][]Reference{}, unresolvedByName: map[string]int{}}
	if sol == nil {
		return idx, nil
	}

	raw := sol.RawContent()
	idx.file = sol.GetPath()

	nodes, err := buildValueNodeMap(raw)
	if err != nil {
		return nil, fmt.Errorf("refindex: %w", err)
	}

	b := &builder{
		idx:   idx,
		raw:   raw,
		li:    sourcepos.NewLineIndex(raw, idx.file),
		sm:    sol.SourceMap(),
		nodes: nodes,
	}

	v := &walk.Visitor{
		Resolver: func(path, name string, r *resolver.Resolver) error {
			b.addDefinition(path, name)
			for i, dep := range r.DependsOn {
				b.addWholeScalarRef(fmt.Sprintf("%s.dependsOn[%d]", path, i), dep, OriginDependsOn)
			}
			return nil
		},
		Condition: func(path, _ string, expr *celexp.Expression) error {
			b.addCELRefs(path, expr)
			return nil
		},
		ValueRef: func(path string, vr *spec.ValueRef) error {
			switch {
			case vr.Resolver != nil:
				b.addWholeScalarRef(path+".rslvr", *vr.Resolver, OriginResolverRef)
			case vr.Expr != nil:
				b.addCELRefs(path, vr.Expr)
			case vr.Tmpl != nil:
				b.addTemplateRefs(path+".tmpl", vr.Tmpl)
			case vr.Literal != nil:
				// walk.Walk does not descend into literal maps/arrays, but scafctl
				// resolves inline ValueRefs ({rslvr}/{expr}/{tmpl}) nested inside
				// them. Those references are not positioned here, so record their
				// target names as unresolved to keep rename fail-safe (C1).
				b.markLiteralResolverRefs(vr.Literal)
			}
			return nil
		},
	}

	if err := walk.Walk(sol, v); err != nil {
		return nil, fmt.Errorf("refindex: walk: %w", err)
	}

	idx.finalize()
	return idx, nil
}

// builder accumulates references during the walk.
type builder struct {
	idx   *Index
	raw   []byte
	li    *sourcepos.LineIndex
	sm    *sourcepos.SourceMap
	nodes map[string]*yaml.Node
}

// markUnresolved records that a reference to name could not be located.
func (b *builder) markUnresolved(name string) { b.idx.unresolvedByName[name]++ }

// markUnresolvedUnknown records an unlocatable reference whose target name could
// not be determined (e.g. a parse failure). It conservatively blocks every
// rename.
func (b *builder) markUnresolvedUnknown() { b.idx.unresolvedOther++ }

// addDefinition records the defining site of a resolver, located at its map key.
func (b *builder) addDefinition(path, name string) {
	pos, ok := b.sm.Get(path)
	if !ok {
		b.markUnresolved(name)
		return
	}
	start := b.li.Offset(pos)
	b.emitAt(start, name, OriginDefinition, true)
}

// addWholeScalarRef records a reference whose entire scalar value is the name
// (dependsOn entries, rslvr values).
func (b *builder) addWholeScalarRef(path, name string, origin Origin) {
	node, ok := b.scalarAt(path)
	if !ok {
		b.markUnresolved(name)
		return
	}
	start, ok := b.contentStart(node)
	if !ok {
		b.markUnresolved(name)
		return
	}
	b.emitAt(start, name, origin, false)
}

// addCELRefs records every _.name reference inside a CEL expression. basePath is
// the ValueRef or condition path; the scalar is at basePath.expr (object form)
// or basePath itself (condition scalar shorthand).
//
// It reconciles the positioned references against the authoritative underscore
// variable set: any resolver name the parser reports that a positioned ref did
// not cover is recorded as unresolved, so a rename fails safe.
func (b *builder) addCELRefs(basePath string, expr *celexp.Expression) {
	if expr == nil {
		return
	}
	node := b.celScalar(basePath)
	if node == nil {
		// No scalar to position against; treat all underscore vars as unresolved.
		if names, err := expr.GetUnderscoreVariables(context.Background()); err == nil {
			for _, name := range names {
				b.markUnresolved(name)
			}
		} else {
			b.markUnresolvedUnknown()
		}
		return
	}

	refs, err := expr.UnderscoreVariableRefs(context.Background())
	if err != nil {
		b.markUnresolvedUnknown()
		return
	}
	start, ok := b.contentStart(node)
	if !ok {
		for _, r := range refs {
			b.markUnresolved(r.Name)
		}
		return
	}

	attempted := make(map[string]bool, len(refs))
	for _, r := range refs {
		attempted[r.Name] = true
		if r.Offset < 0 {
			// The occurrence exists but could not be positioned; treat it as
			// unlocatable so a rename of this name fails safe.
			b.markUnresolved(r.Name)
			continue
		}
		b.emitAt(start+r.Offset, r.Name, OriginCEL, false)
	}
	// Cross-check: any authoritative underscore variable that the positioned
	// walker never returned (e.g. an occurrence it could not anchor) is unlocatable.
	if auth, err := expr.GetUnderscoreVariables(context.Background()); err == nil {
		for _, name := range auth {
			if !attempted[name] {
				b.markUnresolved(name)
			}
		}
	}
}

// addTemplateRefs records explicit ._.name resolver references inside a Go
// template, and reconciles against the authoritative unscoped reference set so
// that references it cannot position -- bare {{ .field }}, $-rooted {{ $.name }},
// or an explicit ref whose bytes do not validate -- are recorded as unresolved
// (C2). Bare/$ references are conservatively attributed by name: they only block
// renaming a resolver that shares that exact name.
func (b *builder) addTemplateRefs(path string, tmpl *gotmpl.GoTemplatingContent) {
	if tmpl == nil {
		return
	}
	tmplStr := string(*tmpl)

	candidates, cerr := gotmpl.UnscopedResolverRefs(tmplStr, "", "")
	if cerr != nil {
		b.markUnresolvedUnknown()
		return
	}

	node, ok := b.scalarAt(path)
	if !ok {
		for _, c := range candidates {
			b.markUnresolved(c.Name)
		}
		return
	}
	start, ok := b.contentStart(node)
	if !ok {
		for _, c := range candidates {
			b.markUnresolved(c.Name)
		}
		return
	}

	refs, err := gotmpl.GetGoTemplatePositionedReferences(tmplStr, "", "")
	if err != nil {
		b.markUnresolvedUnknown()
		return
	}

	// Emit the explicit resolver references we can position byte-exact.
	positionedExplicit := make(map[string]bool)
	for _, r := range refs {
		if r.Scoped || r.Kind != gotmpl.RefKindExplicitResolver {
			continue
		}
		if b.emitAt(start+r.Offset, r.Name, OriginTemplate, false) {
			positionedExplicit[r.Name] = true
		}
	}

	// Reconcile against the authoritative candidate set.
	for _, c := range candidates {
		if c.Explicit {
			if !positionedExplicit[c.Name] {
				b.markUnresolved(c.Name)
			}
			continue
		}
		// Bare or $-rooted reference: cannot be safely positioned.
		b.markUnresolved(c.Name)
	}
}

// emitAt validates that the bytes at [start, start+len(name)) equal name and, if
// so, records the reference and returns true. A mismatch (or out-of-range span)
// is attributed to name as unresolved and returns false.
func (b *builder) emitAt(start int, name string, origin Origin, isDef bool) bool {
	end := start + len(name)
	if start < 0 || end > len(b.raw) || string(b.raw[start:end]) != name {
		b.markUnresolved(name)
		return false
	}
	b.idx.record(Reference{
		Symbol: Symbol{Kind: SymbolResolver, Name: name},
		Range:  b.li.Range(start, end),
		Origin: origin,
		IsDef:  isDef,
	})
	return true
}

// maxLiteralDepth bounds recursion into literal maps/arrays when scanning for
// nested inline ValueRefs, mirroring spec's nested ValueRef depth limit.
const maxLiteralDepth = 32

// markLiteralResolverRefs scans a literal value for inline ValueRef patterns
// ({rslvr}/{expr}/{tmpl}) nested inside maps/arrays and records the resolver
// names they reference as unresolved. These nested references are real (scafctl
// resolves them at runtime) but are not positioned by this index, so recording
// them keeps rename fail-safe rather than silently missing them.
func (b *builder) markLiteralResolverRefs(v any) {
	b.markLiteralResolverRefsDepth(v, 0)
}

func (b *builder) markLiteralResolverRefsDepth(v any, depth int) {
	if depth > maxLiteralDepth {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		if payload, kind, ok := literalValueRef(t); ok {
			b.markLiteralValueRef(payload, kind)
			return
		}
		for _, elem := range t {
			b.markLiteralResolverRefsDepth(elem, depth+1)
		}
	case []any:
		for _, elem := range t {
			b.markLiteralResolverRefsDepth(elem, depth+1)
		}
	}
}

// markLiteralValueRef records the resolver names referenced by a single nested
// inline ValueRef as unresolved.
func (b *builder) markLiteralValueRef(payload, kind string) {
	switch kind {
	case "rslvr":
		b.markUnresolved(payload)
	case "expr":
		expr := celexp.Expression(payload)
		if names, err := expr.GetUnderscoreVariables(context.Background()); err == nil {
			for _, n := range names {
				b.markUnresolved(n)
			}
		} else {
			b.markUnresolvedUnknown()
		}
	case "tmpl":
		if cands, err := gotmpl.UnscopedResolverRefs(payload, "", ""); err == nil {
			for _, c := range cands {
				b.markUnresolved(c.Name)
			}
		} else {
			b.markUnresolvedUnknown()
		}
	}
}

// literalValueRef reports whether m is a single-key inline ValueRef map
// ({rslvr}/{expr}/{tmpl}) with a string payload, returning the payload and key.
func literalValueRef(m map[string]any) (payload, kind string, ok bool) {
	if len(m) != 1 {
		return "", "", false
	}
	for _, k := range []string{"rslvr", "expr", "tmpl"} {
		v, present := m[k]
		if !present {
			continue
		}
		s, isStr := v.(string)
		if !isStr {
			return "", "", false
		}
		return s, k, true
	}
	return "", "", false
}

// scalarAt returns the scalar value node at path, if present.
func (b *builder) scalarAt(path string) (*yaml.Node, bool) {
	n, ok := b.nodes[path]
	if !ok || n.Kind != yaml.ScalarNode {
		return nil, false
	}
	return n, true
}

// celScalar returns the CEL scalar node for a ValueRef/condition path, trying
// the object form (path.expr) before the condition scalar shorthand (path).
func (b *builder) celScalar(basePath string) *yaml.Node {
	if n, ok := b.scalarAt(basePath + ".expr"); ok {
		return n
	}
	if n, ok := b.scalarAt(basePath); ok {
		return n
	}
	return nil
}

// contentStart returns the byte offset of a scalar's content, adjusting for
// quote style and block indicators.
func (b *builder) contentStart(n *yaml.Node) (int, bool) {
	base := b.li.Offset(sourcepos.Position{Line: n.Line, Column: n.Column})
	switch {
	case n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0:
		return base + 1, true
	case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		// The node position points at the block indicator; content begins at the
		// first non-empty decoded line within the raw bytes after base.
		first := firstNonEmptyLine(n.Value)
		if first == "" {
			return base, true
		}
		if i := bytes.Index(b.raw[base:], []byte(first)); i >= 0 {
			return base + i, true
		}
		return base, false
	default:
		return base, true
	}
}

// finalize sorts references by position for deterministic output.
func (i *Index) finalize() {
	sort.SliceStable(i.refs, func(a, c int) bool { return lessRange(i.refs[a].Range, i.refs[c].Range) })
	for name := range i.byName {
		refs := i.byName[name]
		sort.SliceStable(refs, func(a, c int) bool { return lessRange(refs[a].Range, refs[c].Range) })
		i.byName[name] = refs
	}
}

func (i *Index) record(r Reference) {
	i.refs = append(i.refs, r)
	i.byName[r.Symbol.Name] = append(i.byName[r.Symbol.Name], r)
}

func lessRange(a, b sourcepos.Range) bool {
	if a.Start.Line != b.Start.Line {
		return a.Start.Line < b.Start.Line
	}
	return a.Start.Column < b.Start.Column
}

// buildValueNodeMap parses raw YAML and returns a map from logical path to the
// VALUE node at that path, using the same path scheme as sourcepos and walk.Walk
// (dotted keys, [i] for sequence elements).
func buildValueNodeMap(raw []byte) (map[string]*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	m := make(map[string]*yaml.Node)
	var walkNode func(n *yaml.Node, path string)
	walkNode = func(n *yaml.Node, path string) {
		if n == nil {
			return
		}
		switch n.Kind {
		case yaml.DocumentNode:
			for _, c := range n.Content {
				walkNode(c, path)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				key := n.Content[i].Value
				val := n.Content[i+1]
				childPath := key
				if path != "" {
					childPath = path + "." + key
				}
				m[childPath] = val
				walkNode(val, childPath)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				childPath := fmt.Sprintf("%s[%d]", path, i)
				m[childPath] = c
				walkNode(c, childPath)
			}
		case yaml.ScalarNode:
			// Leaf node: nothing to recurse into. Scalars are recorded by their
			// parent mapping/sequence at the appropriate path.
		case yaml.AliasNode:
			if n.Alias != nil {
				walkNode(n.Alias, path)
			}
		}
	}
	walkNode(&doc, "")
	return m, nil
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
