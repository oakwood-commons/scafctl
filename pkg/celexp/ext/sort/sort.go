// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celsort

import (
	"sort"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/conversion"
	"github.com/oakwood-commons/scafctl/pkg/compare"
)

// sortObjects is a helper function that sorts objects by a property in the specified order.
// If descending is true, sorts in descending order; otherwise, sorts in ascending order.
func sortObjects(listVal ref.Val, propName string, descending bool, funcName string) ref.Val {
	// Convert list to slice of objects
	objects, err := conversion.ListToObjectSlice(listVal)
	if err != nil {
		return types.NewErr("%s: %s", funcName, err.Error())
	}

	// Create a copy to avoid modifying the original
	sortedObjects := make([]map[string]any, len(objects))
	copy(sortedObjects, objects)

	// Sort the objects
	sort.SliceStable(sortedObjects, func(i, j int) bool {
		valI, hasI := sortedObjects[i][propName]
		valJ, hasJ := sortedObjects[j][propName]

		// Items without the property go to the end
		if !hasI && !hasJ {
			return false
		}
		if !hasI {
			return false
		}
		if !hasJ {
			return true
		}

		// Compare values based on type
		cmp := compare.Values(valI, valJ)
		if descending {
			return cmp > 0
		}
		return cmp < 0
	})

	// Convert back to CEL value
	return conversion.GoToCelValue(sortedObjects)
}

// ObjectsFunc returns a CEL function that sorts an array of objects by a specified property in ascending order.
// The function takes a list of maps and a property name, returning the sorted list.
//
// Example usage:
//
//	sort.objects([{"name": "Charlie", "age": 30}, {"name": "Alice", "age": 25}], "name")
//	// Returns: [{"name": "Alice", "age": 25}, {"name": "Charlie", "age": 30}]
func ObjectsFunc() celexp.ExtFunction {
	funcName := "sort.objects"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Sorts an array of objects by a specified property in ascending order",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Sort objects by name property",
				Expression:  `sort.objects([{"name": "Charlie", "age": 30}, {"name": "Alice", "age": 25}], "name")`,
			},
			{
				Description: "Sort objects by numeric property",
				Expression:  `sort.objects([{"id": 3, "value": "c"}, {"id": 1, "value": "a"}, {"id": 2, "value": "b"}], "id")`,
			},
			{
				Description: "Sort with missing properties (items without property go last)",
				Expression:  `sort.objects([{"name": "Bob"}, {"name": "Alice", "priority": 1}, {"name": "Charlie"}], "priority")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload("sort_objects",
					[]*cel.Type{cel.ListType(cel.MapType(cel.StringType, cel.DynType)), cel.StringType},
					cel.ListType(cel.MapType(cel.StringType, cel.DynType)),
					cel.BinaryBinding(func(listVal, propVal ref.Val) ref.Val {
						// Convert property name to string
						propName, ok := propVal.Value().(string)
						if !ok {
							return types.NewErr("sort.objects: property name must be a string")
						}

						return sortObjects(listVal, propName, false, "sort.objects")
					}),
				),
			),
		},
	}
}

// ObjectsDescendingFunc returns a CEL function that sorts an array of objects by a specified property in descending order.
// The function takes a list of maps and a property name, returning the sorted list.
//
// Example usage:
//
//	sort.objectsDescending([{"name": "Charlie", "age": 30}, {"name": "Alice", "age": 25}], "name")
//	// Returns: [{"name": "Charlie", "age": 30}, {"name": "Alice", "age": 25}]
func ObjectsDescendingFunc() celexp.ExtFunction {
	funcName := "sort.objectsDescending"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Sorts an array of objects by a specified property in descending order",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Sort objects by name property in descending order",
				Expression:  `sort.objectsDescending([{"name": "Alice", "age": 25}, {"name": "Charlie", "age": 30}], "name")`,
			},
			{
				Description: "Sort objects by numeric property in descending order",
				Expression:  `sort.objectsDescending([{"id": 1, "value": "a"}, {"id": 3, "value": "c"}, {"id": 2, "value": "b"}], "id")`,
			},
			{
				Description: "Sort with missing properties (items without property go last)",
				Expression:  `sort.objectsDescending([{"name": "Bob"}, {"name": "Alice", "priority": 1}, {"name": "Charlie"}], "priority")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload("sort_objectsDesc",
					[]*cel.Type{cel.ListType(cel.MapType(cel.StringType, cel.DynType)), cel.StringType},
					cel.ListType(cel.MapType(cel.StringType, cel.DynType)),
					cel.BinaryBinding(func(listVal, propVal ref.Val) ref.Val {
						// Convert property name to string
						propName, ok := propVal.Value().(string)
						if !ok {
							return types.NewErr("sort.objectsDescending: property name must be a string")
						}

						return sortObjects(listVal, propName, true, "sort.objectsDescending")
					}),
				),
			),
		},
	}
}
