---
name: cel-patterns
description: "CEL expression patterns, context variables, built-in functions, and pitfalls for scafctl. Use when working on CEL evaluation, expression compilation, resolver expressions, or the celexp package."
---

# CEL Patterns in scafctl

## Context Variables

Available during expression evaluation (defined as `celexp.Var*` constants):

| Variable | Available In | Contains |
|----------|-------------|----------|
| `_` | All contexts | Root data -- all resolved values as a map |
| `__self` | Transform, Validate | Current resolver value being transformed/validated |
| `__item` | forEach loops | Current array element |
| `__index` | forEach loops | Current iteration index (0-based) |
| `__actions` | Action `when` clauses | Map of completed action results |
| `__cwd` | All contexts | Original working directory |
| `__execution` | Post-resolve | Resolver execution metadata |
| `__plan` | Pre-execution | Resolver topology/plan |

### Root Data (`_`)

The root data map contains all resolved values keyed by resolver name:

```cel
_.my_resolver              // Access resolved value
_.config.database.host     // Nested access (if resolver returns object)
has(_.optional_resolver)   // Check if resolver exists/resolved
```

## Built-in Functions

### String Operations (CEL strings v4)

```cel
_.name.upperAscii()           // "HELLO"
_.name.lowerAscii()           // "hello"
_.name.trim()                 // Strip whitespace (NOT trimSpace!)
_.name.replace("old", "new")  // Replace all occurrences
_.name.split("-")             // Split to list
_.name.substring(0, 5)        // Substring
_.name.contains("sub")        // Boolean check
_.name.startsWith("pre")      // Prefix check
_.name.endsWith("suf")        // Suffix check
_.name.charAt(0)              // Single character
_.name.indexOf("x")           // First index (-1 if not found)
_.name.reverse()              // Reverse string
_.name.join("-")              // Join list to string (on list type)
size(_.name)                  // String length
```

### List Operations (CEL lists v3)

```cel
_.items.filter(x, x > 10)            // Filter
_.items.map(x, x * 2)                // Map/transform
_.items.exists(x, x == "target")     // Any match
_.items.all(x, x > 0)               // All match
_.items.size()                        // Length
_.items.distinct()                    // Remove duplicates
_.items.flatten()                     // Flatten nested lists
_.items.reverse()                     // Reverse order
_.items.slice(0, 5)                   // Sub-list
_.items.sort()                        // Sort ascending
_.items.sortBy(x, x.name)            // Sort by field
```

### Map Operations

```cel
_.config.key                     // Direct access
has(_.config.key)                // Key exists check
_.config.size()                  // Number of keys
```

### Math (CEL math extension)

```cel
math.ceil(3.2)    // 4
math.floor(3.8)   // 3
math.round(3.5)   // 4
math.min(a, b)    // Minimum
math.max(a, b)    // Maximum
```

### Encoding

```cel
base64.encode(bytes("hello"))   // Base64 encode
base64.decode("aGVsbG8=")      // Base64 decode
```

### Bindings

```cel
cel.bind(x, _.long_expression, x + "_suffix")  // Bind intermediate values
```

## Custom scafctl Functions

Registered via `ext.Custom()` in `pkg/celexp/ext/`. Only functions listed here
are implemented. Check `ext.Custom()` for the authoritative list.

### Arrays (`pkg/celexp/ext/arrays/`)

```cel
arrays.strings.add(["a", "b"], "c")       // Append string -> ["a","b","c"]
arrays.strings.unique(["a", "b", "a"])     // Deduplicate -> ["a","b"]
arrays.groupBy([{"k":"a","v":1},{"k":"b","v":2},{"k":"a","v":3}], "k")
// -> {"a": [{"k":"a","v":1},{"k":"a","v":3}], "b": [{"k":"b","v":2}]}
// O(n) single-pass grouping -- avoids quadratic CEL comprehension cost
```

### Debug (`pkg/celexp/ext/debug/`)

```cel
debug.out(_.value)                         // Print value to writer (requires Writer)
debug.throw("something went wrong")       // Throw error for debugging
debug.sleep(1000)                          // Sleep N milliseconds (testing only)
```

Note: `debug.out` is NOT included in `ext.Custom()` / `ext.All()` because it
requires a Writer parameter. It is added separately via `debug.DebugOutFunc(w)`.

### Filepath (`pkg/celexp/ext/filepath/`)

```cel
filepath.dir("/path/to/file.txt")          // Directory: "/path/to"
filepath.normalize("path//to/../file")     // Clean path
filepath.exists("/path/to/file")           // File exists check
filepath.join("path", "to", "file")        // Join segments
```

### GUID (`pkg/celexp/ext/guid/`)

```cel
guid.new()                                 // UUID v4
```

### Map (`pkg/celexp/ext/map/`)

