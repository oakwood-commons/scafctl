# Actions Implementation Plan

This document outlines the implementation plan for the Actions feature in scafctl, based on the [design specification](design/actions.md).

---

## Executive Summary

The Actions system enables side-effect execution as a declarative action graph. Actions consume resolved data from resolvers, declare dependencies on other actions, and reference results from completed actions. The system supports two modes: `run` (direct execution) and `render` (emit executor-ready artifact).

**Core Principles:**
- Resolvers compute data, Actions perform work
- Actions never feed resolvers
- All resolver CEL/templates evaluated before action execution
- Action-to-action data flow explicit via `__actions.<name>.results`

---

## Design Decisions

| Topic | Decision |
|-------|----------|
| **Shared types** | Extract common types (`ValueRef`, `ForEachClause`, `Condition`, `OnErrorBehavior`) to `pkg/spec/` |
| Execution modes | `run` (execute directly) and `render` (emit graph artifact) |
| Graph structure | DAG with explicit `dependsOn` |
| Result access | Implicit via provider `Output.Data` → `__actions.<name>.results` |
| Deferred expressions | Preserve expressions referencing `__actions` for runtime evaluation |
| Error handling | `onError: fail` (default) or `continue` |
| ForEach in finally | Not allowed (validation enforced) |
| Cross-section deps | Finally cannot `dependsOn` regular actions (implicit dependency on all) |
| Provider capability | Require `CapabilityAction` for action providers |
| Rendered output | JSON (default) or YAML |
| Progress callbacks | Optional interface for real-time feedback |

---

## Shared Types Analysis

Several types are identical or nearly identical between resolvers and actions. To avoid duplication and ensure consistency, these will be extracted to a shared `pkg/spec/` package:

| Type | Current Location | Usage |
|------|------------------|-------|
| `ValueRef` | `pkg/resolver/valueref.go` | Value references (literal, rslvr, expr, tmpl) |
| `ForEachClause` | `pkg/resolver/resolver.go` | Iteration over arrays |
| `Condition` | `pkg/resolver/resolver.go` | CEL expression wrapper for conditionals |
| `OnErrorBehavior` | `pkg/resolver/resolver.go` | Error handling behavior (fail/continue) |
| `IterationContext` | `pkg/resolver/valueref.go` | ForEach iteration variable context |
| Type coercion | `pkg/resolver/types.go` | `CoerceType()` and related functions |

**Note:** The `ForEachClause` in the shared package includes an `OnError` field that is only used by actions (resolvers ignore it). This is acceptable as it maintains a single type definition.

---

## Implementation Phases

### Phase 0: Extract Common Types to Shared Package ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Extract shared types from `pkg/resolver` to `pkg/spec` for reuse by both resolver and action systems

**Motivation:** Several types are identical or nearly identical between resolvers and actions:
- `ValueRef` - Value references (literal, rslvr, expr, tmpl)
- `ForEachClause` - Iteration over arrays
- `Condition` - CEL expression wrapper for conditionals
- `OnErrorBehavior` - Error handling behavior (fail/continue)
- `IterationContext` - ForEach iteration variable context
- Type coercion utilities

**Files created:**
- [x] `pkg/spec/README.md` - Package documentation
- [x] `pkg/spec/valueref.go` - Shared ValueRef and IterationContext types
- [x] `pkg/spec/valueref_test.go` - Tests
- [x] `pkg/spec/condition.go` - Shared Condition type with Evaluate methods
- [x] `pkg/spec/condition_test.go` - Tests
- [x] `pkg/spec/foreach.go` - Shared ForEachClause type
- [x] `pkg/spec/errors.go` - Shared OnErrorBehavior type with IsValid/OrDefault helpers
- [x] `pkg/spec/errors_test.go` - Tests
- [x] `pkg/spec/types.go` - Type coercion utilities (CoerceType, Type constants)
- [x] `pkg/spec/types_test.go` - Tests

**Files modified:**
- [x] `pkg/resolver/resolver.go` - Added type aliases for Type, ErrorBehavior, ForEachClause, and CoerceType from spec
- [x] `pkg/resolver/valueref.go` - Replaced with type aliases for ValueRef and IterationContext from spec
- [x] `pkg/resolver/types.go` - Removed implementation (now re-exported from spec via resolver.go)

**Key implementation details:**
- Type aliases maintain full backward compatibility (`type ValueRef = spec.ValueRef`)
- Constants are re-exported (`const TypeString = spec.TypeString`)
- `Condition` in resolver remains a local struct (not aliased) to preserve resolver-specific YAML structure
- `spec.Condition` has additional methods: `Evaluate`, `EvaluateWithSelf`, `EvaluateWithAdditionalVars`
- `spec.OnErrorBehavior` has helper methods: `IsValid()`, `OrDefault()`
- `spec.ValueRef` has new method: `ReferencesVariable(varName string) bool`

**Types extracted to pkg/spec:**

```go
// pkg/spec/valueref.go

// ValueRef represents a value that can be literal, resolver reference, expression, or template
type ValueRef struct {
    Literal  any                         `json:"-" yaml:"-"`
    Resolver *string                     `json:"rslvr,omitempty" yaml:"rslvr,omitempty" doc:"Reference to another resolver by name"`
    Expr     *celexp.Expression          `json:"expr,omitempty" yaml:"expr,omitempty" doc:"CEL expression to evaluate"`
    Tmpl     *gotmpl.GoTemplatingContent `json:"tmpl,omitempty" yaml:"tmpl,omitempty" doc:"Go template to execute"`
}

// IterationContext holds the context for forEach iteration variables
type IterationContext struct {
    Item       any    // Current array element (__item)
    Index      int    // Current index (__index)
    ItemAlias  string // Custom name for item (if specified)
    IndexAlias string // Custom name for index (if specified)
}

// Resolve resolves the ValueRef to a concrete value
func (v *ValueRef) Resolve(ctx context.Context, resolverData map[string]any, self any) (any, error)

// ResolveWithIterationContext resolves with optional forEach iteration context
func (v *ValueRef) ResolveWithIterationContext(ctx context.Context, resolverData, self any, iterCtx *IterationContext) (any, error)

// ReferencesVariable checks if the ValueRef references a specific variable name
func (v *ValueRef) ReferencesVariable(varName string) bool
```

```go
// pkg/spec/condition.go

// Condition represents a conditional execution clause
type Condition struct {
    Expr *celexp.Expression `json:"expr" yaml:"expr" doc:"CEL expression that must evaluate to boolean"`
}

// Evaluate evaluates the condition with the given data
func (c *Condition) Evaluate(ctx context.Context, data map[string]any) (bool, error)
```

