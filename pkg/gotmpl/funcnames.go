// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"fmt"
	"sort"
	"text/template/parse"
)

// BuiltinFuncNames returns the sorted set of function names that are always
// available to a Go template rendered through the gotmpl service: the
// text/template builtins (and, index, printf, ...) plus every extension
// function (sprig + custom + embedder-registered), honoring the current
// env-function stripping policy.
//
// It is the authoritative name set for detecting collisions with
// solution-author-defined helper functions and for producing "available
// functions" diagnostics. The returned slice is a fresh copy the caller may
// mutate.
func BuiltinFuncNames() []string {
	ext := getExtensionFuncMap()
	seen := make(map[string]struct{}, len(ext)+len(goTemplateBuiltins))
	for name := range ext {
		seen[name] = struct{}{}
	}
	for _, name := range goTemplateBuiltins {
		seen[name] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsBuiltinFunc reports whether name is a built-in function (a text/template
// builtin or a registered extension function) available to every gotmpl
// template. Author-defined helper functions must not collide with these names.
func IsBuiltinFunc(name string) bool {
	if name == "" {
		return false
	}
	for _, b := range goTemplateBuiltins {
		if b == name {
			return true
		}
	}
	_, ok := getExtensionFuncMap()[name]
	return ok
}

// ExtractFunctionCalls parses a Go template body and returns the sorted, unique
// set of function names invoked within it (the identifier at the head of a
// command, e.g. "upper" in {{ upper .x }} or "myHelper" in {{ myHelper .x }}).
//
// declaredFuncs must include every author function name the body may reference.
// The text/template builtins and all extension functions (sprig + custom +
// embedder-registered) are always recognized and need not be supplied; unknown
// identifiers otherwise cause a parse error.
//
// It is used to build the author-function call graph for cycle detection. The
// returned names are not filtered -- callers narrow them to the set of interest
// (e.g. sibling author functions).
func ExtractFunctionCalls(content, leftDelim, rightDelim string, declaredFuncs []string) ([]string, error) {
	if leftDelim == "" {
		leftDelim = DefaultLeftDelim
	}
	if rightDelim == "" {
		rightDelim = DefaultRightDelim
	}

	ext := getExtensionFuncMap()
	funcNames := make(map[string]any, len(declaredFuncs)+len(goTemplateBuiltins)+len(ext))
	for _, name := range goTemplateBuiltins {
		funcNames[name] = true
	}
	for name := range ext {
		funcNames[name] = true
	}
	for _, name := range declaredFuncs {
		funcNames[name] = true
	}

	trees, err := parse.Parse("author-function", content, leftDelim, rightDelim, funcNames)
	if err != nil {
		return nil, fmt.Errorf("parsing template body: %w", err)
	}

	calls := make(map[string]struct{})
	for _, tree := range trees {
		if tree.Root != nil {
			collectFunctionCalls(tree.Root, calls)
		}
	}

	names := make([]string, 0, len(calls))
	for name := range calls {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// collectFunctionCalls recursively walks a parse tree collecting the identifier
// at the head of every command node -- i.e. the function names invoked.
func collectFunctionCalls(node parse.Node, calls map[string]struct{}) {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, child := range n.Nodes {
			collectFunctionCalls(child, calls)
		}
	case *parse.ActionNode:
		collectPipeFunctionCalls(n.Pipe, calls)
	case *parse.IfNode:
		collectPipeFunctionCalls(n.Pipe, calls)
		collectFunctionCalls(n.List, calls)
		collectFunctionCalls(n.ElseList, calls)
	case *parse.RangeNode:
		collectPipeFunctionCalls(n.Pipe, calls)
		collectFunctionCalls(n.List, calls)
		collectFunctionCalls(n.ElseList, calls)
	case *parse.WithNode:
		collectPipeFunctionCalls(n.Pipe, calls)
		collectFunctionCalls(n.List, calls)
		collectFunctionCalls(n.ElseList, calls)
	case *parse.TemplateNode:
		collectPipeFunctionCalls(n.Pipe, calls)
	}
}

// collectPipeFunctionCalls walks a pipe, recording each command's head
// identifier (a function call) and recursing into nested pipes carried as
// command arguments.
func collectPipeFunctionCalls(pipe *parse.PipeNode, calls map[string]struct{}) {
	if pipe == nil {
		return
	}
	for _, cmd := range pipe.Cmds {
		if cmd == nil {
			continue
		}
		for i, arg := range cmd.Args {
			switch a := arg.(type) {
			case *parse.IdentifierNode:
				// The head of a command (index 0) is a function invocation.
				// An identifier can only appear at the head of a command in a
				// valid tree, but guard on position for clarity.
				if i == 0 {
					calls[a.Ident] = struct{}{}
				}
			case *parse.PipeNode:
				collectPipeFunctionCalls(a, calls)
			}
		}
	}
}
