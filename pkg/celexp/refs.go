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
// Both dot notation (_.resolverName) and bracket notation (_["resolverName"]) are supported
// for prefixes ending in "." (like "_.").
//
// Example:
//
//	expr := celexp.CelExpression("_.user.name + _.config.value")
//	 vars, err := expr.GetVariablesWithPrefix(ctx, "\.")
//	 // Returns: []string{"config", "user"}, nil (sorted)
//
//	 expr := celexp.CelExpression(`_["user"].name + _["config"].value`)
//	 vars, err := expr.GetVariablesWithPrefix(ctx, "_.")
//	 // Returns: []string{"config", "user"}, nil (sorted)
func (e Expression) GetVariablesWithPrefix(ctx context.Context, prefix string) ([]string, error) {
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
		env, err = factory(ctx)
	} else {
		// Enable optional types so optional access (_.?name, _[?"name"]) parses
		// even when no extension factory is registered (e.g. white-box tests).
		env, err = cel.NewEnv(cel.OptionalTypes())
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
// Supports both dot notation (_.name) and bracket notation (_["name"]).
//
// Example:
//
//	expr := celexp.CelExpression("_.user.name + _.config.value")
//	vars, err := expr.GetUnderscoreVariables(ctx)
//	// Returns: []string{"config", "user"}, nil
//
//	expr := celexp.CelExpression(`_["user"].name + _["config"].value`)
//	vars, err := expr.GetUnderscoreVariables(ctx)
//	// Returns: []string{"config", "user"}, nil
func (e Expression) GetUnderscoreVariables(ctx context.Context) ([]string, error) {
	return e.GetVariablesWithPrefix(ctx, "_.")
}

// GetUnderscoreVariablesByOptionality parses the CEL expression and returns the
// "_"-scoped references split by access style: hard references (_.name,
// _["name"]) and optional-only references (_.?name, _[?"name"]). A name that is
// accessed with hard syntax anywhere in the expression is reported only in hard,
// never in optional -- a single non-optional use makes the whole dependency hard.
//
// This lets dependency inference treat optional access as a soft dependency: it
// orders the consumer after the target when the target exists, but does not fail
// loading when the target is absent (the author's .orValue()/optional handling
// supplies a fallback).
//
// Both returned slices are deduplicated and sorted.
//
// Example:
//
//	expr := celexp.Expression(`_.a + _.?b.orValue("") + _.?a`)
//	hard, optional, _ := expr.GetUnderscoreVariablesByOptionality(ctx)
//	// hard == []string{"a"}, optional == []string{"b"}
//	// (a is hard despite the _.?a use; b is optional-only)
func (e Expression) GetUnderscoreVariablesByOptionality(ctx context.Context) (hard, optional []string, err error) {
	env, err := e.parseEnv(ctx)
	if err != nil {
		return nil, nil, err
	}

	parsed, issues := env.Parse(string(e))
	if issues != nil && issues.Err() != nil {
		return nil, nil, fmt.Errorf("failed to parse CEL expression: %w", issues.Err())
	}

	parsedExpr, err := cel.AstToParsedExpr(parsed)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert AST: %w", err)
	}

	hardSet := make(map[string]struct{})
	optionalSet := make(map[string]struct{})
	walkVariablesWithPrefix(parsedExpr.GetExpr(), "_.", func(name string, opt bool) {
		if opt {
			optionalSet[name] = struct{}{}
		} else {
			hardSet[name] = struct{}{}
		}
	})

	hard = make([]string, 0, len(hardSet))
	for v := range hardSet {
		hard = append(hard, v)
	}
	sort.Strings(hard)

	// A name accessed with hard syntax anywhere dominates: exclude it from optional.
	optional = make([]string, 0, len(optionalSet))
	for v := range optionalSet {
		if _, isHard := hardSet[v]; !isHard {
			optional = append(optional, v)
		}
	}
	sort.Strings(optional)

	return hard, optional, nil
}

