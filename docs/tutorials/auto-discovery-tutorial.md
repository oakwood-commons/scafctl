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

```bash
# Instead of:
scafctl run solution -f solution.yaml

# Just run:
scafctl run solution
```

## How Auto-Discovery Works

When no `-f` flag is provided, scafctl calls `FindSolution()`, which iterates over a set of **folder prefixes** and **file names** in order, returning the first match.

### Search Order

The search combines these folder prefixes with these file names:

**Folder prefixes** (checked in order):
1. `scafctl/` — conventional project subfolder
2. `.scafctl/` — hidden project subfolder
3. *(current directory)* — no subfolder prefix

**File names** (checked in order for each folder):
1. `solution.yaml`
2. `solution.yml`
3. `scafctl.yaml`
4. `scafctl.yml`
5. `solution.json`
6. `scafctl.json`

This produces 18 candidate paths checked in this exact order:

| Priority | Path |
|----------|------|
| 1 | `scafctl/solution.yaml` |
| 2 | `scafctl/solution.yml` |
| 3 | `scafctl/scafctl.yaml` |
| 4 | `scafctl/scafctl.yml` |
| 5 | `scafctl/solution.json` |
| 6 | `scafctl/scafctl.json` |
| 7 | `.scafctl/solution.yaml` |
| 8 | `.scafctl/solution.yml` |
| 9 | `.scafctl/scafctl.yaml` |
| 10 | `.scafctl/scafctl.yml` |
| 11 | `.scafctl/solution.json` |
| 12 | `.scafctl/scafctl.json` |
| 13 | `solution.yaml` |
| 14 | `solution.yml` |
| 15 | `scafctl.yaml` |
| 16 | `scafctl.yml` |
| 17 | `solution.json` |
| 18 | `scafctl.json` |

The first file that exists on disk is used.

## Supported Commands

Auto-discovery works with every command that accepts `-f`:

| Command | Auto-Discovery | Notes |
|---------|:--------------:|-------|
| `scafctl run solution` | ✅ | Also supports catalog bare names and URLs |
| `scafctl run resolver` | ✅ | Falls back to auto-discovery when `-f` is omitted |
| `scafctl lint` | ✅ | Discovers and lints the local solution |
| `scafctl render solution` | ✅ | Discovers solution for rendering |
| `scafctl test functional` | ✅ | Discovers solution to run functional tests |
| `scafctl test list` | ✅ | Discovers solution to list test cases |
| `scafctl test init` | ✅ | Discovers solution to generate test scaffold |
| `scafctl plugins install` | ✅ | Discovers solution to install referenced plugins |

## Examples

### Basic Usage — Run in Project Directory

```bash
# Given this project structure:
# my-project/
# ├── solution.yaml
# └── templates/
#     └── config.yaml.tpl

cd my-project
scafctl run solution          # discovers ./solution.yaml
scafctl lint                  # discovers ./solution.yaml
scafctl test functional       # discovers ./solution.yaml
```

### Conventional Subfolder

```bash
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

```bash
# Given this project structure:
# my-project/
# ├── .scafctl/
# │   └── solution.yaml
# └── README.md

cd my-project
scafctl lint                  # discovers ./.scafctl/solution.yaml
```

### Explicit `-f` Overrides Discovery

When you specify `-f`, auto-discovery is skipped entirely:

```bash
# Use a specific file regardless of what's in the current directory
scafctl run solution -f path/to/other-solution.yaml

# Use stdin
cat solution.yaml | scafctl lint -f -

# Use a URL
scafctl run solution -f https://example.com/solution.yaml

# Use a catalog name
scafctl run solution -f my-solution@1.0.0
```

## Interaction with `--cwd`

When `--cwd` is set, auto-discovery searches relative to the specified directory instead of the process working directory:

```bash
# Discover solution inside another directory
scafctl run solution --cwd /path/to/project

# This is equivalent to:
cd /path/to/project && scafctl run solution
```

All 18 candidate paths are resolved against the `--cwd` target.

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

```bash
scafctl config paths
```

## Best Practices

1. **Use `solution.yaml` at the project root** — this is the most common convention and is discovered without any subfolder prefix.

2. **Use `scafctl/` subfolder for multi-tool repos** — keeps scafctl configuration separate from other tools.

3. **Use `.scafctl/` for hidden configuration** — useful when the solution is infrastructure that shouldn't be front-and-center.

4. **Always use `-f` in CI/CD** — explicit paths prevent surprises when the working directory changes.

5. **Combine with `--cwd` for monorepos** — target a specific project without changing directories.
