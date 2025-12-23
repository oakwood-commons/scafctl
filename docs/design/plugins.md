# Provider Plugin System

> **Goal:** Document how scafctl discovers, loads, and communicates with external provider executables modeled after the Terraform provider protocol.

## Architecture Overview

- Providers are compiled as standalone executables that implement the Terraform plugin gRPC transport (handshake + gRPC framing).
- scafctl acts as the host: it spawns provider binaries, performs the Terraform-style handshake, and manages RPC traffic through a thin adapter layered with scafctl semantics.
- Each provider declares its interface through a JSON descriptor co-located with the binary.
- Resolvers and actions reference providers by `<namespace>/<name>`; scafctl resolves that reference to a cached binary + descriptor before execution.

## Design Choices

- **Terraform transport, scafctl semantics** — We intentionally reuse the Terraform plugin handshake to avoid re-inventing process lifecycle, logging, and RPC framing. Above that transport we expose scafctl-native concepts (resolvers, templates, actions) instead of Terraform resources. See `docs/design/providers.md` for the input-contract shape.
- **Taskfile replacement** — Providers may implement iterative action graphs (e.g., `foreach`, conditional execution) so scafctl solutions can drive build/test flows the way `Taskfile.yml` would, without Terraform's plan/apply lifecycle. Related design notes live in `actions.md` and `templates.md`.
- **Deterministic discovery** — Resolver providers run once per execution graph and must be side-effect free, aligning with the pipeline described in `resolvers.md`. Actions are opt-in side effects declared in solutions (`solution.md`).
- **Scripting vs state management** — By avoiding Terraform resource semantics we keep executions stateless and deterministic, matching configuration discovery + scaffolding use cases rather than infrastructure reconciliation.
- **More background** — Rationale for the plugin vs. provider separation and catalog packaging is captured in `catalog.md` (distribution model) and `notes.md` (decision history).
- **CLI ergonomics** — Day-to-day usage stays centered on `scafctl run solution:<id>`. For ad-hoc provider testing we support the sugar `scafctl run provider:<namespace/name>` which dispatches through the same resolver/action engine, while administrative verbs remain grouped under `scafctl provider <subcommand>` (install, list, update).

## Cache & Layout

Providers live under the scafctl cache directory (default `~/.scafctl/providers`). The folder layout mirrors Terraform:

```
~/.scafctl/providers/
  registry.example.com/
    scafctl/
      shell/
        1.0.0/
          provider.json
          terraform-provider-shell
      api/
        1.0.0/
          provider.json
          terraform-provider-api
```

- `provider.json` contains descriptor metadata (name, version, schema, capabilities).
- Binary names follow Terraform conventions (`terraform-provider-<name>`), allowing reuse of existing Terraform tooling.
- Versions are semver; multiple versions can coexist.

## Installation Flow

1. **Discovery** — `scafctl provider install scafctl/shell` fetches metadata from a registry endpoint (compatible with Terraform provider registry API).
2. **Download** — The binary and `provider.json` are downloaded to the cache directory.
3. **Verification** — Checksums/signatures (if provided) are verified before caching.
4. **Registration** — scafctl updates its internal registry map with the provider descriptor from `provider.json`.

## Runtime Handshake

1. scafctl resolves a provider reference in a solution.
2. Host reads `provider.json` to confirm API compatibility (matching protocol version and features).
3. Host spawns the provider executable with Terraform handshake environment variables (`TF_REATTACH_PROVIDERS`, plugin protocol version).
4. The provider responds with capabilities; scafctl maps Terraform RPC methods to scafctl provider operations (resolve, execute).
5. Inputs validated via JSON schema from the descriptor are sent through the RPC channel.

## Descriptor Format (`provider.json`)

```json
{
  "name": "shell",
  "namespace": "scafctl",
  "version": "1.0.0",
  "protocol_version": 6,
  "schema": {
    "$schema": "http://json-schema.org/draft-07/schema#",
    "type": "object",
    "properties": {
      "dir": { "type": "string" },
      "cmd": {
        "type": "array",
        "items": { "type": "string" }
      },
      "env": {
        "type": "array",
        "items": { "type": "string" }
      }
    },
    "required": ["cmd"],
    "additionalProperties": false
  },
  "capabilities": {
    "resolvers": true,
    "actions": true,
    "auth": false
  }
}
```

- `protocol_version` must match the Terraform plugin handshake utilized by scafctl.
- `schema` defines the inputs accepted by resolver/action calls.
- `capabilities` toggles which scafctl phases can call the provider.
  - `resolvers`: Provider can be used in resolver `from:` sources
  - `actions`: Provider can be used in action execution
  - `auth`: Provider can handle authentication/authorization

