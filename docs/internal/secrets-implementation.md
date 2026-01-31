# Secrets Package Implementation Plan

## Overview

A cross-platform secrets management system for scafctl that securely stores large authentication tokens (~100KB) using a hybrid approach: OS keychain for master key storage + encrypted files for secret data.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     pkg/secrets API                         │
├─────────────────────────────────────────────────────────────┤
│  Get(name) / Set(name, value) / Delete(name) / List()      │
└──────────────────────────┬──────────────────────────────────┘
                           │
         ┌─────────────────┴─────────────────┐
         │                                   │
         ▼                                   ▼
┌─────────────────────┐           ┌─────────────────────────┐
│   OS Keychain       │           │   Encrypted Files       │
│   (via go-keyring)  │           │   ~/.config/scafctl/    │
├─────────────────────┤           │   secrets/<name>.enc    │
│ Stores:             │           ├─────────────────────────┤
│ • Master encryption │──────────▶│ • AES-256-GCM encrypted │
│   key only          │           │ • One file per secret   │
│ • ~32 bytes         │           │ • No size limit         │
└─────────────────────┘           └─────────────────────────┘
```

## Storage Locations

| Platform | Secrets Directory |\n|----------|-------------------|\n| Linux | `~/.local/share/scafctl/secrets/` (XDG_DATA_HOME) |\n| macOS | `~/Library/Application Support/scafctl/secrets/` |\n| Windows | `%LOCALAPPDATA%\\scafctl\\secrets\\` |\n\n**Override:** `SCAFCTL_SECRETS_DIR` environment variable

## Encryption Details

### Master Key
- **Algorithm:** 256-bit random key
- **Storage:** OS keychain (service: `scafctl`, account: `master-key`)
- **Fallback:** `SCAFCTL_SECRET_KEY` env var (base64-encoded)

### Secret Files
- **Algorithm:** AES-256-GCM
- **File Format:**
  ```
  [version:1 byte][nonce:12 bytes][ciphertext:N bytes][tag:16 bytes]
  ```
- **Version:** `0x01` for initial implementation
- **Permissions:** Files created with `0600`, directory with `0700`

## Public API

```go
package secrets

// Store provides secure secret storage operations.
type Store interface {
    // Get retrieves a secret by name. Returns ErrNotFound if not exists.
    Get(ctx context.Context, name string) ([]byte, error)
    
    // Set stores a secret. Creates or overwrites existing.
    Set(ctx context.Context, name string, value []byte) error
    
    // Delete removes a secret. No error if not exists.
    Delete(ctx context.Context, name string) error
    
    // List returns all secret names.
    List(ctx context.Context) ([]string, error)
    
    // Exists checks if a secret exists.
    Exists(ctx context.Context, name string) (bool, error)
}

// New creates a new Store with default configuration.
func New(opts ...Option) (Store, error)

// Option configures the Store.
type Option func(*config)

func WithSecretsDir(dir string) Option      // Override secrets directory
func WithKeyring(kr Keyring) Option         // Inject keyring (for testing)
func WithLogger(logger logr.Logger) Option  // Set logger

