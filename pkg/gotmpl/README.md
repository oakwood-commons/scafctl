# gotmpl

The `gotmpl` package provides a service-oriented wrapper around Go's standard `text/template` package with enhanced features including custom delimiters, string replacements, template function management, and template data reference extraction.

## Features

- **Service Pattern**: Reusable service instances with default configurations
- **Template Caching**: Thread-safe LRU cache for compiled templates with SHA-256 content hashing
- **Custom Delimiters**: Support for any delimiter pair (e.g., `[[`, `]]` or `{%`, `%}`)
- **String Replacements**: Protect literal strings from template parsing with UUID placeholders
- **Custom Functions**: Add custom template functions with flexible override capabilities
- **Missing Key Handling**: Configurable behavior for missing map keys (default, zero, error)
- **Context Integration**: Full context support for logging and cancellation
- **Structured Logging**: Detailed logging at multiple verbosity levels
- **Reference Extraction**: Extract data field references from templates for dependency analysis
- **Sprig Functions**: 100+ built-in utility functions via [Masterminds/sprig](https://masterminds.github.io/sprig/)
- **Extension System**: Pluggable architecture for custom Go template functions (see `ext/` sub-packages)
- **Cache Metrics**: Per-template hit tracking with configurable limits

## Installation

```go
import "github.com/oakwood-commons/scafctl/pkg/gotmpl"
```

## Quick Start

### Basic Template Execution

```go
ctx := context.Background()

result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
    Name:    "greeting",
    Content: "Hello, {{.Name}}!",
    Data:    map[string]string{"Name": "World"},
})
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Output) // Output: Hello, World!
```

### Using the Service Pattern

For repeated template execution with shared default functions:

```go
// Create a service with default functions
svc := gotmpl.NewService(template.FuncMap{
    "upper": strings.ToUpper,
    "lower": strings.ToLower,
})

// Execute multiple templates
result, err := svc.Execute(ctx, gotmpl.TemplateOptions{
    Name:    "formatted",
    Content: "{{upper .Text}}",
    Data:    map[string]string{"Text": "hello"},
})
```

## API Reference

### Types

#### TemplateOptions

Configuration for template execution:

```go
type TemplateOptions struct {
    // Content is the template content as a string (required)
    Content string

    // Name is the reference name/identifier for the template
    // Used in logging and error messages (optional, defaults to "unnamed-template")
    Name string

    // Data is the data source passed to the template during execution
    Data any

    // LeftDelim sets the left action delimiter (default: "{{")
    LeftDelim string

    // RightDelim sets the right action delimiter (default: "}}")
    RightDelim string

    // Replacements is a slice of strings to replace before template execution
    // The key is replaced with a UUID placeholder, then restored after execution
    Replacements []Replacement

    // Funcs is a map of custom template functions
    Funcs template.FuncMap

    // MissingKey controls the behavior when a map key is missing
    // Options: MissingKeyDefault, MissingKeyZero, MissingKeyError
    MissingKey MissingKeyOption

    // DisableBuiltinFuncs disables the built-in template functions
    DisableBuiltinFuncs bool
}
```

#### MissingKeyOption

Defines behavior for missing map keys:

```go
type MissingKeyOption string

const (
    // MissingKeyDefault prints "<no value>" for missing keys (default)
    MissingKeyDefault MissingKeyOption = "default"

    // MissingKeyZero returns the zero value for the type
    MissingKeyZero MissingKeyOption = "zero"

    // MissingKeyError stops execution with an error
    MissingKeyError MissingKeyOption = "error"
)
```

#### ExecuteResult

Result of template execution:

```go
type ExecuteResult struct {
    // Output is the rendered template content
    Output string

    // TemplateName is the name/identifier of the template
    TemplateName string

    // ReplacementsMade is the number of replacements that were applied
    ReplacementsMade int
}
```

#### TemplateReference

Represents a data field reference found in a template:

```go
type TemplateReference struct {
    // Path is the dot-notation path to the field (e.g., ".User.Name")
    Path string

    // Position is the location in the template (e.g., "line:col")
    Position string
}
```

### Functions

#### Execute

Convenience function for one-off template execution:

```go
func Execute(ctx context.Context, opts TemplateOptions) (*ExecuteResult, error)
```

#### NewService

Creates a new template service with optional default functions:

```go
func NewService(defaultFuncs template.FuncMap) *Service
```

#### Service.Execute

Renders a template with the provided options:

```go
func (s *Service) Execute(ctx context.Context, opts TemplateOptions) (*ExecuteResult, error)
```

#### Service.GetReferences

Extracts data field references from a template:

```go
func (s *Service) GetReferences(ctx context.Context, opts TemplateOptions) ([]TemplateReference, error)
```

#### GetGoTemplateReferences

Convenience function for extracting references without creating a service:

```go
func GetGoTemplateReferences(content, leftDelim, rightDelim string) ([]string, error)
```

## Usage Examples

### Custom Delimiters

Use Jinja2-style delimiters:

```go
svc := gotmpl.NewService(nil)

result, err := svc.Execute(ctx, gotmpl.TemplateOptions{
    Name:      "jinja-style",
    Content:   "{% for item in .Items %}{{ item }}{% endfor %}",
    LeftDelim: "{%",
    RightDelim: "%}",
    Data: map[string][]string{
        "Items": {"apple", "banana", "cherry"},
    },
})
```

### Custom Functions

Add and override template functions:

```go
svc := gotmpl.NewService(template.FuncMap{
    "default": func(def, val string) string {
        if val == "" {
            return def
        }
        return val
    },
})

result, err := svc.Execute(ctx, gotmpl.TemplateOptions{
    Name:    "with-functions",
    Content: "{{default \"N/A\" .Value}}",
    Data:    map[string]string{"Value": ""},
    Funcs: template.FuncMap{
        "upper": strings.ToUpper, // Per-execution function
    },
})
```

### String Replacements

Protect literal template syntax from parsing:

```go
result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
    Name:    "with-replacements",
    Content: "The syntax {{.Name}} uses REPLACE_ME delimiters",
    Data:    map[string]string{"Name": "Go"},
    Replacements: []gotmpl.Replacement{
        {Find: "REPLACE_ME", Replace: "{{...}}"},
    },
})
// Output: The syntax Go uses {{...}} delimiters
```

### Missing Key Handling

Configure behavior for undefined map keys:

```go
// Error on missing keys
result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
    Name:       "strict",
    Content:    "{{.Missing}}",
    Data:       map[string]string{},
    MissingKey: gotmpl.MissingKeyError,
})
// Returns error: "template: strict:1:2: executing \"strict\" at <.Missing>: map has no entry for key \"Missing\""

// Use zero value
result, err = gotmpl.Execute(ctx, gotmpl.TemplateOptions{
    Name:       "zero-value",
    Content:    "{{.Missing}}",
    Data:       map[string]string{},
    MissingKey: gotmpl.MissingKeyZero,
})
// Output: "" (empty string, the zero value for string type)

// Default behavior (print "<no value>")
result, err = gotmpl.Execute(ctx, gotmpl.TemplateOptions{
    Name:       "default",
    Content:    "{{.Missing}}",
    Data:       map[string]string{},
    MissingKey: gotmpl.MissingKeyDefault,
})
// Output: "<no value>"
```

### Extracting Template References

Analyze template dependencies by extracting data field references:

```go
svc := gotmpl.NewService(nil)

refs, err := svc.GetReferences(ctx, gotmpl.TemplateOptions{
    Content: `
        {{.User.Name}}
        {{range .Items}}
            {{.Price}}
        {{end}}
    `,
})

for _, ref := range refs {
    fmt.Printf("%s at %s\n", ref.Path, ref.Position)
}
// Output:
// .User.Name at 1:8
// .Items at 2:15
// .Price at 3:14
```

### Complex Example

Combine multiple features:

```go
svc := gotmpl.NewService(template.FuncMap{
    "upper": strings.ToUpper,
})

result, err := svc.Execute(ctx, gotmpl.TemplateOptions{
    Name:      "complex",
    Content:   "[[ upper .Title ]]\n[[range .Items]]\n- [[.Name]]: $[[.Price]]\n[[end]]\nLiteral: TEMPLATE_SYNTAX",
    LeftDelim: "[[",
    RightDelim: "]]",
    Data: map[string]any{
        "Title": "products",
        "Items": []map[string]any{
            {"Name": "Apple", "Price": "1.50"},
            {"Name": "Banana", "Price": "0.75"},
        },
    },
    Replacements: []gotmpl.Replacement{
        {Find: "TEMPLATE_SYNTAX", Replace: "[[...]]"},
    },
    MissingKey: gotmpl.MissingKeyError,
})

// Output:
// PRODUCTS
// - Apple: $1.50
// - Banana: $0.75
// Literal: [[...]]
```

## Logging

The package integrates with the `github.com/oakwood-commons/scafctl/pkg/logger` package for structured logging:

- **V(1)**: High-level operations (template execution start/complete)
- **V(2)**: Detailed steps (parsing, replacements, function registration)

Enable verbose logging to debug template issues:

```go
import "github.com/oakwood-commons/scafctl/pkg/logger"

// Create a logger with verbosity level 2
lgr := logger.Get(2)
ctx := logger.WithLogger(context.Background(), lgr)

result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
    Name:    "debug",
    Content: "{{.Value}}",
    Data:    map[string]string{"Value": "test"},
})
```

## Error Handling

The package provides detailed error messages with context:

```go
result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
    Name:    "invalid",
    Content: "{{.Value",  // Missing closing delimiter
    Data:    map[string]string{},
})
if err != nil {
    // Error: failed to create template 'invalid': parse error: ...
    fmt.Println(err)
}
```

Common error scenarios:

- **Empty content**: Returns `"template content cannot be empty"`
- **Parse errors**: Invalid template syntax
- **Execution errors**: Type mismatches, undefined functions, missing keys (with `MissingKeyError`)

## Template Reference Extraction

The package can analyze templates to extract data field references, useful for:

- Validating that required data is provided
- Building dependency graphs
- Documentation generation
- Static analysis

The extraction:

- ✅ Includes field accesses (`.User.Name`, `.Items`)
- ✅ Works with custom delimiters
- ✅ Handles nested structures and control flow
- ❌ Excludes function calls (`{{upper .Text}}` only extracts `.Text`)
- ❌ Excludes template variables (`{{$var := .Value}}` extracts `.Value` but not `$var`)

```go
// Using the service method
svc := gotmpl.NewService(nil)
refs, err := svc.GetReferences(ctx, gotmpl.TemplateOptions{
    Content:   "{{.User.Name}} {{range .Orders}}{{.ID}}{{end}}",
    LeftDelim: "[[",
    RightDelim: "]]",
})

// Using the convenience function
refs, err := gotmpl.GetGoTemplateReferences(
    "{{.User.Name}}",
    "{{",
    "}}",
)
```

## Constants

```go
const (
    // DefaultLeftDelim is the default left delimiter for templates
    DefaultLeftDelim = "{{"

    // DefaultRightDelim is the default right delimiter for templates
    DefaultRightDelim = "}}"
)
```

## Testing

The package includes comprehensive test coverage:

```bash
# Run all tests
go test ./pkg/gotmpl

# Run with verbose output
go test -v ./pkg/gotmpl

# Run examples
go test -v ./pkg/gotmpl -run Example

# Run with coverage
go test -cover ./pkg/gotmpl
```

See `gotmpl_test.go`, `refs_test.go`, and `example_test.go` for detailed usage examples.

## Design Rationale

### Service Pattern

The service pattern allows for:

- **Reusability**: Create once, execute many times
- **Default configurations**: Share common functions across executions
- **Testability**: Easy to mock and test
- **Consistency**: Same pattern as other packages in the project

### Context Support

Context integration enables:

- **Cancellation**: Stop long-running template execution
- **Logging**: Structured logging with verbosity levels
- **Tracing**: Future support for distributed tracing
- **Deadline propagation**: Respect timeouts from upstream callers

### Replacement System

The replacement system solves the problem of embedding literal template syntax:

1. Before parsing, specified strings are replaced with UUID placeholders
2. Template is parsed and executed (placeholders pass through unmodified)
3. After execution, placeholders are restored to original strings

This is useful when:

- Documenting template syntax (showing `{{...}}` examples)
- Embedding other template formats in output
- Protecting special characters from template parsing

### Typed MissingKey Options

Using a custom type instead of strings provides:

- **Compile-time safety**: Invalid options caught by the compiler
- **IDE autocomplete**: Better developer experience
- **Self-documentation**: Clear available options
- **Future extensibility**: Easy to add new options

## Performance Considerations

- **Template caching**: Compiled templates are cached in an LRU cache keyed by SHA-256 hash of content + configuration. The default cache holds up to 10,000 entries.
- **Replacements**: Linear scan of content (O(n) per replacement)
- **Reference extraction**: Parse tree traversal (O(nodes) in template)
- **Logging overhead**: Minimal when verbosity is disabled

For high-performance scenarios:

- Reuse Service instances to avoid function map duplication
- Minimize replacements (only protect necessary strings)
- Disable verbose logging in production
- Tune the cache size via `goTemplate.cacheSize` in your app config (default: 10,000)

### Template Cache

The package includes a **thread-safe LRU template cache** (`TemplateCache`) that avoids re-parsing identical templates across executions. Cache keys are SHA-256 hashes of: template content, delimiters, missingKey option, and function map keys.

```go
// Access the default cache (singleton, lazily initialized)
cache := gotmpl.GetDefaultCache()

// Check cache stats
stats := cache.Stats()
fmt.Printf("Size: %d/%d, Hit rate: %.1f%%\n", stats.Size, stats.MaxSize, stats.HitRate)

// Detailed stats with top-N most accessed templates
detailed := cache.GetDetailedStats(10)
for _, ts := range detailed.TopTemplates {
    fmt.Printf("  %s: %d hits\n", ts.TemplateName, ts.Hits)
}
```

#### App-level configuration

The cache size is configured via application config:

```yaml
goTemplate:
  cacheSize: 10000    # Max compiled templates to cache (default: 10000)
  enableMetrics: true  # Enable template cache metrics
```

At startup, call `InitFromAppConfig` to wire the cache to the config:

```go
gotmpl.InitFromAppConfig(ctx, gotmpl.GoTemplateConfigInput{
    CacheSize:     cfg.GoTemplate.CacheSize,
    EnableMetrics: true,
})
```

The function is idempotent — subsequent calls are no-ops.

## Extensions

The `gotmpl/ext` package provides an extension system for registering additional template functions, mirroring the pattern in `pkg/celexp/ext`. Extensions are automatically available to all template executions via `NewService()` and `Execute()`.

### Built-in Extensions

#### Sprig Functions

Over 100 utility functions from [Masterminds/sprig v3](https://masterminds.github.io/sprig/) are automatically available in all templates:

```go
// String functions
{{ "hello" | upper }}        // "HELLO"
{{ "HELLO" | lower }}        // "hello"
{{ "  hello  " | trim }}     // "hello"
{{ "hello" | repeat 3 }}     // "hellohellohello"

// Data format functions
{{ dict "key" "value" | toJson }}           // {"key":"value"}
{{ dict "key" "value" | toPrettyJson }}     // formatted JSON

// Math functions
{{ add 1 2 }}                // 3
{{ max 5 3 }}                // 5

// Date functions
{{ now | date "2006-01-02" }}  // today's date

// List functions
{{ list 1 2 3 | join "," }}    // "1,2,3"
```

See the [sprig documentation](https://masterminds.github.io/sprig/) for the full list.

#### Custom Extensions

##### toHcl

Converts a Go object into HCL (HashiCorp Configuration Language) format:

```go
{{ dict "name" "myapp" "port" 8080 | toHcl }}
// Output:
// name = "myapp"
// port = 8080

// Nested objects become HCL blocks:
{{ dict "server" (dict "host" "localhost" "port" 443) | toHcl }}
// Output:
// server {
//   host = "localhost"
//   port = 443
// }
```

##### toYaml

Encodes a Go value as a YAML string:

```go
{{ dict "name" "myapp" "port" 8080 | toYaml }}
// Output:
// name: myapp
// port: 8080

// Combined with indent for nested YAML:
{{ .config | toYaml | indent 4 }}
```

##### fromYaml

Decodes a YAML string into a `map[string]any`:

```go
{{ $parsed := fromYaml "name: myapp\nport: 8080" }}
{{ $parsed.name }}  // "myapp"
{{ $parsed.port }}  // 8080
```

##### mustToYaml / mustFromYaml

Identical to `toYaml` and `fromYaml` respectively. In Go templates, errors always propagate, so behavior is the same. These exist for Helm naming convention compatibility.

### Discovering Available Functions

#### CLI

```bash
# List all available functions
scafctl get go-template-functions

# List only custom (non-sprig) functions
scafctl get go-template-functions --custom

# List only sprig functions
scafctl get go-template-functions --sprig

# Get details for a specific function
scafctl get go-template-functions toHcl

# Output as JSON
scafctl get go-template-functions -o json
```

#### MCP Server

The `list_go_template_functions` MCP tool exposes the same information:

| Parameter     | Description                      |
|---------------|----------------------------------|
| `name`        | Filter by function name (substring match) |
| `custom_only` | Show only custom extensions      |
| `sprig_only`  | Show only sprig functions        |

### Adding New Extensions

To add a new custom template function:

1. **Create a sub-package** under `pkg/gotmpl/ext/` (e.g., `ext/myfunc/`):

```go
package myfunc

import (
    "github.com/oakwood-commons/scafctl/pkg/gotmpl"
    "text/template"
)

func MyFunc(input string) (string, error) {
    // implementation
    return result, nil
}

func MyFuncDef() gotmpl.ExtFunction {
    return gotmpl.ExtFunction{
        Name:        "myFunc",
        Description: "Describes what myFunc does",
        Custom:      true,
        Links: []string{
            "https://example.com/docs",
        },
        Examples: []gotmpl.Example{
            {
                Description: "Basic usage",
                Template:    `{{ "input" | myFunc }}`,
            },
        },
        Func: template.FuncMap{
            "myFunc": MyFunc,
        },
    }
}
```

2. **Register in the extension registry** (`pkg/gotmpl/ext/ext.go`):

```go
func Custom() gotmpl.ExtFunctionList {
    return gotmpl.ExtFunctionList{
        hcl.ToHclFunc(),
        extyaml.ToYamlFunc(),
        extyaml.FromYamlFunc(),
        extyaml.MustToYamlFunc(),
        extyaml.MustFromYamlFunc(),
        myfunc.MyFuncDef(), // Add here
    }
}
```

3. **Add tests** in `ext/myfunc/myfunc_test.go`

The function will be automatically available in all templates, the MCP server, and the CLI.

### Architecture

Extensions use a factory pattern to avoid import cycles:

```
cmd/scafctl/scafctl.go
  └─ gotmpl.SetExtensionFuncMapFactory(gotmplext.AllFuncMap)  // Wire at init

pkg/gotmpl/gotmpl.go
  └─ NewService() calls factory → gets merged FuncMap
  └─ Execute() calls NewService(nil) → extensions included automatically

pkg/gotmpl/ext/ext.go
  └─ All() = Sprig() + Custom()
  └─ AllFuncMap() merges all into template.FuncMap
```

## Related Packages

- **text/template**: Standard Go template engine (underlying implementation)
- **text/template/parse**: Template AST parsing (used for reference extraction)
- **github.com/oakwood-commons/scafctl/pkg/logger**: Structured logging integration
- **github.com/oakwood-commons/scafctl/pkg/celexp**: CEL expression evaluation (complementary templating)

## License

See the main project LICENSE file.
