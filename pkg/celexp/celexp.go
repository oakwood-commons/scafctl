package celexp

import (
	"fmt"

	"github.com/google/cel-go/cel"
)

type ExtFunctionList []ExtFunction

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

// Compile parses, checks, and compiles a CEL expression into an executable program.
// It creates a CEL environment with the provided options, validates the expression,
// and returns a compiled program ready for evaluation.
//
// Example usage:
//
//	prog, err := celexp.Compile("x + y", cel.Variable("x", cel.IntType), cel.Variable("y", cel.IntType))
//	if err != nil {
//	    return err
//	}
//	result, err := celexp.Eval(prog, map[string]any{"x": 10, "y": 20})
func Compile(expression string, opts ...cel.EnvOption) (cel.Program, error) {
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile expression %q: %w", expression, issues.Err())
	}

	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create program from expression %q: %w", expression, err)
	}

	return prog, nil
}

// Eval evaluates a compiled CEL program with the provided variables.
// Variables should be a map where keys match the variable names declared
// during compilation.
//
// Example usage:
//
//	prog, _ := celexp.Compile("name.startsWith('hello')", cel.Variable("name", cel.StringType))
//	result, err := celexp.Eval(prog, map[string]any{"name": "hello world"})
//	if err != nil {
//	    return err
//	}
//	fmt.Println(result) // true
func Eval(program cel.Program, vars map[string]any) (any, error) {
	if program == nil {
		return nil, fmt.Errorf("program is nil")
	}

	out, _, err := program.Eval(vars)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate expression: %w", err)
	}

	return out.Value(), nil
}
