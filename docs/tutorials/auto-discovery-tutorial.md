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

{{< tabs "auto-discovery-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
# Instead of:
scafctl run solution -f solution.yaml

# Just run:
scafctl run solution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Instead of:
scafctl run solution -f solution.yaml

# Just run:
scafctl run solution
```
{{% /tab %}}
{{< /tabs >}}

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
9. `actions.yaml`
10. `actions.yml`

This produces 30 candidate paths checked in this exact order:

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
| 9 | `scafctl/actions.yaml` |
| 10 | `scafctl/actions.yml` |
| 11 | `.scafctl/solution.yaml` |
| 12 | `.scafctl/solution.yml` |
| 13 | `.scafctl/scafctl.yaml` |
| 14 | `.scafctl/scafctl.yml` |
| 15 | `.scafctl/solution.json` |
| 16 | `.scafctl/scafctl.json` |
| 17 | `.scafctl/taskfile.yaml` |
| 18 | `.scafctl/taskfile.yml` |
| 19 | `.scafctl/actions.yaml` |
| 20 | `.scafctl/actions.yml` |
| 21 | `solution.yaml` |
| 22 | `solution.yml` |
| 23 | `scafctl.yaml` |
| 24 | `scafctl.yml` |
| 25 | `solution.json` |
| 26 | `scafctl.json` |
| 27 | `taskfile.yaml` |
| 28 | `taskfile.yml` |
| 29 | `actions.yaml` |
| 30 | `actions.yml` |

The first file that exists on disk is used.

> **Note:** `taskfile.yaml` is NOT searched in action discovery mode (`scafctl run action`). It is only searched in default and solution discovery modes.

### Multi-Match Ambiguity Handling

When multiple solution files exist in a project, scafctl handles them based on command risk level:

| Risk Level | Commands | Behavior |
|------------|----------|----------|
| Low | `run solution`, `run resolver`, `lint`, `test`, `render` | Uses first match, prints a warning about other files found |
| High | `build solution`, `catalog push` | Returns an error, requires `-f` to disambiguate |

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

{{< tabs "auto-discovery-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
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
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
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
{{% /tab %}}
{{< /tabs >}}

### Conventional Subfolder

{{< tabs "auto-discovery-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
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
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Given this project structure:
# my-project/
# ├── scafctl/
# │   └── solution.yaml
# └── src/
#     └── main.go

cd my-project
scafctl run solution          # discovers ./scafctl/solution.yaml
```
{{% /tab %}}
{{< /tabs >}}

### Hidden Subfolder

{{< tabs "auto-discovery-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
# Given this project structure:
# my-project/
# ├── .scafctl/
# │   └── solution.yaml
# └── README.md

cd my-project
scafctl lint                  # discovers ./.scafctl/solution.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Given this project structure:
# my-project/
# ├── .scafctl/
# │   └── solution.yaml
# └── README.md

cd my-project
scafctl lint                  # discovers ./.scafctl/solution.yaml
```
{{% /tab %}}
{{< /tabs >}}

### Explicit `-f` Overrides Discovery

When you specify `-f`, auto-discovery is skipped entirely:

{{< tabs "auto-discovery-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
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
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Use a specific file regardless of what's in the current directory
scafctl run solution -f path/to/other-solution.yaml

# Use stdin
cat solution.yaml | scafctl lint -f -

# Use a URL
scafctl run solution -f https://example.com/solution.yaml

# Use a catalog name
scafctl run solution -f my-solution@1.0.0
```
{{% /tab %}}
{{< /tabs >}}

## Interaction with `--cwd`

When `--cwd` is set, auto-discovery searches relative to the specified directory instead of the process working directory:

{{< tabs "auto-discovery-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
# Discover solution inside another directory
scafctl run solution --cwd /path/to/project

# This is equivalent to:
cd /path/to/project && scafctl run solution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Discover solution inside another directory
scafctl run solution --cwd /path/to/project

# This is equivalent to:
cd /path/to/project; scafctl run solution
```
{{% /tab %}}
{{< /tabs >}}

All 30 candidate paths are resolved against the `--cwd` target.

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

{{< tabs "auto-discovery-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
scafctl config paths
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl config paths
```
{{% /tab %}}
{{< /tabs >}}

## Best Practices

1. **Use `solution.yaml` at the project root** -- this is the most common convention and is discovered without any subfolder prefix.

2. **Use `scafctl/` subfolder for multi-tool repos** -- keeps scafctl configuration separate from other tools.

3. **Use `.scafctl/` for hidden configuration** -- useful when the solution is infrastructure that shouldn't be front-and-center.

4. **Use `taskfile.yaml` for task runner workflows** -- similar to how `make` uses `Makefile` or `task` uses `Taskfile.yaml`, scafctl discovers `taskfile.yaml` for task-oriented solutions.

5. **Always use `-f` in CI/CD** -- explicit paths prevent surprises when the working directory changes.

6. **Combine with `--cwd` for monorepos** -- target a specific project without changing directories.

7. **Avoid multiple solution files** -- if you must have them (e.g., both `solution.yaml` and `taskfile.yaml`), use `-f` with high-risk commands like `build solution` to avoid errors.
