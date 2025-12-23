# scafctl Implementation Plan

> **Comprehensive phased implementation plan for the complete scafctl system based on design documentation**

## Overview

This document outlines a detailed, phased approach to implementing all features documented in `docs/design/` and `docs/guides/`. The implementation is structured to build foundational components first, then layer on more complex features.

**Estimated Total Timeline**: 12-16 weeks (3-4 months)

**Current Status**: 
- ✅ Core CLI structure (cobra, logging, profiling)
- ✅ Solution package with metadata and validation structure
- ✅ DAG execution engine
- ✅ CEL expression evaluation with custom extensions
- ⚠️ Provider system: Not started
- ⚠️ Resolver pipeline: Not started
- ⚠️ Action orchestration: Not started
- ⚠️ Plugin system: Not started
- ⚠️ Catalog system: Not started

---

## Phase 1: Provider Foundation (Week 1-2)

**Goal**: Build the core provider interface and registry system that all other components depend on.

### 1.1: Provider Interface & Types

**Files to create**:
- `pkg/provider/types.go` - Core types and interfaces
- `pkg/provider/descriptor.go` - Provider metadata
- `pkg/provider/context.go` - Execution context
- `pkg/provider/provider_test.go` - Interface compliance tests

**Key Components**:

```go
// Core provider interface
type Provider interface {
    Descriptor() Descriptor
    Execute(ctx context.Context, input any, dataCtx map[string]any) (any, error)
    Validate(input map[string]any) error
}

// Provider descriptor with schema
type Descriptor struct {
    Name         string
    Namespace    string
    Version      string
    Kind         string
    Description  string
    Schema       SchemaDefinition
    Capabilities Capabilities
}

// SchemaDefinition defines the input schema for a provider
// Maps parameter names to their validation rules and metadata
type SchemaDefinition struct {
    Parameters map[string]ParameterDefinition
}

// ParameterDefinition describes a single input parameter
type ParameterDefinition struct {
    Type        string // "string", "array", "object", "boolean", "number", "integer"
    Description string // Human-readable description for documentation
    Required    bool   // Whether this parameter is mandatory
    Default     any    // Optional default value if parameter is omitted
}

// Example for Shell provider:
// Schema: SchemaDefinition{
//     Parameters: map[string]ParameterDefinition{
//         "cmd": {
//             Type:        "array",
//             Description: "Command and arguments to execute",
//             Required:    true,
//         },
//         "dir": {
//             Type:        "string",
//             Description: "Working directory for command execution",
//             Required:    false,
//         },
//         "env": {
//             Type:        "array",
//             Description: "Environment variables in KEY=VALUE format",
//             Required:    false,
//         },
//     },
// }

// Data context with resolver values and action outputs
type DataContext map[string]any

// Capabilities flags indicating which scafctl phases can use this provider
type Capabilities struct {
    Resolvers bool  // Can be used in resolver resolve phase (from: sources)
    Actions   bool  // Can be used in action execution
    Auth      bool  // Can handle authentication workflows (scafctl auth commands)
}

// Examples:
// - CLI, Env, Git, Static, Expression providers: Resolvers=true, Actions=false, Auth=false
// - Shell, API, File providers: Resolvers=true, Actions=true, Auth=false
// - Entra, OAuth, OIDC providers: Resolvers=false, Actions=false, Auth=true
```

**Tests**:
- Interface compliance checks
- Descriptor validation
- ExecutionContext creation and access
- Schema definition structures

**Deliverables**:
- ✅ Core provider interface defined
- ✅ Type system for inputs/outputs
- ✅ Execution context structure
- ✅ 15+ tests

---

### 1.2: Provider Registry

**Files to create**:
- `pkg/provider/registry.go` - Provider registration and lookup
- `pkg/provider/registry_test.go` - Registry tests

**Key Components**:

```go
// Global registry for providers
type Registry struct {
    providers map[string]Provider
    mu        sync.RWMutex
}

// Register a provider
func (r *Registry) Register(p Provider) error

// Get a provider by reference
func (r *Registry) Get(ref string) (Provider, error)

// List all providers
func (r *Registry) List() []Descriptor

// Parse provider reference: namespace/name or namespace/name@version
func ParseProviderRef(ref string) (ProviderRef, error)
```

**Tests**:
- Register and retrieve providers
- Duplicate registration handling
- Provider reference parsing (namespace/name, namespace/name@version)
- Thread-safety tests
- List and filter operations

**Deliverables**:
- ✅ Thread-safe provider registry
- ✅ Provider reference resolution
- ✅ Version constraint support (using semver)
- ✅ 20+ tests

---

### 1.3: Built-in Providers (Core Set)

**Files to create**:
- `pkg/provider/builtin/static/static.go` - Static value provider
- `pkg/provider/builtin/expression/expression.go` - CEL expression provider
- `pkg/provider/builtin/cli/cli.go` - CLI argument provider
- `pkg/provider/builtin/env/env.go` - Environment variable provider
- `pkg/provider/builtin/git/git.go` - Git metadata provider
- `pkg/provider/builtin/template/template.go` - Go template provider
- Each with corresponding `*_test.go` files

**Static Provider**:
```go
// Returns a static value
type StaticProvider struct{}

type StaticInputs struct {
    Value any `json:"value"`
}
```

**Expression Provider**:
```go
// Evaluates CEL expressions
type ExpressionProvider struct {
    celEnv *cel.Env
}

type ExpressionInputs struct {
    Expr string `json:"expr"`
}
```

**CLI Provider**:
```go
// Gets values from CLI flags/args
type CLIProvider struct {
    cliArgs map[string]any
}

type CLIInputs struct {
    Key string `json:"key"`
}
```

**Template Provider**:
```go
// Renders Go templates with data context
type TemplateProvider struct{}

type TemplateInputs struct {
    Template string `json:"template"` // Go template string
    Data     any    `json:"data"`     // Data to pass to template
}
```

**Tests for each provider**:
- Input validation
- Execution with valid inputs
- Error handling
- Integration with CEL environment

**Example: Using source order for fallback pattern**:
```yaml
spec:
  resolvers:
    environment:
      description: Environment name with fallback to development
      resolve:
        from:
          - provider: cli
            inputs:
              key: env              # 1st: Try CLI flag
          - provider: env
            inputs:
              key: ENVIRONMENT      # 2nd: Try environment variable
          - provider: static
            inputs:
              value: development    # 3rd: Use static default
```

This pattern eliminates the need for a `default` property in CLIInputs. The resolver will try each source in the order listed, and the first non-null value wins. If the user doesn't provide `-r env=...` and the `ENVIRONMENT` variable isn't set, it falls back to the static value `"development"`.

**Deliverables**:
- ✅ 6 built-in providers implemented
- ✅ Each provider has 10-15 tests
- ✅ Schema validation working
- ✅ CEL integration functional

---

## Phase 2: Resolver Pipeline (Week 3-4)

**Goal**: Implement the four-phase resolver pipeline (resolve → transform → validate → emit) with provider integration.

### 2.1: Resolver Types & Schema

**Files to create**:
- `pkg/resolver/types.go` - Core resolver structures
- `pkg/resolver/schema.go` - Schema definitions for YAML parsing
- `pkg/resolver/resolver.go` - Resolver execution logic
- `pkg/resolver/resolver_test.go` - Basic resolver tests

**Key Components**:

```go
// Resolver definition from solution YAML
type Resolver struct {
    Name        string
    Description string
    Resolve     ResolvePhase
    Transform   *TransformPhase
    Validate    []ValidationRule
    Type        string // optional type hint
}

// Resolve phase with source priority
type ResolvePhase struct {
    From  []ResolveSource
    Until celexp.Expression // optional early-exit condition (stop trying sources when true)
}

// ResolveSource defines a provider to invoke with its inputs
// Inputs support Go templating via GoTemplatingContent, allowing dynamic values
// using {{ _.resolverName }} syntax to reference other resolved values
type ResolveSource struct {
    Provider string                                `json:"provider"`
    Inputs   map[string]gotmpl.GoTemplatingContent `json:"inputs,omitempty"`
    When     celexp.Expression                     `json:"when,omitempty"` // optional condition to skip this source
}

// Example:
// resolve:
//   from:
//     - provider: api
//       inputs:
//         endpoint: "https://api.example.com/{{ _.environment }}/config"
//         method: GET
//         headers:
//           Authorization: "Bearer {{ _.apiToken }}"
//     - provider: static
//       inputs:
//         value: { "default": "config" }

// Transform phase with sequential steps
type TransformPhase struct {
    Into  []TransformStep
    Until celexp.Expression // optional early-exit condition
    When  celexp.Expression // optional gate condition
}

type TransformStep struct {
    Expr celexp.Expression
    When celexp.Expression // optional item-level condition
}

// Validation rule
type ValidationRule struct {
    Expr    celexp.Expression
    Regex   string
    Message gotmpl.GoTemplatingContent // Error message template with Go template support
    When    celexp.Expression          // optional condition to gate this validation rule
}

// Resolver result with execution metadata
type Result struct {
    Name     string         // Resolver name
    Value    any            // Resolved value
    Error    error          // Error if resolution failed
    Duration time.Duration  // Time taken to execute all phases
    Phase    int            // Execution phase (1 = no dependencies, N = depends on phase N-1)
}

// Note: Resolvers don't have a Status field (no conditional skipping like actions)
// Success/failure is determined by Error field being nil or non-nil
```

**Master Execution Function**:

