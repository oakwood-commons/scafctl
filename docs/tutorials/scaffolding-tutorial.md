---
title: "Solution Scaffolding Tutorial"
weight: 15
---

# Solution Scaffolding Tutorial

This tutorial covers using `scafctl new solution` to quickly scaffold new solution files with the correct structure, providers, and best practices.

## Overview

The `new solution` command generates a complete, valid solution YAML file based on your inputs. Instead of writing YAML from scratch (and getting field names wrong), scaffolding gives you a working starting point.

```
┌──────────────┐         ┌──────────────┐         ┌──────────────┐
│  Parameters  │  ────►  │  scafctl     │  ────►  │  Solution    │
│  (name, etc) │         │  new solution│         │  YAML File   │
└──────────────┘         └──────────────┘         └──────────────┘
```

## 1. Quick Start

### Generate a Basic Solution

```bash
scafctl new solution \
  --name my-app \
  --description "My application configuration" \
  --output my-app.yaml
```

This generates a valid solution file with:
- Correct `apiVersion` and `kind`
- Metadata with name, version, and description
- A sample resolver using the `static` provider
- An example workflow action

### Verify the Output

Immediately validate the generated file:

{{< tabs "scaffolding-lint-verify" >}}
{{< tab "Bash" >}}
```bash
scafctl lint -f my-app.yaml
```
{{< /tab >}}
{{< tab "PowerShell" >}}
```powershell
scafctl lint -f my-app.yaml
```
{{< /tab >}}
{{< /tabs >}}

Run it:

{{< tabs "scaffolding-run-solution" >}}
{{< tab "Bash" >}}
```bash
scafctl run solution -f my-app.yaml
```
{{< /tab >}}
{{< tab "PowerShell" >}}
```powershell
scafctl run solution -f my-app.yaml
```
{{< /tab >}}
{{< /tabs >}}

## 2. Choosing Providers

Specify which providers to include in the scaffolded solution:

```bash
# Include specific providers
scafctl new solution \
  --name api-collector \
  --description "Collect data from APIs" \
  --providers http,cel,exec \
  --output api-collector.yaml
```

The generator creates resolver examples using each specified provider with correct input schemas.

### Available Providers

Not sure which providers to use? List them first:

```bash
scafctl get provider
```

Or get details on a specific provider:

```bash
scafctl get provider http
```

## 3. Starting from Examples

Instead of scaffolding from scratch, you can also start from an existing example:

{{< tabs "scaffolding-from-examples" >}}
{{< tab "Bash" >}}
```bash
# Browse available solution examples
scafctl examples list --category solutions

# Download one as a starting point
scafctl examples get solutions/email-notifier/solution.yaml -o my-solution.yaml

# Modify and validate
scafctl lint -f my-solution.yaml
```
{{< /tab >}}
{{< tab "PowerShell" >}}
```powershell
scafctl examples list --category solutions
scafctl examples get solutions/email-notifier/solution.yaml -o my-solution.yaml
scafctl lint -f my-solution.yaml
```
{{< /tab >}}
{{< /tabs >}}

This is useful when you want a more complete starting point with real patterns (parameters, validation, actions, composition).

## 4. Common Scaffolding Patterns

### Resolver-Only Solution

For solutions that just gather and transform data without side-effect actions:

```bash
scafctl new solution \
  --name data-collector \
  --description "Gather configuration from multiple sources" \
  --providers static,env,http,cel \
  --output data-collector.yaml
```

Then run with the resolver command:

```bash
scafctl run resolver -f data-collector.yaml -o yaml
```

### Full Workflow Solution

For solutions with actions:

```bash
scafctl new solution \
  --name deploy-pipeline \
  --description "Deployment automation pipeline" \
  --providers static,exec,cel \
  --output deploy-pipeline.yaml
```

### Adding Tests After Scaffolding

Once your solution works, add functional tests:

{{< tabs "scaffolding-add-tests" >}}
{{< tab "Bash" >}}
```bash
# Validate the solution
scafctl lint -f my-solution.yaml

# Use the MCP server to generate test scaffolding (if using AI tools)
# Or see the Functional Testing Tutorial for manual test writing
```
{{< /tab >}}
{{< tab "PowerShell" >}}
```powershell
scafctl lint -f my-solution.yaml
```
{{< /tab >}}
{{< /tabs >}}

## 5. Workflow

The recommended scaffolding workflow:

1. **Scaffold** — `scafctl new solution --name my-app --output my-app.yaml`
2. **Lint** — `scafctl lint -f my-app.yaml`
3. **Edit** — Modify resolvers and actions for your needs
4. **Lint again** — `scafctl lint -f my-app.yaml`
5. **Test** — `scafctl run resolver -f my-app.yaml` to verify resolvers
6. **Run** — `scafctl run solution -f my-app.yaml` for the full workflow

## Next Steps

- [Getting Started](getting-started.md) — Full getting started guide
- [Resolver Tutorial](resolver-tutorial.md) — Deep dive into resolvers
- [Actions Tutorial](actions-tutorial.md) — Build workflow actions
- [Provider Reference](provider-reference.md) — All available providers
- [Functional Testing Tutorial](functional-testing.md) — Add automated tests
- [Eval Tutorial](eval-tutorial.md) — Test expressions before adding to solutions
