package arrays

import (
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	pkgarrays "github.com/kcloutie/scafctl/pkg/arrays"
	"github.com/kcloutie/scafctl/pkg/celexp"
	"github.com/kcloutie/scafctl/pkg/celexp/conversion"
)

func StringAddFunc() celexp.ExtFunction {
	funcName := "arrays.strings.add"
	return celexp.ExtFunction{
		Name:        funcName,
		Description: "Appends a string to a list of strings and returns the new list. Use arrays.strings.add(list, 'value') to add a single string to the end of the list",
		FunctionNames: []string{
			funcName,
		},
		Custom: true,
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.ListType(cel.StringType), cel.StringType},
					cel.ListType(cel.StringType),
					cel.BinaryBinding(func(arrayObj, newValue ref.Val) ref.Val {
						// Type check the string value
						value, ok := newValue.Value().(string)
						if !ok {
							return types.NewErr("arrays.strings.add: expected string argument, got %s", newValue.Type())
						}

						// Convert list to string slice using conversion helper
						result, err := conversion.ListToStringSlice(arrayObj)
						if err != nil {
							return types.NewErr("arrays.strings.add: %s", err.Error())
						}

						// Add the new string
						result = append(result, value)

						// Convert back to CEL list
						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}

func StringsUniqueFunc() celexp.ExtFunction {
	funcName := "arrays.strings.unique"
	return celexp.ExtFunction{
		Name:        funcName,
		Description: "Returns a new list containing only unique strings from the input list, removing all duplicates while preserving the original order of first occurrence. Use arrays.strings.unique(list) to deduplicate a list of strings",
		FunctionNames: []string{
			funcName,
		},
		Custom: true,
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.ListType(cel.StringType)},
					cel.ListType(cel.StringType),
					cel.UnaryBinding(func(arrayObj ref.Val) ref.Val {
						// Convert list to string slice using conversion helper
						list, err := conversion.ListToStringSlice(arrayObj)
						if err != nil {
							return types.NewErr("arrays.strings.unique: %s", err.Error())
						}

						// Get unique strings using the arrays package utility
						result := pkgarrays.UniqueStrings(list)

						// Convert back to CEL list
						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}
