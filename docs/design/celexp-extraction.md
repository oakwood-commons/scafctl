# Blueprint: Extract celexp Package to github.com/oakwood-commons/celexp

## 1. Summary

This blueprint evaluates extracting the `pkg/celexp` package (68 Go files, 13
extension groups, caching system, and type conversion utilities) into an external
library at `github.com/oakwood-commons/celexp`. The goal is twofold: (a) make
the CEL evaluation engine reusable by other Go applications, and (b) unblock
future extraction of celexp-dependent providers (cel, http, validation, debug)
as plugins. This is a **high-impact, medium-risk** change that touches 60+
importing files across every layer of scafctl and directly contradicts the
existing provider extraction plan's built-in boundary rule.

## 2. Pros & Cons Analysis

### Pros

| # | Benefit | Impact |
|---|---------|--------|
| 1 | **Reusability** -- Other Go apps get a batteries-included CEL library with caching, 13 custom extension groups, type conversion, and validation | High |
| 2 | **Plugin unblock** -- CEL, HTTP, validation, and debug providers could become plugins since they'd depend on the external lib, not `pkg/celexp` | High |
| 3 | **Smaller scafctl binary** -- If providers are later extracted as plugins, the core binary shrinks | Medium |
| 4 | **Independent versioning** -- celexp can be versioned, released, and tested independently | Medium |
| 5 | **Cleaner dependency graph** -- Forces removal of `settings` and `writer` coupling from celexp core | Medium |
| 6 | **Precedent exists** -- `httpc` and `scafctl-plugin-sdk` were already extracted successfully | Low |

### Cons

| # | Risk | Severity |
|---|------|----------|
| 1 | **Version skew** -- scafctl host and plugins could run different celexp versions; CEL expressions might behave differently at lint time vs runtime | **Critical** |
| 2 | **60+ file migration** -- Every file importing celexp needs its import path changed | High |
| 3 | **Two-repo development friction** -- Any celexp change requires: bump external lib, tag, update go.mod, test in scafctl. Slows iteration. | High |
| 4 | **API stability burden** -- External consumers require semver discipline; breaking changes to 8+ exported types and 30+ extension functions affect downstream | High |
| 5 | **Contradicts existing plan** -- The provider extraction plan explicitly states "any provider that imports `pkg/celexp` stays built-in" to avoid version skew | Medium |
| 6 | **Testing complexity** -- Integration tests must cover celexp version matrix scenarios | Medium |
| 7 | **settings/writer decoupling** -- Must replace `settings.DefaultCELCacheSize`, `settings.DefaultCELCostLimit`, and `writer.Writer` with injected values or functional options | Medium |
| 8 | **Transitive dependency weight** -- External consumers inherit cel-go v0.28.0 (~2.5MB), protobuf, and yaml dependencies | Low |

### Version Skew Detail

This is the single biggest risk. Today, the host's linter, resolver, and
providers all share the **exact same** celexp binary. If celexp becomes external:

