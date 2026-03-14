// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package celeval provides a Go template extension function for evaluating
// CEL (Common Expression Language) expressions inline within Go templates.
package celeval

import (
	"context"
	"fmt"
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

// CelFunc returns an ExtFunction that evaluates a CEL expression inline
// within a Go template. The template's current context (.) is available
// as the root variable (_) inside the CEL expression.
//
// Example usage in a Go template:
//
//	{{ cel "_.items.filter(x, x.active)" . }}
//	{{ cel "'hello ' + _.name" . }}
func CelFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name: "cel",
		Description: "Evaluates a CEL (Common Expression Language) expression inline. " +
			"The second argument is the data context, accessible as _ in the expression. " +
			"Supports all standard CEL functions plus scafctl CEL extensions. " +
			"For complex or repeated expressions, consider pre-computing values in CEL resolvers.",
		Custom: true,
		Links:  []string{"https://github.com/google/cel-spec"},
		Examples: []gotmpl.Example{
			{
				Description: "Filter a list",
				Template:    `{{ cel "_.items.filter(x, x.status == 'active')" . }}`,
			},
			{
				Description: "String concatenation",
				Template:    `{{ cel "'Hello, ' + _.name + '!'" . }}`,
			},
			{
				Description: "Conditional expression",
				Template:    `{{ cel "_.count > 10 ? 'many' : 'few'" . }}`,
			},
			{
				Description: "Map access with default",
				Template:    `{{ cel "has(_.config.timeout) ? _.config.timeout : '30s'" . }}`,
			},
		},
		Func: template.FuncMap{
			"cel": Cel,
		},
	}
}

// Cel evaluates a CEL expression with the given data as root context.
//
// Parameters:
//   - expr: The CEL expression string to evaluate
//   - data: The data context, accessible as _ in the expression
//
// Returns the result of the CEL evaluation, which can be any Go type
// (string, number, bool, list, map, etc.).
//
// For performance-sensitive templates, prefer pre-computing complex expressions
// in CEL resolvers rather than using inline cel() calls in templates.
func Cel(expr string, data any) (any, error) {
	return CelWithContext(context.Background(), expr, data)
}

// CelWithContext evaluates a CEL expression with an explicit context so that
// parent timeouts and cancellation signals are respected during evaluation.
func CelWithContext(ctx context.Context, expr string, data any) (any, error) {
	if expr == "" {
		return nil, fmt.Errorf("cel: expression cannot be empty")
	}

	result, err := celexp.EvaluateExpression(
		ctx,
		expr,
		data,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("cel: %w", err)
	}

	return result, nil
}

// CelFuncWithContext returns a template.FuncMap entry that binds the cel
// function to the given context. Use this to ensure inline CEL evaluation
// in templates respects the caller's timeout and cancellation.
func CelFuncWithContext(ctx context.Context) template.FuncMap {
	return template.FuncMap{
		"cel": func(expr string, data any) (any, error) {
			return CelWithContext(ctx, expr, data)
		},
	}
}
