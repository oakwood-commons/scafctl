---
description: "scafctl: Fix Go build errors, go vet warnings, and linter issues with minimal surgical changes."
agent: "go-build-resolver"
---
Diagnose and fix the current Go build errors:

1. Run `go build ./...` to identify compilation errors
2. Run `go vet ./...` to find vet warnings
3. Apply minimal, surgical fixes — don't refactor
4. Run `go build ./...` again to verify
5. Run `go test ./...` to ensure nothing broke

Stop if the same error persists after 3 attempts or the fix requires architectural changes.