```go
// Execute runs all resolver phases (resolve → transform → validate) and returns Result with metadata
func (r *Resolver) Execute(ctx context.Context, dataCtx map[string]any) (*Result, error) {
    result := &Result{
        Name: r.Name,
    }
    
    start := time.Now()
    defer func() {
        result.Duration = time.Since(start)
    }()
    
    // 1. Resolve phase - get value from sources
    value, err := r.resolve(ctx, dataCtx)
    if err != nil {
        result.Error = err
        return result, err
    }
    
    // 2. Transform phase - apply transformations
    if r.Transform != nil {
        value, err = r.transform(ctx, value, dataCtx)
        if err != nil {
            result.Error = err
            return result, err
        }
    }
    
    // 3. Validate phase - check constraints
    if len(r.Validate) > 0 {
        err = r.validate(ctx, value, dataCtx)
        if err != nil {
            result.Error = err
            return result, err
        }
    }
    
    result.Value = value
    return result, nil
}
```

**Tests**:
- Resolver struct creation
- YAML unmarshaling
- Validation of resolver definitions
- Type conversions

**Deliverables**:
- ✅ Resolver type system
- ✅ YAML schema support
- ✅ 20+ tests

---

### 2.2: Resolve Phase Implementation

**Files to create**:
- `pkg/resolver/resolve.go` - Source resolution logic
- `pkg/resolver/resolve_test.go` - Resolve phase tests

**Key Features**:
- Sources tried in order they appear in `From` array (no implicit priority by type)
- First non-null wins (default behavior when no `Until` specified)
- `Until` condition determines what "wins" when specified
- Provider invocation for each source
- Error aggregation

**Implementation**:

```go
// Execute resolve phase
func (r *Resolver) resolve(ctx context.Context, dataCtx map[string]any) (any, error) {
    for _, source := range r.Resolve.From {
        // Check when condition for this source (skip if false)
        if source.When != "" {
            shouldRun, err := evaluateCondition(source.When, nil, dataCtx)
            if err != nil {
                return nil, fmt.Errorf("evaluating when condition for source %s: %w", source.Provider, err)
            }
            if !shouldRun {
                continue // Skip this source
            }
        }
        
        provider, err := registry.Get(source.Provider)
        if err != nil {
            continue // Skip unavailable providers
        }
        
        // Interpolate inputs with Go templates (access dataCtx via {{ _.key }})
        interpolatedInputs, err := interpolateInputs(source.Inputs, dataCtx)
        if err != nil {
            continue // Skip sources with template errors
        }
        
        // Execute provider with interpolated inputs and data context
        value, err := provider.Execute(ctx, interpolatedInputs, dataCtx)
        if err != nil {
            continue // Skip failed sources
        }
        
        if value != nil {
            // Check until condition (stop trying sources when true)
            if r.Resolve.Until != "" {
                done, err := evaluateCondition(r.Resolve.Until, value, dataCtx)
                if err != nil {
                    return nil, fmt.Errorf("evaluating until condition: %w", err)
                }
                if done {
                    return value, nil // Early exit - condition met
                }
            } else {
                return value, nil // First non-null wins (default behavior)
            }
        }
    }
    return nil, errors.New("no source provided a value")
}
```

**Tests**:
- Array order enforcement (sources tried in `From` array order)
- First non-null selection (default behavior)
- Until condition determines winner (e.g., `until: __self != ""`)
- Until with custom logic (e.g., `until: __self.startsWith("https://")`)
- Provider error handling
- All sources fail scenario
- Conditional source execution with `when:` (skip sources based on dataCtx)
- When with environment-specific sources: `when: _.environment == 'production'`
- Multiple resolver dependencies

**Deliverables**:
- ✅ Source resolution with configurable win conditions
- ✅ Provider integration
- ✅ 25+ tests

---

### 2.3: Transform Phase Implementation

**Files to create**:
- `pkg/resolver/transform.go` - Transform execution
- `pkg/resolver/transform_test.go` - Transform tests

**Key Features**:
- Sequential step execution
- `__self` context for current value
- `_` context for all resolved values
- `until:` early exit
- `when:` conditional gates (transform-level and item-level)

**Implementation**:

```go
// Execute transform phase
func (r *Resolver) transform(ctx context.Context, value any, dataCtx map[string]any) (any, error) {
    if r.Transform == nil {
        return value, nil
    }
    
    // Check transform-level when condition
    if r.Transform.When != "" {
        shouldRun, err := evaluateCondition(r.Transform.When, value, dataCtx)
        if err != nil || !shouldRun {
            return value, err
        }
    }
    
    current := value
    for _, step := range r.Transform.Into {
        // Check item-level when condition
        if step.When != "" {
            shouldRun, err := evaluateCondition(step.When, current, dataCtx)
            if err != nil {
                return nil, err
            }
            if !shouldRun {
                continue // Skip this step
            }
        }
        
        // Execute transform step (CEL has access to __self and _ via dataCtx)
        result, err := evaluateCEL(step.Expr, current, dataCtx)
        if err != nil {
            return nil, err
        }
        current = result
        
        // Check until condition
        if r.Transform.Until != "" {
            done, err := evaluateCondition(r.Transform.Until, current, dataCtx)
            if err != nil {
                return nil, err
            }
            if done {
                break // Early exit
            }
        }
    }
    
    return current, nil
}
```

**Tests**:
- Sequential transform steps
- `__self` and `_` context access
- `until:` early exit
- `when:` conditional execution (both levels)
- Type transformations
- Error handling

**Deliverables**:
- ✅ Complete transform phase
- ✅ CEL integration
- ✅ 30+ tests

---

### 2.4: Validate Phase Implementation

**Files to create**:
- `pkg/resolver/validate.go` - Validation logic
- `pkg/resolver/validate_test.go` - Validation tests

**Key Features**:
- Multiple validation rules (all must pass)
- CEL expressions for validation
- Regex shorthand support
- Clear error messages

**Implementation**:

```go
// Execute validate phase
func (r *Resolver) validate(ctx context.Context, value any, dataCtx map[string]any) error {
    var errors []error
    
    for _, rule := range r.Validate {
        // Check when condition for this validation rule
        if rule.When != "" {
            shouldRun, err := evaluateCondition(rule.When, value, dataCtx)
            if err != nil {
                errors = append(errors, fmt.Errorf("validation when condition error: %w", err))
                continue
            }
            if !shouldRun {
                continue // Skip this validation rule
            }
        }
        
        var valid bool
        var err error
        
        if rule.Regex != "" {
            // Regex validation
            valid, err = matchesRegex(value, rule.Regex)
        } else if rule.Expr != "" {
            // CEL expression validation (has access to __self and _ via dataCtx)
            valid, err = evaluateCondition(rule.Expr, value, dataCtx)
        }
        
        if err != nil {
            errors = append(errors, err)
            continue
        }
        
        if !valid {
            // Interpolate error message template with current context
            msg, err := interpolateMessage(rule.Message, value, dataCtx)
            if err != nil {
                errors = append(errors, fmt.Errorf("validation message template error: %w", err))
            } else {
                errors = append(errors, fmt.Errorf("validation failed: %s", msg))
            }
        }
    }
    
    if len(errors) > 0 {
        return fmt.Errorf("validation errors: %w", errors)
    }
    
    return nil
}
```

**Tests**:
- CEL expression validation
- Regex validation
- Multiple rules (all pass, some fail, all fail)
- Conditional validation with `when:` (rule-level)
- Message templates with Go templating (access to `__self` and `_`)
- Error message formatting
- Complex validation scenarios

**Deliverables**:
- ✅ Validation rule execution
- ✅ Error aggregation
- ✅ 20+ tests

---

### 2.5: Resolver Engine & DAG

**Files to create**:
- `pkg/resolver/engine.go` - Resolver orchestration
- `pkg/resolver/dag.go` - Dependency graph for resolvers
- `pkg/resolver/engine_test.go` - Integration tests

**Key Features**:
- **Phase-based execution from DAG** (resolvers execute in phases based on dependency depth)
- **Concurrent execution within each phase** (resolvers without dependencies execute in parallel)
- **Execution metadata tracking** (Duration and Phase for each resolver)
- Circular dependency detection
- Result caching with structured metadata
- Returns both execution metadata and resolved values
- Integration with existing `pkg/dag` for phase computation

**Implementation**:

```go
import (
    "context"
    "fmt"
    "sort"
    "sync"
    
    "golang.org/x/sync/errgroup"
)

// Resolver engine
type Engine struct {
    resolvers map[string]*Resolver
    registry  *provider.Registry
    dag       *dag.DAG
}

// ExecuteResult contains both metadata and values from resolver execution
type ExecuteResult struct {
    Results map[string]*Result // Execution metadata (includes Duration, Phase, Error)
    Values  map[string]any     // Resolved values for data context
}

// Execute all resolvers with concurrent execution within each phase
// Returns both execution metadata and resolved values
func (e *Engine) Execute(ctx context.Context) (*ExecuteResult, error) {
    // Build dependency graph
    if err := e.buildDAG(); err != nil {
        return nil, err
    }
    
    // Compute execution phases from DAG
    // Note: ComputePhases needs to be implemented in pkg/dag
    // It should assign a phase number to each node based on dependency depth
    runnerResults := e.dag.ComputePhases()
    phaseMap := runnerResults.GetPhaseMap()
    
    // Get sorted phase numbers for deterministic execution
    phases := make([]int, 0, len(phaseMap))
    for phase := range phaseMap {
        phases = append(phases, phase)
    }
    sort.Ints(phases)
    
    // Track both results (metadata) and values (for data context)
    results := make(map[string]*Result)
    values := make(map[string]any)
    
    // Execute each phase sequentially, but resolvers within a phase concurrently
    for _, phase := range phases {
        resolversInPhase := phaseMap[phase]
        
        // Phase-local storage for this phase's results
        // Resolvers in phase N should NOT see each other's results
        phaseResults := make(map[string]*Result)
        phaseResultsMu := sync.Mutex{}
        
        // Use errgroup for concurrent execution within phase
        g, gctx := errgroup.WithContext(ctx)
        
        for _, exec := range resolversInPhase {
            resolverName := exec.Name
            resolver := e.resolvers[resolverName]
            currentPhase := phase // Capture phase for this iteration
            
            g.Go(func() error {
                // Clone data context from PREVIOUS phases only (using values, not Results)
                // This ensures resolvers in phase N cannot see each other's results
                dataCtx := maps.Clone(values)
                
                // Execute resolver with data context (returns Result with metadata)
                result, err := resolver.Execute(gctx, dataCtx)
                if err != nil {
                    return fmt.Errorf("resolver %s failed: %w", resolverName, err)
                }
                
                // Set phase number for tracking
                result.Phase = currentPhase
                
                // Store result in phase-local map
                phaseResultsMu.Lock()
                phaseResults[resolverName] = result
                phaseResultsMu.Unlock()
                
                return nil
            })
        }
        
        // Wait for all resolvers in this phase to complete before moving to next phase
        if err := g.Wait(); err != nil {
            return nil, err
        }
        
        // Merge phase results into global results and values AFTER phase completes
        // This makes phase N results available to phase N+1
        for name, result := range phaseResults {
            results[name] = result
            values[name] = result.Value // Extract value for data context
        }
    }
    
    return &ExecuteResult{
        Results: results,
        Values:  values,
    }, nil
}
```

