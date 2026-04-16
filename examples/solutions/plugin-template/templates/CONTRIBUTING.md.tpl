# Contributing to <% .name %>

Thank you for your interest in contributing! This document provides guidelines.

## Code of Conduct

See our [Code of Conduct](CODE_OF_CONDUCT.md).

## Getting Started

### Developer Certificate of Origin (DCO) & Commit Signing

All commits must be:
1. **GPG/SSH signed** (`-S`) -- verifies commit author identity
2. **DCO signed-off** (`-s`) -- certifies you have the right to submit

Sign commits with both `-s` and `-S`:

~~~bash
git commit -s -S -m "feat: add new feature"
~~~

### Prerequisites

- Go 1.26.0+
- [Task](https://taskfile.dev/) (go-task)
- golangci-lint

### Setup

~~~bash
git clone <% .module %>
cd <% .name %>
go mod download
task build
task test
~~~

## Development Workflow

1. Create a branch from `main`
2. Make your changes
3. Ensure tests pass: `task test`
4. Ensure lint passes: `task lint`
5. Commit with conventional commit messages
6. Open a Pull Request

## Conventional Commits

Use [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/):

~~~text
feat: add new capability
fix: correct input validation
docs: update README
test: add benchmark tests
chore: update dependencies
~~~

## Testing

- Write table-driven tests
- Use testify/assert for assertions
- Include benchmarks for new features
- Target 70%+ coverage on new code
