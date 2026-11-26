---
title: "scafctl"
description: "Declaratively discover data, shape configuration, and run portable workflows for humans and AI agents."
type: docs
---

<div class="bp-hero" role="region" aria-labelledby="bp-hero-heading">
  <div class="bp-hero-header">
    <img class="bp-lockup" src="images/scafctl-logo.svg" alt="scafctl Configuration Flow" width="180" height="56">
    <span class="bp-edition">OAKWOOD COMMONS // WORKFLOW ENGINE</span>
  </div>
  <div class="bp-hero-body">
    <div>
      <div class="bp-eyebrow"><span class="bp-dot" aria-hidden="true"></span><span>CONFIGURATION FLOW // DECLARATIVE BY DESIGN</span></div>
      <h1 class="bp-hero-title" id="bp-hero-heading">Discover data, shape config,<br>and run portable workflows.</h1>
      <p class="bp-hero-desc"><code>scafctl</code> is a cross-platform CLI and workflow engine for humans and AI agents that turns project setup, configuration, and automation into testable, reusable <strong>Solutions</strong>. Gather live data from APIs, files, Git, and environments, transform and merge configuration safely with <a href="https://cel.dev/">CEL</a>, execute parallel action graphs across Linux, macOS, Windows, and CI, share versioned bundles via OCI registries, and let AI coding assistants inspect schemas and preview changes deterministically over MCP.</p>
      <div class="bp-cta-row">
        <a class="bp-btn-primary" href="docs/tutorials/getting-started/">Get Started -&gt;</a>
        <a class="bp-btn-secondary" href="docs/tutorials/">Explore Tutorials</a>
      </div>
      <div class="bp-hero-badges">
        <a href="https://goreportcard.com/report/github.com/oakwood-commons/scafctl"><img src="https://goreportcard.com/badge/github.com/oakwood-commons/scafctl" alt="Go Report Card"></a>
        <a href="https://github.com/oakwood-commons/scafctl/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License"></a>
        <a href="https://github.com/oakwood-commons/scafctl/releases"><img src="https://img.shields.io/github/v/release/oakwood-commons/scafctl" alt="Release"></a>
      </div>
    </div>
    <svg class="bp-diagram" viewBox="0 0 360 300" aria-hidden="true">
      <g class="construction">
        <path d="M42 25V272M318 25V272M24 50H336M24 239H336M180 12V282" stroke-dasharray="3 5"/>
        <path d="M85 17H275M85 12V22M275 12V22M336 65V221M331 65H341M331 221H341M30 38H54M42 26V50M306 251H330M318 239V263"/>
        <circle cx="180" cy="142" r="112" stroke-dasharray="2 6"/>
      </g>
      <g class="flow">
        <path d="M160 48H190L204 62V100H156V52Q156 48 160 48ZM190 48V62H204M180 101V159M90 178V164Q90 159 96 159H264Q270 159 270 165V178M180 159V178"/>
        <rect x="66" y="178" width="48" height="48" rx="7"/>
        <rect x="156" y="178" width="48" height="48" rx="7"/>
        <rect x="246" y="178" width="48" height="48" rx="7"/>
        <ellipse cx="90" cy="195" rx="10" ry="4"/>
        <path d="M80 195V209C80 215 100 215 100 209V195M80 202C80 208 100 208 100 202M172 195L179 202L172 209M182 195L189 202L182 209"/>
        <path d="M265 193L279 202L265 211Z"/>
      </g>
      <circle class="status" cx="209" cy="100" r="10"/>
      <path class="status-mark" d="M205 100L208 103L213 97"/>
      <text x="90" y="248" text-anchor="middle">DATA</text>
      <text x="180" y="248" text-anchor="middle">TRANSFORM</text>
      <text x="270" y="248" text-anchor="middle">EXECUTE</text>
      <text x="180" y="35" text-anchor="middle">CONFIGURATION FLOW</text>
    </svg>
  </div>
  <div class="bp-hero-footer">
    <span>DECLARATIVE BY DESIGN.</span>
    <span class="bp-state">LINUX / MACOS / WINDOWS // 22 PROVIDERS // MCP READY</span>
  </div>
</div>

## 30-Second Walkthrough