```go
// pkg/spec/foreach.go

// ForEachClause defines iteration over an array
type ForEachClause struct {
    Item        string          `json:"item,omitempty" yaml:"item,omitempty" doc:"Variable name alias for current element"`
    Index       string          `json:"index,omitempty" yaml:"index,omitempty" doc:"Variable name alias for current index"`
    In          *ValueRef       `json:"in,omitempty" yaml:"in,omitempty" doc:"Array to iterate over"`
    Concurrency int             `json:"concurrency,omitempty" yaml:"concurrency,omitempty" doc:"Max parallel iterations (0=unlimited)"`
    OnError     OnErrorBehavior `json:"onError,omitempty" yaml:"onError,omitempty" doc:"Error handling (actions only)"`
}
```

```go
// pkg/spec/errors.go

// OnErrorBehavior defines how to handle errors
type OnErrorBehavior string

const (
    OnErrorFail     OnErrorBehavior = "fail"     // Stop and return error (default)
    OnErrorContinue OnErrorBehavior = "continue" // Continue execution
)
```

**Migration strategy:**
1. Create `pkg/spec/` with extracted types
2. Update `pkg/resolver/` to import from `pkg/spec/`
3. Add type aliases in `pkg/resolver/` for backward compatibility (optional, can be removed)
4. Run tests to ensure no regressions

**Estimated effort:** 2-3 days

---

### Phase 1: Core Action Types & Spec Extension ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Define action-specific types and extend solution spec

**Files created:**
- [x] `pkg/action/types.go` - Action-specific types (Workflow, Action, RetryConfig, BackoffType, ActionStatus, SkipReason, ActionResult, ForEachIterationResult)
- [x] `pkg/action/duration.go` - Duration type with JSON/YAML marshaling support
- [x] `pkg/action/types_test.go` - Comprehensive tests for action types

**Files modified:**
- [x] `pkg/solution/spec.go` - Extended Spec with Workflow field, added HasWorkflow() and HasActions() helper methods

**Key implementation details:**
- `Duration` wraps `time.Duration` with proper JSON/YAML serialization (string format like "30s", "1m")
- `ActionStatus` has `IsTerminal()` and `IsSuccess()` helper methods
- `BackoffType` has `IsValid()` and `OrDefault()` helper methods
- `ActionResult.Duration()` and `ForEachIterationResult.Duration()` compute execution time
- `Spec.HasWorkflow()` and `Spec.HasActions()` check for workflow presence
- Uses shared types from `pkg/spec`: `ValueRef`, `Condition`, `ForEachClause`, `OnErrorBehavior`

**Types implemented:**

```go
// pkg/action/types.go

import "github.com/oakwood-commons/scafctl/pkg/spec"

// Workflow contains the action execution specification
type Workflow struct {
    Actions map[string]*Action `json:"actions,omitempty" yaml:"actions,omitempty"`
    Finally map[string]*Action `json:"finally,omitempty" yaml:"finally,omitempty"`
}

// Action represents a single action definition
type Action struct {
    // Metadata
    Name        string `json:"name" yaml:"name"`
    Description string `json:"description,omitempty" yaml:"description,omitempty"`
    DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
    Sensitive   bool   `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`

    // Provider
    Provider string `json:"provider" yaml:"provider"`

    // Inputs (uses shared ValueRef)
    Inputs map[string]*spec.ValueRef `json:"inputs,omitempty" yaml:"inputs,omitempty"`

    // Dependencies
    DependsOn []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`

    // Conditional execution (uses shared Condition)
    When *spec.Condition `json:"when,omitempty" yaml:"when,omitempty"`

    // Error handling (uses shared OnErrorBehavior)
    OnError spec.OnErrorBehavior `json:"onError,omitempty" yaml:"onError,omitempty"`

    // Timeout
    Timeout *time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`

    // Retry (action-specific)
    Retry *RetryConfig `json:"retry,omitempty" yaml:"retry,omitempty"`

    // Iteration (uses shared ForEachClause)
    ForEach *spec.ForEachClause `json:"forEach,omitempty" yaml:"forEach,omitempty"`
}

// RetryConfig defines retry behavior (action-specific)
type RetryConfig struct {
    MaxAttempts  int           `json:"maxAttempts" yaml:"maxAttempts"`
    Backoff      BackoffType   `json:"backoff,omitempty" yaml:"backoff,omitempty"`
    InitialDelay time.Duration `json:"initialDelay,omitempty" yaml:"initialDelay,omitempty"`
    MaxDelay     time.Duration `json:"maxDelay,omitempty" yaml:"maxDelay,omitempty"`
}

// BackoffType defines backoff strategy
type BackoffType string

const (
    BackoffFixed       BackoffType = "fixed"
    BackoffLinear      BackoffType = "linear"
    BackoffExponential BackoffType = "exponential"
)

// ActionStatus represents execution status
type ActionStatus string

const (
    StatusPending   ActionStatus = "pending"
    StatusRunning   ActionStatus = "running"
    StatusSucceeded ActionStatus = "succeeded"
    StatusFailed    ActionStatus = "failed"
    StatusSkipped   ActionStatus = "skipped"
    StatusTimeout   ActionStatus = "timeout"
    StatusCancelled ActionStatus = "cancelled"
)

// SkipReason indicates why an action was skipped
type SkipReason string

const (
    SkipReasonCondition        SkipReason = "condition"
    SkipReasonDependencyFailed SkipReason = "dependency-failed"
)

// ActionResult represents the result of an action execution
type ActionResult struct {
    Inputs     map[string]any `json:"inputs" yaml:"inputs"`
    Results    any            `json:"results,omitempty" yaml:"results,omitempty"`
    Status     ActionStatus   `json:"status" yaml:"status"`
    SkipReason SkipReason     `json:"skipReason,omitempty" yaml:"skipReason,omitempty"`
    StartTime  *time.Time     `json:"startTime,omitempty" yaml:"startTime,omitempty"`
    EndTime    *time.Time     `json:"endTime,omitempty" yaml:"endTime,omitempty"`
    Error      string         `json:"error,omitempty" yaml:"error,omitempty"`
}

// ForEachIterationResult represents results from forEach expansion
type ForEachIterationResult struct {
    Index     int          `json:"index" yaml:"index"`
    Name      string       `json:"name" yaml:"name"`
    Results   any          `json:"results,omitempty" yaml:"results,omitempty"`
    Status    ActionStatus `json:"status" yaml:"status"`
    StartTime *time.Time   `json:"startTime,omitempty" yaml:"startTime,omitempty"`
    EndTime   *time.Time   `json:"endTime,omitempty" yaml:"endTime,omitempty"`
    Error     string       `json:"error,omitempty" yaml:"error,omitempty"`
}

// Duration wraps time.Duration with JSON/YAML marshaling
type Duration time.Duration
```

