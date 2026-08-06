---
title: "Tutorials"
weight: 1
bookCollapseSection: false
---

# Tutorials

Step-by-step guides for learning scafctl features.

## Getting Started

- [Getting Started](getting-started.md) — Install scafctl and run your first solution
- [Solution Scaffolding Tutorial](scaffolding-tutorial.md) — Create new solutions with `scafctl new solution`

## Core Tutorials

- [Resolver Tutorial](resolver-tutorial.md) — Master dynamic value resolution
- [Run Resolver Tutorial](run-resolver-tutorial.md) — Debug and inspect resolver execution
- [Run Provider Tutorial](run-provider-tutorial.md) — Test providers in isolation
- [Parameterized Calls Tutorial](calls-tutorial.md) — Define reusable, argument-driven provider requests with `spec.calls`
- [Actions Tutorial](actions-tutorial.md) — Learn how to use actions for workflow automation
- [Authentication Tutorial](auth-tutorial.md) — Set up authentication for your workflows
- [Eval Tutorial](eval-tutorial.md) — Test CEL expressions and Go templates from the CLI
- [Linting Tutorial](linting-tutorial.md) — Validate solutions, explore lint rules, and fix issues
- [Refactoring Tutorial](refactor-tutorial.md) — Rename a resolver, action, call, or function and every reference to it with `refactor rename`
- [Language Server (LSP) Tutorial](lsp-tutorial.md) -- Run `scafctl lsp` for in-editor diagnostics
- [CEL Expressions Tutorial](cel-tutorial.md) — Master CEL expressions and extension functions
- [Go Templates Tutorial](go-templates-tutorial.md) — Generate files with Go template rendering
- [Reference-Data Lookups](reference-data-lookups.md) -- Look up taxonomy values from inline, file, or HTTP datasets
- [Catalog Tutorial](catalog-tutorial.md) — Store and manage solutions in your local catalog
- [MCP Server Tutorial](mcp-server-tutorial.md) — Set up AI agent integration with VS Code Copilot, Claude, Cursor, and Windsurf
- [Security Hardening](security-hardening.md) — Secure your solutions and workflows
- [Validation Patterns Tutorial](validation-patterns-tutorial.md) — Input constraints, runtime validation, and common regex/CEL patterns
- [Snapshots Tutorial](snapshots-tutorial.md) — Capture and compare execution snapshots
- [Functional Testing Tutorial](functional-testing.md) — Write and run automated tests for your solutions
- [Dry-Run Tutorial](dryrun-tutorial.md) -- Preview a solution's action plan without executing it
- [Solution Diff Tutorial](soldiff-tutorial.md) -- Compare two solution files or snapshots
- [Auto-Discovery Tutorial](auto-discovery-tutorial.md) -- How scafctl locates a solution file without `-f`
- [Output Directory Tutorial](output-dir-tutorial.md) -- Redirect file-writing actions to a target directory
- [Template Directory Rendering Tutorial](template-directory-rendering.md) -- Render an entire directory tree of templates in one action

## Operations & Configuration

- [API Server Tutorial](api-server-tutorial.md) — Architecture and endpoint reference for the REST API
- [Serve Tutorial](serve-tutorial.md) — Start and configure the REST API server
- [API Mode Authentication](api-authentication.md) -- Authenticate requests to the REST API server
- [Configuration Tutorial](config-tutorial.md) — Manage application configuration
- [Logging & Debugging Tutorial](logging-tutorial.md) — Control log verbosity, format, and output
- [Telemetry Tutorial](telemetry-tutorial.md) — Ship traces and metrics to Jaeger / Prometheus
- [Cache Tutorial](cache-tutorial.md) — Manage cached data and reclaim disk space
- [State Tutorial](state-tutorial.md) -- Persist resolver values across executions
- [Auth Profiles Tutorial](auth-profiles-tutorial.md) -- Manage multiple named authentication profiles
- [Auth Handler Migration Guide](auth-migration-guide.md) -- Migrate from built-in auth handlers to plugin-based ones
- [GCP Custom OAuth Client Setup](gcp-custom-oauth-tutorial.md) -- Configure a custom OAuth client for GCP authentication
- [Docker Credential Helper Tutorial](credential-helper-tutorial.md) -- Use scafctl as a Docker/Podman credential helper

## Reference

- [Exec Provider Tutorial](exec-provider-tutorial.md) — Cross-platform shell execution with embedded and external shells
- [Directory Provider Tutorial](directory-provider-tutorial.md) — Listing, scanning, and managing directories
- [File Provider Tutorial](file-provider-tutorial.md) -- Reading, writing, and checking files
- [HCL Provider Tutorial](hcl-provider-tutorial.md) — Parse, format, validate, and generate Terraform/OpenTofu HCL configuration files
- [Message Provider Tutorial](message-provider-tutorial.md) — Rich terminal output with styled messages, templates, and quiet-mode control
- [Provider Output Shapes](provider-output-shapes.md) — Quick reference for provider output data shapes
- [Provider Reference](provider-reference.md) — Complete documentation for all built-in providers
- [Provider Extraction Tutorial](provider-extraction-tutorial.md) -- Extract official providers as standalone plugin binaries

## Extension Development

- [Extension Concepts](extension-concepts.md) — Provider vs Auth Handler vs Plugin terminology
- [Provider Development Guide](provider-development.md) — Build custom providers (builtin and plugin)
- [Auth Handler Development Guide](auth-handler-development.md) — Build custom auth handlers (builtin and plugin)
- [Plugin Development Guide](plugin-development.md) — Plugin overview and discovery
- [Plugin Auto-Fetching Tutorial](plugin-auto-fetch-tutorial.md) — Automatically fetch plugins from catalogs at runtime
- [Bundle Plugins & Built-in Providers](bundle-plugins-tutorial.md) -- Declare plugin versions in `bundle.plugins` for reproducible builds
- [Multi-Platform Plugin Build Tutorial](multi-platform-plugin-build.md) — Build plugins for multiple platforms
