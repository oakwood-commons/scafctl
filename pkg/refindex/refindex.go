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

	"github.com/oakwood-commons/scafctl/pkg/action"
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
	// SymbolAction is an action defined under spec.workflow.actions or finally.
	SymbolAction
)

// String returns the lowercase kind name.
func (k SymbolKind) String() string {
	switch k {
	case SymbolResolver:
		return "resolver"
	case SymbolAction:
		return "action"
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

// symbolKey identifies a symbol by kind and name; resolvers and actions may
// share a name, so references are indexed per (kind, name).
type symbolKey struct {
	kind SymbolKind
	name string
}

// Index is a queryable set of positioned references over one solution.
type Index struct {
	file  string
	refs  []Reference
	byKey map[symbolKey][]Reference
	// unresolvedByKey counts references that were identified semantically but
	// could not be located byte-exact, attributed to the (kind, name) they
	// target. This makes the fail-safe symbol-scoped: a rename of X is blocked
	// only by unlocatable references to X, not by unrelated ones.
	unresolvedByKey map[symbolKey]int
	// unresolvedOther counts unlocatable references whose target could not be
	// determined (e.g. an expression/template that failed to parse). These
	// conservatively block every rename, since they might reference any symbol.
	unresolvedOther int
}

// All returns every reference (definitions and uses), sorted by position.
func (i *Index) All() []Reference {
	out := make([]Reference, len(i.refs))
	copy(out, i.refs)
	return out
}

// Occurrences returns every occurrence of the (kind, name) symbol (definition
// and uses), sorted by position. This is the set a rename must rewrite.
func (i *Index) Occurrences(kind SymbolKind, name string) []Reference {
	src := i.byKey[symbolKey{kind, name}]
	out := make([]Reference, len(src))
	copy(out, src)
	return out
}

// References returns the non-definition uses of the (kind, name) symbol.
func (i *Index) References(kind SymbolKind, name string) []Reference {
	var out []Reference
	for _, r := range i.byKey[symbolKey{kind, name}] {
		if !r.IsDef {
			out = append(out, r)
		}
	}
	return out
}

// Definition returns the defining occurrence of the (kind, name) symbol, if one
// exists.
func (i *Index) Definition(kind SymbolKind, name string) (Reference, bool) {
	for _, r := range i.byKey[symbolKey{kind, name}] {
		if r.IsDef {
			return r, true
		}
	}
	return Reference{}, false
}

// Names returns the sorted set of symbol names of the given kind.
func (i *Index) Names(kind SymbolKind) []string {
	var out []string
	for k := range i.byKey {
		if k.kind == kind {
			out = append(out, k.name)
		}
	}
	sort.Strings(out)
	return out
}

// Unresolved returns the total number of references that were identified
// semantically but could not be located byte-exact in the source. A non-zero
// value means the index is not a complete picture of every reference.
func (i *Index) Unresolved() int {
	total := i.unresolvedOther
	for _, n := range i.unresolvedByKey {
		total += n
	}
	return total
}

// UnresolvedFor returns the number of unlocatable references that could affect a
// rename of the (kind, name) symbol: those attributed to it plus any whose
// target could not be determined. A rename must refuse when this is non-zero, or
// it risks a partial (solution-breaking) rewrite.
func (i *Index) UnresolvedFor(kind SymbolKind, name string) int {
	return i.unresolvedByKey[symbolKey{kind, name}] + i.unresolvedOther
}

// Build constructs a positioned reference index for the solution. It returns an
// empty (non-nil) index when sol is nil.
func Build(sol *solution.Solution) (*Index, error) {
	idx := &Index{byKey: map[symbolKey][]Reference{}, unresolvedByKey: map[symbolKey]int{}}
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
			b.addDefinition(path, name, SymbolResolver)
			for i, dep := range r.DependsOn {
				b.addWholeScalarRef(fmt.Sprintf("%s.dependsOn[%d]", path, i), dep, SymbolResolver, OriginDependsOn)
			}
			return nil
		},
		Action: func(path, name, _ string, act *action.Action) error {
			b.addDefinition(path, name, SymbolAction)
			for i, dep := range act.DependsOn {
				b.addWholeScalarRef(fmt.Sprintf("%s.dependsOn[%d]", path, i), dep, SymbolAction, OriginDependsOn)
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
				b.addWholeScalarRef(path+".rslvr", *vr.Resolver, SymbolResolver, OriginResolverRef)
			case vr.Expr != nil:
				b.addCELRefs(path, vr.Expr)
			case vr.Tmpl != nil:
				b.addTemplateRefs(path+".tmpl", vr.Tmpl)
			case vr.Literal != nil:
				// walk.Walk does not descend into literal maps/arrays, but scafctl
				// resolves inline ValueRefs ({rslvr}/{expr}/{tmpl}) nested inside
				// them. Those references are not positioned here, so record their
				// target names as unresolved to keep rename fail-safe.
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

// markUnresolved records that a reference to the (kind, name) symbol could not
// be located.
func (b *builder) markUnresolved(kind SymbolKind, name string) {
	b.idx.unresolvedByKey[symbolKey{kind, name}]++
}

// markUnresolvedUnknown records an unlocatable reference whose target could not
// be determined (e.g. a parse failure). It conservatively blocks every rename.
func (b *builder) markUnresolvedUnknown() { b.idx.unresolvedOther++ }

// addDefinition records the defining site of a symbol, located at its map key.
func (b *builder) addDefinition(path, name string, kind SymbolKind) {
	pos, ok := b.sm.Get(path)
	if !ok {
		b.markUnresolved(kind, name)
		return
	}
	start := b.li.Offset(pos)
	b.emitAt(start, name, kind, OriginDefinition, true)
}

// addWholeScalarRef records a reference whose entire scalar value is the name
// (dependsOn entries, rslvr values).
func (b *builder) addWholeScalarRef(path, name string, kind SymbolKind, origin Origin) {
	node, ok := b.scalarAt(path)
	if !ok {
		b.markUnresolved(kind, name)
		return
	}
	start, ok := b.contentStart(node)
	if !ok {
		b.markUnresolved(kind, name)
		return
	}
	b.emitAt(start, name, kind, origin, false)
}

// celPrefixes are the CEL namespaces this index tracks, each mapped to the
// symbol kind its references target: "_." for resolver data, "__actions." for
// action results.
var celPrefixes = []struct {
	prefix string
	kind   SymbolKind
}{
	{"_.", SymbolResolver},
	{celexp.VarActions + ".", SymbolAction},
}

// addCELRefs records every namespaced reference inside a CEL expression, for
// each tracked prefix. basePath is the ValueRef or condition path; the scalar is
// at basePath.expr (object form) or basePath itself (condition scalar shorthand).
//
// It reconciles positioned references against the authoritative variable set for
// each prefix: any name the parser reports that a positioned ref did not cover
// is recorded as unresolved, so a rename fails safe.
func (b *builder) addCELRefs(basePath string, expr *celexp.Expression) {
	if expr == nil {
		return
	}
	node := b.celScalar(basePath)
	start := -1
	if node != nil {
		if s, ok := b.contentStart(node); ok {
			start = s
		}
	}

	for _, p := range celPrefixes {
		if start < 0 {
			b.markCELNamesUnresolved(expr, p.prefix, p.kind)
			continue
		}
		b.addCELPrefixRefs(start, expr, p.prefix, p.kind)
	}
}

// markCELNamesUnresolved records all of an expression's names under prefix as
// unresolved (used when there is no scalar to position against).
func (b *builder) markCELNamesUnresolved(expr *celexp.Expression, prefix string, kind SymbolKind) {
	names, err := expr.GetVariablesWithPrefix(context.Background(), prefix)
	if err != nil {
		b.markUnresolvedUnknown()
		return
	}
	for _, name := range names {
		b.markUnresolved(kind, name)
	}
}

// addCELPrefixRefs positions and reconciles the references of one prefix.
func (b *builder) addCELPrefixRefs(start int, expr *celexp.Expression, prefix string, kind SymbolKind) {
	refs, err := expr.PrefixedVariableRefs(context.Background(), prefix)
	if err != nil {
		b.markUnresolvedUnknown()
		return
	}
	attempted := make(map[string]bool, len(refs))
	for _, r := range refs {
		attempted[r.Name] = true
		if r.Offset < 0 {
			b.markUnresolved(kind, r.Name)
			continue
		}
		b.emitAt(start+r.Offset, r.Name, kind, OriginCEL, false)
	}
	// Cross-check against the authoritative name set for this prefix.
	if auth, err := expr.GetVariablesWithPrefix(context.Background(), prefix); err == nil {
		for _, name := range auth {
			if !attempted[name] {
				b.markUnresolved(kind, name)
			}
		}
	}
}

// addTemplateRefs records both explicit resolver references ({{ ._.name }}) and
// action references ({{ .__actions.name }}) inside a Go template, reconciling
// each against its authoritative unscoped set so unpositionable references are
// recorded as unresolved (fail-safe).
func (b *builder) addTemplateRefs(path string, tmpl *gotmpl.GoTemplatingContent) {
	if tmpl == nil {
		return
	}
	tmplStr := string(*tmpl)
	node, nodeOk := b.scalarAt(path)
	start := -1
	if nodeOk {
		if s, ok := b.contentStart(node); ok {
			start = s
		}
	}
	b.addTemplateResolverRefs(tmplStr, start)
	b.addTemplateActionRefs(tmplStr, start)
}

// addTemplateResolverRefs handles the resolver forms ({{ ._.name }}, and
// context-dependent bare/$-rooted fields which are conservatively marked).
func (b *builder) addTemplateResolverRefs(tmplStr string, start int) {
	candidates, cerr := gotmpl.UnscopedResolverRefs(tmplStr, "", "")
	if cerr != nil {
		b.markUnresolvedUnknown()
		return
	}
	if start < 0 {
		for _, c := range candidates {
			b.markUnresolved(SymbolResolver, c.Name)
		}
		return
	}
	refs, err := gotmpl.GetGoTemplatePositionedReferences(tmplStr, "", "")
	if err != nil {
		b.markUnresolvedUnknown()
		return
	}
	positionedExplicit := make(map[string]bool)
	for _, r := range refs {
		if r.Scoped || r.Kind != gotmpl.RefKindExplicitResolver {
			continue
		}
		if b.emitAt(start+r.Offset, r.Name, SymbolResolver, OriginTemplate, false) {
			positionedExplicit[r.Name] = true
		}
	}
	for _, c := range candidates {
		if c.Explicit {
			if !positionedExplicit[c.Name] {
				b.markUnresolved(SymbolResolver, c.Name)
			}
			continue
		}
		b.markUnresolved(SymbolResolver, c.Name)
	}
}

// addTemplateActionRefs handles the action form ({{ .__actions.name }}).
func (b *builder) addTemplateActionRefs(tmplStr string, start int) {
	names, err := gotmpl.UnscopedActionRefs(tmplStr, "", "")
	if err != nil {
		b.markUnresolvedUnknown()
		return
	}
	if len(names) == 0 {
		return
	}
	if start < 0 {
		for _, n := range names {
			b.markUnresolved(SymbolAction, n)
		}
		return
	}
	refs, err := gotmpl.GetGoTemplatePositionedActionReferences(tmplStr, "", "")
	if err != nil {
		b.markUnresolvedUnknown()
		return
	}
	positioned := make(map[string]bool)
	for _, r := range refs {
		if r.Scoped {
			continue
		}
		if b.emitAt(start+r.Offset, r.Name, SymbolAction, OriginTemplate, false) {
			positioned[r.Name] = true
		}
	}
	for _, n := range names {
		if !positioned[n] {
			b.markUnresolved(SymbolAction, n)
		}
	}
}

// emitAt validates that the bytes at [start, start+len(name)) equal name and, if
// so, records the reference and returns true. A mismatch (or out-of-range span)
// is attributed to the (kind, name) symbol as unresolved and returns false.
func (b *builder) emitAt(start int, name string, kind SymbolKind, origin Origin, isDef bool) bool {
	end := start + len(name)
	if start < 0 || end > len(b.raw) || string(b.raw[start:end]) != name {
		b.markUnresolved(kind, name)
		return false
	}
	b.idx.record(Reference{
		Symbol: Symbol{Kind: kind, Name: name},
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

// markLiteralValueRef records the symbol names referenced by a single nested
// inline ValueRef as unresolved (both resolver and action namespaces).
func (b *builder) markLiteralValueRef(payload, kind string) {
	switch kind {
	case "rslvr":
		b.markUnresolved(SymbolResolver, payload)
	case "expr":
		expr := celexp.Expression(payload)
		for _, p := range celPrefixes {
			names, err := expr.GetVariablesWithPrefix(context.Background(), p.prefix)
			if err != nil {
				b.markUnresolvedUnknown()
				return
			}
			for _, n := range names {
				b.markUnresolved(p.kind, n)
			}
		}
	case "tmpl":
		if cands, err := gotmpl.UnscopedResolverRefs(payload, "", ""); err == nil {
			for _, c := range cands {
				b.markUnresolved(SymbolResolver, c.Name)
			}
		} else {
			b.markUnresolvedUnknown()
			return
		}
		if names, err := gotmpl.UnscopedActionRefs(payload, "", ""); err == nil {
			for _, n := range names {
				b.markUnresolved(SymbolAction, n)
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
	for key := range i.byKey {
		refs := i.byKey[key]
		sort.SliceStable(refs, func(a, c int) bool { return lessRange(refs[a].Range, refs[c].Range) })
		i.byKey[key] = refs
	}
}

func (i *Index) record(r Reference) {
	key := symbolKey{r.Symbol.Kind, r.Symbol.Name}
	i.refs = append(i.refs, r)
	i.byKey[key] = append(i.byKey[key], r)
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
