---
title: "Auto-Discovery Tutorial"
weight: 88
---

# Auto-Discovery Tutorial

This tutorial explains how scafctl automatically discovers solution files, making the `-f` flag optional for most commands.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with YAML syntax and solution files
- Completion of the [Getting Started](../getting-started/) tutorial

## Table of Contents

1. [Overview](#overview)
2. [How Auto-Discovery Works](#how-auto-discovery-works)
3. [Search Order](#search-order)
4. [Supported Commands](#supported-commands)
5. [Examples](#examples)
6. [Interaction with `--cwd`](#interaction-with---cwd)
7. [When Auto-Discovery Fails](#when-auto-discovery-fails)
8. [Best Practices](#best-practices)

---

## Overview

Many scafctl commands accept a `-f` / `--file` flag to specify the solution file path. When this flag is omitted, scafctl searches the current directory (or the `--cwd` target) for a solution file using a well-defined search order.

This means you can simply `cd` into a project directory and run commands without specifying the file:

```shell
# Instead of:
scafctl run solution -f solution.yaml

# Just run:
scafctl run solution
```

## How Auto-Discovery Works

When no `-f` flag is provided, scafctl searches for solution files using the unified resolution chain, which iterates over a set of **folder prefixes** and **file names** in order, returning the first match.

### Search Order

The search combines these folder prefixes with these file names:

**Folder prefixes** (checked in order):
1. `scafctl/` -- conventional project subfolder
2. `.scafctl/` -- hidden project subfolder
3. *(current directory)* -- no subfolder prefix

**File names** (checked in order for each folder):
1. `solution.yaml`
2. `solution.yml`
3. `scafctl.yaml`
4. `scafctl.yml`
5. `solution.json`
6. `scafctl.json`
7. `taskfile.yaml`
8. `taskfile.yml`

This produces 24 candidate paths checked in this exact order:

| Priority | Path |
|----------|------|
| 1 | `scafctl/solution.yaml` |
| 2 | `scafctl/solution.yml` |
| 3 | `scafctl/scafctl.yaml` |
| 4 | `scafctl/scafctl.yml` |
| 5 | `scafctl/solution.json` |
| 6 | `scafctl/scafctl.json` |
| 7 | `scafctl/taskfile.yaml` |
| 8 | `scafctl/taskfile.yml` |
| 9 | `.scafctl/solution.yaml` |
| 10 | `.scafctl/solution.yml` |
| 11 | `.scafctl/scafctl.yaml` |
| 12 | `.scafctl/scafctl.yml` |
| 13 | `.scafctl/solution.json` |
| 14 | `.scafctl/scafctl.json` |
| 15 | `.scafctl/taskfile.yaml` |
| 16 | `.scafctl/taskfile.yml` |
| 17 | `solution.yaml` |
| 18 | `solution.yml` |
| 19 | `scafctl.yaml` |
| 20 | `scafctl.yml` |
| 21 | `solution.json` |
| 22 | `scafctl.json` |
| 23 | `taskfile.yaml` |
| 24 | `taskfile.yml` |

The first file that exists is used -- with one exception: a `taskfile.yaml`/
`taskfile.yml` that is **not** a scafctl Solution (it parses as YAML but carries
none of the recognized solution headers -- `apiVersion: scafctl.io/v1`,
`kind: Solution`, or a non-null `spec` -- for example a
[go-task](https://taskfile.dev) taskfile) is skipped and discovery continues to
the next candidate, with a note under `--verbose`. Explicit `-f` is unaffected:
pointing `-f` at such a file still reports the full validation error.

> **Note:** Action files (`actions.yaml`, `actions.yml`) are excluded from the default 24-path search list and from `scafctl run solution`. In action discovery mode (`scafctl run action`, `scafctl run resolver --action`, `scafctl validate resolver --action`), action files are searched first across the 3 folders with solution files as fallback (and `taskfile.yaml` is excluded). In resolver discovery mode (`scafctl run resolver`, `scafctl validate resolver`), the 24 solution candidates above are searched first, falling back to `actions.yaml`/`actions.yml` across the 3 folders only when no solution file exists.

### Multi-Match Ambiguity Handling

When multiple solution files exist in a project, scafctl handles them based on command risk level:

| Risk Level | Commands | Behavior |
|------------|----------|----------|
| Low | `run solution`, `run resolver`, `lint`, `test`, `render` | Uses first match, prints a warning about other files found |
| High | `package solution`, `catalog push` | Returns an error, requires `-f` to disambiguate |

When auto-discovery succeeds, scafctl always prints which file was selected:

```
Using scafctl/solution.yaml
```

When multiple files are found with a low-risk command:

```
Using scafctl/solution.yaml
WARNING: Multiple solution files found (also: solution.yaml); using first match
```

When multiple files are found with a high-risk command:

```
Error: multiple solution files found: scafctl/solution.yaml, solution.yaml; use -f/--file to specify which one
```

## Supported Commands

Auto-discovery works with every command that accepts `-f`:

| Command | Auto-Discovery | Notes |
|---------|:--------------:|-------|
| `scafctl run solution` | Yes | Also supports catalog bare names and URLs |
| `scafctl run action` | Yes | Searches `actions.yaml`/`actions.yml` first, then solution files (including `--list` / `-l`) |
| `scafctl run resolver` | Yes | Searches solution files first, falls back to `actions.yaml`/`actions.yml`, then valid catalog bare names |
| `scafctl validate resolver` | Yes | Same discovery and catalog fallback as `run resolver` |
| `scafctl lint` / `scafctl lint solution` | Yes | Discovers and lints the local solution |
| `scafctl validate` / `scafctl validate solution` | Yes | Discovers and validates the local solution |
| `scafctl render solution` | Yes | Discovers solution for rendering |
| `scafctl test` / `scafctl test functional` | Yes | Discovers solution to run functional tests |
| `scafctl test list` | Yes | Discovers solution to list test cases |
| `scafctl test init` | Yes | Discovers solution to generate test scaffold |
| `scafctl plugins install` | Yes | Discovers solution to install referenced plugins |

## Examples

### Basic Usage — Run in Project Directory

```shell
# Given this project structure:
# my-project/
# ├── solution.yaml
# └── templates/
#     └── config.yaml.tpl

cd my-project
scafctl run solution          # discovers ./solution.yaml
scafctl run action --list     # discovers ./actions.yaml (or ./solution.yaml) and lists actions
scafctl lint                  # discovers ./solution.yaml
scafctl test functional       # discovers ./solution.yaml
```

### Conventional Subfolder

```shell
# Given this project structure:
# my-project/
# ├── scafctl/
# │   └── solution.yaml
# └── src/
#     └── main.go

cd my-project
scafctl run solution          # discovers ./scafctl/solution.yaml
```

### Hidden Subfolder

```shell
# Given this project structure:
# my-project/
# ├── .scafctl/
# │   └── solution.yaml
# └── README.md

cd my-project
scafctl lint                  # discovers ./.scafctl/solution.yaml
```

### Explicit Sources Override Discovery

When you specify `-f`/`--file` (for a local path or `-` stdin), `-s`/`--solution`, or a positional catalog/URL reference, auto-discovery is skipped entirely:

```shell
# Use a specific local file regardless of what's in the current directory
scafctl run solution -f path/to/other-solution.yaml

# Use stdin
cat solution.yaml | scafctl lint -f -

# Use a URL (positional reference)
scafctl run solution https://example.com/solution.yaml

# Use a catalog name (positional on solution commands, -s on action/resolver commands)
scafctl run solution my-solution@1.0.0
scafctl run action build -s my-solution@1.0.0
```

## Interaction with `--cwd`

When `--cwd` is set, auto-discovery searches relative to the specified directory instead of the process working directory:

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
# Discover solution inside another directory
scafctl run solution --cwd /path/to/project

# This is equivalent to:
cd /path/to/project && scafctl run solution
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
# Discover solution inside another directory
scafctl run solution --cwd /path/to/project

# This is equivalent to:
cd /path/to/project; scafctl run solution
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

All 24 candidate paths are resolved against the `--cwd` target.

## When Auto-Discovery Fails

If no solution file is found, scafctl returns a clear error:

```
Error: no solution path provided and no solution file found in default locations
```

Common causes:
- You're not in the correct directory
- The solution file has a non-standard name (use `-f` to specify it)
- The `--cwd` flag points to the wrong directory

You can see the candidate paths scafctl checks with:

```shell
scafctl config paths
```

## Best Practices

1. **Use `solution.yaml` at the project root** -- this is the most common convention and is discovered without any subfolder prefix.

2. **Use `scafctl/` subfolder for multi-tool repos** -- keeps scafctl configuration separate from other tools.

3. **Use `.scafctl/` for hidden configuration** -- useful when the solution is infrastructure that shouldn't be front-and-center.

4. **Use `taskfile.yaml` for task runner workflows** -- similar to how `make` uses `Makefile` or `task` uses `Taskfile.yaml`, scafctl discovers `taskfile.yaml` for task-oriented solutions. If your repo already has a go-task taskfile under that name, discovery skips it (it carries no recognized solution headers) and loads your solution file instead; keep your scafctl solution in `solution.yaml` or pass `-f` explicitly.

5. **Always use `-f` in CI/CD** -- explicit paths prevent surprises when the working directory changes.

6. **Combine with `--cwd` for monorepos** -- target a specific project without changing directories.

7. **Avoid multiple solution files** -- if you must have them (e.g., both `solution.yaml` and `taskfile.yaml`), use `-f` with high-risk commands like `package solution` to avoid errors.
