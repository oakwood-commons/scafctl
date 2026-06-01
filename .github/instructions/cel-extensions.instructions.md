---
description: "CEL extension function rules for scafctl. Covers file layout, ExtFunction struct, naming, types, conversion, error handling, registration, testing, and documentation. Use when creating or editing CEL extension functions."
applyTo: "pkg/celexp/ext/**/*.go"
---

# CEL Extension Functions

CEL extensions are custom functions that extend the CEL expression language
with scafctl-specific capabilities. They are registered at startup and
available in all CEL evaluation contexts (resolvers, conditions, transforms).

## File Layout

```
pkg/celexp/ext/<namespace>/
    <func>.go           # Function implementation
    <func>_test.go      # Tests + benchmarks
```

One file per function (or per closely related group). The namespace directory
matches the CEL function prefix (e.g., `arrays/` for `arrays.*` functions).

## ExtFunction Struct

Every function returns `celexp.ExtFunction` from a constructor:

```go
func GroupByFunc() celexp.ExtFunction {
    funcName := "arrays.groupBy"
    return celexp.ExtFunction{
        Name:          funcName,
        Signature:     "arrays.groupBy(list<map<string,dyn>>, string) -> map<string, list<map<string,dyn>>>",
        Description:   "Groups a list of objects by a field value. Use arrays.groupBy(list, fieldName) to create a map of grouped items",
        FunctionNames: []string{funcName},
        Custom:        true,
        Examples: []celexp.Example{
            {
                Description: "Group items by category",
                Expression:  `arrays.groupBy([{"name": "a", "cat": "x"}, {"name": "b", "cat": "x"}], "cat")`,
            },
        },
        EnvOptions: []cel.EnvOption{
            cel.Function(funcName,
                cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
                    []*cel.Type{/* input types */},
                    /* return type */,
                    cel.BinaryBinding(func(arg1, arg2 ref.Val) ref.Val {
                        // implementation
                    }),
                ),
            ),
        },
    }
}
```

## Naming Conventions

| Convention | Rule | Example |
|-----------|------|---------|
| Function name | `<namespace>.<camelCase>` | `arrays.groupBy`, `map.fromEntries` |
| Constructor | `<PascalCase>Func()` | `GroupByFunc()`, `FromEntriesFunc()` |
| Overload ID | `strings.ReplaceAll(funcName, ".", "_")` | `arrays_groupBy` |
| Custom flag | Always `true` for scafctl functions | `Custom: true` |

## CEL Type Patterns

Common type signatures for list/map operations:

```go
// Input: list of objects
cel.ListType(cel.MapType(cel.StringType, cel.DynType))

// Input: list of strings
cel.ListType(cel.StringType)

// Output: map with string keys
cel.MapType(cel.StringType, cel.DynType)

// Output: map with list values
cel.MapType(cel.StringType, cel.ListType(cel.MapType(cel.StringType, cel.DynType)))
```

## Conversion Helpers

Use helpers from `pkg/celexp/conversion/` for Go <-> CEL type conversion:

```go
// CEL list -> Go slice of maps
items, err := conversion.ListToObjectSlice(listVal)

// CEL list -> Go slice of strings
strs, err := conversion.ListToStringSlice(listVal)

// Go value -> CEL value
return types.DefaultTypeAdapter.NativeToValue(result)

// Go value -> CEL ref.Val (deep conversion)
return conversion.GoToCelValue(result)
```

## Binding Types

| Args | Binding | Use |
|------|---------|-----|
| 1 | `cel.UnaryBinding(func(ref.Val) ref.Val)` | Single argument |
| 2 | `cel.BinaryBinding(func(ref.Val, ref.Val) ref.Val)` | Two arguments |
| 3+ | `cel.FunctionBinding(func(...ref.Val) ref.Val)` | Variadic |

## Error Handling

Return errors via `types.NewErr` with the function name prefix:

```go
return types.NewErr("arrays.groupBy: expected string key, got %s", val.Type())
return types.NewErr("arrays.groupBy: %s", err.Error())
```

Do NOT panic or return Go errors. CEL functions must return `ref.Val`.

## Registration

Add the function to `Custom()` in `pkg/celexp/ext/ext.go` under the
appropriate namespace comment:

```go
func Custom() celexp.ExtFunctionList {
    funcs := celexp.ExtFunctionList{
        // Arrays functions
        arrays.StringAddFunc(),
        arrays.StringsUniqueFunc(),
        arrays.GroupByFunc(),          // <-- add here
        // ...
    }
```

## Testing

- Table-driven tests covering: happy path, empty input, error cases, edge cases
- Benchmark test for performance-critical functions
- Test CEL evaluation end-to-end (compile + eval), not just the Go function

```go
func TestGroupByFunc(t *testing.T) {
    fn := arrays.GroupByFunc()
    env, _ := cel.NewEnv(fn.EnvOptions...)
    // compile and eval with test data
}

func BenchmarkGroupByFunc(b *testing.B) {
    // setup once, eval in loop
}
```

## Documentation

After adding a function, update the cel-patterns skill:
`.github/skills/cel-patterns/SKILL.md` -- add the function under the correct
namespace heading in "Custom scafctl Functions".

Only document implemented functions. Never add planned/aspirational functions
to the skill file.
