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
3. **validate** — optional steps that enforce constraints on the value. Validation runs in two phases: rules that reference only the owning resolver (via __self) run **inline** during resolution and fail fast; rules that reference another resolver (via _.other, in the expression, inputs, when, or message) are **deferred** and run after every resolver has resolved, just before actions. The phase is chosen automatically, and deferred references do not add edges to the resolution dependency graph — so two resolvers can validate against each other without forming a false ordering cycle.

Resolvers can depend on each other via 'dependsOn' or implicit CEL references (_.otherResolver). The dependency graph must be a DAG — circular references in resolve/transform/when are rejected at lint time (cross-resolver validation references do not count).`,
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
	{
		Name:     "call",
		Title:    "Parameterized Call",
		Category: "resolvers",
		Summary:  "A reusable, argument-driven provider request defined once under spec.calls and invoked from many resolve, transform, validate, or action steps.",
		Explanation: `A call decouples a request's shape from its inputs. Instead of duplicating a full provider configuration (URL, headers, body, auth, retry) for every distinct set of inputs, you declare it once as a definition and invoke it from many call sites.

There are two halves:

1. **Call definition** (spec.calls.<name>) — declares typed 'args', a 'provider', and provider 'inputs' that reference those arguments via the 'args' namespace: _.args.x in CEL and {{ .args.x }} in Go templates.
2. **Call site** (call: + args:) — a resolve, transform, validate, or action step that invokes a definition and supplies argument values as standard ValueRefs (literals, rslvr, expr, or tmpl).

