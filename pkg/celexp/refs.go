// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/cel-go/cel"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// GetVariablesWithPrefix parses the CEL expression and returns all variable references
// that start with the specified prefix. The returned variable names do not include the prefix.
// It returns a deduplicated, sorted list of variable names. If prefix is empty, it defaults to "_."
//
// Example:
//
//	expr := celexp.CelExpression("_.user.name + _.config.value")
//	vars, err := expr.GetVariablesWithPrefix("_.")
//	// Returns: []string{"config", "user"}, nil (sorted)
//
//	expr := celexp.CelExpression("ctx.user.name + ctx.config.value")
//	vars, err := expr.GetVariablesWithPrefix("ctx.")
//	// Returns: []string{"config", "user"}, nil (sorted)
func (e Expression) GetVariablesWithPrefix(prefix string) ([]string, error) {
	// Default prefix to _. if empty
	if prefix == "" {
		prefix = "_."
	}

	// Create a CEL environment for parsing
	// Use the environment factory if available to include custom extensions
	var env *cel.Env
	var err error
	factory := getEnvFactory()
	if factory != nil {
		env, err = factory(context.Background())
	} else {
		env, err = cel.NewEnv()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	// Parse the expression to get the AST
	parsed, issues := env.Parse(string(e))
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to parse CEL expression: %w", issues.Err())
	}

	// Get the parsed expression
	parsedExpr, err := cel.AstToParsedExpr(parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to convert AST: %w", err)
	}

	// Extract variable references starting with prefix
	vars := make(map[string]struct{})
	extractVariablesWithPrefix(parsedExpr.GetExpr(), prefix, vars)

	// Convert map to sorted slice
	result := make([]string, 0, len(vars))
	for v := range vars {
		result = append(result, v)
	}
	sort.Strings(result)

	return result, nil
}

// GetUnderscoreVariables is a convenience method that calls GetVariablesWithPrefix with "_." prefix.
//
// Example:
//
//	expr := celexp.CelExpression("_.user.name + _.config.value")
//	vars, err := expr.GetUnderscoreVariables()
//	// Returns: []string{"user", "config"}, nil
func (e Expression) GetUnderscoreVariables() ([]string, error) {
	return e.GetVariablesWithPrefix("_.")
}

// RequiredVariables parses the CEL expression and returns all variable references
// found in the expression, regardless of prefix. This extracts ALL top-level identifiers
// that are not function names or comprehension variables.
// It returns a deduplicated, sorted list of variable names.
//
// This is useful for:
//   - Validating that all required variables are provided before evaluation
//   - Auto-generating input prompts for missing variables
//   - Documentation generation showing what inputs are needed
//   - IDE autocomplete for configuration files
//
// For expressions with prefixed variables (like _.x or ctx.y), use GetVariablesWithPrefix() instead.
//
// Example:
//
//	expr := celexp.Expression("x + y + z")
//	vars, err := expr.RequiredVariables()
//	// Returns: []string{"x", "y", "z"}, nil (sorted)
//
//	expr = celexp.Expression("user.name + config.value")
//	vars, err = expr.RequiredVariables()
//	// Returns: []string{"config", "user"}, nil (sorted)
//
//	expr = celexp.Expression("[1, 2, 3].filter(x, x > 1)")
//	vars, err = expr.RequiredVariables()
//	// Returns: []string{}, nil (x is a comprehension variable, not external)
func (e Expression) RequiredVariables() ([]string, error) {
	// Create a CEL environment for parsing
	// Use the environment factory if available to include custom extensions
	var env *cel.Env
	var err error
	factory := getEnvFactory()
	if factory != nil {
		env, err = factory(context.Background())
	} else {
		env, err = cel.NewEnv()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	// Parse the expression to get the AST
	parsed, issues := env.Parse(string(e))
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to parse CEL expression: %w", issues.Err())
	}

	// Get the parsed expression
	parsedExpr, err := cel.AstToParsedExpr(parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to convert AST: %w", err)
	}

	// Extract all variable references
	vars := make(map[string]struct{})
	comprehensionVars := make(map[string]struct{})
	extractAllVariables(parsedExpr.GetExpr(), vars, comprehensionVars)

	// Convert map to sorted slice
	result := make([]string, 0, len(vars))
	for v := range vars {
		result = append(result, v)
	}
	sort.Strings(result)

	return result, nil
}

