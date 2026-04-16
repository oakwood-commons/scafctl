---
description: "scafctl: Run Go tests with race detection, check coverage, and diagnose failures."
agent: "go-fixer"
---
Run the Go test suite and report results:

1. Run `go test -race -count=1 ./...` — report any failures
2. Run `go test -coverprofile=coverage.out ./...` — check coverage
3. Run `go tool cover -func=coverage.out | tail -1` — report total coverage
4. If failures exist, diagnose root cause and suggest fixes
5. Identify packages with coverage below 80%

Focus on recently changed packages first. Use `git diff --name-only -- '*.go'` to identify them.
