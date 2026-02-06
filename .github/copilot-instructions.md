# scafctl - AI Agent Instructions

## Overview
Go-based CLI tool using CEL (Common Expression Language) for dynamic configuration evaluation and template processing.

## Key Patterns

### Logging
Uses **logr/zapr**. Context-aware: `logger.FromContext(ctx)`, `logger.WithLogger(ctx, lgr)`

### CLI Output (`pkg/terminal/writer/`)
Use **Writer** for all terminal output—**never** use `fmt.Fprintf` directly.
- Get via `writer.FromContext(ctx)` or `writer.MustFromContext(ctx)`
- Respects `--quiet` and `--no-color` flags automatically
- For testing, use `writer.WithExitFunc()` to capture exit calls

### Data Output (`pkg/terminal/kvx/`)
Commands returning structured data should use **kvx** with `OutputOptions`:
- Default to **table** view; support `-o` flag for `table`, `json`, `yaml`, `quiet`
- Use `kvx.NewOutputOptions(ioStreams)` then `opts.Write(data)`
- Supports `--interactive` for TUI and `--expression` for CEL filtering

### HTTP Client
See `pkg/httpc/README.md`

### Configuration

- Use `pkg/settings` for storing defaults and managing settings.
- Use `pkg/config/` for storing, loading and managing application configuration. Services should store their config in this package when it makes sense.

### CLI integration tests

- Use `tests/integration/cli_test.go` for integration tests of CLI commands.
- Always add new commands to the CLI integration tests.

### `settings` Package

## Conventions

- **Commits**: Use [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/#specification)
- **Errors**: Return errors with `fmt.Errorf("context: %w", err)`, don't panic
- **Testing**: Use `testify/assert`, mocks in `mock.go` files
- **Linting**: Run `golangci-lint run` before committing
- **Build**: Include LDFLAGS for version injection (see `taskfile.yaml`)
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

**Note**: The project uses `task` (go-task/task) as a convenience wrapper, but AI agents should use raw Go commands for clarity and portability.