// extractVariablesWithPrefix recursively walks the AST and collects variable names starting with the given prefix
func extractVariablesWithPrefix(expr *exprpb.Expr, prefix string, vars map[string]struct{}) {
	if expr == nil {
		return
	}

	// Determine the base identifier and field separator based on prefix
	// For "_.", the base is "_" and we append the field with "."
	// For "$", the base is "" and we use the identifier directly with "$" prefix
	var baseIdent string
	var useSelect bool

	if len(prefix) > 1 && prefix[len(prefix)-1] == '.' {
		// Prefix like "_." - base identifier is the part before the dot
		baseIdent = prefix[:len(prefix)-1]
		useSelect = true
	} else {
		// Prefix like "$" - match identifiers that start with this prefix
		baseIdent = ""
		useSelect = false
	}

	switch expr.GetExprKind().(type) {
	case *exprpb.Expr_IdentExpr:
		ident := expr.GetIdentExpr().GetName()
		// For "_." style, we don't capture standalone base identifiers
		// They will be captured via SelectExpr
		if !useSelect {
			// For "$" style, check if identifier starts with prefix
			if len(ident) >= len(prefix) && ident[:len(prefix)] == prefix {
				// Store without the prefix
				vars[ident[len(prefix):]] = struct{}{}
			}
		}

	case *exprpb.Expr_SelectExpr:
		selectExpr := expr.GetSelectExpr()
		operand := selectExpr.GetOperand()

		if useSelect {
			// For "_." style prefix, check if the operand is the base identifier
			if operand.GetIdentExpr() != nil && operand.GetIdentExpr().GetName() == baseIdent {
				// This is a _.something expression - capture it without the prefix
				field := selectExpr.GetField()
				vars[field] = struct{}{}
			} else {
				// Continue traversing for other variables
				extractVariablesWithPrefix(operand, prefix, vars)
			}
		} else {
			// For "$" style, traverse the operand
			extractVariablesWithPrefix(operand, prefix, vars)
		}

	case *exprpb.Expr_CallExpr:
		// Process function calls and their arguments
		call := expr.GetCallExpr()
		if call.GetTarget() != nil {
			extractVariablesWithPrefix(call.GetTarget(), prefix, vars)
		}
		for _, arg := range call.GetArgs() {
			extractVariablesWithPrefix(arg, prefix, vars)
		}

	case *exprpb.Expr_ListExpr:
		// Process list elements
		for _, elem := range expr.GetListExpr().GetElements() {
			extractVariablesWithPrefix(elem, prefix, vars)
		}

	case *exprpb.Expr_StructExpr:
		// Process struct/map entries
		structExpr := expr.GetStructExpr()
		for _, entry := range structExpr.GetEntries() {
			if entry.GetMapKey() != nil {
				extractVariablesWithPrefix(entry.GetMapKey(), prefix, vars)
			}
			extractVariablesWithPrefix(entry.GetValue(), prefix, vars)
		}

	case *exprpb.Expr_ComprehensionExpr:
		// Process comprehension expressions
		comp := expr.GetComprehensionExpr()
		extractVariablesWithPrefix(comp.GetIterRange(), prefix, vars)
		extractVariablesWithPrefix(comp.GetAccuInit(), prefix, vars)
		extractVariablesWithPrefix(comp.GetLoopCondition(), prefix, vars)
		extractVariablesWithPrefix(comp.GetLoopStep(), prefix, vars)
		extractVariablesWithPrefix(comp.GetResult(), prefix, vars)

	case *exprpb.Expr_ConstExpr:
		// Literals don't contain variable references
	}
}

// extractAllVariables recursively walks the AST and collects ALL identifier names,
// excluding comprehension variables (like 'x' in 'list.filter(x, x > 1)').
// The comprehensionVars map tracks variables introduced by comprehensions to exclude them.
func extractAllVariables(expr *exprpb.Expr, vars, comprehensionVars map[string]struct{}) {
	if expr == nil {
		return
	}

	switch expr.GetExprKind().(type) {
	case *exprpb.Expr_IdentExpr:
		ident := expr.GetIdentExpr().GetName()
		// Only add if it's not a comprehension variable
		if _, isCompVar := comprehensionVars[ident]; !isCompVar {
			vars[ident] = struct{}{}
		}

	case *exprpb.Expr_SelectExpr:
		// For select expressions like 'user.name', we only want the root identifier 'user'
		selectExpr := expr.GetSelectExpr()
		operand := selectExpr.GetOperand()

		// Recursively process the operand to get the root identifier
		extractAllVariables(operand, vars, comprehensionVars)

	case *exprpb.Expr_CallExpr:
		// Process function calls and their arguments
		call := expr.GetCallExpr()
		if call.GetTarget() != nil {
			extractAllVariables(call.GetTarget(), vars, comprehensionVars)
		}
		for _, arg := range call.GetArgs() {
			extractAllVariables(arg, vars, comprehensionVars)
		}

	case *exprpb.Expr_ListExpr:
		// Process list elements
		for _, elem := range expr.GetListExpr().GetElements() {
			extractAllVariables(elem, vars, comprehensionVars)
		}

	case *exprpb.Expr_StructExpr:
		// Process struct/map entries
		structExpr := expr.GetStructExpr()
		for _, entry := range structExpr.GetEntries() {
			if entry.GetMapKey() != nil {
				extractAllVariables(entry.GetMapKey(), vars, comprehensionVars)
			}
			extractAllVariables(entry.GetValue(), vars, comprehensionVars)
		}

	case *exprpb.Expr_ComprehensionExpr:
		// Comprehensions introduce new variables (like 'x' in 'list.filter(x, x > 1)')
		// We need to track these and exclude them from the results
		comp := expr.GetComprehensionExpr()

		// Create a new scope for comprehension variables
		localCompVars := make(map[string]struct{})
		for k, v := range comprehensionVars {
			localCompVars[k] = v
		}

		// Add the iteration variable to the exclusion list
		iterVar := comp.GetIterVar()
		if iterVar != "" {
			localCompVars[iterVar] = struct{}{}
		}

		// Add the accumulation variable to the exclusion list
		accumVar := comp.GetAccuVar()
		if accumVar != "" {
			localCompVars[accumVar] = struct{}{}
		}

		// Process iter_range with original scope (before comprehension variable is introduced)
		extractAllVariables(comp.GetIterRange(), vars, comprehensionVars)

		// Process other parts with the new scope (comprehension variables excluded)
		extractAllVariables(comp.GetAccuInit(), vars, localCompVars)
		extractAllVariables(comp.GetLoopCondition(), vars, localCompVars)
		extractAllVariables(comp.GetLoopStep(), vars, localCompVars)
		extractAllVariables(comp.GetResult(), vars, localCompVars)

	case *exprpb.Expr_ConstExpr:
		// Literals don't contain variable references
	}
}
