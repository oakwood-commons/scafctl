// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package concepts

// builtinConcepts defines the canonical set of scafctl domain concepts.
var builtinConcepts = []Concept{
	// --- Resolvers ---
	{
		Name:     "resolver",
		Title:    "Resolver",
		Category: "resolvers",
		Summary:  "A named unit that resolves a value through one or more provider steps (resolve → transform → validate).",
		Explanation: `A resolver is the primary data-gathering primitive in scafctl. Each resolver has a name, an optional type hint, and a pipeline of phases:

1. **resolve** — one or more provider steps that produce the initial value (e.g., prompt user, read file, call API).
2. **transform** — optional steps that reshape or enrich the resolved value.
3. **validate** — optional steps that enforce constraints on the final value.

Resolvers can depend on each other via 'dependsOn' or implicit CEL references (_.otherResolver). The dependency graph must be a DAG — circular references are rejected at lint time.`,
		Examples: []string{
			"spec:\n  resolvers:\n    region:\n      type: string\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              name: region\n              default: us-east-1",
		},
		SeeAlso: []string{"provider", "cel-expression", "depends-on"},
	},
	{
		Name:     "depends-on",
		Title:    "dependsOn",
		Category: "resolvers",
		Summary:  "Declares explicit ordering between resolvers or actions.",
		Explanation: `The 'dependsOn' field creates an explicit edge in the dependency graph. scafctl also infers implicit dependencies from CEL expressions (_.resolverName), but dependsOn is useful when:

- A resolver has a side effect that another resolver needs (e.g., writing a temp file).
- You want to force ordering without a data dependency.
- The implicit detection doesn't cover your case (e.g., dynamic resolver references in templates).

For actions, dependsOn controls execution order within the workflow DAG.`,
		Examples: []string{
			"resolvers:\n  setup:\n    resolve:\n      with:\n        - provider: exec\n          inputs:\n            command: setup.sh\n  main:\n    dependsOn: [setup]\n    resolve:\n      with:\n        - provider: file\n          inputs:\n            path: output.json",
		},
		SeeAlso: []string{"resolver", "action", "dag"},
	},
	// --- Providers ---
	{
		Name:     "provider",
		Title:    "Provider",
		Category: "providers",
		Summary:  "A pluggable executor that performs a specific operation (e.g., read file, call API, prompt user).",
		Explanation: `Providers are the execution engines in scafctl. Each provider has a name, a typed input schema, and produces output. Providers are referenced by name in resolver steps and action definitions.

Built-in providers include: static, parameter, env, file, exec, http, cel, go-template, validation, directory, hcl, and solution. Custom providers can be added via the plugin system.

Use 'list_providers' to see all available providers and 'get_provider_schema' to see a provider's input/output schema.`,
		SeeAlso: []string{"resolver", "action", "go-template-provider"},
	},
	{
		Name:     "go-template-provider",
		Title:    "Go Template Provider",
		Category: "providers",
		Summary:  "Renders Go templates with access to resolver data and custom functions.",
		Explanation: `The go-template provider evaluates Go templates using the standard text/template engine augmented with sprig functions and scafctl extensions.

Template data is available as the root context '.', which contains all resolved values. Custom functions include: slugify, toDnsString, where, selectField, cel, toYaml, fromYaml, toHcl, and all sprig functions.

Common pitfall: Go templates are strongly typed at render time. Passing a string where a map is expected (or vice versa) produces 'can't evaluate field X of type string' errors. Use the cel() function for complex data manipulation.`,
		Examples: []string{
			"resolve:\n  with:\n    - provider: go-template\n      inputs:\n        template: |\n          name: {{ .appName | slugify }}\n          replicas: {{ .replicas }}",
		},
		SeeAlso: []string{"provider", "cel-expression", "template-functions"},
	},
	{
		Name:     "template-functions",
		Title:    "Template Functions",
		Category: "providers",
		Summary:  "Custom functions available in Go templates beyond standard sprig.",
		Explanation: `scafctl extends Go templates with these custom functions:

- **slugify** / **toDnsString** — Convert a string to a DNS-safe label (RFC 1123). Lowercases, replaces non-alphanumeric chars with hyphens, trims, truncates to 63 chars.
- **where** — Filter a list of maps: {{ where "status" "active" .items }}
- **selectField** — Project a single field from a list: {{ selectField "name" .items }}  
- **cel** — Evaluate a CEL expression inline: {{ cel "_.items.filter(x, x.active)" . }}
- **toYaml** / **fromYaml** / **mustToYaml** / **mustFromYaml** — YAML serialization.
- **toHcl** — Convert data to HCL format.

Plus all sprig v3 functions (https://masterminds.github.io/sprig/).`,
		SeeAlso: []string{"go-template-provider", "cel-expression"},
	},
	// --- Actions & Workflow ---
	{
		Name:     "action",
		Title:    "Action",
		Category: "actions",
		Summary:  "A workflow step that performs a side effect using a provider (e.g., create file, deploy resource).",
		Explanation: `Actions are defined under spec.workflow.actions and execute after all resolvers complete. Each action specifies a provider and inputs, and can optionally declare dependencies on other actions via 'dependsOn'.

Actions support: conditional execution (when), retry policies, forEach iteration, timeouts, result schemas, and aliases. The workflow engine executes actions as a DAG, running independent actions in parallel.

Actions can declare an 'alias' field for shorter expression references. For example, alias: config allows using config.results.endpoint instead of the verbose __actions.fetchConfiguration.results.endpoint.

Cleanup actions go under spec.workflow.finally and always execute (even if earlier actions fail).`,
		Examples: []string{
			"spec:\n  workflow:\n    actions:\n      create-config:\n        provider: file\n        inputs:\n          path: output/config.yaml\n          content:\n            tmpl: |\n              region: {{ .region }}",
		},
		SeeAlso: []string{"provider", "depends-on", "workflow"},
	},
	{
		Name:     "workflow",
		Title:    "Workflow",
		Category: "actions",
		Summary:  "The action execution engine that runs actions as a DAG after resolver resolution.",
		Explanation: `The workflow section (spec.workflow) defines actions that execute after all resolvers have been resolved. Actions form a DAG based on their dependsOn declarations and are executed with maximum parallelism.

Key sections:
- **actions** — The main action definitions, executed as a DAG.
- **finally** — Cleanup actions that always run, even after failures.

The workflow engine provides special variables: __actions (results of completed actions), __error (failure info), __item/__index (forEach iteration).`,
		SeeAlso: []string{"action", "depends-on", "dag"},
	},
	// --- CEL ---
	{
		Name:     "cel-expression",
		Title:    "CEL Expression",
		Category: "expressions",
		Summary:  "Common Expression Language expressions used for conditions, filtering, and dynamic values.",
		Explanation: `CEL (Common Expression Language) is used throughout scafctl for:

- **when** conditions on resolvers and actions
- **expr** fields in ValueRef inputs (dynamic values)
- **validation** expressions in the validation provider
- **forEach** iteration sources

The root variable '_' contains all resolved values. Special variables include __self (current resolver value in transform/validate), __actions (workflow results), __item/__index (forEach).

Use list_cel_functions to see all available CEL functions and evaluate_cel to test expressions.`,
		Examples: []string{
			"when: \"_.environment == 'production'\"\n\n# In inputs:\ninputs:\n  value:\n    expr: \"_.items.filter(x, x.status == 'active')\"",
		},
		SeeAlso: []string{"resolver", "action", "template-functions"},
	},
	// --- forEach ---
	{
		Name:     "foreach",
		Title:    "forEach (Array Iteration)",
		Category: "resolvers",
		Summary:  "Iterate over an array in resolve or transform steps, executing a provider once per element.",
		Explanation: `forEach is supported on both resolve.with and transform.with steps. It is NOT supported on validate.with.

When forEach is present, the provider executes once per element and results are collected into an output array preserving order.

Key difference between phases:
- On resolve steps, forEach.in is REQUIRED (no __self available in resolve phase).
- On transform steps, forEach.in defaults to __self (the current value).

Fields:
- item: Variable alias for current element (default: __item always available)
- index: Variable alias for current 0-based index (default: __index always available)
- in: ValueRef for source array (required on resolve, defaults to __self on transform)
- concurrency: Max parallel iterations (0 = unlimited)
- keepSkipped: Retain nil entries for items skipped by when condition (default: false)
- onError: Error handling (fail or continue). Actions only; resolvers ignore this.

Context variables __item and __index are always injected. Custom aliases (item, index fields) are added alongside them.

Filtering: Combine forEach with a step-level 'when' condition to filter arrays. Items where when is false are removed from the output unless keepSkipped is true.`,
		Examples: []string{
			"# Resolve: fan-out HTTP requests\nresolve:\n  with:\n    - provider: http\n      forEach:\n        in:\n          rslvr: urls\n        item: url\n      inputs:\n        url:\n          expr: \"url\"",
			"# Transform: double each number\ntransform:\n  with:\n    - provider: cel\n      forEach:\n        item: num\n      inputs:\n        expression: \"num * 2\"",
		},
		SeeAlso: []string{"resolver", "action", "cel-expression"},
	},
	// --- Testing ---
	{
		Name:     "functional-testing",
		Title:    "Functional Testing",
		Category: "testing",
		Summary:  "Built-in test framework for validating solutions via spec.testing.cases.",
		Explanation: `scafctl includes a functional test framework that runs solution commands in isolated sandboxes and validates results with CEL assertions.

Test cases are defined in spec.testing.cases (or composed from separate files). Each test specifies: command, args, expected exit code, assertions (CEL expressions over output), and file dependencies.

Key features:
- **Sandbox isolation** — each test runs in a temporary directory with only declared files.
- **CEL assertions** — validate output structure and values.
- **Tags** — organize and filter tests (e.g., --tag smoke).
- **Templates** — share common config via extends and test templates (names starting with _).
- **File dependencies** — declare which files the test needs via the files list (supports paths, globs, directories).
- **Shared config (config)** — suite-level env, setup, cleanup, and files via spec.testing.config.

## Test templates and inheritance

Template cases reduce duplication across tests that share files, env vars, or other config. A template is a regular test case whose name starts with '_'. Templates are NOT executed as tests.

Rules:
- Template names must start with '_' (e.g., '_files-base', '_common-env').
- Templates are defined alongside regular cases in spec.testing.cases.
- A test inherits from templates via the 'extends' field, which must be an array: extends: [_files-base].
- Multiple templates can be listed: extends: [_files-base, _common-env] — applied left-to-right.
- Inherited fields are merged; the child test's own fields take precedence.
- Templates can extend other templates (up to 10 levels deep).
- Template cases do not need a command field — they exist only for inheritance.`,
		Examples: []string{
			"spec:\n  testing:\n    cases:\n      smoke-test:\n        description: Verify basic rendering\n        command: [render, solution]\n        exitCode: 0\n        files: [templates/]\n        assertions:\n          - expression: \"size(__output) > 0\"\n            message: Output should not be empty",
			"# Template case (not executed, used for inheritance)\nspec:\n  testing:\n    cases:\n      _files-base:\n        files:\n          - templates/.github/copilot-instructions.md.tpl\n          - templates/.github/instructions/terraform-hcl.instructions.md.tpl\n\n      resolve-defaults:\n        extends: [_files-base]\n        command: [run, resolver]\n        args: [-o, json]\n        exitCode: 0\n\n      render-defaults:\n        extends: [_files-base]\n        command: [render, solution]\n        exitCode: 0",
		},
		SeeAlso: []string{"test-sandbox", "test-assertions", "test-scaffold"},
	},
	{
		Name:     "test-sandbox",
		Title:    "Test Sandbox",
		Category: "testing",
		Summary:  "Isolated temporary directory where each test case executes.",
		Explanation: `Each test runs in its own sandbox — a temporary directory containing only:

1. The solution file itself.
2. Compose files referenced by the solution.
3. Bundle files.
4. Test-specific files declared in the test's 'files' list.

The files list supports three entry types:
- **Plain paths**: 'templates/main.yaml' — copies a single file.
- **Globs**: 'templates/**/*.yaml' — copies all matching files (uses doublestar).
- **Directories**: 'templates/' or 'templates' — recursively copies all files.

Files not declared in the list are NOT available in the sandbox. If a test fails with "file not found", check that the file is listed in the test's files array.`,
		SeeAlso: []string{"functional-testing", "test-scaffold"},
	},
	{
		Name:     "test-scaffold",
		Title:    "Test Scaffold",
		Category: "testing",
		Summary:  "Auto-generate starter test cases from a solution's structure.",
		Explanation: `The scaffold generator analyzes a solution's resolvers, validation rules, and workflow actions to produce starter test cases. It generates:

- A smoke test for resolver resolution with defaults.
- A smoke test for solution rendering.
- A lint test.
- Per-resolver output tests with basic assertions.
- Validation failure tests for resolvers with validation rules.
- Per-action execution tests.

The scaffold also auto-populates the 'files' list based on static analysis of provider inputs (e.g., file provider paths, template references).

Use the CLI test init command or 'generate_test_scaffold' (MCP) to generate.`,
		SeeAlso: []string{"functional-testing", "test-sandbox"},
	},
	{
		Name:     "test-assertions",
		Title:    "Test Assertions",
		Category: "testing",
		Summary:  "CEL expressions that validate test output.",
		Explanation: `Each test case can include assertions — CEL expressions evaluated against the test's output. The special variable __output contains the parsed command output (typically JSON or YAML).

Assertions have two fields:
- **expression** — a CEL expression that must evaluate to true.
- **message** — a human-readable failure message.

For commands that output JSON (e.g., -o json), __output is the parsed object. For plain text output, __output is the raw string.`,
		Examples: []string{
			"assertions:\n  - expression: \"__output.region == 'us-east-1'\"\n    message: Region should default to us-east-1\n  - expression: \"size(__output.items) > 0\"\n    message: Should have at least one item",
		},
		SeeAlso: []string{"functional-testing", "cel-expression"},
	},
	// --- Composition ---
	{
		Name:     "compose",
		Title:    "Solution Composition",
		Category: "structure",
		Summary:  "Merge partial YAML files into a solution using the compose field.",
		Explanation: `The top-level 'compose' field lists relative paths to partial YAML files that are deep-merged into the solution at load time. This enables splitting large solutions into logical modules:

- Separate resolver definitions from workflow actions.
- Keep test cases in their own file.
- Share common configurations across solutions.

Compose files are merged in order — later files override earlier ones for conflicting keys. Array fields are replaced, not concatenated.`,
		Examples: []string{
			"# solution.yaml\napiVersion: scafctl.io/v1\ncompose:\n  - resolvers.yaml\n  - actions.yaml\n  - tests.yaml\nmetadata:\n  name: my-solution",
		},
		SeeAlso: []string{"bundle"},
	},
	{
		Name:     "bundle",
		Title:    "Bundling",
		Category: "structure",
		Summary:  "Package a solution and its file dependencies for catalog publishing.",
		Explanation: `Bundling creates a self-contained archive of a solution and all files it needs. The bundle.include field specifies which files to include (supports globs).

scafctl automatically discovers file dependencies through static analysis of provider inputs (e.g., file paths in the file provider, template references). Use 'bundle.include' to explicitly add files that can't be detected statically.

The lint rule 'unbundled-test-file' warns when test files aren't covered by bundle.include patterns.`,
		Examples: []string{
			"bundle:\n  include:\n    - templates/**\n    - configs/*.yaml\n    - tests/**",
		},
		SeeAlso: []string{"compose", "catalog"},
	},
	// --- DAG ---
	{
		Name:     "dag",
		Title:    "Directed Acyclic Graph (DAG)",
		Category: "architecture",
		Summary:  "The dependency graph model used for resolver and action execution ordering.",
		Explanation: `scafctl uses DAGs (Directed Acyclic Graphs) to determine execution order for both resolvers and actions:

- **Resolver DAG** — built from dependsOn declarations and implicit CEL references (_.resolverName). Resolvers with no dependencies execute first, in parallel.
- **Action DAG** — built from dependsOn declarations on actions. Independent actions execute in parallel.

Circular dependencies are detected at lint time (workflow-validation rule) and rejected. Use 'render_solution' with graph_type 'resolver-deps' or 'action-deps' to visualize the DAG.`,
		SeeAlso: []string{"depends-on", "resolver", "action"},
	},
	// --- Catalog ---
	{
		Name:     "catalog",
		Title:    "Solution Catalog",
		Category: "catalog",
		Summary:  "A registry for publishing, discovering, and consuming reusable solutions.",
		Explanation: `The catalog is a centralized registry of solutions that can be searched, installed, and referenced. Solutions are published as bundles with metadata (name, version, description, tags).

Key operations:
- **catalog search** — find solutions by name, tag, or description.
- **catalog install** — install a solution locally.
- **solution provider** — reference a catalog solution as a nested dependency (source: 'solution-name@version').

The catalog supports versioning, visibility controls (public/private), and beta flags.`,
		SeeAlso: []string{"bundle", "compose"},
	},
}
