# Flags Resolve Package

Resolves and fetches data from key-value flag values based on URI scheme prefixes. This package validates, fetches, and parses data in one operation.

## Features

- **Scheme-based resolution**: Automatically validates, fetches, and parses based on prefix
- **Type conversion**: Returns native Go types (maps, slices, bytes, strings)
- **Scheme stripping**: Removes URI scheme prefixes from results
- **HTTP client integration**: Uses `pkg/httpc` for robust HTTP requests
- **Context support**: All operations accept context for cancellation/timeout

## Supported Schemes

| Scheme | Processing | Return Type |
|--------|-----------|-------------|
| `json://` | Validates & parses JSON | `map[string]any`, `[]any`, or primitive |
| `yaml://` | Validates & parses YAML | `map[string]any`, `[]any`, or primitive |
| `base64://` | Validates & decodes | `[]byte` |
| `file://` | Validates file exists & reads | `[]byte` |
| `http://`, `https://` | Validates URL & fetches | `[]byte` |
| _(none)_ | No processing | `string` (as-is) |

## API

### ResolveValue

Validates and resolves a single value, returning parsed/fetched data.

```go
import (
    "context"
    "github.com/oakwood-commons/scafctl/pkg/flags/resolve"
)

ctx := context.Background()

// JSON - returns parsed Go types
result, err := resolve.ResolveValue(ctx, "config", `json://{"db":"postgres","port":5432}`)
config := result.(map[string]any)
fmt.Println(config["db"])  // "postgres"
fmt.Println(config["port"]) // 5432.0 (float64)

// YAML - returns parsed Go types  
result, err := resolve.ResolveValue(ctx, "data", `yaml://items: [a, b, c]`)
data := result.(map[string]any)
items := data["items"].([]any)

// Base64 - returns decoded bytes
result, err := resolve.ResolveValue(ctx, "token", `base64://SGVsbG8sIFdvcmxkIQ==`)
token := result.([]byte)
fmt.Println(string(token))  // "Hello, World!"

// File - returns file contents as bytes
result, err := resolve.ResolveValue(ctx, "config", `file:///etc/config.json`)
content := result.([]byte)

// HTTP - returns response body as bytes
result, err := resolve.ResolveValue(ctx, "data", `https://api.example.com/config`)
body := result.([]byte)

// Plain value - returns as string
result, err := resolve.ResolveValue(ctx, "env", "production")
env := result.(string)
```

### ResolveAll

Resolves all values in a parsed key-value map.

```go
import (
    "github.com/oakwood-commons/scafctl/pkg/flags"
    "github.com/oakwood-commons/scafctl/pkg/flags/resolve"
)

// Parse flags (using shorthand syntax)
parsed, err := flags.ParseKeyValueCSV([]string{
    `config=json://{"db":"postgres"}`,
    `data=yaml://items: [a,b,c]`,
    `env=production,staging,qa`,  // Shorthand: multiple values for same key
})

// Resolve all values
ctx := context.Background()
resolved, err := resolve.ResolveAll(ctx, parsed)
// map[string][]any with parsed/fetched data

// Access resolved values
config := resolve.GetFirst(resolved, "config").(map[string]any)
envs := resolve.GetAll(resolved, "env")  // ["production", "staging", "qa"]
```

### Helper Functions

Same interface as `pkg/flags` helpers but for resolved data:

```go
// Get first value (returns any or nil)
value := resolve.GetFirst(resolved, "config")

// Get all values (returns []any)
values := resolve.GetAll(resolved, "region")

// Check if key exists
if resolve.Has(resolved, "apiKey") {
    // ...
}
```

## Type Assertions

After resolving, assert to the expected type:

```go
// JSON object
result, _ := resolve.ResolveValue(ctx, "config", `json://{"key":"value"}`)
config := result.(map[string]any)

// JSON array
result, _ := resolve.ResolveValue(ctx, "list", `json://[1,2,3]`)
list := result.([]any)

// YAML
result, _ := resolve.ResolveValue(ctx, "yaml", `yaml://key: value`)
data := result.(map[string]any)

// Base64
result, _ := resolve.ResolveValue(ctx, "token", `base64://dGVzdA==`)
bytes := result.([]byte)

// File/HTTP
result, _ := resolve.ResolveValue(ctx, "file", `file:///path/to/file`)
bytes := result.([]byte)

