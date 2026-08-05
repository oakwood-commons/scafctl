// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"sync"

	celexpext "github.com/oakwood-commons/scafctl/pkg/celexp/ext"
	gotmplext "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext"
)

// FuncInfo is a uniform view of a CEL or Go-template function's metadata, shared
// by hover (#772), function completion (#775), and signature help (#777) so all
// three surface the same information from one source of truth.
type FuncInfo struct {
	// Name is the function name as written in an expression/template.
	Name string
	// Signature is the type signature (CEL functions only; empty for templates).
	Signature string
	// Description is the human-readable description.
	Description string
	// Examples are usage snippets (CEL expressions or template snippets).
	Examples []string
	// CEL reports whether the function is a CEL function (true) or a Go-template
	// function (false).
	CEL bool
}

// funcIndex is the lazily-built, immutable lookup over both function registries.
type funcIndex struct {
	byName map[string]FuncInfo // keyed by every invocable name
	all    []FuncInfo          // stable, de-duplicated order (CEL first)
}

var (
	funcIndexOnce sync.Once
	funcIndexVal  *funcIndex
)

// getFuncIndex builds the function index once. The registries are static for the
// life of the process, so a single build is safe and cheap on the hot path.
func getFuncIndex() *funcIndex {
	funcIndexOnce.Do(func() {
		idx := &funcIndex{byName: make(map[string]FuncInfo)}

		// CEL functions first, so a name present in both registries resolves to
		// its CEL definition (CEL is the primary expression language).
		for _, f := range celexpext.All() {
			info := FuncInfo{
				Name:        f.Name,
				Signature:   f.Signature,
				Description: f.Description,
				CEL:         true,
			}
			for _, ex := range f.Examples {
				if ex.Expression != "" {
					info.Examples = append(info.Examples, ex.Expression)
				}
			}
			idx.add(info, f.GetSubNames())
		}

		for _, f := range gotmplext.All() {
			info := FuncInfo{
				Name:        f.Name,
				Description: f.Description,
				CEL:         false,
			}
			for _, ex := range f.Examples {
				if ex.Template != "" {
					info.Examples = append(info.Examples, ex.Template)
				}
			}
			idx.add(info, nil)
		}

		funcIndexVal = idx
	})
	return funcIndexVal
}

// add records info under its Name and any additional invocable sub-names,
// without overwriting an already-registered name (CEL registered first wins).
// Only the primary entry is appended to `all`; sub-names are lookup-only (they
// resolve via LookupFunc but are not enumerated by AllFuncs), so callers that
// list functions see one entry per function rather than every alias form.
func (i *funcIndex) add(info FuncInfo, subNames []string) {
	if info.Name == "" {
		return
	}
	if _, exists := i.byName[info.Name]; !exists {
		i.byName[info.Name] = info
		i.all = append(i.all, info)
	}
	for _, sub := range subNames {
		if sub == "" || sub == info.Name {
			continue
		}
		if _, exists := i.byName[sub]; !exists {
			// Sub-names (e.g. base64.encode within an "encoders" group) resolve
			// to the group's metadata but report their own invocable name.
			entry := info
			entry.Name = sub
			i.byName[sub] = entry
		}
	}
}

// LookupFunc returns the metadata for the function named name, searching CEL
// functions first, then Go-template functions. ok is false when no function of
// that name exists in either registry. The returned FuncInfo's Examples slice is
// a copy, so callers may retain or mutate it without corrupting the shared,
// effectively-immutable function index.
func LookupFunc(name string) (FuncInfo, bool) {
	info, ok := getFuncIndex().byName[name]
	if !ok {
		return FuncInfo{}, false
	}
	info.Examples = cloneExamples(info.Examples)
	return info, true
}

// AllFuncs returns every known function's metadata (CEL first, then template),
// de-duplicated by primary name. The returned slice -- including each entry's
// Examples slice -- is a copy the caller may retain or mutate without affecting
// the shared function index.
func AllFuncs() []FuncInfo {
	src := getFuncIndex().all
	out := make([]FuncInfo, len(src))
	copy(out, src)
	for i := range out {
		out[i].Examples = cloneExamples(out[i].Examples)
	}
	return out
}

// cloneExamples returns a copy of the examples slice, or nil when empty, so
// callers cannot mutate the function index's shared backing storage. Element
// strings are immutable, so a shallow copy of the slice suffices.
func cloneExamples(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}
