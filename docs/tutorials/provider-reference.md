---
title: "Provider Reference"
weight: 110
---

# Provider Reference

This document provides a reference for all built-in providers in scafctl.

> **Note:** All YAML examples in this reference show only the relevant resolver or action snippet. To use them, place each snippet inside a complete solution file with `apiVersion`, `kind`, `metadata`, and `spec` sections. See the [Getting Started](getting-started.md) tutorial for the full solution structure.

## Overview

Providers are execution primitives used by resolvers and actions. Each provider has **capabilities** that determine where it can be used:

| Capability | Used In | Description |
|------------|---------|-------------|
| `from` | Resolver `resolve.with` | Fetch or generate data |
| `transform` | Resolver `transform.with` | Transform data |
| `validation` | Resolver `validate.with` | Validate data |
| `action` | Action `provider` | Perform side effects |
| `authentication` | HTTP auth | Provide authentication |

## Capabilities Matrix

| Provider | from | transform | validation | action |
|----------|:----:|:---------:|:----------:|:------:|
| [cel](#cel) | ❌ | ✅ | ❌ | ✅ |
| [debug](#debug) | ✅ | ✅ | ✅ | ✅ |
| [directory](#directory) | ✅ | ❌ | ❌ | ✅ |
| [env](#env) | ✅ | ❌ | ❌ | ❌ |
| [exec](#exec) | ✅ | ✅ | ❌ | ✅ |
| [file](#file) | ✅ | ✅ | ❌ | ✅ |
| [git](#git) | ✅ | ❌ | ❌ | ✅ |
| [github](#github) | ✅ | ✅ | ❌ | ❌ |
| [go-template](#go-template) | ❌ | ✅ | ❌ | ✅ |
| [hcl](#hcl) | ✅ | ✅ | ❌ | ❌ |
| [http](#http) | ✅ | ✅ | ❌ | ✅ |
| [identity](#identity) | ✅ | ❌ | ❌ | ❌ |
| [metadata](#metadata) | ✅ | ❌ | ❌ | ❌ |
| [parameter](#parameter) | ✅ | ❌ | ❌ | ❌ |
| [secret](#secret) | ✅ | ❌ | ❌ | ❌ |
| [sleep](#sleep) | ✅ | ✅ | ✅ | ✅ |
| [static](#static) | ✅ | ✅ | ❌ | ❌ |
| [validation](#validation) | ❌ | ✅ | ✅ | ❌ |

---

## cel

Transform and evaluate data using CEL (Common Expression Language) expressions.

### Capabilities

`transform`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `expression` | string | ✅ | CEL expression to evaluate. Resolver data available under `_` |
| `variables` | any | ❌ | Additional variables for the CEL context |

### Output

Returns the evaluation result (any type).

### Examples

```yaml
# Transform: uppercase a string
transform:
  with:
    - provider: cel
      inputs:
        expression: "__self.toUpperCase()"

# Action: compute a value
provider: cel
inputs:
  expression: "_.items.map(i, i.price).sum()"
```

---

## debug

Debugging provider for inspecting resolver data during workflow execution.

### Capabilities

`from`, `transform`, `validation`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `expression` | string | ❌ | CEL expression to filter/transform data before output |
| `label` | string | ❌ | Label or message for debug output context |
| `format` | string | ❌ | Output format: `yaml`, `json`, `pretty` (default: `yaml`) |
| `destination` | string | ❌ | Where to output: `stdout`, `stderr`, `file` (default: `stdout`) |
| `path` | string | ❌ | File path when destination is `file` |
| `colorize` | bool | ❌ | Whether to colorize terminal output |

### Examples

```yaml
# Debug all resolver data
resolve:
  with:
    - provider: debug
      inputs:
        label: "Resolver Context"

# Debug specific value
transform:
  with:
    - provider: debug
      inputs:
        expression: "_.config"
        format: json
```

---

## directory

Directory operations: listing contents with filtering, creating, removing, and copying directories.

### Capabilities

`from`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `list`, `mkdir`, `rmdir`, `copy` |
| `path` | string | ✅ | Target directory path (absolute or relative) |
| `recursive` | bool | ❌ | Enable recursive directory traversal (default: `false`) |
| `maxDepth` | int | ❌ | Maximum recursion depth, 1–50 (default: `10`) |
| `includeContent` | bool | ❌ | Read and include file contents in output (default: `false`) |
| `maxFileSize` | int | ❌ | Maximum file size in bytes for content reading (default: `1048576`) |
| `filterGlob` | string | ❌ | Glob pattern to filter entries (e.g., `*.go`). Mutually exclusive with `filterRegex` |
| `filterRegex` | string | ❌ | Regex to filter entry names. Mutually exclusive with `filterGlob` |
| `excludeHidden` | bool | ❌ | Exclude hidden files/directories (names starting with `.`) |
| `checksum` | string | ❌ | Compute checksum for files: `md5`, `sha256`, `sha512` (requires `includeContent`) |
| `createDirs` | bool | ❌ | Create parent directories for `mkdir` (like `mkdir -p`) |
| `destination` | string | ❌ | Destination path for `copy` operation |
| `force` | bool | ❌ | Force removal of non-empty directories for `rmdir` |

### Output (list)

| Field | Type | Description |
|-------|------|-------------|
| `entries` | array | List of directory entries |
| `entries[].path` | string | Relative path from the listed directory |
| `entries[].absolutePath` | string | Absolute filesystem path |
| `entries[].name` | string | File or directory name |
| `entries[].extension` | string | File extension including dot (e.g., `.go`) |
| `entries[].size` | int | Size in bytes |
| `entries[].isDir` | bool | Whether entry is a directory |
| `entries[].type` | string | Entry type: `file` or `dir` |
| `entries[].mode` | string | File permission mode (e.g., `0644`) |
| `entries[].modTime` | string | Last modification time (RFC3339) |
| `entries[].mimeType` | string | MIME type based on extension |
| `entries[].content` | string | File content (when `includeContent` is true) |
| `entries[].contentEncoding` | string | `text` or `base64` |
| `entries[].checksum` | string | File checksum (when `checksum` is specified) |
| `entries[].checksumAlgorithm` | string | Algorithm used |
| `totalCount` | int | Total number of entries |
| `dirCount` | int | Number of directories |
| `fileCount` | int | Number of files |
| `totalSize` | int | Total size of all files in bytes |
| `basePath` | string | Absolute path of the listed directory |

### Output (mkdir, rmdir, copy)

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool | Whether the operation succeeded |
| `operation` | string | Operation that was performed |
| `path` | string | Absolute path of the target directory |

### Examples

```yaml
# List directory contents
resolve:
  with:
    - provider: directory
      inputs:
        operation: list
        path: ./src

# Recursively find all Go files
resolve:
  with:
    - provider: directory
      inputs:
        operation: list
        path: ./pkg
        recursive: true
        filterGlob: "*.go"
        excludeHidden: true

# List with file contents and checksums
resolve:
  with:
    - provider: directory
      inputs:
        operation: list
        path: ./config
        recursive: true
        includeContent: true
        filterGlob: "*.yaml"
        checksum: sha256
        maxFileSize: 524288

# Create nested directory structure
provider: directory
inputs:
  operation: mkdir
  path: ./output/reports/2026
  createDirs: true

# Force-remove a directory
provider: directory
inputs:
  operation: rmdir
  path: ./tmp/build-output
  force: true

# Copy a directory tree
provider: directory
inputs:
  operation: copy
  path: ./config
  destination: ./config-backup
```

---

## env

Read environment variables.

### Capabilities

`from`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `get`, `list` |
| `name` | string | ❌ | Variable name (required for `get`) |
| `default` | string | ❌ | Default value if variable not set |
| `prefix` | string | ❌ | Filter variables by prefix (for `list`) |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `value` | string | Variable value (for `get`) |
| `variables` | map | Key-value pairs (for `list`) |
| `found` | bool | Whether the variable exists |

### Examples

```yaml
# Get environment variable with default
resolve:
  with:
    - provider: env
      inputs:
        operation: get
        name: DATABASE_URL
        default: "postgres://localhost/dev"

# List all vars with prefix
resolve:
  with:
    - provider: env
      inputs:
        operation: list
        prefix: "APP_"
```

---

## exec

Execute shell commands using an embedded cross-platform POSIX shell interpreter. Commands work identically on Linux, macOS, and Windows without requiring external shell binaries. Supports pipes, redirections, variable expansion, command substitution, and common coreutils on all platforms. Optionally use external shells (bash, pwsh, cmd) for platform-specific features.

### Capabilities

`from`, `transform`, `action`

### Inputs

| Field | Type | Required | Default | Description |
|-------|------|:--------:|:-------:|-------------|
| `command` | string | ✅ | — | Command to execute. Supports POSIX shell syntax including pipes, redirections, variable expansion, and command substitution by default |
| `args` | array | ❌ | — | Additional arguments appended to the command. Arguments are automatically shell-quoted for safety |
| `stdin` | string | ❌ | — | Standard input to provide to the command |
| `workingDir` | string | ❌ | — | Working directory for command execution |
| `env` | object | ❌ | — | Environment variables to set (key-value pairs). Merged with the parent process environment |
| `timeout` | int | ❌ | — | Timeout in seconds (0 or omit for no timeout, max 3600) |
| `shell` | string | ❌ | `auto` | Shell interpreter to use: `auto` (embedded POSIX shell — works on all platforms), `sh` (alias for auto), `bash` (external bash), `pwsh` (external PowerShell Core), `cmd` (external cmd.exe — Windows only) |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `stdout` | string | Standard output |
| `stderr` | string | Standard error |
| `exitCode` | int | Exit code |
| `success` | bool | Whether command succeeded (exit code 0) — action capability only |
| `command` | string | The full command that was executed |
| `shell` | string | The shell interpreter that was used |

### Shell Modes

| Value | Description | Platform |
|-------|-------------|----------|
| `auto` | Embedded POSIX shell (default). Pure Go — no external shell binary required. Supports pipes, redirections, variable expansion, command substitution, and Go-native coreutils on Windows. | All |
| `sh` | Alias for `auto` | All |
| `bash` | External bash binary from `$PATH`. Use for bash-specific features (globstar, arrays, etc.) | Linux, macOS |
| `pwsh` | External PowerShell Core from `$PATH`. Use for PowerShell cmdlets and Windows administration | All (where pwsh is installed) |
| `cmd` | External cmd.exe. Use for Windows batch commands | Windows |

### Examples

```yaml
# Simple command — pipes and shell features work by default
provider: exec
inputs:
  command: "echo 'Hello, World!'"

# Command with arguments (automatically shell-quoted)
provider: exec
inputs:
  command: "echo"
  args: ["Hello", "World"]

# Shell pipeline — works on all platforms
provider: exec
inputs:
  command: "echo 'hello world' | tr a-z A-Z"

# With environment variables and working directory
provider: exec
inputs:
  command: "./deploy.sh"
  workingDir: "/opt/app"
  env:
    ENVIRONMENT: production
  timeout: 300

# PowerShell command
provider: exec
inputs:
  command: "Get-ChildItem | Select-Object Name"
  shell: pwsh

# External bash for bash-specific features
provider: exec
inputs:
  command: "shopt -s globstar; echo **/*.go"
  shell: bash
```

---

## file

Filesystem operations: read, write, check existence, delete.

### Capabilities

`from`, `transform`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `read`, `write`, `exists`, `delete` |
| `path` | string | ✅ | File path (absolute or relative) |
| `content` | string | ❌ | Content to write (required for `write`) |
| `createDirs` | bool | ❌ | Create parent directories if missing |
| `encoding` | string | ❌ | File encoding: `utf-8`, `binary` (default: `utf-8`) |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `content` | string | File content (for `read`) |
| `exists` | bool | Whether file exists |
| `size` | int | File size in bytes |
| `success` | bool | Operation success (action only) |

### Examples

```yaml
# Read file
resolve:
  with:
    - provider: file
      inputs:
        operation: read
        path: "./config.json"

# Write file
provider: file
inputs:
  operation: write
  path: "./output/result.txt"
  content:
    expr: "_.processedData"
  createDirs: true

# Check if file exists
resolve:
  with:
    - provider: file
      inputs:
        operation: exists
        path: "./optional-config.yaml"
```

---

## git

Git version control operations.

### Capabilities

`from`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `clone`, `pull`, `status`, `add`, `commit`, `push`, `checkout`, `branch`, `log`, `tag` |
| `url` | string | ❌ | Repository URL (for `clone`) |
| `path` | string | ❌ | Local repository path |
| `branch` | string | ❌ | Branch name |
| `message` | string | ❌ | Commit message |
| `files` | array | ❌ | Files to add |
| `tag` | string | ❌ | Tag name |
| `remote` | string | ❌ | Remote name (default: `origin`) |
| `depth` | int | ❌ | Clone depth for shallow clone |
| `username` | string | ❌ | Username for authentication |
| `password` | string | ❌ | Password/token (secret) |
| `force` | bool | ❌ | Force the operation |

### Examples

```yaml
# Clone repository
provider: git
inputs:
  operation: clone
  url: https://github.com/org/repo.git
  path: ./repo
  depth: 1

# Commit and push
provider: git
inputs:
  operation: commit
  path: ./repo
  message:
    expr: "'Release ' + _.version"

# Then push
provider: git
inputs:
  operation: push
  path: ./repo
```

---

## github

Retrieve data from the GitHub REST API. Supports repository info, file content, releases, and pull requests. Uses the configured GitHub auth handler automatically.

> For arbitrary HTTP requests to GitHub, use the `http` provider with `auth: github`.

### Capabilities

`from`, `transform`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | API operation: `get_repo`, `get_file`, `list_releases`, `get_latest_release`, `list_pull_requests`, `get_pull_request` |
| `owner` | string | ✅ | Repository owner (user or organization) |
| `repo` | string | ✅ | Repository name |
| `path` | string | ❌ | File path within the repository (for `get_file`) |
| `ref` | string | ❌ | Git reference (branch, tag, or commit SHA). Defaults to repo's default branch |
| `number` | int | ❌ | Pull request number (for `get_pull_request`) |
| `state` | string | ❌ | Filter by state: `open`, `closed`, `all` (default: `open`) |
| `per_page` | int | ❌ | Results per page, 1–100 (default: 30) |
| `api_base` | string | ❌ | GitHub API base URL (default: `https://api.github.com`). Set for GitHub Enterprise. |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `result` | any | API response — structure varies by operation (see below) |

**Operation output shapes:**

| Operation | `result` type | Key fields |
|-----------|--------------|------------|
| `get_repo` | object | `name`, `full_name`, `description`, `default_branch`, `private`, `html_url` |
| `get_file` | object | `name`, `content`, `decoded_content`, `sha`, `size`, `type` |
| `list_releases` | array | Each: `tag_name`, `name`, `body`, `published_at`, `prerelease` |
| `get_latest_release` | object | `tag_name`, `name`, `body`, `published_at` |
| `list_pull_requests` | array | Each: `number`, `title`, `state`, `user`, `created_at` |
| `get_pull_request` | object | `number`, `title`, `state`, `body`, `user`, `mergeable` |

### Examples

```yaml
# Get repository info
resolve:
  with:
    - provider: github
      inputs:
        operation: get_repo
        owner: octocat
        repo: hello-world

# Get file content from a specific branch
transform:
  with:
    - provider: github
      inputs:
        operation: get_file
        owner: octocat
        repo: hello-world
        path: README.md
        ref: main

# Get latest release
resolve:
  with:
    - provider: github
      inputs:
        operation: get_latest_release
        owner: cli
        repo: cli

# List open pull requests
resolve:
  with:
    - provider: github
      inputs:
        operation: list_pull_requests
        owner: golang
        repo: go
        state: open
        per_page: 10
```

---

## go-template

Transform data using Go text/template syntax.

### Capabilities

`transform`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `template` | string | ✅ | Go template content |
| `name` | string | ✅ | Template name (for error messages) |
| `missingKey` | string | ❌ | Behavior for missing keys: `default`, `zero`, `error` |
| `leftDelim` | string | ❌ | Left delimiter (default: `{{`) |
| `rightDelim` | string | ❌ | Right delimiter (default: `}}`) |
| `data` | any | ❌ | Additional data to merge with resolver context |
| `ignoredBlocks` | array | ❌ | Blocks to pass through literally without template processing. Each entry uses EITHER `{ start, end }` markers (multi-line) OR `{ line }` marker (single-line). Content is preserved as-is. |

### Output

Returns the rendered template as a string.

### Ignored Blocks

Use `ignoredBlocks` to bypass template rendering for specific sections. This is useful when templates contain syntax that conflicts with Go template delimiters (e.g., Terraform `${}`, Helm `{{ }}`, GitHub Actions `${{ }}`).

#### Start/End Mode (Multi-line)

Define `start` and `end` markers. All content between matched markers (inclusive) is preserved:

```yaml
transform:
  with:
    - provider: go-template
      inputs:
        name: terraform-config
        template: |
          resource "aws_instance" "main" {
            ami           = "{{ .ami }}"
            instance_type = "{{ .instanceType }}"
            /*scafctl:ignore:start*/
            tags = {
              Name = "${var.name}"
            }
            /*scafctl:ignore:end*/
          }
        ignoredBlocks:
          - start: "/*scafctl:ignore:start*/"
            end: "/*scafctl:ignore:end*/"
```

#### Line Mode (Single-line)

Define a `line` marker. Every line containing that substring is preserved literally:

```yaml
transform:
  with:
    - provider: go-template
      inputs:
        name: workflow-config
        template: |
          name: Deploy {{ .appName }}
          steps:
            - run: echo ${{ secrets.TOKEN }}  # scafctl:ignore
            - run: echo "deployed"
        ignoredBlocks:
          - line: "# scafctl:ignore"
```

> **Note:** `line` and `start`/`end` are mutually exclusive within a single entry, but different entries can use different modes.

The content between `start` and `end` markers (including the markers themselves) passes through unchanged. For `line` mode, the entire line containing the marker is preserved.

### Examples

```yaml
# Render template
transform:
  with:
    - provider: go-template
      inputs:
        name: config
        template: |
          server:
            host: {{ .host }}
            port: {{ .port }}
            env: {{ .environment }}
```

---

## hcl

Process HCL (HashiCorp Configuration Language) content. Supports four operations: `parse` (default) extracts structured block information; `format` canonically formats; `validate` checks syntax; `generate` produces HCL from structured input. Accepts single files, multiple paths, or a directory of `.tf` files.

### Capabilities

`from`, `transform`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ❌ | `parse` (default), `format`, `validate`, or `generate` |
| `content` | string | ❌ | Raw HCL content to process |
| `path` | string | ❌ | Path to a single HCL file |
| `paths` | array | ❌ | Array of HCL file paths (merged for parse; per-file for format/validate) |
| `dir` | string | ❌ | Directory path — all `.tf`/`.tf.json` files are processed |
| `blocks` | object | ❌ | Structured block data for `generate` (same schema as parse output) |
| `output_format` | string | ❌ | Generation output format: `hcl` (default) or `json` (Terraform JSON syntax `.tf.json`) |

**Source selection:** For `parse`/`format`/`validate`, provide exactly one of `content`, `path`, `paths`, or `dir` (mutually exclusive). For `generate`, use `blocks` and optionally `output_format`.

### Output — `parse` (default)

| Field | Type | Description |
|-------|------|-------------|
| `variables` | array | Variable blocks (name, type, default, description, sensitive, validation) |
| `resources` | array | Resource blocks (type, name, attributes, sub-blocks) |
| `data` | array | Data source blocks (type, name, attributes, sub-blocks) |
| `modules` | array | Module blocks (name, source, version, attributes) |
| `outputs` | array | Output blocks (name, value, description, sensitive) |
| `locals` | map | Local values merged across all `locals` blocks |
| `providers` | array | Provider configuration blocks (name, alias, attributes) |
| `terraform` | object | Terraform block (required_version, required_providers, backend, cloud) |
| `moved` | array | Moved blocks (from, to) |
| `import` | array | Import blocks (to, id, provider) |
| `check` | array | Check blocks (name, data, assertions) |

When multiple files are parsed (`paths` or `dir`), results are merged: arrays are concatenated, `locals` and `terraform` maps are merged (last-file-wins for conflicting keys).

### Output — `format`

| Field | Type | Description |
|-------|------|-------------|
| `formatted` | string | The canonically formatted HCL content (single file) |
| `changed` | bool | `true` if the formatter modified the content |

Multi-file format returns `{ files: [{filename, formatted, changed}, ...], changed: bool }`.

### Output — `validate`

| Field | Type | Description |
|-------|------|-------------|
| `valid` | bool | `true` if no syntax errors were found |
| `error_count` | int | Number of error-level diagnostics |
| `diagnostics` | array | Diagnostic entries with severity, summary, detail, range |

Multi-file validate returns `{ valid: bool, error_count: int, files: [{filename, valid, error_count, diagnostics}, ...] }`.

### Output — `generate`

| Field | Type | Description |
|-------|------|-------------|
| `hcl` | string | Generated HCL text (native HCL syntax or Terraform JSON depending on `output_format`) |

**Metadata** includes `output_format` (`hcl` or `json`) indicating which format was produced.

### Examples

```yaml
# Parse inline HCL content (operation defaults to "parse")
resolve:
  with:
    - provider: hcl
      inputs:
        content: |
          variable "region" {
            type    = string
            default = "us-east-1"
          }

# Parse an HCL file
resolve:
  with:
    - provider: hcl
      inputs:
        path: ./main.tf

# Parse all .tf files in a directory (results merged)
resolve:
  with:
    - provider: hcl
      inputs:
        dir: ./terraform

# Parse multiple specific files
resolve:
  with:
    - provider: hcl
      inputs:
        paths:
          - ./main.tf
          - ./variables.tf
          - ./outputs.tf

# Transform: parse HCL from another resolver's output
transform:
  with:
    - provider: hcl
      inputs:
        content: "{{ .resolvers.tfFile.content }}"

# Format inline HCL content
resolve:
  with:
    - provider: hcl
      inputs:
        operation: format
        content: |
          variable "region" {
          type=string
          default="us-east-1"
          }

# Format all files in a directory
resolve:
  with:
    - provider: hcl
      inputs:
        operation: format
        dir: ./terraform

# Validate HCL syntax
resolve:
  with:
    - provider: hcl
      inputs:
        operation: validate
        path: ./main.tf

# Generate HCL from structured data
resolve:
  with:
    - provider: hcl
      inputs:
        operation: generate
        blocks:
          variables:
            - name: region
              type: string
              default: us-east-1
              description: "AWS region"

# Generate Terraform JSON (.tf.json) from structured data
resolve:
  with:
    - provider: hcl
      inputs:
        operation: generate
        output_format: json
        blocks:
          variables:
            - name: region
              type: string
              default: us-east-1
          resources:
            - type: aws_instance
              name: web
              attributes:
                ami: ami-12345
                instance_type: t3.micro
```

---

## http

HTTP client for API calls with built-in pagination support for fetching data across multiple pages.

### Capabilities

`from`, `transform`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `url` | string | ✅ | URL to request |
| `method` | string | ❌ | HTTP method (default: `GET`) |
| `headers` | object | ❌ | HTTP headers |
| `body` | string | ❌ | Request body |
| `timeout` | int | ❌ | Timeout in seconds (max 300) |
| `retry` | object | ❌ | Retry configuration |
| `auth` | string | ❌ | Auth provider (e.g., `entra`, `github`) |
| `scope` | string | ❌ | OAuth scope for authentication |
| `pagination` | object | ❌ | Pagination configuration (see below) |
| `autoParseJson` | bool | ❌ | Parse response body as JSON when Content-Type is `application/json`. Enables direct field access (e.g., `_.result.body.items`) |
| `poll` | object | ❌ | Polling configuration — re-execute request until a condition is met (see below) |

### Pagination

The `pagination` input enables automatic multi-page fetching. Five strategies are supported to cover different API pagination patterns.

#### Pagination Fields

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `strategy` | string | ✅ | One of: `offset`, `pageNumber`, `cursor`, `linkHeader`, `custom` |
| `maxPages` | int | ✅ | Safety limit for max pages to fetch (default: 100, max: 10000) |
| `collectPath` | string | ❌ | CEL expression to extract items from each response (e.g., `body.items`) |
| `stopWhen` | string | ❌ | CEL expression; if true, stop paginating (e.g., `size(body.items) == 0`) |

**CEL variables available** in `collectPath`, `stopWhen`, and strategy-specific expressions:

| Variable | Type | Description |
|----------|------|-------------|
| `statusCode` | int | HTTP response status code |
| `body` | any | Parsed JSON response body |
| `rawBody` | string | Raw response body string |
| `headers` | object | Response headers |
| `page` | int | Current page number (1-based) |

#### Strategy: `offset`

Increments an offset query parameter each page.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | *(required)* | Page size |
| `offsetParam` | string | `offset` | Query parameter name for offset |
| `limitParam` | string | `limit` | Query parameter name for limit |

#### Strategy: `pageNumber`

Increments a page number query parameter each page.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pageSize` | int | *(required)* | Page size |
| `pageParam` | string | `page` | Query parameter name for page number |
| `pageSizeParam` | string | `pageSize` | Query parameter name for page size |
| `startPage` | int | `1` | Starting page number |

#### Strategy: `cursor`

Extracts a cursor token or next URL from the response to fetch subsequent pages.

| Field | Type | Description |
|-------|------|-------------|
| `nextTokenPath` | string | CEL expression to extract cursor from response (e.g., `body.nextCursor`) |
| `nextTokenParam` | string | Query parameter to set with the cursor value (required with `nextTokenPath`) |
| `nextURLPath` | string | CEL expression to extract the full next page URL (e.g., `body['@odata.nextLink']`). Alternative to `nextTokenPath`. |

Use `nextTokenPath` + `nextTokenParam` for APIs that return a token. Use `nextURLPath` for APIs that return a full URL (e.g., Microsoft Graph `@odata.nextLink`).

#### Strategy: `linkHeader`

Follows `rel="next"` links in the `Link` response header (RFC 8288). Used by GitHub, GitLab, and other REST APIs. No additional configuration needed.

#### Strategy: `custom`

Full control using CEL expressions.

| Field | Type | Description |
|-------|------|-------------|
| `nextURL` | string | CEL expression returning the full next page URL (empty string = stop) |
| `nextParams` | string | CEL expression returning a map of query params for the next request (empty map = stop) |

### Polling

The `poll` input enables re-executing the request until a response condition is met. This is different from `retry` (which handles transient failures) — polling re-executes on successful responses until the content matches expectations.

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `until` | string | ✅ | CEL expression evaluated against the response. Polling stops when this returns `true`. Available variables: `body` (parsed if JSON), `statusCode`, `headers` |
| `failWhen` | string | ❌ | CEL expression that triggers immediate failure (e.g., terminal error states) |
| `interval` | string | ❌ | Duration between polls (default: `5s`). Format: `1s`, `30s`, `2m` |
| `maxAttempts` | int | ❌ | Maximum number of poll attempts (default: 60) |

```yaml
# Wait for deployment to complete
resolve:
  with:
    - provider: http
      inputs:
        url: https://api.example.com/deployments/123/status
        method: GET
        auth: entra
        autoParseJson: true
        poll:
          until: 'body.status == "succeeded"'
          failWhen: 'body.status == "failed"'
          interval: 10s
          maxAttempts: 30
```

### Output

| Field | Type | Description |
|-------|------|-------------|
| `statusCode` | int | HTTP status code (last page when paginating) |
| `body` | any | Response body as string, or parsed JSON object when `autoParseJson: true`. When paginating with `collectPath`, contains JSON array of all collected items |
| `headers` | object | Response headers (last page when paginating) |
| `success` | bool | Whether request succeeded (action only) |
| `pages` | int | Number of pages fetched (only when paginating) |
| `totalItems` | int | Total items collected across all pages (only when paginating) |

### Examples

```yaml
# GET request
resolve:
  with:
    - provider: http
      inputs:
        url: https://api.example.com/config
        headers:
          Accept: application/json

# POST with body
provider: http
inputs:
  url: https://api.example.com/deploy
  method: POST
  headers:
    Content-Type: application/json
  body:
    expr: 'toJson({"image": _.image, "env": _.environment})'
  timeout: 60

# With retry
provider: http
inputs:
  url: https://api.example.com/status
  retry:
    maxAttempts: 3
    backoff: exponential
    initialDelay: 1s

# Authenticated GitHub API request
resolve:
  with:
    - provider: http
      inputs:
        url: https://api.github.com/user/repos
        headers:
          Accept: application/json
        auth: github
        scope: repo

# Cursor pagination (token-based)
provider: http
inputs:
  url: https://api.example.com/items
  pagination:
    strategy: cursor
    maxPages: 10
    nextTokenPath: "body.nextCursor"
    nextTokenParam: "cursor"
    collectPath: "body.items"
    stopWhen: "body.nextCursor == null"

# Cursor pagination (OData / Microsoft Graph nextLink)
provider: http
inputs:
  url: https://graph.microsoft.com/v1.0/users?$top=100
  authProvider: entra
  scope: "https://graph.microsoft.com/.default"
  pagination:
    strategy: cursor
    maxPages: 50
    nextURLPath: "body['@odata.nextLink']"
    collectPath: "body.value"

# Link header pagination (GitHub-style)
provider: http
inputs:
  url: https://api.github.com/users/octocat/repos?per_page=30
  headers:
    Accept: application/vnd.github+json
  pagination:
    strategy: linkHeader
    maxPages: 5
    collectPath: "body"

# Offset pagination
provider: http
inputs:
  url: https://api.example.com/records
  pagination:
    strategy: offset
    maxPages: 20
    limit: 50
    collectPath: "body.records"
    stopWhen: "size(body.records) < 50"

# Page number pagination
provider: http
inputs:
  url: https://api.example.com/products
  pagination:
    strategy: pageNumber
    maxPages: 10
    pageSize: 25
    pageParam: "page"
    pageSizeParam: "per_page"
    collectPath: "body.products"
    stopWhen: "size(body.products) == 0"

# Custom pagination with CEL expressions
provider: http
inputs:
  url: https://api.example.com/search?q=test
  pagination:
    strategy: custom
    maxPages: 10
    nextURL: "has(body.links) && has(body.links.next) ? body.links.next : ''"
    collectPath: "body.results"
    stopWhen: "!has(body.links) || !has(body.links.next)"
```

---

## identity

Get authentication identity information without exposing tokens.

### Capabilities

`from`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `status`, `claims`, `list` |
| `handler` | string | ❌ | Auth handler name (e.g., `entra`, `github`) |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `authenticated` | bool | Whether authenticated |
| `identityType` | string | Type of identity |
| `claims` | object | Token claims |
| `tenantId` | string | Tenant ID |
| `expiresAt` | string | Token expiration |
| `handlers` | array | Available handlers (for `list`) |

### Examples

```yaml
# Check if authenticated (Entra)
resolve:
  with:
    - provider: identity
      inputs:
        operation: status
        handler: entra

# Check if authenticated (GitHub)
resolve:
  with:
    - provider: identity
      inputs:
        operation: status
        handler: github

# Get claims
resolve:
  with:
    - provider: identity
      inputs:
        operation: claims
        handler: entra

# Get GitHub claims (login, name, email)
resolve:
  with:
    - provider: identity
      inputs:
        operation: claims
        handler: github
```

---

## metadata

Return structured metadata about a solution. Accepts arbitrary key-value pairs and returns them as a map, optionally returning a single field by key. Useful for exposing solution name, version, description, tags, and custom attributes to templates and downstream resolvers.

### Capabilities

`from`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `field` | string | ❌ | Return only this single field from the metadata map instead of the full map |
| *(any key)* | any | ❌ | Arbitrary metadata key-value pairs |

### Output

When `field` is set: the value of that specific key.
When `field` is omitted: a map of all provided key-value pairs (excluding `field` itself).

### Examples

**Full metadata map:**

```yaml
resolvers:
  solution-meta:
    type: metadata
    from:
      name: my-solution
      version: 2.1.0
      description: Scaffolds a GCP project
      category: infrastructure
      tags:
        - gcp
        - terraform
```

**Single field extraction:**

```yaml
resolvers:
  solution-name:
    type: metadata
    from:
      field: name
      name: my-solution
      version: 2.1.0
```

---

## parameter

Access CLI parameters passed via `-r` flags.

### Capabilities

`from`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `key` | string | ✅ | Parameter name |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `value` | any | Parameter value |
| `found` | bool | Whether parameter was provided |
| `type` | string | Detected type of value |

### Examples

```yaml
# Get parameter with fallback
resolve:
  with:
    - provider: parameter
      inputs:
        key: environment
    - provider: static
      inputs:
        value: "dev"  # Default if not provided
```

Usage:

```bash
scafctl run solution -f sol.yaml -r environment=production
```

---

## secret

Retrieve encrypted secrets from the scafctl secrets store.

### Capabilities

`from`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `get` or `list` |
| `name` | string | ❌ | Secret name (for `get`) |
| `pattern` | string | ❌ | Regex pattern to match names |
| `required` | bool | ❌ | Error if not found |
| `default` | string | ❌ | Value when not found |

### Examples

```yaml
# Get secret
resolve:
  with:
    - provider: secret
      inputs:
        operation: get
        name: api-key
        required: true

# Get with default
resolve:
  with:
    - provider: secret
      inputs:
        operation: get
        name: optional-key
        default: "fallback-value"
```

Manage secrets via CLI:

```bash
scafctl secrets set api-key "my-secret-value"
scafctl secrets list
```

---

## sleep

Pause execution for a specified duration.

### Capabilities

`from`, `transform`, `validation`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `duration` | string | ✅ | Duration (Go format: `1s`, `500ms`, `2m`) |

### Examples

```yaml
# Wait between API calls
provider: sleep
inputs:
  duration: "2s"
```

---

## static

Return a constant value. Useful for defaults and fallbacks.

### Capabilities

`from`, `transform`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `value` | any | ✅ | Static value to return |

### Examples

```yaml
# Default fallback
resolve:
  with:
    - provider: env
      inputs:
        operation: get
        name: CONFIG_PATH
    - provider: static
      inputs:
        value: "/etc/app/config.yaml"

# Complex default
resolve:
  with:
    - provider: static
      inputs:
        value:
          timeout: 30
          retries: 3
          endpoints:
            - https://primary.example.com
            - https://backup.example.com
```

---

## validation

Validate data using regex patterns and CEL expressions.

### Capabilities

`transform`, `validation`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `value` | string | ❌ | Value to validate (uses `__self` in transform context) |
| `match` | string | ❌ | Regex pattern that must match |
| `notMatch` | string | ❌ | Regex pattern that must NOT match |
| `expression` | string | ❌ | CEL expression that must be true |
| `message` | string | ❌ | Custom error message on failure |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `valid` | bool | Whether validation passed |
| `errors` | array | Validation error messages |
| `details` | string | Failure details |

### Examples

```yaml
# Validate with regex
validate:
  with:
    - provider: validation
      inputs:
        match: "^[a-z][a-z0-9-]+$"
        message: "Name must be lowercase alphanumeric with dashes"

# Validate with CEL
validate:
  with:
    - provider: validation
      inputs:
        expression: "__self in ['dev', 'staging', 'prod']"
        message: "Environment must be dev, staging, or prod"

# Combined validation
validate:
  with:
    - provider: validation
      inputs:
        match: "^v[0-9]+\\.[0-9]+\\.[0-9]+$"
        expression: "!__self.startsWith('v0.')"
        message: "Version must be semver format and >= v1.0.0"

```

## Next Steps

- [Provider Development](provider-development.md) — Build custom providers (builtin and plugin)
- [Auth Handler Development](auth-handler-development.md) — Build custom auth handlers (builtin and plugin)
- [Extension Concepts](extension-concepts.md) — Provider vs Auth Handler vs Plugin terminology
- [Resolver Tutorial](resolver-tutorial.md) — Using providers within resolvers
- [Getting Started](getting-started.md) — Run your first solution
