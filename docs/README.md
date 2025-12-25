# scafctl Documentation

This folder contains comprehensive documentation for scafctl - a schema-first execution engine for configuration discovery, scaffolding, and controlled actions.

## Core Principle

**Nothing runs unless explicitly requested.** Resolvers produce data deterministically; providers are pluggable, stateless execution primitives; actions cause side effects only when invoked.

## Documentation Structure

### [Guides](./guides)
Step-by-step tutorials and conceptual explanations.

- **[Getting Started](./guides/01-getting-started.md)** - Introduction to scafctl concepts
- **[Resolver Chain](./guides/02-resolver-pipeline.md)** - Deep dive into resolve → transform → validate → emit
- **[Transform Phase](./guides/03-transform-phase.md)** - Mastering `into:`, `when:`, `until:` patterns
- **[Action Orchestration](./guides/04-action-orchestration.md)** - Dependencies, conditional execution, foreach
- **[Expression Language](./guides/05-expression-language.md)** - CEL vs templating, reserved keywords
- **[Providers Guide](./guides/06-providers.md)** - Available providers and their capabilities
- **[Templates Guide](./guides/07-templates.md)** - Template sources, rendering, and integration

### [Schemas](./schemas)
Formal schema definitions and reference documentation.

- **[Solution Schema](./schemas/solution-schema.md)** - Top-level solution.yaml structure
- **[Resolver Schema](./schemas/resolver-schema.md)** - Resolver definition schema
- **[Transform Schema](./schemas/transform-schema.md)** - Transform phase configuration
- **[Action Schema](./schemas/action-schema.md)** - Action definition schema
- **[Provider Schema](./schemas/provider-schema.md)** - Provider interface and configuration
- **[Config Schema](./schemas/config-schema.md)** - CLI configuration structure (profiles)
- **[Auth Session Schema](./schemas/auth-session.md)** - Stored credential format

### [Reference](./reference)
Quick lookup documentation.

- **[Provider Reference](./reference/providers.md)** - Complete provider type reference
- **[CEL Functions](./reference/cel-functions.md)** - Available CEL functions and examples
- **[Template Functions](./reference/template-functions.md)** - Go template helpers
- **[Resolver Context](./reference/resolver-context.md)** - Context variables (`_`, `__self`, `__item`)
- **[Best Practices](./reference/best-practices.md)** - Recommended patterns and antipatterns
- **[Authentication Reference](./reference/auth.md)** - Auth providers, token broker, storage

### [CLI](./cli)
Markdown proposal for auto-generated CLI help.

- **[CLI Docs Proposal](./cli/README.md)** - Planned structure for `scafctl --help` exports

## Quick Links

- **Want to learn scafctl?** → Start with [Getting Started](./guides/01-getting-started.md)
- **Need to understand how resolvers work?** → Read [Resolver Chain](./guides/02-resolver-pipeline.md)
- **Building complex transforms?** → Check [Transform Phase](./guides/03-transform-phase.md)
- **Generating configuration or code?** → See [Templates Guide](./guides/07-templates.md)
- **Looking up a provider?** → Find it in [Provider Reference](./reference/providers.md)
- **Need the exact schema?** → Browse [Schemas](./schemas)
- **Curious about future CLI docs?** → Read the [CLI proposal](./cli/README.md)
- **Want to see examples?** → Check `/examples` folder (e.g., `scafctl-build` for self-hosted builds)

## Key Concepts

### Five Pillars of scafctl

1. **Resolvers** - Produce named values deterministically
   - Four-phase resolver chain: resolve → transform → validate → emit
   - Multiple resolve sources, first non-null wins
   - Transform array with conditional stopping via `until:`

2. **Providers** - Stateless execution primitives
   - Input → operation → output/error
   - Never own orchestration or validation
   - Types: `sh`, `go-template`, `cel`, `api`, `filesystem`, `git`

3. **Templates** - Pure renderers with no side effects
   - Three source types: inline, filesystem, remote
   - Render only; never fetch data or mutate
   - Accessed via resolvers
   - Can be used as resolver sources

4. **Actions** - Explicit, opt-in side effects
   - Execute only when explicitly selected
   - Support `dependsOn` for DAG execution
   - Optional `foreach` for iteration
   - Optional `when` for conditional execution

5. **Solutions** - Versioned, declarative units
   - Kubernetes-style: apiVersion, kind, metadata, spec
   - Single source of truth for workflows
   - Catalog-publishable

## Solution File Basics

All solutions follow Kubernetes conventions:

```yaml
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: solution-id
  version: 1.0.0
  displayName: Human-readable name
  description: What this does
  category: infrastructure|application|automation

spec:
  providers: [...]    # Optional: define execution primitives
  resolvers: {...}    # Required: produce data
  templates: [...]    # Optional: define renderers
  actions: {...}      # Optional: define side effects
```

## Expression Languages

scafctl uses **two expression languages strategically**:

- **CEL (Common Expression Language)** - For logic, conditions, data operations
  - Reserved keywords: `when`, `expr`, `foreach.over`, `validate.*`, `dependsOn`
  - Used in: `resolve: from: [provider: expr]`, `transform: when:`, `action: when:`

- **Go Templating** - For text rendering and path generation
  - Field name convention: `path`, `message`, `subject`, `endpoint`, `command`
  - Used in: provider inputs, action commands, display strings

## Getting Help

- **Conceptual?** Read the [Guides](./guides)
- **Technical?** Check [Reference](./reference)
- **Exact format?** See [Schemas](./schemas)
- **Real-world example?** Browse `/examples`

---

Last Updated: December 2025