---

### Phase 2: Deferred Expression Support ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Add deferred expression handling for `__actions` references

**Files created:**
- [x] `pkg/action/deferred.go` - `DeferredValue` type and `Materialize`/`MaterializeInputs`/`ResolveDeferredValues` functions
- [x] `pkg/action/deferred_test.go` - Comprehensive tests for deferred expression handling

**Files modified:**
- [x] `pkg/celexp/context.go` - Added `VarActions` constant for `__actions` variable name
- [x] `pkg/spec/valueref.go` - Enhanced `ReferencesVariable` method to detect both top-level variables (like `__actions`) and underscore-prefixed variables (like `_.resolver`)

**Key implementation details:**
- `DeferredValue` preserves expressions that reference `__actions` for runtime evaluation
- `Materialize` checks if a `ValueRef` references `__actions` before resolving; if so, returns a `DeferredValue`
- `MaterializeInputs` processes all action inputs, materializing immediate values and preserving deferred ones
- `ResolveDeferredValues` evaluates deferred values at runtime when action results are available
- `ReferencesVariable` now checks both `RequiredVariables()` (top-level like `__actions`) and `GetUnderscoreVariables()` (resolver refs like `_.env`)
- Template reference detection handles the leading dot in paths (e.g., `.__actions.build.results` → `__actions`)

**Types implemented:**

```go
// pkg/action/deferred.go

// DeferredValue represents an expression preserved for runtime evaluation
type DeferredValue struct {
    OriginalExpr string `json:"originalExpr,omitempty" yaml:"originalExpr,omitempty"`
    OriginalTmpl string `json:"originalTmpl,omitempty" yaml:"originalTmpl,omitempty"`
    Deferred     bool   `json:"deferred" yaml:"deferred"`
}

// Materialize evaluates a ValueRef, returning either a concrete value
// or a DeferredValue if it references __actions
func Materialize(ctx context.Context, v *spec.ValueRef, resolverData map[string]any) (any, error)

// MaterializeInputs processes all inputs for an action
func MaterializeInputs(ctx context.Context, inputs map[string]*spec.ValueRef, resolverData map[string]any) (map[string]any, error)

// ResolveDeferredValues evaluates all deferred values using action results
func ResolveDeferredValues(ctx context.Context, values map[string]any, resolverData map[string]any, actionsData map[string]any) (map[string]any, error)

// HasDeferredValues checks if any values in the map are deferred
func HasDeferredValues(values map[string]any) bool

// GetDeferredInputNames returns names of inputs containing deferred values
func GetDeferredInputNames(values map[string]any) []string
```

```go
// pkg/celexp/context.go (addition)

const (
    VarActions = "__actions"  // Variable name for actions namespace
)
```

```go
// pkg/spec/valueref.go (enhanced)

// ReferencesVariable checks if the ValueRef references a specific variable name.
// Checks both top-level variables (like __actions, __self) and underscore-prefixed
// variables (like _.resolver references).
func (v *ValueRef) ReferencesVariable(varName string) bool
```

---

### Phase 3: Action Context & Namespace ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Implement `__actions` namespace management with thread-safe storage

**Files created:**
- [x] `pkg/action/context.go` - `Context` struct with thread-safe __actions namespace management
- [x] `pkg/action/context_test.go` - Comprehensive tests including thread-safety tests

**Key implementation details:**
- `Context` (renamed from ActionContext to avoid stutter when accessed as `action.Context`) provides thread-safe storage for action results using `sync.RWMutex`
- Support for forEach iteration results aggregation via `AddIteration` and `FinalizeForEach`
- Convenience methods for marking action status: `MarkRunning`, `MarkSucceeded`, `MarkFailed`, `MarkSkipped`, `MarkTimeout`, `MarkCancelled`
- `GetNamespace()` returns the actions data as `map[string]any` for CEL/template evaluation
- `Clone()` creates a deep copy for testing or snapshots
- `Reset()` clears all stored results

**Types implemented:**

```go
// pkg/action/context.go

// Context manages the __actions namespace during workflow execution.
// It provides thread-safe storage for action results and supports forEach
// iteration result aggregation.
type Context struct {
    mu         sync.RWMutex
    actions    map[string]*ActionResult
    iterations map[string][]*ForEachIterationResult
}

// NewContext creates a new action context for workflow execution.
func NewContext() *Context

// SetResult stores an action's result in the context.
func (c *Context) SetResult(name string, result *ActionResult)

// GetResult retrieves an action's result by name.
func (c *Context) GetResult(name string) (*ActionResult, bool)

// HasResult checks if a result exists for the given action name.
func (c *Context) HasResult(name string) bool

// GetNamespace returns the __actions map for CEL/template evaluation.
func (c *Context) GetNamespace() map[string]any

// AddIteration records a forEach iteration result.
func (c *Context) AddIteration(actionName string, result *ForEachIterationResult)

// GetIterations retrieves all iteration results for a forEach action.
func (c *Context) GetIterations(actionName string) []*ForEachIterationResult

// FinalizeForEach aggregates forEach iteration results into a single ActionResult.
func (c *Context) FinalizeForEach(actionName string, inputs map[string]any) *ActionResult

// MarkRunning marks an action as running with the current time.
func (c *Context) MarkRunning(name string, inputs map[string]any)

// MarkSucceeded marks an action as successfully completed.
func (c *Context) MarkSucceeded(name string, results any)

// MarkFailed marks an action as failed with an error message.
func (c *Context) MarkFailed(name, errMsg string)

// MarkSkipped marks an action as skipped with a reason.
func (c *Context) MarkSkipped(name string, reason SkipReason)

// MarkTimeout marks an action as timed out.
func (c *Context) MarkTimeout(name string)

// MarkCancelled marks an action as cancelled.
func (c *Context) MarkCancelled(name string)

// AllActionNames returns all action names that have results.
func (c *Context) AllActionNames() []string

// ActionCount returns the number of actions with results.
func (c *Context) ActionCount() int

// Reset clears all stored results and iterations.
func (c *Context) Reset()

// Clone creates a deep copy of the action context.
func (c *Context) Clone() *Context
```

---

### Phase 4: Validation ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Implement comprehensive action validation

**Files created:**
- [x] `pkg/action/validation.go` - Validation logic with `ValidateWorkflow` function
- [x] `pkg/action/validation_test.go` - Comprehensive tests for all validation rules

