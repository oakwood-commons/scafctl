# secrets

Package `secrets` provides secure secret storage operations using AES-256-GCM encryption with OS keychain integration for master key management.

## Overview

The package uses a hybrid approach for secret storage:
- **Master encryption key** is stored in the OS keychain (or environment variable fallback)
- **Secrets** are encrypted with AES-256-GCM and stored as individual files

This allows for storing large secrets (e.g., authentication tokens up to ~100KB) that wouldn't fit in the OS keychain directly, while still leveraging secure key storage.

## Installation

```go
import "github.com/oakwood-commons/scafctl/pkg/secrets"
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/oakwood-commons/scafctl/pkg/secrets"
)

func main() {
    // Create a new store with default options
    store, err := secrets.New()
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // Store a secret
    err = store.Set(ctx, "my-api-key", []byte("sk-1234567890abcdef"))
    if err != nil {
        log.Fatal(err)
    }

    // Retrieve a secret
    value, err := store.Get(ctx, "my-api-key")
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Secret: %s", value)

    // List all secrets
    names, err := store.List(ctx)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Secrets: %v", names)

    // Check if a secret exists
    exists, err := store.Exists(ctx, "my-api-key")
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Exists: %v", exists)

    // Delete a secret
    err = store.Delete(ctx, "my-api-key")
    if err != nil {
        log.Fatal(err)
    }
}
```

## Configuration Options

### Custom Secrets Directory

```go
store, err := secrets.New(
    secrets.WithSecretsDir("/custom/path/to/secrets"),
)
```

### Custom Keyring (for testing)

```go
mockKeyring := secrets.NewMockKeyring()
store, err := secrets.New(
    secrets.WithKeyring(mockKeyring),
)
```

### With Logger

```go
import "github.com/go-logr/zapr"

logger := zapr.NewLogger(zapLogger)
store, err := secrets.New(
    secrets.WithLogger(logger),
)
```

## Storage Locations

| Platform | Default Secrets Directory |
|----------|---------------------------|
| Linux    | `~/.local/share/scafctl/secrets/` (or `$XDG_DATA_HOME/scafctl/secrets/`) |
| macOS    | `~/Library/Application Support/scafctl/secrets/` |
| Windows  | `%LOCALAPPDATA%\scafctl\secrets\` |

Override with the `SCAFCTL_SECRETS_DIR` environment variable.

## Secret Name Rules

- **Allowed characters:** `a-z`, `A-Z`, `0-9`, `-`, `_`, `.`
- **Length:** 1-255 characters
- **Cannot** start with `.` or `-`
- **Cannot** contain `..`
- **Case-sensitive**

Valid examples: `my-api-key`, `AWS_SECRET_KEY`, `config.prod.v2`

Invalid examples: `.hidden`, `-invalid`, `my..secret`

## Encryption Details

### Master Key
- **Algorithm:** 256-bit random key (32 bytes)
- **Storage:** OS keychain (service: `scafctl`, account: `master-key`)
- **Fallback:** `SCAFCTL_SECRET_KEY` environment variable (base64-encoded)

### Secret Files
- **Algorithm:** AES-256-GCM (authenticated encryption)
- **File format:** `[version:1 byte][nonce:12 bytes][ciphertext+tag:N bytes]`
- **File extension:** `.enc`
- **Permissions:** Files `0600`, Directory `0700`

## Error Handling

```go
import "errors"

value, err := store.Get(ctx, "my-secret")
if err != nil {
    switch {
    case errors.Is(err, secrets.ErrNotFound):
        // Secret doesn't exist
    case errors.Is(err, secrets.ErrInvalidName):
        // Invalid secret name
    case errors.Is(err, secrets.ErrCorrupted):
        // Secret file is corrupted (auto-deleted)
    case errors.Is(err, secrets.ErrKeyringAccess):
        // Cannot access OS keychain
    default:
        // Other error (filesystem, etc.)
    }
}
```

## Testing

### Using MockStore

```go
func TestMyFunction(t *testing.T) {
    store := secrets.NewMockStore()
    
    // Pre-populate data
    store.Data["api-key"] = []byte("test-value")
    
    // Inject errors
    store.GetErr = errors.New("simulated error")
    
    // Test your code
    result, err := myFunction(store)
    
    // Verify calls
    assert.Equal(t, []string{"api-key"}, store.GetCalls)
}
```

### Using MockKeyring

```go
func TestWithCustomKeyring(t *testing.T) {
    keyring := secrets.NewMockKeyring()
    
    store, err := secrets.New(
        secrets.WithSecretsDir(t.TempDir()),
        secrets.WithKeyring(keyring),
    )
    require.NoError(t, err)
    
    // Test your code
}
```

## CI/CD Considerations

In CI environments where OS keychain access is unavailable:

1. Set `SCAFCTL_SECRET_KEY` environment variable with a base64-encoded 32-byte key:
   ```bash
   export SCAFCTL_SECRET_KEY=$(openssl rand -base64 32)
   ```

2. The store will automatically fall back to using this key.

## Thread Safety

All Store operations are thread-safe and can be called concurrently from multiple goroutines.

## Benchmarks

Run benchmarks with:

```bash
go test -bench=. -benchmem ./pkg/secrets/...
```

Example results:
```
BenchmarkEncrypt/100B-10          500000    2345 ns/op   42.65 MB/s
BenchmarkEncrypt/1KB-10           200000    5678 ns/op  180.23 MB/s
BenchmarkEncrypt/100KB-10          10000  123456 ns/op  810.12 MB/s
BenchmarkDecrypt/100B-10          500000    1234 ns/op   81.04 MB/s
BenchmarkDecrypt/1KB-10           200000    4567 ns/op  224.53 MB/s
```

## API Reference

### Store Interface

```go
type Store interface {
    Get(ctx context.Context, name string) ([]byte, error)
    Set(ctx context.Context, name string, value []byte) error
    Delete(ctx context.Context, name string) error
    List(ctx context.Context) ([]string, error)
    Exists(ctx context.Context, name string) (bool, error)
}
```

### Constructor

```go
func New(opts ...Option) (Store, error)
```

### Options

```go
func WithSecretsDir(dir string) Option
func WithKeyring(kr Keyring) Option
func WithLogger(logger logr.Logger) Option
```

### Errors

```go
var (
    ErrNotFound      = errors.New("secret not found")
    ErrInvalidName   = errors.New("invalid secret name")
    ErrCorrupted     = errors.New("secret is corrupted")
    ErrKeyringAccess = errors.New("cannot access keyring")
)
```