// Plain value
result, _ := resolve.ResolveValue(ctx, "env", "prod")
str := result.(string)
```

## Usage Pattern with Cobra

```go
var resourceFlags []string

cmd := &cobra.Command{
    Use: "mycommand",
    RunE: func(cmd *cobra.Command, args []string) error {
        ctx := cmd.Context()
        
        // Step 1: Parse key-value flags
        parsed, err := flags.ParseKeyValueCSV(resourceFlags)
        if err != nil {
            return fmt.Errorf("parse error: %w", err)
        }
        
        // Step 2: Resolve all values (validate + fetch + parse)
        resolved, err := resolve.ResolveAll(ctx, parsed)
        if err != nil {
            return fmt.Errorf("resolve error: %w", err)
        }
        
        // Step 3: Use resolved values with type assertions
        if resolve.Has(resolved, "config") {
            config := resolve.GetFirst(resolved, "config").(map[string]any)
            // Use config...
        }
        
        if resolve.Has(resolved, "token") {
            token := resolve.GetFirst(resolved, "token").([]byte)
            // Use token...
        }
        
        return nil
    },
}

cmd.Flags().StringArrayVarP(&resourceFlags, "resource", "r", nil, "Key-value pairs")
```

## Error Handling

All errors include context about the key and operation:

```go
result, err := resolve.ResolveValue(ctx, "config", `json://{invalid}`)
// Error: invalid JSON for key "config": malformed JSON: ...

result, err := resolve.ResolveValue(ctx, "file", `file:///nonexistent`)
// Error: invalid file path for key "file": file does not exist: /nonexistent

result, err := resolve.ResolveValue(ctx, "url", `http://example.com/404`)
// Error: failed to fetch URL for key "url": HTTP error: status 404
```

## HTTP Fetching

The package uses `pkg/httpc` client with:
- 30 second timeout
- Custom User-Agent: `scafctl-flags-resolver/1.0`
- Context-aware cancellation
- Proper error handling for non-200 status codes

## Context Usage

All resolve operations accept a context for:
- Cancellation during long operations
- Timeout control
- Request propagation for HTTP calls

```go
// With timeout
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

resolved, err := resolve.ResolveAll(ctx, parsed)

// With cancellation
ctx, cancel := context.WithCancel(context.Background())
go func() {
    // Cancel after some condition
    cancel()
}()

result, err := resolve.ResolveValue(ctx, "url", "https://slow-api.com/data")
```

## Comparison with Validate Package

| Feature | `pkg/flags/validate` | `pkg/flags/resolve` |
|---------|---------------------|-------------------|
| **Purpose** | Validate syntax only | Validate + fetch + parse |
| **Returns** | Original value with scheme | Parsed/fetched data |
| **Scheme prefix** | Preserved | Stripped |
| **HTTP/File** | URL/path validation only | Actual fetch/read |
| **JSON/YAML** | Syntax check only | Full parsing |
| **Base64** | Encoding validation | Full decoding |
| **Return type** | `string` | `any` (typed data) |

## Testing

Comprehensive test coverage includes:
- All scheme types with valid/invalid inputs
- Type assertion verification
- HTTP mocking with httptest
- File system operations
- Helper function tests
- Integration tests with multiple schemes

```bash
go test ./pkg/flags/resolve/... -v
```

## Performance Considerations

- **JSON/YAML/Base64**: Single parse/decode operation (no redundant validation)
- **File validation**: Lightweight `os.Stat()` check before reading (prevents reading non-existent files)
- **URL validation**: Quick `url.Parse()` check before fetching (prevents invalid HTTP requests)
- **HTTP requests**: Use context timeouts to prevent hanging
- **Large files**: `file://` reads entire file into memory
- **Concurrent resolution**: Call `ResolveValue` concurrently for independent values if needed

**Efficiency Note**: The resolve package is optimized to avoid double-parsing. For JSON, YAML, and Base64, 
validation and parsing happen in a single operation - the native Go parsers provide clear error messages 
if data is invalid, eliminating the need for separate validation calls.

## Examples

See test file for comprehensive examples including:
- Type assertions
- Re-marshaling JSON/YAML
- HTTP server mocking
- File operations
- Mixed scheme resolution
