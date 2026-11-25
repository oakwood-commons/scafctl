// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"strings"
	"text/template/parse"
)

// DepScanContextKey is the reserved input-map key under which resolver dependency
// extraction context is passed to a template-based provider's ExtractDependencies
// function. A provider that resolves inline templates should read this key,
// falling back to best-effort local computation when it is absent.
const DepScanContextKey = "__scafctl_dep_scan_context"

// DepScanContext carries the pre-computed template scan context (data-input key
// set and forEach aliases) from the dependency-graph builder into a provider's
// ExtractDependencies function. It exists because that function receives only the
// provider input map and cannot otherwise see sibling constructs such as a
// forEach clause or statically analyse CEL data expressions.
type DepScanContext struct {
	// HasDataInput indicates the step supplies an explicit data input.
	HasDataInput bool
	// DataKeys is the set of top-level keys guaranteed to be present in the
	// data input. Guaranteed keys are excluded from bare {{ .field }} resolver
	// dependencies even when DataKeysComplete is false (for example when static
	// shared data is merged with dynamic per-entry data).
	DataKeys map[string]bool
	// DataKeysComplete indicates DataKeys is an authoritative, complete key set
	// with no additional dynamic keys unknown at scan time.
	DataKeysComplete bool
	// Aliases is the set of forEach item/index alias names bound locally.
	Aliases map[string]bool
}

// ResolverDepsInput configures resolver-dependency extraction from a Go template
// string. It captures everything the extractor needs to correctly disambiguate a
// {{ .field }} accessor between a resolver reference and a local template-context
// binding (a data-input key or a forEach loop alias).
type ResolverDepsInput struct {
	// Template is the raw Go template text to scan. Callers should strip any
	// ignored/raw pass-through regions before passing the text.
	Template string

	// LeftDelim and RightDelim are the template delimiters. Empty values fall
	// back to the standard "{{" and "}}".
	LeftDelim  string
	RightDelim string

	// HasDataInput indicates the step supplies an explicit `data` input. When
	// true, the template's root namespace is dominated by the data context, so
	// bare {{ .field }} accessors are resolved against the data rather than the
	// resolver graph.
	HasDataInput bool

	// DataKeys is the set of top-level keys guaranteed to be present in the
	// `data` input. Guaranteed keys are excluded from bare {{ .field }}
	// resolver dependencies and fallback candidates regardless of whether
	// DataKeysComplete is true.
	DataKeys map[string]bool

	// DataKeysComplete indicates DataKeys is an authoritative, complete list of
	// the keys the data input provides. It is false when any merged data source
	// is dynamic (e.g. a rslvr reference or a non-map-literal expression) and
	// additional keys cannot be determined statically.
	DataKeysComplete bool

	// Aliases is the set of forEach item/index alias names bound locally for the
	// step. They are never resolver dependencies.
	Aliases map[string]bool
}

// pathKind classifies a template reference path.
type pathKind int

const (
	// pathSpecial is a special/internal variable (e.g. .__self, .__item) that is
	// never a resolver dependency.
	pathSpecial pathKind = iota
	// pathExplicitResolver is an explicit resolver-context reference
	// ({{ ._.name }}) that is always a resolver dependency.
	pathExplicitResolver
	// pathField is a bare data-context accessor ({{ .field }}) that may or may
	// not be a resolver dependency depending on the surrounding context.
	pathField
)

// ExtractResolverDeps returns the resolver dependency names referenced by
// root-level accessors in a Go template, applying context-aware disambiguation:
//
//   - {{ ._.name }} is always treated as a resolver dependency.
//   - {{ .field }} is treated as a resolver dependency only when there is no
//     data input and the field is not a forEach alias. When a data input is
//     present, a field matching a statically-known data key (or any field when
//     the data key set is not statically known) is treated as local context.
//   - Special variables ({{ .__item }}, {{ .__self }}, ...) and scoped
//     references (inside {{ with }}/{{ range }} bodies) are ignored.
//
// The returned names are de-duplicated and preserve first-seen order. Template
// parse errors yield a nil slice (extraction is best-effort).
func ExtractResolverDeps(in ResolverDepsInput) []string {
	deps, _, _ := scanTemplateResolverDeps(in)
	return deps
}