```cel
map.add(_.config, "key", "value")          // Add key-value pair
map.addFailIfExists(_.m, "k", "v")         // Add, error if key exists
map.addIfMissing(_.m, "k", "v")            // Add only if key missing
map.select(_.config, ["key1", "key2"])     // Pick keys
map.omit(_.config, ["secret"])             // Remove keys
map.merge(_.base, _.overrides)             // Merge maps (right wins)
map.recurse(_.nested, "children", expr)    // Recursive tree operation
map.fromEntries(_.list, "name")            // List of objects -> map keyed by field
```

### Marshalling (`pkg/celexp/ext/marshalling/`)

```cel
json.marshal(_.data)                       // Compact JSON string
json.marshalPretty(_.data)                 // Pretty-printed JSON
json.unmarshal(_.jsonString)               // Parse JSON string
yaml.marshal(_.data)                       // YAML string
yaml.unmarshal(_.yamlString)               // Parse YAML string
```

### Output (`pkg/celexp/ext/out/`)

```cel
out.nil()                                  // Returns null value
```

### Regex (`pkg/celexp/ext/regex/`)

```cel
regex.match("^[a-z]+$", _.name)            // Boolean match
regex.replace("[0-9]+", _.input, "X")      // Replace matches
regex.findAll("[a-z]+", _.input)           // List of matches
regex.split("[,;]", _.input)               // Split by pattern
```

### Sort (`pkg/celexp/ext/sort/`)

```cel
sort.objects(_.items, "name")              // Sort objects by field (ascending)
sort.objectsDescending(_.items, "score")   // Sort objects by field (descending)
```

### Strings (`pkg/celexp/ext/strings/`)

```cel
strings.clean(_.messy)                     // Normalize whitespace
strings.title(_.name)                      // Title Case
strings.repeat(_.char, 5)                  // Repeat string N times
```

### Time (`pkg/celexp/ext/time/`)

```cel
time.now()                                 // Current UTC timestamp (RFC3339)
time.nowFmt("2006-01-02")                  // Current time with Go layout format
```

## Common Patterns

### Conditional Values (Ternary)

```cel
_.env == "prod" ? "https://api.example.com" : "http://localhost:8080"
```

### Default Values

```cel
has(_.optional) ? _.optional : "default"
```

### String Building

```cel
_.prefix + "-" + _.name + "-" + _.suffix
```

### List Transformation Pipeline

```cel
_.items.filter(x, x.enabled).map(x, x.name).sort().join(", ")
```

### Type Coercion

```cel
int(_.port)           // String to int
string(_.count)       // Int to string
double(_.value)       // To float
```

### Nested Has Checks

```cel
has(_.config) && has(_.config.database) && has(_.config.database.host)
```

## Design Patterns from Solution Authors

- **Prefer `when` clauses over ternaries** for conditional resolvers -- cleaner DAG
- **Use `transform` phase** for reshaping instead of complex inline CEL
- **Keep resolvers small and focused** -- one value per resolver
- **Use `cel.bind()`** to avoid repeating long expressions

## Pitfalls

| Pitfall | Wrong | Right |
|---------|-------|-------|
| Trim function | `trimSpace()` | `trim()` |
| Null check | `_.x != null` | `has(_.x)` |
| Type mismatch | `_.port + 1` (if port is string) | `int(_.port) + 1` |
| Empty string | `_.name == ""` | `size(_.name) == 0` or `_.name == ""` (both work) |
| Missing field | `_.missing.field` (runtime error) | `has(_.missing) ? _.missing.field : default` |
| List on non-list | `_.value.filter(...)` (if scalar) | Check type first or ensure resolver type is `array` |

## Key Packages

- `pkg/celexp/`: Expression type, Compile/Eval, EvaluateExpression convenience
- `pkg/celexp/env/`: CEL environment setup, extension loading, caching
- `pkg/celexp/ext/`: Custom function registration (regex, arrays, map, guid, time, sort, out)
- `pkg/provider/builtin/celprovider/`: CEL provider for transform phase

## Cost Limits

CEL expressions have a runtime cost limit (default: 1,000,000) to prevent
runaway computations. The limit is enforced by cel-go's cost tracker.

### Error Diagnostics

When a cost limit is exceeded, the error includes actual cost, limit, and expression:

```
CEL expression cost limit exceeded (actual cost: 1500000, limit: 1000000)
for expression "[large].map(x, x.filter(...))": runtime cost limit exceeded
```

### Per-Solution Override

Solutions can lower the cost limit via `spec.options.cel.costLimit`:

```yaml
spec:
  options:
    cel:
      costLimit: 500000  # Tighter limit for this solution
```

The effective limit is `min(solutionLimit, globalLimit)` -- solutions cannot
raise the limit above the operator-configured global default.

### Avoiding High-Cost Patterns

| Pattern | Cost | Alternative |
|---------|------|-------------|
| `list.filter(x, list2.exists(y, ...))` | O(n*m) | Use `arrays.groupBy` + lookup |
| `list.map(x, list.filter(...))` | O(n^2) | Pre-group with `arrays.groupBy` |
| Nested `map` + `filter` | Multiplicative | Single-pass with custom extension |