**Key implementation details:**
- `ValidationError` provides detailed context (section, action name, field, message)
- `AggregatedValidationError` collects multiple validation failures
- `RegistryInterface` allows for provider registry mocking in tests
- Cycle detection using DFS with path reconstruction
- `__actions` reference extraction from CEL expressions and Go templates
- Uses `celexp.Expression.RequiredVariables()` and `gotmpl.GetGoTemplateReferences()` for parsing

**Validation rules implemented:**

1. ✅ Action names must match `^[a-zA-Z_][a-zA-Z0-9_-]*$`
2. ✅ Action names starting with `__` are reserved
3. ✅ Action names containing `[` or `]` are reserved (for forEach expansion)
4. ✅ Action names unique across `workflow.actions` and `workflow.finally`
5. ✅ `dependsOn` in `workflow.actions` references existing actions in same section
6. ✅ `dependsOn` in `workflow.finally` references only finally actions
7. ✅ No dependency cycles (within each section)
8. ✅ Provider exists and has `CapabilityAction` (when registry provided)
9. ✅ `__actions.<name>` references must be valid (in dependsOn for actions, or any regular action for finally)
10. ✅ `forEach` only in `workflow.actions`, not `workflow.finally`
11. ✅ `retry.maxAttempts` >= 1
12. ✅ `timeout` is valid duration (implicit via Duration type)
13. ✅ `forEach.onError` is `fail` or `continue`
14. ✅ `forEach.concurrency` >= 0

**Types implemented:**

```go
// pkg/action/validation.go

// ValidationError provides detailed validation failure information.
type ValidationError struct {
    Section    string `json:"section,omitempty" yaml:"section,omitempty" doc:"Workflow section (actions or finally)"`
    ActionName string `json:"actionName,omitempty" yaml:"actionName,omitempty" doc:"Action that failed validation"`
    Field      string `json:"field,omitempty" yaml:"field,omitempty" doc:"Field that failed validation"`
    Message    string `json:"message" yaml:"message" doc:"Validation failure message"`
}

// AggregatedValidationError represents multiple validation errors.
type AggregatedValidationError struct {
    Errors []*ValidationError `json:"errors" yaml:"errors" doc:"All validation errors"`
}

// RegistryInterface defines the provider registry operations needed for validation.
type RegistryInterface interface {
    Get(name string) (provider.Provider, bool)
    Has(name string) bool
}

// ValidateWorkflow validates the entire workflow definition.
// Pass nil for registry to skip provider capability checks.
func ValidateWorkflow(w *Workflow, registry RegistryInterface) error

// Helper functions for __actions reference extraction
func extractActionsReferences(action *Action) []string
func parseActionsRefsFromString(s string, refs map[string]struct{})

// Cycle detection
func findCycle(deps map[string][]string) []string
```

---

### Phase 5: Dependency Graph Building ✅

**Status:** Complete

**Goal:** Build action DAG with forEach expansion

**Files created:**
- [x] `pkg/action/graph.go` - DAG construction
- [x] `pkg/action/graph_test.go` - Tests

**Implemented features:**
- Extract dependencies from `dependsOn` and `__actions` references in expressions
- Expand forEach actions at render time (e.g., `deploy` → `deploy[0]`, `deploy[1]`, etc.)
- Handle dependency rewriting for expanded actions (depending on a forEach action depends on all iterations)
- Compute execution phases using topological sort (Kahn's algorithm)
- Separate execution phases for main actions and finally actions

**Key types and functions:**

```go
// Graph represents the executable action graph (renamed from ActionGraph to avoid stutter)
type Graph struct {
    Actions        map[string]*ExpandedAction
    ExecutionOrder [][]string // Phases of parallel action names
    FinallyOrder   [][]string // Phases for finally section
}

// ExpandedAction is an action with materialized inputs (where possible)
type ExpandedAction struct {
    *Action
    ExpandedName       string // The name for this expanded action (e.g., "deploy[0]")
    MaterializedInputs map[string]any
    DeferredInputs     map[string]*DeferredValue
    Section            string // "actions" or "finally"
    ForEachMetadata    *ForEachExpansionMetadata
    Dependencies       []string // Effective dependencies for scheduling
}

// ForEachExpansionMetadata tracks forEach expansion info
type ForEachExpansionMetadata struct {
    ExpandedFrom string
    Index        int
    Item         any
}

// BuildGraphOptions configures graph building behavior
type BuildGraphOptions struct {
    SkipInputMaterialization bool
}

// BuildGraph constructs the action dependency graph from a workflow
func BuildGraph(ctx context.Context, w *Workflow, resolverData map[string]any, opts *BuildGraphOptions) (*Graph, error)

// Helper methods on Graph
func (g *Graph) GetAllActionNames() []string
func (g *Graph) GetMainActionNames() []string
func (g *Graph) GetFinallyActionNames() []string
func (g *Graph) GetForEachIterations(baseName string) []string
func (g *Graph) TotalPhases() int
func (g *Graph) TotalFinallyPhases() int
func (g *Graph) GetActionsByPhase(phase int) []*ExpandedAction
func (g *Graph) GetFinallyActionsByPhase(phase int) []*ExpandedAction

// Helper methods on ExpandedAction
func (e *ExpandedAction) HasDeferredInputs() bool
func (e *ExpandedAction) GetOriginalName() string
func (e *ExpandedAction) GetExpandedName() string
func (e *ExpandedAction) IsForEachIteration() bool
```

**Test coverage:**
- BuildGraph with nil workflow, empty workflow, single action
- Action dependencies (linear chain, diamond, parallel)
- Resolver data integration
- Deferred inputs and implicit dependencies
- ForEach expansion with various configurations
- Finally section handling
- Skip input materialization option
- Multiple chained forEach actions
- Helper methods

---

### Phase 6: Renderer ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Implement the `render` command output

**Files created:**
- [x] `pkg/action/renderer.go` - Graph rendering with JSON/YAML output support
- [x] `pkg/action/renderer_test.go` - Comprehensive tests

**Key implementation details:**
- `Render()` produces executor-ready action graph artifacts in JSON or YAML format
- `RenderToStruct()` provides programmatic access to the rendered graph without serialization
- `RenderedGraph` structure includes APIVersion, Kind, metadata, execution order, and actions
- `RenderedMetadata` includes generation timestamp, action counts, phase counts, and forEach expansions map
- `RenderedAction` includes all action fields serialized with appropriate types (durations as strings, conditions as DeferredValue)
- Conditions are preserved as `DeferredValue` for executor flexibility
- ForEach expansion metadata tracks original name, index, item, concurrency, and onError settings
- Retry configuration serialized with duration strings
- `GetFormat()` helper normalizes format strings (json, yaml, yml, etc.)
- `DefaultRenderOptions()` provides sensible defaults (JSON, timestamps enabled, pretty print)

**Output formats:**
- JSON (default) - with optional compact or pretty-print modes
- YAML - using gopkg.in/yaml.v3

**Types implemented:**

```go
// pkg/action/renderer.go

const (
    APIVersion = "scafctl.oakwood-commons.github.io/v1alpha1"
    KindActionGraph = "ActionGraph"
    FormatJSON = "json"
    FormatYAML = "yaml"
)

// RenderedGraph is the executor-ready action graph output structure.
type RenderedGraph struct {
    APIVersion     string                      `json:"apiVersion" yaml:"apiVersion"`
    Kind           string                      `json:"kind" yaml:"kind"`
    Metadata       *RenderedMetadata           `json:"metadata,omitempty" yaml:"metadata,omitempty"`
    ExecutionOrder [][]string                  `json:"executionOrder" yaml:"executionOrder"`
    FinallyOrder   [][]string                  `json:"finallyOrder,omitempty" yaml:"finallyOrder,omitempty"`
    Actions        map[string]*RenderedAction  `json:"actions" yaml:"actions"`
}

// RenderedMetadata contains graph-level metadata.
type RenderedMetadata struct {
    GeneratedAt       string              `json:"generatedAt,omitempty" yaml:"generatedAt,omitempty"`
    TotalActions      int                 `json:"totalActions" yaml:"totalActions"`
    TotalPhases       int                 `json:"totalPhases" yaml:"totalPhases"`
    HasFinally        bool                `json:"hasFinally" yaml:"hasFinally"`
    ForEachExpansions map[string][]string `json:"forEachExpansions,omitempty" yaml:"forEachExpansions,omitempty"`
}

// RenderedAction is a fully rendered action ready for execution.
type RenderedAction struct {
    Name         string                   `json:"name" yaml:"name"`
    OriginalName string                   `json:"originalName,omitempty" yaml:"originalName,omitempty"`
    Description  string                   `json:"description,omitempty" yaml:"description,omitempty"`
    DisplayName  string                   `json:"displayName,omitempty" yaml:"displayName,omitempty"`
    Sensitive    bool                     `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`
    Provider     string                   `json:"provider" yaml:"provider"`
    DependsOn    []string                 `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
    When         any                      `json:"when,omitempty" yaml:"when,omitempty"`
    OnError      string                   `json:"onError,omitempty" yaml:"onError,omitempty"`
    Timeout      string                   `json:"timeout,omitempty" yaml:"timeout,omitempty"`
    Retry        *RenderedRetryConfig     `json:"retry,omitempty" yaml:"retry,omitempty"`
    Inputs       map[string]any           `json:"inputs" yaml:"inputs"`
    Section      string                   `json:"section" yaml:"section"`
    ForEach      *RenderedForEachMetadata `json:"forEach,omitempty" yaml:"forEach,omitempty"`
}

