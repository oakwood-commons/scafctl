# Configurable Services Implementation Plan

This document outlines services in scafctl that could benefit from app-level configuration (similar to what we implemented for HTTP client), along with implementation plans for each.

## Executive Summary

> **Status**: ✅ All phases implemented

All identified services now support configuration via `~/.scafctl/config.yaml`. This includes the HTTP client (previously implemented), CEL expression engine, resolver executor, action executor, and logging.

### Services Identified

| Service | Priority | Effort | Value | Status |
|---------|----------|--------|-------|--------|
| CEL Expression Engine | High | Medium | High | ✅ Implemented |
| Resolver Executor | High | Medium | High | ✅ Implemented |
| Action Executor | Medium | Low | Medium | ✅ Implemented |
| Logging/Debugging | Low | Low | Low | ✅ Implemented |
| Authentication (Entra) | Medium | Low | Medium | ✅ Implemented |

---

## 1. CEL Expression Engine

### Current State

The CEL package (`pkg/celexp`) has several configurable parameters currently set via package-level defaults:

| Parameter | Default | Source |
|-----------|---------|--------|
| `cacheSize` | 10,000 | `settings.DefaultCELCacheSize` |
| `costLimit` | 1,000,000 | `settings.DefaultCELCostLimit` |
| `useASTBasedCaching` | false | hardcoded |

### Why Configure?

- **Large solutions**: May need larger cache sizes to avoid recompilation overhead
- **Complex expressions**: May need higher cost limits for legitimate use cases
- **Performance tuning**: AST-based caching can improve hit rates by ~75% but has memory tradeoffs
- **Security**: Organizations may want to enforce lower cost limits to prevent DoS via malicious expressions

### Proposed Config Structure

```yaml
version: 1

cel:
  # Maximum number of compiled programs to cache
  cacheSize: 10000
  
  # Cost limit for expression evaluation (0 = disabled)
  # Prevents runaway expressions from consuming resources
  costLimit: 1000000
  
  # Enable AST-based caching for better hit rates
  # Expressions with same structure share cache entries
  useASTBasedCaching: false
  
  # Enable expression metrics collection
  enableMetrics: true
```

### Implementation Tasks

1. **Add `CELConfig` struct to `pkg/config/types.go`** ✅
   ```go
   type CELConfig struct {
       CacheSize         int   `json:"cacheSize,omitempty" yaml:"cacheSize,omitempty" mapstructure:"cacheSize" doc:"CEL program cache size" example:"10000" maximum:"1000000"`
       CostLimit         int64 `json:"costLimit,omitempty" yaml:"costLimit,omitempty" mapstructure:"costLimit" doc:"CEL cost limit (0=disabled)" example:"1000000"`
       UseASTBasedCaching bool  `json:"useASTBasedCaching,omitempty" yaml:"useASTBasedCaching,omitempty" mapstructure:"useASTBasedCaching" doc:"Enable AST-based cache keys"`
       EnableMetrics     *bool `json:"enableMetrics,omitempty" yaml:"enableMetrics,omitempty" mapstructure:"enableMetrics" doc:"Enable expression metrics"`
   }
   ```

2. **Add to `Config` struct** ✅
   ```go
   type Config struct {
       // ... existing fields
       CEL CELConfig `json:"cel,omitempty" yaml:"cel,omitempty" mapstructure:"cel" doc:"CEL expression engine configuration"`
   }
   ```

3. **Add defaults in `pkg/config/config.go`** ✅
   ```go
   m.v.SetDefault("cel.cacheSize", settings.DefaultCELCacheSize)
   m.v.SetDefault("cel.costLimit", settings.DefaultCELCostLimit)
   m.v.SetDefault("cel.useASTBasedCaching", false)
   m.v.SetDefault("cel.enableMetrics", true)
   ```

4. **Add validation in `pkg/config/validation.go`** ✅

5. **Add `InitFromAppConfig()` to `pkg/celexp/appconfig.go`** ✅
   - Reconfigure global cache with specified size
   - Set cost limit via `SetDefaultCostLimit()`
   - Configure AST-based caching option

6. **Update initialization in main/root command** ✅

### Environment Variable Support

| Config Key | Environment Variable |
|------------|---------------------|
| `cel.cacheSize` | `SCAFCTL_CEL_CACHESIZE` |
| `cel.costLimit` | `SCAFCTL_CEL_COSTLIMIT` |
| `cel.useASTBasedCaching` | `SCAFCTL_CEL_USEASTBASEDCACHING` |
| `cel.enableMetrics` | `SCAFCTL_CEL_ENABLEMETRICS` |

