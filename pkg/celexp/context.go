package celexp

import (
	"context"
	"fmt"

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
)

// BuildCELContext creates CEL environment options and variables for evaluation.
// This is a common pattern used throughout scafctl for setting up CEL execution contexts.
//
// Variable placement:
//   - resolverData: Placed under the "_" variable as map[string]any
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
//	// Basic resolver context with just resolver data
//	envOpts, vars := celexp.BuildCELContext(resolverData, nil)
//
//	// Transform context with __self
//	envOpts, vars := celexp.BuildCELContext(resolverData, map[string]any{celexp.VarSelf: currentValue})
//
//	// ForEach context with __self, __item, __index, and custom aliases
//	envOpts, vars := celexp.BuildCELContext(resolverData, map[string]any{
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
	resolverData map[string]any,
	additionalVars map[string]any,
) (envOpts []cel.EnvOption, vars map[string]any) {
	vars = make(map[string]any)
	envOpts = []cel.EnvOption{}

	// Add resolver data under "_" variable
	if resolverData != nil {
		vars["_"] = resolverData
		envOpts = append(envOpts, cel.Variable("_", cel.MapType(cel.StringType, cel.DynType)))
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
//   - resolverData: Data available under the "_" variable (e.g., _.name)
//   - additionalVars: Top-level variables (use Var* constants for __self, __item, __index)
//   - opts: Additional compile options to pass to expr.Compile()
//
// Returns:
//   - The evaluated result as a Go value (with CEL types converted to Go types)
//   - An error if compilation or evaluation fails
//
// Example usage:
//
//	// Simple evaluation with resolver data
//	result, err := celexp.EvaluateExpression(ctx, "_.name.upperAscii()", resolverData, nil)
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
//	result, err := celexp.EvaluateExpression(ctx, "prefix + ' ' + _.name", resolverData, map[string]any{
//	    "prefix": "Hello",
//	})
func EvaluateExpression(
	ctx context.Context,
	exprStr string,
	resolverData map[string]any,
	additionalVars map[string]any,
	opts ...Option,
) (any, error) {
	// Build CEL context with resolver data under "_" variable
	// and additional variables at top level
	envOpts, celVars := BuildCELContext(resolverData, additionalVars)

	// Create expression and add WithContext to the compile options
	expr := Expression(exprStr)
	compileOpts := append([]Option{WithContext(ctx)}, opts...)
	compiled, err := expr.Compile(envOpts, compileOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to compile expression: %w", err)
	}

	// Evaluate the expression
	result, err := compiled.Eval(celVars)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate expression: %w", err)
	}

	// Convert CEL types to Go types (handles deep conversion for arrays, maps, etc.)
	goResult := conversion.GoToCelValue(result)
	convertedResult := conversion.CelValueToGo(goResult)

	return convertedResult, nil
}