- **Plugin A** might pin `celexp v1.2.0` (has `arrays.window()`)
- **Host** might pin `celexp v1.1.0` (doesn't have `arrays.window()`)
- A solution author writes `arrays.window(_.items, 3)` in a CEL expression --
  lint passes (plugin's celexp) but resolver evaluation fails (host's celexp)
- Or vice versa: lint fails but runtime would succeed

**Mitigation**: Pin a minimum celexp version in the plugin SDK and enforce
compatibility checks at plugin load time. This adds complexity but is solvable.

## 3. Architecture Decisions

### What Must Move

| Package | Files | Internal Dependencies | Extraction Difficulty |
|---------|-------|----------------------|----------------------|
| `pkg/celexp` (core) | 8 files | `settings` (2 constants), `logger` (appconfig only) | Medium -- must replace with functional options |
| `pkg/celexp/conversion` | 1 file | None | Trivial |
| `pkg/celexp/detail` | 2 files | `celexp` only | Trivial |
| `pkg/celexp/env` | 3 files | `writer.Writer` (1 function) | Medium -- must inject or remove |
| `pkg/celexp/ext` + 13 subdirs | ~26 files | `celexp/conversion`, `debug` uses `writer` | Medium |

### What Stays in scafctl

| Component | Reason |
|-----------|--------|
| `pkg/celexp/appconfig.go` | Orchestrates scafctl-specific `settings` + `logger` initialization; becomes a thin adapter calling the external lib |
| `pkg/settings` CEL constants | Remain as scafctl defaults; passed to external lib via options |
| `pkg/terminal/writer` integration | Stays in scafctl; passed to external lib's env factory via dependency injection |

### New External Library Structure

~~~text
github.com/oakwood-commons/celexp/
  go.mod                        # module github.com/oakwood-commons/celexp
  celexp.go                     # Expression, CompileResult, ProgramCache, Options
  cache.go                      # ProgramCache, CacheStats
  validation.go                 # VarDecl, CompileWithVarDecls, ValidateVars
  refs.go                       # Variable extraction
  context.go                    # EvaluateExpression
  helpers.go                    # NewConditional, NewCoalesce, etc.
  conversion/
    conversion.go               # CEL type conversions
  detail/
    detail.go                   # Function listing/detail
  env/
    env.go                      # CEL environment creation
    global.go                   # Global cache singleton
  ext/
    ext.go                      # Extension registry
    arrays/  debug/  filepath/  guid/  map/  marshalling/
    out/  regex/  sort/  strings/  time/
~~~

### Interface Changes

The `writer.Writer` dependency must be replaced with a standard `io.Writer`
interface:

```go
// Before (scafctl-coupled):
func NewWithWriter(w *writer.Writer, opts ...cel.EnvOption) (*cel.Env, error)
func DebugOutEnvOptions(w *writer.Writer) []cel.EnvOption

// After (generic):
func NewWithWriter(w io.Writer, opts ...cel.EnvOption) (*cel.Env, error)
func DebugOutEnvOptions(w io.Writer) []cel.EnvOption
```

The `settings` dependency must be replaced with functional options:

```go
// Before:
DefaultCacheSize = settings.DefaultCELCacheSize
defaultCostLimit.Store(settings.DefaultCELCostLimit)

// After:
const (
    DefaultCacheSize = 10000
    DefaultCostLimit = 1000000
)

// Callers override via:
WithCacheSize(n int) Option
WithCostLimit(limit uint64) Option
```

## 4. Task Breakdown

| # | Task | Files | Complexity | Depends On |
|---|------|-------|-----------|------------|
| 1 | Create `github.com/oakwood-commons/celexp` repo with module skeleton | New repo | S | -- |
| 2 | Replace `settings.*` constants with local defaults + functional options in celexp | `celexp.go` | S | 1 |
| 3 | Replace `writer.Writer` with `io.Writer` in env and debug extension | `env/env.go`, `ext/debug/debug.go` | S | 1 |
| 4 | Remove `logger.FromContext` from appconfig; use functional option for logger | `appconfig.go` | S | 2 |
| 5 | Copy all celexp code to external repo, update internal imports | 68 files | M | 2, 3, 4 |
| 6 | Add comprehensive tests to external repo (port existing tests) | ~30 test files | M | 5 |
| 7 | Tag `celexp v0.1.0` | External repo | S | 6 |
| 8 | Update scafctl `go.mod` to depend on `github.com/oakwood-commons/celexp` | `go.mod` | S | 7 |
| 9 | Create scafctl adapter: `pkg/celexp/` becomes a thin re-export + `InitFromAppConfig` bridge | `pkg/celexp/*.go` (rewrite) | L | 8 |
| 10 | Update all 60+ importing files to use external lib (or adapter) | 60+ files across `pkg/` | L | 9 |
| 11 | Update all tests | 30+ test files | M | 10 |
| 12 | Run `task test:e2e`, fix breakage | -- | M | 11 |
| 13 | Update documentation, examples, MCP tool references | `docs/`, `examples/` | S | 12 |

**Total estimated scope: ~100 files touched across 2 repos.**

## 5. Interface Design

### External Library API Surface

```go
package celexp

// Core types (unchanged API, new module path)
type Expression string
type CompileResult struct { ... }
type ProgramCache struct { ... }
type CacheStats struct { ... }
type VarInfo struct { ... }
type ExtFunction struct { ... }
type Option func(*config)

// Defaults (hardcoded, no settings dependency)
const (
    DefaultCacheSize uint64 = 10000
    DefaultCostLimit uint64 = 1000000
)

// Functional options
func WithCacheSize(n int) Option
func WithCostLimit(limit uint64) Option
func WithLogger(fn func(format string, args ...any)) Option

// Core API (unchanged signatures)
func EvaluateExpression(ctx context.Context, expr Expression, data map[string]any, vars map[string]any) (any, error)
func (e Expression) Compile(envOpts []cel.EnvOption, opts ...Option) (*CompileResult, error)
func (e Expression) GetUnderscoreVariables(ctx context.Context) ([]string, error)
func NewProgramCache(size int) *ProgramCache
```

### scafctl Adapter Layer

```go
// pkg/celexp/adapter.go -- thin bridge in scafctl
package celexp

import (
    extcelexp "github.com/oakwood-commons/celexp"
    "github.com/oakwood-commons/scafctl/pkg/settings"
)

// Re-export types for backward compatibility within scafctl
type Expression = extcelexp.Expression
type ProgramCache = extcelexp.ProgramCache
// ...

// scafctl-specific initialization
func InitFromAppConfig(ctx context.Context, cfg CELConfigInput) {
    extcelexp.SetDefaultCostLimit(cfg.CostLimit)
    // ... bridge settings -> functional options
}
```

## 6. Error Handling

- No new sentinel errors needed -- existing error types move as-is
- Error wrapping strategy unchanged: `fmt.Errorf("context: %w", err)`
- Version compatibility errors should be added to the plugin SDK for load-time
  checks:

  ```go
  var ErrCelexpVersionMismatch = errors.New("plugin celexp version incompatible with host")
  ```

## 7. Testing Strategy

| Layer | What | Where |
|-------|------|-------|
| External lib unit tests | Port all existing celexp tests | `github.com/oakwood-commons/celexp/**/*_test.go` |
| External lib benchmarks | Port cache, sort, out benchmarks | Same |
| scafctl adapter tests | Verify re-exports work, InitFromAppConfig bridges correctly | `pkg/celexp/*_test.go` |
| scafctl integration tests | Existing CLI, solution, API tests must pass unchanged | `tests/integration/` |
| E2E | `task test:e2e` must pass | -- |
| Version skew tests | Test plugin with different celexp version than host | New integration test |

## 8. Risks & Edge Cases

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Version skew breaks expressions | High | Critical | Pin minimum version in plugin SDK; add load-time compatibility check |
| Two-repo iteration slows development | High | High | Use `go.mod replace` during development; accept the tradeoff |
| Breaking change cascade | Medium | High | Use adapter/re-export layer to shield scafctl internals initially |
| `io.Writer` vs `writer.Writer` behavior difference | Low | Medium | `writer.Writer` likely wraps `io.Writer`; adapter can bridge |
| External consumers depend on unstable API | Medium | Medium | Start at v0.x; document instability |
| Merge conflicts during migration (60+ files) | Medium | Low | Do in one PR; coordinate timing |

## 9. Recommendation

**Recommendation: Do not extract celexp at this time.**

### The version skew problem is not theoretical

CEL is the **expression language** of scafctl. It's used in resolvers, actions,
providers, linting, validation, and the API server. Every layer must agree on
what functions exist and how they behave. An external library creates a seam
where versions can diverge. The existing provider extraction plan explicitly
identified this risk and drew the built-in boundary at "imports celexp -> stays
built-in."

### The reuse case is speculative

While other Go apps _could_ use a CEL library with caching and custom
extensions, the 13 extension groups (`arrays`, `guid`, `time`, `regex`,
`filepath`, `out`, `debug`, etc.) are heavily scafctl-flavored. External
consumers would likely want different extensions. The generic value is really
just the caching layer + type conversion -- a much smaller extraction surface.

### The plugin unblock is achievable without extraction

If the goal is to extract cel/http/validation/debug providers as plugins, there
are two alternatives:

1. **Embed celexp in the plugin SDK** -- The SDK already exists. Add celexp as a
   sub-module of the SDK. Both host and plugins import from the same SDK module,
   and version is locked by the SDK version. This is simpler than a separate
   repo and eliminates version skew risk.

2. **Keep providers built-in** -- The existing plan already decided these 8
   providers stay built-in. The remaining 12 providers can still be extracted.
   The benefit of extracting 4 more providers (from 12 to 16) is marginal.

### If you proceed anyway

If the team decides the reusability benefit outweighs the risks:

1. **Start with v0.x** to signal instability
2. **Use an adapter layer** in scafctl (`pkg/celexp` becomes re-exports) to
   minimize the blast radius
3. **Add version compatibility checks** to the plugin SDK
4. **Extract only the core** (cache, evaluation, conversion) first; keep
   extensions in scafctl until the API stabilizes
5. **Budget 2-3 weeks** of focused work for the migration + stabilization across
   ~100 files

### Alternative: Extract just the caching layer

A smaller, lower-risk extraction would be to pull out only `ProgramCache` +
`CacheStats` + type conversion as `github.com/oakwood-commons/celcache`. This
gives external consumers the high-value generic piece without exposing scafctl's
opinionated extension surface. This would be ~5 files, zero scafctl-internal
dependencies, and no version skew risk.