// RenderedRetryConfig is the serializable retry configuration.
type RenderedRetryConfig struct {
    MaxAttempts  int    `json:"maxAttempts" yaml:"maxAttempts"`
    Backoff      string `json:"backoff,omitempty" yaml:"backoff,omitempty"`
    InitialDelay string `json:"initialDelay,omitempty" yaml:"initialDelay,omitempty"`
    MaxDelay     string `json:"maxDelay,omitempty" yaml:"maxDelay,omitempty"`
}

// RenderedForEachMetadata tracks forEach expansion information.
type RenderedForEachMetadata struct {
    ExpandedFrom string `json:"expandedFrom" yaml:"expandedFrom"`
    Index        int    `json:"index" yaml:"index"`
    Item         any    `json:"item,omitempty" yaml:"item,omitempty"`
    Concurrency  int    `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
    OnError      string `json:"onError,omitempty" yaml:"onError,omitempty"`
}

// RenderOptions configures rendering behavior.
type RenderOptions struct {
    Format           string // "json" or "yaml"
    IncludeTimestamp bool
    PrettyPrint      bool
}

// Render produces an executor-ready action graph artifact.
func Render(graph *Graph, opts *RenderOptions) ([]byte, error)

// RenderToStruct produces a RenderedGraph struct without serialization.
func RenderToStruct(graph *Graph, opts *RenderOptions) (*RenderedGraph, error)

// GetFormat normalizes and validates the output format string.
func GetFormat(format string) (string, error)

// DefaultRenderOptions returns the default rendering options.
func DefaultRenderOptions() *RenderOptions
```

**Test coverage:**
- Nil and empty graph handling
- Single action rendering (JSON and YAML)
- Action dependencies and execution order
- Deferred inputs preservation (both expr and tmpl)
- Condition rendering as DeferredValue
- Timeout and retry configuration serialization
- OnError behavior preservation
- ForEach expansion metadata
- Finally section handling
- Sensitive and display name fields
- Timestamp generation
- Compact vs pretty-print JSON
- Format validation and normalization
- Nil action handling in graph
- Complex graph integration test

---

### Phase 7: Executor (Run Mode) ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Implement action execution engine

**Files created:**
- [x] `pkg/action/executor.go` - Execution engine with phase-based parallel execution
- [x] `pkg/action/executor_test.go` - Comprehensive tests

**Key implementation details:**
- `Executor` struct manages workflow execution with configurable options
- `Execute()` runs workflows in dependency order with parallel phase execution
- Phase-based execution: actions within a phase run concurrently, phases run sequentially
- `maxConcurrency` option limits parallel action execution
- Timeout handling per action with default timeout fallback
- Error handling with `onError: fail` (default) or `continue` behavior
- Dependency failure propagation: actions skip if dependencies fail
- Graceful cancellation with context support
- Finally section always executes, even after main section failures
- Progress callbacks for real-time execution monitoring
- Integration with retry executor for automatic retry support
- Deferred input resolution at execution time using `__actions` namespace

**Types implemented:**

```go
// pkg/action/executor.go

// Executor runs actions in dependency order with support for parallel execution,
// retry, timeout, and error handling.
type Executor struct {
    registry         RegistryInterface
    resolverData     map[string]any
    actionContext    *Context
    progressCallback ProgressCallback
    maxConcurrency   int
    gracePeriod      time.Duration
    defaultTimeout   time.Duration
}

// ExecutorOption configures the executor
type ExecutorOption func(*Executor)

// Execute runs the workflow actions in dependency order.
func (e *Executor) Execute(ctx context.Context, w *Workflow) (*ExecutionResult, error)

// ExecutionResult contains the final execution state
type ExecutionResult struct {
    Actions        map[string]*ActionResult
    FinalStatus    ExecutionStatus // succeeded, failed, cancelled, partial-success
    StartTime      time.Time
    EndTime        time.Time
    FailedActions  []string
    SkippedActions []string
}

// ExecutionStatus represents the overall execution status
type ExecutionStatus string

const (
    ExecutionSucceeded      ExecutionStatus = "succeeded"
    ExecutionFailed         ExecutionStatus = "failed"
    ExecutionCancelled      ExecutionStatus = "cancelled"
    ExecutionPartialSuccess ExecutionStatus = "partial-success"
)

// ProgressCallback receives execution events
type ProgressCallback interface {
    OnActionStart(actionName string)
    OnActionComplete(actionName string, results any)
    OnActionFailed(actionName string, err error)
    OnActionSkipped(actionName, reason string)
    OnActionTimeout(actionName string, timeout time.Duration)
    OnActionCancelled(actionName string)
    OnRetryAttempt(actionName string, attempt, maxAttempts int, err error)
    OnForEachProgress(actionName string, completed, total int)
    OnPhaseStart(phase int, actionNames []string)
    OnPhaseComplete(phase int)
    OnFinallyStart()
    OnFinallyComplete()
}

// NoOpProgressCallback is a progress callback that does nothing.
type NoOpProgressCallback struct{}
```

**Executor options:**
- `WithRegistry(registry)` - Set provider registry
- `WithResolverData(data)` - Set resolver data for input resolution
- `WithProgressCallback(callback)` - Set progress callback
- `WithMaxConcurrency(n)` - Limit parallel actions
- `WithGracePeriod(d)` - Set cancellation grace period
- `WithDefaultTimeout(d)` - Set default action timeout

**Test coverage:**
- Nil and empty workflow handling
- Single action execution
- Action chain (sequential dependencies)
- Parallel action execution
- Action failure handling
- OnError continue behavior
- Dependency failure skipping
- Timeout handling
- Cancellation handling
- Finally section execution (always runs)
- Finally runs after failures
- Progress callback events
- Retry integration
- Max concurrency limiting
- Executor reset

---

### Phase 8: Retry Logic ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Implement retry with backoff strategies

**Files created:**
- [x] `pkg/action/retry.go` - Retry logic with backoff strategies
- [x] `pkg/action/retry_test.go` - Comprehensive tests

**Key implementation details:**
- `RetryExecutor` wraps action execution with configurable retry behavior
- Support for three backoff strategies: fixed, linear, exponential
- `CalculateDelay()` computes delay based on attempt number and strategy
- Max delay cap prevents excessive waits
- Context-aware: respects cancellation during retry delays
- Optional jitter function for testing or production use
- `RetryCallback` interface for progress reporting on retries
- `ExecuteWithRetryDetailed()` provides detailed execution results

**Types implemented:**

```go
// pkg/action/retry.go

// RetryExecutor wraps action execution with retry logic and backoff strategies.
type RetryExecutor struct {
    config   *RetryConfig
    jitterFn func(time.Duration) time.Duration
}

// NewRetryExecutor creates a new retry executor with the given configuration.
func NewRetryExecutor(config *RetryConfig) *RetryExecutor

// WithJitter sets a custom jitter function for testing.
func (r *RetryExecutor) WithJitter(fn func(time.Duration) time.Duration) *RetryExecutor

// CalculateDelay computes the delay before a retry attempt based on backoff strategy.
func (r *RetryExecutor) CalculateDelay(attempt int) time.Duration

// MaxAttempts returns the maximum number of execution attempts.
func (r *RetryExecutor) MaxAttempts() int

// ShouldRetry determines if an execution should be retried.
func (r *RetryExecutor) ShouldRetry(ctx context.Context, err error, attempt int) bool

// ExecuteWithRetry runs an action with retry support.
func (r *RetryExecutor) ExecuteWithRetry(
    ctx context.Context,
    actionName string,
    execute ExecuteFunc,
    callback RetryCallback,
) (*provider.Output, error)

// ExecuteWithRetryDetailed runs an action with retry support and detailed results.
func (r *RetryExecutor) ExecuteWithRetryDetailed(
    ctx context.Context,
    actionName string,
    execute ExecuteFunc,
    callback RetryCallback,
) *RetryResult

// RetryResult contains information about a retry execution.
type RetryResult struct {
    Output        *provider.Output
    Attempts      int
    TotalDuration time.Duration
    FinalError    error
    AttemptErrors []error
}

// RetryCallback receives retry events for progress reporting.
type RetryCallback interface {
    OnRetryAttempt(actionName string, attempt, maxAttempts int, err error)
}

// ExecuteFunc is the function signature for action execution.
type ExecuteFunc func(ctx context.Context) (*provider.Output, error)
```

**Backoff calculations:**
- **Fixed:** `delay = initialDelay` (constant)
- **Linear:** `delay = initialDelay * retryNumber` (increases linearly)
- **Exponential:** `delay = min(initialDelay * 2^(retryNumber-1), maxDelay)` (doubles each retry)

**Test coverage:**
- Constructor with nil and valid config
- MaxAttempts with various configs
- CalculateDelay for all backoff strategies
- Max delay cap verification
- Jitter function support
- ShouldRetry logic (nil error, nil config, max attempts, cancelled context)
- ExecuteWithRetry success on first attempt
- ExecuteWithRetry success on retry
- ExecuteWithRetry all attempts fail
- No retries with nil config
- Cancellation during retry delay
- Cancellation before first attempt
- Retry callback invocation
- ExecuteWithRetryDetailed comprehensive results
- Backoff strategy mathematical properties

---

### Phase 9: ForEach Expansion & Execution ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Implement forEach iteration for actions

**Files created:**
- [x] `pkg/action/foreach.go` - ForEach expansion and execution
- [x] `pkg/action/foreach_test.go` - Comprehensive tests

**Key implementation details:**
- `ForEachExecutor` struct manages concurrent iteration execution with semaphores
- `FromForEachClause` factory creates executor from spec.ForEachClause
- `Execute` runs iterations with concurrency control and error handling
- `CreateIterationName` generates iteration names: `actionName[index]`
- `IsForEachIteration` detects iteration names using pattern matching
- `ParseIterationName` extracts base name and index from iteration names
- `AggregateForEachResults` combines iteration results into ActionResult

**Types implemented:**

```go
// pkg/action/foreach.go

// ForEachExecutor manages forEach iteration execution
type ForEachExecutor struct {
    items            []any
    concurrencyLimit int
    onError          spec.OnErrorBehavior
    itemVarName      string
    indexVarName     string
}

// ForEachResult contains the overall forEach execution result
type ForEachResult struct {
    Iterations []*ForEachIterationResult
    TotalCount int
    Succeeded  int
    Failed     int
    Skipped    int
    Cancelled  int
    Error      error
}

// ForEachIterationResult contains a single iteration's result
type ForEachIterationResult struct {
    Index     int
    Name      string
    Item      any
    Output    *provider.Output
    Error     error
    Status    Status
    StartTime *time.Time
    EndTime   *time.Time
}

// ForEachProgressCallback reports iteration progress
type ForEachProgressCallback func(completed, total int, current *ForEachIterationResult)

// Factory function
func FromForEachClause(clause *spec.ForEachClause) (*ForEachExecutor, error)

// Execute runs all iterations with concurrency control
func (e *ForEachExecutor) Execute(
    ctx context.Context,
    actionName string,
    execute func(ctx context.Context, item any, index int) (*provider.Output, error),
    progressCallback ForEachProgressCallback,
) *ForEachResult

// Helper functions
func CreateIterationName(baseName string, index int) string
func IsForEachIteration(name string) bool
func ParseIterationName(name string) (baseName string, index int, ok bool)
func AggregateForEachResults(actionName string, iterations []*ForEachIterationResult, inputs map[string]any) *ActionResult
```

**Test coverage:**
- Concurrency limit enforcement with semaphores
- `onError: fail` stops on first error
- `onError: continue` processes all items
- Progress callback invocation
- Iteration name creation and parsing
- Result aggregation with status tracking

---

### Phase 10: Provider Capability Extension ✅ COMPLETED

**Status:** Completed (pre-existing implementation verified on 2026-01-29)

**Goal:** Extend provider system for action support

**Implementation verified:**
- [x] `pkg/provider/provider.go` - `CapabilityAction` already defined
- [x] `pkg/provider/builtin/http/` - Has CapabilityAction
- [x] `pkg/provider/builtin/file/` - Has CapabilityAction  
- [x] `pkg/provider/builtin/exec/` - Has CapabilityAction
- [x] `pkg/provider/builtin/sleep/` - Has CapabilityAction
- [x] `pkg/provider/builtin/git/` - Has CapabilityAction
- [x] `pkg/provider/builtin/gotmpl/` - Has CapabilityAction

**Key implementation details:**
- `CapabilityAction` constant defined in `pkg/provider/provider.go`
- Providers declare capabilities via `GetCapabilities()` method
- Action system validates provider capability before execution
- Provider `Output` struct provides standardized result format

```go
// pkg/provider/provider.go (existing)
const CapabilityAction Capability = "action"

// Provider interface (partial)
type Provider interface {
    GetCapabilities() []Capability
    Execute(ctx context.Context, input *Input) (*Output, error)
}
```

---

### Phase 11: CLI Integration ✅ COMPLETED (Updated 2026-02-02)

**Status:** Completed on 2026-01-29, refactored on 2026-02-02

**Goal:** Integrate action execution into `run solution` command

**Note:** The separate `run workflow` command was merged into `run solution` per the CLI design doc. Solutions now execute both resolvers AND actions.

**Files created/modified:**
- [x] `pkg/cmd/scafctl/render/render.go` - Render command root
- [x] `pkg/cmd/scafctl/render/workflow.go` - Render workflow subcommand (to be refactored into render/solution.go)
- [x] `pkg/cmd/scafctl/run/solution.go` - **Now executes resolvers + actions**

**Files removed:**
- [x] `pkg/cmd/scafctl/run/workflow.go` - Merged into solution.go

**Commands implemented:**

```bash
# Run solution (resolvers + actions)
scafctl run solution <file> [flags]
  --dry-run                    # Show what would be executed
  --skip-actions               # Run resolvers only (skip actions)
  --max-action-concurrency=N   # Limit parallel actions (default: unlimited)
  --action-timeout=5m          # Default timeout per action
  --progress                   # Show progress output
  -o table|json|yaml|quiet     # Output format (default: table)
  -r key=value                 # Override resolver/input values

# Render executor-ready graph
scafctl render solution <file> [flags]
  -o json|yaml     # Output format (default: json)
  -r key=value     # Override resolver/input values
```

**Key implementation details:**
- Registry adapter pattern for interface compatibility between action and resolver systems
- `actionRegistryAdapter` wraps registry for action system (converts `(Provider, error)` → `(Provider, bool)`)
- `registryAdapter` wraps for resolver executor (passes through `(Provider, error)`)
- Resolver execution runs first to populate data context
- If workflow defined and `--skip-actions` not set, action graph is built and executed
- Dry-run mode shows execution plan without side effects
- Progress callback integration for real-time status updates (with `--progress`)
- JSON and YAML output format support for action results

**Types implemented:**

```go
// pkg/cmd/scafctl/run/solution.go

type SolutionOptions struct {
    // ... existing resolver options
    
    // Action execution options
    ActionTimeout        time.Duration
    MaxActionConcurrency int
    DryRun               bool
    SkipActions          bool
}

// Adapters for registry interface compatibility
type registryAdapter struct { ... }      // For resolver system
type actionRegistryAdapter struct { ... } // For action system

// ActionProgressCallback for CLI progress output
type ActionProgressCallback struct { ... }
```

---

### Phase 12: Integration Testing ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** Comprehensive end-to-end tests

**Files created:**
- [x] `pkg/action/integration_test.go` - Integration tests
- [x] `examples/actions/README.md` - Examples documentation
- [x] `examples/actions/hello-world.yaml` - Basic action example
- [x] `examples/actions/sequential-chain.yaml` - Linear dependencies
- [x] `examples/actions/parallel-with-deps.yaml` - Diamond pattern
- [x] `examples/actions/foreach-deploy.yaml` - ForEach expansion
- [x] `examples/actions/error-handling.yaml` - OnError continue
- [x] `examples/actions/retry-backoff.yaml` - Retry strategies
- [x] `examples/actions/conditional-execution.yaml` - When conditions
- [x] `examples/actions/finally-cleanup.yaml` - Finally section
- [x] `examples/actions/complex-workflow.yaml` - Full CI/CD example

**Test scenarios covered:**
1. ✅ Simple linear action chain
2. ✅ Parallel actions with dependencies (diamond pattern)
3. ✅ ForEach expansion and execution
4. ✅ Error handling (fail vs continue)
5. ✅ Retry with backoff (exponential, linear, fixed)
6. ✅ Timeout handling
7. ✅ Condition evaluation (resolver-only)
8. ✅ Finally section execution (normal and after failure)
9. ✅ Cancellation behavior
10. ✅ Progress callback events
11. ✅ Rendered graph correctness
12. ✅ Deferred inputs with __actions reference
13. ✅ Complex multi-phase workflow
14. ✅ Max concurrency enforcement
15. ✅ Validation error scenarios

**Key implementation details:**
- Mock registry and provider infrastructure for isolated testing
- Progress callback recorder for event verification
- Thread-safe execution order tracking
- Comprehensive test coverage for all major features

---

### Phase 13: Documentation ✅ COMPLETED

**Status:** Completed on 2026-01-29

**Goal:** User-facing documentation

**Files created:**
- [x] `docs/actions-tutorial.md` - Getting started guide with comprehensive documentation
- [x] `examples/actions/` - Example configurations (9 examples)
- [x] Updated `README.md` with actions overview

**Documentation coverage:**
- Quick start guide
- Core concepts (providers, inputs, dependencies)
- Action results and __actions namespace
- Conditions (when)
- ForEach expansion
- Error handling (onError)
- Retry with backoff strategies
- Timeouts
- Finally section
- Running workflows (execute and render modes)
- Best practices
- Troubleshooting guide

---

## Dependency Order

```
Phase 0 (Extract Common Types)
    ↓
Phase 1 (Action Types) ────────────────┐
    ↓                                   │
Phase 2 (Deferred Expressions)          │
    ↓                                   │
Phase 3 (Context)                       │
    ↓                                   │
Phase 4 (Validation) ───────────────────┤
    ↓                                   │
Phase 5 (Graph) ────────────────────────┤
    ↓                                   │
Phase 10 (Provider) ────────────────────┘
    ↓
Phase 6 (Renderer) ← Phase 7 (Executor)
                          ↓
                    Phase 8 (Retry)
                          ↓
                    Phase 9 (ForEach)
                          ↓
                    Phase 11 (CLI)
                          ↓
                    Phase 12 (Testing)
                          ↓
                    Phase 13 (Docs)
```

---

## Implementation Notes

### Shared Package Structure

The `pkg/spec/` package provides common types used by both resolvers and actions:

```
pkg/spec/
├── valueref.go          # ValueRef type with Resolve() methods
├── valueref_test.go
├── condition.go         # Condition type for CEL conditionals
├── condition_test.go
├── foreach.go           # ForEachClause type
├── foreach_test.go
├── errors.go            # OnErrorBehavior type
├── types.go             # Type coercion utilities (CoerceType, etc.)
└── types_test.go
```

This enables both resolver and action packages to share:
- YAML/JSON unmarshalling logic
- Expression/template resolution
- Type coercion
- Validation helpers

### Expression Evaluation Strategy

When materializing inputs:

1. Parse expression/template to detect variable references
2. If references only `_` (resolver data): evaluate immediately → concrete value
3. If references `__actions`: preserve as `DeferredValue` for runtime
4. If mixed: preserve entire expression as `DeferredValue`

```go
// Using the shared spec.ValueRef
func (v *spec.ValueRef) ReferencesVariable(varName string) bool {
    if v.Expr != nil {
        vars := v.Expr.GetUnderscoreVariables()
        return slices.Contains(vars, varName)
    }
    if v.Tmpl != nil {
        refs := v.Tmpl.GetReferences()
        return slices.Contains(refs, varName)
    }
    return false
}

// Action-specific materialization
func Materialize(v *spec.ValueRef, resolverData map[string]any) (any, error) {
    if v.ReferencesVariable("__actions") {
        return &DeferredValue{Expr: ..., Deferred: true}, nil
    }
    return v.Resolve(ctx, resolverData, nil)
}
```

### Cancellation Handling

```go
func (e *Executor) handleCancellation(ctx context.Context) {
    // 1. Signal running actions via context
    // 2. Start grace period timer
    // 3. Wait for running actions or grace period
    // 4. Force terminate if needed
    // 5. Mark pending as cancelled
    // 6. Always execute finally section
}
```

### ForEach Result Aggregation

```go
// During forEach execution:
for i, item := range items {
    result := execute(item, i)
    actionCtx.AddIteration(actionName, &ForEachIterationResult{
        Index:   i,
        Name:    fmt.Sprintf("%s[%d]", actionName, i),
        Results: result.Data,
        Status:  StatusSucceeded,
    })
}

// Aggregate results available via:
// __actions.deploy.results      → [result0, result1, ...]
// __actions.deploy.iterations   → [{index, name, results, status}, ...]
// __actions["deploy[0]"].results → result0
```

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Complex deferred expression handling | High | Start with simple cases, expand incrementally |
| ForEach expansion complexity | Medium | Reuse patterns from resolver forEach |
| Provider capability audit | Low | Most providers will naturally support actions |
| Cancellation edge cases | Medium | Extensive testing, conservative defaults |
| Performance with large graphs | Low | Lazy evaluation, efficient concurrency |

---

## Success Criteria

1. ✅ Actions execute in correct dependency order
2. ✅ ForEach expands correctly with proper naming
3. ✅ Deferred expressions preserved in rendered output
4. ✅ Finally section always executes
5. ✅ Retry/timeout/error handling works correctly
6. ✅ Progress callbacks fire at appropriate times
7. ✅ Cancellation gracefully handles in-flight actions
8. ✅ All validation rules enforced
9. ✅ Rendered graph consumable by external executors
10. ✅ Comprehensive test coverage (>80%)

---

## Estimated Total Effort

| Phase | Effort |
|-------|--------|
| Phase 0: Extract Common Types | 2-3 days |
| Phase 1: Action Types | 2-3 days |
| Phase 2: Deferred Expressions | 2 days |
| Phase 3: Context | 1-2 days |
| Phase 4: Validation | 2-3 days |
| Phase 5: Graph | 3-4 days |
| Phase 6: Renderer | 2-3 days |
| Phase 7: Executor | 5-7 days |
| Phase 8: Retry | 1-2 days |
| Phase 9: ForEach | 3-4 days |
| Phase 10: Provider | 2-3 days |
| Phase 11: CLI | 3-4 days |
| Phase 12: Testing | 3-4 days |
| Phase 13: Documentation | 2 days |
| **Total** | **34-47 days** |

---

## Future Enhancements (Deferred)

These features are documented in the design but deferred from initial implementation:

- Result Schema Validation
- Conditional Retry (`retryIf` expression)
- Matrix Strategy (multi-dimensional expansion)
- Action Alias
- Exclusive Actions (mutual exclusion)
- Action Concurrency Limit (global CLI flag)

See [design/actions.md](design/actions.md#future-enhancements) for details.
