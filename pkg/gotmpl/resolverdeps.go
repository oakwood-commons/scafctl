// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import "strings"

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
	// DataKeys is the set of top-level keys the data input provides. Only
	// meaningful when DataKeysComplete is true.
	DataKeys map[string]bool
	// DataKeysComplete indicates DataKeys is an authoritative, complete key set.
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

	// DataKeys is the set of top-level keys the `data` input provides. It is
	// only meaningful when DataKeysComplete is true.
	DataKeys map[string]bool

	// DataKeysComplete indicates DataKeys is an authoritative, complete list of
	// the keys the data input provides. It is false when the data input is
	// dynamic (e.g. a rslvr reference or a non-map-literal expression) and the
	// key set cannot be determined statically.
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
	refs, err := GetGoTemplateReferences(in.Template, in.LeftDelim, in.RightDelim)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var deps []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			deps = append(deps, name)
		}
	}

	for _, ref := range refs {
		// Scoped references point at fields of a {{ with }}/{{ range }} narrowed
		// context, not the root data, so they are never resolver dependencies.
		if ref.Scoped {
			continue
		}

		name, kind := classifyTemplatePath(ref.Path)
		switch kind {
		case pathSpecial:
			continue
		case pathExplicitResolver:
			add(name)
		case pathField:
			if in.Aliases[name] {
				continue
			}
			if !in.HasDataInput {
				// No data input: the template root is resolver data.
				add(name)
				continue
			}
			if !in.DataKeysComplete {
				// Data input present but its keys are not statically known:
				// treat the whole template root as data context.
				continue
			}
			if in.DataKeys[name] {
				continue
			}
			// A field not provided by the known data keys still resolves against
			// resolver data (buildTemplateData copies resolver data first).
			add(name)
		}
	}

	return deps
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
