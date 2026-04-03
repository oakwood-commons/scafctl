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

### Paths
- Use xdg paths via the `pkg/paths` package

### Configuration
- Use `pkg/settings` for storing defaults and managing settings
- Use `pkg/config/` for storing, loading, and managing application configuration

### CEL
Use `celexp.EvaluateExpression()` from `pkg/celexp/context.go`. For CEL provider, prefer the `expression` field over `expr`.

## Conventions

- **Commits**: Use [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/#specification)
- **Signing**: All commits must be GPG/SSH signed (`-S`) and include DCO sign-off (`-s`)
- **Errors**: Return errors with `fmt.Errorf("context: %w", err)`, don't panic
- **Breaking changes**: Allowed—this app is not in production. Note when doing so.
- **Backward compatibility**: Do not do it, see Breaking changes above.

## Build & Test Commands

```bash
# Build
go build -ldflags "-s -w -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.BuildVersion=dev -X main.Commit=$(git rev-parse HEAD)" -o dist/scafctl ./cmd/scafctl/scafctl.go

# Test
go test ./...                    # Run all tests

# Linting
golangci-lint run --fix          # Run Linter and auto-fix issues
```

The project uses `task` (go-task/task) as a convenience wrapper, but AI agents should use raw Go commands for clarity and portability.

## Critical Rules

- **Business logic placement**: Never in CLI command packages (`pkg/cmd/scafctl/...`), MCP handler files (`pkg/mcp/tools_*.go`), or API packages — put it in shared domain packages (`pkg/...`)
- **After any change**: Run `task test:e2e` to ensure everything passes
- **No magic values**: Always define constants or use settings for configuration values
- **Git safety**: Never run `git commit`, `git push`, or `git commit --amend` unless the user explicitly asks. Never commit or push without approval first

## Security Scanning

```bash
gosec ./...
```

## Additional Conventions

Go coding conventions (struct tags, error handling, design patterns), testing rules, integration test scoping, and documentation requirements are in `.github/instructions/*.instructions.md` files — they load automatically when editing relevant files.