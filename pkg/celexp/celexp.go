package celexp

import (
	"fmt"

	"github.com/google/cel-go/cel"
)

type (
	Expression      string
	ExtFunctionList []ExtFunction
)

type ExtFunction struct {
	Name          string          `json:"name,omitempty" yaml:"name,omitempty"`
	Links         []string        `json:"links,omitempty" yaml:"links,omitempty"`
	Examples      []Example       `json:"examples,omitempty" yaml:"examples,omitempty"`
	Description   string          `json:"description,omitempty" yaml:"description,omitempty"`
	EnvOptions    []cel.EnvOption `json:"-" yaml:"-"`
	FunctionNames []string        `json:"function_names,omitempty" yaml:"function_names,omitempty"`
	Custom        bool            `json:"custom,omitempty" yaml:"custom,omitempty"`
}

type Example struct {
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Expression  string   `json:"expression,omitempty" yaml:"expression,omitempty"`
	Links       []string `json:"links,omitempty" yaml:"links,omitempty"`
}

// CompileResult contains the compiled CEL program and metadata
type CompileResult struct {
	// Program is the compiled CEL program ready for evaluation
	Program cel.Program

	// Expression is the original expression that was compiled
	Expression Expression
}

// Compile parses, checks, and compiles a CEL expression into an executable program.
// It creates a CEL environment with the provided options, validates the expression,
// and returns a compiled program ready for evaluation.
//
// Example usage:
//
//	expr := celexp.CelExpression("x + y")
//	result, err := expr.Compile(cel.Variable("x", cel.IntType), cel.Variable("y", cel.IntType))
//	if err != nil {
//	    return err
//	}
//	value, err := result.Eval(map[string]any{"x": 10, "y": 20})
func (e Expression) Compile(opts ...cel.EnvOption) (*CompileResult, error) {
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	ast, issues := env.Compile(string(e))
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile expression %q: %w", e, issues.Err())
	}

	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create program from expression %q: %w", e, err)
	}

	return &CompileResult{
		Program:    prog,
		Expression: e,
	}, nil
}

// Eval evaluates the compiled CEL program with the provided variables.
// Variables should be a map where keys match the variable names declared
// during compilation.
//
// Example usage:
//
//	expr := celexp.CelExpression("name.startsWith('hello')")
//	result, _ := expr.Compile(cel.Variable("name", cel.StringType))
//	value, err := result.Eval(map[string]any{"name": "hello world"})
//	if err != nil {
//	    return err
//	}
//	fmt.Println(value) // true
func (r *CompileResult) Eval(vars map[string]any) (any, error) {
	if r == nil || r.Program == nil {
		return nil, fmt.Errorf("compile result or program is nil")
	}

	out, _, err := r.Program.Eval(vars)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate expression %q: %w", r.Expression, err)
	}

	return out.Value(), nil
}
