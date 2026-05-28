---
title: "Auto-Discovery Resolution Chain"
weight: 10
---

# Auto-Discovery Resolution Chain

## Overview

When no `-f` flag is provided, scafctl automatically searches for solution files
using a unified resolution chain. This design introduces risk-based ambiguity
handling and adds `taskfile.yaml`/`taskfile.yml` as discoverable file names.

## Problem Statement

1. **Inconsistent discovery** -- Each CLI command implemented its own discovery
   logic with slightly different behavior, making it hard to predict which file
   would be used.
2. **Silent ambiguity** -- When multiple solution files existed in a project
   (e.g., `solution.yaml` and `actions.yaml`), the first match was used with
   no feedback to the user.
3. **Destructive command risk** -- Commands like `build` that publish artifacts
   should not silently pick an ambiguous file -- the consequences of choosing
   wrong are harder to reverse.
4. **Missing taskfile support** -- Projects using `taskfile.yaml` as their
   solution filename had to always specify `-f`.

## Design

### Unified Resolution Chain

All commands use a single `Resolve()` function from `pkg/solution/get` that
implements a three-step chain:

1. **Explicit flag** -- If `-f` is provided, use it directly (no discovery).
2. **Positional argument** -- If a positional arg is given (catalog reference),
   use it as-is.
3. **Auto-discovery** -- Search configured folders and file names in priority
   order, returning all matches.

~~~go
resolved, err := get.Resolve(ctx, getter, file, positionalArg, get.ResolveOptions{
    Risk: get.DiscoveryRiskLow,
})
~~~

### Discovery Risk Levels

The `DiscoveryRisk` type controls how multi-match ambiguity is handled:

| Risk Level | Behavior | Commands |
|------------|----------|----------|
| `DiscoveryRiskLow` | Use first match, emit warning about others to stderr | `run`, `lint`, `test`, `render` |
| `DiscoveryRiskHigh` | Return an error listing all matches, require `-f` | `build` |

This ensures read-only or easily-reversible commands remain convenient while
destructive operations require explicit intent.

### File Name Search Order

The search combines folder prefixes with file names in this order:

**Folder prefixes** (checked in order):
1. `scafctl/` -- conventional project subfolder
2. `.scafctl/` -- hidden project subfolder
3. *(current directory)* -- root

**File names** (checked in order per folder):
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

This produces 30 candidate paths checked in priority order (3 folders x 10 names).

### FindAllSolutions vs FindSolution

| Function | Behavior |
|----------|----------|
| `FindSolution()` | Returns the first match (backward compatible) |
| `FindAllSolutions()` | Returns all matches in priority order with deduplication |

`FindAllSolutions` uses canonical path resolution to deduplicate entries when
different relative paths resolve to the same absolute file.

### MCP Server Integration

The MCP server's `discoverSolutionFiles()` uses `FindAllSolutions()` to report
all discoverable solutions to AI clients, enabling them to present the full set
of available solutions in a workspace.

## Behavior Examples

### Single match (common case)

~~~
$ scafctl lint
Using solution.yaml
...
~~~

### Multiple matches, low-risk command

~~~
$ scafctl lint
Using solution.yaml
WARNING: Multiple solution files found (also: actions.yaml); using first match
...
~~~

### Multiple matches, high-risk command

~~~
$ scafctl build solution
Error: multiple solution files found: solution.yaml, actions.yaml; use -f/--file to specify which one
~~~

## API

~~~go
package get

type DiscoveryRisk int

const (
    DiscoveryRiskLow  DiscoveryRisk = iota  // warn on ambiguity
    DiscoveryRiskHigh                       // error on ambiguity
)

type ResolveOptions struct {
    Risk          DiscoveryRisk
    DiscoveryMode settings.DiscoveryMode
}

func Resolve(ctx context.Context, getter *Getter, file, positionalArg string, opts ResolveOptions) (string, error)

func (o *Getter) FindAllSolutions() []DiscoveryResult
func (o *Getter) SearchedPaths() []string
~~~

## Testing

- Unit tests in `pkg/solution/get/resolve_test.go` cover the resolution chain,
  risk levels, and edge cases (no files, single match, multi-match).
- Integration tests in `tests/integration/cli_test.go` verify end-to-end
  discovery of `taskfile.yaml` and multi-match warning output.