---

## 2. Resolver Executor

### Current State

The resolver executor (`pkg/resolver/executor.go`) has these configurable parameters:

| Parameter | Default | Source | CLI Flag |
|-----------|---------|--------|----------|
| `timeout` | 30s | `settings.DefaultResolverTimeout` | `--resolver-timeout` |
| `phaseTimeout` | 5m | `settings.DefaultPhaseTimeout` | `--phase-timeout` |
| `maxConcurrency` | 0 (unlimited) | hardcoded | none |
| `warnValueSize` | 1MB | `settings.DefaultWarnValueSize` | `--warn-value-size` |
| `maxValueSize` | 10MB | `settings.DefaultMaxValueSize` | `--max-value-size` |

### Why Configure?

- **Slow networks/systems**: May need longer timeouts for resolvers that fetch remote data
- **Resource constraints**: Limit concurrency on memory-constrained systems
- **CI/CD pipelines**: Different defaults for automated vs interactive use
- **Large data**: Adjust value size limits for data-heavy solutions

### Proposed Config Structure

```yaml
version: 1

resolver:
  # Default timeout per resolver execution
  timeout: "30s"
  
  # Maximum time for each resolution phase
  phaseTimeout: "5m"
  
  # Maximum concurrent resolvers per phase (0 = unlimited)
  maxConcurrency: 0
  
  # Warn when value exceeds this size (bytes, 0 = disabled)
  warnValueSize: 1048576  # 1MB
  
  # Fail when value exceeds this size (bytes, 0 = disabled)
  maxValueSize: 10485760  # 10MB
  
  # Enable validate-all mode (collect all errors vs stop at first)
  validateAll: false
```

### Implementation Tasks

1. **Add `ResolverConfig` struct to `pkg/config/types.go`** ✅
   ```go
   type ResolverConfig struct {
       Timeout        string `json:"timeout,omitempty" yaml:"timeout,omitempty" mapstructure:"timeout" doc:"Default resolver timeout" example:"30s" maxLength:"20"`
       PhaseTimeout   string `json:"phaseTimeout,omitempty" yaml:"phaseTimeout,omitempty" mapstructure:"phaseTimeout" doc:"Maximum phase duration" example:"5m" maxLength:"20"`
       MaxConcurrency int    `json:"maxConcurrency,omitempty" yaml:"maxConcurrency,omitempty" mapstructure:"maxConcurrency" doc:"Max concurrent resolvers (0=unlimited)" example:"10" maximum:"1000"`
       WarnValueSize  int64  `json:"warnValueSize,omitempty" yaml:"warnValueSize,omitempty" mapstructure:"warnValueSize" doc:"Warn threshold in bytes" example:"1048576"`
       MaxValueSize   int64  `json:"maxValueSize,omitempty" yaml:"maxValueSize,omitempty" mapstructure:"maxValueSize" doc:"Max value size in bytes" example:"10485760"`
       ValidateAll    bool   `json:"validateAll,omitempty" yaml:"validateAll,omitempty" mapstructure:"validateAll" doc:"Collect all errors vs stop at first"`
   }
   ```

2. **Add to `Config` struct** ✅

3. **Add defaults and validation** ✅

4. **Add `OptionsFromAppConfig()` to `pkg/resolver/executor.go`** ✅

5. **Update `run solution` and `render solution` commands to use config** ✅
   - Uses `cfg.Resolver.ToResolverValues()` helper function
   - CLI flags override config values

### CLI Flag Interaction

CLI flags should override config file values:
```bash
# Uses config file default (e.g., 30s)
scafctl run solution example

# Overrides for this execution only
scafctl run solution example --resolver-timeout=60s
```

---

## 3. Action Executor

### Current State

The action executor (`pkg/action/executor.go`) has these configurable parameters:

| Parameter | Default | Source | CLI Flag |
|-----------|---------|--------|----------|
| `defaultTimeout` | 5m | `settings.DefaultActionTimeout` | `--action-timeout` |
| `gracePeriod` | 30s | `settings.DefaultGracePeriod` | none |
| `maxConcurrency` | 0 (unlimited) | hardcoded | none |

### Why Configure?

- **Long-running actions**: Build/deploy actions may need longer timeouts
- **Graceful shutdown**: Configure how long to wait during cancellation
- **Resource management**: Limit concurrent actions

