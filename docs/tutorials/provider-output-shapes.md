---
title: "Provider Output Shapes"
weight: 18
---

# Provider Output Shapes

Quick reference for what each built-in provider returns in resolver output — the data shape you can access via `_.resolverName` in CEL expressions and Go templates.

## How Provider Output Works

When a resolver uses a provider, the resolved value becomes accessible to other resolvers. The **output shape** determines what fields are available:

```
┌─────────────────────┐         ┌──────────────────┐
│ Resolver: api_data  │         │ Resolver: summary │
│ provider: http      │  ───►   │ expr: _.api_data  │
│ output: {           │         │   .body.items     │
│   body: {...}       │         │   | size()        │
│   statusCode: 200   │         └──────────────────┘
│   headers: {...}    │
│ }                   │
└─────────────────────┘
```

> **Tip:** Use the MCP tool `get_provider_output_shape` to query output schemas programmatically, or `get_provider_schema` for full provider documentation.

---

## Core Providers

### `static`
Returns the literal value as-is.

| Capability | Output | Type |
|-----------|--------|------|
| from | `<value>` | any |
| transform | `<value>` | any |

```yaml
# Output is whatever you set as the value
resolve:
  with:
    - provider: static
      inputs:
        value: "hello"
# _.my_resolver == "hello"
```

### `cel`
Returns the result of evaluating a CEL expression.

| Capability | Output | Type |
|-----------|--------|------|
| from | `<expression result>` | any |
| transform | `<expression result>` | any |

```yaml
# Output is the CEL evaluation result
resolve:
  with:
    - provider: cel
      inputs:
        expression: "size(_.items)"
# _.my_resolver == 42  (or whatever the expression returns)
```

### `parameter`
Returns the value from CLI `-r key=value` flags.

| Capability | Output | Type |
|-----------|--------|------|
| from | `<parameter value>` | string |

### `env`
Returns the value of an environment variable.

| Capability | Output | Type |
|-----------|--------|------|
| from | `<env value>` | string |

---

## Data Providers

### `http`

| Capability | Fields | Type | Description |
|-----------|--------|------|-------------|
| from / transform | `body` | any (string or parsed JSON if autoParseJson) | Response body |
| | `statusCode` | int | HTTP status code |
| | `headers` | map[string]string | Response headers |
| action | `body` | any | Response body |
| | `statusCode` | int | HTTP status code |
| | `headers` | map[string]string | Response headers |
| | `success` | bool | Whether statusCode < 400 |

```yaml
# Access HTTP response fields
transform:
  with:
    - provider: cel
      inputs:
        expression: "_.api_data.body.items[0].name"
```

### `github`

Uses GitHub GraphQL API for reads and mutations, and REST API for releases.

| Capability | Fields | Type | Description |
|-----------|--------|------|-------------|
| from / transform | `result` | any | API response (structure varies by operation) |
| action | `success` | bool | Whether the write operation succeeded |
| | `operation` | string | The operation that was performed |
| | `result` | any | API response data |
| | `error` | string | Error message if failed |

**Read operations (`from` / `transform`) — `result` shapes:**

| Operation | `result` type | Key fields |
|-----------|--------------|------------|
| `get_repo` | object | `name`, `full_name`, `description`, `default_branch`, `isPrivate`, `url`, `stargazerCount` |
| `get_file` | object | `name`, `content` (plain text), `sha`, `size` |
| `list_releases` | array | Each: `tagName`, `name`, `description`, `publishedAt`, `isPrerelease` |
| `get_latest_release` | object | `tagName`, `name`, `description`, `publishedAt` |
| `list_pull_requests` | array | Each: `number`, `title`, `state`, `author`, `createdAt`, `headRefName` |
| `get_pull_request` | object | `number`, `title`, `state`, `body`, `author`, `mergeable`, `isDraft` |
| `list_issues` | array | Each: `number`, `title`, `state`, `author`, `createdAt` |
| `get_issue` | object | `number`, `title`, `state`, `body`, `author`, `labels` |
| `list_issue_comments` | array | Each: `body`, `author`, `createdAt` |
| `list_branches` | array | Each: `name`, `target.oid` |
| `get_branch` | object | `name`, `target.oid` |
| `list_tags` | array | Each: `name`, `target.oid` |
| `get_head_oid` | object | `oid`, `branch` |

