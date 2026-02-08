# Contributing to scafctl

Thank you for your interest in contributing to scafctl! This document provides guidelines and best practices for contributing.

## Code of Conduct

See our [Code of Conduct](CODE_OF_CONDUCT.md).

## Getting Started

### Developer Certificate of Origin (DCO)

All contributions must be signed off per the [Developer Certificate of Origin](https://developercertificate.org/).
This certifies you have the right to submit the contribution under the project's license.

Sign commits with `-s`:

```bash
git commit -s -m "feat(provider): add new provider"
```

If you forget, amend the last commit:

```bash
git commit --amend -s --no-edit
```

### Prerequisites

- Go 1.25.4+
- golangci-lint
- Git

### Setup

```bash
# Clone the repository
git clone https://github.com/oakwood-commons/scafctl.git
cd scafctl

# Install dependencies
go mod download

# Build
go build -o dist/scafctl ./cmd/scafctl/scafctl.go

# Run tests
go test ./...

# Run linter
golangci-lint run
```

## Development Workflow

### 1. Create a Branch

```bash
git checkout -b feat/my-feature
# or
git checkout -b fix/my-bugfix
```

### 2. Make Changes

Follow the coding standards below.

### 3. Test Your Changes

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./pkg/provider/...

# Run integration tests
go test ./tests/integration/... -v

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 4. Lint Your Code

```bash
golangci-lint run --fix
```

### 5. Commit with Conventional Commits

```bash
# Format: <type>(<scope>): <description>

git commit -m "feat(provider): add rate-limit provider"
git commit -m "fix(resolver): handle nil pointer in graph builder"
git commit -m "docs(tutorial): add provider development guide"
git commit -m "test(action): add retry backoff tests"
git commit -m "refactor(cli): simplify output formatting"
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation
- `test`: Tests
- `refactor`: Code refactoring
- `perf`: Performance improvement
- `chore`: Maintenance tasks

### 6. Push and Create PR

```bash
git push origin feat/my-feature
```

Then create a Pull Request on GitHub.

## Coding Standards

### Error Handling

```go
// Good: Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}

// Good: Use sentinel errors
var ErrNotFound = errors.New("resource not found")

// Bad: Panic in library code
if config == nil {
    panic("config is nil") // Don't do this
}
```

### Logging

Use `logr/zapr` for logging:

```go
lgr := logger.FromContext(ctx)
lgr.V(1).Info("processing request", "url", url)  // Debug level
lgr.V(0).Info("request complete", "status", 200) // Info level
lgr.Error(err, "request failed", "url", url)     // Error level
```

### CLI Output

Use the `Writer` package for terminal output:

```go
w := writer.FromContext(ctx)
w.WriteMessage(output.MessageTypeInfo, "Processing...")
w.WriteMessage(output.MessageTypeSuccess, "Done!")
w.WriteMessage(output.MessageTypeError, "Failed!")
```

### Struct Tags

Always include JSON/YAML tags and Huma validation:

```go
type Config struct {
    Name        string `json:"name" yaml:"name" doc:"Resource name" maxLength:"100" example:"my-resource"`
    Port        int    `json:"port" yaml:"port" doc:"Listen port" minimum:"1" maximum:"65535" example:"8080"`
    Enabled     bool   `json:"enabled" yaml:"enabled" doc:"Enable feature"`
    Tags        []string `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Resource tags" maxItems:"20"`
}
```

### Testing

Use testify for assertions:

```go
func TestMyFunction(t *testing.T) {
    // Arrange
    input := "test"
    
    // Act
    result, err := MyFunction(input)
    
    // Assert
    require.NoError(t, err)
    assert.Equal(t, "expected", result)
    assert.NotEmpty(t, result)
    assert.Contains(t, result, "test")
}

// Table-driven tests
func TestMyFunction_Cases(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {"empty input", "", "", true},
        {"valid input", "test", "result", false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := MyFunction(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### Mocks

Place mocks in `mock.go` files:

```go
// mock.go
type MockProvider struct {
    ExecuteFunc func(ctx context.Context, input any) (*Output, error)
}

func (m *MockProvider) Execute(ctx context.Context, input any) (*Output, error) {
    if m.ExecuteFunc != nil {
        return m.ExecuteFunc(ctx, input)
    }
    return &Output{Data: map[string]any{}}, nil
}
```

## Project Structure

```
scafctl/
├── cmd/scafctl/          # Main entry point
├── docs/                  # Documentation
│   ├── design/           # Design documents
│   ├── internal/         # Implementation notes
│   └── tutorials/        # User guides
├── examples/             # Example solutions and configs
├── pkg/                  # Library code
│   ├── action/           # Action execution
│   ├── celexp/           # CEL expression handling
│   ├── cmd/              # CLI commands
│   ├── config/           # Configuration management
│   ├── provider/         # Provider framework
│   │   └── builtin/      # Built-in providers
│   ├── plugin/           # Plugin system
│   ├── resolver/         # Resolver execution
│   ├── settings/         # Runtime settings
│   ├── solution/         # Solution loading
│   └── terminal/         # Terminal I/O
└── tests/
    └── integration/      # Integration tests
```

## Adding a New Provider

1. Create directory: `pkg/provider/builtin/myprovider/`
2. Implement `provider.Provider` interface
3. Register in `pkg/provider/builtin/builtin.go`
4. Add tests: `my_provider_test.go`
5. Add examples to `Descriptor.Examples`
6. Update provider reference docs if public

See [Provider Development Guide](docs/tutorials/provider-development.md).

## Adding a New CLI Command

1. Create package: `pkg/cmd/scafctl/mycommand/`
2. Implement command factory function
3. Register in parent command (e.g., `root.go`)
4. Add integration tests in `tests/integration/cli_test.go`
5. Update docs/design/cli.md

Pattern:

```go
package mycommand

import (
    "github.com/spf13/cobra"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
)

func CommandMyCommand(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "mycommand",
        Short: "Brief description",
        Long:  "Detailed description...",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Implementation
            return nil
        },
    }
    
    cmd.Flags().StringVarP(&flag, "flag", "f", "", "Flag description")
    
    return cmd
}
```

## Documentation

- Keep README.md up to date
- Add tutorials for new features in `docs/tutorials/`
- Update TODO.md when completing items
- Include examples in provider descriptors

## Breaking Changes

This project is pre-1.0, so breaking changes are allowed. When making them:

1. Document in commit message: `feat!: remove deprecated API`
2. Update migration notes if needed
3. Ensure integration tests are updated

## Release Process

Releases are automated via GitHub Actions on tag push:

```bash
git tag v0.2.0
git push origin v0.2.0
```

## Getting Help

- Open an issue for bugs or feature requests
- Check existing issues and discussions
- Review design docs in `docs/design/`
- For security reports, see [SECURITY.md](.github/SECURITY.md)

## Recognition

Contributors are recognized in release notes and the GitHub contributors page.

Thank you for contributing! 🎉
