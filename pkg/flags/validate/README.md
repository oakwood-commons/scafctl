# Flags Validation Package

Validates values from key-value flag parsing based on URI scheme prefixes.

## Features

- **Scheme-based validation**: Automatically validates based on prefix (json://, yaml://, etc.)
- **Scheme preservation**: Keeps URI scheme prefixes in validated values for downstream processing
- **Multiple validators**: JSON, YAML, Base64, File existence, HTTP/HTTPS URLs
- **Batch validation**: Validate all parsed key-value pairs at once

## Supported Schemes

| Scheme | Validation | Description |
|--------|-----------|-------------|
| `json://` | Valid JSON syntax | Ensures content is parseable JSON |
| `yaml://` | Valid YAML syntax | Ensures content is parseable YAML |
| `base64://` | Valid Base64 encoding | Ensures content is decodable Base64 |
| `file://` | Path validity + file exists | Verifies file exists and is not a directory |
| `http://`, `https://` | Valid URL format | Ensures valid HTTP/HTTPS URL with host |
| _(none)_ | No validation | Values without schemes pass through unchanged |

## API

### ValidateValue

Validates a single value and returns it with the scheme prefix preserved.

```go
import "github.com/oakwood-commons/scafctl/pkg/flags/validate"

value, err := validate.ValidateValue("config", `json://{"key":"value"}`)
// value: `json://{"key":"value"}` (prefix preserved)
// err: nil

value, err := validate.ValidateValue("config", `json://{invalid}`)
// value: ""
// err: invalid JSON for key "config": malformed JSON: ...
```

### ValidateAll

Validates all values in a parsed key-value map.

```go
import (
    "github.com/oakwood-commons/scafctl/pkg/flags"
    "github.com/oakwood-commons/scafctl/pkg/flags/validate"
)

// Parse flags (using shorthand syntax)
parsed, err := flags.ParseKeyValueCSV([]string{
    `config=json://{"db":"postgres"}`,
    `data=yaml://items: [a,b,c]`,
    `env=production,staging,qa`,  // Shorthand: multiple values for same key
})

// Validate all values
validated, err := validate.ValidateAll(parsed)
// Returns same map structure with scheme prefixes preserved
// Returns error on first validation failure
```

### Individual Validators

Each scheme has a dedicated validation function:

```go
// JSON validation
err := validate.ValidateJSON(`{"key":"value"}`)

// YAML validation
err := validate.ValidateYAML(`key: value`)

// Base64 validation
err := validate.ValidateBase64(`SGVsbG8sIFdvcmxkIQ==`)

// File validation (exists + is file)
err := validate.ValidateFile(`/path/to/file.txt`)

// URL validation (http/https)
err := validate.ValidateURL(`https://example.com`)
```

## Usage Pattern with Cobra

```go
var rawFlags []string

cmd := &cobra.Command{
    Use: "mycommand",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Parse key-value flags
        parsed, err := flags.ParseKeyValueCSV(rawFlags)
        if err != nil {
            return err
        }
        
        // Validate all values
        validated, err := validate.ValidateAll(parsed)
        if err != nil {
            return err
        }
        
        // Use validated values (schemes still present)
        for key, values := range validated {
            for _, val := range values {
                // Process value - strip scheme if needed
                // e.g., if strings.HasPrefix(val, "json://") { ... }
            }
        }
        
        return nil
    },
}

cmd.Flags().StringArrayVarP(&rawFlags, "resource", "r", nil, "Key-value pairs")
```

## Validation Behavior

### JSON Scheme (`json://`)
- Validates entire content is valid JSON (object, array, string, number, etc.)
- Error if JSON is malformed or incomplete

### YAML Scheme (`yaml://`)
- Validates entire content is valid YAML
- Supports multi-line YAML, inline arrays, nested structures

### Base64 Scheme (`base64://`)
- Validates content can be decoded as standard Base64
- Rejects invalid characters or incorrect padding

### File Scheme (`file://`)
- Validates path is not empty
- Verifies file exists at specified path
- Ensures path points to a file (not a directory)
- Returns specific errors for "not found" vs "is directory"

### HTTP/HTTPS Schemes
- Validates URL is well-formed
- Ensures scheme is exactly `http` or `https`
- Requires host to be present
- Accepts any valid URL path and query parameters

### No Scheme
Values without recognized schemes pass through validation without errors.

## Error Handling

All validation errors include context:

```go
value, err := validate.ValidateValue("mykey", `json://{bad}`)
// err.Error(): invalid JSON for key "mykey": malformed JSON: invalid character 'b' ...

value, err := validate.ValidateValue("filepath", `file:///nonexistent`)
// err.Error(): invalid file path for key "filepath": file does not exist: /nonexistent
```

## Testing

The package includes comprehensive tests for all validators and schemes:

```bash
go test ./pkg/flags/validate/... -v
```

Tests cover:
- Valid and invalid inputs for each scheme
- Edge cases (empty values, malformed data)
- File system operations (existence checks)
- URL parsing edge cases
- Scheme prefix preservation
- Batch validation with mixed schemes
