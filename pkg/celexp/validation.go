package celexp

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/google/cel-go/cel"
)

// VarDecl creates a variable declaration that can be used for both compilation and validation.
// This is a convenience wrapper around cel.Variable that also enables type validation.
//
// Example:
//
//	decls := []celexp.VarDecl{
//	    celexp.NewVarDecl("x", cel.IntType),
//	    celexp.NewVarDecl("name", cel.StringType),
//	}
//	result, _ := expr.CompileWithVarDecls(decls)
//	err := result.ValidateVars(map[string]any{"x": int64(10), "name": "test"})
type VarDecl struct {
	name    string
	celType *cel.Type
}

// NewVarDecl creates a new variable declaration for use with CompileWithVarDecls.
func NewVarDecl(name string, celType *cel.Type) VarDecl {
	return VarDecl{name: name, celType: celType}
}

// ToEnvOption converts the variable declaration to a cel.EnvOption.
func (v VarDecl) ToEnvOption() cel.EnvOption {
	return cel.Variable(v.name, v.celType)
}

// CompileWithVarDecls compiles a CEL expression with variable declarations that enable
// runtime type validation. This is a convenience method that wraps Compile() and enables
// ValidateVars() functionality.
//
// Example:
//
//	decls := []celexp.VarDecl{
//	    celexp.NewVarDecl("x", cel.IntType),
//	    celexp.NewVarDecl("y", cel.IntType),
//	}
//	result, _ := expr.CompileWithVarDecls(decls)
//
//	// Now you can validate before evaluation
//	err := result.ValidateVars(map[string]any{"x": int64(10), "y": int64(20)})
func (e Expression) CompileWithVarDecls(varDecls []VarDecl, opts ...Option) (*CompileResult, error) {
	// Convert VarDecl to cel.EnvOption
	envOpts := make([]cel.EnvOption, len(varDecls))
	for i, vd := range varDecls {
		envOpts[i] = vd.ToEnvOption()
	}

	// Compile with the env options
	result, err := e.Compile(envOpts, opts...)
	if err != nil {
		return nil, err
	}

	// Store variable declarations for validation
	result.declaredVars = make(map[string]*cel.Type, len(varDecls))
	for _, vd := range varDecls {
		result.declaredVars[vd.name] = vd.celType
	}

	return result, nil
}

// ValidateVars checks if the provided runtime variables match the types declared
// during compilation. This provides early type validation before evaluation,
// catching type mismatches with clear error messages.
//
// Note: This only works if the expression was compiled using CompileWithVarDecls().
// If compiled with Compile() directly, this will skip validation (no declarations available).
//
// Returns an error if:
//   - A required variable is missing
//   - A variable's type doesn't match the declared type
//   - A nil value is provided for a non-nullable type
//
// Example:
//
//	decls := []celexp.VarDecl{
//	    celexp.NewVarDecl("x", cel.IntType),
//	    celexp.NewVarDecl("y", cel.IntType),
//	}
//	compiled, _ := expr.CompileWithVarDecls(decls)
//
//	// Valid - types match
//	err := compiled.ValidateVars(map[string]any{
//	    "x": int64(10),
//	    "y": int64(20),
//	})
//	// Returns: nil
//
//	// Invalid - wrong type
//	err = compiled.ValidateVars(map[string]any{
//	    "x": "string",
//	    "y": int64(20),
//	})
//	// Returns: error - variable "x": expected int, got string
//
//	// Invalid - missing variable
//	err = compiled.ValidateVars(map[string]any{
//	    "x": int64(10),
//	})
//	// Returns: error - missing required variable "y"
func (r *CompileResult) ValidateVars(vars map[string]any) error {
	if r == nil {
		return fmt.Errorf("compile result is nil")
	}

	// If no variable declarations were stored, skip validation
	// This happens when using Compile() instead of CompileWithVarDecls()
	if len(r.declaredVars) == 0 {
		return nil
	}

	// Check for missing required variables
	for varName := range r.declaredVars {
		if _, exists := vars[varName]; !exists {
			// Collect available variable names for helpful error message
			availableVars := make([]string, 0, len(vars))
			for k := range vars {
				availableVars = append(availableVars, k)
			}
			sort.Strings(availableVars)

			expectedType := celTypeToString(r.declaredVars[varName])
			if len(availableVars) > 0 {
				return fmt.Errorf("missing required variable %q (declared type: %s). Available variables: %v",
					varName, expectedType, availableVars)
			}
			return fmt.Errorf("missing required variable %q (declared type: %s). No variables provided",
				varName, expectedType)
		}
	}

	// Validate types for provided variables
	for varName, value := range vars {
		expectedType, declared := r.declaredVars[varName]
		if !declared {
			// Variable not declared during compilation - skip validation
			// (CEL will handle unknown variables during evaluation)
			continue
		}

		if err := validateType(varName, value, expectedType); err != nil {
			return err
		}
	}

	return nil
}

