# scafctl - AI Agent Instructions

## Project Overview
`scafctl` is a Go-based configuration discovery and scaffolding CLI tool. It uses CEL (Common Expression Language) with custom extensions for dynamic configuration evaluation and template processing.

## Architecture & Key Components

### Core Packages
- **`pkg/celexp/`** - CEL (Common Expression Language) extensions framework
  - Custom functions organized by domain: `ext/arrays/`, `ext/strings/`, `ext/filepath/`, `ext/debug/`, etc.
  - Each function follows the `celexp.ExtFunction` pattern with metadata and `cel.EnvOption` implementations
  - Conversion utilities in `pkg/celexp/conversion/` handle type conversions between CEL and Go types
  
- **`pkg/solution/`** - Solution manifest handling (YAML/JSON)
  - `Solution` struct with semver validation and metadata (maintainers, links, tags)
  - `get/` subpackage provides `Interface` for fetching solutions from filesystem or URLs
  - Solutions define scaffolding templates and configurations

- **`pkg/dag/`** - Dependency graph execution engine
  - Manages execution order with dependency resolution and cycle detection
  - `RunnerResults` tracks execution phases, timing, and errors
  - Used for orchestrating multi-step scaffolding operations

- **`pkg/cmd/scafctl/`** - Cobra-based CLI structure
  - Root command in `root.go` sets up persistent flags, logging, and profiling
  - Subcommands in subdirectories: `get/`, `version/`
  - Uses lipgloss for styled terminal output

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
go test ./pkg/celexp/ext/...     # Run tests for specific package
go test -v ./...                 # Verbose output
go test -cover ./...             # With coverage
go test -run TestFuncName ./...  # Run specific test

# Linting
golangci-lint run                # Run linter
golangci-lint run --fix          # Auto-fix issues

# Dependencies
go mod tidy                      # Clean up dependencies
go mod download                  # Download dependencies
```

**Note**: The project uses `task` (go-task/task) as a convenience wrapper, but AI agents should use raw Go commands for clarity and portability.

### Version Management
- Versions follow semantic versioning with `v` prefix (e.g., `v1.2.3`)
- Local/PR builds: `{latest-tag}-alpha{YYYYMMDD}` (e.g., `0.81.0-alpha20251210`)
- Release builds: Tag-based via goreleaser
- LDFLAGS inject version info into `main.BuildVersion`, `main.Commit`, `main.BuildTime`

### Testing Conventions
- Test files: `*_test.go` in same package
- Use `testify/assert` and `testify/require` for assertions
- CEL extension tests follow pattern:
  ```go
  func TestFuncName_CELIntegration(t *testing.T) {
      funcObj := FuncName()
      env, _ := cel.NewEnv(funcObj.EnvOptions...)
      ast, issues := env.Compile(`expression`)
      prog, _ := env.Program(ast)
      result, _, _ := prog.Eval(map[string]interface{}{})
      assert.Equal(t, expected, result.Value())
  }
  ```
- Benchmarks use `BenchmarkFuncName_CEL` naming pattern
- Mock implementations go in `mock.go` files (see `pkg/solution/get/mock.go`)

## Coding Conventions

### CEL Extension Functions
All custom CEL functions must:
1. Return `celexp.ExtFunction` struct with:
   - `Name`: Fully qualified (e.g., `"arrays.strings.add"`)
   - `Description`: Usage documentation
   - `Examples`: Array of `celexp.Example` with Description and Expression fields
   - `EnvOptions`: CEL environment options slice
2. Use `strings.ReplaceAll(funcName, ".", "_")` for overload naming
3. Handle type errors explicitly: `types.NewErr("funcname: error msg")`
4. Use conversion helpers from `pkg/celexp/conversion/` for type safety

Example pattern from `pkg/celexp/ext/arrays/arrays.go`:
```go
func StringAddFunc() celexp.ExtFunction {
    funcName := "arrays.strings.add"
    return celexp.ExtFunction{
        Name: funcName,
        Description: "Appends a string to a list of strings",
        Examples: []celexp.Example{
            {
                Description: "Add a single string to an existing list",
                Expression:  `arrays.strings.add(["apple", "banana"], "cherry")`,
            },
            {
                Description: "Chain multiple add operations",
                Expression:  `arrays.strings.add(arrays.strings.add(["a"], "b"), "c")`,
            },
        },
        EnvOptions: []cel.EnvOption{
            cel.Function(funcName,
                cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
                    []*cel.Type{cel.ListType(cel.StringType), cel.StringType},
                    cel.ListType(cel.StringType),
                    cel.BinaryBinding(func(arrayObj, newValue ref.Val) ref.Val {
                        // Implementation with type checking
                    }),
                ),
            ),
        },
    }
}
```

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
- Run `task lint:fix` before committing

### Profiling Support
Hidden flags for CPU/memory profiling:
- `--pprof memory|cpu` - Enable profiling
- `--pprof-output-dir ./` - Output directory
- Access via `profiler.GetProfiler()` and `profiler.StopProfiler()`
- Used for performance analysis, not production features

## Project-Specific Patterns

### Context Usage
- Logger stored in context: `logger.WithLogger(ctx, lgr)`
- Profiler context available via `profiler` package
- HTTP client operations accept `context.Context` for cancellation

### HTTP Client (`pkg/httpc/`)
Custom HTTP client with:
- Automatic retries via `hashicorp/go-retryablehttp`
- HTTP caching via `ivan.dev/httpcache`
- Configurable timeouts and retry policies
- See `pkg/httpc/README.md` for detailed usage

### Dependency Injection
- Use functional options pattern for constructors (e.g., `NewGetter(...Option)`)
- Interfaces defined for testability (e.g., `solution.get.Interface`)
- Mock implementations for testing

## File Organization
- Entry point: `cmd/scafctl/scafctl.go`
- Package-level logic in `pkg/`
- Tests colocated with implementation files
- Checksum directory for task caching (`.task/`, `checksum/`)

## Important Notes
- Build commands should include LDFLAGS for version injection (see Build & Test Commands section)
- **Never** modify test files to reduce coverage - fix the actual issues
- When adding CEL functions, update both implementation and tests
- Solutions can be YAML or JSON - use `UnmarshalFromBytes()` for auto-detection
