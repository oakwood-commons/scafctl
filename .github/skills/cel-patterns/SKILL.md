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

Registered via `ext.Custom()` in `pkg/celexp/ext/`:

### Regex

```cel
regex.match("^[a-z]+$", _.name)           // Boolean match
regex.replace("[0-9]+", _.input, "X")     // Replace matches
regex.findAll("[a-z]+", _.input)          // List of matches
regex.split("[,;]", _.input)              // Split by pattern
```

### Arrays

```cel
arrays.groupBy(_.items, "category")       // Group by field -> map
arrays.from(5, i, i * 2)                 // Generate: [0,2,4,6,8]
arrays.partition(_.items, 3)              // Chunk into groups of 3
arrays.window(_.items, 2)                 // Sliding window of size 2
```

### Map

```cel
map.toMap(_.pairs, "key", "value")        // List of objects -> map
map.keys(_.config)                        // List of keys
map.values(_.config)                      // List of values
```

### GUID

```cel
guid.new()                                // UUID v4
```

### Time

```cel
time.now()                                // Current timestamp
time.toDate("2024-01-15T00:00:00Z")      // Parse ISO date
time.formatDate(_.timestamp, "2006-01-02") // Format date
time.parseDate("2024-01-15", "2006-01-02") // Parse with layout
```

### Sort

```cel
sort.sortBy(_.items, "name")              // Sort objects by field
```

### Output

```cel
out.print("Processing: " + _.name)        // Print to terminal
debug.debug("Debug value: " + string(_.x)) // Debug output
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
