---
title: "Dry-Run & WhatIf"
weight: 14
---

# Dry-Run & WhatIf Architecture

This document describes the two complementary dry-run mechanisms in scafctl and how they interact.

---

## Overview

scafctl has two distinct dry-run paths:

| Mechanism | Scope | Command | How it works |
|-----------|-------|---------|--------------|
| **WhatIf** (solution-level) | `run solution --dry-run` | Solution dry-run | Resolvers execute normally (side-effect-free); actions are **never invoked** — each action's provider generates a WhatIf description |
| **DryRunFromContext** (provider-level) | `run provider --dry-run` | Provider dry-run | The provider's `Execute()` method is called with `DryRunFromContext(ctx) == true`; providers return mock data |

### Why Two Mechanisms?

- **Solution-level dry-run** answers "what would this solution do?" — it needs real resolver values to materialize action inputs and generate accurate WhatIf messages. Since resolvers are side-effect-free by design, they run normally.
- **Provider-level dry-run** answers "what would this provider return?" — useful for testing a single provider in isolation. The provider itself decides what mock data to return.

---

## Solution-Level Dry-Run (WhatIf)

### Flow

```text
┌──────────────────┐     ┌────────────────────┐     ┌─────────────────┐
│ Execute resolvers │────>│ dryrun.Generate()  │────>│ WhatIf Report   │
│ (real data)       │     │                    │     │ (JSON/YAML/     │
│                   │     │ For each action:   │     │  table)         │
│                   │     │  - Materialize     │     │                 │
│                   │     │    inputs           │     │                 │
│                   │     │  - Call provider's  │     │                 │
│                   │     │    DescribeWhatIf() │     │                 │
└──────────────────┘     └────────────────────┘     └─────────────────┘
```

1. Resolvers execute normally — they are side-effect-free, so real data is available
2. `dryrun.Generate()` builds the action graph and materializes inputs using real resolver data
3. For each action, `Descriptor.DescribeWhatIf(ctx, inputs)` is called to get a provider-specific message
4. Actions are **never** executed — only their WhatIf descriptions are used

### WhatIf on Descriptor

Providers implement `WhatIf` on their `Descriptor` to generate context-specific descriptions:

```go
desc := &provider.Descriptor{
    Name: "exec",
    WhatIf: func(ctx context.Context, input any) (string, error) {
        inputs, _ := input.(map[string]any)
        command, _ := inputs["command"].(string)
        shell, _ := inputs["shell"].(string)
        return fmt.Sprintf("Would execute via %s shell: %s", shell, command), nil
    },
    // ...
}
```

The `DescribeWhatIf(ctx, input)` helper method on `Descriptor` provides the fallback chain:
1. Call `WhatIf` function if set
2. If `WhatIf` returns an error or empty string, fall back to: `"Would execute {name} provider"`

### Report Structure

```go
type Report struct {
    DryRun       bool           `json:"dryRun"`
    Solution     string         `json:"solution"`
    Version      string         `json:"version,omitempty"`
    HasWorkflow  bool           `json:"hasWorkflow"`
    ActionPlan   []WhatIfAction `json:"actionPlan,omitempty"`
    TotalActions int            `json:"totalActions,omitempty"`
    TotalPhases  int            `json:"totalPhases,omitempty"`
    Warnings     []string       `json:"warnings,omitempty"`
}

type WhatIfAction struct {
    Name               string            `json:"name"`
    Provider           string            `json:"provider"`
    Description        string            `json:"description,omitempty"`
    WhatIf             string            `json:"wouldDo"`
    Phase              int               `json:"phase"`
    Section            string            `json:"section"`
    Dependencies       []string          `json:"dependencies,omitempty"`
    When               string            `json:"when,omitempty"`
    MaterializedInputs map[string]any    `json:"materializedInputs,omitempty"`
    DeferredInputs     map[string]string `json:"deferredInputs,omitempty"`
}
```

### CLI Usage

```bash
# Table output (default)
scafctl run solution -f solution.yaml --dry-run

# JSON output
scafctl run solution -f solution.yaml --dry-run -o json

# Verbose (includes materializedInputs)
scafctl run solution -f solution.yaml --dry-run --verbose
```

### MCP Tool

The `dry_run_solution` MCP tool provides the same functionality. It accepts `resolver_overrides` to override specific resolver values for testing purposes.

---

## Provider-Level Dry-Run (DryRunFromContext)

### Flow

```text
┌─────────────────┐     ┌─────────────────────┐     ┌──────────────────┐
│ CLI sets         │────>│ Provider.Execute()  │────>│ Mock output      │
│ DryRunFromContext│     │ checks context flag │     │ (no side effects)│
│ = true           │     │ returns mock data   │     │                  │
└─────────────────┘     └─────────────────────┘     └──────────────────┘
```

1. `run provider --dry-run` sets `provider.WithDryRun(ctx, true)`
2. Provider's `Execute()` method checks `provider.DryRunFromContext(ctx)`
3. When true, provider returns mock/representative data without side effects

### Implementation Pattern

```go
func (p *MyProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    if provider.DryRunFromContext(ctx) {
        return &provider.Output{
            Data: map[string]any{"result": "[DRY-RUN] Would process input"},
        }, nil
    }
    // ... real execution ...
}
```

### CLI Usage

```bash
scafctl run provider exec command=echo args='["hello"]' --dry-run
```

---

## Plugin Support

### Builtin Providers

Builtin providers implement `WhatIf` directly on their `Descriptor`:

```go
WhatIf: func(ctx context.Context, input any) (string, error) {
    inputs, _ := input.(map[string]any)
    // ... generate message from inputs ...
    return "Would do X with Y", nil
},
```

### Plugin Providers (gRPC)

Plugin providers implement the `DescribeWhatIf` gRPC RPC:

```protobuf
service PluginService {
    rpc DescribeWhatIf(DescribeWhatIfRequest) returns (DescribeWhatIfResponse);
}
```

The `ProviderWrapper` wires up the plugin's gRPC call as the `WhatIf` function on the descriptor. Older plugins that don't implement `DescribeWhatIf` gracefully degrade — the gRPC client detects `codes.Unimplemented` and returns an empty string, which triggers the generic fallback message.

---

## Key Design Decisions

1. **Resolvers always execute during solution dry-run.** They are side-effect-free by design, and real data produces more accurate WhatIf messages than mocked values.

2. **Actions are never executed during solution dry-run.** Only WhatIf descriptions are generated. This is fundamentally different from the provider-level dry-run where providers actively return mock data.

3. **`WhatIf` is optional.** Providers that don't implement it get a generic message (`"Would execute {name} provider"`). This avoids boilerplate for providers where a generic message is sufficient (e.g., resolver-only providers).

4. **`DryRunFromContext` is only set by `run provider --dry-run`.** Solution-level dry-run does not set this flag — it uses the WhatIf model instead. This separation keeps the two mechanisms independent and prevents confusion.
