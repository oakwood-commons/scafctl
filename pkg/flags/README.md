# Package flags

Package `flags` provides utilities for parsing command-line flag values in `key=value` format with optional CSV support.

## Features

- Parse `key=value` pairs from command-line flags
- Support for repeated flags with the same key (values are combined)
- Optional CSV parsing within a single flag value
- Quote support (single and double quotes)
- Escaped quote support within quoted values
- Automatic whitespace trimming

## Usage

### Basic Parsing (No CSV)

Use `ParseKeyValue()` when each flag value is a single `key=value` pair:

```go
// Command: app --flag key1=value1 --flag key2=value2
var flagValues []string
cmd.Flags().StringArrayVarP(&flagValues, "flag", "f", []string{}, "Key-value pairs")

// In your command handler:
result, err := flags.ParseKeyValue(flagValues)
if err != nil {
    return err
}

value := flags.GetFirst(result, "key1")  // "value1"
```

### CSV-Aware Parsing

Use `ParseKeyValueCSV()` to allow comma-separated pairs within a single flag. Supports both explicit `key=value` pairs and shorthand syntax where values without `=` use the previous key:

```go
// Command: app -r env=prod,qa,staging -r region=us-east1,us-west1
var resolverPairs []string
cmd.Flags().StringArrayVarP(&resolverPairs, "resolver", "r", []string{}, "Resolver parameters")

// In your command handler:
result, err := flags.ParseKeyValueCSV(resolverPairs)
if err != nil {
    return err
}

envs := flags.GetAll(result, "env")         // ["prod", "qa", "staging"]
regions := flags.GetAll(result, "region")   // ["us-east1", "us-west1"]
```

### Examples

#### Shorthand Syntax (New)

Values without `=` are treated as additional values for the previous key within the same flag:

```bash
# Shorthand - two values for same key
-r "env=prod,qa"
# Result: env = [prod, qa]

# Shorthand - multiple values
-r "env=prod,qa,staging"
# Result: env = [prod, qa, staging]

# Mixed keys with shorthand
-r "region=us-east,us-west,env=prod,qa"
# Result: region = [us-east, us-west], env = [prod, qa]

# Shorthand still allows explicit syntax
-r "env=prod,env=qa,staging"
# Result: env = [prod, qa, staging]
```

**Note**: Shorthand only applies within a single flag. Each `-r` flag resets the key context.

#### Traditional Explicit Syntax

```bash
# Multiple entries in one flag (explicit)
-r "region=us-east1,region=us-west1,region=eu-west1"
# Result: region = [us-east1, us-west1, eu-west1]

# Mixed keys (explicit)
-r "region=us-east1,env=prod,region=us-west1"
# Result: region = [us-east1, us-west1], env = [prod]

# Multiple separate flags
-r env=prod -r env=qa
# Result: env = [prod, qa]

# Quoted values preserve commas
-r "region=\"us-east1,region=us-west1,region=eu-west1\""
# Result: region = ["us-east1,region=us-west1,region=eu-west1"]

# Escaped quotes in values
-r "msg=\"Hello \\\"world\\\"\""
# Result: msg = ["Hello \"world\""]

# Combining flags
-r "region=us-east1,region=us-west1" -r region=eu-west1
# Result: region = [us-east1, us-west1, eu-west1]

# Whitespace is trimmed
-r "region=us-east1, region=us-west1 , region=eu-west1"
# Result: region = [us-east1, us-west1, eu-west1]
```

### URI Scheme Support

**New**: Values can use URI schemes to avoid escaping special characters like quotes and commas in JSON, YAML, etc.

Supported schemes: `json://`, `yaml://`, `base64://`, `http://`, `https://`, `file://`

When a scheme is detected, all content after it (including commas, quotes, backslashes) is treated literally until the next `key=value` pattern.

