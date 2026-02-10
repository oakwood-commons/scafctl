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

- [x] **`scafctl delete solution <name[@version]>`**
  - Remove solution from catalog via `scafctl catalog delete`
  - Support `--version` flag for specific version
  - Implemented in `pkg/cmd/scafctl/catalog/delete.go`

- [x] **`scafctl build solution`**
  - Validate artifact schema and structure
  - Package as OCI artifact
  - Store in local catalog with annotations
  - Implemented in `pkg/cmd/scafctl/build/solution.go`
  - Note: `scafctl build plugin` not yet implemented

- [x] **`scafctl catalog push <name[@version]>`**
  - Push artifact from local to remote catalog
  - Handle OCI authentication via docker config
  - Support `--catalog` flag for target registry
  - Implemented in `pkg/cmd/scafctl/catalog/push.go`

- [x] **`scafctl catalog pull <registry/repository/kind/name[@version]>`**
  - Download artifact from remote to local catalog
  - Cache locally for offline use
  - Implemented in `pkg/cmd/scafctl/catalog/pull.go`

- [x] **`scafctl catalog inspect <name[@version]>`**
  - Display artifact metadata and annotations
  - Show digest, created time, version
  - Support `-o` output formats (table, json, yaml)
  - Implemented in `pkg/cmd/scafctl/catalog/inspect.go`

- [x] **`scafctl catalog list`**
  - List all artifacts in local catalog
  - Support `--kind` filter
  - Support `-o` output formats
  - Implemented in `pkg/cmd/scafctl/catalog/list.go`

- [x] **`scafctl catalog prune`**
  - Remove orphaned blobs from catalog storage
  - Implemented in `pkg/cmd/scafctl/catalog/prune.go`

- [x] **`scafctl catalog tag <name@version> <alias>`**
  - Create version aliases (e.g., latest, stable)
  - Auto-infers artifact kind from local catalog
  - Supports `--catalog` flag for remote registry tagging
  - Validates alias is not a semver version
  - Implemented in `pkg/cmd/scafctl/catalog/tag.go`

- [x] **`scafctl catalog save <name[@version]> -o <file>`**
  - Export artifact and dependencies as OCI Image Layout tar archive
  - Support air-gapped environments
  - Infer artifact kind automatically
  - Default to latest version if not specified
  - Implemented in `pkg/cmd/scafctl/catalog/save.go`

- [x] **`scafctl catalog load --input <file>`**
  - Import artifact from OCI Image Layout tar archive
  - Support `--force` flag to overwrite existing artifacts
  - Implemented in `pkg/cmd/scafctl/catalog/load.go`

- [x] **`scafctl cache` subcommands**
  - `cache clear` - clear all cached content
  - `cache clear --kind <kind>` - clear specific kind (http, all)
  - `cache clear --name <name>` - clear cache entries matching pattern
  - `cache info` - show cache information and size
  - Implemented in `pkg/cmd/scafctl/cache/`

## File Resolution

- [x] **Local solution file lookup**
  - Search patterns: `./solution.{yaml,json}`, `./scafctl/solution.{yaml,json}`, `./.scafctl/solution.{yaml,json}`
  - Support both YAML and JSON formats
  - Implemented via `FindSolution()` in `pkg/solution/get/get.go`

- [x] **Catalog-first resolution**
  - Query catalog for named artifacts
  - Apply version constraints (supports `name@version` syntax)
  - Fallback to file system when not found in catalog
  - Implemented via `SolutionResolver` in `pkg/catalog/resolver.go`

## Catalog Integration

- [x] **Local catalog implementation**
  - OCI content store at `~/.scafctl/catalog/` (via XDG data dir)
  - Store solutions as OCI artifacts
  - Content-addressed blobs with OCI manifest/index
  - Implemented in `pkg/catalog/local.go`
  - Uses oras-go for OCI operations

