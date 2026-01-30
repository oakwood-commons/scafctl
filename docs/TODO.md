# TODO - scafctl Implementation Roadmap

This document tracks remaining implementation tasks for scafctl.

## Parameter Provider CLI Integration

- [ ] **Parse `-r`/`--resolver` flags in CLI root command**
  - Add persistent flag to cobra root command
  - Support multiple `-r key=value` flags
  - Parse key=value format

- [ ] **Handle multiple values for same parameter**
  - Merge multiple `-r items=a -r items=b` into array `["a", "b"]`
  - Store in map[string]any with array values

- [ ] **Stdin handling for parameters**
  - Detect when any `-r key=-` flag is present
  - Read stdin once at CLI initialization
  - Store content in parameters map
  - Warn if multiple parameters reference stdin

- [ ] **Store parameters in context**
  - Call `provider.WithParameters(ctx, params)` in CLI initialization
  - Pass context through command execution chain

- [ ] **Pass context to provider execution**
  - Ensure context flows through solution execution
  - Verify resolver execution receives parameter context

## CLI Commands

### Core Commands (from design/cli.md)

- [ ] **`scafctl run solution <name[@version]>`**
  - Implement catalog-first lookup strategy
  - Support local file resolution with `-f` flag
  - Support `-r` parameter passing
  - Execute actions with side effects

- [ ] **`scafctl render solution <name[@version]>`**
  - Implement `--mode resolved` (default) - output fully resolved solution
  - Implement `--mode actions` - output action graph only
  - Support `-o` output formats (yaml, json, text)
  - Support `-r` parameter passing

- [ ] **`scafctl get <kind>`**
  - List resources from catalog
  - Support output formats: table, json, yaml
  - Support filtering with `-o jsonpath=...`

- [ ] **`scafctl explain <kind> <name>`**
  - Display documentation for providers, solutions
  - Format output for human readability

- [ ] **`scafctl publish <kind> <name[@version]>`**
  - Validate artifact before publishing
  - Package and upload to catalog
  - Handle authentication

- [ ] **`scafctl cache` subcommands**
  - `cache clear` - clear all cached artifacts
  - `cache clear --kind <kind>` - clear specific kind
  - `cache clear --name <name>` - clear specific artifact

## File Resolution

- [ ] **Local solution file lookup**
  - Search patterns: `./solution.{yaml,json}`, `./scafctl/solution.{yaml,json}`, `./.scafctl/solution.{yaml,json}`
  - Support both YAML and JSON formats
  - Error handling for missing files

- [ ] **Catalog-first resolution**
  - Query catalog for named artifacts
  - Apply version constraints
  - Download and cache artifacts
  - Fallback behavior when catalog unavailable

## Catalog Integration

- [ ] **Configuration file support**
  - Parse `~/.scafctl/config.yaml`
  - Support multiple catalog definitions
  - Handle active catalog selection

- [ ] **Environment variable support**
  - `SCAFCTL_CATALOG_URL`
  - `SCAFCTL_CATALOG_INSECURE`
  - `SCAFCTL_CACHE_DIR`
  - `SCAFCTL_CACHE_TTL`
  - `SCAFCTL_NO_CACHE`

- [ ] **Command-line catalog override**
  - `--catalog <url>` flag support
  - Override config and env vars

- [ ] **OCI authentication**
  - Read credentials from `~/.docker/config.json`
  - Support docker credential helpers
  - Handle authentication errors

- [ ] **Cache management**
  - Implement cache structure: `~/.scafctl/cache/{kind}/{name@version}.{digest}.tar.gz`
  - Respect TTL configuration (default 24h)
  - `--no-cache` flag support

## Output Formatting

- [ ] **Format flag implementation (`-o`/`--output`)**
  - `text` - human-readable (default for run/render)
  - `yaml` - YAML format
  - `json` - JSON format
  - `table` - formatted table (default for get/list)
  - `jsonpath` - JSONPath query support

- [ ] **Output mode for render command**
  - `--mode resolved` - fully resolved solution
  - `--mode actions` - action graph only

## Interactive Features

- [ ] **Progress indicators**
  - Show spinners for long-running operations
  - Display progress bars for downloads
  - Detect TTY vs non-TTY output

- [ ] **Confirmation prompts**
  - Implement `--yes`/`-y` flag to skip prompts
  - Add prompts for destructive operations

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

- [ ] **Resolver execution**
  - Implement resolver resolution with fallback chains
  - Support `from`, `transform`, `validation` providers
  - Handle resolver context
  - Cache resolved values

- [ ] **Action execution**
  - Build action DAG from dependencies
  - Execute actions in dependency order
  - Collect action results
  - Handle action failures

- [ ] **CEL expression evaluation**
  - Evaluate expressions in resolver/action inputs
  - Access resolver context: `${ resolvers.env }`
  - Access action results: `${ actions.deploy.result }`

- [ ] **Template rendering**
  - Render Go templates in inputs
  - Access resolver context
  - Access action results

## Built-in Providers

### Static Provider (needed for fallbacks)
- [ ] **Implement static provider**
  - Return hardcoded values
  - Support all data types
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

## Testing

- [ ] **Integration tests**
  - End-to-end CLI command tests
  - Solution execution tests
  - Catalog integration tests

- [ ] **Example solutions**
  - Create example solutions in `examples/`
  - Document common patterns
  - Use in integration tests

## Documentation

- [ ] **User guide**
  - Getting started tutorial
  - Solution authoring guide
  - Provider reference documentation

- [ ] **Developer guide**
  - Plugin development guide
  - Provider development guide
  - Contributing guidelines

## Error Handling

- [ ] **Exit codes**
  - Standardize exit codes (0=success, 1=general, 2=validation, etc.)
  - Document exit codes

- [ ] **Error messages**
  - Consistent error formatting
  - Helpful error messages with suggestions
  - Secret redaction in errors

## Performance

- [ ] **Resolver caching**
  - Cache resolved values within execution
  - Avoid re-resolving same resolver

- [ ] **Parallel execution**
  - Execute independent actions in parallel
  - Respect dependency ordering

## Security

- [ ] **Secret handling**
  - Mark sensitive inputs with `isSecret`
  - Redact secrets in logs and errors
  - Secure credential storage

- [ ] **Input validation**
  - Validate all provider inputs against schema
  - Prevent injection attacks
  - Sanitize file paths

## Completed

- [x] Parameter provider implementation
- [x] Provider context helpers (WithParameters, ParametersFromContext)
- [x] Parameter parsing precedence rules
- [x] Provider registry
- [x] Provider executor
- [x] Built-in providers (env, http, file, exec, git, cel, debug, sleep, validation, go-template)
- [x] Provider schema validation
- [x] Dry-run support
- [x] Basic CLI structure with cobra
