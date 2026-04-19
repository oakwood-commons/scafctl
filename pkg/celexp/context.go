// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp/conversion"
)

// Common CEL variable names used throughout scafctl.
// Use these constants when building additionalVars to ensure consistency.
const (
	// VarSelf is the variable name for the current value in transform/validate contexts.
	// Access via __self in CEL expressions.
	VarSelf = "__self"

	// VarItem is the variable name for the current array element in forEach iterations.
	// Access via __item in CEL expressions.
	VarItem = "__item"

	// VarIndex is the variable name for the current index in forEach iterations.
	// Access via __index in CEL expressions.
	VarIndex = "__index"

	// VarActions is the variable name for the actions namespace in workflow contexts.
	// Access via __actions in CEL expressions to get results from completed actions.
	// Example: __actions.build.results.exitCode
	VarActions = "__actions"

	// VarCwd is the variable name for the original working directory in action contexts.
	// Access via __cwd in CEL expressions when --output-dir redirects action output.
	VarCwd = "__cwd"

	// VarExecution is the variable name for resolver execution metadata in action contexts.
	// Available via __execution when --show-execution was used or when injected explicitly.
	// Example: __execution.resolvers.myResolver.status
	VarExecution = "__execution"

	// VarPlan is the variable name for pre-execution resolver topology data.
	// Injected before any resolver runs so resolvers can reference phase, dependsOn,
	// and dependencyCount for any resolver in when conditions and provider inputs.
	// Example: __plan["myResolver"].phase
	VarPlan = "__plan"

	// VarParams is the variable name for CLI parameters passed via -r flags.
	// Available in state backend input expressions so that dynamic backend
	// configuration (paths, URLs) can reference runtime values explicitly.
	// Unlike _, which contains resolver outputs, __params always contains the
	// raw CLI parameters regardless of resolver execution state.
	// Example: __params.gcp_project
	VarParams = "__params"
)

// BuildCELContext creates CEL environment options and variables for evaluation.
// This is a common pattern used throughout scafctl for setting up CEL execution contexts.
//
// Variable placement:
//   - rootData: Placed under the "_" variable (can be any type)
//   - additionalVars: Top-level variables (e.g., __self, __item, __index, custom aliases)
//
// Use the Var* constants (VarSelf, VarItem, VarIndex) for standard variable names:
//
//	additionalVars := map[string]any{
//	    celexp.VarSelf:  currentValue,
//	    celexp.VarItem:  item,
//	    celexp.VarIndex: index,
//	}
//
// Example usage:
//
//	// Basic resolver context with just root data
//	envOpts, vars := celexp.BuildCELContext(rootData, nil)
//
//	// Transform context with __self
//	envOpts, vars := celexp.BuildCELContext(rootData, map[string]any{celexp.VarSelf: currentValue})
//
//	// ForEach context with __self, __item, __index, and custom aliases
//	envOpts, vars := celexp.BuildCELContext(rootData, map[string]any{
//	    celexp.VarSelf:  currentValue,
//	    celexp.VarItem:  item,
//	    celexp.VarIndex: index,
//	    "myItem":        item,  // custom alias
//	})
//
//	// Then use for compilation and evaluation
//	expr := celexp.Expression("_.port + 1000")
//	compiled, _ := expr.Compile(envOpts, celexp.WithContext(ctx))
//	result, _ := compiled.Eval(vars)
func BuildCELContext(
	rootData any,
	additionalVars map[string]any,
) (envOpts []cel.EnvOption, vars map[string]any) {
	vars = make(map[string]any)
	envOpts = []cel.EnvOption{}

	// Add root data under "_" variable
	if rootData != nil {
		vars["_"] = rootData
		envOpts = append(envOpts, cel.Variable("_", cel.DynType))
	}

	// Add additional top-level variables (includes __self, __item, __index, and any custom vars)
	for k, v := range additionalVars {
		vars[k] = v
		envOpts = append(envOpts, cel.Variable(k, cel.DynType))
	}

	return envOpts, vars
}