{{% steps %}}
1. **Install `scafctl`**

   Install on macOS, Linux, or Windows (`x86_64` and `arm64` archives on [GitHub Releases](https://github.com/oakwood-commons/scafctl/releases/latest)):

   {{< install-tabs "walkthrough" >}}

2. **Define a Solution (`hello.yaml`)**

   Declare resolvers to gather and transform data, and optional workflow actions to execute side effects:

   ```yaml
   apiVersion: scafctl.io/v1
   kind: Solution
   metadata:
     name: hello
     version: 1.0.0
   spec:
     resolvers:
       greeting:
         type: string
         resolve:
           with:
             - provider: cel
               inputs:
                 expression: "'Hello, ' + 'world!'"
   ```

3. **Run and Inspect**

   Evaluate resolvers and emit structured YAML or JSON output:

   ```shell
   scafctl run resolver -f hello.yaml -o yaml
   # greeting: Hello, world!
   ```
{{% /steps %}}

---

## Core Concepts

<div class="bp-pillars">
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="9" y1="13" x2="15" y2="13"/><line x1="9" y1="17" x2="13" y2="17"/></svg>
      <h3>Solution</h3>
    </div>
    <p>A declarative YAML file stating what data to gather and what work to perform -- versionable, composable, testable, and shareable via OCI registries.</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
      <h3>Resolver</h3>
    </div>
    <p>A named pipeline unit that fetches, transforms, and validates a value across fallback sources while executing independent branches in parallel.</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polygon points="5 3 19 12 5 21 5 3"/></svg>
      <h3>Action</h3>
    </div>
    <p>A side-effect operation (run a command, call an API, render a file tree) arranged in a dependency graph with retries, conditions, <code>forEach</code>, and <code>finally</code>.</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/></svg>
      <h3>Provider</h3>
    </div>
    <p>A stateless execution primitive that powers resolvers and actions. Ships with 11 built-in providers and 11 official plugin providers auto-fetched from OCI catalogs.</p>
  </div>
</div>

---

## Providers

`scafctl` ships with 11 built-in providers and 11 official plugin providers (22 total, auto-resolved on first use):

`cel` | `debug` | `directory` | `env` | `exec` | `file` | `git` | `github` | `go-template` | `hcl` | `http` | `identity` | `kubeconfig` | `message` | `metadata` | `parameter` | `secret` | `sleep` | `solution` | `state` | `static` | `validation`

```shell
# List all providers
scafctl get provider

# Inspect a specific provider's schema and examples
scafctl explain provider http
```

---

## Key Features

<div class="bp-pillars">
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
      <h3>CEL &amp; Go Templates</h3>
    </div>
    <p>Combine <a href="https://cel.dev/">Common Expression Language</a> for type-safe filtering and data shaping with Sprig + custom Go templates for multi-file rendering.</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="6" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="12" r="3"/><line x1="8.59" y1="7.51" x2="15.42" y2="10.49"/><line x1="15.41" y1="13.51" x2="8.59" y2="16.49"/></svg>
      <h3>Parallel Dependency Graph</h3>
    </div>
    <p>Resolvers and actions infer and validate dependencies automatically, running independent stages concurrently and previewing plans via <code>--dry-run</code>.</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
      <h3>Cross-Platform Shell</h3>
    </div>
    <p>Execute shell actions identically on Linux, macOS, and Windows using an embedded POSIX shell with zero WSL or Git Bash dependency.</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/><line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/><line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="14" x2="23" y2="14"/><line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="14" x2="4" y2="14"/></svg>
      <h3>AI Agent Integration (MCP)</h3>
    </div>
    <p>Expose live JSON schemas, CEL/template evaluation, linting, source-preserving refactoring, and WhatIf previews to AI coding tools via <code>scafctl mcp serve</code>.</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M22 11.08V12a10 10 0 11-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
      <h3>Functional Testing &amp; Snapshots</h3>
    </div>
    <p>Verify resolvers, validation rules, and sandboxed workflows deterministically with <code>scafctl test functional</code>, lint rules, and diffable execution snapshots.</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg>
      <h3>OCI Catalog &amp; Plugins</h3>
    </div>
    <p>Package multi-file solution bundles and gRPC provider plugins as signed OCI artifacts, with lock-file pinning and offline vendoring.</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0110 0v4"/></svg>
      <h3>Secrets &amp; Multi-Cloud Auth</h3>
    </div>
    <p>OS-keyring AES-256-GCM secrets plus Microsoft Entra ID, GitHub, Google Cloud, and OpenShift authentication (browser PKCE, device code, service principals, workload identity).</p>
  </div>
  <div class="bp-card">
    <div class="bp-card-head">
      <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z"/></svg>
      <h3>State, LSP &amp; Refactoring</h3>
    </div>
    <p>Replay parameters and enforce immutable locks via <code>state</code>, get live editor diagnostics via <code>scafctl lsp</code>, and rename symbols safely with <code>refactor rename</code>.</p>
  </div>
</div>

---

## Documentation

### Get Started
- [Getting Started](docs/tutorials/getting-started/): Install and run your first solution
- [Resolvers](docs/tutorials/resolver-tutorial/): Gather and transform data
- [Actions](docs/tutorials/actions-tutorial/): Execute operations

### Go Deeper
- [CEL Expressions](docs/tutorials/cel-tutorial/): Dynamic evaluation
- [Catalog](docs/tutorials/catalog-tutorial/): Share solutions via OCI
- [Publishing Catalog Solutions](docs/tutorials/publishing-solutions-tutorial/): Design, sign, and publish reusable solutions
- [MCP Server](docs/tutorials/mcp-server-tutorial/): Connect AI coding agents (`scafctl mcp serve`)
- [Provider Reference](docs/tutorials/provider-reference/): All 22 providers

---

## Example: CI/CD Pipeline

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: pipeline
spec:
  resolvers:
    servers:
      type: array
      resolve:
        with:
          - provider: static
            inputs:
              value: ["prod-1", "prod-2"]

  workflow:
    actions:
      build:
        provider: exec
        inputs:
          command: "go build ./..."

      test:
        provider: exec
        dependsOn: [build]
        inputs:
          command: "go test ./..."

      deploy:
        provider: exec
        dependsOn: [test]
        forEach:
          in:
            expr: "_.servers"
          concurrency: 2
        inputs:
          command:
            expr: "'deploy.sh ' + __item"

    finally:
      notify:
        provider: http
        inputs:
          url: "https://hooks.slack.com/..."
          method: POST
```

---

## Links

- [GitHub Repository](https://github.com/oakwood-commons/scafctl)
- [Releases](https://github.com/oakwood-commons/scafctl/releases)
- [Discussions](https://github.com/oakwood-commons/scafctl/discussions)
- [Contributing](https://github.com/oakwood-commons/scafctl/blob/main/CONTRIBUTING.md)
