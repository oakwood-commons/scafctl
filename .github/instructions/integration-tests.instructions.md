---
description: "Integration test rules for scafctl: CLI, solution, and API test scope boundaries. Use when writing or editing integration tests."
applyTo: "tests/integration/**"
---

# Integration Test Rules

## CLI Integration Tests (`tests/integration/cli_test.go`)

- Always add new commands to the CLI integration tests
- Tests CLI command behavior and output

## Solution Integration Tests (`tests/integration/solutions/`)

- For **non-API** features: providers, resolvers, actions, plugins, solutions
- Always create solution integration tests when a non-API feature, provider, or command is added or updated
- **Cannot** test API/server features -- use API integration tests instead

## API Integration Tests (`tests/integration/api_test.go`)

- For **API/server** features: REST endpoints, middleware, auth, rate limiting
- Always add new API endpoints to the API integration tests
- API features must **only** be tested here, not in solution integration tests

## Scope Boundaries

| Feature Type | Test Location |
|-------------|---------------|
| CLI commands | `tests/integration/cli_test.go` |
| Providers, resolvers, actions, plugins | `tests/integration/solutions/` |
| REST endpoints, middleware, auth | `tests/integration/api_test.go` |
