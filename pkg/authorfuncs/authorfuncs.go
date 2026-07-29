// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package authorfuncs compiles solution-author-defined Go template helper
// functions (spec.functions) into a bindable library of template.FuncMap
// entries.
//
// Each declared function has ordered, typed parameters and exactly one body:
//
//   - a CEL expression (spec-idiomatic; returns any typed value), evaluated with
//     the bound arguments under the args namespace (_.args.name), or
//   - a Go template body (Helm-style named template; returns a string), rendered
//     with the bound arguments as {{ .args.name }} and able to call sibling
//     author functions and all built-in helpers.
//
// Compilation validates names, parameters, bodies, and -- for template bodies --
// the acyclicity of the author-function call graph, which guarantees rendering
// terminates. The compiled Library implements provider.TemplateFuncBinder so it
// can be injected into go-template provider execution via the provider context
// without the provider or executor packages importing this package.
package authorfuncs

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/call"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// argsNamespace is the key under which bound arguments are exposed inside a
// function body (_.args.name in CEL, {{ .args.name }} in a template). It mirrors
// the Call args namespace for consistency across author-facing features.
const argsNamespace = call.ArgsNamespace

// compiledFn is a validated author function ready to be bound.
type compiledFn struct {
	name   string
	params []*spec.ParamDef
	isCel  bool
	body   string
	// refs holds the names of sibling author functions this function's template
	// body invokes. It is empty for CEL bodies (CEL cannot call author funcs).
	refs []string
}

// Library is a compiled, immutable set of author functions. Its zero value is
// not usable; construct one with Compile. A nil *Library is valid and behaves as
// an empty library (Bind returns nil, Fingerprint returns "").
type Library struct {
	order       []string // function names in stable (sorted) order
	fns         map[string]*compiledFn
	fingerprint string
	// render renders template-bodied functions. It is created in Compile rather
	// than at package init so it captures the extension (sprig + custom) function
	// set after the application has installed the func-map factory; a package-
	// level service would snapshot an empty extension set. It is stateless
	// (author funcs are supplied per call via TemplateOptions.Funcs) and safe for
	// concurrent use.
	render *gotmpl.Service
}

// Compile validates the given function definitions and returns a Library. It
// returns (nil, nil) when functions is empty. All validation problems are
// aggregated into a single error so authors see every issue at once.
func Compile(functions map[string]*spec.Function) (*Library, error) {
	if len(functions) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)

	lib := &Library{
		order:  names,
		fns:    make(map[string]*compiledFn, len(functions)),
		render: gotmpl.NewService(nil),
	}

	var problems []string
	for _, name := range names {
		cf, errs := compileOne(name, functions[name], names)
		problems = append(problems, errs...)
		if cf != nil {
			lib.fns[name] = cf
		}
	}

	// Reject cycles among template-bodied functions so rendering is guaranteed
	// to terminate. Only run when every referenced function compiled cleanly, to
	// avoid noise from already-reported problems.
	if len(problems) == 0 {
		if cycle := detectCycle(lib.fns, names); cycle != "" {
			problems = append(problems, cycle)
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("invalid spec.functions: %s", strings.Join(problems, "; "))
	}

	lib.fingerprint = computeFingerprint(functions, names)
	return lib, nil
}

// Bind returns the author functions as a template.FuncMap whose closures capture
// ctx for CEL evaluation and nested template rendering. It returns nil for a nil
// or empty library.
func (l *Library) Bind(ctx context.Context) template.FuncMap {
	if l == nil || len(l.fns) == 0 {
		return nil
	}

	fm := make(template.FuncMap, len(l.fns))
	for _, name := range l.order {
		cf := l.fns[name]
		// fm is captured by reference; by the time any closure runs (at template
		// execution) the loop has populated every sibling, so template bodies can
		// call each other. The compile-time acyclicity check guarantees this
		// mutual reference terminates.
		fm[name] = l.makeInvoker(ctx, cf, fm)
	}
	return fm
}

// Fingerprint returns a stable identifier for the function implementations so
// template caches distinguish same-named functions with different bodies.
func (l *Library) Fingerprint() string {
	if l == nil {
		return ""
	}
	return l.fingerprint
}

// Names returns the declared function names in stable order.
func (l *Library) Names() []string {
	if l == nil {
		return nil
	}
	out := make([]string, len(l.order))
	copy(out, l.order)
	return out
}

// makeInvoker builds the template.FuncMap closure for a single function. The
// closure accepts positional arguments, binds them to the declared parameters,
// and evaluates the body.
func (l *Library) makeInvoker(ctx context.Context, cf *compiledFn, authorFuncs template.FuncMap) func(...any) (any, error) {
	return func(args ...any) (any, error) {
		bound, err := bindArgs(cf, args)
		if err != nil {
			return nil, fmt.Errorf("function %q: %w", cf.name, err)
		}

		data := map[string]any{argsNamespace: bound}

		if cf.isCel {
			result, evalErr := celexp.EvaluateExpression(ctx, cf.body, data, nil)
			if evalErr != nil {
				return nil, fmt.Errorf("function %q: evaluating cel body: %w", cf.name, evalErr)
			}
			return result, nil
		}

		res, execErr := l.render.Execute(ctx, gotmpl.TemplateOptions{
			Content:          cf.body,
			Name:             "function:" + cf.name,
			Data:             data,
			Funcs:            authorFuncs,
			FuncsFingerprint: l.fingerprint,
			MissingKey:       gotmpl.MissingKeyError,
		})
		if execErr != nil {
			return nil, fmt.Errorf("function %q: rendering template body: %w", cf.name, execErr)
		}
		return res.Output, nil
	}
}

// bindArgs maps positional call arguments to the function's declared parameters,
// applying defaults for omitted optional parameters and coercing each value to
// its declared type.
func bindArgs(cf *compiledFn, args []any) (map[string]any, error) {
	if len(args) > len(cf.params) {
		return nil, fmt.Errorf("expected at most %d argument(s), got %d", len(cf.params), len(args))
	}

	bound := make(map[string]any, len(cf.params))
	for i, p := range cf.params {
		var raw any
		switch {
		case i < len(args):
			raw = args[i]
		case p.Required:
			return nil, fmt.Errorf("missing required argument %q (position %d)", p.Name, i+1)
		default:
			raw = p.Default
		}

		coerced, err := spec.CoerceType(raw, p.Type)
		if err != nil {
			return nil, fmt.Errorf("argument %q (position %d): %w", p.Name, i+1, err)
		}
		bound[p.Name] = coerced
	}
	return bound, nil
}

// computeFingerprint produces a stable hex digest of every function's name,
// parameters, body kind, and body content, so any change to an implementation
// changes the fingerprint (and thereby template cache keys).
func computeFingerprint(functions map[string]*spec.Function, sortedNames []string) string {
	h := sha256.New()
	for _, name := range sortedNames {
		fn := functions[name]
		fmt.Fprintf(h, "fn:%s\x00", name)
		for _, p := range fn.Params {
			fmt.Fprintf(h, "param:%s:%s:%t:%v\x00", p.Name, p.Type, p.Required, p.Default)
		}
		fmt.Fprintf(h, "cel:%s\x00template:%s\x00", fn.Cel, fn.Template)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
