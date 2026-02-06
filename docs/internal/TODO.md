# TODO - scafctl Implementation Roadmap

This document tracks remaining implementation tasks for scafctl.

## Parameter Provider CLI Integration

- [x] **Parse `-r`/`--resolver` flags in CLI root command**
  - Add persistent flag to cobra root command
  - Support multiple `-r key=value` flags
  - Parse key=value format

- [x] **Handle multiple values for same parameter**
  - Merge multiple `-r items=a -r items=b` into array `["a", "b"]`
  - Store in map[string]any with array values

- [x] **Stdin handling for parameters**
  - Detect when any `-r key=-` flag is present
  - Read stdin once at CLI initialization
  - Store content in parameters map
  - Warn if multiple parameters reference stdin

- [x] **Store parameters in context**
  - Call `provider.WithParameters(ctx, params)` in CLI initialization
  - Pass context through command execution chain

- [x] **Pass context to provider execution**
  - Ensure context flows through solution execution
  - Verify resolver execution receives parameter context

## CLI Commands

### Secrets Commands

- [x] **`scafctl secrets set <name> [value]`**
  - Set secret via argument, flag, file, or stdin
  - Support `--value`, `--file` flags
  - Encrypt with master key

- [x] **`scafctl secrets get <name>`**
  - Retrieve and decrypt secret
  - Support `-o` output formats

- [x] **`scafctl secrets list`**
  - List all secrets (names only)
  - Support `-i`/`--interactive` mode
  - Support `-o` output formats

- [x] **`scafctl secrets delete <name>`**
  - Remove secret from store

- [x] **`scafctl secrets exists <name>`**
  - Check if secret exists

- [x] **`scafctl secrets export`**
  - Export secrets to file

- [x] **`scafctl secrets import`**
  - Import secrets from file

- [x] **`scafctl secrets rotate`**
  - Rotate master encryption key
  - Re-encrypt all secrets with new key

### Auth Commands

- [x] **`scafctl auth login [handler]`**
  - Authenticate with auth handler (entra, etc.)
  - Support interactive login
  - Cache credentials securely

- [x] **`scafctl auth logout [handler]`**
  - Clear cached credentials
  - Support specific handler or all

- [x] **`scafctl auth status [handler]`**
  - Show authentication status
  - Display token claims and expiry
  - Support `-o` output formats

- [x] **`scafctl auth token [handler]`**
  - Get access token for handler
  - Support `--resource` flag for scopes

### Core Commands (from design/cli.md)

- [x] **`scafctl run solution <name[@version]>`**
  - Support local file resolution with `-f` flag
  - Support `-r` parameter passing
  - Execute actions with side effects
  - Support `--dry-run`, `--skip-actions`, `--progress`, `-i`/`--interactive`
  - Note: Catalog-first lookup pending catalog implementation

- [x] **`scafctl render solution <name[@version]>`**
  - Render action graph (default mode)
  - Support `--graph` for dependency visualization
  - Support `--snapshot` for execution snapshots
  - Support `-o` output formats (yaml, json)
  - Support `-r` parameter passing
  - Support `--redact` for sensitive value redaction

- [x] **`scafctl get <kind>`**
  - Subcommands: solution, provider, resolver
  - Support output formats: table, json, yaml
  - Support `-i`/`--interactive` and `-e`/`--expression` filtering

- [x] **`scafctl explain <kind> <name>`**
  - Display documentation for providers, solutions
  - Format output for human readability

- [x] **`scafctl resolver graph`**
  - Visualize resolver dependency graph
  - Support output formats (ascii, dot, mermaid, json)

- [ ] **`scafctl delete solution <name[@version]>`**
  - Remove solution from catalog
  - Support `--catalog` flag for target

- [ ] **`scafctl build [solution|plugin]`**
  - Validate artifact schema and structure
  - Resolve and fetch remote dependencies
  - Verify dependency compatibility
  - Detect circular dependencies
  - Package as OCI artifact
  - Store in local catalog with annotations

- [ ] **`scafctl push [solution|plugin] <name[@version]>`**
  - Push artifact from local to remote catalog
  - Handle OCI authentication
  - Support `--catalog` flag for target

- [ ] **`scafctl pull [solution|plugin] <name[@version]>`**
  - Download artifact from remote to local catalog
  - Cache locally for offline use