### Proposed Config Structure

```yaml
version: 1

action:
  # Default timeout per action execution
  defaultTimeout: "5m"
  
  # Grace period for cancellation
  gracePeriod: "30s"
  
  # Maximum concurrent actions (0 = unlimited)
  maxConcurrency: 0
```

### Implementation Tasks

1. **Add `ActionConfig` struct to `pkg/config/types.go`** ✅
   ```go
   type ActionConfig struct {
       DefaultTimeout string `json:"defaultTimeout,omitempty" yaml:"defaultTimeout,omitempty" mapstructure:"defaultTimeout" doc:"Default action timeout" example:"5m" maxLength:"20"`
       GracePeriod    string `json:"gracePeriod,omitempty" yaml:"gracePeriod,omitempty" mapstructure:"gracePeriod" doc:"Cancellation grace period" example:"30s" maxLength:"20"`
       MaxConcurrency int    `json:"maxConcurrency,omitempty" yaml:"maxConcurrency,omitempty" mapstructure:"maxConcurrency" doc:"Max concurrent actions (0=unlimited)" example:"5" maximum:"100"`
   }
   ```

2. **Add to `Config` struct** ✅

3. **Add defaults and validation** ✅

4. **Add `OptionsFromAppConfig()` to `pkg/action/executor.go`** ✅

5. **Update `run solution` command** ✅
   - Uses `cfg.Action.ToActionValues()` helper function

---

## 4. Logging/Debugging Configuration

> **Status**: ✅ Implemented

### Current State

Logging is configured via:
- `logging` section in config file
- `--log-level` CLI flag

### Implemented Config Structure

```yaml
version: 1

logging:
  # Log level (-1=Debug, 0=Info, 1=Warn, 2=Error)
  level: 0
  
  # Output format: text, json
  format: "text"
  
  # Include timestamps in logs
  timestamps: true
  
  # Enable profiling data collection (unhides --pprof flag)
  enableProfiling: false
```

---

## 5. Authentication Configuration

> **Status**: ✅ Implemented (added beyond original plan)

Authentication handler configuration was added to support Microsoft Entra ID:

```yaml
version: 1

auth:
  entra:
    # Azure application ID (uses default if not set)
    clientId: ""
    
    # Default tenant (common, organizations, or specific GUID)
    tenantId: "common"
    
    # Default OAuth scopes requested during login
    defaultScopes:
      - "https://graph.microsoft.com/.default"
```

### Implementation

- `GlobalAuthConfig` and `EntraAuthConfig` structs in `pkg/config/types.go`
- Used by `pkg/auth/entra` package for Entra ID authentication
- Supports CLI override via `scafctl auth login entra --tenant-id=...`

---

## 6. Complete Config File Example

The fully implemented configuration file:

```yaml
version: 1

settings:
  defaultCatalog: "internal"
  noColor: false
  quiet: false

logging:
  level: 0
  format: "text"
  timestamps: true
  enableProfiling: false

httpClient:
  timeout: "30s"
  retryMax: 3
  enableCache: true
  cacheType: "filesystem"
  cacheTTL: "10m"
  enableCircuitBreaker: false
  enableCompression: true

cel:
  cacheSize: 10000
  costLimit: 1000000
  useASTBasedCaching: true
  enableMetrics: true

resolver:
  timeout: "30s"
  phaseTimeout: "5m"
  maxConcurrency: 0
  warnValueSize: 1048576
  maxValueSize: 10485760
  validateAll: false

action:
  defaultTimeout: "5m"
  gracePeriod: "30s"
  maxConcurrency: 0

auth:
  entra:
    tenantId: "common"
    defaultScopes:
      - "https://graph.microsoft.com/.default"

catalogs:
  - name: internal
    type: filesystem
    path: ./catalogs
  - name: production
    type: oci
    url: oci://registry.example.com/scafctl
    httpClient:
      timeout: "60s"  # Per-catalog override
```

---

## Implementation Order

### Phase 1: CEL Configuration ✅ Complete
- Most impactful for performance tuning
- Self-contained changes in `pkg/celexp`
- Implemented in `pkg/celexp/appconfig.go`

### Phase 2: Resolver Configuration ✅ Complete
- High value for users running complex solutions
- Implemented with `ToResolverValues()` helper
- CLI flags override config values

