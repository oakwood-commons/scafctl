// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authorfuncs

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// identifierPattern matches a valid function or parameter identifier. It mirrors
// the pattern documented on spec.ParamDef.Name.
var identifierPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// FunctionNamePattern is the canonical pattern an author-defined function name
// must match: a Go-template identifier (no hyphens, unlike resolver/action/call
// names). Exposed so tooling (e.g. rename) can validate a candidate name with
// the same rule the loader enforces.
const FunctionNamePattern = `^[a-zA-Z_][a-zA-Z0-9_]*$`

// ValidateFunctionName reports whether name is a valid author-defined function
// name, applying the same rules the loader enforces in compileOne: it must be a
// valid Go-template identifier, must not use the reserved "__" prefix, and must
// not collide with a built-in or extension template function. It returns a
// descriptive error when name is invalid, or nil when it is acceptable.
func ValidateFunctionName(name string) error {
	if !identifierPattern.MatchString(name) {
		return fmt.Errorf("%q is not a valid function name (must match %s)", name, FunctionNamePattern)
	}
	if strings.HasPrefix(name, "__") {
		return fmt.Errorf("%q is not a valid function name (may not start with %q, a reserved prefix)", name, "__")
	}
	if gotmpl.IsBuiltinFunc(name) {
		return fmt.Errorf("%q is not a valid function name (collides with a built-in template function)", name)
	}
	return nil
}

// knownParamTypes is the set of accepted parameter type strings (canonical names
// plus the aliases spec.CoerceType/normalizeType understand). The empty type is
// allowed separately and means "pass through unchanged".
var knownParamTypes = map[string]struct{}{
	"string": {}, "int": {}, "integer": {}, "float": {}, "number": {},
	"bool": {}, "boolean": {}, "array": {}, "object": {}, "map": {},
	"time": {}, "timestamp": {}, "datetime": {}, "duration": {}, "any": {},
}

// compileOne validates a single function definition and, on success, returns a
// compiledFn. All problems are returned (rather than short-circuiting) so authors
// see every issue for the function at once. cf is non-nil only when there are no
// problems that would make binding unsafe.
func compileOne(name string, fn *spec.Function, allNames []string) (*compiledFn, []string) {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("function %q: "+format, append([]any{name}, args...)...))
	}

	if !identifierPattern.MatchString(name) {
		add("name is not a valid identifier (must match %s)", identifierPattern.String())
	}
	if strings.HasPrefix(name, "__") {
		add("name may not start with %q (reserved prefix)", "__")
	}
	if gotmpl.IsBuiltinFunc(name) {
		add("name collides with a built-in template function")
	}

	if fn == nil {
		add("definition is empty")
		return nil, problems
	}

	problems = append(problems, validateParams(name, fn.Params)...)

	// Exactly one body.
	switch {
	case fn.HasCel() && fn.HasTemplate():
		add("cel and template are mutually exclusive; set exactly one")
	case !fn.HasCel() && !fn.HasTemplate():
		add("must set exactly one of cel or template")
	}

	// Body syntax + reference extraction. Only attempt when a single body is set;
	// a malformed declaration above already produced a problem.
	var refs []string
	switch {
	case fn.HasCel() && !fn.HasTemplate():
		if err := celexp.ValidateSyntax(context.Background(), fn.Cel); err != nil {
			add("invalid cel body: %v", err)
		}
	case fn.HasTemplate() && !fn.HasCel():
		calls, err := gotmpl.ExtractFunctionCalls(fn.Template, "", "", allNames)
		if err != nil {
			add("invalid template body: %v", err)
		} else {
			refs = filterAuthorRefs(calls, allNames)
		}
	}

	if len(problems) > 0 {
		return nil, problems
	}

	return &compiledFn{
		name:   name,
		params: fn.Params,
		isCel:  fn.HasCel(),
		body:   bodyOf(fn),
		refs:   refs,
	}, nil
}

// validateParams checks that parameter names are valid and unique, that required
// parameters do not also declare a default, and that declared types are known.
func validateParams(fnName string, params []*spec.ParamDef) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("function %q: "+format, append([]any{fnName}, args...)...))
	}

	seen := make(map[string]struct{}, len(params))
	sawOptional := false
	for i, p := range params {
		if p == nil {
			add("parameter at position %d is empty", i+1)
			continue
		}
		if !identifierPattern.MatchString(p.Name) {
			add("parameter %q (position %d) is not a valid identifier", p.Name, i+1)
		}
		if _, dup := seen[p.Name]; dup {
			add("parameter name %q is declared more than once", p.Name)
		}
		seen[p.Name] = struct{}{}

		if p.Required && p.Default != nil {
			add("parameter %q is required and may not declare a default", p.Name)
		}

		// Parameters are bound by position, so a required parameter that follows
		// an optional one is unreachable: callers cannot supply the later
		// required argument without also supplying the earlier optional one,
		// defeating its default. Reject required-after-optional.
		if p.Required && sawOptional {
			add("required parameter %q must not follow an optional parameter (positional binding)", p.Name)
		}
		if !p.Required {
			sawOptional = true
		}

		if p.Type != "" {
			if _, ok := knownParamTypes[strings.ToLower(string(p.Type))]; !ok {
				add("parameter %q has unknown type %q", p.Name, p.Type)
			}
		}
	}
	return problems
}

// bodyOf returns the function's single body string.
func bodyOf(fn *spec.Function) string {
	if fn.HasCel() {
		return fn.Cel
	}
	return fn.Template
}

// filterAuthorRefs narrows a set of invoked function names to those that are
// author-declared (i.e. present in allNames). Self-references are kept so that a
// function whose template body calls itself is reported as a cycle.
func filterAuthorRefs(calls, allNames []string) []string {
	nameSet := make(map[string]struct{}, len(allNames))
	for _, n := range allNames {
		nameSet[n] = struct{}{}
	}
	var refs []string
	for _, c := range calls {
		if _, ok := nameSet[c]; ok {
			refs = append(refs, c)
		}
	}
	sort.Strings(refs)
	return refs
}

// detectCycle looks for a cycle in the author-function call graph (template
// bodies referencing sibling functions). It returns a human-readable problem
// describing the first cycle found, or "" when the graph is acyclic. Acyclicity
// guarantees that nested template rendering terminates.
func detectCycle(fns map[string]*compiledFn, order []string) string {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[string]int, len(fns))

	var stack []string
	var visit func(name string) string
	visit = func(name string) string {
		color[name] = gray
		stack = append(stack, name)
		cf := fns[name]
		if cf != nil {
			for _, dep := range cf.refs {
				switch color[dep] {
				case gray:
					return formatCycle(stack, dep)
				case white:
					if cyc := visit(dep); cyc != "" {
						return cyc
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[name] = black
		return ""
	}

	for _, name := range order {
		if color[name] == white {
			if cyc := visit(name); cyc != "" {
				return cyc
			}
		}
	}
	return ""
}

// formatCycle renders the cycle path from the point dep re-enters the DFS stack.
func formatCycle(stack []string, dep string) string {
	start := 0
	for i, n := range stack {
		if n == dep {
			start = i
			break
		}
	}
	cycle := append(append([]string{}, stack[start:]...), dep)
	return fmt.Sprintf("functions form a cycle: %s (template bodies may not call each other cyclically)", strings.Join(cycle, " -> "))
}