- [ ] **`scafctl inspect [solution|plugin] <name[@version]>`**
  - Display artifact metadata and dependencies
  - Show available providers (for plugins)
  - Show platform requirements

- [ ] **`scafctl tag [solution|plugin] <source> <target>`**
  - Create version aliases (e.g., latest, stable)

- [ ] **`scafctl save [solution|plugin] <name[@version]> -o <file>`**
  - Export artifact and dependencies as tar archive
  - Support air-gapped environments

- [ ] **`scafctl load -i <file>`**
  - Import artifact from tar archive

- [ ] **`scafctl cache` subcommands**
  - `cache clear` - clear all cached artifacts
  - `cache clear --kind <kind>` - clear specific kind
  - `cache clear --name <name>` - clear specific artifact

## File Resolution

- [x] **Local solution file lookup**
  - Search patterns: `./solution.{yaml,json}`, `./scafctl/solution.{yaml,json}`, `./.scafctl/solution.{yaml,json}`
  - Support both YAML and JSON formats
  - Implemented via `FindSolution()` in `pkg/solution/get/get.go`

- [ ] **Catalog-first resolution**
  - Query catalog for named artifacts
  - Apply version constraints
  - Download and cache artifacts
  - Fallback behavior when catalog unavailable

## Catalog Integration

- [ ] **Local catalog implementation**
  - OCI content store at `~/.scafctl/catalog/`
  - Store solutions and plugins as OCI artifacts
  - Support content-addressed blobs and index

- [ ] **Remote catalog support**
  - Standard OCI registry integration
  - Support private registries
  - GUI frontend discovery integration

- [x] **Configuration file support**
  - Parse `~/.scafctl/config.yaml`
  - Support multiple catalog definitions
  - Handle active catalog selection
  - Commands: `config init`, `config view`, `config show`, `config get`, `config set`, `config unset`, `config validate`, `config add-catalog`, `config remove-catalog`, `config use-catalog`, `config schema`

- [x] **Environment variable support**
  - `SCAFCTL_` prefix for all config values
  - Automatic env var binding via Viper

- [ ] **Command-line catalog override**
  - `--catalog <url>` flag support
  - Override config and env vars

- [ ] **OCI authentication**
  - Read credentials from `~/.docker/config.json`
  - Support docker credential helpers
  - Handle authentication errors

- [ ] **Artifact identification**
  - Media types: `application/vnd.scafctl.solution.v1+yaml`, `application/vnd.scafctl.plugin.v1+binary`
  - OCI annotations for artifact metadata
  - Repository structure for solutions and plugins

- [ ] **Dependency resolution**
  - Recursive dependency resolution during build
  - Circular dependency detection
  - Version constraint validation
  - Cache resolved dependencies

- [ ] **Multi-platform support**
  - OCI image index for platform-specific plugins
  - Platform detection at runtime
  - Cross-platform build support

- [ ] **Cache management**
  - Implement cache structure: `~/.scafctl/cache/{kind}/{name@version}.{digest}.tar.gz`
  - Respect TTL configuration (default 24h)
  - `--no-cache` flag support

## Output Formatting

- [x] **Format flag implementation (`-o`/`--output`)**
  - `yaml` - YAML format
  - `json` - JSON format
  - `table` - formatted table (default for get/list)
  - `quiet` - minimal output
  - CEL expression filtering via `-e`/`--expression`

- [x] **Output modes for render command**
  - Default: action graph rendering
  - `--graph`: dependency graph visualization (ascii, dot, mermaid, json)
  - `--snapshot`: execution state snapshot

## Interactive Features

- [x] **Progress indicators**
  - Progress bars via `--progress` flag (uses mpb)
  - TTY detection for appropriate output

- [x] **Interactive TUI**
  - `-i`/`--interactive` flag for kvx-based data exploration
  - Search, filter, and navigate results

- [x] **Confirmation prompts**
  - `--force`/`-f` flag to skip prompts (used in `secrets delete`)
  - `pkg/terminal/input/` provides `ConfirmOptions` infrastructure
  - Respects `--quiet` mode for non-interactive environments

## Dynamic Kind Registration