// Errors
var (
    ErrNotFound       = errors.New("secret not found")
    ErrInvalidName    = errors.New("invalid secret name")
    ErrCorrupted      = errors.New("secret is corrupted")
    ErrKeyringAccess  = errors.New("cannot access keyring")
)
```

## Secret Name Validation

**Allowed characters:** `a-z`, `A-Z`, `0-9`, `-`, `_`, `.`

**Rules:**
- Length: 1-255 characters
- Cannot start with `.` or `-`
- Cannot contain `..`
- Case-sensitive

**Regex:** `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`

## Failure Handling

| Scenario | Behavior |
|----------|----------|
| Master key deleted | Log warning, delete all secrets, generate new master key, return `ErrNotFound` for any Get |
| Keychain unavailable | Fall back to `SCAFCTL_SECRET_KEY` env var, error if not set |
| Secret file corrupted | Log error, delete the corrupted file, return `ErrCorrupted` |
| Secret file deleted externally | Return `ErrNotFound` |
| Invalid permissions on directory | Log warning, attempt to fix, error if cannot |
| Disk full | Return wrapped OS error |
| Keychain locked/denied | Return `ErrKeyringAccess` with details |
| Invalid secret name | Return `ErrInvalidName` |

## Package Structure

```
pkg/secrets/
├── secrets.go         # Public API, Store interface, New()
├── store.go           # Store implementation
├── keyring.go         # Keyring interface + OS keychain wrapper
├── encrypt.go         # AES-256-GCM encryption/decryption
├── storage.go         # File operations (read, write, delete, list)
├── validation.go      # Secret name validation
├── options.go         # Option functions and config struct
├── errors.go          # Error types
├── mock.go            # Mock implementations for testing
└── secrets_test.go    # Tests
```

## Dependencies

| Dependency | Purpose | Notes |
|------------|---------|-------|
| `github.com/zalando/go-keyring` | OS keychain access | Lightweight, well-maintained |
| `crypto/aes` | AES encryption | Standard library |
| `crypto/cipher` | GCM mode | Standard library |
| `crypto/rand` | Secure random | Standard library |

---

## Implementation Phases

### Phase 1: Core Infrastructure ✅

**Files:** `errors.go`, `validation.go`, `options.go`

**Tasks:**
- [x] Define error types (`ErrNotFound`, `ErrInvalidName`, `ErrCorrupted`, `ErrKeyringAccess`)
- [x] Implement secret name validation with regex
- [x] Define `Option` type and config struct
- [x] Implement `WithSecretsDir`, `WithLogger` options
- [x] Write tests for validation

**Tests:**
- Valid/invalid secret names
- Option application

---

### Phase 2: Encryption Layer ✅

**Files:** `encrypt.go`

**Tasks:**
- [x] Implement `encrypt(key, plaintext []byte) ([]byte, error)`
  - Generate random 12-byte nonce
  - Encrypt with AES-256-GCM
  - Return `[version][nonce][ciphertext][tag]`
- [x] Implement `decrypt(key, ciphertext []byte) ([]byte, error)`
  - Parse version byte, validate
  - Extract nonce, ciphertext, tag
  - Decrypt and authenticate
- [x] Implement `generateMasterKey() ([]byte, error)`
  - Generate 32 bytes from `crypto/rand`
- [x] Write comprehensive tests

**Tests:**
- Round-trip encryption/decryption
- Corrupted data detection
- Wrong key detection
- Version byte handling

---

### Phase 3: Keyring Integration ✅

**Files:** `keyring.go`

**Tasks:**
- [x] Define `Keyring` interface:
  ```go
  type Keyring interface {
      Get(service, account string) (string, error)
      Set(service, account, value string) error
      Delete(service, account string) error
  }
  ```
- [x] Implement `OSKeyring` wrapper around `go-keyring`
- [x] Implement `envKeyring` fallback (reads `SCAFCTL_SECRET_KEY`)
- [x] Implement `fallbackKeyring` that tries OS keyring, falls back to env
- [x] Add `WithKeyring` option for testing
- [x] Implement master key helpers: `GetMasterKeyFromKeyring`, `SetMasterKeyInKeyring`, `DeleteMasterKeyFromKeyring`
- [x] Write tests with mock keyring

**Tests:**
- Get/Set/Delete operations
- Fallback behavior when keychain unavailable
- Env var fallback
- Master key CRUD operations

---

### Phase 4: File Storage ✅

**Files:** `storage.go`

**Tasks:**
- [x] Implement `getSecretsDir() (string, error)` with platform detection
  - Supports macOS (`~/Library/Application Support/scafctl/secrets/`)
  - Supports Linux (`~/.config/scafctl/secrets/` or `XDG_CONFIG_HOME`)
  - Supports Windows (`%APPDATA%\scafctl\secrets\`)
  - Override via `SCAFCTL_SECRETS_DIR` environment variable
- [x] Implement `ensureSecretsDir(dir string) error`
  - Create directory with `0700` permissions
  - Validate/fix permissions if exists
- [x] Implement `writeSecret(dir, name string, data []byte) error`
  - Atomic write (temp file + rename)
  - Set `0600` permissions
- [x] Implement `readSecret(dir, name string) ([]byte, error)`
- [x] Implement `deleteSecret(dir, name string) error`
- [x] Implement `listSecrets(dir string) ([]string, error)`
- [x] Implement `secretExists(dir, name string) (bool, error)`
- [x] Implement `deleteAllSecrets(dir string) error` (for master key recovery)
- [x] Write comprehensive tests

**Tests:**
- CRUD operations
- Atomic write behavior
- Permission handling
- Platform-specific paths
- Round-trip write/read verification

---

### Phase 5: Store Implementation ✅

**Files:** `store.go`, `secrets.go`

**Tasks:**
- [x] Define `Store` interface in `secrets.go` with full package documentation
- [x] Implement `store` struct with all dependencies (secretsDir, keyring, masterKey, logger, mutex)
- [x] Implement `New(opts ...Option) (Store, error)`
  - Initialize keyring (with fallback)
  - Ensure secrets directory exists
  - Get or create master key
- [x] Implement `initMasterKey()` logic:
  - Try to get from keyring
  - If missing, check for existing secrets
    - If secrets exist: log warning, delete all, generate new key
    - If no secrets: generate new key
  - Store new key in keyring
- [x] Implement `Get(ctx, name)`:
  - Validate name
  - Read encrypted file
  - Decrypt with master key
  - Handle corrupted files (log, delete, return error)
- [x] Implement `Set(ctx, name, value)`:
  - Validate name
  - Encrypt with master key
  - Write to file (atomic)
- [x] Implement `Delete(ctx, name)`:
  - Validate name
  - Delete file (ignore not exists)
- [x] Implement `List(ctx)`:
  - List `.enc` files in directory
  - Strip extension, return names
- [x] Implement `Exists(ctx, name)`:
  - Validate name
  - Check file exists
- [x] Add thread-safety with `sync.RWMutex`
- [x] Write comprehensive tests

**Tests:**
- Full CRUD operations
- Master key recovery scenario (orphaned secrets deletion)
- Corrupted file handling (auto-delete corrupted files)
- Context cancellation
- Concurrent access (race condition testing)
- Round-trip with various data types (text, binary, unicode, large)

---

### Phase 6: Testing & Documentation ✅

**Files:** `mock.go`, `mock_test.go`, `benchmark_test.go`, `README.md`

**Tasks:**
- [x] Create `MockStore` for use in other packages
  - Full Store interface implementation
  - Error injection support (`GetErr`, `SetErr`, `DeleteErr`, `ListErr`, `ExistsErr`)
  - Call tracking (`GetCalls`, `SetCalls`, `DeleteCalls`, `ListCalls`, `ExistsCalls`)
  - Thread-safe with `sync.RWMutex`
  - `Reset()` method to clear state
- [x] Create `MockKeyring` for testing
  - Full Keyring interface implementation
  - Error injection support
  - Call tracking with `KeyringCall` and `KeyringSetCall` structs
  - Thread-safe with `sync.RWMutex`
- [x] Write tests for mocks (`mock_test.go`)
- [x] Add comprehensive benchmarks (`benchmark_test.go`)
  - Encrypt/Decrypt at various sizes (100B, 1KB, 10KB, 100KB, 1MB)
  - Full round-trip benchmarks
  - Store.Set/Get benchmarks
  - ValidateName benchmarks
  - Master key generation benchmarks
- [x] Document package in `pkg/secrets/README.md`
  - Quick start guide
  - Configuration options
  - Storage locations by platform
  - Secret name rules
  - Encryption details
  - Error handling guide
  - Testing guide with MockStore/MockKeyring
  - CI/CD considerations
  - Thread safety notes
  - API reference

**Tests:**
- MockStore CRUD operations with error injection
- MockKeyring operations with error injection
- Reset functionality for both mocks

---

### Phase 7: CLI Commands ✅

**Files:** `pkg/cmd/scafctl/secrets/*.go`

**Overview:** User-facing CLI commands for secrets management. Commands operate on **user secrets only** and block operations on reserved `scafctl.*` namespace used for internal secrets.

**Command Structure:**
```
scafctl secrets
  ├── list                 # List all user secrets
  ├── get <name>          # Get secret value (prints by default)
  ├── set <name> [value]  # Set secret (stdin/file/flag)
  ├── delete <name>       # Delete secret
  ├── exists <name>       # Check if secret exists
  ├── import <file>       # Import secrets from file
  ├── export <file>       # Export secrets to file
  └── rotate              # Rotate master key and re-encrypt all secrets
```

**Tasks:**
- [x] Create parent command: `pkg/cmd/scafctl/secrets/secrets.go`
  - Cobra command with `secrets` subcommands
  - Initialize `Store` from `pkg/secrets`
  - Pass IOStreams for output
- [x] Implement `list` command: `list.go`
  - Call `Store.List()`, filter out `scafctl.*` names
  - Support `-o table|json|yaml|quiet` via `kvx.OutputOptions`
  - Default to table view with name column
- [x] Implement `get` command: `get.go`
  - Call `Store.Get(name)`
  - Validate name doesn't start with `scafctl.`
  - Print value to stdout by default
  - Support `-o json|yaml` for structured output
  - Add `--no-newline` flag for scripting
- [x] Implement `set` command: `set.go`
  - Accept value from: `--value`, `--file`, stdin (default)
  - Call `Store.Set(name, value)`
  - Validate name doesn't start with `scafctl.`
  - Support `--overwrite` confirmation for existing secrets
- [x] Implement `delete` command: `delete.go`
  - Call `Store.Delete(name)`
  - Validate name doesn't start with `scafctl.`
  - Support `--force` to skip confirmation
- [x] Implement `exists` command: `exists.go`
  - Call `Store.Exists(name)`
  - Validate name doesn't start with `scafctl.`
  - Exit code 0 if exists, 1 if not
  - Print boolean to stdout
- [x] Implement `import` command: `import.go`
  - Support plaintext format (YAML/JSON)
  - Support encrypted format (password-protected)
  - Auto-detect format from file header/extension
  - Filter out `scafctl.*` names from import
  - Support `--dry-run` to preview
  - Support `--overwrite` to replace existing
  - Show progress/summary
- [x] Implement `export` command: `export.go`
  - Default to plaintext YAML format
  - Show **scary warning** for plaintext exports
  - Support `--encrypt` for password-protected export
  - Filter out `scafctl.*` names from export
  - Support `--format yaml|json`
  - Prompt for password if `--encrypt` (with confirmation)
- [x] Implement `rotate` command: `rotate.go`
  - Placeholder for future implementation
  - Requires internal changes to pkg/secrets for atomic master key rotation
  - Currently returns error with explanation
- [x] Add name validation helper: `validation.go`
  - Reject `scafctl.*` prefix for user commands
  - Use `secrets.ValidateName()` for format validation
  - Return clear error messages
- [x] Fix API issues and pass linter
  - Use `golang.org/x/term.ReadPassword` for password input
  - Use `kvx.IsTerminal` for terminal detection
  - Use `encoding/json.Marshal` for JSON output
  - Fix all unused parameter warnings
  - All commands compile cleanly with 0 linter issues

**Export Format Specifications:**

**Plaintext YAML:**
```yaml
version: scafctl-secrets-v1
exported_at: "2026-02-04T10:30:00Z"
secrets:
  - name: github-token
    value: "ghp_abc123..."
  - name: gitlab-token
    value: "glpat-xyz..."
```

**Plaintext JSON:**
```json
{
  "version": "scafctl-secrets-v1",
  "exported_at": "2026-02-04T10:30:00Z",
  "secrets": [
    {"name": "github-token", "value": "ghp_abc123..."},
    {"name": "gitlab-token", "value": "glpat-xyz..."}
  ]
}
```

**Encrypted Format:**
```
SCAFCTL-ENC-V1
[salt:16 bytes][iterations:4 bytes][encrypted-json]
```
- Uses PBKDF2 to derive encryption key from password
- Encrypts the plaintext JSON with AES-256-GCM
- Binary format, base64-encoded for text storage

**Tests:**
- Manual testing verified: set, list, get, delete workflow works correctly
- Commands properly filter internal `scafctl.*` secrets
- TODO: Add comprehensive unit tests for all commands

---

### Phase 8: Resolver Integration ✅

**Files:** `pkg/provider/builtin/secretprovider/secret.go`, `examples/resolvers/secrets.yaml`

**Overview:** Enable resolvers to retrieve secrets via a dedicated provider. **Decision:** Provider-only approach - CEL functions cannot access Go context during evaluation.

**Implemented:**

#### 8.1: Secret Provider ✅
- ✅ Created `pkg/provider/builtin/secretprovider/secret.go`
  - Implements `Provider` interface from `pkg/provider`
  - SecretOps interface for testing with mockable operations
  - Supports single secret retrieval and pattern matching
  - Registered in `pkg/provider/builtin/builtin.go`
- ✅ Provider descriptor
  - Type: `secret`
  - Capabilities: `from` (read-only)
  - Schema: `operation` (enum: get/list), `name`, `pattern`, `required`, `fallback`
  - Complete examples in descriptor
- ✅ Execute() implementation
  - `operation: get` with `name`: retrieves specific secret
  - `operation: get` with `pattern`: finds first matching secret via regex
  - `operation: list`: returns all secret names
  - Supports `required` flag (defaults to true, fails if not found)
  - Supports `fallback` value (used when required=false and not found)
  - Proper error handling with errors.Is for ErrNotFound
- ✅ Comprehensive test coverage (18 tests, all passing)
  - Single secret retrieval success/failure
  - Pattern matching with valid/invalid regex
  - Required flag behavior (true/false)
  - Fallback value handling
  - List operation (with/without secrets)
  - Error scenarios (store errors, missing operations)
  - Dry-run mode
  - 0 linter issues
- ✅ Registered as 13th builtin provider (updated test)

**Provider Configuration Examples:**

```yaml
# Single secret retrieval
resolvers:
  - name: github-token
    from:
      provider: secret
      inputs:
        operation: get
        name: github-token
        required: true

# Pattern matching (returns first match)
resolvers:
  - name: prod-token
    from:
      provider: secret
      inputs:
        operation: get
        pattern: ^prod-.*-token$
        required: true

# With fallback (optional secret)
resolvers:
  - name: optional-token
    from:
      provider: secret
      inputs:
        operation: get
        name: optional-token
        required: false
        fallback: default-value

# List all secrets
resolvers:
  - name: all-secrets
    from:
      provider: secret
      inputs:
        operation: list
```

#### 8.2: CEL Functions Decision ✅
**Decision:** Provider-only approach is the correct design.

**Rationale:**
- CEL functions cannot access Go context during evaluation
- Context is required for secrets.Store operations
- Provider pattern handles this correctly via Execute(ctx context.Context, input any)
- CEL functions would need hacky workarounds (global state, thread-unsafe patterns)
- Provider approach is more testable, maintainable, and follows established patterns

**Alternative Considered:** CEL functions like `secret()`, `secretOr()`, `hasSecret()`
- **Rejected:** Technical limitation - CEL evaluation happens without Go context access
- **Workaround:** Provider offers all necessary functionality with proper context handling

#### 8.3: Documentation ✅
- ✅ Comprehensive `examples/resolvers/secrets.yaml` with:
  - Prerequisites (setting up test secrets)
  - Basic usage (get by name, list, pattern matching)
  - Optional secrets with fallback
  - Using secrets in transformations
  - Conditional secret access
  - Error handling patterns
  - Combining multiple secrets
  - Security best practices (never log secrets, namespace secrets, required flag usage)
  - Advanced patterns (dynamic selection, validation)
  - Integration with other providers (HTTP, File)
  - Troubleshooting guide
- ✅ Provider examples in descriptor for self-documenting API
  - Return boolean
  - Never errors
- [ ] Update CEL environment initialization in `pkg/celexp/`
  - Register functions in global environment
  - Inject `Store` instance from context
- [ ] Write comprehensive tests
  - All functions with valid/invalid inputs
  - Not found scenarios
  - Pattern matching edge cases
  - Integration with existing CEL expressions

**CEL Function Examples:**

```yaml
resolvers:
  # Direct secret access
  api_config:
    providers:
      - type: cel
        expression: |
          {
            "token": secret("github-token"),
            "url": "https://api.github.com"
          }
  
  # With fallback
  optional_config:
    providers:
      - type: cel
        expression: |
          secretOr("optional-token", "default-token")
  
  # Pattern matching
  all_credentials:
    providers:
      - type: cel
        expression: |
          secretMatching(".*-credential$")
  
  # Conditional logic
  dynamic_auth:
    providers:
      - type: cel
        expression: |
          hasSecret("oauth-token") 
            ? secret("oauth-token")
            : secret("api-key")
  
  # Internal secret access (allowed)
  internal_oauth:
    providers:
      - type: cel
        expression: |
          secret("scafctl.internal.oauth.token")
```

#### 8.3: Documentation & Examples
- [ ] Update `examples/resolvers/secrets.yaml` with comprehensive examples
  - Provider usage
  - CEL function usage
  - Pattern matching
  - Fallback chains
  - Internal secret access
- [ ] Create `docs/secrets-resolver-integration.md`
  - Usage guide for both approaches
  - When to use provider vs CEL functions
  - Security considerations
  - Best practices

**Tests:**
- Provider: single retrieval, pattern matching, required/fallback
- CEL functions: all functions with edge cases
- Integration: resolvers using secrets in real configs
- Error handling: not found, invalid names, corrupted secrets
- Permission: verify resolvers can access internal secrets

---

## Internal vs User Secrets

**Protection Strategy:** Reserved namespace with API-level enforcement

**Rules:**
- **Internal secrets:** Use `scafctl.*` prefix (e.g., `scafctl.internal.oauth.token`)
- **User secrets:** Cannot use `scafctl.*` prefix
- **CLI commands:** Block all operations on `scafctl.*` names
- **Resolvers/API:** Can access both internal and user secrets
- **Validation:** Enhanced `ValidateName()` with `scope` parameter

**Why this approach:**
- Filesystem protection is bypassed by users with directory access
- Protection is about **preventing accidents** and **namespace management**
- If internal secrets are tampered with, scafctl handles gracefully (re-auth, regenerate)
- Clear separation of concerns: CLI for user management, API for programmatic access

**Implementation:**
```go
// Enhanced validation for scoped access
func ValidateNameScoped(name string, scope string) error {
    if scope == "user" && strings.HasPrefix(name, "scafctl.") {
        return fmt.Errorf("%w: cannot operate on internal secrets (scafctl.*)", ErrInvalidName)
    }
    return ValidateName(name)
}
```

---

## Implementation Status

| Phase | Description | Status |
|-------|-------------|--------|
| 1 | Core Infrastructure (errors, validation, options) | ✅ |
| 2 | Encryption Layer (AES-256-GCM) | ✅ |
| 3 | Keyring Integration (OS keychain + env fallback) | ✅ |
| 4 | File Storage (platform-specific, atomic writes) | ✅ |
| 5 | Store Implementation (full API) | ✅ |
| 6 | Testing & Documentation | ✅ |
| 7 | CLI Commands (user secrets management) | ✅ |
| 8 | Resolver Integration (secret provider) | ✅ |

---

## Testing Strategy

### Unit Tests
- Each file has corresponding test coverage
- Mock keyring for isolated testing
- Test all error paths

### Integration Tests
- Use temp directories for secrets storage
- Test actual encryption round-trips
- Skip keychain tests in CI (use env fallback)

### CI Considerations
- Set `SCAFCTL_SECRET_KEY` in CI environment
- Tests should work without OS keychain access