## Example: Shell Provider Plugin

**Folder** `~/.scafctl/providers/registry.example.com/scafctl/shell/1.0.0/`

- `terraform-provider-shell` implements Terraform RPC.
- Executes shell commands for actions and resolves command output when used in resolvers.
- Uses the descriptor JSON above.

**Solution usage**

```yaml
spec:
  actions:
    run-tests:
      provider: scafctl/shell
      inputs:
        dir: {{ _.projectRoot }}
        cmd:
          - go test ./...
```

At runtime scafctl marshals `inputs` into the Terraform RPC request for the shell provider, ensuring validation via the descriptor.

## Example: API Provider Plugin

**Folder** `~/.scafctl/providers/registry.example.com/scafctl/api/1.0.0/`

Descriptor (simplified):

```json
{
  "name": "api",
  "namespace": "scafctl",
  "version": "1.0.0",
  "protocol_version": 6,
  "schema": {
    "$schema": "http://json-schema.org/draft-07/schema#",
    "type": "object",
    "properties": {
      "endpoint": { "type": "string", "format": "uri" },
      "method": {
        "type": "string",
        "enum": ["GET", "POST", "PUT", "PATCH", "DELETE"]
      },
      "headers": {
        "type": "object",
        "additionalProperties": { "type": "string" }
      },
      "body": { "type": "string" }
    },
    "required": ["endpoint", "method"],
    "additionalProperties": false
  },
  "capabilities": {
    "resolvers": false,
    "actions": true,
    "auth": false
  }
}
```

**Solution usage**

```yaml
spec:
  actions:
    deploy:
      provider: scafctl/api
      inputs:
        endpoint: https://api.example.com/deploy
        method: POST
        headers:
          Authorization: "Bearer {{ _.token }}"
        body: '{"version": "{{ _.version }}"}'
```

The API provider emits HTTP requests and returns status metadata which scafctl records in action outputs.

## Example: Auth Provider Plugin

Auth providers handle authentication workflows (login, logout, token refresh). They must set `"auth": true` in capabilities.

**Descriptor** `~/.scafctl/providers/registry.example.com/scafctl/entra/1.0.0/provider.json`

```json
{
  "protocol_version": 6,
  "name": "entra",
  "namespace": "scafctl",
  "version": "1.0.0",
  "schema": {
    "type": "object",
    "properties": {
      "tenant": { "type": "string" },
      "clientId": { "type": "string" },
      "scopes": {
        "type": "array",
        "items": { "type": "string" }
      },
      "noBrowser": { "type": "boolean", "default": false }
    },
    "required": ["tenant", "clientId"],
    "additionalProperties": false
  },
  "capabilities": {
    "resolvers": false,
    "actions": false,
    "auth": true
  }
}
```

**CLI usage**:
```bash
scafctl auth login entra --tenant contoso.com --client-id abc123
```

The provider handles OAuth flows, token storage, and refresh logic. Auth providers are invoked via `scafctl auth` commands, not through resolvers or actions.

## Lifecycle

- **Initialization** — scafctl lazily launches providers only when referenced in the current execution graph.
- **Concurrency** — Each provider process can handle multiple RPC streams; scafctl multiplexes requests per provider instance.
- **Shutdown** — After run completion, scafctl sends the Terraform close message and terminates idle processes.

## Future Extensions

- Integrate Terraform registry discovery endpoints for version constraints (`~> 1.0`).
- Support signed provider manifests to enforce supply-chain integrity.
- Add optional WASM providers that reuse the same `provider.json` descriptor but load in-process.
- Publish design deep dives alongside these docs once RFCs for resolver/action plugins land.

## Catalog Integration

- Provider plugins live under the unified catalog layout described in `catalog.md`: `providers/<namespace>/<name>/index.json` enumerates versions, while each version folder hosts a `build.json` plus the published binaries/descriptor. `catalog.json.types.providers` advertises the top-level collection for discovery.
- Solutions reference providers via catalog IDs (`provider: scafctl/shell@1.0.0`). The CLI resolves those IDs to cached binaries and ensures version ranges declared in `spec.providers` are satisfied.
- Provider metadata follows the same JSON-only contract as other catalog assets to keep air-gapped replication simple. `build.json.primaryArtifact` points to the descriptor (`provider.json`) so consumers know which file to hydrate first. See `catalog.md` for the publishing workflow and `testing.md` for catalog validation steps.