### Phase 3: Action Configuration ✅ Complete
- Similar pattern to resolver config
- Implemented with `ToActionValues()` helper

### Phase 4: Logging Enhancements ✅ Complete
- Full `LoggingConfig` struct with format, timestamps, profiling
- Integrated with root command initialization

### Bonus: Auth Configuration ✅ Complete
- Added Entra ID configuration support
- Not in original plan but implemented for auth feature

---

## Decisions Made

The following design decisions have been confirmed:

| Question | Decision | Notes |
|----------|----------|-------|
| **Solution-level overrides** | ✅ Yes | Solutions can override config defaults in their metadata. Solution settings take precedence over app config. |
| **Config profiles** | ❌ No | Not implementing named profiles at this time. |
| **Unknown config keys** | ⚠️ Warn | Log warnings for unknown keys (implemented in `pkg/config/unknown_keys.go`). Does not fail validation. |
| **Per-catalog resolver/action config** | ❌ No | Only HTTP client has per-catalog overrides. |

### Unknown Keys Implementation

A custom implementation was added since Viper doesn't have built-in "warn on unknown" behavior (only `UnmarshalExact()` which errors):

- `WarnUnknownKeys(ctx)` - logs warnings via logr for unknown config keys
- `GetUnknownKeys()` - returns list of unknown keys for programmatic use
- Uses reflection to derive known keys from Config struct
- Handles Viper's case-insensitive key normalization

**Limitation**: Unknown fields inside array elements (like `catalogs[0].unknownField`) are not detected because Viper stores arrays as single values, not as individual indexed keys.

---

## Questions - All Resolved

| Question | Decision | Implementation |
|----------|----------|----------------|
| Config file vs solution-level defaults | ✅ Yes - Solutions can override | Solution metadata takes precedence |
| Config profiles | ❌ No | Not implementing at this time |
| Config validation strictness | ⚠️ Warn on unknown keys | `pkg/config/unknown_keys.go` |
| Metrics endpoint | 📋 Deferred | May add Prometheus endpoint later |
| Per-catalog resolver/action config | ❌ No | Only HTTP client has per-catalog overrides |

---

## Files Modified

All files have been updated as planned:

| File | Changes | Status |
|------|---------|--------|
| `pkg/config/types.go` | Added `CELConfig`, `ResolverConfig`, `ActionConfig`, `LoggingConfig`, `GlobalAuthConfig`, `EntraAuthConfig` | ✅ |
| `pkg/config/config.go` | Added defaults for all new config sections | ✅ |
| `pkg/config/validation.go` | Added validation for all new config types | ✅ |
| `pkg/config/helpers.go` | Added `ToCELValues()`, `ToResolverValues()`, `ToActionValues()` | ✅ |
| `pkg/config/unknown_keys.go` | Added unknown key detection and warnings | ✅ |
| `pkg/celexp/appconfig.go` | Added `InitFromAppConfig()` | ✅ |
| `pkg/resolver/executor.go` | Added `OptionsFromAppConfig()` | ✅ |
| `pkg/action/executor.go` | Added `OptionsFromAppConfig()` | ✅ |
| `pkg/cmd/scafctl/run/solution.go` | Uses config + CLI override pattern | ✅ |
| `pkg/cmd/scafctl/render/solution.go` | Uses config + CLI override pattern | ✅ |
| `pkg/cmd/scafctl/root.go` | Initializes CEL from app config | ✅ |
| `pkg/schema/config_schema.go` | Schema auto-updates via reflection | ✅ |

---

## Estimated Effort

| Phase | Effort | Status |
|-------|--------|--------|
| Phase 1 (CEL) | ~4-6 hours | ✅ Complete |
| Phase 2 (Resolver) | ~4-6 hours | ✅ Complete |
| Phase 3 (Action) | ~2-3 hours | ✅ Complete |
| Phase 4 (Logging) | ~2-3 hours | ✅ Complete |
| Bonus (Auth) | ~2 hours | ✅ Complete |
| **Total** | **~16-20 hours** | ✅ Complete |

---

## Success Criteria

All criteria have been met:

1. ✅ All new config options have defaults matching current behavior
2. ✅ Existing CLI flags continue to work and override config
3. ✅ Environment variables work for all new options (via Viper `SCAFCTL_` prefix)
4. ✅ JSON Schema includes all new options (auto-generated via reflection)
5. ✅ Tests cover validation and precedence (default < config < env < CLI)
6. ✅ Documentation updated with examples
