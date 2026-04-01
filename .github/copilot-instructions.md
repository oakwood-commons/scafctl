# scafctl - AI Agent Instructions

## Overview
Go-based CLI tool using CEL (Common Expression Language) for dynamic configuration evaluation and template processing.

## Key Patterns

### CLI Output (`pkg/terminal/writer/`)

Use **Writer** for all terminal output—**never** use `fmt.Fprintf` directly.
- Get via `writer.FromContext(ctx)`
- Respects `--quiet` and `--no-color` flags automatically
- For testing, use `writer.WithExitFunc()` to capture exit calls

### Data Output (`pkg/terminal/kvx/`)
Commands returning structured data should use **kvx** with `OutputOptions`:
- Default to **table** view; support `-o` flag for `table`, `json`, `yaml`, `quiet`
- Use `kvx.NewOutputOptions(ioStreams)` then `opts.Write(data)`
- Supports `--interactive` for TUI and `--expression` for CEL filtering

### HTTP Client
See `pkg/httpc/README.md`

### Docs, Examples, and Tutorials

- Use `pkg/docs/` for generating and managing documentation and tutorials.
- Use `pkg/examples/` for storing example configurations and usage scenarios.
- Always create documentation, tutorials, and examples for new features, providers and commands.
- Always update documentation, tutorials, and examples when features, providers or commands change.


### Paths

- Use xdg paths via the pkg/paths package

### Configuration

- Use `pkg/settings` for storing defaults and managing settings.
- Use `pkg/config/` for storing, loading and managing application configuration. Services should store their config in this package when it makes sense.

### CLI integration tests

- Use `tests/integration/cli_test.go` for integration tests of CLI commands.
- Always add new commands to the CLI integration tests.

### Solution integration tests

- Use `tests/integration/solutions/` for integration tests of **non-API** features (providers, resolvers, actions, plugins, solutions, etc.).
- Always create solution integration tests whenever a non-API feature, provider, or command is added or updated.
- Solution integration tests **cannot** test API/server features — use API integration tests instead.

### API integration tests

- Use `tests/integration/api_test.go` for integration tests of **API/server** features (REST endpoints, middleware, auth, rate limiting, etc.).
- Always add new API endpoints to the API integration tests.
- API features must **only** be tested here, not in solution integration tests.

### `settings` Package

## Conventions

- **Commits**: Use [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/#specification)
- **Errors**: Return errors with `fmt.Errorf("context: %w", err)`, don't panic
- **Testing**: Use `testify/assert`, mocks in `mock.go` files
- **Breaking changes**: Allowed—this app is not in production. Note when doing so.
- **Backward compatibility**: Do not do it, see Breaking changes above.

## Struct Tags

Always add JSON/YAML tags. Use [Huma validation tags](https://huma.rocks/features/request-validation/#validation-tags):
- All fields: `doc`
- Strings: `maxLength`, `example`, `pattern`, `patternDescription`
- Integers: `maximum`, `example`
- Arrays: `maxItems` (no `example`)
- Objects/maps: no `example` tag

## Special Field Types

| Field | Type | Package |
|-------|------|---------|
| `expr` | `Expression` | `pkg/celexp` |
| `tmpl` | `GoTemplatingContent` | `pkg/gotmpl` |

## CEL
Use `celexp.EvaluateExpression()` from `pkg/celexp/context.go`. For CEL provider, prefer the `expression` field over `expr`.

## Build & Test Commands
Standard Go commands for development (task runner available but use raw commands for AI agents):

```bash
# Build
go build -ldflags "-s -w -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.BuildVersion=dev -X main.Commit=$(git rev-parse HEAD)" -o dist/scafctl ./cmd/scafctl/scafctl.go

# Test
go test ./...                    # Run all tests

# Linting
golangci-lint run --fix          # Run Linter and auto-fix issues

```

**IMPORTANT**: Never include business logic in CLI command packages (`pkg/cmd/scafctl/...`), MCP handler files (`pkg/mcp/tools_*.go`) or API packages (future) instead, put it into proper shared domain packages (`pkg/...`)

**Note**: The project uses `task` (go-task/task) as a convenience wrapper, but AI agents should use raw Go commands for clarity and portability.

**IMPORTANT**: Always update documentation, tutorials, examples, mcp server tools (if applicable) when features, providers or commands change or added.
**IMPORTANT**: Add benchmark tests for any new features or providers in `*_test.go` files using Go's `testing` package.
**IMPORTANT**: After any change, run `task test:e2e` to ensure everything passes.
**IMPORTANT**: Never use magic strings or numbers; always define constants or use settings for configuration values.
**IMPORTANT**: Never commit or push any code without approval first

---
paths:
  - "**/*.go"
  - "**/go.mod"
  - "**/go.sum"
---
- **gofmt/goimports**: Auto-format `.go` files after edit
- **go vet**: Run static analysis after editing `.go` files
- **staticcheck**: Run extended static checks on modified packages

## Formatting

- **gofmt** and **goimports** are mandatory — no style debates

## Design Principles

- Accept interfaces, return structs
- Keep interfaces small (1-3 methods)

## Error Handling

Always wrap errors with context:

```go
if err != nil {
    return fmt.Errorf("failed to create user: %w", err)
}
```

## Functional Options

```go
type Option func(*Server)

func WithPort(port int) Option {
    return func(s *Server) { s.port = port }
}

func NewServer(opts ...Option) *Server {
    s := &Server{port: 8080}
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

## Small Interfaces

Define interfaces where they are used, not where they are implemented.

## Dependency Injection

Use constructor functions to inject dependencies:

```go
func NewUserService(repo UserRepository, logger Logger) *UserService {
    return &UserService{repo: repo, logger: logger}
}
```

## Secret Management

```go
apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    log.Fatal("OPENAI_API_KEY not configured")
}
```

## Security Scanning

- Use **gosec** for static security analysis:
  ```bash
  gosec ./...
  ```

## Context & Timeouts

Always use `context.Context` for timeout control:

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
```

## Framework

Use the standard `go test` with **table-driven tests**.

## Race Detection

Always run with the `-race` flag:

```bash
go test -race ./...
```

## Coverage

```bash
go test -cover ./...
```

## Reference

See skill: `golang-patterns` for comprehensive Go idioms and patterns.