**DAG Package Enhancement Required**:

Add `ComputePhases` method to `pkg/dag` package:

```go
// ComputePhases assigns a phase number to each node in the graph based on dependency depth.
// Phase 1 contains nodes with no dependencies (roots).
// Phase N contains nodes whose dependencies are all in phases < N.
// Returns RunnerResults with ExecutionOrder populated with phase assignments.
func (g *Graph) ComputePhases() *RunnerResults {
    results := &RunnerResults{
        ExecutionOrder: make([]ObjectExecution, 0, len(g.Nodes)),
    }
    
    // Track assigned phases
    phases := make(map[string]int)
    
    // Helper to compute phase for a node
    var computePhase func(node *Node) int
    computePhase = func(node *Node) int {
        if phase, exists := phases[node.Key]; exists {
            return phase
        }
        
        if len(node.Prev) == 0 {
            phases[node.Key] = 1
            return 1
        }
        
        maxPrevPhase := 0
        for _, prev := range node.Prev {
            prevPhase := computePhase(prev)
            if prevPhase > maxPrevPhase {
                maxPrevPhase = prevPhase
            }
        }
        
        phase := maxPrevPhase + 1
        phases[node.Key] = phase
        return phase
    }
    
    // Compute phases for all nodes
    position := 0
    for name, node := range g.Nodes {
        phase := computePhase(node)
        results.ExecutionOrder = append(results.ExecutionOrder, ObjectExecution{
            Name:     name,
            Phase:    phase,
            Position: position,
        })
        position++
    }
    
    return results
}
```

