---
description: "API server layer rules for scafctl. Endpoints are thin handlers -- no business logic. Use chi+Huma, validate with struct tags, test in api_test.go. Use when editing API server packages."
applyTo: "pkg/api/**/*.go"
---

# API Server Layer

API endpoints are **thin handlers** -- they validate input, call domain packages, and return responses.

## Rules

- **No business logic** -- delegate to packages in `pkg/`
- Use chi for routing and Huma for OpenAPI-driven request/response handling
- Use Huma struct tags for request validation (`doc`, `maxLength`, `example`, `required`)
- Use `ServerOption` functional options for server configuration
- Return structured error responses via `pkg/api/errors.go` helpers
- Always add new endpoints to API integration tests (`tests/integration/api_test.go`)
- API features must **only** be tested in `tests/integration/api_test.go`, not in solution integration tests

## Embedder Awareness

- The API server is configurable via `ServerOption` functions
- Use `settings.Run.BinaryName` for any user-facing strings, not hardcoded `"scafctl"`
- Middleware must be composable -- embedders may add or replace middleware