// ExtractResolverDepsWithCandidates partitions root-level template references
// into unambiguous dependencies (explicit {{ ._.name }} references plus bare
// {{ .field }} accessors not shadowed by guaranteed DataKeys or loop aliases
// when HasDataInput is false or DataKeysComplete is true) and fallback
// candidates (bare {{ .field }} accessors not in DataKeys or Aliases when
// HasDataInput is true and DataKeysComplete is false, which resolve against the
// base resolver context whenever the dynamic data map omits that key). Names
// present in hardDeps are never repeated in candidateDeps regardless of
// reference order.
func ExtractResolverDepsWithCandidates(in ResolverDepsInput) (hardDeps, candidateDeps []string) {
	hardDeps, candidateDeps, _ = scanTemplateResolverDeps(in)
	return hardDeps, candidateDeps
}

// UnscopedResolverRef is a root-level, unscoped reference candidate in a Go
// template that could denote a resolver, with whether it was written using
// explicit resolver syntax.
type UnscopedResolverRef struct {
	// Name is the root reference name (with any resolver prefix removed).
	Name string
	// Explicit reports that the reference used explicit resolver syntax
	// ({{ ._.name }} / {{ ._name }}) as opposed to a bare or $-rooted accessor.
	Explicit bool
}

// UnscopedResolverRefs returns every root-level, unscoped reference in a
// template that could denote a resolver -- explicit resolver refs
// ({{ ._.name }} / {{ ._name }}), bare data-context accessors ({{ .name }}),
// and $-rooted accessors ({{ $.name }}) -- each flagged with whether it used
// explicit resolver syntax. Special variables ({{ .__x }}) and scoped
// references (inside {{ with }}/{{ range }} bodies) are excluded.
//
// This is the authoritative set a positioned-reference consumer must reconcile
// against: any name returned here that a positioned walker cannot locate is a
// reference that must be treated as unresolved (so a rename fails safe rather
// than silently missing it). Results are de-duplicated per (name, explicit)
// pair, preserving first-seen order. Parse errors yield a nil slice and error.
func UnscopedResolverRefs(template, leftDelim, rightDelim string, declaredFuncs []string) ([]UnscopedResolverRef, error) {
	refs, err := getGoTemplateReferencesWithFuncs(template, leftDelim, rightDelim, declaredFuncs)
	if err != nil {
		return nil, err
	}

	seen := make(map[UnscopedResolverRef]bool)
	var out []UnscopedResolverRef
	for _, ref := range refs {
		if ref.Scoped {
			continue
		}
		name, kind := classifyTemplatePath(ref.Path)
		if name == "" || kind == pathSpecial {
			continue
		}
		u := UnscopedResolverRef{Name: name, Explicit: kind == pathExplicitResolver}
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out, nil
}

// ExtractExplicitResolverRefs returns only the resolver names referenced with
// EXPLICIT resolver syntax ({{ ._.name }} / {{ ._name }}) in a Go template.
//
// Unlike ExtractResolverDeps, bare data-context accessors ({{ .field }}) are
// never returned, because a bare accessor may legitimately resolve against a
// step's `data` keys or a forEach alias rather than a resolver. Callers that
// must know a reference definitely names a resolver (e.g. validating that the
// referenced resolver exists) need this stricter view.
//
// The returned names are de-duplicated and preserve first-seen order. Template
// parse errors yield a nil slice (extraction is best-effort).
func ExtractExplicitResolverRefs(template, leftDelim, rightDelim string) []string {
	refs, err := GetGoTemplateReferences(template, leftDelim, rightDelim)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var out []string
	for _, ref := range refs {
		if ref.Scoped {
			continue
		}
		name, kind := classifyTemplatePath(ref.Path)
		if kind != pathExplicitResolver || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// classifyTemplatePath maps a dot-notation template reference path to the
// dependency name of its root segment and a classification. The recognised
// prefixes are:
//
//	"._.name"  -> explicit resolver reference (ValueRef tmpl form)
//	".__x"     -> special/internal variable (skipped)
//	"._name"   -> explicit resolver reference (_.name form)
//	".name"    -> bare data-context accessor
func classifyTemplatePath(path string) (string, pathKind) {
	switch {
	case strings.HasPrefix(path, "._."):
		return firstSegment(strings.TrimPrefix(path, "._.")), pathExplicitResolver
	case strings.HasPrefix(path, ".__"):
		return "", pathSpecial
	case strings.HasPrefix(path, "._"):
		return firstSegment(strings.TrimPrefix(path, "._")), pathExplicitResolver
	case strings.HasPrefix(path, "."):
		return firstSegment(strings.TrimPrefix(path, ".")), pathField
	default:
		return "", pathSpecial
	}
}

// firstSegment returns the portion of a dotted path before the first ".",
// e.g. "config.host" -> "config".
func firstSegment(path string) string {
	if idx := strings.Index(path, "."); idx >= 0 {
		return path[:idx]
	}
	return path
}

// actionNamespace is the reserved template root identifier under which prior
// action results are exposed, e.g. {{ .__actions.deploy.results }}.
const actionNamespace = "__actions"

// actionRefPathPrefix is the template path prefix for an action reference,
// e.g. ".__actions.deploy.results" references action "deploy".
const actionRefPathPrefix = "." + actionNamespace + "."

// UnscopedActionRefs returns the action names referenced via {{ .__actions.name }}
// (unscoped) in a Go template, de-duplicated in first-seen order. It is the
// authoritative set a positioned action-reference consumer reconciles against so
// a rename fails safe. Parse errors yield a nil slice and error.
func UnscopedActionRefs(template, leftDelim, rightDelim string, declaredFuncs []string) ([]string, error) {
	refs, err := getGoTemplateReferencesWithFuncs(template, leftDelim, rightDelim, declaredFuncs)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var out []string
	for _, ref := range refs {
		if ref.Scoped || !strings.HasPrefix(ref.Path, actionRefPathPrefix) {
			continue
		}
		name := firstSegment(strings.TrimPrefix(ref.Path, actionRefPathPrefix))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// ReferencesWholeResolverNamespace reports whether in.Template accesses the
// root resolver map as a whole (for example via {{ ._ }}, {{ printf "%v" ._ }},
// {{ $._ }}, unscoped {{ . }}, or bare {{ $ }}) across reachable template
// invocations such that individual resolver names cannot be enumerated
// statically. Parse errors return false.
func ReferencesWholeResolverNamespace(in ResolverDepsInput) bool {
	_, _, whole := scanTemplateResolverDeps(in)
	return whole
}

type tmplScopeKind int

const (
	tmplScopeRoot tmplScopeKind = iota
	tmplScopeResolverMap
	tmplScopeNarrowed
)

type tmplVisitKey struct {
	name   string
	dot    tmplScopeKind
	dollar tmplScopeKind
}

const rootTemplateScanName = "resolver-deps-scan"

func scanTemplateResolverDeps(in ResolverDepsInput) ([]string, []string, bool) {
	if in.Template == "" {
		return nil, nil, false
	}
	leftDelim := in.LeftDelim
	if leftDelim == "" {
		leftDelim = DefaultLeftDelim
	}
	rightDelim := in.RightDelim
	if rightDelim == "" {
		rightDelim = DefaultRightDelim
	}

	svc := NewService(nil)
	trees, err := parseTemplatesTolerant(rootTemplateScanName, in.Template, leftDelim, rightDelim, svc.templateFuncNamesWith(nil))
	if err != nil {
		return nil, nil, false
	}
	mainTree := trees[rootTemplateScanName]
	if mainTree == nil || mainTree.Root == nil {
		return nil, nil, false
	}

	sc := &scopedTemplateScanner{
		in:       in,
		trees:    trees,
		seen:     make(map[string]bool),
		seenSoft: make(map[string]bool),
		active:   make(map[tmplVisitKey]bool),
	}
	sc.walkNode(mainTree.Root, tmplScopeRoot, tmplScopeRoot)
	var softDeps []string
	for _, name := range sc.softDeps {
		if !sc.seen[name] {
			softDeps = append(softDeps, name)
		}
	}
	return sc.deps, softDeps, sc.whole
}

type scopedTemplateScanner struct {
	in       ResolverDepsInput
	trees    map[string]*parse.Tree
	seen     map[string]bool
	seenSoft map[string]bool
	deps     []string
	softDeps []string
	whole    bool
	active   map[tmplVisitKey]bool
}

func (s *scopedTemplateScanner) addDep(name string) {
	if name != "" && !s.seen[name] {
		s.seen[name] = true
		s.deps = append(s.deps, name)
	}
}

func (s *scopedTemplateScanner) addSoftDep(name string) {
	if name != "" && !s.seen[name] && !s.seenSoft[name] {
		s.seenSoft[name] = true
		s.softDeps = append(s.softDeps, name)
	}
}

func (s *scopedTemplateScanner) walkNode(node parse.Node, dotScope, dollarScope tmplScopeKind) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, child := range n.Nodes {
			s.walkNode(child, dotScope, dollarScope)
		}
	case *parse.ActionNode:
		s.walkPipe(n.Pipe, dotScope, dollarScope)
	case *parse.IfNode:
		s.walkPipe(n.Pipe, dotScope, dollarScope)
		s.walkNode(n.List, dotScope, dollarScope)
		s.walkNode(n.ElseList, dotScope, dollarScope)
	case *parse.RangeNode:
		s.walkPipe(n.Pipe, dotScope, dollarScope)
		s.walkNode(n.List, tmplScopeNarrowed, dollarScope)
		s.walkNode(n.ElseList, dotScope, dollarScope)
	case *parse.WithNode:
		s.walkPipe(n.Pipe, dotScope, dollarScope)
		s.walkNode(n.List, tmplScopeNarrowed, dollarScope)
		s.walkNode(n.ElseList, dotScope, dollarScope)
	case *parse.TemplateNode:
		s.walkTemplateInvocation(n, dotScope, dollarScope)
	}
}

func (s *scopedTemplateScanner) walkTemplateInvocation(n *parse.TemplateNode, dotScope, dollarScope tmplScopeKind) {
	subTree := s.trees[n.Name]
	if subTree == nil || subTree.Root == nil {
		// External/undefined template: inspect the invocation argument normally.
		s.walkPipe(n.Pipe, dotScope, dollarScope)
		return
	}

	subScope := tmplScopeNarrowed
	if n.Pipe != nil {
		if bound, ok := singlePipeArgScope(n.Pipe, dotScope, dollarScope); ok {
			subScope = bound
		} else {
			s.walkPipe(n.Pipe, dotScope, dollarScope)
		}
	}

	key := tmplVisitKey{name: n.Name, dot: subScope, dollar: subScope}
	if s.active[key] {
		return
	}
	s.active[key] = true
	s.walkNode(subTree.Root, subScope, subScope)
	delete(s.active, key)
}

func singlePipeArgScope(pipe *parse.PipeNode, dotScope, dollarScope tmplScopeKind) (tmplScopeKind, bool) {
	if pipe == nil || len(pipe.Decl) > 0 || len(pipe.Cmds) != 1 || pipe.Cmds[0] == nil || len(pipe.Cmds[0].Args) != 1 {
		return tmplScopeNarrowed, false
	}
	arg := unwrapSinglePipeArg(pipe.Cmds[0].Args[0])
	switch n := arg.(type) {
	case *parse.DotNode:
		if dotScope == tmplScopeRoot || dotScope == tmplScopeResolverMap {
			return dotScope, true
		}
	case *parse.FieldNode:
		if len(n.Ident) == 1 && n.Ident[0] == "_" && dotScope == tmplScopeRoot {
			return tmplScopeResolverMap, true
		}
	case *parse.VariableNode:
		if len(n.Ident) == 1 && n.Ident[0] == "$" && (dollarScope == tmplScopeRoot || dollarScope == tmplScopeResolverMap) {
			return dollarScope, true
		}
		if len(n.Ident) == 2 && n.Ident[0] == "$" && n.Ident[1] == "_" && dollarScope == tmplScopeRoot {
			return tmplScopeResolverMap, true
		}
	case *parse.ChainNode:
		base := unwrapSinglePipeArg(n.Node)
		if len(n.Field) == 1 && n.Field[0] == "_" {
			if _, isDot := base.(*parse.DotNode); isDot && dotScope == tmplScopeRoot {
				return tmplScopeResolverMap, true
			}
			if vn, isVar := base.(*parse.VariableNode); isVar && len(vn.Ident) == 1 && vn.Ident[0] == "$" && dollarScope == tmplScopeRoot {
				return tmplScopeResolverMap, true
			}
		}
	}
	return tmplScopeNarrowed, false
}

func (s *scopedTemplateScanner) walkPipe(pipe *parse.PipeNode, dotScope, dollarScope tmplScopeKind) {
	if pipe == nil {
		return
	}
	for _, cmd := range pipe.Cmds {
		if cmd == nil {
			continue
		}
		for _, arg := range cmd.Args {
			s.walkArg(arg, dotScope, dollarScope)
		}
	}
}

func (s *scopedTemplateScanner) walkArg(arg parse.Node, dotScope, dollarScope tmplScopeKind) {
	switch n := arg.(type) {
	case *parse.DotNode:
		if dotScope == tmplScopeRoot || dotScope == tmplScopeResolverMap {
			s.whole = true
		}
	case *parse.FieldNode:
		s.recordPath(buildFieldPath(n.Ident), dotScope)
	case *parse.VariableNode:
		if len(n.Ident) >= 1 && n.Ident[0] == "$" {
			if len(n.Ident) == 1 {
				if dollarScope == tmplScopeRoot || dollarScope == tmplScopeResolverMap {
					s.whole = true
				}
				return
			}
			s.recordPath("."+strings.Join(n.Ident[1:], "."), dollarScope)
		}
	case *parse.ChainNode:
		if len(n.Field) == 0 {
			s.walkArg(n.Node, dotScope, dollarScope)
			return
		}
		base := unwrapSinglePipeArg(n.Node)
		switch b := base.(type) {
		case *parse.VariableNode:
			if len(b.Ident) >= 1 && b.Ident[0] == "$" {
				parts := make([]string, 0, len(b.Ident)-1+len(n.Field))
				parts = append(parts, b.Ident[1:]...)
				parts = append(parts, n.Field...)
				s.recordPath("."+strings.Join(parts, "."), dollarScope)
			}
			return
		case *parse.FieldNode:
			parts := make([]string, 0, len(b.Ident)+len(n.Field))
			parts = append(parts, b.Ident...)
			parts = append(parts, n.Field...)
			s.recordPath("."+strings.Join(parts, "."), dotScope)
			return
		case *parse.DotNode:
			s.recordPath("."+strings.Join(n.Field, "."), dotScope)
			return
		default:
			s.walkArg(n.Node, dotScope, dollarScope)
		}
	case *parse.PipeNode:
		s.walkPipe(n, dotScope, dollarScope)
	}
}

func (s *scopedTemplateScanner) recordPath(path string, scope tmplScopeKind) {
	switch scope {
	case tmplScopeNarrowed:
		return
	case tmplScopeResolverMap:
		if path == "." || path == "" {
			s.whole = true
			return
		}
		seg := firstSegment(strings.TrimPrefix(path, "."))
		if strings.HasPrefix(seg, "__") {
			return
		}
		s.addDep(seg)
	case tmplScopeRoot:
		if path == "._" || path == "." {
			s.whole = true
			return
		}
		name, kind := classifyTemplatePath(path)
		switch kind {
		case pathSpecial:
			return
		case pathExplicitResolver:
			s.addDep(name)
		case pathField:
			if s.in.Aliases[name] {
				return
			}
			if !s.in.HasDataInput {
				s.addDep(name)
				return
			}
			if s.in.DataKeys[name] {
				return
			}
			if !s.in.DataKeysComplete {
				s.addSoftDep(name)
				return
			}
			s.addDep(name)
		}
	}
}

func unwrapSinglePipeArg(node parse.Node) parse.Node {
	for {
		p, ok := node.(*parse.PipeNode)
		if !ok || p == nil || len(p.Decl) > 0 || len(p.Cmds) != 1 || p.Cmds[0] == nil || len(p.Cmds[0].Args) != 1 {
			return node
		}
		node = p.Cmds[0].Args[0]
	}
}