**Tests**:
- Simple resolver execution (single resolver, no dependencies)
- Dependent resolvers (linear chain: A → B → C)
- **Concurrent execution within phase** (multiple resolvers with same dependencies execute in parallel)
- **Phase-based execution** (verify phase 1 completes before phase 2 starts)
- **Phase isolation** (verify resolvers in phase N do NOT see each other's results, only previous phases)
- **Data context correctness** (concurrent resolvers get consistent view of previous phase results)
- **Result metadata tracking** (verify Result struct includes Name, Value, Error, Duration, Phase)
- **Duration tracking** (verify Duration field captures execution time for all phases)
- **Phase field correctness** (verify Phase 1 resolvers have Phase=1, Phase 2 resolvers have Phase=2, etc.)
- Circular dependency detection
- Complex dependency graphs (diamond patterns, fan-out/fan-in)
- Error propagation (error in phase halts execution)
- **Concurrent error handling** (error in one resolver stops all resolvers in same phase)
- **ExecuteResult structure** (verify both Results map and Values map are populated correctly)

**Deliverables**:
- ✅ Complete resolver engine with concurrent execution
- ✅ DAG integration with phase computation
- ✅ ComputePhases method in pkg/dag
- ✅ Phase-local result collection for proper isolation
- ✅ 40+ integration tests including concurrency scenarios

---

### 2.6: Minimal Resolver Execution (Dependency Analysis)

**Files to create**:
- `pkg/resolver/analyzer.go` - Dependency analyzer for resolvers, actions, templates
- `pkg/resolver/analyzer_test.go` - Analyzer tests

**Goal**: Execute only the minimal set of resolvers required for a given execution context (specific action, template, or subset of actions).

**Key Features**:
- Static dependency analysis at solution load time
- Build dependency graph from:
  - Template/action input references (`{{ _.resolverName }}`)
  - Resolver source inputs with Go templates (`{{ _.otherResolver }}`)
  - Resolver transform expressions (CEL: `_.someResolver + ".suffix"`)
  - Resolver validation expressions (CEL: `_.maxLength`)
  - Action `dependsOn` property (action dependencies)
  - Conditional references in `when:` clauses (even if condition might skip execution)
- Transitive dependency resolution (if A needs B and B needs C, execute all three)
- CLI-driven execution targeting

**Implementation**:

```go
// Dependency analyzer
type DependencyAnalyzer struct {
    resolvers map[string]*Resolver
    actions   map[string]*Action
}

// AnalyzeResolverDependencies builds a map of resolver -> required resolvers
func (a *DependencyAnalyzer) AnalyzeResolverDependencies() (map[string][]string, error) {
    deps := make(map[string][]string)
    
    for name, resolver := range a.resolvers {
        resolverDeps := make(map[string]bool)
        
        // 1. Analyze resolve phase source inputs (Go templates)
        for _, source := range resolver.Resolve.From {
            for _, input := range source.Inputs {
                refs := extractTemplateReferences(input) // {{ _.resolverName }}
                for _, ref := range refs {
                    resolverDeps[ref] = true
                }
            }
            // Also check when condition
            if source.When != "" {
                refs := extractCELReferences(source.When) // _.resolverName
                for _, ref := range refs {
                    resolverDeps[ref] = true
                }
            }
        }
        
        // 2. Analyze until condition
        if resolver.Resolve.Until != "" {
            refs := extractCELReferences(resolver.Resolve.Until)
            for _, ref := range refs {
                resolverDeps[ref] = true
            }
        }
        
        // 3. Analyze transform phase
        if resolver.Transform != nil {
            if resolver.Transform.When != "" {
                refs := extractCELReferences(resolver.Transform.When)
                for _, ref := range refs {
                    resolverDeps[ref] = true
                }
            }
            if resolver.Transform.Until != "" {
                refs := extractCELReferences(resolver.Transform.Until)
                for _, ref := range refs {
                    resolverDeps[ref] = true
                }
            }
            for _, step := range resolver.Transform.Into {
                refs := extractCELReferences(step.Expr)
                for _, ref := range refs {
                    resolverDeps[ref] = true
                }
                if step.When != "" {
                    refs := extractCELReferences(step.When)
                    for _, ref := range refs {
                        resolverDeps[ref] = true
                    }
                }
            }
        }
        
        // 4. Analyze validation phase
        for _, rule := range resolver.Validate {
            if rule.Expr != "" {
                refs := extractCELReferences(rule.Expr)
                for _, ref := range refs {
                    resolverDeps[ref] = true
                }
            }
            if rule.When != "" {
                refs := extractCELReferences(rule.When)
                for _, ref := range refs {
                    resolverDeps[ref] = true
                }
            }
            // Check message template
            refs := extractTemplateReferences(rule.Message)
            for _, ref := range refs {
                resolverDeps[ref] = true
            }
        }
        
        // Convert to slice
        depSlice := make([]string, 0, len(resolverDeps))
        for dep := range resolverDeps {
            depSlice = append(depSlice, dep)
        }
        deps[name] = depSlice
    }
    
    return deps, nil
}

// AnalyzeActionDependencies builds a map of action -> required resolvers
func (a *DependencyAnalyzer) AnalyzeActionDependencies() (map[string][]string, error) {
    deps := make(map[string][]string)
    
    for name, action := range a.actions {
        resolverDeps := make(map[string]bool)
        
        // 1. Analyze action inputs (Go templates and direct values)
        for _, input := range action.Inputs {
            refs := extractAllReferences(input) // Handles nested maps/arrays
            for _, ref := range refs {
                resolverDeps[ref] = true
            }
        }
        
        // 2. Analyze when condition (CEL)
        if action.When != "" {
            refs := extractCELReferences(action.When)
            for _, ref := range refs {
                resolverDeps[ref] = true
            }
        }
        
        // 3. Analyze foreach.over (CEL)
        if action.Foreach != nil && action.Foreach.Over != "" {
            refs := extractCELReferences(action.Foreach.Over)
            for _, ref := range refs {
                resolverDeps[ref] = true
            }
        }
        
        // 4. Add dependsOn actions' resolver dependencies (transitive)
        // Note: This is handled separately in ComputeMinimalResolvers
        
        // Convert to slice
        depSlice := make([]string, 0, len(resolverDeps))
        for dep := range resolverDeps {
            depSlice = append(depSlice, dep)
        }
        deps[name] = depSlice
    }
    
    return deps, nil
}

// ComputeMinimalResolvers returns the minimal set of resolvers needed for given actions
func (a *DependencyAnalyzer) ComputeMinimalResolvers(targetActions []string) ([]string, error) {
    // Build dependency graphs
    resolverDeps, err := a.AnalyzeResolverDependencies()
    if err != nil {
        return nil, err
    }
    
    actionDeps, err := a.AnalyzeActionDependencies()
    if err != nil {
        return nil, err
    }
    
    // Start with resolvers directly needed by target actions
    needed := make(map[string]bool)
    
    // Handle action dependsOn relationships first
    allActions := a.expandActionDependencies(targetActions)
    
    for _, actionName := range allActions {
        for _, resolverName := range actionDeps[actionName] {
            needed[resolverName] = true
        }
    }
    
    // Transitively add resolver dependencies
    changed := true
    for changed {
        changed = false
        for resolver := range needed {
            for _, dep := range resolverDeps[resolver] {
                if !needed[dep] {
                    needed[dep] = true
                    changed = true
                }
            }
        }
    }
    
    // Convert to sorted slice for deterministic execution order
    result := make([]string, 0, len(needed))
    for resolver := range needed {
        result = append(result, resolver)
    }
    sort.Strings(result)
    
    return result, nil
}

// expandActionDependencies resolves action dependsOn transitively
func (a *DependencyAnalyzer) expandActionDependencies(targetActions []string) []string {
    expanded := make(map[string]bool)
    queue := make([]string, len(targetActions))
    copy(queue, targetActions)
    
    for len(queue) > 0 {
        actionName := queue[0]
        queue = queue[1:]
        
        if expanded[actionName] {
            continue
        }
        expanded[actionName] = true
        
        // Add dependencies to queue
        if action, exists := a.actions[actionName]; exists {
            for _, dep := range action.DependsOn {
                if !expanded[dep] {
                    queue = append(queue, dep)
                }
            }
        }
    }
    
    result := make([]string, 0, len(expanded))
    for action := range expanded {
        result = append(result, action)
    }
    return result
}

// Helper functions for extracting references
func extractTemplateReferences(content string) []string {
    // Parse Go template {{ _.resolverName }} and extract resolver names
    // Returns list of resolver names referenced
}

func extractCELReferences(expr string) []string {
    // Parse CEL expression and extract _.resolverName references
    // Returns list of resolver names referenced
}

func extractAllReferences(value any) []string {
    // Recursively extract references from nested structures
    // Handles maps, arrays, strings with templates
}
```

**Updated Engine.Execute signature**:

```go
// ExecuteOptions configures resolver execution
type ExecuteOptions struct {
    // TargetActions limits execution to specific actions and their dependencies
    // If empty, all resolvers are executed (current behavior)
    TargetActions []string
    
    // TargetResolvers explicitly specifies which resolvers to execute
    // If set, overrides TargetActions analysis
    TargetResolvers []string
}

// Execute resolvers with optional minimal execution
// Returns ExecuteResult with both metadata and values
func (e *Engine) Execute(ctx context.Context, opts ExecuteOptions) (*ExecuteResult, error) {
    var resolversToExecute []string
    
    if len(opts.TargetResolvers) > 0 {
        // Explicit resolver list provided
        resolversToExecute = opts.TargetResolvers
    } else if len(opts.TargetActions) > 0 {
        // Compute minimal resolvers for target actions
        analyzer := &DependencyAnalyzer{
            resolvers: e.resolvers,
            actions:   e.actions, // Need to pass actions to engine
        }
        var err error
        resolversToExecute, err = analyzer.ComputeMinimalResolvers(opts.TargetActions)
        if err != nil {
            return nil, err
        }
    } else {
        // Execute all resolvers (default)
        resolversToExecute = make([]string, 0, len(e.resolvers))
        for name := range e.resolvers {
            resolversToExecute = append(resolversToExecute, name)
        }
    }
    
    // Build DAG with only the resolvers we need to execute
    filteredResolvers := make(map[string]*Resolver)
    for _, name := range resolversToExecute {
        if resolver, exists := e.resolvers[name]; exists {
            filteredResolvers[name] = resolver
        }
    }
    
    // Build dependency graph for filtered resolvers
    if err := e.buildDAGForResolvers(filteredResolvers); err != nil {
        return nil, err
    }
    
    // Compute execution phases from DAG
    runnerResults := e.dag.ComputePhases(filteredResolvers)
    phaseMap := runnerResults.GetPhaseMap()
    
    // Get sorted phase numbers
    phases := make([]int, 0, len(phaseMap))
    for phase := range phaseMap {
        phases = append(phases, phase)
    }
    sort.Ints(phases)
    
    // Track both results (metadata) and values (for data context)
    results := make(map[string]*Result)
    values := make(map[string]any)
    
    // Execute each phase sequentially, with resolvers within each phase running concurrently
    for _, phase := range phases {
        resolversInPhase := phaseMap[phase]
        
        // Phase-local storage for this phase's results
        // Resolvers in phase N should NOT see each other's results
        phaseResults := make(map[string]*Result)
        phaseResultsMu := sync.Mutex{}
        
        // Use errgroup for concurrent execution within phase
        g, gctx := errgroup.WithContext(ctx)
        
        for _, exec := range resolversInPhase {
            resolverName := exec.Name
            resolver := filteredResolvers[resolverName]
            currentPhase := phase // Capture phase for this iteration
            
            g.Go(func() error {
                // Clone data context from PREVIOUS phases only (using values, not Results)
                // This ensures resolvers in phase N cannot see each other's results
                dataCtx := maps.Clone(values)
                
                // Execute resolver with data context (returns Result with metadata)
                result, err := resolver.Execute(gctx, dataCtx)
                if err != nil {
                    return fmt.Errorf("resolver %s failed: %w", resolverName, err)
                }
                
                // Set phase number for tracking
                result.Phase = currentPhase
                
                // Store result in phase-local map
                phaseResultsMu.Lock()
                phaseResults[resolverName] = result
                phaseResultsMu.Unlock()
                
                return nil
            })
        }
        
        // Wait for all resolvers in this phase to complete
        if err := g.Wait(); err != nil {
            return nil, err
        }
        
        // Merge phase results into global results and values AFTER phase completes
        // This makes phase N results available to phase N+1
        for name, result := range phaseResults {
            results[name] = result
            values[name] = result.Value // Extract value for data context
        }
    }
    
    return &ExecuteResult{
        Results: results,
        Values:  values,
    }, nil
}
```

**Tests**:
- Extract template references from Go templates
- Extract CEL references from expressions
- Analyze resolver dependencies (direct)
- Analyze resolver dependencies (transitive)
- Analyze action dependencies
- Compute minimal resolvers for single action
- Compute minimal resolvers for multiple actions
- Handle action dependsOn chains
- Handle circular dependencies in actions
- Conditional references are included even if condition is false
- Complex nested input structures
- Validate minimal set is sufficient (no missing dependencies)
- Performance with large solution files (1000+ resolvers)

**CLI Integration**:

```bash
# Execute all resolvers and all actions (current default)
scafctl run solution:myapp

# Execute only resolvers needed for specific action
scafctl run solution:myapp --action deploy

# Execute only resolvers needed for multiple actions
scafctl run solution:myapp --action build --action test

# Force all resolvers even when targeting specific action
scafctl run solution:myapp --action deploy --resolve-all
```

**Deliverables**:
- ✅ Dependency analyzer with reference extraction
- ✅ Minimal resolver computation algorithm
- ✅ Updated resolver engine with execution options
- ✅ CLI flag support for targeted execution
- ✅ 50+ tests covering all scenarios

---

## Phase 3: Action Orchestration (Week 5-6)

**Goal**: Implement the action execution system with dependency graphs, conditionals, and foreach iteration.

### 3.1: Action Types & Schema

**Files to create**:
- `pkg/action/types.go` - Action structures
- `pkg/action/schema.go` - YAML schema definitions
- `pkg/action/action.go` - Core action execution
- `pkg/action/action_test.go` - Basic tests

**Key Components**:

```go
// Action definition
type Action struct {
    Name        string
    Description string
    Provider    string
    Inputs      map[string]any
    Outputs     map[string]string // Output mappings
    DependsOn   []string
    When        string   // Conditional expression
    Foreach     *Foreach // Iteration config
}

// Foreach configuration
type Foreach struct {
    Over string // CEL expression for collection
    As   string // Alias name (must start with __)
}

// Action result
type Result struct {
    Name     string
    Status   Status
    Outputs  map[string]any
    Error    error
    Duration time.Duration
    Phase    int // Execution phase (1 = no dependencies, N = depends on phase N-1)
}

type Status string

// Action execution is synchronous, so we only track final states.
// Results are only accessible after execution completes.
// Duration field provides timing information.
// Phase field indicates the dependency depth in the DAG.
const (
    StatusSuccess  Status = "success"
    StatusFailed   Status = "failed"
    StatusSkipped  Status = "skipped"
)
```

**Tests**:
- Action struct creation
- YAML unmarshaling
- Validation of action definitions
- Foreach configuration parsing

**Deliverables**:
- ✅ Action type system
- ✅ YAML schema support
- ✅ 15+ tests

---

### 3.2: Action Execution

**Files to create**:
- `pkg/action/execute.go` - Single action execution
- `pkg/action/context.go` - Action execution context
- `pkg/action/execute_test.go` - Execution tests

**Key Features**:
- Provider invocation with inputs
- Input interpolation with resolver values
- Conditional execution (`when:`)
- Output extraction
- Error handling

**Implementation**:

```go
// Execute a single action
func (a *Action) Execute(ctx context.Context, dataCtx map[string]any) (*Result, error) {
    result := &Result{
        Name: a.Name,
    }
    
    start := time.Now()
    defer func() {
        result.Duration = time.Since(start)
    }()
    
    // Check when condition
    if a.When != "" {
        shouldRun, err := evaluateCondition(a.When, nil, dataCtx)
        if err != nil {
            result.Status = StatusFailed
            result.Error = err
            return result, err
        }
        if !shouldRun {
            result.Status = StatusSkipped
            return result, nil
        }
    }
    
    // Get provider
    provider, err := registry.Get(a.Provider)
    if err != nil {
        result.Status = StatusFailed
        result.Error = err
        return result, err
    }
    
    // Interpolate inputs (templates can reference dataCtx via {{ _.key }})
    inputs, err := interpolateInputs(a.Inputs, dataCtx)
    if err != nil {
        result.Status = StatusFailed
        result.Error = err
        return result, err
    }
    
    // Execute provider with interpolated inputs and data context
    output, err := provider.Execute(ctx, inputs, dataCtx)
    if err != nil {
        result.Status = StatusFailed
        result.Error = err
        return result, err
    }
    
    // Extract outputs
    if len(a.Outputs) > 0 {
        result.Outputs = extractOutputs(output, a.Outputs)
    }
    
    result.Status = StatusSuccess
    return result, nil
}
```

**Tests**:
- Basic action execution
- Input interpolation
- Conditional execution (when true, when false, when error)
- Output extraction
- Provider errors
- Multiple actions

**Deliverables**:
- ✅ Action execution logic
- ✅ Context interpolation
- ✅ 25+ tests

---

### 3.3: Foreach Implementation

**Files to create**:
- `pkg/action/foreach.go` - Iteration logic
- `pkg/action/foreach_test.go` - Foreach tests

**Key Features**:
- Iterate over collections from resolvers
- Custom alias support (`as: __env`)
- Sub-context creation per iteration
- Result aggregation

**Implementation**:

```go
// Execute action with foreach
func (a *Action) executeWithForeach(ctx context.Context, execCtx ExecutionContext) ([]*Result, error) {
    // Evaluate collection expression
```go
// Execute action with foreach
func (a *Action) executeWithForeach(ctx context.Context, dataCtx map[string]any) ([]*Result, error) {
    // Evaluate collection expression
    collection, err := evaluateCEL(a.Foreach.Over, nil, dataCtx)
    if err != nil {
        return nil, err
    }
    
    // Convert to slice
    items, ok := collection.([]any)
    if !ok {
        return nil, errors.New("foreach.over must evaluate to array")
    }
    
    // Determine alias
    alias := a.Foreach.As
    if alias == "" {
        alias = "__item"
    }
    if !strings.HasPrefix(alias, "__") {
        return nil, errors.New("foreach alias must start with __")
    }
    
    // Execute for each item
    var results []*Result
    for i, item := range items {
        // Create sub-context with iteration value
        subCtx := make(map[string]any)
        for k, v := range dataCtx {
            subCtx[k] = v
        }
        subCtx[alias] = item
        subCtx["__index"] = i
        
        // Execute action
        result, err := a.Execute(ctx, subCtx)
        if err != nil {
            return results, err
        }
        results = append(results, result)
    }
    
    return results, nil
}
```

**Tests**:
- Foreach over simple array
- Foreach with custom alias
- Foreach with complex objects
- Foreach with conditional when
- Foreach with dependencies
- Empty collection handling

**Deliverables**:
- ✅ Foreach iteration
- ✅ Alias support
- ✅ 20+ tests

---

### 3.4: Action Engine & DAG

**Files to create**:
- `pkg/action/engine.go` - Action orchestration
- `pkg/action/dag.go` - Action dependency graph
- `pkg/action/engine_test.go` - Integration tests

**Key Features**:
- **Phase-based execution from DAG** (actions execute in phases based on dependency depth)
- **Concurrent execution within each phase** (actions without dependencies execute in parallel)
- **Execution metadata tracking** (Duration and Phase for each action)
- Circular dependency detection
- Stop on first error (entire phase halts)
- Result aggregation with structured metadata
- Integration with `pkg/dag` for phase computation

**Implementation**:

```go
import (
    "context"
    "fmt"
    "sort"
    "sync"
    
    "golang.org/x/sync/errgroup"
)

// Action engine
type Engine struct {
    actions  map[string]*Action
    registry *provider.Registry
    dag      *dag.DAG
}

// Execute all actions with concurrent execution within each phase
// Accepts ExecuteResult from resolvers containing both metadata and values
func (e *Engine) Execute(ctx context.Context, resolverResult *resolver.ExecuteResult) (map[string]*Result, error) {
    // Build dependency graph
    if err := e.buildDAG(); err != nil {
        return nil, err
    }
    
    // Compute execution phases from DAG
    runnerResults := e.dag.ComputePhases()
    phaseMap := runnerResults.GetPhaseMap()
    
    // Get sorted phase numbers for deterministic execution
    phases := make([]int, 0, len(phaseMap))
    for phase := range phaseMap {
        phases = append(phases, phase)
    }
    sort.Ints(phases)
    
    // Initialize data context with resolver values (not Results)
    dataCtx := make(map[string]any)
    for k, v := range resolverResult.Values {
        dataCtx[k] = v
    }
    
    // Execute each phase sequentially, but actions within a phase concurrently
    results := make(map[string]*Result)
    
    for _, phase := range phases {
        actionsInPhase := phaseMap[phase]
        
        // Phase-local storage for this phase's action results
        // Actions in phase N should NOT see each other's results
        phaseResults := make(map[string]*Result)
        phaseResultsMu := sync.Mutex{}
        
        // Use errgroup for concurrent execution within phase
        g, gctx := errgroup.WithContext(ctx)
        
        for _, exec := range actionsInPhase {
            actionName := exec.Name
            action := e.actions[actionName]
            currentPhase := phase // Capture phase for this iteration
            
            g.Go(func() error {
                // Clone data context from resolver results + PREVIOUS phase action results only
                // This ensures actions in phase N cannot see each other's results
                currentDataCtx := maps.Clone(dataCtx)
                
                var result *Result
                var err error
                
                // Handle foreach
                if action.Foreach != nil {
                    foreachResults, fErr := action.executeWithForeach(gctx, currentDataCtx)
                    result = aggregateForeachResults(actionName, foreachResults)
                    err = fErr
                } else {
                    // Regular execution
                    result, err = action.Execute(gctx, currentDataCtx)
                }
                
                if err != nil {
                    return fmt.Errorf("action %s failed: %w", actionName, err)
                }
                
                // Set phase number for tracking
                result.Phase = currentPhase
                
                // Store result in phase-local map
                phaseResultsMu.Lock()
                phaseResults[actionName] = result
                phaseResultsMu.Unlock()
                
                return nil
            })
        }
        
        // Wait for all actions in this phase to complete before moving to next phase
        if err := g.Wait(); err != nil {
            return results, err
        }
        
        // Merge phase results into global results and data context AFTER phase completes
        // This makes phase N results available to phase N+1
        for name, result := range phaseResults {
            results[name] = result
            dataCtx[name] = result
        }
    }
    
    return results, nil
}
```

**Tests**:
- Linear action pipeline (sequential phases)
- **Concurrent execution within phase** (multiple independent actions execute in parallel)
- **Phase-based execution** (verify phase 1 completes before phase 2 starts)
- **Phase isolation** (verify actions in phase N do NOT see each other's results, only resolver results + previous phase action results)
- **Phase field tracking** (verify result.Phase is correctly populated with execution phase number)
- **Phase field correctness** (verify Phase 1 actions have Phase=1, Phase 2 actions have Phase=2, etc.)
- Fan-out/fan-in patterns
- Conditional dependencies (when clause)
- Foreach with dependencies
- **Foreach phase tracking** (verify aggregated foreach results include correct phase number)
- Error handling and propagation (error stops phase execution)
- **Concurrent error handling** (error in one action stops all actions in same phase)
- Complex multi-action workflows
- **Data context consistency** (concurrent actions see same resolver/action results from previous phases)

**Deliverables**:
- ✅ Complete action engine with concurrent execution
- ✅ DAG integration with phase computation
- ✅ Phase-local result collection for proper isolation
- ✅ 50+ integration tests including concurrency scenarios

---

## Phase 4: Plugin System (Week 7-8)

**Goal**: Implement Terraform-compatible plugin system for external providers.

### 4.1: Plugin Discovery

**Files to create**:
- `pkg/plugin/discovery.go` - Plugin discovery logic
- `pkg/plugin/cache.go` - Plugin cache management
- `pkg/plugin/discovery_test.go` - Discovery tests

**Key Features**:
- Cache directory structure (`~/.scafctl/providers/`)
- Version folder layout
- `provider.json` descriptor parsing
- Binary discovery (Terraform naming convention)

**Implementation**:

```go
// Plugin cache
type Cache struct {
    root string // ~/.scafctl/providers
}

// Discover plugins in cache
func (c *Cache) Discover() ([]PluginDescriptor, error) {
    // Walk cache directory
    // Parse provider.json files
    // Return list of available plugins
}

// Get plugin by reference
func (c *Cache) Get(ref provider.ProviderRef) (*Plugin, error) {
    // Resolve reference to cached plugin
    // Validate descriptor
    // Return plugin info
}

// Plugin descriptor from provider.json
type PluginDescriptor struct {
    Name            string
    Namespace       string
    Version         string
    ProtocolVersion int
    Schema          map[string]any // JSON Schema definition for provider inputs
    Capabilities    provider.Capabilities
    BinaryPath      string
}

// Note: Schema is stored as map[string]any (JSON Schema format) for flexibility and human-readability.
// When communicating with Terraform-protocol plugins via gRPC, the schema is converted to
// tfprotov6.Schema using convertToTerraformSchema() in the plugin adapter.
// This approach provides:
// - Simple, human-readable provider.json files
// - Flexibility to support multiple schema formats (JSON Schema, OpenAPI, etc.)
// - Runtime conversion to Terraform's protobuf schema only when needed for RPC calls

// Example Schema for a CEL expression provider:
// Schema: map[string]any{
//     "type": "object",
//     "properties": map[string]any{
//         "expression": map[string]any{
//             "type":        "string",
//             "description": "The CEL expression to evaluate",
//         },
//         "data": map[string]any{
//             "type":        "object",
//             "description": "Variables to make available in the expression context",
//         },
//     },
//     "required": []string{"expression"},
// }
```

**Tests**:
- Cache directory layout
- Discovery with multiple providers
- Version selection
- Descriptor parsing
- Missing plugin handling

**Deliverables**:
- ✅ Plugin discovery
- ✅ Cache management
- ✅ 20+ tests

---

### 4.2: Terraform Plugin Protocol

**Files to create**:
- `pkg/plugin/handshake.go` - Terraform handshake
- `pkg/plugin/grpc.go` - gRPC client wrapper
- `pkg/plugin/lifecycle.go` - Plugin lifecycle management
- `pkg/plugin/protocol_test.go` - Protocol tests

**Key Features**:
- Use `hashicorp/go-plugin` library
- Terraform protocol version 6 support
- Plugin handshake verification
- gRPC transport
- Process lifecycle (spawn, connect, close)

**Implementation**:

```go
import (
    "github.com/hashicorp/go-plugin"
    "google.golang.org/grpc"
)

// Plugin client
type PluginClient struct {
    client *plugin.Client
    rpc    *PluginRPCClient
}

// Start plugin process
func (p *Plugin) Start(ctx context.Context) (*PluginClient, error) {
    client := plugin.NewClient(&plugin.ClientConfig{
        HandshakeConfig: terraformHandshake,
        Plugins: map[string]plugin.Plugin{
            "provider": &ProviderPlugin{},
        },
        Cmd: exec.CommandContext(ctx, p.BinaryPath),
        AllowedProtocols: []plugin.Protocol{
            plugin.ProtocolGRPC,
        },
    })
    
    rpcClient, err := client.Client()
    if err != nil {
        return nil, err
    }
    
    raw, err := rpcClient.Dispense("provider")
    if err != nil {
        return nil, err
    }
    
    return &PluginClient{
        client: client,
        rpc:    raw.(*PluginRPCClient),
    }, nil
}
```

**Tests**:
- Handshake process
- Plugin spawn and connect
- gRPC communication
- Plugin shutdown
- Error handling

**Deliverables**:
- ✅ Terraform protocol support
- ✅ Plugin lifecycle
- ✅ 15+ tests

---

### 4.3: Plugin Provider Adapter

**Files to create**:
- `pkg/plugin/adapter.go` - Adapt plugin to provider interface
- `pkg/plugin/adapter_test.go` - Adapter tests

**Key Features**:
- Implement `provider.Provider` interface for plugins
- Schema conversion (plugin's map[string]any to provider SchemaDefinition)
- Input/output marshaling
- Error translation

**Implementation**:

```go
// Plugin provider adapter
type PluginProvider struct {
    descriptor PluginDescriptor
    client     *PluginClient
}

// Implement provider.Provider interface
func (p *PluginProvider) Descriptor() provider.Descriptor {
    return provider.Descriptor{
        Name:         p.descriptor.Name,
        Namespace:    p.descriptor.Namespace,
        Version:      p.descriptor.Version,
        Kind:         "plugin",
        Schema:       convertSchema(p.descriptor.Schema), // Convert map[string]any JSON Schema to SchemaDefinition
        Capabilities: p.descriptor.Capabilities,
    }
}

func (p *PluginProvider) Execute(ctx context.Context, input any, dataCtx map[string]any) (any, error) {
    // Marshal input to JSON
    inputJSON, err := json.Marshal(input)
    if err != nil {
        return nil, err
    }
    
    // Marshal data context
    dataCtxJSON, err := json.Marshal(dataCtx)
    if err != nil {
        return nil, err
    }
    
    // Call plugin via gRPC with both input and data context
    outputJSON, err := p.client.rpc.Execute(ctx, inputJSON, dataCtxJSON)
    if err != nil {
        return nil, err
    }
    
    // Unmarshal output
    var output any
    if err := json.Unmarshal(outputJSON, &output); err != nil {
        return nil, err
    }
    
    return output, nil
}
```

**Tests**:
- Descriptor conversion
- Input marshaling
- Output unmarshaling
- Error handling
- Schema validation

**Schema Conversion Strategy**:

The plugin system uses a **two-layer schema approach**:

1. **Storage Layer** (`PluginDescriptor.Schema`): `map[string]any`
   - Format: JSON Schema (human-readable, flexible)
   - Used in: `provider.json` files, caching, serialization
   - Benefits: Simple to write/edit, flexible for future formats

2. **Protocol Layer** (Terraform RPC): `tfprotov6.Schema`
   - Format: Protocol Buffers (Terraform's native schema)
   - Used in: gRPC communication with Terraform-protocol plugins
   - Benefits: Direct compatibility with Terraform ecosystem

**Conversion Functions**:

```go
// convertSchema: JSON Schema → provider.SchemaDefinition
// Used when exposing plugin descriptor to provider consumers
func convertSchema(jsonSchema map[string]any) provider.SchemaDefinition {
    // Parse JSON Schema properties, required fields, types
    // Convert to unified SchemaDefinition structure
    // Return for resolver/action validation
}

// convertToTerraformSchema: JSON Schema → tfprotov6.Schema
// Used ONLY when communicating with Terraform-protocol plugins via gRPC
func convertToTerraformSchema(jsonSchema map[string]any) (*tfprotov6.Schema, error) {
    // Extract properties from JSON Schema
    // Build tfprotov6.SchemaBlock with SchemaAttributes
    // Map JSON Schema types to Terraform types
    // Return protobuf schema for RPC boundary
}
```

**Why Not Use tfprotov6.Schema Everywhere?**
- ❌ Protobuf is harder to write by hand in `provider.json`
- ❌ Couples us tightly to Terraform's protocol internals
- ❌ Makes non-Terraform plugins more complex
- ✅ JSON Schema is industry-standard, well-documented
- ✅ Conversion only happens at RPC boundaries (performance non-issue)
- ✅ Maintains flexibility for future schema formats (OpenAPI, etc.)

**Deliverables**:
- ✅ Plugin adapter
- ✅ Provider interface compliance
- ✅ Schema conversion layer
- ✅ 20+ tests

---

### 4.4: Plugin Installation

**Files to create**:
- `pkg/plugin/install.go` - Plugin installation
- `pkg/plugin/registry_client.go` - Registry API client
- `pkg/plugin/install_test.go` - Installation tests

**Key Features**:
- Download from registry
- Checksum verification
- Cache installation
- Version constraint resolution

**Implementation**:

```go
// Plugin installer
type Installer struct {
    cache    *Cache
    registry *RegistryClient
}

// Install plugin
func (i *Installer) Install(ctx context.Context, ref provider.ProviderRef) error {
    // 1. Query registry for versions
    versions, err := i.registry.ListVersions(ctx, ref)
    if err != nil {
        return err
    }
    
    // 2. Resolve version constraint
    version, err := resolveVersion(ref.Version, versions)
    if err != nil {
        return err
    }
    
    // 3. Download plugin binary and descriptor
    binary, descriptor, err := i.registry.Download(ctx, ref.Namespace, ref.Name, version)
    if err != nil {
        return err
    }
    
    // 4. Verify checksums
    if err := verifyChecksums(binary, descriptor); err != nil {
        return err
    }
    
    // 5. Install to cache
    return i.cache.Install(ref.Namespace, ref.Name, version, binary, descriptor)
}
```

**Tests**:
- Registry API client
- Version resolution
- Download and verification
- Cache installation
- Error scenarios

**Deliverables**:
- ✅ Plugin installation
- ✅ Registry integration
- ✅ 25+ tests

---

## Phase 5: Solution Integration (Week 9-10)

**Goal**: Connect solutions with resolvers and actions, add CLI commands.

### 5.1: Solution Resolver/Action Schema

**Files to update**:
- `pkg/solution/solution.go` - Add Resolvers and Actions fields
- `pkg/solution/solution_test.go` - Update tests

**Key Changes**:

```go
// Enhanced Solution struct
type Solution struct {
    APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
    Kind       string   `yaml:"kind" json:"kind"`
    Metadata   Metadata `yaml:"metadata" json:"metadata"`
    Catalog    Catalog  `yaml:"catalog,omitempty" json:"catalog,omitempty"`
    Spec       Spec     `yaml:"spec" json:"spec"`
}

// Spec contains the execution specification for the solution
type Spec struct {
    Providers []ProviderRequirement        `yaml:"providers,omitempty" json:"providers,omitempty"`
    Resolvers map[string]resolver.Resolver `yaml:"resolvers,omitempty" json:"resolvers,omitempty"`
    Actions   map[string]action.Action     `yaml:"actions,omitempty" json:"actions,omitempty"`
}

type ProviderRequirement struct {
    Namespace string `yaml:"namespace" json:"namespace"`
    Name      string `yaml:"name" json:"name"`
    Version   string `yaml:"version,omitempty" json:"version,omitempty"` // semver constraint
}
```

**Tests**:
- Solution with resolvers
- Solution with actions
- Solution with provider requirements
- Full YAML round-trip
- Validation

**Deliverables**:
- ✅ Enhanced solution schema
- ✅ YAML parsing
- ✅ 15+ tests

---

### 5.2: Solution Execution Engine

**Files to create**:
- `pkg/solution/engine.go` - Orchestrate resolvers and actions
- `pkg/solution/engine_test.go` - Engine tests

**Key Features**:
- Load solution from file/URL
- Validate provider requirements
- Execute resolver pipeline
- Execute action DAG
- Return results

**Implementation**:

```go
// Solution engine
type Engine struct {
    providerRegistry *provider.Registry
    pluginCache      *plugin.Cache
    resolverEngine   *resolver.Engine
    actionEngine     *action.Engine
}

// ExecutionResult contains results from solution execution
type ExecutionResult struct {
    ResolverResults map[string]*resolver.Result // Resolver execution metadata
    ResolverValues  map[string]any              // Resolver values for reference
    ActionResults   map[string]*action.Result   // Action execution metadata
}

// Execute solution
func (e *Engine) Execute(ctx context.Context, sol *Solution, opts ExecuteOptions) (*ExecutionResult, error) {
    // 1. Validate provider requirements
    if err := e.validateProviders(sol.Spec.Providers); err != nil {
        return nil, err
    }
    
    // 2. Execute resolvers
    resolverExecuteResult, err := e.resolverEngine.Execute(ctx)
    if err != nil {
        return nil, err
    }
    
    // 3. Execute actions (if requested)
    var actionResults map[string]*action.Result
    if opts.ExecuteActions {
        actionResults, err = e.actionEngine.Execute(ctx, resolverExecuteResult)
        if err != nil {
            return nil, err
        }
    }
    
    return &ExecutionResult{
        ResolverResults: resolverExecuteResult.Results,
        ResolverValues:  resolverExecuteResult.Values,
        ActionResults:   actionResults,
    }, nil
}
```

**Tests**:
- Solution with only resolvers
- Solution with resolvers and actions
- Provider requirement validation
- Execution options
- Error handling

**Deliverables**:
- ✅ Solution engine
- ✅ Full integration
- ✅ 30+ tests

---

### 5.3: CLI Run Command

**Files to create**:
- `pkg/cmd/scafctl/run/run.go` - Run command
- `pkg/cmd/scafctl/run/solution/solution.go` - Solution runner
- `pkg/cmd/scafctl/run/provider/provider.go` - Direct provider runner
- `pkg/cmd/scafctl/run/*_test.go` - CLI tests

**Key Features**:
- `scafctl run solution:<id>` - Run solution
- `scafctl run provider:<namespace/name>` - Run provider directly
- `-r key=value` - Set resolver values
- `--action name` - Run specific action
- `--dry-run` - Show execution plan
- `--no-cache` - Force re-evaluation

**Commands**:

```bash
# Run solution
scafctl run solution:example.app/frontend

# Run with resolver overrides
scafctl run solution:myapp -r env=prod -r version=1.2.3

# Run specific action
scafctl run solution:myapp --action build

# Dry run
scafctl run solution:myapp --dry-run

# Run provider directly (testing/debugging)
scafctl run provider:scafctl/shell --input '{"cmd": ["echo", "hello"]}'
```

**Tests**:
- Run solution
- Run with resolvers
- Run with actions
- Dry run mode
- Provider direct run
- Error scenarios

**Deliverables**:
- ✅ CLI run command
- ✅ Multiple run modes
- ✅ 25+ tests

---

### 5.4: CLI Provider Management Commands

**Files to create**:
- `pkg/cmd/scafctl/provider/install.go` - Install command
- `pkg/cmd/scafctl/provider/list.go` - List command
- `pkg/cmd/scafctl/provider/update.go` - Update command
- `pkg/cmd/scafctl/provider/remove.go` - Remove command
- `pkg/cmd/scafctl/provider/*_test.go` - Tests

**Commands**:

```bash
# Install provider
scafctl provider install scafctl/shell
scafctl provider install scafctl/shell@1.0.0

# List installed providers
scafctl provider list
scafctl provider list --available

# Update provider
scafctl provider update scafctl/shell
scafctl provider update --all

# Remove provider
scafctl provider remove scafctl/shell
scafctl provider remove scafctl/shell@1.0.0
```

**Tests**:
- Install provider
- List providers
- Update providers
- Remove providers
- Version constraints

**Deliverables**:
- ✅ Provider management CLI
- ✅ 20+ tests

---

## Phase 6: Catalog System (Week 11-12)

**Goal**: Implement unified catalog for solutions, providers, and artifacts.

### 6.1: Catalog Structure & Types

**Files to create**:
- `pkg/catalog/types.go` - Catalog structures
- `pkg/catalog/metadata.go` - Metadata files (catalog.json, index.json, build.json)
- `pkg/catalog/types_test.go` - Type tests

**Key Components**:

```go
// Root catalog manifest
type Catalog struct {
    SchemaVersion string
    GeneratedAt   time.Time
    Types         map[string]TypeInfo
}

type TypeInfo struct {
    Path   string
    Count  int
    Latest []LatestItem
}

// Per-artifact index
type Index struct {
    ID          string
    Type        string
    Description string
    Tags        []string
    Versions    []VersionInfo
}

type VersionInfo struct {
    Version      string
    ArtifactPath string
    Meta         string // path to build.json
    CreatedAt    time.Time
    Digest       string
}

// Build metadata
type BuildMetadata struct {
    SchemaVersion   string
    Type            string
    ID              string
    Version         string
    CreatedAt       time.Time
    BuiltBy         BuildInfo
    Inputs          map[string]any
    PrimaryArtifact string
    Artifacts       []ArtifactInfo
    Metadata        map[string]any
}
```

**Tests**:
- Structure creation
- JSON marshaling
- Validation

**Deliverables**:
- ✅ Catalog type system
- ✅ 15+ tests

---

### 6.2: Catalog Backend Interface

**Files to create**:
- `pkg/catalog/backend/interface.go` - Backend interface
- `pkg/catalog/backend/filesystem.go` - Local filesystem backend
- `pkg/catalog/backend/gcs.go` - Google Cloud Storage backend (stub)
- `pkg/catalog/backend/*_test.go` - Backend tests

**Key Features**:
- Abstract backend interface
- File operations (read, write, list, delete)
- URI resolution (file://, gs://, s3://)
- Local filesystem implementation

**Implementation**:

```go
// Catalog backend interface
type Backend interface {
    // Read file
    Read(ctx context.Context, path string) ([]byte, error)
    
    // Write file
    Write(ctx context.Context, path string, data []byte) error
    
    // List directory
    List(ctx context.Context, path string) ([]string, error)
    
    // Delete file
    Delete(ctx context.Context, path string) error
    
    // Stat file
    Stat(ctx context.Context, path string) (*FileInfo, error)
}

// Parse backend URI
func ParseURI(uri string) (Backend, error) {
    switch {
    case strings.HasPrefix(uri, "file://") || filepath.IsAbs(uri):
        return NewFilesystemBackend(uri), nil
    case strings.HasPrefix(uri, "gs://"):
        return NewGCSBackend(uri), nil
    case strings.HasPrefix(uri, "s3://"):
        return NewS3Backend(uri), nil
    default:
        return nil, errors.New("unsupported backend URI")
    }
}
```

**Tests**:
- Filesystem backend CRUD
- URI parsing
- Path normalization
- Error handling

**Deliverables**:
- ✅ Backend interface
- ✅ Filesystem backend
- ✅ 25+ tests

---

### 6.3: Catalog Reader

**Files to create**:
- `pkg/catalog/reader.go` - Read catalog metadata
- `pkg/catalog/reader_test.go` - Reader tests

**Key Features**:
- Read catalog.json
- Read index.json for artifact type
- Read build.json for version
- Artifact discovery
- Version resolution

**Implementation**:

```go
// Catalog reader
type Reader struct {
    backend Backend
}

// Read root catalog
func (r *Reader) ReadCatalog(ctx context.Context) (*Catalog, error)

// Read artifact index
func (r *Reader) ReadIndex(ctx context.Context, artifactType, id string) (*Index, error)

// Read build metadata
func (r *Reader) ReadBuildMetadata(ctx context.Context, artifactType, id, version string) (*BuildMetadata, error)

// List artifacts
func (r *Reader) ListArtifacts(ctx context.Context, artifactType string) ([]string, error)

// Get latest version
func (r *Reader) GetLatestVersion(ctx context.Context, artifactType, id string) (string, error)
```

**Tests**:
- Read catalog
- Read index
- Read build metadata
- List artifacts
- Version resolution
- Missing file handling

**Deliverables**:
- ✅ Catalog reader
- ✅ 20+ tests

---

### 6.4: Catalog Builder

**Files to create**:
- `pkg/catalog/builder.go` - Build catalog artifacts
- `pkg/catalog/builder_test.go` - Builder tests

**Key Features**:
- Build solution artifacts
- Build provider artifacts
- Generate build.json metadata
- Validate artifacts

**Implementation**:

```go
// Catalog builder
type Builder struct {
    outputDir string
}

// Build solution
func (b *Builder) BuildSolution(ctx context.Context, opts BuildOptions) (*BuildMetadata, error) {
    // 1. Load and validate solution
    sol, err := solution.UnmarshalFromBytes(opts.SourceData)
    if err != nil {
        return nil, err
    }
    
    // 2. Create version directory
    versionDir := filepath.Join(b.outputDir, "solutions", opts.ID, opts.Version)
    if err := os.MkdirAll(versionDir, 0755); err != nil {
        return nil, err
    }
    
    // 3. Write normalized solution.yaml
    solutionPath := filepath.Join(versionDir, "solution.yaml")
    if err := sol.WriteToFile(solutionPath); err != nil {
        return nil, err
    }
    
    // 4. Generate build.json
    buildMeta := &BuildMetadata{
        SchemaVersion:   "1.0",
        Type:            "solution",
        ID:              opts.ID,
        Version:         opts.Version,
        CreatedAt:       time.Now(),
        PrimaryArtifact: "solution.yaml",
        // ... fill remaining fields
    }
    
    buildPath := filepath.Join(versionDir, "build.json")
    if err := writeBuildMetadata(buildPath, buildMeta); err != nil {
        return nil, err
    }
    
    return buildMeta, nil
}
```

**Tests**:
- Build solution
- Build provider
- Metadata generation
- Directory structure
- Validation

**Deliverables**:
- ✅ Catalog builder
- ✅ 25+ tests

---

### 6.5: Catalog Publisher

**Files to create**:
- `pkg/catalog/publisher.go` - Publish to catalog
- `pkg/catalog/publisher_test.go` - Publisher tests

**Key Features**:
- Upload artifacts to backend
- Update index.json
- Update catalog.json
- Prevent overwrites (unless --force)
- Atomic updates where possible

**Implementation**:

```go
// Catalog publisher
type Publisher struct {
    backend Backend
}

// Publish artifact
func (p *Publisher) Publish(ctx context.Context, opts PublishOptions) error {
    // 1. Check if version already exists
    exists, err := p.versionExists(ctx, opts.Type, opts.ID, opts.Version)
    if err != nil {
        return err
    }
    if exists && !opts.Force {
        return errors.New("version already published, use --force to overwrite")
    }
    
    // 2. Upload artifact files
    if err := p.uploadArtifacts(ctx, opts); err != nil {
        return err
    }
    
    // 3. Update index.json
    if err := p.updateIndex(ctx, opts); err != nil {
        return err
    }
    
    // 4. Update catalog.json
    return p.updateCatalog(ctx, opts.Type)
}
```

**Tests**:
- Publish new version
- Prevent duplicate publish
- Force publish
- Index updates
- Catalog updates
- Error scenarios

**Deliverables**:
- ✅ Catalog publisher
- ✅ 20+ tests

---

### 6.6: CLI Catalog Commands

**Files to create**:
- `pkg/cmd/scafctl/catalog/build.go` - Build command
- `pkg/cmd/scafctl/catalog/publish.go` - Publish command
- `pkg/cmd/scafctl/catalog/sync.go` - Sync command (stub)
- `pkg/cmd/scafctl/catalog/*_test.go` - Tests

**Commands**:

```bash
# Build solution
scafctl catalog build solution ./solutions/frontend \
  --id example.app/frontend \
  --version 2.3.1 \
  --out ./dist/catalog

# Publish solution
scafctl catalog publish solutions example.app/frontend@2.3.1 \
  --catalog staging

# Sync catalog (download for offline)
scafctl catalog sync --catalog public --local ./catalog-mirror
```

**Tests**:
- Build command
- Publish command
- Multiple catalog backends
- Error handling

**Deliverables**:
- ✅ Catalog CLI commands
- ✅ 20+ tests

---

## Phase 7: Additional Providers (Week 13)

**Goal**: Implement remaining built-in providers for complete functionality.

### 7.1: Shell Provider

**Files to create**:
- `pkg/provider/builtin/shell/shell.go` - Shell command execution
- `pkg/provider/builtin/shell/shell_test.go` - Tests

**Features**:
- Execute shell commands
- Working directory support
- Environment variable support
- Capture stdout/stderr
- Exit code handling

**Deliverables**:
- ✅ Shell provider
- ✅ 15+ tests

---

### 7.2: API Provider

**Files to create**:
- `pkg/provider/builtin/api/api.go` - HTTP API client
- `pkg/provider/builtin/api/api_test.go` - Tests

**Features**:
- HTTP methods (GET, POST, PUT, PATCH, DELETE)
- Headers support
- Request body
- Response parsing
- Authentication support

**Deliverables**:
- ✅ API provider
- ✅ 20+ tests

---

### 7.3: File Provider

**Files to create**:
- `pkg/provider/builtin/file/file.go` - File operations
- `pkg/provider/builtin/file/file_test.go` - Tests

**Features**:
- Read files
- Write files
- File existence checks
- Directory operations
- Path resolution

**Deliverables**:
- ✅ File provider
- ✅ 15+ tests

---

### 7.4: State Provider

**Files to create**:
- `pkg/provider/builtin/state/state.go` - State management
- `pkg/provider/builtin/state/state_test.go` - Tests

**Features**:
- Read/write state files
- JSON/YAML state format
- State versioning
- State locking

**Deliverables**:
- ✅ State provider
- ✅ 15+ tests

---

## Phase 8: Documentation & Examples (Week 14)

**Goal**: Create comprehensive documentation and working examples.

### 8.1: API Documentation

**Files to create/update**:
- `docs/api/provider.md` - Provider API reference
- `docs/api/resolver.md` - Resolver API reference
- `docs/api/action.md` - Action API reference
- `docs/api/plugin.md` - Plugin API reference
- `docs/api/catalog.md` - Catalog API reference

**Deliverables**:
- ✅ Complete API documentation
- ✅ Code examples for each API

---

### 8.2: User Guides

**Files to update**:
- `docs/guides/01-getting-started.md` - Update with actual implementation
- `docs/guides/06-providers.md` - Update with provider development guide

**New guides to create**:
- `docs/guides/08-plugin-development.md` - How to create plugins
- `docs/guides/09-catalog-workflow.md` - Build and publish workflow

**Deliverables**:
- ✅ Updated guides
- ✅ Plugin development guide
- ✅ Catalog workflow guide

---

### 8.3: Working Examples

**Directories to create**:
- `examples/simple-resolver/` - Basic resolver example
- `examples/action-pipeline/` - Action orchestration example
- `examples/foreach-deployment/` - Foreach iteration example
- `examples/custom-provider/` - Custom provider plugin example
- `examples/catalog-solution/` - Complete catalog workflow

**Each example should include**:
- `solution.yaml` - Working solution file
- `README.md` - Explanation and usage
- Test scripts or validation

**Deliverables**:
- ✅ 5 working examples
- ✅ Each with documentation
- ✅ All examples tested

---

## Phase 9: Testing & Polish (Week 15-16)

**Goal**: Comprehensive testing, performance optimization, and final polish.

### 9.1: Integration Testing

**Files to create**:
- `test/integration/` - Integration test suite
- `test/e2e/` - End-to-end test scenarios

**Test Scenarios**:
- Complete solution execution (resolvers + actions)
- Plugin lifecycle (install, use, remove)
- Catalog workflow (build, publish, consume)
- Complex DAG execution
- Error scenarios and recovery
- Performance benchmarks

**Deliverables**:
- ✅ 50+ integration tests
- ✅ 20+ e2e tests
- ✅ Performance benchmarks

---

### 9.2: Documentation Review

**Tasks**:
- Review all documentation for accuracy
- Ensure examples work with actual implementation
- Add missing documentation
- Fix any inconsistencies
- Create troubleshooting guide

**Deliverables**:
- ✅ Verified documentation
- ✅ Troubleshooting guide
- ✅ FAQ document

---

### 9.3: CLI Polish

**Tasks**:
- Improve error messages
- Add progress indicators
- Enhance output formatting
- Add shell completion
- Improve help text

**Deliverables**:
- ✅ Polished CLI UX
- ✅ Shell completion (bash, zsh, fish)
- ✅ Comprehensive help text

---

### 9.4: Performance Optimization

**Tasks**:
- Profile hot paths
- Optimize DAG execution
- Optimize CEL evaluation
- Optimize plugin communication
- Add caching where appropriate

**Deliverables**:
- ✅ Performance benchmarks
- ✅ Optimization report
- ✅ 20%+ performance improvement

---

## Summary & Milestones

### Key Milestones

- **Week 2**: Provider system functional ✅
- **Week 4**: Resolver pipeline complete ✅
- **Week 6**: Action orchestration complete ✅
- **Week 8**: Plugin system functional ✅
- **Week 10**: Solution integration complete ✅
- **Week 12**: Catalog system functional ✅
- **Week 13**: All built-in providers complete ✅
- **Week 14**: Documentation complete ✅
- **Week 16**: Production ready ✅

### Test Coverage Goals

| Phase | Target Coverage |
|-------|----------------|
| Provider Foundation | 90%+ |
| Resolver Pipeline | 85%+ |
| Action Orchestration | 85%+ |
| Plugin System | 80%+ |
| Solution Integration | 85%+ |
| Catalog System | 85%+ |
| Overall | 85%+ |

### Dependencies Between Phases

```
Phase 1 (Provider) ──┬─→ Phase 2 (Resolver) ─→ Phase 5 (Solution)
                     │                           ↓
                     └─→ Phase 3 (Action) ──────┘
                                                 ↓
Phase 4 (Plugin) ────────────────────────────→ Phase 5
                                                 ↓
Phase 6 (Catalog) ←──────────────────────────→ Phase 5
                     
Phase 7 (Providers) ─→ Phase 8 (Docs) ─→ Phase 9 (Polish)
```

### Risk Mitigation

**High-Risk Areas**:
1. **Plugin Protocol Compatibility** - Use well-tested go-plugin library
2. **DAG Complexity** - Leverage existing pkg/dag implementation
3. **CEL Integration** - Extensive test coverage for edge cases
4. **Catalog Consistency** - Atomic updates and locking strategy

**Mitigation Strategies**:
- Start with simple cases, add complexity incrementally
- Write tests first for critical paths
- Regular integration testing throughout
- Early prototype of risky components

---

## Next Steps

1. **Review this plan** - Ensure alignment with project goals
2. **Adjust timeline** - Based on team capacity
3. **Start Phase 1** - Provider foundation
4. **Set up CI/CD** - Automated testing from the start
5. **Regular check-ins** - Weekly progress reviews

**Questions to resolve**:
- Do we need plugin authentication/signing in Phase 4?
- Should catalog sync (Phase 6) support delta updates?
- What additional providers are needed in Phase 7?
- Performance requirements for large catalogs?

---

*Last Updated: December 22, 2025*
*Status: Planning - Ready for implementation*
