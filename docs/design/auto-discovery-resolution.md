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

All commands pick the solution source with `Router.Select()` from
`pkg/solution/load`, which implements this chain:

1. **Explicit flag** -- If `-f` is provided, use it directly (no discovery).
2. **Unambiguous positional argument** -- If the first positional arg is a URL,
   registry reference, or versioned name (`app@1.2.3`), use it.
3. **Auto-discovery** -- Search configured folders and file names in priority
   order, returning all matches.
4. **Catalog fallback** -- With `ArgsAreNamesOrCatalog`, a bare positional name
   is used as a catalog reference when nothing is discovered.

~~~go
router := load.NewRouterFromContext(ctx, lgr,
    load.CLIRouterOptions(stdin, noCache, settings.DiscoveryModeDefault)...)
sel, err := router.Select(ctx, file, args, load.ArgsAreNamesOrCatalog)
if err != nil {
    return err
}
ms, err := router.SelectionLoader(sel).Load(ctx, sel.Source)
~~~

`SelectionLoader` reuses the matches found by `Select`, so discovery runs once.

### Discovery Risk Levels

The `DiscoveryRisk` type (set with `load.WithDiscoveryRisk`, or passed to
`Selection.DiscoveredPath`) controls how multi-match ambiguity is handled:

| Risk Level | Behavior | Commands |
|------------|----------|----------|
| `DiscoveryRiskLow` | Use first match, emit warning about others to stderr | `run`, `lint`, `test`, `render` |
| `DiscoveryRiskHigh` | Return `load.ErrAmbiguousSolution` listing all matches, require `-f` | `build` |

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

Action file names (`actions.yaml`, `actions.yml`) are only searched when
`DiscoveryModeAction` is active (e.g., `run action`). They are not included
in the default search list.

This produces 24 candidate paths checked in priority order (3 folders x 8 names).

### Discover vs Load

| Method | Behavior |
|--------|----------|
| `DiscoverLoader.Discover()` | Returns all matches in priority order, or `ErrNoSolutionFound` |
| `DiscoverLoader.Load()` | Discovers and loads one match, applying the risk level |

Candidate paths are resolved against the context working directory (`--cwd`).

### MCP Server Integration

The MCP server's `discoverSolutionFiles()` calls `findSolutionFiles()`, which
runs `DiscoverLoader.Discover()` in the working directory and in each MCP client
workspace root and deduplicates the results. This reports all discoverable
solutions to AI clients, enabling them to present the full set of available
solutions in a workspace.

## Behavior Examples

The `Using ...` and multi-match messages below are emitted via `writer.Verbosef`
and only appear when `--verbose` is enabled.

### Single match (common case)

~~~
$ scafctl lint --verbose
Using solution.yaml
...
~~~

### Multiple matches, low-risk command

~~~
$ scafctl lint --verbose
Using solution.yaml
WARNING: Multiple solution files found (also: taskfile.yaml); using first match
...
~~~

### Multiple matches, high-risk command

~~~
$ scafctl package solution
Error: multiple solution files found: solution.yaml, taskfile.yaml; use -f/--file to specify which one
~~~

## API

~~~go
package load

type DiscoveryRisk int

const (
    DiscoveryRiskLow  DiscoveryRisk = iota  // warn on ambiguity
    DiscoveryRiskHigh                       // error on ambiguity
)

type Discovery struct {
    Path          string
    IsActionFile  bool
    AlternatePath string
}

func NewDiscoverLoader(files *FileLoader, binaryName string, opts ...DiscoverOption) *DiscoverLoader
func WithDiscoveryMode(mode settings.DiscoveryMode) DiscoverOption
func WithDiscoveryRisk(risk DiscoveryRisk) DiscoverOption

func (l *DiscoverLoader) Discover(ctx context.Context) ([]Discovery, error)
func (l *DiscoverLoader) Load(ctx context.Context, c source.Classification) (*MaterializedSolution, error)

func (r *Router) Select(ctx context.Context, explicit string, args []string, policy ArgPolicy, opts ...source.Option) (Selection, error)
func (r *Router) SelectionLoader(s Selection) Loader
func (s Selection) DiscoveredPath(risk DiscoveryRisk) (string, error)
~~~

## Testing

- Unit tests in `pkg/solution/load/discover_test.go` and
  `pkg/solution/load/select_test.go` cover the resolution chain, risk levels,
  and edge cases (no files, single match, multi-match).
- Integration tests in `tests/integration/cli_test.go` verify end-to-end
  discovery of `taskfile.yaml` and multi-match warning output.
