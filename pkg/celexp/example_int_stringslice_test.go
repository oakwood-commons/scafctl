// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp_test

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// ExampleEvalAs_int demonstrates evaluating CEL expressions that return int values.
// This is useful for configuration values like port numbers, counts, or timeouts.
func ExampleEvalAs_int() {
	// Define a CEL expression that calculates a port number
	expr := celexp.Expression("port > 0 && port < 65536 ? port : 8080")

	// Compile the expression
	result, err := expr.Compile([]cel.EnvOption{
		cel.Variable("port", cel.IntType),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Evaluate with a valid port
	port, err := celexp.EvalAs[int](result, map[string]any{
		"port": int64(3000),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Port: %d\n", port)
	// Output: Port: 3000
}

// ExampleEvalAs_stringSlice demonstrates evaluating CEL expressions that return []string.
// This is useful for lists of file paths, tags, environment variables, etc.
func ExampleEvalAs_stringSlice() {
	// Define a CEL expression that returns environment-specific tags
	expr := celexp.Expression("env == 'prod' ? ['production', 'critical'] : ['development']")

	// Compile the expression
	result, err := expr.Compile([]cel.EnvOption{
		cel.Variable("env", cel.StringType),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Evaluate for production environment
	tags, err := celexp.EvalAs[[]string](result, map[string]any{
		"env": "prod",
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Tags: %v\n", tags)
	// Output: Tags: [production critical]
}

// ExampleEvalAsWithContext_int demonstrates evaluating int expressions with context support.
func ExampleEvalAsWithContext_int() {
	expr := celexp.Expression("x * 2")

	result, err := expr.Compile([]cel.EnvOption{
		cel.Variable("x", cel.IntType),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	ctx := context.Background()
	value, err := celexp.EvalAsWithContext[int](ctx, result, map[string]any{
		"x": int64(21),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Result: %d\n", value)
	// Output: Result: 42
}

// ExampleEvalAsWithContext_stringSlice demonstrates evaluating []string expressions with context.
func ExampleEvalAsWithContext_stringSlice() {
	// Filter file extensions
	expr := celexp.Expression("extensions.filter(e, e.startsWith('.'))")

	result, err := expr.Compile([]cel.EnvOption{
		cel.Variable("extensions", cel.ListType(cel.StringType)),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	ctx := context.Background()
	filtered, err := celexp.EvalAsWithContext[[]string](ctx, result, map[string]any{
		"extensions": []string{".go", ".md", "txt", ".yaml"},
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Filtered: %v\n", filtered)
	// Output: Filtered: [.go .md .yaml]
}