// parseEnv builds a CEL environment for parsing, using the registered
// environment factory (with custom extensions) when available and falling back
// to a minimal environment with optional types enabled so optional access
// (_.?name, _[?"name"]) parses in white-box tests.
func (e Expression) parseEnv(ctx context.Context) (*cel.Env, error) {
	if factory := getEnvFactory(); factory != nil {
		env, err := factory(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL environment: %w", err)
		}
		return env, nil
	}
	env, err := cel.NewEnv(cel.OptionalTypes())
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}
	return env, nil
}

// MapLiteralKeys parses the CEL expression and, if its top-level node is a map
// literal with constant string keys (e.g. {"a": _.x, "b": proj}), returns the
// list of those keys and true. For any other expression shape -- a function
// call such as map.merge(...), an identifier, a map with non-constant or
// non-string keys, etc. -- it returns nil and false, signalling that the set of
// keys the expression produces cannot be determined statically.
//
// This is used by resolver dependency inference to decide whether a go-template
// step's `data: {expr: ...}` input has a statically-known key set. When the keys
// are known, template `{{ .field }}` accessors that match a key are recognised
// as data-context references rather than resolver dependencies.
//
// Example:
//
//	keys, ok := celexp.Expression(`{"projects": proj, "app": _.appName}`).MapLiteralKeys(ctx)
//	// keys == []string{"app", "projects"}, ok == true
//
//	keys, ok := celexp.Expression(`map.merge(_.a, _.b)`).MapLiteralKeys(ctx)
//	// keys == nil, ok == false
func (e Expression) MapLiteralKeys(ctx context.Context) ([]string, bool) {
	var env *cel.Env
	var err error
	factory := getEnvFactory()
	if factory != nil {
		env, err = factory(ctx)
	} else {
		env, err = cel.NewEnv(cel.OptionalTypes())
	}
	if err != nil {
		return nil, false
	}

	parsed, issues := env.Parse(string(e))
	if issues != nil && issues.Err() != nil {
		return nil, false
	}

	parsedExpr, err := cel.AstToParsedExpr(parsed)
	if err != nil {
		return nil, false
	}

	structExpr := parsedExpr.GetExpr().GetStructExpr()
	if structExpr == nil {
		// Not a map/struct literal (e.g. a function call or identifier).
		return nil, false
	}
	// A message-construction expression (e.g. Foo{...}) is not a plain map.
	if structExpr.GetMessageName() != "" {
		return nil, false
	}

	entries := structExpr.GetEntries()
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		mapKey := entry.GetMapKey()
		if mapKey == nil {
			return nil, false
		}
		sv, ok := mapKey.GetConstExpr().GetConstantKind().(*exprpb.Constant_StringValue)
		if !ok {
			// Non-constant or non-string key -- keys are not statically known.
			return nil, false
		}
		keys = append(keys, sv.StringValue)
	}

	sort.Strings(keys)
	return keys, true
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
//	vars, err := expr.RequiredVariables(ctx)
//	// Returns: []string{"x", "y", "z"}, nil (sorted)
//
//	expr = celexp.Expression("user.name + config.value")
//	vars, err = expr.RequiredVariables(ctx)
//	// Returns: []string{"config", "user"}, nil (sorted)
//
//	expr = celexp.Expression("[1, 2, 3].filter(x, x > 1)")
//	vars, err = expr.RequiredVariables(ctx)
//	// Returns: []string{}, nil (x is a comprehension variable, not external)
func (e Expression) RequiredVariables(ctx context.Context) ([]string, error) {
	// Create a CEL environment for parsing
	// Use the environment factory if available to include custom extensions
	var env *cel.Env
	var err error
	factory := getEnvFactory()
	if factory != nil {
		env, err = factory(ctx)
	} else {
		// Enable optional types so optional access (_.?name, _[?"name"]) parses
		// even when no extension factory is registered (e.g. white-box tests).
		env, err = cel.NewEnv(cel.OptionalTypes())
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

// refVisitor receives each discovered prefix-scoped reference along with whether
// it was reached through optional-access syntax (_.?name or _[?"name"]). The same
// name may be reported more than once (e.g. both hard and optional); callers
// reconcile duplicates (a hard occurrence dominates an optional one).
type refVisitor func(name string, optional bool)

// extractVariablesWithPrefix recursively walks the AST and collects variable
// names starting with the given prefix into vars, ignoring optionality.
func extractVariablesWithPrefix(expr *exprpb.Expr, prefix string, vars map[string]struct{}) {
	walkVariablesWithPrefix(expr, prefix, func(name string, _ bool) {
		vars[name] = struct{}{}
	})
}

// walkVariablesWithPrefix recursively walks the AST and invokes visit for every
// prefix-scoped reference, distinguishing hard access (_.name, _["name"]) from
// optional access (_.?name, _[?"name"]). Optionality is only tracked for the
// explicit optional-access operators; has()-guarded plain access (_.name) is
// reported as hard.
func walkVariablesWithPrefix(expr *exprpb.Expr, prefix string, visit refVisitor) {
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
				visit(ident[len(prefix):], false)
			}
		}

	case *exprpb.Expr_SelectExpr:
		selectExpr := expr.GetSelectExpr()
		operand := selectExpr.GetOperand()

		if useSelect {
			// For "_." style prefix, check if the operand is the base identifier
			if operand.GetIdentExpr() != nil && operand.GetIdentExpr().GetName() == baseIdent {
				// This is a _.something expression - capture it as a hard reference.
				visit(selectExpr.GetField(), false)
			} else {
				// Continue traversing for other variables
				walkVariablesWithPrefix(operand, prefix, visit)
			}
		} else {
			// For "$" style, traverse the operand
			walkVariablesWithPrefix(operand, prefix, visit)
		}

	case *exprpb.Expr_CallExpr:
		// Process function calls and their arguments
		call := expr.GetCallExpr()

		// Handle index and optional access, all of which are parsed as a CallExpr
		// with args[0] the operand and args[1] the field/key string constant:
		//   _["resolverName"]  -> function "_[_]"   (bracket index, hard)
		//   _.?resolverName    -> function "_?._"   (optional select)
		//   _[?"resolverName"] -> function "_[?_]"  (optional index)
		if useSelect && len(call.GetArgs()) == 2 {
			fn := call.GetFunction()
			switch fn {
			case "_[_]", "_?._", "_[?_]":
				operand := call.GetArgs()[0]
				key := call.GetArgs()[1]
				if operand.GetIdentExpr() != nil && operand.GetIdentExpr().GetName() == baseIdent {
					// Check if the key is a string constant (e.g., _["resolverName"])
					if key.GetConstExpr() != nil && key.GetConstExpr().GetStringValue() != "" {
						// Only the two optional operators signal optionality; a
						// plain bracket index (_[_]) is a hard reference.
						visit(key.GetConstExpr().GetStringValue(), fn != "_[_]")
					}
				}
			}
		}

		if call.GetTarget() != nil {
			walkVariablesWithPrefix(call.GetTarget(), prefix, visit)
		}
		for _, arg := range call.GetArgs() {
			walkVariablesWithPrefix(arg, prefix, visit)
		}

	case *exprpb.Expr_ListExpr:
		// Process list elements
		for _, elem := range expr.GetListExpr().GetElements() {
			walkVariablesWithPrefix(elem, prefix, visit)
		}

	case *exprpb.Expr_StructExpr:
		// Process struct/map entries
		structExpr := expr.GetStructExpr()
		for _, entry := range structExpr.GetEntries() {
			if entry.GetMapKey() != nil {
				walkVariablesWithPrefix(entry.GetMapKey(), prefix, visit)
			}
			walkVariablesWithPrefix(entry.GetValue(), prefix, visit)
		}

	case *exprpb.Expr_ComprehensionExpr:
		// Process comprehension expressions
		comp := expr.GetComprehensionExpr()
		walkVariablesWithPrefix(comp.GetIterRange(), prefix, visit)
		walkVariablesWithPrefix(comp.GetAccuInit(), prefix, visit)
		walkVariablesWithPrefix(comp.GetLoopCondition(), prefix, visit)
		walkVariablesWithPrefix(comp.GetLoopStep(), prefix, visit)
		walkVariablesWithPrefix(comp.GetResult(), prefix, visit)

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
