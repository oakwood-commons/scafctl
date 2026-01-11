# Testing

## Purpose

Testing in scafctl ensures that solutions behave correctly, deterministically, and safely before performing side effects.

scafctl separates data resolution, transformation, and execution. This separation enables testing without mocks, without shell simulation, and without executing actions.

Testing is an outcome of the design, not a separate subsystem.

---

## Core Principles

- All data enters through resolvers
- All computation is provider-backed
- All side effects are isolated to actions
- Render mode produces a fully concrete execution plan

Because of this:

- Resolvers are unit-testable
- Providers are unit-testable
- Solutions are integration-testable without execution

---

## Testing Layers

scafctl supports three testing layers.

---

## 1. Resolver Tests (Unit)

### Scope

Resolver tests validate:

- resolve behavior
- transform logic
- validation rules
- emitted values
- dependency ordering

Resolvers are pure and deterministic. They do not perform side effects.

---

### What Is Tested

- Parameter overrides
- Default fallbacks
- Transform correctness
- Validation failures
- DAG dependency resolution

---

### Example

Given this resolver:

~~~yaml
resolvers:
  environment:
    resolve:
      with:
        - provider: parameter
          inputs:
            key: env
        - provider: static
          inputs:
            value: dev

    transform:
      with:
        - provider: cel
          inputs:
            expression: "__self.toLowerCase()"

    validate:
      with:
        - provider: validation
          inputs:
            expression: "__self in [\"dev\", \"staging\", \"prod\"]"
          message: "Invalid environment"
~~~

Test cases:

- `-r env=Prod` emits `prod`
- `-r env=foo` fails validation
- no parameter emits `dev`

---

### Execution Model

Resolver tests:

- run in-process
- do not require filesystem or network
- do not invoke actions
- do not require render mode

---

## 2. Provider Tests (Unit)

### Scope

Providers are tested independently of:

- resolvers
- actions
- CLI parsing

#### Unit

Providers have unit tests defined in their own code base

#### Integration

Provider tests validate individual provider behavior given concrete inputs.

Provider behavior should also be testable when used within a solution. External dependencies (such as HTTP APIs or authentication systems) must be isolated using mocks, fakes, or test fixtures so that provider tests run deterministically without requiring real network access or credentials.

---

### What Is Tested

- Input schema validation
- Typed input handling
- Deterministic outputs
- Error conditions

---

### Examples

- Template provider renders expected output for given context
- Expression provider evaluates CEL correctly
- Filesystem provider reads and writes expected content in a temp directory
- API provider builds correct request structure (in render mode)

---

### Important Rule

Providers never receive expressions, templates, or resolver references.

All inputs are concrete by the time a provider executes.

---

## 3. Solution Tests (Integration, Render-Only)

### Scope

Solution tests validate the full solution behavior without executing side effects.

Render mode is the primary testing mechanism for solutions.

---

### Render Mode

In render mode, scafctl:

- executes all required resolvers
- evaluates expressions
- renders templates
- resolves conditions
- produces a fully concrete action graph

Render mode does not:

- perform filesystem writes
- make network calls
- execute shell commands

---

### What Is Tested

- Resolver integration
- Action ordering
- foreach expansion
- provider inputs
- conditional logic
- templated output content

---

### Example

~~~bash
scafctl render solution terraform-scaffold \
  -r environments=dev,prod
~~~

Assertions may include:

- two action instances are rendered
- paths are correct
- template outputs match snapshots
- inputs contain no expressions or placeholders

---

### Snapshot Testing

Rendered output may be snapshot-tested.

This is equivalent to:

- `terraform plan`
- `helm template`
- `kubectl apply --dry-run`

Snapshots are stable because render output is deterministic.

---

## What Is Not Tested

The following are intentionally not tested in scafctl:

- shell command correctness
- API side effect behavior
- external system availability
- credentials or secrets

These concerns belong to providers and execution environments, not solution logic.

---

## CI Usage

In publish pipelines, testing typically consists of:

1. Render the solution
2. Validate resolver outputs
3. Validate rendered action graph
4. Optionally lint rendered data

No side effects are required.

---

## Why This Model Works

Compared to imperative task runners:

- no shell simulation is required
- no environment guessing is required

Everything is explicit, declarative, and inspectable.

Testing works because the architecture enforces:

- purity where required
- isolation where required
- determinism everywhere else

---

## Summary

scafctl testing is structured around its execution model.

- Resolvers are unit-tested
- Providers are unit-tested
- Solutions are render-tested

This provides strong correctness guarantees without increasing complexity or introducing testing-specific abstractions.