// GetDeclaredVariables returns the variable declarations that were provided during compilation.
// This can be useful for debugging or documentation purposes.
//
// Returns a map of variable name to CEL type string (e.g., "int", "string", "list").
// Returns an empty map if no variables were declared or compiled with Compile() instead of CompileWithVarDecls().
func (r *CompileResult) GetDeclaredVariables() map[string]string {
	if r == nil || r.declaredVars == nil {
		return make(map[string]string)
	}

	result := make(map[string]string, len(r.declaredVars))
	for name, celType := range r.declaredVars {
		result[name] = celTypeToString(celType)
	}
	return result
}

// validateType checks if a runtime value's type matches the expected CEL type
func validateType(varName string, value any, expectedType *cel.Type) error {
	if value == nil {
		// Check if nil is allowed for this type
		// In CEL, wrapper types (e.g., google.protobuf.Int64Value) allow nil
		// For now, we'll be conservative and reject nil unless it's a clear wrapper type
		return fmt.Errorf("variable %q type mismatch: nil value provided (expected %s)",
			varName, celTypeToString(expectedType))
	}

	actualType := reflect.TypeOf(value)
	compatible, reason := isCompatibleType(actualType, expectedType)

	if !compatible {
		return fmt.Errorf("variable %q type mismatch: expected %s, got %s (actual value: %v)%s",
			varName,
			celTypeToString(expectedType),
			actualType.String(),
			value,
			reason)
	}

	return nil
}

// isCompatibleType checks if a Go type is compatible with a CEL type
// Returns (compatible bool, reason string)
func isCompatibleType(goType reflect.Type, celType *cel.Type) (bool, string) {
	// Handle nil CEL type (should not happen in practice)
	if celType == nil {
		return true, ""
	}

	switch celType.String() {
	case "int":
		// CEL int is int64
		return goType.Kind() == reflect.Int64, ""

	case "uint":
		return goType.Kind() == reflect.Uint64, ""

	case "double":
		// CEL double is float64
		return goType.Kind() == reflect.Float64, ""

	case "bool":
		return goType.Kind() == reflect.Bool, ""

	case "string":
		return goType.Kind() == reflect.String, ""

	case "bytes":
		return goType.Kind() == reflect.Slice && goType.Elem().Kind() == reflect.Uint8, ""

	case "list":
		// Check if it's a slice or array
		if goType.Kind() != reflect.Slice && goType.Kind() != reflect.Array {
			return false, " (expected slice/array for list type)"
		}
		// TODO: Could check element type if cel.Type has that info
		return true, ""

	case "map":
		// Check if it's a map
		if goType.Kind() != reflect.Map {
			return false, " (expected map)"
		}
		// TODO: Could check key/value types if cel.Type has that info
		return true, ""

	default:
		// For parameterized types like list(string), check if base type matches
		typeStr := celType.String()

		// Handle list(T) types
		if len(typeStr) > 5 && typeStr[:4] == "list" {
			if goType.Kind() != reflect.Slice && goType.Kind() != reflect.Array {
				return false, " (expected slice/array)"
			}
			return true, ""
		}

		// Handle map(K, V) types
		if len(typeStr) > 4 && typeStr[:3] == "map" {
			if goType.Kind() != reflect.Map {
				return false, " (expected map)"
			}
			return true, ""
		}

		// For complex types (structs, messages, etc.), check if it's a map (common CEL pattern)
		// or accept the value and let CEL runtime do full validation
		if goType.Kind() == reflect.Map {
			return true, ""
		}
		// Accept structs as they might be message types
		if goType.Kind() == reflect.Struct {
			return true, ""
		}
		// Also accept interface types (like ref.Val)
		if goType.Kind() == reflect.Interface {
			return true, ""
		}
		return false, fmt.Sprintf(" (unsupported type for %s)", celType.String())
	}
}

// celTypeToString converts a CEL type to a human-readable string
func celTypeToString(t *cel.Type) string {
	if t == nil {
		return "any"
	}
	return t.String()
}
