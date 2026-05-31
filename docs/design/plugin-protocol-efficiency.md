---
title: "Plugin Protocol Efficiency"
weight: 40
---

# Plugin Protocol Efficiency

## Problem

When a plugin provider is called via gRPC, the entire resolver context (`_` map) is serialized into every `ExecuteProviderRequest`. For solutions with large accumulated resolver data, this causes gRPC `ResourceExhausted` errors because the message exceeds the hardcoded 4MB gRPC max message size limit.

The problem is compounded by `forEach`, which makes N separate gRPC calls (one per iteration), each carrying the full context.

**Reference**: [GitHub Issue #451](https://github.com/oakwood-commons/scafctl/issues/451)

---

## Current State

> **Updated**: The data flow below reflects the state **after** P0 was implemented. The resolver context (`_`) is no longer sent over the wire.

### Data flow per ExecuteProvider call (post-P0)

~~~text
Host (engine)                              Plugin (gRPC server)
     |                                          |
     |  ExecuteProviderRequest {                 |
     |    input:             ~1-100KB            |
     |    context:           empty  <-- pruned   |
     |    parameters:        ~1-10KB             |
     |    solution_metadata: ~500B               |
     |    iteration_context: ~1KB                |
     |    ...flags/strings                       |
     |  }                                        |
     |------------------------------------------>|
     |                                           |
     |  ExecuteProviderResponse {                |
     |    output: ~1-100KB                       |
     |  }                                        |
     |<------------------------------------------|
~~~

### Prior state (before P0)

The `context` field carried the full `_` map (all resolved resolver values), which could reach 1–100+ MB for data-heavy solutions. This caused `ResourceExhausted` gRPC errors when the message exceeded the 4 MB default limit.

### Why sending `_` was wrong

1. **The engine resolves all ValueRefs before calling the plugin.** By the time `ExecuteProvider` is called, every `expr:`, `rslvr:`, and `tmpl:` reference has been evaluated to a concrete value. The plugin receives fully resolved `input` -- it has no use for `_`.

2. **Sending `_` was a side-channel.** Providers declare an input schema via `Descriptor()`. That schema is the provider's API contract. Accessing `_` directly bypasses input validation, creates invisible dependencies, and makes provider behavior non-deterministic.

3. **Terraform does not do this.** Terraform sends only the provider's configured arguments across the plugin boundary, never the full state. This is the principle of least privilege applied to plugin communication.

4. **The gRPC max message size was hardcoded at 4MB.** The Go gRPC library default `MaxRecvMsgSize` is 4,194,304 bytes. scafctl now configures this to 64MB (see P0 below), and the resolver context is no longer included in messages.

---

## Changes

> **Status**: P0 is implemented and shipped. P1–P3 are planned future phases.

### P0: Prune resolver context from plugin calls ✅

**What**: Stop serializing `resolverContext` into `ExecuteProviderRequest.Context`.

**Where**: `buildExecuteProviderRequest()` in `pkg/plugin/grpc.go` -- remove the `resolverContext` key from `contextData`. On the server side, `applyRequestContext()` already handles missing context gracefully (the `if resolverCtx, ok := ...` guard returns false).

**Why this is safe**:

- The engine resolves all ValueRefs before the gRPC call. Plugins receive concrete input values.
- No plugin in the SDK or known ecosystem reads `_` from context. The `InputResolver` on the plugin side creates an empty map when no resolver context is present -- this is the correct behavior.
- Built-in providers (CEL, debug) that do read `_` run in-process and never go through gRPC. They are unaffected.
- This is an alpha tool. Breaking changes are expected.

**Server-side behavior**: `applyRequestContext()` will simply not set resolver context on the Go context. `provider.ResolverContextFromContext()` returns `(nil, false)`. `NewInputResolver` handles this by creating an empty map. No crash, no behavior change for well-behaved plugins.

### P0: Configurable gRPC max message size ✅

**What**: Add `GRPCDialOptions` with `grpc.MaxCallRecvMsgSize(N)` and `grpc.MaxCallSendMsgSize(N)` to the `plugin.ClientConfig`. Expose the value via settings.

**Where**:

- `pkg/plugin/client.go` -- add `GRPCDialOptions` to `plugin.ClientConfig`
- `pkg/settings/` -- add `Plugin.GRPCMaxMessageSize` with a default (64MB)
- `pkg/plugin/pool.go` -- thread the setting through to `connectPlugin`

**Why**: Even after pruning resolver context, legitimately large inputs (e.g., a 10MB HCL file) or large outputs can exceed 4MB. A configurable limit provides a safety valve without requiring code changes.

**Default**: 64MB (`67108864`). This matches common gRPC production defaults and is large enough for any reasonable provider payload while still protecting against unbounded allocation.

### P1: Batch execution for forEach (planned)

**What**: Add a `BatchExecuteProvider` RPC to the plugin protocol that sends all forEach items in a single request.

**Current behavior**: forEach with 1000 items = 1000 separate gRPC round-trips, each with full serialization overhead (parameters, metadata, context flags). This is the biggest performance bottleneck for data-heavy solutions.

**New behavior**: The host sends a single `BatchExecuteProviderRequest` containing the common fields once, plus a `repeated IterationItem items` array. The plugin processes all items and returns a `repeated IterationResult results` array.

**Protocol addition**:

~~~protobuf
message BatchExecuteProviderRequest {
  string provider_name = 1;
  bytes common_input = 2;        // Shared inputs (non-forEach-variable fields)
  repeated IterationItem items = 3;
  bool dry_run = 4;
  string execution_mode = 5;
  string working_directory = 6;
  string output_directory = 7;
  string conflict_strategy = 8;
  bool backup = 9;
  bytes parameters = 10;
  SolutionMeta solution_metadata = 11;
}

message IterationItem {
  bytes input_overrides = 1;     // Per-item input values (merged with common_input)
  IterationContext iteration_context = 2;
}

message BatchExecuteProviderResponse {
  repeated IterationResult results = 1;
  string error = 2;              // Batch-level error (aborts all)
}

message IterationResult {
  int32 index = 1;
  bytes output = 2;
  string error = 3;
  int32 exit_code = 4;
  repeated Diagnostic diagnostics = 5;
}
~~~

**Backward compatibility**: Plugins that don't implement `BatchExecuteProvider` return `codes.Unimplemented`. The host falls back to individual `ExecuteProvider` calls per iteration (current behavior). No breakage for existing plugins.

**Host-side logic** (in `pkg/resolver/executor.go`):

1. Check if the provider supports batch execution (capability on descriptor, or attempt + fallback)
2. If supported: collect all forEach items, build `BatchExecuteProviderRequest`, single RPC call
3. If not supported: fall back to current per-item `ExecuteProvider` loop

**Concurrency**: The plugin controls internal parallelism for batch execution. The `forEach.concurrency` field from the solution YAML can be sent as a hint in the batch request.

### P1: Host-side input validation before gRPC call (planned)

**What**: Validate resolved inputs against the provider's schema on the host side before serializing and sending the gRPC request.

**Where**: `pkg/resolver/executor.go` -- after resolving all inputs and before calling `prov.Execute()`, validate against the cached descriptor schema.

**Current behavior**: Invalid inputs (wrong type, missing required field, constraint violation) cross the gRPC boundary, get deserialized on the plugin side, fail validation there, serialize the error, send it back, and get deserialized on the host side. A full wasted round-trip.

**New behavior**: The host validates inputs against the descriptor's JSON Schema before the call. If validation fails, the error is returned immediately with a clear message indicating which input violated which constraint. The plugin can still re-validate (defense in depth).

**Edge case**: Some plugins may accept inputs not declared in their schema (passthrough/dynamic inputs). If the schema has `additionalProperties: true` or no schema is defined, skip host-side validation.

### P2: Move static data to ConfigureProvider (planned)

**What**: Send solution parameters and solution metadata once during `ConfigureProvider` instead of on every `ExecuteProvider` call.

**Where**:

- `pkg/plugin/grpc.go` -- add `parameters` and `solution_metadata` fields to `ConfigureProviderRequest`
- `pkg/plugin/grpc.go` -- remove them from `buildExecuteProviderRequest` (or mark deprecated)
- Plugin SDK server side -- store configured parameters and metadata, merge into context for each `ExecuteProvider` call

**Why**: Parameters and solution metadata are immutable for the lifetime of an execution. Sending them on every call (especially with forEach) is redundant serialization.

**Protocol consideration**: This requires a plugin SDK update. For backward compatibility, the host can send parameters in both `ConfigureProvider` and `ExecuteProvider` initially, then drop them from `ExecuteProvider` in a future protocol version.

### P3: Response size bounds (planned)

**What**: Set `grpc.MaxCallRecvMsgSize` on the client to bound response sizes. Log a warning when responses exceed a threshold (e.g., 80% of the limit).

**Why**: A misbehaving plugin returning an unbounded response can OOM the host. The same `GRPCMaxMessageSize` setting from P0 applies bidirectionally.

---

## What We Are NOT Doing

### Selective context (send only referenced resolver keys)

Rejected. The engine resolves all ValueRefs before calling the plugin, so the plugin never needs `_` at all. Selective context is a half-measure -- it still sends data the plugin shouldn't access. Full pruning is the correct approach.

### Opt-in context via plugin capability flag

Rejected. No plugin should access `_` directly. If a legitimate use case surfaces, the right solution is an explicit `contextKeys` field on the provider source in the solution YAML:

~~~yaml
- provider: some-plugin
  contextKeys: [config, environment]
  inputs:
    operation: parse
~~~

This is explicit, minimal, auditable, and bounded. Build it when someone needs it, not before.

### Response streaming for large outputs

Deferred. `ExecuteProviderStream` already handles stdout/stderr streaming. Streaming the final result payload would require a new chunked protocol and is not justified until we have a concrete use case exceeding the 64MB default.

---

## Implementation Order

| Phase | Items | Scope | Breaking | Status |
|-------|-------|-------|----------|--------|
| 1 | P0: Prune context + configurable max message size | `pkg/plugin/`, `pkg/settings/` | Yes (plugin protocol) | ✅ Shipped |
| 2 | P1: Host-side input validation | `pkg/resolver/` | No | Planned |
| 3 | P1: Batch execution | Plugin SDK + `pkg/plugin/` + `pkg/resolver/` | No (additive) | Planned |
| 4 | P2: Move static data to ConfigureProvider | Plugin SDK + `pkg/plugin/` | No (additive, then deprecate) | Planned |
| 5 | P3: Response size bounds | `pkg/plugin/` | No | Planned |

Phase 1 resolves the immediate issue (#451). Each subsequent phase is independently valuable and can be shipped separately.

---

## Testing

- **P0 context pruning** ✅: `TestBuildExecuteProviderRequest_RoundTrip` asserts `req.Context` is empty. `TestBuildExecuteProviderRequest_ResolverContextPruned` creates 100 resolvers × 10KB each and verifies no context is sent.
- **P0 max message size** ✅: `TestWithGRPCMaxMessageSize_SetsClientOption` and `TestWithGRPCMaxMessageSize_DefaultUsedWhenZero` cover the client option. Full e2e plugin tests pass with the new dial options.
- **P1 batch execution** (planned): Test forEach with 100+ items via batch RPC. Verify results match per-item execution. Test fallback when plugin doesn't implement batch.
- **P1 host-side validation** (planned): Test that invalid inputs fail before the gRPC call with schema-specific error messages.
- **P2 static data** (planned): Verify parameters and metadata are available in plugin context after ConfigureProvider, even when not sent in ExecuteProvider.

---

## Risks

- **Phase 1 is a breaking protocol change.** Third-party plugins that read `resolverContext` from the request context will get an empty map. This is intentional -- they should use declared inputs instead. A lint rule can help surface this during solution development.
- **Batch execution changes the error model.** Per-item errors vs batch-level errors need clear semantics. A single poisoned item should not abort the entire batch unless the plugin explicitly chooses to.
- **Plugin SDK must be updated in lockstep for Phase 3+.** The proto definitions live in `scafctl-plugin-sdk`. Phases 1-2 only touch the host side.
