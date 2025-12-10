package celmap

import (
	"maps"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/kcloutie/scafctl/pkg/celexp"
	"github.com/kcloutie/scafctl/pkg/celexp/conversion"
)

// AddFunc adds a key-value pair to a map and returns a new map.
func AddFunc() celexp.ExtFunction {
	funcName := "map.add"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Adds a key-value pair to a map and returns a new map with the added entry. The original map is not modified. Use map.add(map, key, value) to add an entry to a map",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Add a string value to a map",
				Expression:  `map.add({"name": "John"}, "age", "30")`,
			},
			{
				Description: "Add a number value to a map",
				Expression:  `map.add({"x": 10}, "y", 20)`,
			},
			{
				Description: "Chain multiple add operations",
				Expression:  `map.add(map.add({"a": 1}, "b", 2), "c", 3)`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType, cel.DynType},
					cel.MapType(cel.StringType, cel.DynType),
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						// Get the map argument
						mapVal := args[0]

						// Get the key argument
						keyVal := args[1]
						key, ok := keyVal.Value().(string)
						if !ok {
							return types.NewErr("map.add: expected string key, got %s", keyVal.Type())
						}

						// Get the value argument (can be any type)
						value := args[2]

						// Convert the CEL map to Go map using the conversion helper
						mapObj, err := conversion.ToObject(mapVal)
						if err != nil {
							return types.NewErr("map.add: %s", err.Error())
						}

						// Create a new map with the existing entries plus the new one
						result := make(map[string]any, len(mapObj)+1)
						maps.Copy(result, mapObj)
						result[key] = value.Value()

						// Convert back to CEL value
						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}