- [x] **Remote catalog support**
  - Standard OCI registry integration via `RemoteCatalog` in `pkg/catalog/remote.go`
  - Support private registries with credential helpers
  - Push/pull via `catalog push` and `catalog pull` commands
  - Copy operations between local and remote catalogs (`CopyTo`, `CopyFrom`)

- [x] **Configuration file support**
  - Parse `~/.scafctl/config.yaml`
  - Support multiple catalog definitions
  - Handle active catalog selection
  - Commands: `config init`, `config view`, `config show`, `config get`, `config set`, `config unset`, `config validate`, `config add-catalog`, `config remove-catalog`, `config use-catalog`, `config schema`

- [x] **Environment variable support**
  - `SCAFCTL_` prefix for all config values
  - Automatic env var binding via Viper

- [x] **Command-line catalog override**
  - `--catalog <url|name>` flag on push and delete commands
  - Resolution order: direct URL → config name lookup → default catalog from config
  - Implemented in `pkg/cmd/scafctl/catalog/resolve.go`

- [x] **OCI authentication**
  - Read credentials from `~/.docker/config.json` and podman auth configs
  - Support docker credential helpers (`docker-credential-*`)
  - Support `SCAFCTL_REGISTRY_USERNAME`/`SCAFCTL_REGISTRY_PASSWORD` env vars
  - Handle Docker Hub hostname normalization
  - Implemented in `pkg/catalog/auth.go`

- [x] **Artifact identification**
  - Media types: `application/vnd.scafctl.solution.v1+yaml`, `application/vnd.scafctl.provider.v1+binary`, `application/vnd.scafctl.auth-handler.v1+binary`
  - OCI annotations for artifact metadata (`pkg/catalog/annotations.go`)
  - Repository structure: `<registry>/<repo>/<kind-plural>/<name>` (e.g., `ghcr.io/myorg/scafctl/solutions/my-solution`)
  - Implemented in `pkg/catalog/media_types.go`, `pkg/catalog/annotations.go`, `pkg/catalog/reference.go`

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
  - Cache tutorial (`docs/tutorials/cache-tutorial.md`)

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

- [x] `scafctl catalog tag` — create version aliases for catalog artifacts
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
- [x] `scafctl build solution` command with OCI packaging
- [x] Local catalog implementation (pkg/catalog) with OCI storage
- [x] Catalog-first resolution via SolutionResolver
- [x] `scafctl catalog` commands (list, inspect, delete, prune)
- [x] `scafctl cache` commands (clear, info) with kind and name filters
- [x] Remote catalog support (pkg/catalog/remote.go) with push/pull
- [x] OCI authentication via docker/podman credential stores (pkg/catalog/auth.go)
- [x] `--catalog` flag with URL/name/default resolution (pkg/cmd/scafctl/catalog/resolve.go)
- [x] Artifact media types for solution, provider, and auth-handler kinds
- [x] `scafctl catalog push` and `scafctl catalog pull` commands
- [x] `ArtifactKindProvider` and `ArtifactKindAuthHandler` artifact kinds

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

- [x] **Replace custom SchemaDefinition with jsonschema.Schema**
  - Migrated `pkg/provider.SchemaDefinition` to use `jsonschema.Schema`
  - Migrated `pkg/provider.PropertyDefinition` to use `jsonschema.Schema`
  - Migrated `pkg/provider.OutputSchemas` to use `jsonschema.Schema`
  - Updated all builtin providers to use JSON Schema format
  - Added `pkg/provider/schemahelper` package for ergonomic schema construction

### ~~Conditional Retry~~ ✅

- [x] **`retryIf` expression for actions**
  - Retry only on specific error types
  - Expression-based condition: `__error.statusCode == 429`
  - Avoid wasting retries on non-transient failures
  - **Implemented:** `retryIf` field in `RetryConfig`, `__error` context with message, type, statusCode, exitCode, attempt, maxAttempts
