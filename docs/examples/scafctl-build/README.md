# Build scafctl with scafctl

This example replaces the traditional Taskfile workflow for the scafctl CLI with a scafctl solution. Actions orchestrate the Go toolchain to tidy modules, format code, run vet/tests, and compile the binary. Once scafctl is implemented, the project can self-host its build by running `scafctl` directly.

## Actions Overview

| Action  | Description                       | Depends On |
|---------|-----------------------------------|------------|
| tidy    | `go mod tidy` to sync dependencies| –          |
| fmt     | `go fmt ./...`                    | tidy       |
| lint    | `golangci-lint run`               | fmt        |
| vet     | `go vet ./...`                    | lint       |
| test    | `go test ./...`                   | vet        |
| build   | `go build` with ldflags           | test       |
| clean   | Remove build output directory     | –          |

Running the `build` action automatically executes the full DAG (`tidy → fmt → vet → test → build`).

The folder includes a minimal Go CLI (`cmd/scafctl/main.go`) and version helper (`internal/version/version.go`) so the scafctl solution can build a tangible binary. A `.golangci.yml` config mirrors the one used in the real project.

## Usage

```bash
# Preview what build would do
scafctl run solution:scafctl-build --dry-run --action build

# Execute the build (once the scafctl engine is wired up)
scafctl run solution:scafctl-build --action build

# Execute linting or tests (dependencies run automatically)
scafctl run solution:scafctl-build --action lint
scafctl run solution:scafctl-build --action test

# Provide custom project root or output directory
scafctl run solution:scafctl-build \
  -r projectRoot=/path/to/scafctl \
  -r output=./dist \
  --action build

# Clean build output
scafctl run solution:scafctl-build --action clean
```

> Prerequisites: Go 1.21+ and `golangci-lint` must be available on `PATH` for the actions to succeed.

## Customization

- Use `-r goBin=/opt/go/bin/go` to point at a specific Go toolchain.
- Override `ldFlags` resolver to inject build metadata (`-X main.version=...`).
- Extend the action graph with additional steps (static analysis, sarif reports, etc.).

## Tests

The solution defines CLI dry-run tests to ensure action wiring remains valid.

```
scafctl test run solution:scafctl-build
```

## Next Steps

When scafctl’s engine is complete, we can retire the Taskfile and rely solely on this solution for local builds and CI pipelines. Add publish actions to push artifacts or container images as needed.