// EvaluateExpression compiles and evaluates a CEL expression with the provided context.
// This is a higher-level convenience function that combines BuildCELContext, compilation,
// evaluation, and type conversion into a single call.
//
// Parameters:
//   - ctx: Context for compilation (used for caching and cancellation)
//   - exprStr: The CEL expression string to evaluate
//   - rootData: Data available under the "_" variable (e.g., _.name)
//   - additionalVars: Top-level variables (use Var* constants for __self, __item, __index)
//   - opts: Additional compile options to pass to expr.Compile()
//
// Returns:
//   - The evaluated result as a Go value (with CEL types converted to Go types)
//   - An error if compilation or evaluation fails
//
// Example usage:
//
//	// Simple evaluation with root data
//	result, err := celexp.EvaluateExpression(ctx, "_.name.upperAscii()", rootData, nil)
//
//	// With __self for transforms
//	result, err := celexp.EvaluateExpression(ctx, "__self * 2", nil, map[string]any{
//	    celexp.VarSelf: currentValue,
//	})
//
//	// With __item and __index for forEach
//	result, err := celexp.EvaluateExpression(ctx, "__item.name + ' at ' + string(__index)", nil, map[string]any{
//	    celexp.VarItem:  item,
//	    celexp.VarIndex: index,
//	})
//
//	// With custom variables
//	result, err := celexp.EvaluateExpression(ctx, "prefix + ' ' + _.name", rootData, map[string]any{
//	    "prefix": "Hello",
//	})
func EvaluateExpression(
	ctx context.Context,
	exprStr string,
	rootData any,
	additionalVars map[string]any,
	opts ...Option,
) (any, error) {
	// Build CEL context with root data under "_" variable
	// and additional variables at top level
	envOpts, celVars := BuildCELContext(rootData, additionalVars)

	// Create expression and add WithContext to the compile options
	expr := Expression(exprStr)
	compileOpts := append([]Option{WithContext(ctx)}, opts...)
	compiled, err := expr.Compile(envOpts, compileOpts...)
	if err != nil {
		availableVars := describeAvailableVars(rootData, additionalVars)
		return nil, fmt.Errorf("failed to compile expression %q: %w\nAvailable variables: %s", exprStr, err, availableVars)
	}

	// Evaluate the expression
	result, err := compiled.Eval(celVars)
	if err != nil {
		availableVars := describeAvailableVars(rootData, additionalVars)
		dataShape := describeDataShape(rootData)
		return nil, fmt.Errorf("failed to evaluate expression %q: %w\nAvailable variables: %s\nData shape of _: %s", exprStr, err, availableVars, dataShape)
	}

	// Convert CEL types to Go types (handles deep conversion for arrays, maps, etc.)
	goResult := conversion.GoToCelValue(result)
	convertedResult := conversion.CelValueToGo(goResult)

	return convertedResult, nil
}

// describeAvailableVars returns a human-readable summary of available variable names.
// This is used in error messages to help users understand what variables they can reference.
func describeAvailableVars(rootData any, additionalVars map[string]any) string {
	var names []string
	if rootData != nil {
		names = append(names, "_")
	}
	for k := range additionalVars {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// describeDataShape returns a concise summary of a data value's structure.
// For maps, it shows top-level keys and their Go types.
// For slices, it shows the element type if uniform, or "mixed" otherwise.
// For scalar types, it returns the Go type name.
// This is used in error messages to help users understand the shape of `_`.
//
// The output never includes values — only keys and types — to avoid leaking
// sensitive data in error messages.
func describeDataShape(data any) string {
	if data == nil {
		return "(nil)"
	}

	v := reflect.ValueOf(data)

	switch v.Kind() { //nolint:exhaustive // Only map/slice/array need special handling; everything else falls through to describeType.
	case reflect.Map:
		if v.Len() == 0 {
			return "{} (empty map)"
		}

		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)

		// Truncate if too many keys
		const maxKeys = 20
		truncated := false
		if len(keys) > maxKeys {
			keys = keys[:maxKeys]
			truncated = true
		}

		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			val := v.MapIndex(reflect.ValueOf(key))
			if val.IsValid() {
				parts = append(parts, fmt.Sprintf("%s: %s", key, describeType(val.Interface())))
			}
		}

		result := "{" + strings.Join(parts, ", ") + "}"
		if truncated {
			result += fmt.Sprintf(" (and %d more keys)", v.Len()-maxKeys)
		}
		return result

	case reflect.Slice, reflect.Array:
		if v.Len() == 0 {
			return "[] (empty list)"
		}
		return fmt.Sprintf("[%s] (len=%d)", describeElementTypes(v), v.Len())

	default: // reflect.String, reflect.Bool, reflect.Int*, reflect.Uint*, reflect.Float*, reflect.Struct, etc.
		return describeType(data)
	}
}

// describeType returns a concise type description for a value.
func describeType(v any) string {
	if v == nil {
		return "nil"
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() { //nolint:exhaustive // Exhaustive listing not needed; default formats with %T.
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "uint"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Map:
		return "map"
	case reflect.Slice, reflect.Array:
		return "list"
	default: // reflect.Struct, reflect.Ptr, reflect.Chan, reflect.Func, etc.
		return fmt.Sprintf("%T", v)
	}
}

// describeElementTypes returns a description of element types in a slice/array.
func describeElementTypes(v reflect.Value) string {
	if v.Len() == 0 {
		return "empty"
	}

	types := make(map[string]bool)
	for i := 0; i < v.Len() && i < 10; i++ {
		elem := v.Index(i)
		if elem.IsValid() {
			types[describeType(elem.Interface())] = true
		}
	}

	typeNames := make([]string, 0, len(types))
	for t := range types {
		typeNames = append(typeNames, t)
	}
	sort.Strings(typeNames)

	if len(typeNames) == 1 {
		return typeNames[0]
	}
	return strings.Join(typeNames, "|")
}