A host step must set exactly one of 'provider' or 'call'. Arguments are typed with optional defaults and a 'required' flag; supplied values are coerced to the declared type before the definition's inputs resolve. Set 'dedup: true' on a definition to collapse identical invocations within a single run (in-memory only, never persisted). Bare 'args.x' in CEL is not supported — always use the _. prefix.`,
		Examples: []string{
			"spec:\n  calls:\n    greet:\n      provider: cel\n      args:\n        salutation:\n          type: string\n          default: Hello\n        name:\n          type: string\n          required: true\n      inputs:\n        expression: '_.args.salutation + \", \" + _.args.name + \"!\"'\n  resolvers:\n    english:\n      resolve:\n        with:\n          - call: greet\n            args:\n              name: World",
		},
		SeeAlso: []string{"resolver", "action", "provider", "cel-expression"},
	},
	// --- Providers ---
	{
		Name:     "provider",
		Title:    "Provider",
		Category: "providers",
		Summary:  "A pluggable executor that performs a specific operation (e.g., read file, call API, prompt user).",
		Explanation: `Providers are the execution engines in scafctl. Each provider has a name, a typed input schema, and produces output. Providers are referenced by name in resolver steps and action definitions.

Built-in providers (always available): cel, debug, file, go-template, http, message, parameter, solution, static, and validation. Official plugin providers (auto-fetched on first use): directory, env, exec, git, github, hcl, identity, metadata, secret, and sleep. Custom providers can be added via the plugin system.

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
			"# Template case (not executed, used for inheritance)\nspec:\n  testing:\n    cases:\n      _files-base:\n        files:\n          - templates/.github/copilot-instructions.md.tpl\n          - templates/.github/instructions/terraform-hcl.instructions.md.tpl\n\n      resolve-defaults:\n        extends: [_files-base]\n        command: [run, resolver]\n        args: [-o, json]\n        exitCode: 0\n\n      render-check:\n        extends: [_files-base]\n        command: [render, solution]\n        args: [-r, appName=demo]\n        exitCode: 0",
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
	// --- State ---
	{
		Name:     "state",
		Title:    "State Persistence",
		Category: "state",
		Summary:  "Opt-in persistence that saves and replays CLI parameters across solution runs.",
		Explanation: `State persistence lets a solution replay its inputs between executions. When enabled, the CLI parameters ('-r key=value' values) used on each run are saved and automatically replayed on the next run, so the solution produces the same resolver values without re-supplying inputs.

**Configuration** — add a top-level 'state' block to the solution:
- **enabled** — literal bool, CEL expression, or template controlling activation.
- **backend** — which provider stores the state (e.g., file, http).

**Lifecycle**:
1. Before resolvers run, the state manager calls the backend provider with 'state_load' to load any existing state, then merges the saved parameters with the current CLI parameters (CLI values win on conflict).
2. Resolvers execute normally — the parameter provider returns the merged (replayed) values.
3. After execution, the state manager calls 'state_save' to persist the merged parameter set plus the locked values of any resolvers marked 'immutable: true'.

**Immutable resolvers** — mark a resolver 'immutable: true' to lock its resolved value in state after the first run. On later runs the resolver still executes, but its value is compared against the stored value and execution fails if they differ. Use this for non-deterministic values (e.g., UUIDs) that must stay stable across runs.

**Backend providers** — file (local JSON; relative paths resolve against the solution directory), http (remote REST API). External providers (e.g., github) can be installed as plugins. Each implements CapabilityState with state_load, state_save, and state_delete operations.`,
		Examples: []string{
			"# Enable state with file backend; parameters replay automatically\nstate:\n  enabled: true\n  backend:\n    provider: file\n    inputs:\n      path: \"my-app-state.json\"\n\nspec:\n  resolvers:\n    # Locked after the first run -- value is saved and verified on later runs\n    deployment_id:\n      type: string\n      immutable: true\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              key: \"deployment_id\"\n    # Mutable -- replayed from saved parameters, overridable via -r\n    region:\n      type: string\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              key: \"region\"\n          - provider: static\n            inputs:\n              value: \"us-west-2\"",
		},
		SeeAlso: []string{"resolver", "provider", "cel-expression"},
	},
	// --- Context / authoring ---
	{
		Name:     "context-variables",
		Title:    "Context Variables",
		Category: "context",
		Summary:  "The special variables injected into CEL and Go-template evaluation (_, __self, __item, __plan, __execution, __actions, __cwd, __params, __error, and the Go-template __file* path parts) and the phase each is available in.",
		Explanation: `Beyond the CEL and template functions, scafctl injects a set of *context variables* into expression evaluation. Which variables exist depends on the phase — a variable available in an action is not necessarily available in a resolver. No function list covers these; use the 'list_context_variables' tool for the full, machine-readable matrix (optionally filtered by phase).

**CEL context variables**
- **_** — map of resolved resolver values (_.region). Available in resolve inputs/when, transform, validate, and action when/inputs. Referencing _.other also creates an implicit dependency edge.
- **__self** — the current resolver's in-progress value. Available in transform (the resolved value) and validate (the final value) only; NOT during resolve.
- **__item / __index** — the current element / zero-based index inside a forEach iteration (resolve or transform).
- **__plan** — pre-execution resolver topology injected before any resolver runs, so a resolver's when/inputs can read __plan["name"].phase, .dependsOn, and .dependencyCount.
- **__execution** — resolver execution metadata available to ACTIONS: __execution.resolvers.<name>.status/phase/duration and __execution.summary.
- **__actions** — results of completed actions, available to downstream ACTIONS: __actions.<name>.results and .status.
- **__cwd** — the original working directory, available in ACTIONS ONLY (useful when --output-dir redirects action output). Not injected into resolvers.
- **__params** — raw CLI parameters (-r key=value), available in STATE BACKEND input expressions only. Unlike _ (resolver outputs), __params always holds the raw parameters.
- **__error** — the error bound in failure contexts (continueOnError conditions and messages.error). Its shape is context-dependent: in RESOLVER contexts it is a string (__error.contains("...")); in ACTION contexts it is a structured map with message, statusCode, attempt, and maxAttempts (__error.message.contains("..."), __error.statusCode).

**Go-template availability** — most of these (_, __self, __item, __index, __actions, __cwd, __execution) are injected into BOTH CEL and Go-template evaluation, so {{ ._.region }}, {{ .__self }}, and {{ .__item }} also work. __plan, __params, and __error are CEL-only; the __file* path parts below are Go-template only.

**Go-template file-generation variables** (available in the file provider's outputPath during directory -> render-tree -> write-tree generation): __filePath, __fileName, __fileStem, __fileExtension, __fileDir -- injected without a leading dot and accessed in templates as {{ .__fileStem }} etc., used to rename or restructure output files (e.g. strip a .tpl suffix).

Decision guide: prefer _.other to reference another resolver (it also wires the dependency); use __self only to talk about the value currently being built.`,
		Examples: []string{
			"# __self in a transform, __plan in a resolver when\nresolvers:\n  name:\n    resolve:\n      with:\n        - provider: parameter\n          inputs: { name: name }\n    transform:\n      with:\n        - provider: cel\n          inputs: { expression: \"__self.trim()\" }\n  gated:\n    when: '__plan[\"name\"].phase == 1'\n    resolve:\n      with:\n        - provider: static\n          inputs: { value: ok }",
		},
		SeeAlso: []string{"cel-expression", "resolver", "action", "foreach", "go-template-provider"},
	},
	{
		Name:     "phase-execution",
		Title:    "Resolver Phase Execution",
		Category: "context",
		Summary:  "How the resolve, transform, and validate phases actually execute at runtime — ordering, fallback, and fatality — which the schema shape does not describe.",
		Explanation: `A resolver runs up to three phases with distinct runtime behavior:

**resolve** — the 'with' steps are tried in order and the FIRST step that produces a value wins and stops the chain; a step that fails is treated as a fallback and evaluation continues to the next step. An optional 'until' condition (which can read __self) can stop earlier. This makes 'with' a fallback chain: parameter first, static default last.

**transform** — steps run SEQUENTIALLY, each seeing the previous value via __self. Transform steps default to FAIL on error (a broken transform is fatal to that resolver).

**validate** — all inline rules are evaluated and their failures are AGGREGATED (you see every failure, not just the first). Validation is NON-FATAL by default: the value is still returned and dependents still run, with failures reported as diagnostics. Rules that reference only the owning resolver (via __self) run inline; rules that reference another resolver (via _.other) are deferred until after all resolvers resolve and do NOT add edges to the resolution DAG (so two resolvers can validate against each other without a false cycle).

**forEach** — 'in' is required on a resolve step but defaults to __self on a transform step; the body runs once per element with __item and __index bound.

**DAG** — resolvers are topologically sorted into phases from explicit dependsOn plus implicit CEL/template references; cycles in resolve/transform/when are rejected at lint.`,
		Examples: []string{
			"# resolve = first-success fallback chain; static default is the floor\nresolvers:\n  region:\n    resolve:\n      with:\n        - provider: parameter\n          inputs: { name: region }\n        - provider: static\n          inputs: { value: us-east-1 }",
		},
		SeeAlso: []string{"resolver", "foreach", "dag", "cel-expression"},
	},
	{
		Name:     "cel-cost-model",
		Title:    "CEL Cost Model",
		Category: "context",
		Summary:  "How scafctl bounds CEL evaluation cost, the per-solution override, and the anti-patterns that blow the budget.",
		Explanation: `Every CEL expression is evaluated under a cost limit to guard against runaway or malicious expressions. The default limit is 1,000,000 cost units. When an expression exceeds the limit, evaluation fails with a diagnostic showing the actual cost versus the limit.

**Overriding** — a solution may raise or lower its own limit via spec.options.cel.costLimit (0 = use the global default). The EFFECTIVE limit is min(solution, global): a solution can lower the ceiling but cannot exceed the operator-configured global. When global limiting is disabled (global = 0), solution overrides are ignored.

**Anti-patterns** (conceptual — cost grows with data size):
- Nested comprehensions over large lists (list.filter(...).map(...) where both are large) are roughly O(n*m); narrow the list before joining.
- Repeated re-computation of the same sub-expression; use cel.bind to compute once.

Keep expressions small and push heavy shaping into transform steps or dedicated resolvers. See 'list_cel_functions' for the available functions.`,
		Examples: []string{
			"spec:\n  options:\n    cel:\n      costLimit: 2000000\n  resolvers:\n    example:\n      resolve:\n        with:\n          - provider: cel\n            inputs: { expression: \"_.items.filter(i, i.active).size()\" }",
		},
		SeeAlso: []string{"cel-expression", "authoring-workflow"},
	},
	{
		Name:     "template-dependency-inference",
		Title:    "Go-Template Dependency Inference",
		Category: "context",
		Summary:  "How scafctl decides whether a Go-template accessor is a resolver reference, a data-input key, or a forEach alias — and why unknown accessors silently render empty.",
		Explanation: `A Go template's root namespace is the UNION of three sources:

1. Resolver values — every resolver in the solution.
2. The step's 'data' input keys — variables passed explicitly to the template.
3. forEach aliases — the item/index bindings when the step iterates.

Accessor rules:
- {{ ._.name }} is ALWAYS a resolver reference (and creates a dependency edge).
- {{ .field }} is a resolver dependency only when there is no matching 'data' key and it is not a forEach alias.
- When 'data' is present with statically-known keys, matching keys resolve to the data value; unknown keys still resolve against resolver data.
- When the data keys are not statically knowable, the whole root is treated as the data context.
- {{ .__* }} accessors are special context variables, never dependencies.

Because Go templates default to rendering a missing key as empty, a typo'd accessor silently blanks out rather than erroring. The 'template-unknown-accessor' lint rule (WARNING) flags a root accessor that is neither a resolver, a data key, nor an alias. Use 'extract_resolver_refs' to see the inferred references and 'explain_lint_rule template-unknown-accessor' for details.`,
		Examples: []string{
			"# .env comes from data; ._.region is a resolver reference\nresolve:\n  with:\n    - provider: go-template\n      inputs:\n        data: { env: prod }\n        template: \"{{ .env }}-{{ ._.region }}\"",
		},
		SeeAlso: []string{"go-template-provider", "depends-on", "resolver"},
	},
	{
		Name:     "snapshot-masking",
		Title:    "Snapshot Masking",
		Category: "context",
		Summary:  "How golden/snapshot functional tests normalize volatile output via built-in presets and custom masks so runs stay deterministic.",
		Explanation: `Snapshot tests compare a solution's output against a stored golden file. Because output often contains volatile regions (timestamps, UUIDs, sandbox paths), scafctl masks those regions before comparison.

**Built-in presets** — 'timestamp', 'uuid', and 'sandbox' are enabled by default. Additional presets ('email', 'ipv4', 'mac') are opt-in. Toggle presets in a test case with masks: [{use: <name>}], and disable a default with {use: <name>, disabled: true}.

**Custom masks** — provide {name, pattern, placeholder, path} to normalize solution-specific volatile content.

**Snapshot source** — snapshotSource: stdout (default) compares captured output; snapshotSource: files compares the rendered file tree (required when a mask is scoped with 'path').

**Updating goldens** — run tests with --update-snapshots to regenerate the stored baselines after an intentional change.

In the test report, a case shown as PASS* is a RELAXED pass: it passed but masks loosened snapshot fidelity, so a masked region was not compared literally. See 'functional-testing' and 'get_solution_schema' for the full spec.testing.cases shape.`,
		Examples: []string{
			"spec:\n  testing:\n    cases:\n      render:\n        description: renders with volatile ids masked\n        command: [run, resolver]\n        snapshotSource: files\n        masks:\n          - name: build-id\n            pattern: \"build-[0-9]+\"\n            placeholder: \"build-<ID>\"",
		},
		SeeAlso: []string{"functional-testing", "test-assertions", "test-sandbox"},
	},
	{
		Name:     "authoring-workflow",
		Title:    "Authoring Workflow",
		Category: "context",
		Summary:  "The recommended MCP tool loop for authoring a solution: scaffold -> verify shapes -> preview -> lint -> test -> run.",
		Explanation: `When authoring a solution with AI assistance, drive the scafctl MCP tools in this order rather than hand-writing YAML and guessing:

1. **Scaffold** — 'scaffold_solution' generates a valid skeleton to start from.
2. **Verify shapes ("what")** — 'get_solution_schema' and 'explain_kind' for the spec; 'get_provider_schema' and 'list_providers' for provider inputs/outputs. Never guess field or provider names.
3. **Check expressions in isolation** — 'validate_expression', 'evaluate_cel', and 'evaluate_go_template' before wiring them in.
4. **Preview** — 'preview_resolvers' (use the resolver argument to focus on one) to confirm resolved values; 'preview_action' or 'dry_run_solution' to preview the action graph without side effects.
5. **Lint** — 'lint_solution' to validate structure; 'list_lint_rules' and 'explain_lint_rule' to resolve findings.
6. **Test** — 'generate_test_scaffold' then 'run_solution_tests' for functional tests.
7. **Run** — 'get_run_command' for the exact CLI invocation.

For the runtime evaluation environment these tools operate against, see 'context-variables' and 'phase-execution'. These tools are the source of truth for schemas, functions, and providers — reference them rather than relying on static copies, which drift.`,
		SeeAlso: []string{"resolver", "provider", "functional-testing", "context-variables", "phase-execution"},
	},
}
