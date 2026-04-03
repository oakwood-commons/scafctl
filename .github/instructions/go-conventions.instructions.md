---
description: "Go coding conventions for scafctl: struct tags, Huma validation, error handling, design principles, functional options, context/timeouts, and formatting. Use when writing or editing Go code."
applyTo: "**/*.go"
---

# Go Conventions

## Struct Tags

Always add JSON/YAML tags. Use [Huma validation tags](https://huma.rocks/features/request-validation/#validation-tags):
- All fields: `doc`
- Strings: `maxLength`, `example`, `pattern`, `patternDescription`
- Integers: `maximum`, `example`
- Arrays: `maxItems` (no `example`)
- Objects/maps: no `example` tag

## Special Field Types

| Field | Type | Package |
|-------|------|---------|
| `expr` | `Expression` | `pkg/celexp` |
| `tmpl` | `GoTemplatingContent` | `pkg/gotmpl` |

## Error Handling

Always wrap errors with context:

```go
if err != nil {
    return fmt.Errorf("failed to create user: %w", err)
}
```

## Design Principles

- Accept interfaces, return structs
- Keep interfaces small (1-3 methods)
- Define interfaces where they are used, not where they are implemented

## Dependency Injection

Use constructor functions to inject dependencies:

```go
func NewUserService(repo UserRepository, logger Logger) *UserService {
    return &UserService{repo: repo, logger: logger}
}
```

## Functional Options

```go
type Option func(*Server)

func WithPort(port int) Option {
    return func(s *Server) { s.port = port }
}

func NewServer(opts ...Option) *Server {
    s := &Server{port: 8080}
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

## Context & Timeouts

Always use `context.Context` for timeout control:

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
```

## Secret Management

```go
apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    log.Fatal("OPENAI_API_KEY not configured")
}
```

## Formatting

- **gofmt** and **goimports** are mandatory — no style debates
- Never use magic strings or numbers; always define constants or use settings

## Reference

See skill: `golang-patterns` for comprehensive Go idioms and patterns.
