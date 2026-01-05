# scafctl - AI Agent Instructions

## Project Overview
`scafctl` is a Go-based configuration discovery and scaffolding CLI tool. It uses CEL (Common Expression Language) with custom extensions for dynamic configuration evaluation and template processing.

## Architecture & Key Components

### CLI & Terminal Libraries
- **cobra** - Command structure and flag parsing
- **lipgloss** - Terminal styling (colors, borders, layouts)
- **bubbletea** - Interactive TUI components (planned for forms/menus)

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

## Coding Conventions

### Error Handling
- Return errors, don't panic (except in main initialization)
- Use `fmt.Errorf("context: %w", err)` for error wrapping
- CLI errors write to stderr and exit non-zero
- Profiler shutdown errors are logged but non-fatal (see `cmd/scafctl/scafctl.go`)

### Go Style Preferences
- Use `any` instead of `interface{}` (Go 1.18+ modern style)
- Use `maps.Copy()` instead of manual loops for copying maps
- Prefer standard library functions over manual implementations

### Linting & Formatting
- **golangci-lint** configuration in `.golangci.yml` with strict rules
- **gofumpt** and **goimports** auto-formatters enabled
- Test files exclude certain linters (errcheck, dupl, gosec, forcetypeassert)

## Project-Specific Patterns

### Context Usage
- Logger stored in context: `logger.WithLogger(ctx, lgr)`
- HTTP client operations accept `context.Context` for cancellation

### HTTP Client (`pkg/httpc/`)
Custom HTTP client with:
- See `pkg/httpc/README.md` for detailed usage

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