**Write operations (`action`) — `result` shapes:**

| Operation | `result` key fields |
|-----------|--------------------|
| `create_issue` | `id`, `number`, `title`, `url`, `state` |
| `create_pull_request` | `id`, `number`, `title`, `url`, `headRefName`, `baseRefName` |
| `create_commit` | `oid`, `url`, `message`, `signature.isValid` |
| `create_branch` | `name`, `target.oid` |
| `create_release` | `id`, `tag_name`, `name`, `url` (REST) |

```yaml
# Access GitHub API results (read)
transform:
  with:
    - provider: cel
      inputs:
        expression: "_.repo_info.result.default_branch"

# Check write operation success (action)
transform:
  with:
    - provider: cel
      inputs:
        expression: "_.commit_result.success && _.commit_result.result.signature.isValid"
```

### `file`
Returns file content.

| Capability | Fields | Type | Description |
|-----------|--------|------|-------------|
| from | `content` | string | Raw file content |
| | `path` | string | Resolved file path |
| | `size` | int | File size in bytes |

### `directory`
Returns directory listings.

| Capability | Fields | Type | Description |
|-----------|--------|------|-------------|
| from | `entries` | array | Directory entries |
| | `path` | string | Directory path |

Each entry: `{ name, path, isDir, size, modTime }`

---

## Processing Providers

### `exec`

| Capability | Fields | Type | Description |
|-----------|--------|------|-------------|
| from / transform | `stdout` | string | Standard output |
| | `stderr` | string | Standard error |
| | `exitCode` | int | Exit code |
| | `command` | string | Full command string |
| | `shell` | string | Shell used |
| action | `success` | bool | Whether exit code is 0 |
| | `stdout` | string | Standard output |
| | `stderr` | string | Standard error |
| | `exitCode` | int | Exit code |
| | `command` | string | Full command string |
| | `shell` | string | Shell used |

```yaml
# Parse exec output
transform:
  with:
    - provider: cel
      inputs:
        expression: "_.git_info.stdout.trim()"
```

### `go-template`
Returns rendered template output.

| Capability | Fields | Type | Description |
|-----------|--------|------|-------------|
| transform | `<rendered text>` | string | Template output as string |

### `hcl`
Varies by operation — parse returns structured data, format/validate return strings.

### `validation`

| Capability | Fields | Type | Description |
|-----------|--------|------|-------------|
| validation | `valid` | bool | Always `true` on success |
| | `details` | string | `"all validations passed"` |

---

## Discovering Output Shapes

### CLI

```bash
# View full provider info including output schemas
scafctl get provider http -o json

# View all providers
scafctl get providers
```

### MCP Tools

```
# Get output shape for a specific provider
get_provider_output_shape(name: "http", capability: "from")

# Get full provider docs
get_provider_schema(name: "http")
```

### In CEL Expressions

When writing CEL that references resolver output, the shape depends on the provider used:

```yaml
# HTTP provider → access .body, .statusCode, .headers
expression: "_.api_call.body.count > 0"

# Exec provider → access .stdout, .exitCode
expression: "_.cmd_result.exitCode == 0"

# Static/CEL provider → direct value
expression: "_.my_string == 'expected'"
```

---

## Next Steps

- [Provider Reference](provider-reference.md) — complete documentation for all providers
- [CEL Expressions Tutorial](cel-tutorial.md) — use output fields in expressions
- [Resolver Tutorial](resolver-tutorial.md) — chain resolvers using output data