- [ ] **Plugin discovery mechanism**
  - Define plugin interface for registering kinds
  - Load plugins at startup
  - Discover available kinds dynamically

- [ ] **Help text generation**
  - Dynamically generate help for registered kinds
  - Show available verbs per kind
  - Show available kinds per verb

## Solution Execution

- [x] **Resolver execution**
  - Resolver resolution with fallback chains
  - Support `from`, `transform`, `validation` providers
  - Resolver context (`_` map)
  - Parallel execution within phases

- [x] **Action execution**
  - Build action DAG from dependencies
  - Execute actions in dependency order (phases)
  - Collect action results (`__actions` namespace)
  - Handle action failures with retry, timeout, finally blocks

- [x] **CEL expression evaluation**
  - Evaluate expressions in resolver/action inputs
  - Access resolver context: `${ _.resolverName }`
  - Access action results: `${ __actions.actionName.result }`
  - Deferred expression support for action references

- [x] **Template rendering**
  - Render Go templates in inputs
  - Access resolver context
  - Access action results

## Built-in Providers

### Static Provider (needed for fallbacks)

- [x] **Implement static provider**
  - Return hardcoded values
  - Support all data types (string, number, boolean, object, array)
  - Use for default values in resolver chains

### Validation Provider

- [x] Already implemented

### Other Providers

- [x] env - environment variables
- [x] http - HTTP requests  
- [x] file - file operations
- [x] exec - command execution
- [x] git - git operations
- [x] cel - CEL expressions
- [x] debug - debugging output
- [x] sleep - delays
- [x] parameter - CLI parameters
- [x] go-template - Go text/template rendering
- [x] secret - secure secret store access
- [x] identity - authentication/identity provider

## Testing

- [x] **Integration tests**
  - End-to-end CLI command tests (`tests/integration/cli_test.go`)
  - Solution execution tests
  - 42 tests covering version, help, providers, run, render, graph, config, secrets, auth, lint, error handling

- [x] **Example solutions**
  - Created 20+ example solutions in `examples/`
  - Covers resolvers, actions, workflows, transforms
  - Includes comprehensive, terraform, email-notifier examples

## Documentation

- [x] **User guide**
  - Getting started tutorial (`docs/tutorials/getting-started.md`)
  - Provider reference documentation (`docs/tutorials/provider-reference.md`)
  - Resolver tutorial (`docs/tutorials/resolver-tutorial.md`)
  - Actions tutorial (`docs/tutorials/actions-tutorial.md`)
  - Auth tutorial (`docs/tutorials/auth-tutorial.md`)

- [x] **Developer guide**
  - Plugin development guide (`docs/tutorials/plugin-development.md`)
  - Provider development guide (`docs/tutorials/provider-development.md`)
  - Contributing guidelines (`CONTRIBUTING.md`)

## Error Handling

- [x] **Exit codes**
  - Standardized in `pkg/exitcode/exitcode.go`
  - 0=success, 1=general, 2=validation, 3=invalid input, 4=file not found, 5=render failed, 6=action failed, 7=config error, 8=catalog error, 9=timeout, 10=permission denied

- [x] **Error messages**
  - Consistent error formatting
  - Secret redaction via `RedactedError` type
  - Helpful error messages with context

## Performance

- [x] **Parallel execution**
  - Resolvers execute in parallel within phases
  - Actions execute in parallel within phases
  - Configurable `maxConcurrency` for both resolvers and actions
  - Respects dependency ordering via DAG phases

- [x] **Prometheus metrics**
  - Implemented in `pkg/metrics/metrics.go`
  - Resolver execution metrics (duration, success/failure counts)
  - Provider execution metrics
  - Action execution metrics

- [ ] **Artifact caching**
  - Cache downloaded catalog artifacts
  - Respect TTL configuration

## Security

- [x] **Secret handling**
  - Mark sensitive resolvers/actions with `sensitive: true`
  - Redact secrets in logs and errors
  - `--redact` flag for snapshot output

- [x] **Input validation**
  - Validate all provider inputs against schema
  - Schema-based validation with Huma tags

## Completed