```bash
# JSON without escaping - no commas require quotes
-r "data=json://{\"key\":\"value\"}"
# Result: data = ["json://{\"key\":\"value\"}"]

# JSON with commas in value
-r "data=json://[1,2,3]"
# Result: data = ["json://[1,2,3]"]

# JSON in CSV context
-r "env=prod,config=json://{\"a\":1,\"b\":2},region=us-east1"
# Result: env=[prod], config=["json://{\"a\":1,\"b\":2}"], region=[us-east1]

# Nested URLs in JSON
-r "data=json://{\"url\":\"https://example.com\"}"
# Result: data = ["json://{\"url\":\"https://example.com\"}"]

# YAML with commas
-r "config=yaml://items: [a, b, c]"
# Result: config = ["yaml://items: [a, b, c]"]

# Base64 data
-r "token=base64://SGVsbG8sIFdvcmxkIQ=="
# Result: token = ["base64://SGVsbG8sIFdvcmxkIQ=="]

# HTTP/HTTPS URLs with commas (quote if used in CSV)
-r "url=\"https://example.com?a=1,b=2\""
# Result: url = ["https://example.com?a=1,b=2"]

# File paths
-r "path=file:///etc/config.json"
# Result: path = ["file:///etc/config.json"]
```

**Important**: The scheme prefix is preserved in the value - your application code should detect and process it accordingly.

## Resolution

For validating AND fetching/parsing scheme-prefixed values in one operation, use the `resolve` package:

```go
import (
    "context"
    "github.com/oakwood-commons/scafctl/pkg/flags"
    "github.com/oakwood-commons/scafctl/pkg/flags/resolve"
)

ctx := context.Background()

// Parse flags
parsed, err := flags.ParseKeyValueCSV([]string{
    `config=json://{"db":"postgres"}`,
    `data=yaml://items: [a,b,c]`,
    `token=base64://SGVsbG8=`,
})
if err != nil {
    return err
}

// Resolve all values (validate + fetch + parse)
resolved, err := resolve.ResolveAll(ctx, parsed)
if err != nil {
    return err // Returns error if JSON is invalid, file missing, HTTP fails, etc.
}

// Use resolved values as native Go types
config := resolve.GetFirst(resolved, "config").(map[string]any)
// config is a parsed map, not a string with "json://" prefix

token := resolve.GetFirst(resolved, "token").([]byte)
// token is decoded bytes, not a base64 string
```

See [resolve/README.md](resolve/README.md) for detailed resolution documentation.

## Validation

For validating scheme-prefixed values (JSON, YAML, Base64, file existence, URL format), use the `validate` package:

```go
import (
    "github.com/oakwood-commons/scafctl/pkg/flags"
    "github.com/oakwood-commons/scafctl/pkg/flags/validate"
)

// Parse flags
parsed, err := flags.ParseKeyValueCSV([]string{
    `config=json://{"db":"postgres"}`,
    `data=yaml://items: [a,b,c]`,
})
if err != nil {
    return err
}

// Validate all values
validated, err := validate.ValidateAll(parsed)
if err != nil {
    return err // Returns error if JSON is invalid, YAML is malformed, etc.
}

// Use validated values (schemes still present)
for key, values := range validated {
    for _, val := range values {
        // Process value - strip scheme if needed
        if strings.HasPrefix(val, "json://") {
            jsonContent := val[7:]
            // Use jsonContent...
        }
    }
}
```

See [validate/README.md](validate/README.md) for detailed validation documentation.

## Helper Functions

- `GetFirst(m, key)` - Returns the first value for a key, or empty string
- `GetAll(m, key)` - Returns all values for a key as a slice
- `Has(m, key)` - Checks if a key exists

## Important Notes

1. **Use StringArrayVarP, not StringSliceVarP**: Cobra's `StringSliceVarP` uses CSV parsing which causes issues with special characters. Always use `StringArrayVarP` to avoid this.

2. **Key Restrictions**: Keys cannot contain whitespace or newlines. They are trimmed automatically.

3. **Value Support**: Values support ALL characters including newlines, quotes, commas, and special characters.

4. **Combining Values**: When the same key appears multiple times (either within CSV or across multiple flags), all values are combined into a slice.

## Cobra Integration

```go
var resolverPairs []string

cmd := &cobra.Command{
    RunE: func(cmd *cobra.Command, args []string) error {
        resolvers, err := flags.ParseKeyValueCSV(resolverPairs)
        if err != nil {
            return fmt.Errorf("invalid resolver parameters: %w", err)
        }
        
        // Use parsed values
        if flags.Has(resolvers, "env") {
            env := flags.GetFirst(resolvers, "env")
            // ...
        }
        
        return nil
    },
}

// IMPORTANT: Use StringArrayVarP, NOT StringSliceVarP
cmd.Flags().StringArrayVarP(&resolverPairs, "resolver", "r", []string{},
    "Resolver parameters in key=value format (repeatable)")
```
