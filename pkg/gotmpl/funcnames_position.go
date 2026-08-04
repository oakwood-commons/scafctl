// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"fmt"
	"text/template/parse"
)

// GetGoTemplatePositionedFunctionCalls parses a Go template and returns every
// function invocation ({{ name ... }} -- the identifier at the head of a
// command) with the byte range of its name within the template source, in
// source order. It is the positioned counterpart of ExtractFunctionCalls.
//
// declaredFuncs must include every author function name the body may reference
// (the text/template builtins and all extension functions are always
// recognized); unknown identifiers otherwise cause a parse error. The returned
// refs are NOT filtered to author functions -- callers narrow them to the set of
// interest (e.g. spec.functions names), mirroring how CEL/template references
// are reconciled against an authoritative name set.
func GetGoTemplatePositionedFunctionCalls(content, leftDelim, rightDelim string, declaredFuncs []string) ([]PositionedRef, error) {
	if content == "" {
		return nil, fmt.Errorf("template content cannot be empty")
	}
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
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var out []PositionedRef
	for _, tree := range trees {
		if tree.Root != nil {
			walkPositionedFuncCalls(tree.Root, content, &out)
		}
	}
	return out, nil
}

// walkPositionedFuncCalls recursively walks the parse tree, emitting a
// PositionedRef for the head identifier of every command (a function call). It
// mirrors collectFunctionCalls but records byte positions.
func walkPositionedFuncCalls(node parse.Node, src string, out *[]PositionedRef) {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, child := range n.Nodes {
			walkPositionedFuncCalls(child, src, out)
		}
	case *parse.ActionNode:
		walkPipeFuncCalls(n.Pipe, src, out)
	case *parse.IfNode:
		walkPipeFuncCalls(n.Pipe, src, out)
		walkPositionedFuncCalls(n.List, src, out)
		walkPositionedFuncCalls(n.ElseList, src, out)
	case *parse.RangeNode:
		walkPipeFuncCalls(n.Pipe, src, out)
		walkPositionedFuncCalls(n.List, src, out)
		walkPositionedFuncCalls(n.ElseList, src, out)
	case *parse.WithNode:
		walkPipeFuncCalls(n.Pipe, src, out)
		walkPositionedFuncCalls(n.List, src, out)
		walkPositionedFuncCalls(n.ElseList, src, out)
	case *parse.TemplateNode:
		walkPipeFuncCalls(n.Pipe, src, out)
	}
}

// walkPipeFuncCalls records each command's head identifier (a function call)
// and recurses into nested pipes carried as command arguments.
func walkPipeFuncCalls(pipe *parse.PipeNode, src string, out *[]PositionedRef) {
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
				// Only the head of a command (index 0) is a function invocation.
				if i == 0 {
					if ref, ok := identNodeRange(src, a); ok {
						*out = append(*out, ref)
					}
				}
			case *parse.PipeNode:
				walkPipeFuncCalls(a, src, out)
			}
		}
	}
}

// identNodeRange computes the byte range of a function-call identifier. The
// parser sets IdentifierNode.Pos to the byte offset of the identifier's start in
// the template source; the result is validated against src and dropped if it
// does not slice out the identifier exactly (fail-safe).
func identNodeRange(src string, id *parse.IdentifierNode) (PositionedRef, bool) {
	start := int(id.Pos)
	end := start + len(id.Ident)
	if start < 0 || end > len(src) || src[start:end] != id.Ident {
		return PositionedRef{}, false
	}
	return PositionedRef{Name: id.Ident, Offset: start, Len: len(id.Ident), Kind: RefKindFunctionCall}, true
}