- [x] Parameter provider implementation
- [x] Provider context helpers (WithParameters, ParametersFromContext)
- [x] Parameter parsing precedence rules
- [x] Provider registry
- [x] Provider executor
- [x] Built-in providers (env, http, file, exec, git, cel, debug, sleep, validation, go-template, static, parameter, secret, identity)
- [x] Provider schema validation
- [x] Dry-run support
- [x] Basic CLI structure with cobra
- [x] `scafctl run solution` command with full feature set
- [x] `scafctl render solution` command with graph/snapshot modes
- [x] `scafctl get` command (solution, provider, resolver)
- [x] `scafctl explain` command (solution, provider)
- [x] `scafctl snapshot` commands (show, diff)
- [x] `scafctl config` commands (init, view, show, get, set, unset, validate, add-catalog, remove-catalog, use-catalog, schema)
- [x] `scafctl secrets` commands (set, get, list, delete, exists, export, import, rotate)
- [x] `scafctl auth` commands (login, logout, status, token)
- [x] `scafctl resolver graph` command
- [x] Configuration file support (~/.scafctl/config.yaml)
- [x] Environment variable support (SCAFCTL_ prefix)
- [x] Output formatting (-o json, yaml, table, quiet)
- [x] Interactive TUI mode (-i/--interactive)
- [x] CEL expression filtering (-e/--expression)
- [x] Progress indicators (--progress)
- [x] Exit codes (pkg/exitcode)
- [x] Secret redaction (sensitive field, --redact flag)
- [x] Parallel execution (resolvers and actions)
- [x] Action execution with DAG, retry, timeout, finally
- [x] Resolver execution with phases and fallback chains
- [x] CEL expression evaluation
- [x] Go template rendering
- [x] kvx integration for structured output
- [x] Prometheus metrics (pkg/metrics)
- [x] Example solutions (20+ in examples/)
- [x] `scafctl lint` command with severity filtering and multiple output formats

## Future Enhancements

These are planned features from design docs for future implementation:

### Linting

- [x] **`scafctl lint`**
  - Detect unused resolvers
  - Detect unreachable actions
  - Identify anti-patterns
  - Validate solution structure beyond schema
  - Implementation: `pkg/cmd/scafctl/lint/lint.go`
  - Supports `-o` formats: table, json, yaml, quiet
  - Supports `--severity` filter: error, warning, info
  - Rules: unused-resolver, invalid-dependency, missing-provider, invalid-expression, invalid-template, empty-workflow, finally-with-foreach, missing-description, long-timeout

### Action Graph Visualization

- [x] **Action DAG visualization (ASCII/DOT/Mermaid)**
  - Implemented `--action-graph` flag for `scafctl render solution`
  - Supports formats: ascii (default), dot, mermaid, json via `--graph-format`
  - Shows execution phases, dependencies, finally blocks, and forEach expansions
  - Implementation: `pkg/action/graph_visualization.go`
  - Tests: `pkg/action/graph_visualization_test.go`, integration tests in `tests/integration/cli_test.go`

### Result Schema Validation

- [x] **Action result schema validation**
  - Uses `github.com/google/jsonschema-go/jsonschema.Schema` for full JSON Schema 2020-12 support
  - Optional `resultSchema` field on actions accepts standard JSON Schema
  - Validate provider output matches expected shape via `Resolved.Validate()`
  - Full JSON Schema features: `$ref`, `allOf`, `anyOf`, `oneOf`, `if/then/else`, etc.
  - Lint rules for schema validation
  - Implementation: `pkg/action/result_validation.go`, `pkg/action/types.go`
  - Tests: `pkg/action/result_validation_test.go`
  - Example: `examples/actions/result-schema-validation.yaml`

### Migrate Provider Schemas to JSON Schema

- [ ] **Replace custom SchemaDefinition with jsonschema.Schema**
  - Migrate `pkg/provider.SchemaDefinition` to use `jsonschema.Schema`
  - Migrate `pkg/provider.PropertyDefinition` to use `jsonschema.Schema`
  - Migrate `pkg/provider.OutputSchemas` to use `jsonschema.Schema`
  - Update all builtin providers to use JSON Schema format
  - Benefits: standard format, full JSON Schema power, single validation library
  - Breaking change: provider input/output schema format will change

### Conditional Retry

- [ ] **`retryIf` expression for actions**
  - Retry only on specific error types
  - Expression-based condition: `__error.statusCode == 429`
  - Avoid wasting retries on non-transient failures
