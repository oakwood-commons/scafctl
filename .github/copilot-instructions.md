# scafctl - AI Agent Instructions

## Project Overview
`scafctl` is a Go-based configuration discovery and scaffolding CLI tool. It uses CEL (Common Expression Language) with custom extensions for dynamic configuration evaluation and template processing.

## Architecture & Key Components

### Logging Pattern
Uses **logr** interface with **zapr** (zap adapter) for structured logging:
- `logger.Get(verbosity)` creates loggers with verbosity levels (negative numbers, e.g., `-1` for debug)
- Context-aware: `logger.WithLogger(ctx, lgr)` and `logger.FromContext(ctx)`
- Global keys defined in `logger/logger.go`: `RootCommandKey`, `CommitKey`, `VersionKey`, etc.
- Example: `lgr.V(1).Info("message", "key", value)` for verbose logging

## Development Workflow

### Build & Test Commands
Standard Go commands for development (task runner available but use raw commands for AI agents):
```bash
# Build
go build -ldflags "-s -w -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.BuildVersion=dev -X main.Commit=$(git rev-parse HEAD)" -o dist/scafctl ./cmd/scafctl/scafctl.go

# Test
go test ./...                    # Run all tests

# Linting
golangci-lint run                # Run linter
golangci-lint run --fix          # Auto-fix issues

```

**Note**: The project uses `task` (go-task/task) as a convenience wrapper, but AI agents should use raw Go commands for clarity and portability.

### Testing Conventions
- Test files: `*_test.go` in same package
- Use `testify/assert` and `testify/require` for assertions
- Mock implementations go in `mock.go` files (see `pkg/solution/get/mock.go`)
- Use httptest to mock HTTP servers in tests

## Coding Conventions

**Note**: This application is not in production, large or breaking changes are fine. Please make a note when doing so.

### Commit Messages
- Use conventional commits https://www.conventionalcommits.org/en/v1.0.0/#specification when creating a commit message

### Error Handling
- Return errors, don't panic (except in main initialization)
- Use `fmt.Errorf("context: %w", err)` for error wrapping or `errors.New("message")` for new errors
- CLI errors write to stderr and exit non-zero

### Go Style Preferences
- Use `any` instead of `interface{}` (Go 1.18+ modern style)
- Use `maps.Copy()` instead of manual loops for copying maps
- Prefer standard library functions over manual implementations

### Linting & Formatting
- **golangci-lint** configuration in `.golangci.yml` with strict rules
- **gofumpt** and **goimports** auto-formatters enabled
- Test files exclude certain linters (errcheck, dupl, gosec, forcetypeassert)

## Project-Specific Patterns

### HTTP Client (`pkg/httpc/`)
Custom HTTP client.
- See `pkg/httpc/README.md` for detailed usage

### Parsing Key-Value Flags (`pkg/flags/`)

- Use `flags.ParseKeyValueCSV([]string)` to parse key-value pairs with CSV support
- Supports URI schemes: `json://`, `yaml://`, `base64://`, `http://`, `https://`, `file://`
- See `pkg/flags/README.md` for detailed documentation

### Dependency Injection
- Use functional options pattern for constructors (e.g., `NewGetter(...Option)`)
- Interfaces defined for testability (e.g., `solution.get.Interface`)
- Mock implementations for testing

## File Organization
- Entry point: `cmd/scafctl/scafctl.go`
- Package-level logic in `pkg/`
- Tests colocated with implementation files

## Important Notes
- Build commands should include LDFLAGS for version injection (see Build & Test Commands section)
- **Never** modify test files to reduce coverage - fix the actual issues
- Always run `golangci-lint` and tests before committing code

## Struct Tags

Struct tags should always be added to all structs for JSON and YAML serialization, even if not immediately needed

Use https://huma.rocks/features/request-validation/#validation-tags for additional struct tags. Minimally include `doc` for all fields. For scalar fields (strings, integers, booleans), include appropriate validation tags such as `example` where helpful. For string fields, also include `maxLength`, `example`, `pattern` and `patternDescription`. For integer fields, include `maximum` and `example`. For array/slice fields, include `maxItems` but do not supply the `example` tag. Do not supply the `example` tag to objects, arrays or maps. If any other tags are applicable, include those as well.

## CEL

- Use the `EvaluateExpression` function in `pkg/celexp/context.go` to evaluate CEL expressions

### CEL provider

- When providing a CEL expression to the CEL provider, always use the literal struct field for the `expression` property. The CEL provider supports both `expression` and `expr` properties, but best practice is to use `expression` directly. if `expr` is used it will need to result in a valid CEL expression string.


## Important Fields

### expr

The `expr` field in structs represents a CEL (Common Expression Language) expression. This should always be of type `Expression` from the `github.com/oakwood-commons/scafctl/pkg/celexp` package. This ensures that the expression is properly parsed and validated according to the project's CEL extensions. 

### tmpl

The `tmpl` field in structs represents a go templating expression. This should always be of type `GoTemplatingContent` from the `github.com/oakwood-commons/scafctl/pkg/gotmpl` package. This ensures that the templating content is properly handled according to the project's templating processing logic.