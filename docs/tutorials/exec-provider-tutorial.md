---
title: "Exec Provider Tutorial"
weight: 94
---

# Exec Provider Tutorial

This tutorial walks you through using the `exec` provider to run shell commands cross-platform. You'll learn how the embedded POSIX shell works, how to use pipes, environment variables, timeouts, and when to use external shells like bash or PowerShell.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with YAML syntax and solution files
- (Optional) `bash` and/or `pwsh` installed for external shell examples

## Table of Contents

1. [How It Works](#how-it-works)
2. [Running Simple Commands](#running-simple-commands)
3. [Pipes and Shell Features](#pipes-and-shell-features)
4. [Arguments](#arguments)
5. [Environment Variables](#environment-variables)
6. [Working Directory](#working-directory)
7. [Standard Input](#standard-input)
8. [Timeouts](#timeouts)
9. [Shell Types](#shell-types)
10. [Cross-Platform Patterns](#cross-platform-patterns)
11. [Error Handling](#error-handling)
12. [Common Patterns](#common-patterns)

---

## How It Works

The exec provider uses an **embedded POSIX shell** (powered by [mvdan.cc/sh](https://github.com/mvdan/sh)) as its default execution engine. This means:

- **No external shell required** — commands run in a pure-Go shell interpreter
- **Cross-platform** — the same command works on Linux, macOS, and Windows
- **Shell features by default** — pipes, redirections, variable expansion, command substitution, conditionals, and loops all work out of the box
- **Go-native coreutils on Windows** — common commands like `cat`, `cp`, `mkdir`, `rm`, `ls` are provided as Go builtins, so they work on Windows without requiring WSL or Git Bash

You can optionally switch to an **external shell** (bash, pwsh, cmd) when you need platform-specific features.

---

## Running Simple Commands

The simplest usage is a single command. Create a file called `simple-exec.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: simple-exec
  version: 1.0.0

spec:
  resolvers: {}
  workflow:
    actions:
      hello:
        provider: exec
        inputs:
          command: "echo 'Hello, World!'"
```

Run it:

```bash
scafctl run solution -f simple-exec.yaml
```

Output:

```
Hello, World!
```

### Output Fields

Every exec action produces these output fields:

| Field | Type | Description |
|-------|------|-------------|
| `stdout` | string | Standard output from the command |
| `stderr` | string | Standard error output |
| `exitCode` | int | The command's exit code (0 = success) |
| `success` | bool | `true` if exitCode is 0 (action capability only) |
| `command` | string | The full command that was executed |
| `shell` | string | Which shell interpreter was used |

You can reference these in downstream actions:

```yaml
# Add these to your solution's workflow.actions section:
actions:
  run-cmd:
    provider: exec
    inputs:
      command: "echo 'hello'"

  use-output:
    provider: exec
    dependsOn: [run-cmd]
    inputs:
      command:
        expr: "'echo Got: ' + __actions['run-cmd'].results.stdout"
```

---

## Pipes and Shell Features

Unlike traditional exec implementations that require a special flag for shell features, the embedded shell handles all POSIX syntax by default.

> **Note:** The YAML snippets in the remaining sections show only the `actions:` block. To run them, place each snippet inside a complete solution file with `apiVersion`, `kind`, `metadata`, `spec.resolvers: {}`, and `spec.workflow` sections — like the `simple-exec.yaml` example above.

```yaml
actions:
  # Pipes
  pipeline:
    provider: exec
    inputs:
      command: "echo 'hello world' | tr a-z A-Z"

  # Redirections
  redirect:
    provider: exec
    inputs:
      command: "echo 'log entry' >> /tmp/app.log"

  # Command substitution
  substitution:
    provider: exec
    inputs:
      command: 'echo "Today is $(date +%Y-%m-%d)"'

  # Conditionals
  conditional:
    provider: exec
    inputs:
      command: |
        if [ -d /tmp ]; then
          echo "temp dir exists"
        fi

  # Loops
  loop:
    provider: exec
    inputs:
      command: |
        for item in alpha beta gamma; do
          echo "Processing: $item"
        done
```

### Multi-line Scripts

Use YAML block scalars (`|`) for multi-line scripts:

```yaml
actions:
  setup:
    provider: exec
    inputs:
      command: |
        echo "=== Step 1: Create structure ==="
        mkdir -p /tmp/myapp/src
        mkdir -p /tmp/myapp/docs

        echo "=== Step 2: Write config ==="
        echo '{"version": "1.0.0"}' > /tmp/myapp/config.json

        echo "=== Step 3: Verify ==="
        cat /tmp/myapp/config.json
        echo "Done!"
```

---

## Arguments

Use the `args` field to pass arguments separately. Arguments are automatically shell-quoted to prevent injection:

```yaml
actions:
  safe-echo:
    provider: exec
    inputs:
      command: echo
      args:
        - "Hello"
        - "World"
        - "with special chars: $HOME ; rm -rf /"
```

The `args` values are appended to the command after being single-quoted for safety (e.g., `echo 'Hello' 'World' 'with special chars: $HOME ; rm -rf /'`).

> **Tip**: Use `args` when the values come from user input or resolved values to prevent shell injection. Use inline command strings when you want shell expansion.

---

## Environment Variables

Custom environment variables are merged with the parent process environment:

```yaml
actions:
  deploy:
    provider: exec
    inputs:
      command: |
        echo "Deploying $APP_NAME to $REGION"
        echo "Home: $HOME"
      env:
        APP_NAME: "my-service"
        REGION: "us-east-1"
```

The command has access to both the custom variables (`APP_NAME`, `REGION`) and the inherited environment variables (`HOME`, `PATH`, etc.).

### Using Resolved Values in Environment

```yaml
spec:
  resolvers:
    deploy-env:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "production"

  workflow:
    actions:
      deploy:
        provider: exec
        inputs:
          command: "echo Deploying to $ENVIRONMENT"
          env:
            ENVIRONMENT:
              expr: "_.deploy_env"
```

---

## Working Directory

Set the `workingDir` field to run commands in a specific directory:

```yaml
actions:
  build:
    provider: exec
    inputs:
      command: "ls -la && pwd"
      workingDir: /path/to/project
```

---

## Standard Input

Provide stdin content to commands:

```yaml
actions:
  # Pipe text to a command
  uppercase:
    provider: exec
    inputs:
      command: "tr a-z A-Z"
      stdin: "this text will be uppercased"

  # Multi-line stdin
  count-lines:
    provider: exec
    inputs:
      command: "wc -l"
      stdin: |
        line one
        line two
        line three
```

---

## Timeouts

Set a timeout in seconds. The command is killed if it exceeds the limit:

```yaml
actions:
  # Will complete normally (command finishes in ~1 second, timeout is 10)
  fast-command:
    provider: exec
    inputs:
      command: "echo 'quick' && sleep 1 && echo 'done'"
      timeout: 10

  # Will be killed (command would take 60 seconds, timeout is 5)
  slow-command:
    provider: exec
    inputs:
      command: "sleep 60"
      timeout: 5
```

When a command is killed by timeout, it returns a non-zero exit code and the `success` field is `false`.

---

## Shell Types

The `shell` field controls which interpreter runs your command:

| Value | Engine | Requires | Best For |
|-------|--------|----------|----------|
| `auto` | Embedded POSIX shell | Nothing | Cross-platform commands (default) |
| `sh` | Embedded POSIX shell | Nothing | Same as `auto` (alias) |
| `bash` | External `/usr/bin/env bash` | bash in PATH | Bash-specific features (arrays, globstar) |
| `pwsh` | External `pwsh` | pwsh in PATH | PowerShell cmdlets, Windows admin |
| `cmd` | External `cmd.exe` | Windows | Windows batch commands |

### Default: Embedded Shell (`auto`)

The default is the embedded POSIX shell. You never need to specify `shell: auto` — it's the default:

```yaml
actions:
  # These two are identical:
  implicit:
    provider: exec
    inputs:
      command: "echo hello | tr a-z A-Z"

  explicit:
    provider: exec
    inputs:
      command: "echo hello | tr a-z A-Z"
      shell: auto
```

### External Bash

Use `shell: bash` when you need bash-specific features that POSIX doesn't support:

```yaml
actions:
  bash-arrays:
    provider: exec
    inputs:
      command: |
        # Bash arrays and parameter expansion
        declare -a services=("api" "web" "worker")
        for svc in "${services[@]}"; do
          echo "Checking $svc..."
        done

        # Globstar
        shopt -s globstar
        echo "Go files: $(ls **/*.go 2>/dev/null | wc -l)"
      shell: bash
```

### PowerShell Core

Use `shell: pwsh` for PowerShell cmdlets:

```yaml
actions:
  # List files with PowerShell
  ps-list:
    provider: exec
    inputs:
      command: "Get-ChildItem -Path /tmp | Select-Object Name, Length"
      shell: pwsh

  # PowerShell scripting
  ps-script:
    provider: exec
    inputs:
      command: |
        $info = @{
          Shell = "PowerShell Core"
          Version = $PSVersionTable.PSVersion.ToString()
          Platform = $PSVersionTable.OS
        }
        $info | ConvertTo-Json
      shell: pwsh
```

### Windows cmd.exe

Use `shell: cmd` for Windows batch commands (Windows only):

```yaml
actions:
  batch:
    provider: exec
    inputs:
      command: "dir /b C:\\Users"
      shell: cmd
```

### Choosing the Right Shell

| Scenario | Shell |
|----------|-------|
| Simple commands, scripts, pipelines | `auto` (default) |
| Need to work on all platforms | `auto` (default) |
| Bash arrays, associative arrays, regex matching | `bash` |
| PowerShell cmdlets, .NET integration | `pwsh` |
| Legacy Windows batch scripts | `cmd` |

---

## Cross-Platform Patterns

The embedded shell provides Go-native implementations of common coreutils on Windows, so these commands work on all platforms:

```yaml
actions:
  # File operations — work on Linux, macOS, AND Windows
  setup:
    provider: exec
    inputs:
      command: |
        mkdir -p /tmp/myapp/config
        echo '{"port": 8080}' > /tmp/myapp/config/app.json
        cat /tmp/myapp/config/app.json
        cp /tmp/myapp/config/app.json /tmp/myapp/config/backup.json
        ls /tmp/myapp/config/

  cleanup:
    provider: exec
    dependsOn: [setup]
    inputs:
      command: "rm -rf /tmp/myapp"
```

> **Note**: On Windows, the Go-native coreutils are enabled by default. Set the `SCAFCTL_CORE_UTILS=false` environment variable to disable them if needed.

---

## Error Handling

The exec provider captures exit codes but does **not** return a Go error for non-zero exits. This means:

- The action always "succeeds" from the workflow engine's perspective
- Use the `exitCode` and `success` output fields to detect failures
- Use CEL expressions in `retryIf` or `skipIf` to react to failures

```yaml
actions:
  might-fail:
    provider: exec
    inputs:
      command: "false"

  check-result:
    provider: exec
    dependsOn: [might-fail]
    inputs:
      command:
        expr: |
          __actions['might-fail'].results.success
            ? "'echo Command succeeded'"
            : "'echo Command failed with exit code ' + string(__actions['might-fail'].results.exitCode)"
```

### Command Not Found

If a command doesn't exist, the embedded shell returns exit code 127 (standard POSIX behavior):

```yaml
actions:
  missing:
    provider: exec
    inputs:
      command: "nonexistent-command"
      # exitCode will be 127
      # success will be false
```

---

## Common Patterns

### Build and Test Pipeline

```yaml
actions:
  build:
    provider: exec
    inputs:
      command: "go build -o dist/app ./cmd/app"
      workingDir: /path/to/project
      timeout: 120

  test:
    provider: exec
    dependsOn: [build]
    inputs:
      command: "go test ./... -count=1"
      workingDir: /path/to/project
      timeout: 300

  report:
    provider: exec
    dependsOn: [test]
    inputs:
      command:
        expr: |
          __actions.test.results.success
            ? "'echo All tests passed'"
            : "'echo Tests failed with exit code ' + string(__actions.test.results.exitCode)"
```

### Generate and Verify Files

```yaml
actions:
  generate:
    provider: exec
    inputs:
      command: |
        mkdir -p /tmp/output
        echo '<!DOCTYPE html><html><body>Hello</body></html>' > /tmp/output/index.html

  verify:
    provider: exec
    dependsOn: [generate]
    inputs:
      command: |
        if [ -f /tmp/output/index.html ]; then
          echo "File exists, size: $(wc -c < /tmp/output/index.html) bytes"
        else
          echo "ERROR: File not found" >&2
          exit 1
        fi
```

### Platform-Adaptive Commands

```yaml
# Use the embedded shell for the common case, PowerShell for Windows-specific tasks
actions:
  # Works everywhere
  common-task:
    provider: exec
    inputs:
      command: "echo 'This runs on any OS'"

  # Use pwsh when you need Windows-specific cmdlets
  windows-admin:
    provider: exec
    dependsOn: [common-task]
    inputs:
      command: "Get-Service | Where-Object {$_.Status -eq 'Running'} | Select-Object -First 5"
      shell: pwsh
```

---

## Example Files

Complete runnable examples are available in the `examples/exec/` directory:

| Example | Description |
|---------|-------------|
| [simple.yaml](../../examples/exec/simple.yaml) | Basic commands, arguments, working directory |
| [shell-features.yaml](../../examples/exec/shell-features.yaml) | Pipes, variables, conditionals, loops |
| [shell-types.yaml](../../examples/exec/shell-types.yaml) | Using auto, bash, and PowerShell shells |
| [environment-and-io.yaml](../../examples/exec/environment-and-io.yaml) | Environment variables, stdin, timeouts |
| [cross-platform.yaml](../../examples/exec/cross-platform.yaml) | Patterns that work on all operating systems |

## Next Steps

- [Directory Provider Tutorial](directory-provider-tutorial.md) — Listing, scanning, and managing directories
- [Logging & Debugging Tutorial](logging-tutorial.md) — Control log verbosity, format, and output
- [Provider Reference](provider-reference.md) — Complete provider documentation
- [Actions Tutorial](actions-tutorial.md) — Use exec providers in workflows
