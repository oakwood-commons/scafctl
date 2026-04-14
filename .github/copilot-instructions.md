# scafctl - AI Agent Instructions

## Overview
Go-based CLI tool using CEL (Common Expression Language) for dynamic configuration evaluation and template processing.

## Key Patterns

- **CLI Output**: Use `writer.FromContext(ctx)` -- never `fmt.Fprintf` directly. See `pkg/terminal/writer/`
- **Data Output**: Use `kvx.OutputOptions` for structured table/json/yaml/quiet output. See `pkg/terminal/kvx/`
- **HTTP Client**: See `pkg/httpc/README.md`
- **Paths**: Use xdg paths via `pkg/paths`
- **Configuration**: `pkg/settings` for defaults, `pkg/config/` for app configuration
- **CEL**: Use `celexp.EvaluateExpression()`. Prefer `expression` field over `expr`

## Conventions

- **Commits**: Use [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/#specification)
- **Signing**: All commits must be GPG/SSH signed (`-S`) and include DCO sign-off (`-s`)
- **Errors**: Return errors with `fmt.Errorf("context: %w", err)`, don't panic
- **Breaking changes**: Allowed -- this app is not in production. Note when doing so.
- **Backward compatibility**: Do not do it, see Breaking changes above.

## Build & Test Commands

```bash
# Build
go build -ldflags "-s -w -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.BuildVersion=dev -X main.Commit=$(git rev-parse HEAD)" -o dist/scafctl ./cmd/scafctl/scafctl.go

# Test
go test ./...                    # Run all tests

# Linting
task lint                        # Run Linter (uses pinned golangci-lint version)
task lint:fix                    # Run Linter and auto-fix issues
```

The project uses `task` (go-task/task) for builds and linting. **Always use `task lint` instead of running `golangci-lint` directly** to ensure the correct pinned version is used.

## Critical Rules

- **Business logic placement**: Never in CLI command packages (`pkg/cmd/scafctl/...`), MCP handler files (`pkg/mcp/tools_*.go`), or API packages -- put it in shared domain packages (`pkg/...`)
- **After any change**: Run `task test:e2e` to ensure everything passes
- **Test coverage**: Every new or changed file must have tests. Target 70%+ patch coverage. Never submit a new file with 0% test coverage
- **No magic values**: Always define constants or use settings for configuration values
- **Git safety**: Never run `git commit`, `git push`, or `git commit --amend` unless the user explicitly asks. Never commit or push without approval first

## Embedder Contract

scafctl is used as a **library by external CLIs**. Every new feature must be consumable by embedders via `RootOptions` or domain package APIs.

- **No hardcoded "scafctl"**: Use `settings.CliBinaryName` or `settings.Run.BinaryName` in context
- **`RootOptions` is the embedder API surface**: New CLI-level capabilities must be exposed as fields with sensible defaults
- **Test embedder scenarios**: Include a test with a non-default binary name (e.g., `"mycli"`)

## Security Scanning

```bash
gosec ./...
```

## Additional Conventions

Go coding conventions (struct tags, error handling, design patterns), testing rules, integration test scoping, and documentation requirements are in `.github/instructions/*.instructions.md` files -- they load automatically when editing relevant files.