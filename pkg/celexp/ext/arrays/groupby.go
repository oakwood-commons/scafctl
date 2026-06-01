// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package arrays

import (
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/conversion"
)

// GroupByFunc returns a CEL extension function that groups a list of objects by a specified field.
// The function takes a list of maps and a field name, returning a map where keys are the distinct
// field values and values are lists of objects with that field value. This runs in O(n) time,
// avoiding the O(n^2) cost of CEL comprehension-based grouping patterns.
func GroupByFunc() celexp.ExtFunction {
	funcName := "arrays.groupBy"
	return celexp.ExtFunction{
		Name:          funcName,
		Signature:     "arrays.groupBy(list<map<string,dyn>>, string) -> map<string, list<map<string,dyn>>>",
		Description:   "Groups a list of objects by a field value, returning a map of grouped items. Use arrays.groupBy(list, fieldName) to efficiently group objects without quadratic CEL cost",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Group items by category",
				Expression:  `arrays.groupBy([{"name": "a", "cat": "x"}, {"name": "b", "cat": "x"}, {"name": "c", "cat": "y"}], "cat")`,
			},
			{
				Description: "Group environments by region",
				Expression:  `arrays.groupBy([{"env": "dev", "region": "us"}, {"env": "prod", "region": "eu"}, {"env": "staging", "region": "us"}], "region")`,
			},
			{
				Description: "Empty list returns empty map",
				Expression:  `arrays.groupBy([], "key")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{
						cel.ListType(cel.MapType(cel.StringType, cel.DynType)),
						cel.StringType,
					},
					cel.MapType(cel.StringType, cel.ListType(cel.MapType(cel.StringType, cel.DynType))),
					cel.BinaryBinding(func(listVal, keyFieldVal ref.Val) ref.Val {
						keyField, ok := keyFieldVal.Value().(string)
						if !ok {
							return types.NewErr("arrays.groupBy: expected string keyField, got %s", keyFieldVal.Type())
						}

						items, err := conversion.ListToObjectSlice(listVal)
						if err != nil {
							return types.NewErr("arrays.groupBy: %s", err.Error())
						}

						// Single-pass O(n) grouping
						groups := make(map[string][]map[string]any)
						for _, item := range items {
							keyVal, exists := item[keyField]
							if !exists {
								return types.NewErr("arrays.groupBy: object missing key field %q", keyField)
							}

							key, ok := keyVal.(string)
							if !ok {
								return types.NewErr("arrays.groupBy: key field %q must be a string, got %T", keyField, keyVal)
							}

							groups[key] = append(groups[key], item)
						}

						// Convert to map[string]any for CEL compatibility
						result := make(map[string]any, len(groups))
						for k, v := range groups {
							result[k] = v
						}

						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}
