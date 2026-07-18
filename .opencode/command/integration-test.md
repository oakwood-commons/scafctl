---
description: "scafctl: Build the binary and run real CLI commands to verify changes work end-to-end."
---
Build the scafctl binary and run real CLI integration tests against it.
Automated tests can pass while the actual CLI is broken. This prompt catches those gaps.

## Steps

1. **Build the binary**
   ```
   go build -ldflags "-s -w -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.BuildVersion=dev -X main.Commit=$(git rev-parse HEAD)" -o dist/scafctl ./cmd/scafctl/scafctl.go
   ```

2. **Identify what changed** -- use `git diff --name-only` to find modified packages and commands.

3. **Run real commands** against the built binary (`./dist/scafctl`):
   - Always use `--debug` to surface internal logging and catch silent failures.
   - Use `--verbose` to verify verbose output paths work.
   - Test the **happy path** first, then **error paths** (bad input, missing args, invalid flags).
   - Use `--dry-run` where available to verify it short-circuits without side effects.
   - Use `--help` on changed commands to verify flag registration and descriptions.
   - For commands that write state (build, install, catalog), use isolated temp dirs:
     ```
     XDG_DATA_HOME=/tmp/scafctl-itest/data XDG_CACHE_HOME=/tmp/scafctl-itest/cache ./dist/scafctl ...
     ```
   - After writes, use read commands (catalog inspect, catalog list) to verify stored data.
   - Clean up temp dirs when done.

4. **Report results** as a table: command, result (pass/fail), and any issues found.

5. **If a test reveals a bug**, fix the code, rebuild, and re-test that specific scenario.

## What to test per command type

- **CLI commands** (plugins install, build solution, etc.): flags, args, output format, exit codes
- **MCP tools**: use the MCP server or unit tests -- MCP tools aren't directly CLI-testable
- **Provider changes**: `./dist/scafctl run provider <name> ...` with real inputs
- **Resolver changes**: `./dist/scafctl run resolver -f <solution> ...` with example solutions

## Key things to watch for

- Commands that silently succeed but produce wrong output
- Flags that are registered but ignored in execution
- Error messages that don't match what the code intends
- `--verbose` / `--debug` output that crashes or shows stale info
- Exit codes (0 for success, non-zero for errors)
