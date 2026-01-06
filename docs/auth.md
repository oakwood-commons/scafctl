# Authentication and Authorization

## Purpose

Authentication in scafctl is declarative, provider-driven, and execution-agnostic.

Providers declare what kind of token they require. scafctl or an external executor decides how that token is obtained and supplied. For local users, scafctl can initiate interactive authentication flows. For APIs and workflow engines, credentials are injected explicitly.

Authentication is a system concern, not a provider implementation detail.

---

## Core Principles

- Providers declare auth requirements, never credentials
- Token acquisition is separated from provider execution
- Long-lived credentials are managed centrally
- Execution tokens are short-lived and ephemeral
- Render mode never acquires credentials
- Raw secrets and tokens never appear in solution files

---

## Auth Providers

Auth providers define how identities are authenticated and how tokens are minted.

Examples:
- `entra`
- `github`
- `gcloud`

Auth providers are responsible for:
- Supporting one or more authentication flows
- Managing refresh tokens or equivalent credentials
- Minting execution tokens for providers
- Normalizing token metadata and claims

Auth providers are not action providers and do not perform side effects outside authentication.

---

## CLI Authentication

Local users authenticate explicitly using the `auth` command.

~~~bash
scafctl auth login entra
~~~

Behavior:
- Initiates device code flow
- User authenticates in a browser
- A refresh token (or equivalent credential) is obtained
- Credentials are stored securely by scafctl
- No provider execution occurs

This establishes a local identity context for future runs.

Logout clears stored credentials:

~~~bash
scafctl auth logout entra
~~~

---

## Credential Storage

Auth providers manage credential storage.

Rules:
- Refresh tokens are stored securely
- Storage is provider-specific
- Tokens are scoped to the provider and tenant
- Credentials are never embedded in solutions
- Credentials are never exposed to providers directly

Exact storage mechanisms are implementation-specific but must meet platform security expectations.

---

## Token Acquisition Model

### Execution Tokens

When a provider requires authentication:

1. The provider declares required auth type and scopes
2. scafctl selects the matching auth provider
3. The auth provider mints a short-lived execution token
4. The token is injected into provider execution
5. The token is discarded after use

Providers never see refresh tokens.

---

### Example Provider Declaration

~~~yaml
caasByID:
  authenticationType: entra
  scope: "{{ .platform.scope }}"
  headers:
    Accept: application/json, application/problem+json
  method: GET
  uri: https://{{ .platform.host }}/platform-assets/api/v1/kubenamespace/find?clientID={{ .platformClientID }}
~~~

Meaning:
- The provider requires an Entra-issued access token
- The token must include the declared scope
- The provider does not care how the token is obtained

---

## Render Mode Behavior

When running in render mode:

~~~bash
scafctl render solution:myapp
~~~

- No authentication flows are initiated
- No tokens are minted
- Auth requirements are emitted declaratively
- External executors are responsible for supplying tokens

Rendered output includes auth requirements but no credentials.

---

## External Executors

When an external system executes a rendered graph:

- It must supply tokens matching declared auth requirements
- Tokens are injected as execution inputs
- scafctl does not manage credentials in this mode

This allows integration with:
- CI systems
- Workflow engines
- Platform-native identity systems

---

## Auth Context (`_._.auth`)

Token metadata and claims may be exposed for conditional logic.

Important constraints:
- Only rendered claims are exposed
- No raw tokens
- No secrets
- No signatures

Example:

~~~yaml
when:
  expr: _._.auth.tenantId == "c990bb7a-51f4-439b-bd36-9c07fb1041c0"
~~~

Allowed data includes:
- Issuer
- Tenant ID
- Subject
- Client ID
- Scopes
- Expiration timestamps

---

## Flow Summary

### Local CLI Execution

1. User logs in via `scafctl auth login`
2. Refresh token is stored securely
3. Provider declares auth requirements
4. Auth provider mints execution token
5. Provider executes with injected token

---

### Render and External Execution

1. Resolvers are executed
2. Actions are rendered
3. Auth requirements are emitted
4. External executor supplies tokens
5. Providers execute outside scafctl

---

## Design Constraints

- Providers must never acquire credentials
- Auth providers must manage refresh tokens
- Execution tokens must be short-lived
- Render mode must not initiate auth
- `_._.auth` exposes claims only
- Secrets must never appear in solution artifacts

---

## Why This Model Works

This design:
- Matches cloud-native auth patterns
- Supports human and machine execution
- Avoids secret sprawl
- Enables portable execution graphs
- Keeps providers simple and auditable

---

## Summary

Authentication in scafctl is explicit, declarative, and provider-driven. Auth providers manage identity and token minting. Providers declare required token types and scopes. scafctl supports interactive login for local users and clean integration with external executors, while keeping secrets out of configuration and artifacts.
