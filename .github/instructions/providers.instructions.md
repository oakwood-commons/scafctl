---
description: "Provider implementation rules for scafctl. Providers implement the Provider interface with Descriptor() and Execute(). Must include benchmarks, tests, and mock.go. Use when creating or editing providers."
applyTo: "pkg/provider/**/*.go"
---

# Provider Layer

Providers are **stateless execution primitives** that implement the Provider interface.

## Interface

Implement both methods:
- `Descriptor()` returns provider metadata, schema, and capabilities
- `Execute(ctx, input)` runs the provider logic

## Rules

- Define `ProviderName` and `Version` constants
- Implement `Descriptor()` returning name, version, API version, schema, and capabilities
- Use `schemahelper` for JSON schema generation
- Define a testable ops interface (e.g., `EnvOps`) and inject via constructor for mockability
- Provide `DefaultXxxOps` struct for real implementations
- Place mocks in `mock.go`
- Always include benchmark tests (`*_benchmark_test.go`)
- Register in `pkg/provider/builtin/builtin.go`
- Add struct tags with Huma validation on all Descriptor fields

## Package naming

- `pkg/provider/builtin/<name>provider/` (e.g., `envprovider`, `httpprovider`)
- Main file matches the provider name (e.g., `env.go`, `http.go`)
