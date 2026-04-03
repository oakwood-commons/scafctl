---
description: "scafctl: Run Go code review on recent changes. Checks for idiomatic Go, security, error handling, concurrency, and scafctl conventions."
agent: "go-reviewer"
---
Review the current Go code changes for:
- Security vulnerabilities (command injection, path traversal, hardcoded secrets)
- Error handling (ignored errors, missing wrapping, panics for recoverable errors)
- Concurrency issues (goroutine leaks, race conditions, deadlocks)
- Code quality (function length, nesting depth, idiomatic patterns)
- scafctl conventions (Writer usage, kvx output, struct tags, business logic placement)

Run `go vet ./...` and `task lint` first, then review the code.