// AddFailIfExistsFunc adds a key-value pair to a map but fails if the key already exists.
func AddFailIfExistsFunc() celexp.ExtFunction {
	funcName := "map.addFailIfExists"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Adds a key-value pair to a map and returns a new map with the added entry. Throws an error if the key already exists. Use map.addFailIfExists(map, key, value) to safely add entries without overwriting",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Add a new key to a map",
				Expression:  `map.addFailIfExists({"name": "John"}, "age", 30)`,
			},
			{
				Description: "Fails when key already exists",
				Expression:  `map.addFailIfExists({"name": "John"}, "name", "Jane")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType, cel.DynType},
					cel.MapType(cel.StringType, cel.DynType),
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						mapVal := args[0]
						keyVal := args[1]
						key, ok := keyVal.Value().(string)
						if !ok {
							return types.NewErr("map.addFailIfExists: expected string key, got %s", keyVal.Type())
						}
						value := args[2]

						mapObj, err := conversion.ToObject(mapVal)
						if err != nil {
							return types.NewErr("map.addFailIfExists: %s", err.Error())
						}

						// Check if key already exists
						if _, exists := mapObj[key]; exists {
							return types.NewErr("map.addFailIfExists: key '%s' already exists", key)
						}

						result := make(map[string]any, len(mapObj)+1)
						maps.Copy(result, mapObj)
						result[key] = value.Value()

						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}

// AddIfMissingFunc adds a key-value pair to a map only if the key doesn't exist.
func AddIfMissingFunc() celexp.ExtFunction {
	funcName := "map.addIfMissing"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Adds a key-value pair to a map only if the key doesn't already exist. Returns a new map. Use map.addIfMissing(map, key, value) to add entries without overwriting existing values",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Add a new key to a map",
				Expression:  `map.addIfMissing({"name": "John"}, "age", 30)`,
			},
			{
				Description: "Key already exists, value not overwritten",
				Expression:  `map.addIfMissing({"name": "John"}, "name", "Jane")`,
			},
			{
				Description: "Set default values that won't override existing ones",
				Expression:  `map.addIfMissing(map.addIfMissing({"name": "John"}, "name", "Default"), "age", 25)`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType, cel.DynType},
					cel.MapType(cel.StringType, cel.DynType),
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						mapVal := args[0]
						keyVal := args[1]
						key, ok := keyVal.Value().(string)
						if !ok {
							return types.NewErr("map.addIfMissing: expected string key, got %s", keyVal.Type())
						}
						value := args[2]

						mapObj, err := conversion.ToObject(mapVal)
						if err != nil {
							return types.NewErr("map.addIfMissing: %s", err.Error())
						}

						result := make(map[string]any, len(mapObj)+1)
						maps.Copy(result, mapObj)

						// Only add if key doesn't exist
						if _, exists := mapObj[key]; !exists {
							result[key] = value.Value()
						}

						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}

// SelectFunc returns a new map containing only the specified keys.
func SelectFunc() celexp.ExtFunction {
	funcName := "map.select"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Returns a new map containing only the specified keys from the input map. Keys that don't exist in the input map are ignored. Use map.select(map, keys) to filter a map to specific keys",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Select specific keys from a map",
				Expression:  `map.select({"name": "John", "age": 30, "city": "NYC"}, ["name", "city"])`,
			},
			{
				Description: "Select with non-existent keys",
				Expression:  `map.select({"name": "John", "age": 30}, ["name", "country"])`,
			},
			{
				Description: "Select all keys",
				Expression:  `map.select({"a": 1, "b": 2}, ["a", "b"])`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.ListType(cel.StringType)},
					cel.MapType(cel.StringType, cel.DynType),
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						mapVal := args[0]
						keysVal := args[1]

						mapObj, err := conversion.ToObject(mapVal)
						if err != nil {
							return types.NewErr("map.select: %s", err.Error())
						}

						keys, err := conversion.ListToStringSlice(keysVal)
						if err != nil {
							return types.NewErr("map.select: %s", err.Error())
						}

						result := make(map[string]any)
						for _, key := range keys {
							if val, exists := mapObj[key]; exists {
								result[key] = val
							}
						}

						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}

// OmitFunc returns a new map with the specified keys removed.
func OmitFunc() celexp.ExtFunction {
	funcName := "map.omit"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Returns a new map with the specified keys removed from the input map. Keys that don't exist in the input map are ignored. Use map.omit(map, keys) to exclude specific keys from a map",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Remove specific keys from a map",
				Expression:  `map.omit({"name": "John", "age": 30, "city": "NYC"}, ["age"])`,
			},
			{
				Description: "Remove multiple keys",
				Expression:  `map.omit({"a": 1, "b": 2, "c": 3, "d": 4}, ["b", "d"])`,
			},
			{
				Description: "Remove non-existent keys has no effect",
				Expression:  `map.omit({"name": "John"}, ["age", "city"])`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.ListType(cel.StringType)},
					cel.MapType(cel.StringType, cel.DynType),
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						mapVal := args[0]
						keysVal := args[1]

						mapObj, err := conversion.ToObject(mapVal)
						if err != nil {
							return types.NewErr("map.omit: %s", err.Error())
						}

						keys, err := conversion.ListToStringSlice(keysVal)
						if err != nil {
							return types.NewErr("map.omit: %s", err.Error())
						}

						// Create set of keys to omit
						omitKeys := make(map[string]bool, len(keys))
						for _, key := range keys {
							omitKeys[key] = true
						}

						result := make(map[string]any)
						for k, v := range mapObj {
							if !omitKeys[k] {
								result[k] = v
							}
						}

						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}

// MergeFunc merges two maps together with the second map's values taking precedence.
func MergeFunc() celexp.ExtFunction {
	funcName := "map.merge"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Merges two maps together and returns a new map. If keys conflict, the second map's values take precedence. Use map.merge(map1, map2) to combine maps",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Merge two maps",
				Expression:  `map.merge({"name": "John", "age": 30}, {"city": "NYC", "country": "USA"})`,
			},
			{
				Description: "Second map overwrites conflicting keys",
				Expression:  `map.merge({"name": "John", "age": 30}, {"age": 31, "city": "NYC"})`,
			},
			{
				Description: "Chain multiple merges",
				Expression:  `map.merge(map.merge({"a": 1}, {"b": 2}), {"c": 3})`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.MapType(cel.StringType, cel.DynType)},
					cel.MapType(cel.StringType, cel.DynType),
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						map1Val := args[0]
						map2Val := args[1]

						map1Obj, err := conversion.ToObject(map1Val)
						if err != nil {
							return types.NewErr("map.merge: first map: %s", err.Error())
						}

						map2Obj, err := conversion.ToObject(map2Val)
						if err != nil {
							return types.NewErr("map.merge: second map: %s", err.Error())
						}

						result := make(map[string]any, len(map1Obj)+len(map2Obj))
						maps.Copy(result, map1Obj)
						maps.Copy(result, map2Obj) // Second map overwrites conflicts

						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}

// RecurseFunc recursively resolves dependencies between objects.
func RecurseFunc() celexp.ExtFunction {
	funcName := "map.recurse"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Recursively resolves dependencies between objects in a list. Returns a unique list of objects with all transitive dependencies included. Use map.recurse(allObjects, startIds, idProperty, depsProperty) to compute recursive dependencies",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Resolve dependencies for packages",
				Expression:  `map.recurse([{"id": "a", "deps": ["b"]}, {"id": "b", "deps": ["c"]}, {"id": "c", "deps": []}], ["a"], "id", "deps")`,
			},
			{
				Description: "Resolve module dependencies",
				Expression:  `map.recurse([{"name": "app", "requires": ["lib1"]}, {"name": "lib1", "requires": ["lib2"]}, {"name": "lib2", "requires": []}], ["app"], "name", "requires")`,
			},
			{
				Description: "Handle circular dependencies gracefully",
				Expression:  `map.recurse([{"id": "a", "deps": ["b"]}, {"id": "b", "deps": ["a"]}], ["a"], "id", "deps")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{
						cel.ListType(cel.MapType(cel.StringType, cel.DynType)),
						cel.ListType(cel.StringType),
						cel.StringType,
						cel.StringType,
					},
					cel.ListType(cel.MapType(cel.StringType, cel.DynType)),
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						allObjectsVal := args[0]
						startIDsVal := args[1]
						idPropertyVal := args[2]
						depsPropertyVal := args[3]

						// Get the property names
						idProperty, ok := idPropertyVal.Value().(string)
						if !ok {
							return types.NewErr("map.recurse: expected string for idProperty, got %s", idPropertyVal.Type())
						}

						depsProperty, ok := depsPropertyVal.Value().(string)
						if !ok {
							return types.NewErr("map.recurse: expected string for depsProperty, got %s", depsPropertyVal.Type())
						}

						// Convert all objects to Go slice
						allObjects, err := conversion.ListToObjectSlice(allObjectsVal)
						if err != nil {
							return types.NewErr("map.recurse: allObjects: %s", err.Error())
						}

						// Convert start IDs to Go string slice
						startIDs, err := conversion.ListToStringSlice(startIDsVal)
						if err != nil {
							return types.NewErr("map.recurse: startIds: %s", err.Error())
						}

						// Create a map for quick lookup of objects by ID
						objectsByID := make(map[string]map[string]any)
						for _, obj := range allObjects {
							id, ok := obj[idProperty].(string)
							if !ok {
								return types.NewErr("map.recurse: object missing string id property '%s'", idProperty)
							}
							objectsByID[id] = obj
						}

						// Track visited objects to avoid infinite loops and duplicates
						visited := make(map[string]bool)
						result := make([]map[string]any, 0)

						// Recursive function to resolve dependencies
						var resolveDeps func(obj map[string]any)
						resolveDeps = func(obj map[string]any) {
							// Get the ID of the current object
							id, ok := obj[idProperty].(string)
							if !ok {
								return
							}

							// Skip if already visited
							if visited[id] {
								return
							}
							visited[id] = true

							// Add this object to the result
							result = append(result, obj)

							// Get dependencies
							depsVal, hasDeps := obj[depsProperty]
							if !hasDeps {
								return
							}

							// Handle different dependency formats
							switch deps := depsVal.(type) {
							case []string:
								// Direct string slice
								for _, depID := range deps {
									if depObj, exists := objectsByID[depID]; exists {
										resolveDeps(depObj)
									}
								}
							case []any:
								// Interface slice (common from CEL)
								for _, depVal := range deps {
									if depID, ok := depVal.(string); ok {
										if depObj, exists := objectsByID[depID]; exists {
											resolveDeps(depObj)
										}
									}
								}
							case []ref.Val:
								// CEL ref.Val slice
								for _, depVal := range deps {
									if depID, ok := depVal.Value().(string); ok {
										if depObj, exists := objectsByID[depID]; exists {
											resolveDeps(depObj)
										}
									}
								}
							}
						}

						// Start recursion from each start ID
						for _, startID := range startIDs {
							if startObj, exists := objectsByID[startID]; exists {
								resolveDeps(startObj)
							}
						}

						// Convert back to CEL value
						return types.DefaultTypeAdapter.NativeToValue(result)
					}),
				),
			),
		},
	}
}
