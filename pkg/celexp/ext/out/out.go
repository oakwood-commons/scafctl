package out

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// NilFunc returns a CEL function that takes any value and returns nil.
// This is useful for discarding values or as a placeholder in expressions.
//
// Example usage:
//
//	out.nil(someValue) // Always returns nil regardless of input
func NilFunc() celexp.ExtFunction {
	funcName := "out.nil"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Takes any value and returns nil",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Discard a value and return nil",
				Expression:  `out.nil("some value")`,
			},
			{
				Description: "Use with any type",
				Expression:  `out.nil({"key": "value"})`,
			},
			{
				Description: "Chain with other operations",
				Expression:  `out.nil(42)`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload("out_nil",
					[]*cel.Type{cel.DynType},
					cel.NullType,
					cel.UnaryBinding(func(_ ref.Val) ref.Val {
						// Always return null regardless of input
						return types.NullValue
					}),
				),
			),
		},
	}
}
