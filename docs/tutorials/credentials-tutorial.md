---
title: "Credential Injection & Redaction Tutorial"
weight: 41
---

# Credential Injection & Redaction Tutorial

This tutorial shows how to inject secrets (`scrt:`) and short-lived authentication tokens (`tkn:`) directly into provider inputs -- both top-level fields and nested maps or arrays -- while keeping credential material out of rendered plans, dry-run previews, action inputs, logs, and terminal streams.

## Prerequisites

- `scafctl` installed and available in your `PATH`
- A stored secret (`scafctl secrets set`) or an authenticated session (`scafctl auth login`)

## Why `scrt:` and `tkn:` ValueRefs?

Previously, passing a secret or access token to a provider required defining an intermediate `sensitive: true` resolver with the `secret` provider (or relying on a specific provider's bespoke `autoAuth` support) and wiring `rslvr:` references into downstream actions.

`ValueRef` supports two first-class credential branches alongside `rslvr:`, `expr:`, and `tmpl:`:

- **`scrt:`** -- reads a user secret directly from the encrypted secret store at execution time.
- **`tkn:`** -- mints or retrieves a cached access token from a registered auth handler (`entra`, `github`, `gcp`, or an auth-handler plugin) at execution time.

Both branches work anywhere a `ValueRef` is accepted, including **nested inside literal maps and arrays** such as `exec` environment maps, `http` headers, `git` credentials, `oci` registry auth, `spec.calls`, `state`, and `scafctl run provider`.

---

## Syntax Reference

### `scrt:` -- Secret Store Lookup

`scrt:` accepts a user secret name matching `^[a-zA-Z0-9._-]+$`:

```yaml
inputs:
  apiKey:
    scrt: deploy-api-key
```

Internal auth-handler secrets (`scafctl.*` or `<binary>.*` when embedded) are reserved and rejected both at lint time (`invalid-credential-ref`) and at runtime.

### `tkn:` -- Auth Handler Token

`tkn:` supports a string shorthand naming the auth handler, or an object form (`TokenRef`) when you need a specific `scope`, a named `profile`, a cluster/enterprise `hostname`, or `minValidFor` refresh threshold:

```yaml
# Shorthand: uses default profile and handler defaults
inputs:
  token:
    tkn: github
```

```yaml
# Object form: fine-grained token options
inputs:
  token:
    tkn:
      handler: entra
      scope: https://management.azure.com/.default
      profile: prod-subscription
      minValidFor: 5m
```

For host-aware or cluster-scoped auth handlers (such as GitHub Enterprise Server or cluster-targeted OIDC plugins), specify `hostname`:

```yaml
inputs:
  token:
    tkn:
      handler: k8s-oidc
      hostname: cluster-a.example.com
```

Inspect both schemas from the CLI at any time:

```bash
scafctl explain valueref
scafctl explain tokenref
```

---

## Nested Credential Injection (`env`, `headers`, `spec.calls`)

Credential `ValueRef` objects can be placed directly inside nested maps and slices without wrapping the outer map in a template or CEL expression:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: deploy-service
  version: 1.0.0
spec:
  calls:
    notify_api:
      description: Send an authenticated webhook notification
      provider: http
      args:
        body:
          type: string
          required: true
      inputs:
        url: https://api.example.com/v1/notify
        method: POST
        body:
          expr: _.args.body
        headers:
          X-Webhook-Secret:
            scrt: webhook-hmac-secret

  workflow:
    actions:
      run-migrations:
        provider: exec
        inputs:
          command: ./scripts/migrate.sh
          env:
            DB_PASSWORD:
              scrt: prod-db-password
            GCP_ACCESS_TOKEN:
              tkn:
                handler: gcp
                scope: https://www.googleapis.com/auth/cloud-platform

      send-notification:
        dependsOn:
          - run-migrations
        call: notify_api
        args:
          body: '{"status":"migrated"}'
```

### Ad-Hoc Provider Runs (`scafctl run provider`)

You can also pass `scrt:` and `tkn:` references via `@inputs.yaml` (or `@inputs.json`) when testing providers directly:

~~~bash
scafctl secrets set api-token "my-test-secret-value"

cat > inputs.yaml <<'EOF'
url: https://httpbin.example.com/headers
headers:
  X-API-Token:
    scrt: api-token
EOF

scafctl run provider http @inputs.yaml --dry-run -o json
~~~

---

## Using Credentials in Resolvers

When a resolver uses `scrt:` or `tkn:` in `resolve`, `transform`, or `validate` inputs -- for example to format a composite `Bearer <token>` header or connection string -- mark the resolver `sensitive: true`:

```yaml
spec:
  resolvers:
    auth_header:
      type: string
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                tkn: github
      transform:
        with:
          - provider: cel
            inputs:
              expression: '"Bearer " + __self'
```

- Resolvers marked `sensitive: true` retain their resolved string in memory so downstream steps can consume `_.auth_header`, and automatically register the derived value in the run-level redaction tracker.
- Resolvers **not** marked `sensitive: true` have any tracked credential substrings scrubbed to `[REDACTED]` before their value is stored, and `scafctl lint` emits an `unmarked-sensitive-credential-resolver` warning.

---

## Security & Redaction Guarantees

scafctl enforces defense-in-depth protections whenever `scrt:` or `tkn:` is used:

1. **Plan, Render, and Dry-Run Placeholders**
   - `scafctl render solution`, `scafctl run solution --dry-run`, `scafctl run provider --dry-run`, and the MCP `preview_action` / `dry_run_solution` tools **never** contact the secret store or mint tokens.
   - Instead, resolver-side credential references substitute deterministic placeholders (`<redacted:scrt:<name>>` and `<redacted:tkn:<handler>>`) and deferred action inputs display symbolic summaries (`scrt:<name>` and `tkn:<handler>`) so rendered graphs, plans, and `--fingerprint` hashes reflect structural changes without exposing plaintext credentials.

2. **Deferred Action Materialization**
   - Action inputs containing `scrt:` or `tkn:` are deferred to execution time alongside runtime expressions, and `ActionResult.Inputs` stores redacted values rather than raw credentials.

3. **Stream & Output Scrubbing (`[REDACTED]`)**
   - Every resolved secret or token string (`>= 4` bytes, including trimmed values and individual lines `>= 4` bytes of multi-line credentials such as PEM keys) is registered in a run-scoped `redact.Tracker`.
   - Terminal streams (`IOStreams.Out` / `IOStreams.ErrOut`, used by `exec` `passthrough: true` or `echo: true`) are wrapped in a rolling holdback `StreamWriter` that catches secrets even when split across `Write()` chunk boundaries.
   - Non-sensitive `Output.Data` maps/strings (such as captured `stdout` and `stderr`), `Output.Warnings`, `Output.Metadata`, and returned error messages are scrubbed before leaving the executor.

4. **GitHub Actions Log Masking**
   - When `GITHUB_ACTIONS=true` is set in the environment, `scafctl` automatically emits `::add-mask::<line>` workflow commands for each tracked secret prior to provider execution so runner logs apply secondary masking as well.

5. **Read-Only Plugin Secret Broker**
   - External provider plugins (such as `scafctl-plugin-secret` or `scafctl-plugin-oci`) can read user secrets over gRPC `HostService.GetSecret` and `HostService.ListSecrets` when a secret store is configured.
   - Internal `<binary>.*` auth secrets and mutating RPCs (`SetSecret`, `DeleteSecret`) remain strictly denied to provider plugins; auth-handler plugins continue to receive scoped read/write access only to their own `<binary>.auth.<handler>.*` namespace.

---

## Linting Credential References

`scafctl lint` validates credential references statically before execution:

| Rule | Severity | What it checks |
| ---- | -------- | -------------- |
| `invalid-credential-ref` | `error` | Rejects empty/malformed `scrt:` names, reserved `<binary>.*` internal secret prefixes, empty `tkn.handler`, invalid `tkn.minValidFor` durations, and malformed nested `scrt:`/`tkn:` objects. |
| `unmarked-sensitive-credential-resolver` | `warning` | Flags resolvers whose inputs contain `scrt:` or `tkn:` without `sensitive: true`. |

Run `scafctl lint rule <rule>` for examples and remediation steps:

```bash
scafctl lint rule invalid-credential-ref
scafctl lint rule unmarked-sensitive-credential-resolver
```

## Related Guides

- [Authentication Tutorial](auth-tutorial.md) -- Logging in with Entra, GitHub, GCP, and custom auth handlers
- [Security Hardening](security-hardening.md) -- Keyring security, SSRF policy, plugin digest verification, and API server hardening
- [Dry-Run Tutorial](dryrun-tutorial.md) -- Inspecting execution plans safely before applying changes
