---
description: "Go coding conventions: struct tags, error handling, and formatting."
applyTo: "**/*.go"
---

# Go Conventions

## Struct Tags

Always add JSON/YAML tags on exported structs.

## Error Handling

- Return errors with `fmt.Errorf("context: %w", err)`
- Never panic in library code
- Check all error returns

## Design Principles

- Prefer composition over inheritance
- Keep interfaces small (1-3 methods)
- Accept interfaces, return structs
